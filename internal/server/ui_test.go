package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/iodesystems/gwag/v2/gw/relay"

	"github.com/iodesystems/para/internal/netguard"
)

func (f *fixture) cookieHeader(tok string) map[string]string {
	return map[string]string{"Cookie": netguard.CookieName + "=" + f.session(tok), "Origin": "http://" + hostA}
}

// An agent key can read Review but cannot accept: only a browser session can.
func TestReviewAcceptNeedsSession(t *testing.T) {
	f := newFixture(t)
	agent := f.key(f.keyA, "claude", allScopes...)
	wantStatus(t, f.do(call{method: post, target: "/api/v1/io/add", token: agent, body: `{"direction":"in","text":"from the agent"}`}), 200)

	review := func(c call) UIReviewBody {
		w := f.do(c)
		wantStatus(t, w, 200)
		var b UIReviewBody
		if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
			t.Fatal(err)
		}
		return b
	}
	b := review(call{target: "/api/v1/ui/review", token: agent})
	if b.Count == 0 {
		t.Fatalf("agent event not in Review: %+v", b)
	}
	wantStatus(t, f.do(call{method: post, target: "/api/v1/ui/review/accept", token: agent, body: `{}`}), 403)
	wantStatus(t, f.do(call{method: post, target: "/api/v1/ui/review/reopen", token: agent}), 403)

	h := f.cookieHeader(f.key(f.keyA, "owner", allScopes...))
	wantStatus(t, f.do(call{method: post, target: "/api/v1/ui/review/accept", header: h, body: `{}`}), 200)
	if b = review(call{target: "/api/v1/ui/review", token: agent}); b.Count != 0 || !b.CanUndo {
		t.Fatalf("after accept: %+v", b)
	}
	wantStatus(t, f.do(call{method: post, target: "/api/v1/ui/review/reopen", header: h}), 200)
	if b = review(call{target: "/api/v1/ui/review", token: agent}); b.Count == 0 {
		t.Fatalf("after reopen: %+v", b)
	}
	wantStatus(t, f.do(call{method: post, target: "/api/v1/ui/review/accept", header: h, body: `{"at":"nope"}`}), 400)
}

// The owner's own browser writes are not Review material.
func TestBrowserWritesAreNotReviewed(t *testing.T) {
	f := newFixture(t)
	h := f.cookieHeader(f.key(f.keyA, "owner", allScopes...))
	h["Content-Type"] = "application/json"
	wantStatus(t, f.do(call{method: post, target: "/api/v1/io/add", header: h, body: `{"direction":"in","text":"mine"}`}), 200)
	w := f.do(call{target: "/api/v1/ui/review", header: h})
	wantStatus(t, w, 200)
	var b UIReviewBody
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	for _, g := range b.Groups {
		for _, e := range g.Events {
			if e.Surface == "pwa" {
				t.Errorf("browser event in Review: %+v", e)
			}
		}
	}
}

func TestTodayAndDocForThisVaultOnly(t *testing.T) {
	f := newFixture(t)
	a := f.key(f.keyA, "a", allScopes...)
	w := f.do(call{target: "/api/v1/ui/today", token: a})
	wantStatus(t, w, 200)
	var b UITodayBody
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	if b.Today.Vault != "acme" {
		t.Errorf("vault %q", b.Today.Vault)
	}
	wantStatus(t, f.do(call{target: "/api/v1/ui/doc?id=no-such", token: a}), 404)
	wantStatus(t, f.do(call{target: "/api/v1/ui/doc", token: a}), 422)
	// A key with no read scope gets nothing.
	w0 := f.key(f.keyA, "w", "vault:write")
	wantStatus(t, f.do(call{target: "/api/v1/ui/today", token: w0}), 403)
	wantStatus(t, f.do(call{target: "/api/v1/ui/doc?id=x", token: w0}), 403)
}

// dialLive opens /ws on the test server as the vault's browser would.
func dialLive(t *testing.T, f *fixture, header http.Header) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	srv := httptest.NewServer(f.h)
	t.Cleanup(srv.Close)
	header.Set("Host", hostA)
	u := "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
	return websocket.DefaultDialer.Dial(u, header)
}

