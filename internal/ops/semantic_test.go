package ops

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/raglit"
	"github.com/iodesystems/parasolve/internal/store"
)

// fakeRaglit is a stand-in for a raglit daemon: one project, one token, and a
// search tool that returns reply (after delay). It records the queries.
type fakeRaglit struct {
	*httptest.Server
	reply   string
	delay   time.Duration
	queries atomic.Int32
}

func newFakeRaglit(t *testing.T, reply string) *fakeRaglit {
	t.Helper()
	f := &fakeRaglit{reply: reply}
	s := server.NewMCPServer("fake-raglit", "0")
	s.AddTool(mcp.NewTool("search", mcp.WithString("query", mcp.Required()), mcp.WithNumber("limit")),
		func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			f.queries.Add(1)
			select {
			case <-time.After(f.delay):
			case <-ctx.Done():
			}
			return mcp.NewToolResultText(f.reply), nil
		})
	h := server.NewStreamableHTTPServer(s, server.WithStateLess(true))
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp/life", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	})
	f.Server = httptest.NewServer(mux)
	t.Cleanup(f.Close)
	return f
}

func connect(t *testing.T, d Deps, url, token string) {
	t.Helper()
	if err := raglit.Save(d.Store.Info().StateDir, raglit.Config{URL: url, Project: "life", Token: token}); err != nil {
		t.Fatal(err)
	}
}

