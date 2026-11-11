package server

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"weak"

	"github.com/iodesystems/para/internal/keys"
	"github.com/iodesystems/para/internal/netguard"
	"github.com/iodesystems/para/internal/update"
)

const mcpInit = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`

func (f *fixture) mcp(token, session, body string) (status int, text, sid string) {
	f.t.Helper()
	h := map[string]string{"Accept": "application/json, text/event-stream"}
	if session != "" {
		h["Mcp-Session-Id"] = session
	}
	w := f.do(call{method: post, target: "/mcp", token: token, body: body, header: h})
	return w.Code, w.Body.String(), w.Header().Get("Mcp-Session-Id")
}

// /mcp serves the agent tools to a bearer key; each tool needs the scope of
// its REST route, so a read key reads and cannot write.
func TestMCPOverHTTPChecksEachToolsScope(t *testing.T) {
	f := newFixture(t)
	if code, _, _ := f.mcp("", "", mcpInit); code != 401 {
		t.Fatalf("no token: %d", code)
	}
	reader := f.key(f.keyA, "reader", netguard.ScopeVaultRead)
	code, body, sid := f.mcp(reader, "", mcpInit)
	if code != 200 || sid == "" || !strings.Contains(body, `"listChanged":true`) {
		t.Fatalf("initialize: %d %s", code, body)
	}
	f.mcp(reader, sid, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	_, body, _ = f.mcp(reader, sid, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"task_add","arguments":{"title":"x","why":"t"}}}`)
	if !strings.Contains(body, "key lacks scope vault:write") {
		t.Fatalf("a read key wrote: %s", body)
	}
	_, body, _ = f.mcp(reader, sid, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"lint","arguments":{}}}`)
	if strings.Contains(body, "lacks scope") || !strings.Contains(body, `"result"`) {
		t.Fatalf("a read was refused: %s", body)
	}
	// Another key does not share the session.
	other := f.key(f.keyA, "other", netguard.ScopeVaultRead)
	if code, _, _ := f.mcp(other, sid, `{"jsonrpc":"2.0","id":4,"method":"tools/list"}`); code != 404 {
		t.Fatalf("another key used the session: %d", code)
	}
}

// The update check's record in each vault holds ids only: no document, the
// default, and the destination.
func TestLogEgressIsIDsOnly(t *testing.T) {
	f := newFixture(t)
	if err := f.hub.LogEgress(t.Context(), "update:manifest"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"acme", "beta"} {
		t2, err := f.hub.tenant(id)
		if err != nil || t2 == nil {
			t.Fatal(id, err)
		}
		files, _ := filepath.Glob(filepath.Join(t2.path, "events", "*.jsonl"))
		var line string
		for _, p := range files {
			b, _ := os.ReadFile(p)
			for _, l := range strings.Split(string(b), "\n") {
				if strings.Contains(l, "update:manifest") {
					line = l
				}
			}
		}
		if !strings.Contains(line, `"op":"egress"`) || !strings.Contains(line, `"to":"update:manifest"`) || strings.Contains(line, "http") {
			t.Fatalf("%s: egress record %q", id, line)
		}
	}
}

func TestBusyAfterAWrite(t *testing.T) {
	f := newFixture(t)
	if why := f.srv.Busy(time.Now(), time.Minute); why != "" {
		t.Fatalf("busy before any request: %s", why)
	}
	tok := f.key(f.keyA, "w", netguard.ScopeVaultRead, netguard.ScopeVaultWrite)
	f.do(call{target: "/api/v1/update/status", token: tok})
	if why := f.srv.Busy(time.Now(), time.Minute); why != "" {
		t.Fatalf("a GET made the server busy: %s", why)
	}
	f.do(call{method: post, target: "/api/v1/tasks", token: tok, body: `{"title":"x","why":"t"}`})
	if why := f.srv.Busy(time.Now(), time.Minute); !strings.Contains(why, "request") {
		t.Fatalf("a REST POST did not count: %q", why)
	}
	if why := f.srv.Busy(time.Now().Add(2*time.Minute), time.Minute); why != "" {
		t.Fatalf("still busy after the window: %s", why)
	}
}

// Over MCP only a tool call that may write counts: initialize, a read, and
// request_wait (an agent that polls) leave the machine quiet.
func TestBusyCountsOnlyMCPWrites(t *testing.T) {
	f := newFixture(t)
	tok := f.key(f.keyA, "w", netguard.ScopeVaultRead, netguard.ScopeVaultWrite)
	_, _, sid := f.mcp(tok, "", mcpInit)
	f.mcp(tok, sid, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	f.mcp(tok, sid, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"lint","arguments":{}}}`)
	_, body, _ := f.mcp(tok, sid, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"request_wait","arguments":{"timeout_seconds":1}}}`)
	if !strings.Contains(body, "claimable") {
		t.Fatalf("request_wait: %s", body)
	}
	if why := f.srv.Busy(time.Now(), time.Hour); why != "" {
		t.Fatalf("reads and a wait made the server busy: %s", why)
	}
	// A write tool counts once its scope check passes, whatever it answers.
	f.mcp(tok, sid, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"task_add","arguments":{"title":"x","why":"t"}}}`)
	if why := f.srv.Busy(time.Now(), time.Hour); !strings.Contains(why, "request") {
		t.Fatalf("an MCP write did not count: %q", why)
	}
}