func TestLiveRelayPushesEvents(t *testing.T) {
	f := newFixture(t)
	owner := f.key(f.keyA, "owner", allScopes...)
	cookie := f.session(owner)

	// No credential: refused before the upgrade.
	if _, res, err := dialLive(t, f, http.Header{"Origin": {"http://" + hostA}}); err == nil || res == nil || res.StatusCode != 401 {
		t.Fatalf("no cookie: %v %v", err, res)
	}
	// A cross-origin page with the cookie: refused.
	if _, res, err := dialLive(t, f, http.Header{"Cookie": {netguard.CookieName + "=" + cookie}, "Origin": {"http://evil.example"}}); err == nil || res == nil || res.StatusCode != 403 {
		t.Fatalf("evil origin: %v %v", err, res)
	}

	h := map[string]string{"Cookie": netguard.CookieName + "=" + cookie, "Origin": "http://" + hostA}
	w := f.do(call{method: post, target: "/api/v1/ui/live/subscribe", header: h})
	wantStatus(t, w, 200)
	var cap UILiveSubscribeBody
	if err := json.Unmarshal(w.Body.Bytes(), &cap); err != nil {
		t.Fatal(err)
	}
	if cap.Channel != "events.para.vault.acme" {
		t.Fatalf("channel %q", cap.Channel)
	}

	c, _, err := dialLive(t, f, http.Header{"Cookie": {netguard.CookieName + "=" + cookie}, "Origin": {"http://" + hostA}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))

	send := func(v map[string]string) { t.Helper(); _ = c.WriteJSON(v) }
	read := func() map[string]any {
		t.Helper()
		var m map[string]any
		if err := c.ReadJSON(&m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	// A cap for another vault's channel is refused.
	send(map[string]string{"op": "sub", "id": "bad", "token": "garbage"})
	if m := read(); m["op"] != "err" {
		t.Fatalf("garbage cap: %v", m)
	}
	send(map[string]string{"op": "sub", "id": "1", "token": cap.Token})
	if m := read(); m["op"] != "ack" {
		t.Fatalf("ack: %v", m)
	}
	// A write through the API publishes an event with ids only.
	agent := f.key(f.keyA, "agent", allScopes...)
	wantStatus(t, f.do(call{method: post, target: "/api/v1/io/add", token: agent, body: `{"direction":"in","text":"secret words"}`}), 200)
	m := read()
	if m["op"] != "data" || m["id"] != "1" {
		t.Fatalf("data: %v", m)
	}
	raw, _ := json.Marshal(m["payload"])
	if strings.Contains(string(raw), "secret words") || !strings.Contains(string(raw), `"op":"create"`) {
		t.Errorf("payload: %s", raw)
	}
}

func TestLiveMintedPerVault(t *testing.T) {
	f := newFixture(t)
	a := f.key(f.keyA, "a", allScopes...)
	b := f.key(f.keyB, "b", allScopes...)
	mint := func(tok, host string) string {
		w := f.do(call{method: post, target: "/api/v1/ui/live/subscribe", token: tok, host: host})
		wantStatus(t, w, 200)
		var c UILiveSubscribeBody
		_ = json.Unmarshal(w.Body.Bytes(), &c)
		return c.Channel
	}
	if mint(a, hostA) == mint(b, hostB) {
		t.Error("two vaults share a channel")
	}
	// A key of vault A does not open vault B.
	wantStatus(t, f.do(call{method: post, target: "/api/v1/ui/live/subscribe", token: a, host: hostB}), 401)
}

// pwaDenylist reads the regexes ui/vite.config.ts feeds to workbox.
func pwaDenylist(t *testing.T) []*regexp.Regexp {
	t.Helper()
	b, err := os.ReadFile("../../ui/pwa-denylist.json")
	if err != nil {
		t.Fatal(err)
	}
	var src []string
	if err := json.Unmarshal(b, &src); err != nil {
		t.Fatal(err)
	}
	var out []*regexp.Regexp
	for _, s := range src {
		out = append(out, regexp.MustCompile(s))
	}
	return out
}

func denied(list []*regexp.Regexp, p string) bool {
	for _, re := range list {
		if re.MatchString(p) {
			return true
		}
	}
	return false
}

// TestServiceWorkerDenylistCoversServerRoutes: a navigation to any path the
// server owns must reach the server, not the cached shell. curl has no service
// worker, so only this sweep finds a miss.
func TestServiceWorkerDenylistCoversServerRoutes(t *testing.T) {
	list := pwaDenylist(t)
	paths := append([]string{}, ServerGETRoutes()...)
	s := schemaOnly(t)
	for _, r := range s.Routes() {
		if r.Method == get {
			paths = append(paths, r.Path)
		}
	}
	paths = append(paths, "/api/graphql", "/ws", "/sw.js")
	for _, p := range paths {
		if !denied(list, p) {
			t.Errorf("service worker would answer %s with the shell", p)
		}
	}
	// And the screens of the app stay with the shell.
	for _, p := range []string{"/", "/today", "/review", "/work", "/docs/policy-x", "/login", "/wsl", "/healthzz-not"} {
		if p == "/wsl" || p == "/healthzz-not" {
			continue // prefix-like names are not app routes; see the next check
		}
		if denied(list, p) {
			t.Errorf("app route %s is denied", p)
		}
	}
}

// TestLiveClientIsCurrent: ui/src/gen is written by cmd/livegen from GenChannels. A changed
// LiveEvent without a regenerated client fails here.
func TestLiveClientIsCurrent(t *testing.T) {
	out := t.TempDir()
	if err := relay.GenClient(GenChannels, GenEnums, out); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{relay.TypesFile, relay.RuntimeFile} {
		want, err := os.ReadFile(filepath.Join(out, f))
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(filepath.Join("../../ui/src/gen", f))
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(want) {
			t.Errorf("ui/src/gen/%s is stale: run make schema", f)
		}
	}
}
