package index

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

var ctx = context.Background()

// fake is an in-memory vault. gets counts Get calls per id.
type fake struct {
	docs map[string]model.Document
	gets map[string]int
}

func newFake(docs ...model.Document) *fake {
	f := &fake{docs: map[string]model.Document{}, gets: map[string]int{}}
	for _, d := range docs {
		f.put(d)
	}
	return f
}

func (f *fake) put(d model.Document) {
	d.Rev = fmt.Sprintf("rev:%s:%s:%s", d.ID, d.Title, d.Body)
	f.docs[d.ID] = d
}

func (f *fake) Refs(context.Context) ([]model.Ref, error) {
	var out []model.Ref
	for _, d := range f.docs {
		out = append(out, model.Ref{ID: d.ID, Folder: d.Folder, Type: d.Meta.Header().Type, Title: d.Title, Rev: d.Rev})
	}
	return out, nil
}

func (f *fake) Get(_ context.Context, id string) (model.Document, error) {
	f.gets[id]++
	d, ok := f.docs[id]
	if !ok {
		return d, model.ErrNotFound
	}
	return d, nil
}

func src(url string) model.Source {
	return model.Source{URL: url, Fidelity: model.FidelityExact}
}

func policy(id, title, body string, verified bool) model.Document {
	return model.Document{ID: id, Folder: model.FolderPolicies, Title: title, Body: body,
		Meta: &model.Policy{Common: model.Common{Type: model.TypePolicy}, Followed: model.Followed{Source: src("https://wiki.acme.example/p/" + id), Verified: verified}}}
}

func transcript(id, title, body string) model.Document {
	return model.Document{ID: id, Folder: model.FolderPolicies, Title: title, Body: body,
		Meta: &model.Transcript{Common: model.Common{Type: model.TypeTranscript}, Of: model.LinkTo("policy-logging"), Source: src("https://wiki.acme.example/p/logging")}}
}

func note(id, title, body string) model.Document {
	return model.Document{ID: id, Folder: model.FolderInbox, Title: title, Body: body,
		Meta: &model.Inbox{Common: model.Common{Type: model.TypeInbox}}}
}

func openIndex(t *testing.T) (*Index, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "index.sqlite")
	ix, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ix.Close() })
	return ix, p
}

func ids(hs []Hit) []string {
	out := []string{}
	for _, h := range hs {
		out = append(out, h.ID)
	}
	return out
}