func TestUpdateStatusRoute(t *testing.T) {
	f := newFixture(t)
	tok := f.key(f.keyA, "r", netguard.ScopeVaultRead)
	w := f.do(call{target: "/api/v1/update/status", token: tok})
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"running": "test"`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}

// egressLines returns the egress records for dest in a vault's events.
func (f *fixture) egressLines(id, dest string) int {
	f.t.Helper()
	files, _ := filepath.Glob(filepath.Join(f.vaultPath(id), "events", "*.jsonl"))
	n := 0
	for _, p := range files {
		b, _ := os.ReadFile(p)
		n += strings.Count(string(b), `"to":"`+dest+`"`)
	}
	return n
}

// egress.default=deny (the default of para init) does not stop the update
// check: it fetches and each vault logs it. A vault with egress.update=deny
// stops it: no fetch, no record.
func TestUpdateCheckUnderADenyVault(t *testing.T) {
	f := newFixture(t)
	if id, err := f.hub.UpdateDenied(t.Context()); err != nil || id != "" {
		t.Fatalf("egress.default=deny alone blocked updates: %q %v", id, err)
	}
	fetches := 0
	a := &update.Auto{
		StatePath: filepath.Join(t.TempDir(), "update.json"), Current: "v0.4.0", Release: true,
		Settings:     func() update.Settings { return update.SettingsFrom(nil) },
		Latest:       func(context.Context) (*update.Manifest, error) { fetches++; return &update.Manifest{}, nil },
		Egress:       f.hub.LogEgress,
		UpdateDenied: f.hub.UpdateDenied,
		Quiet:        func(time.Time) (bool, string) { return true, "" },
		Install:      func(context.Context) error { return nil },
	}
	a.Pass(t.Context())
	if fetches != 1 || f.egressLines("acme", update.DestManifest) != 1 || f.egressLines("beta", update.DestManifest) != 1 {
		t.Fatalf("%d fetches, records acme %d beta %d", fetches, f.egressLines("acme", update.DestManifest), f.egressLines("beta", update.DestManifest))
	}
	p := filepath.Join(f.vaultPath("beta"), ".para", "vault.properties")
	cur, _ := os.ReadFile(p)
	if err := os.WriteFile(p, append(cur, []byte("\negress.update=deny\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if id, err := f.hub.UpdateDenied(t.Context()); err != nil || id != "beta" {
		t.Fatalf("denied by %q %v", id, err)
	}
	a.StatePath = filepath.Join(t.TempDir(), "update2.json")
	if got := a.Pass(t.Context()); got != update.OutcomeDenied || fetches != 1 || f.egressLines("acme", update.DestManifest) != 1 {
		t.Fatalf("pass %s, %d fetches, %d records", got, fetches, f.egressLines("acme", update.DestManifest))
	}
}

// mcpReq is f.mcp with a method, host, and headers.
func (f *fixture) mcpReq(method, host, token, session, body string, extra map[string]string) int {
	f.t.Helper()
	h := map[string]string{"Accept": "application/json, text/event-stream"}
	if session != "" {
		h["Mcp-Session-Id"] = session
	}
	for k, v := range extra {
		h[k] = v
	}
	return f.do(call{method: method, target: "/mcp", host: host, token: token, body: body, header: h}).Code
}

func (f *fixture) mcpEntries() []*mcpEntry {
	f.srv.mcp.mu.Lock()
	defer f.srv.mcp.mu.Unlock()
	out := []*mcpEntry{}
	for _, e := range f.srv.mcp.by {
		out = append(out, e)
	}
	return out
}

func (s *mcpSessions) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.live)
}

// A key holds at most mcpSessionsPerKey sessions: a new one evicts the one
// used least recently, which then gets 404.
func TestMCPSessionsPerKeyAreCapped(t *testing.T) {
	f := newFixture(t)
	tok := f.key(f.keyA, "r", netguard.ScopeVaultRead)
	var sids []string
	for range mcpSessionsPerKey + 1 {
		code, _, sid := f.mcp(tok, "", mcpInit)
		if code != 200 || sid == "" {
			t.Fatalf("initialize: %d", code)
		}
		sids = append(sids, sid)
	}
	es := f.mcpEntries()
	if len(es) != 1 || es[0].ids.count() != mcpSessionsPerKey {
		t.Fatalf("live sessions: %d entries", len(es))
	}
	list := `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`
	if code, _, _ := f.mcp(tok, sids[0], list); code != 404 {
		t.Fatalf("the evicted session: %d", code)
	}
	if code, _, _ := f.mcp(tok, sids[len(sids)-1], list); code != 200 {
		t.Fatalf("the newest session: %d", code)
	}
}

// DELETE ends a session and its notes; the server of a revoked key, or of a
// key unused for the idle TTL, is dropped and its watcher stopped.
func TestMCPSessionAndServerCleanup(t *testing.T) {
	f := newFixture(t)
	tok := f.key(f.keyA, "r", netguard.ScopeVaultRead)
	_, _, sid := f.mcp(tok, "", mcpInit)
	ended := []string{}
	e := f.mcpEntries()[0]
	e.ids.end = func(id string) { ended = append(ended, id); e.srv.ForgetSession(id) }
	if code := f.mcpReq(http.MethodDelete, "", tok, sid, "", nil); code != 200 {
		t.Fatalf("delete: %d", code)
	}
	if e.ids.count() != 0 || len(ended) != 1 || ended[0] != sid {
		t.Fatalf("after delete: %d live, ended %v", e.ids.count(), ended)
	}
	if code, _, _ := f.mcp(tok, sid, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`); code != 404 {
		t.Fatalf("a deleted session: %d", code)
	}
	// Unused for the TTL: dropped.
	f.srv.mcp.sweep(time.Now().Add(time.Hour))
	if len(f.mcpEntries()) != 1 {
		t.Fatal("a server in use was dropped")
	}
	f.srv.mcp.sweep(time.Now().Add(mcpSessionIdle + time.Minute))
	if len(f.mcpEntries()) != 0 {
		t.Fatal("an unused server was kept")
	}
	// Revoked: dropped at the next sweep.
	f.mcp(tok, "", mcpInit)
	if _, err := f.keyA.Revoke("r"); err != nil {
		t.Fatal(err)
	}
	f.srv.mcp.sweep(time.Now())
	if len(f.mcpEntries()) != 0 {
		t.Fatal("the server of a revoked key was kept")
	}
}