func addNote(t *testing.T, d Deps, id, title, body string) {
	t.Helper()
	_, err := d.Store.Create(ctx, model.Document{ID: id, Folder: model.FolderInbox, Title: title, Body: body,
		Meta: &model.Inbox{Common: model.Common{Type: model.TypeInbox}}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestSearchMergesMeaningHits(t *testing.T) {
	d := newDeps(t)
	addNote(t, d, "inbox-a", "Heron count", "count the heron")
	addNote(t, d, "inbox-b", "Wading birds", "egrets and storks")
	addNote(t, d, "inbox-c", "Cranes", "tall birds")
	reply, _ := json.Marshal(map[string]any{"hits": []raglit.Hit{
		{Rel: "inbox/inbox-a.md", Snippet: "dup of an exact hit"},
		{Rel: "inbox/inbox-b.md", Snippet: "egrets\x1b[31m and\n storks", Origin: "frontmatter", Caveat: "title only"},
		{Rel: "inbox/inbox-b.md", Snippet: "second fragment, body", Origin: ""},
		{Rel: "inbox/inbox-c.md", Snippet: "a caption", Origin: "identity"},
		{Rel: "inbox/inbox-gone.md", Snippet: "not in the vault"},
		{Rel: "../../etc/passwd", Snippet: "no .md"},
		{Rel: "../../../secret.md", Snippet: "traversal"},
	}})
	f := newFakeRaglit(t, string(reply))
	connect(t, d, f.URL, "secret")

	out, err := Search.Run(ctx, d, SearchInput{Query: "heron"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Degraded != "" {
		t.Fatalf("degraded: %s", out.Degraded)
	}
	var got []string
	for _, h := range out.Hits {
		got = append(got, h.ID+":"+h.Match)
	}
	if want := "inbox-a:exact inbox-b:meaning inbox-c:meaning"; strings.Join(got, " ") != want {
		t.Fatalf("hits = %v, want %s", got, want)
	}
	b, c := out.Hits[1], out.Hits[2]
	if b.Passage != "second fragment, body" || b.Caveat != "" {
		t.Errorf("a body hit must beat a frontmatter hit: %+v", b)
	}
	if !strings.Contains(c.Caveat, "generated") || c.Title != "Cranes" {
		t.Errorf("caption hit: %+v", c)
	}
	if b.Title != "Wading birds" {
		t.Errorf("title must come from the vault: %+v", b)
	}

	// The filters apply to meaning hits too.
	out, err = Search.Run(ctx, d, SearchInput{Query: "heron", Type: "task"})
	if err != nil || len(out.Hits) != 0 {
		t.Errorf("type filter: %+v %v", out.Hits, err)
	}

	// The egress record names the destination and holds no query text.
	evs, err := d.Store.Events(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range evs {
		raw, _ := json.Marshal(e)
		if strings.Contains(string(raw), "heron") && e.Op == "egress" {
			t.Errorf("query text in event: %s", raw)
		}
		if e.Op == "egress" && strings.Contains(string(raw), "raglit:search") {
			n++
		}
	}
	if n != 2 {
		t.Errorf("%d raglit egress events, want 2", n)
	}
}

func TestSearchSanitizesSnippet(t *testing.T) {
	d := newDeps(t)
	addNote(t, d, "inbox-b", "Wading birds", "egrets")
	reply, _ := json.Marshal(map[string]any{"hits": []raglit.Hit{{Rel: "inbox/inbox-b.md", Snippet: "a\x1b[31m b\n\tc" + strings.Repeat("x", 500)}}})
	f := newFakeRaglit(t, string(reply))
	connect(t, d, f.URL, "secret")
	out, err := Search.Run(ctx, d, SearchInput{Query: "heron"})
	if err != nil || len(out.Hits) != 1 {
		t.Fatalf("%+v %v", out, err)
	}
	p := out.Hits[0].Passage
	if strings.ContainsAny(p, "\x1b\n\t") || len(p) > 310 || !strings.HasPrefix(p, "a[31m b c") {
		t.Errorf("passage = %q", p)
	}
}

func TestSearchDegrades(t *testing.T) {
	d := newDeps(t)
	addNote(t, d, "inbox-a", "Heron count", "count the heron")
	f := newFakeRaglit(t, `{"hits":[]}`)

	cases := []struct {
		name  string
		setup func()
		want  string
	}{
		{"wrong token", func() { connect(t, d, f.URL, "wrong") }, "exact only: raglit refused the token"},
		{"slow", func() { connect(t, d, f.URL, "secret"); f.delay = 5 * time.Second }, "exact only: raglit timed out"},
		{"bad reply", func() { connect(t, d, f.URL, "secret"); f.delay = 0; f.reply = "not json" }, "exact only: raglit reply unreadable"},
		{"down", func() { connect(t, d, "http://127.0.0.1:1", "secret") }, "exact only: raglit unreachable"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.setup()
			start := time.Now()
			out, err := Search.Run(ctx, d, SearchInput{Query: "heron"})
			if err != nil {
				t.Fatalf("a raglit failure must not be an error: %v", err)
			}
			if out.Degraded != c.want {
				t.Errorf("degraded = %q, want %q", out.Degraded, c.want)
			}
			if len(out.Hits) != 1 || out.Hits[0].ID != "inbox-a" || out.Hits[0].Match != MatchExact {
				t.Errorf("hits = %+v", out.Hits)
			}
			if time.Since(start) > 3*time.Second {
				t.Errorf("took %s", time.Since(start))
			}
			if strings.Contains(out.Degraded, "secret") || strings.Contains(out.Degraded, f.URL) {
				t.Errorf("degraded line leaks: %s", out.Degraded)
			}
		})
	}
}

func TestSearchWithoutRaglitIsExactOnly(t *testing.T) {
	d := newDeps(t)
	addNote(t, d, "inbox-a", "Heron count", "count the heron")
	out, err := Search.Run(ctx, d, SearchInput{Query: "heron"})
	if err != nil || out.Degraded != "" || len(out.Hits) != 1 || out.Hits[0].Match != MatchExact {
		t.Fatalf("%+v %v", out, err)
	}
}

// stub answers Get from a map, for resolveID.
type stub struct {
	store.Store
	docs map[string]model.Document
}

func (s stub) Get(_ context.Context, id string) (model.Document, error) {
	d, ok := s.docs[id]
	if !ok {
		return d, model.ErrNotFound
	}
	return d, nil
}

func TestResolveID(t *testing.T) {
	pol := model.Document{ID: "policy-x", Meta: &model.Policy{Common: model.Common{Type: model.TypePolicy}}}
	d := Deps{Store: stub{docs: map[string]model.Document{"policy-x": pol}}}
	cases := []struct{ rel, id, via string }{
		{"policies/policy-x.md", "policy-x", ""},
		{"policies/policy-x.source.md", "policy-x", "policy-x.source"},
		{"policies/policy-y.source.md", "policy-y.source", ""},
		{"/abs/path/inbox-a.md", "inbox-a", ""},
		{"../../x.md", "x", ""},
		{"..", "", ""},
		{"notes.txt", "", ""},
		{"", "", ""},
	}
	for _, c := range cases {
		id, via := resolveID(ctx, d, c.rel)
		if id != c.id || via != c.via {
			t.Errorf("resolveID(%q) = %q, %q; want %q, %q", c.rel, id, via, c.id, c.via)
		}
	}
}