func TestSearch(t *testing.T) {
	v := newFake(
		policy("policy-logging", "Logging policy", "Services log every authentication failure.", false),
		transcript("policy-logging.source", "Logging policy (transcript)", "Verbatim: services must log authentication failures with a request id."),
		note("inbox-a", "Buy pelican food", "Remember the pelicans."),
		note("inbox-b", "Other", "nothing relevant"),
	)
	ix, _ := openIndex(t)
	cases := []struct {
		name, query string
		want        []string
	}{
		{"both kinds", "authentication", []string{"policy-logging", "policy-logging.source"}},
		{"stemming", "failures", []string{"policy-logging", "policy-logging.source"}},
		{"all words must match", "authentication pelican", []string{}},
		{"title only", "pelican", []string{"inbox-a"}},
		{"phrase chars are data", `"authentication" OR (`, []string{}},
		{"fts operators are data", "NOT authentication", []string{}},
		{"no word characters", "!!! ---", []string{}},
		{"empty", "  ", []string{}},
		{"miss", "zebrafish", []string{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			hs, err := ix.Search(ctx, v, c.query, 10)
			if err != nil {
				t.Fatal(err)
			}
			got := ids(hs)
			if fmt.Sprint(sorted(got)) != fmt.Sprint(sorted(c.want)) {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
}

func sorted(s []string) []string {
	out := append([]string{}, s...)
	for i := range out {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func TestHitFields(t *testing.T) {
	v := newFake(
		policy("policy-logging", "Logging policy", "Services log every authentication failure.", false),
		transcript("policy-logging.source", "Logging policy (transcript)", "Verbatim authentication text."),
		policy("policy-verified", "Reviewed", "authentication reviewed", true),
		note("inbox-a", "authentication note", "x"),
	)
	ix, _ := openIndex(t)
	hs, err := ix.Search(ctx, v, "authentication", 10)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]Hit{}
	for _, h := range hs {
		by[h.ID] = h
	}
	cases := []struct {
		id, kind, url, path string
		unverified          bool
	}{
		{"policy-logging", KindSummary, "https://wiki.acme.example/p/policy-logging", "policies/policy-logging.md", true},
		{"policy-logging.source", KindTranscript, "https://wiki.acme.example/p/logging", "policies/policy-logging.source.md", false},
		{"policy-verified", KindSummary, "https://wiki.acme.example/p/policy-verified", "policies/policy-verified.md", false},
		{"inbox-a", "inbox", "", "inbox/inbox-a.md", false},
	}
	for _, c := range cases {
		h, ok := by[c.id]
		if !ok {
			t.Errorf("%s: no hit", c.id)
			continue
		}
		if h.Kind != c.kind || h.SourceURL != c.url || h.Path != c.path || h.Unverified != c.unverified {
			t.Errorf("%s: %+v", c.id, h)
		}
		if h.Passage == "" || h.Title == "" {
			t.Errorf("%s: empty passage or title: %+v", c.id, h)
		}
	}
	if p := by["policy-logging"].Passage; !contains(p, "<<authentication>>") {
		t.Errorf("passage = %q", p)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestSearchLimit(t *testing.T) {
	v := newFake()
	for i := range 5 {
		v.put(note(fmt.Sprintf("inbox-%d", i), "pelican", "pelican"))
	}
	ix, _ := openIndex(t)
	for limit, want := range map[int]int{1: 1, 3: 3, 0: 5, 50: 5} {
		hs, err := ix.Search(ctx, v, "pelican", limit)
		if err != nil || len(hs) != want {
			t.Errorf("limit %d: %d hits, err %v", limit, len(hs), err)
		}
	}
}

// Stale-safe: an edit, an add, and a delete show in the next search, and
// unchanged documents are not read again.
func TestSearchTracksVault(t *testing.T) {
	v := newFake(note("inbox-a", "alpha", "pelican"), note("inbox-b", "beta", "heron"))
	ix, _ := openIndex(t)
	search := func(q string) []string {
		hs, err := ix.Search(ctx, v, q, 10)
		if err != nil {
			t.Fatal(err)
		}
		return ids(hs)
	}
	if got := search("pelican"); fmt.Sprint(got) != "[inbox-a]" {
		t.Fatalf("got %v", got)
	}
	v.put(note("inbox-a", "alpha", "ibis"))
	v.put(note("inbox-c", "gamma", "pelican"))
	delete(v.docs, "inbox-b")
	cases := []struct{ q, want string }{
		{"pelican", "[inbox-c]"},
		{"ibis", "[inbox-a]"},
		{"heron", "[]"},
	}
	for _, c := range cases {
		if got := search(c.q); fmt.Sprint(got) != c.want {
			t.Errorf("%s: got %v want %s", c.q, got, c.want)
		}
	}
	if v.gets["inbox-b"] != 1 || v.gets["inbox-a"] != 2 {
		t.Errorf("gets = %v", v.gets)
	}
}

func TestRebuild(t *testing.T) {
	v := newFake(note("inbox-a", "alpha", "pelican"), note("inbox-b", "beta", "heron"))
	ix, p := openIndex(t)
	st, err := ix.Rebuild(ctx, v)
	if err != nil || st.Documents != 2 || st.Added != 2 {
		t.Fatalf("stats %+v err %v", st, err)
	}
	// A rebuild into a deleted file gives the same answers.
	_ = ix.Close()
	if err := removeFiles(p); err != nil {
		t.Fatal(err)
	}
	ix2, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ix2.Close() }()
	if _, err := ix2.Rebuild(ctx, v); err != nil {
		t.Fatal(err)
	}
	hs, err := ix2.Search(ctx, v, "heron", 10)
	if err != nil || fmt.Sprint(ids(hs)) != "[inbox-b]" {
		t.Fatalf("hits %v err %v", hs, err)
	}
	st, err = ix2.Rebuild(ctx, v)
	if err != nil || st.Documents != 2 || st.Added != 2 || st.Removed != 0 {
		t.Fatalf("second rebuild %+v err %v", st, err)
	}
}

func TestOpenRecovers(t *testing.T) {
	cases := []struct {
		name  string
		write func(p string)
	}{
		{"garbage", func(p string) { _ = os.WriteFile(p, []byte("not a database, long enough to be a header......"), 0o600) }},
		{"old schema", func(p string) {
			ix, err := Open(p)
			if err != nil {
				t.Fatal(err)
			}
			_, _ = ix.db.Exec(`UPDATE meta SET v='0' WHERE k='version'`)
			_ = ix.Close()
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := filepath.Join(t.TempDir(), "index.sqlite")
			c.write(p)
			ix, err := Open(p)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = ix.Close() }()
			v := newFake(note("inbox-a", "alpha", "pelican"))
			hs, err := ix.Search(ctx, v, "pelican", 10)
			if err != nil || len(hs) != 1 {
				t.Fatalf("hits %v err %v", hs, err)
			}
		})
	}
}

func task(id, title, body, status, project, area, updated string) model.Document {
	return model.Document{ID: id, Folder: model.FolderProjects, Title: title, Body: body,
		Meta: &model.Task{Common: model.Common{Type: model.TypeTask, Created: "2026-01-01", Updated: model.Date(updated)},
			Status: model.TaskStatus(status), Project: model.Link(project), Area: model.Link(area)}}
}

func TestFindFilters(t *testing.T) {
	v := newFake(
		task("t-open", "Heron count", "count the heron", "todo", "[[p1]]", "", "2026-10-30"),
		task("t-done", "Heron photo", "photo of heron", "done", "[[p1]]", "", "2026-03-01"),
		task("t-area", "Heron feeder", "heron feeder", "doing", "", "[[a1]]", ""),
		note("inbox-a", "Heron note", "heron"),
	)
	ix, _ := openIndex(t)
	cases := []struct {
		name string
		q    Query
		want []string
	}{
		{"none", Query{}, []string{"inbox-a", "t-area", "t-done", "t-open"}},
		{"type", Query{Type: "task"}, []string{"t-area", "t-done", "t-open"}},
		{"type none", Query{Type: "project"}, []string{}},
		{"status exact", Query{Status: "done"}, []string{"t-done"}},
		{"status open", Query{Status: "open"}, []string{"t-area", "t-open"}},
		{"project", Query{Project: "p1"}, []string{"t-done", "t-open"}},
		{"project link form", Query{Project: "[[p1]]"}, []string{"t-done", "t-open"}},
		{"area", Query{Area: "a1"}, []string{"t-area"}},
		{"since uses updated", Query{Since: "2026-10-01"}, []string{"t-open"}},
		{"since falls back to created", Query{Since: "2026-01-01", Type: "task", Status: "open"}, []string{"t-area", "t-open"}},
		{"combined", Query{Type: "task", Status: "open", Project: "p1"}, []string{"t-open"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			c.q.Text = "heron"
			hs, err := ix.Find(ctx, v, c.q)
			if err != nil {
				t.Fatal(err)
			}
			got := ids(hs)
			sort.Strings(got)
			if fmt.Sprint(got) != fmt.Sprint(c.want) {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}