// A GET stream ends when its session ends.
func TestMCPStreamEndsWithItsSession(t *testing.T) {
	f := newFixture(t)
	tok := f.key(f.keyA, "r", netguard.ScopeVaultRead)
	_, _, sid := f.mcp(tok, "", mcpInit)
	done := make(chan int, 1)
	go func() { done <- f.mcpReq(http.MethodGet, "", tok, sid, "", nil) }()
	e := f.mcpEntries()[0]
	open := false
	for range 100 {
		e.ids.mu.Lock()
		open = len(e.ids.streams[sid]) == 1
		e.ids.mu.Unlock()
		if open {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !open {
		t.Fatal("no stream recorded")
	}
	select {
	case code := <-done:
		t.Fatalf("the stream did not open: %d", code)
	case <-time.After(100 * time.Millisecond):
	}
	e.ids.Terminate(sid)
	select {
	case code := <-done:
		if code != 200 {
			t.Fatalf("stream: %d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the stream outlived its session")
	}
}

// The gate and the transport refuse what is not this key's: a wrong Host,
// a foreign Origin, a browser session, another vault's key, a person-only
// op, and another key's session on GET and DELETE.
func TestMCPOverHTTPRefusals(t *testing.T) {
	f := newFixture(t)
	tok := f.key(f.keyA, "a", allScopes...)
	if code := f.mcpReq(post, "evil.example:7700", tok, "", mcpInit, nil); code != 421 {
		t.Fatalf("wrong Host: %d", code)
	}
	if code := f.mcpReq(post, "", tok, "", mcpInit, map[string]string{"Origin": "http://evil.example"}); code != 403 {
		t.Fatalf("wrong Origin: %d", code)
	}
	login, ltok, err := f.keyA.CreateLogin(netguard.ScopeSets["web"], keys.LoginTTL)
	if err != nil || login.Name == "" {
		t.Fatal(err)
	}
	if code := f.mcpReq(post, "", "", "", mcpInit, f.cookieHeader(ltok)); code != 403 {
		t.Fatalf("browser session: %d", code)
	}
	btok := f.key(f.keyB, "b", allScopes...)
	if code := f.mcpReq(post, hostA, btok, "", mcpInit, nil); code != 401 && code != 403 {
		t.Fatalf("a beta key on acme's Host: %d", code)
	}
	_, _, sid := f.mcp(tok, "", mcpInit)
	f.mcp(tok, sid, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	_, body, _ := f.mcp(tok, sid, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"item_close","arguments":{"id":"task-sample"}}}`)
	if !strings.Contains(body, `"isError":true`) && !strings.Contains(body, `"error"`) {
		t.Fatalf("a person-only op ran over HTTP: %s", body)
	}
	other := f.key(f.keyA, "other", allScopes...)
	if code := f.mcpReq(http.MethodGet, "", other, sid, "", nil); code != 404 {
		t.Fatalf("GET with another key's session: %d", code)
	}
	if code := f.mcpReq(http.MethodDelete, "", other, sid, "", nil); code != 404 {
		t.Fatalf("DELETE with another key's session: %d", code)
	}
	if code, _, _ := f.mcp(tok, sid, `{"jsonrpc":"2.0","id":3,"method":"tools/list"}`); code != 200 {
		t.Fatalf("the session did not survive another key's DELETE: %d", code)
	}
}

// Why the last check failed can name URLs and paths: a key without the
// admin scope sees only its category.
func TestUpdateStatusCheckErrorIsACategoryForNonAdmins(t *testing.T) {
	f := newFixture(t)
	f.srv.opt.Update = func() UpdateStatus {
		return UpdateStatus{Running: "test", CheckError: "https://example.invalid/x: dial tcp: refused", CheckKind: "network"}
	}
	tok := f.key(f.keyA, "r", netguard.ScopeVaultRead)
	w := f.do(call{target: "/api/v1/update/status", token: tok})
	if b := w.Body.String(); w.Code != 200 || !strings.Contains(b, `"check_error": "network"`) || strings.Contains(b, "example.invalid") {
		t.Fatalf("a read key: %d %s", w.Code, b)
	}
	admin, err := f.keyA.AdminToken()
	if err != nil {
		t.Fatal(err)
	}
	w = f.do(call{target: "/api/v1/update/status", token: admin})
	if b := w.Body.String(); w.Code != 200 || !strings.Contains(b, "example.invalid") {
		t.Fatalf("admin: %d %s", w.Code, b)
	}
}

// settledGoroutines waits up to a second for the goroutine count to fall to
// at most n, and returns the count.
func settledGoroutines(n int) int {
	g := runtime.NumGoroutine()
	for i := 0; i < 50 && g > n; i++ {
		time.Sleep(20 * time.Millisecond)
		g = runtime.NumGoroutine()
	}
	return g
}

func heapInUse() uint64 {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapInuse
}

// New starts no goroutine, so a Server nobody closes (para schema dump, a
// test) is freed. A goroutine from New kept every built Server (huma and
// GraphQL schemas, ~20 MB) alive, and the server tests reached 8 GB.
func TestNewLeavesNothingRunning(t *testing.T) {
	before := settledGoroutines(0)
	s, err := New(Options{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	ptr := weak.Make(s)
	s = nil
	_ = s
	if n := settledGoroutines(before); n > before {
		t.Errorf("goroutines: %d before New, %d after", before, n)
	}
	for i := 0; i < 20 && ptr.Value() != nil; i++ {
		runtime.GC()
		time.Sleep(10 * time.Millisecond)
	}
	if ptr.Value() != nil {
		t.Error("a Server nobody holds was not freed")
	}
}

// Sessions that end (DELETE, eviction, a stream) leave no goroutine and no
// heap behind; Close stops what the key's server started.
func TestMCPSessionsDoNotAccumulate(t *testing.T) {
	f := newFixture(t)
	tok := f.key(f.keyA, "r", netguard.ScopeVaultRead)
	cycle := func(n int) {
		for i := range n {
			code, _, sid := f.mcp(tok, "", mcpInit)
			if code != 200 || sid == "" {
				t.Fatalf("initialize: %d", code)
			}
			f.mcp(tok, sid, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)
			f.mcp(tok, sid, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`)
			switch i % 3 {
			case 0: // ended by DELETE
				if code := f.mcpReq(http.MethodDelete, "", tok, sid, "", nil); code != 200 {
					t.Fatalf("delete: %d", code)
				}
			case 1: // a stream, ended with its session
				done := make(chan int, 1)
				go func() { done <- f.mcpReq(http.MethodGet, "", tok, sid, "", nil) }()
				e := f.mcpEntries()[0]
				for range 100 {
					e.ids.mu.Lock()
					open := len(e.ids.streams[sid]) > 0
					e.ids.mu.Unlock()
					if open {
						break
					}
					time.Sleep(5 * time.Millisecond)
				}
				e.ids.Terminate(sid)
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Fatal("the stream outlived its session")
				}
			default: // left open: evicted at the cap
			}
		}
	}
	base := settledGoroutines(0)
	// Warm up: the key's server, its watcher, and the first sessions.
	cycle(mcpSessionsPerKey * 3)
	warm := settledGoroutines(0)
	heap := heapInUse()
	cycle(200)
	if e := f.mcpEntries(); len(e) != 1 || e[0].ids.count() > mcpSessionsPerKey {
		t.Fatalf("entries %d, or sessions over the cap", len(e))
	}
	if g := settledGoroutines(warm); g > warm+2 {
		t.Errorf("goroutines: %d after warm-up, %d after 200 more sessions", warm, g)
	}
	if h := heapInUse(); h > heap+16<<20 {
		t.Errorf("heap in use: %d MB after warm-up, %d MB after 200 more sessions", heap>>20, h>>20)
	}
	f.srv.Close()
	if g := settledGoroutines(base); g > base {
		t.Errorf("goroutines: %d before MCP, %d after Close", base, g)
	}
}
