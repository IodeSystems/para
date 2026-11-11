package mcpserver

import (
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/iodesystems/para/internal/ops"
)

// notified records the methods and log texts a client receives.
type notified struct {
	mu      sync.Mutex
	methods []string
	texts   []string
}

func (n *notified) on(c *client.Client) {
	c.OnNotification(func(m mcp.JSONRPCNotification) {
		n.mu.Lock()
		defer n.mu.Unlock()
		n.methods = append(n.methods, m.Method)
		if d, ok := m.Params.AdditionalFields["data"].(string); ok {
			n.texts = append(n.texts, d)
		}
	})
}

func (n *notified) has(method, text string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, m := range n.methods {
		if m == method {
			if text == "" {
				return true
			}
			for _, x := range n.texts {
				if strings.Contains(x, text) {
					return true
				}
			}
		}
	}
	return false
}

// wait is has, retried for up to two seconds: delivery is asynchronous.
func (n *notified) wait(method, text string) bool {
	for range 40 {
		if n.has(method, text) {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func startWith(t *testing.T, o Options) (*client.Client, *mcp.InitializeResult, *notified) {
	t.Helper()
	s, _ := newVault(t)
	srv, err := New(ops.Deps{Store: s}, o)
	if err != nil {
		t.Fatal(err)
	}
	c, err := client.NewInProcessClient(srv.MCP())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	n := &notified{}
	n.on(c)
	if err := c.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	res, err := c.Initialize(t.Context(), mcp.InitializeRequest{})
	if err != nil {
		t.Fatal(err)
	}
	return c, res, n
}

// After para serve restarts on a new binary, an HTTP session that reconnects
// is told in the instructions, and on its first call gets tools/list_changed
// and the log line; the next call says nothing more.
func TestHTTPSessionAfterAnUpgradeGetsListChanged(t *testing.T) {
	seen := filepath.Join(t.TempDir(), "mcp_seen_http.json")
	_, first, n0 := startWith(t, Options{Version: "v0.3.0", SeenPath: seen, HTTP: true})
	if strings.Contains(first.Instructions, "para upgraded") || first.Capabilities.Tools == nil || !first.Capabilities.Tools.ListChanged {
		t.Fatalf("first session: %+v %q", first.Capabilities.Tools, first.Instructions)
	}
	c, res, n := startWith(t, Options{Version: "v0.4.0", SeenPath: seen, HTTP: true})
	if !strings.Contains(res.Instructions, UpgradedLine("v0.3.0", "v0.4.0")) {
		t.Fatalf("instructions lack the upgrade: %q", res.Instructions)
	}
	r := call(t, c, "lint", "")
	if !strings.Contains(allText(r), "para upgraded v0.3.0 → v0.4.0: call changes") {
		t.Fatalf("first call lacks the note: %s", allText(r))
	}
	if !n.wait(string(mcp.MethodNotificationToolsListChanged), "") || !n.wait("notifications/message", "para upgraded v0.3.0 → v0.4.0") {
		t.Fatalf("notifications %v %v", n.methods, n.texts)
	}
	if r := call(t, c, "lint", ""); strings.Contains(allText(r), "para upgraded") {
		t.Fatal("the note repeated")
	}
	if n0.has(string(mcp.MethodNotificationToolsListChanged), "") {
		t.Fatal("a first session was told the tools changed")
	}
	// A stdio server never sends list_changed for the seen record.
	_, plain, _ := startWith(t, Options{Version: "v0.5.0", SeenPath: seen})
	if strings.Contains(plain.Instructions, "para upgraded") {
		t.Fatal("a stdio server announced the HTTP upgrade line")
	}
}

func TestNoticeInInstructionsAndOncePerSession(t *testing.T) {
	notice := "para v0.4.0 is available (this is v0.3.0): it installs automatically at a quiet moment"
	c, res, _ := startWith(t, Options{Version: "v0.3.0", Notice: func() string { return notice }})
	if !strings.Contains(res.Instructions, "Update: "+notice) || !strings.Contains(res.Instructions, "para upgrade") {
		t.Fatalf("instructions %q", res.Instructions)
	}
	if r := call(t, c, "lint", ""); !strings.Contains(allText(r), notice) || !strings.Contains(allText(r), "Vault upgrade") {
		t.Fatalf("first call: %s", allText(r))
	}
	if r := call(t, c, "lint", ""); strings.Contains(allText(r), notice) {
		t.Fatal("the notice repeated in one session")
	}
	_, none, _ := startWith(t, Options{Version: "v0.3.0", Notice: func() string { return "" }})
	if strings.Contains(none.Instructions, "Update:") {
		t.Fatal("an empty notice was shown")
	}
}

func TestAllowRefusesATool(t *testing.T) {
	c, _, _ := startWith(t, Options{Version: "v1", Allow: func(op string, read bool) error {
		if !read {
			return errors.New("key lacks scope vault:write")
		}
		return nil
	}})
	if r := call(t, c, "lint", ""); r.IsError {
		t.Fatalf("a read was refused: %s", allText(r))
	}
	r := call(t, c, "task_add", `{"title":"x","why":"test"}`)
	if !r.IsError || !strings.Contains(allText(r), "vault:write") {
		t.Fatalf("a write was allowed: %s", allText(r))
	}
}

func TestStaleMessageNamesTheHTTPTransport(t *testing.T) {
	if !strings.Contains(StaleMessage, "HTTP transport") || !strings.Contains(staleMessage("v1", "v2", true), "HTTP transport") {
		t.Fatal("no HTTP hint")
	}
}

// A session that ends leaves nothing in the per-session notes.
func TestForgetSessionDropsItsNotes(t *testing.T) {
	s := &Server{sess: newSessionNotes()}
	s.sess.upgraded["a"], s.sess.notice["a"] = true, "n"
	s.sess.upgraded["b"] = true
	s.ForgetSession("a")
	if _, ok := s.sess.upgraded["a"]; ok || len(s.sess.notice) != 0 || !s.sess.upgraded["b"] {
		t.Fatalf("notes after forget: %v %v", s.sess.upgraded, s.sess.notice)
	}
}

// A notice is one line of at most noticeMax bytes, cut on a rune boundary.
func TestNoticeIsCapped(t *testing.T) {
	long := strings.Repeat("é", noticeMax)
	got := clean(long)
	if len(got) > noticeMax || !utf8.ValidString(got) || got == "" {
		t.Fatalf("clean of a long notice: %d bytes, valid %v", len(got), utf8.ValidString(got))
	}
	if clean("a\nb") != "" || clean(" ok ") != "ok" {
		t.Fatal("clean")
	}
}
