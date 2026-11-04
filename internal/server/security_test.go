package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/keys"
	"github.com/iodesystems/para/internal/netguard"
)

// table is every huma route plus the GraphQL and schema endpoints.
func table(f *fixture) []Route {
	out := append([]Route{}, f.srv.Routes()...)
	return append(out,
		Route{ID: "graphql", Method: post, Path: "/api/graphql", Scope: netguard.ScopeAnyKey},
		Route{ID: "graphql-get", Method: get, Path: "/api/graphql", Scope: netguard.ScopeAnyKey},
		Route{ID: "schema-sdl", Method: get, Path: "/api/schema/graphql", Scope: netguard.ScopeAnyKey},
		Route{ID: "schema-post", Method: post, Path: "/api/schema/graphql", Scope: netguard.ScopeAnyKey},
		Route{ID: "schema-proto", Method: get, Path: "/api/schema/proto", Scope: netguard.ScopeAnyKey},
		Route{ID: "schema-openapi", Method: get, Path: "/api/schema/openapi", Scope: netguard.ScopeAnyKey},
	)
}

// gqlEvery names every GraphQL op field with its scope and a minimal document.
func gqlEvery(t *testing.T, f *fixture, types map[string]gqlType) []struct {
	Route Route
	Doc   string
	Vars  map[string]any
} {
	t.Helper()
	fields := fieldsOf(types)
	var out []struct {
		Route Route
		Doc   string
		Vars  map[string]any
	}
	for _, r := range f.srv.Routes() {
		fd, ok := fields[r.ID]
		if !ok {
			t.Fatalf("no field %s", r.ID)
		}
		input := "{}"
		if r.Op != "" {
			input = sample(r.Op)
		}
		if r.ID == "adminKeyCreate" {
			input = `{"name":"x","scopes":["vault:read"]}`
		}
		if r.ID == "adminKeyRevoke" {
			input = `{"name":"x"}`
		}
		doc, vars := gqlDoc(types, fd.Kind, fd.Field, input, t)
		out = append(out, struct {
			Route Route
			Doc   string
			Vars  map[string]any
		}{r, doc, vars})
	}
	return out
}

func TestNoTokenGives401(t *testing.T) {
	f := newFixture(t)
	for _, r := range table(f) {
		w := f.do(call{method: r.Method, target: r.Path})
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s: %d", r.Method, r.Path, w.Code)
		}
	}
	// Every GraphQL field: the endpoint is shut before any field runs.
	types := f.gqlTypes(f.key(f.keyA, "t", allScopes...))
	for _, g := range gqlEvery(t, f, types) {
		w, _ := f.graphql("", g.Doc, g.Vars)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("graphql %s: %d %s", g.Route.ID, w.Code, w.Body)
		}
	}
}

// TestPositiveControl proves the denials above are not vacuous.
func TestPositiveControl(t *testing.T) {
	f := newFixture(t)
	tok := f.key(f.keyA, "all", allScopes...)
	w := f.do(call{target: "/api/v1/task/list", token: tok})
	wantStatus(t, w, 200)
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Error("API responses must not be cached")
	}
}

func TestKeyFromVaultAOnVaultBDenied(t *testing.T) {
	f := newFixture(t)
	tok := f.key(f.keyA, "a", allScopes...)
	f.key(f.keyB, "b", allScopes...)
	for _, r := range table(f) {
		if w := f.do(call{method: r.Method, target: r.Path, token: tok, host: hostB}); w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s on vault b: %d", r.Method, r.Path, w.Code)
		}
	}
	// A key with the same name in vault b does not help: the secret differs.
	f.key(f.keyB, "a", allScopes...)
	wantStatus(t, f.do(call{target: "/api/v1/task/list", token: tok, host: hostB}), 401)
	// Nor does a session cookie from vault a.
	cookie := f.session(tok)
	wantStatus(t, f.do(call{target: "/api/v1/task/list", host: hostB, header: map[string]string{"Cookie": netguard.CookieName + "=" + cookie}}), 401)
}

func TestWrongScopeDeniedOnBothSurfaces(t *testing.T) {
	f := newFixture(t)
	tok := f.key(f.keyA, "ro", netguard.ScopeVaultRead)
	types := f.gqlTypes(tok)
	for _, r := range f.srv.Routes() {
		if r.Scope == netguard.ScopeVaultRead || r.Scope == netguard.ScopeAnyKey {
			continue
		}
		c := call{method: r.Method, target: r.Path, token: tok}
		if w := f.do(c); w.Code != http.StatusForbidden {
			t.Errorf("REST %s: %d", r.Path, w.Code)
		}
	}
	for _, g := range gqlEvery(t, f, types) {
		if g.Route.Scope == netguard.ScopeVaultRead || g.Route.Scope == netguard.ScopeAnyKey {
			continue
		}
		_, res := f.graphql(tok, g.Doc, g.Vars)
		errs := fmt.Sprint(res["errors"])
		if !strings.Contains(errs, "lacks scope") {
			t.Errorf("graphql %s: %s", g.Route.ID, errs)
		}
	}
}

func TestForgedForwardedForIgnoredFromNonProxy(t *testing.T) {
	f := newFixture(t)
	f.rules("acme", "allow / 198.51.100.7\n")
	tok := f.key(f.keyA, "k", allScopes...)
	spoof := map[string]string{"X-Forwarded-For": "198.51.100.7"}
	// A direct peer that is not a proxy claims an allowed address.
	wantStatus(t, f.do(call{target: "/api/v1/task/list", token: tok, peer: "203.0.113.9:1", header: spoof}), 403)
	// Loopback is not a proxy either.
	wantStatus(t, f.do(call{target: "/api/v1/task/list", token: tok, header: spoof}), 403)
	// The trusted proxy says so: allowed.
	wantStatus(t, f.do(call{target: "/api/v1/task/list", token: tok, peer: proxyPeer, header: spoof}), 200)
	// Through the proxy a client prepends a forgery; the rightmost entry rules.
	wantStatus(t, f.do(call{target: "/api/v1/task/list", token: tok, peer: proxyPeer,
		header: map[string]string{"X-Forwarded-For": "198.51.100.7, 203.0.113.9"}}), 403)
	// A trusted peer with no header is an unknown client.
	wantStatus(t, f.do(call{target: "/api/v1/task/list", token: tok, peer: proxyPeer}), 403)
}

func TestRebindingHostGets421(t *testing.T) {
	f := newFixture(t)
	tok := f.key(f.keyA, "k", allScopes...)
	var first string
	for _, host := range []string{"evil.example", "127.0.0.1:7700", "localhost:7700", "nope.localhost:7700", "acme.evil.example", "a.b.localhost", "acme.localhost.evil.example", ""} {
		w := f.do(call{target: "/healthz", host: host + "", token: tok})
		if host == "" {
			continue
		}
		wantStatus(t, w, http.StatusMisdirectedRequest)
		if first == "" {
			first = w.Body.String()
		}
		if w.Body.String() != first {
			t.Errorf("host %q got another body: %s", host, w.Body)
		}
	}
	// X-Forwarded-Host is ignored, even from the trusted proxy.
	wantStatus(t, f.do(call{target: "/healthz", host: "evil.example", peer: proxyPeer,
		header: map[string]string{"X-Forwarded-Host": hostA, "X-Forwarded-For": "127.0.0.1"}}), http.StatusMisdirectedRequest)
	// Every route, not only /healthz.
	for _, r := range table(f) {
		wantStatus(t, f.do(call{method: r.Method, target: r.Path, host: "evil.example", token: tok}), http.StatusMisdirectedRequest)
	}
}

func TestCrossOriginDenied(t *testing.T) {
	f := newFixture(t)
	tok := f.key(f.keyA, "k", allScopes...)
	cookie := netguard.CookieName + "=" + f.session(tok)
	evil := map[string]string{"Origin": "http://evil.example"}
	for _, r := range table(f) {
		if r.Method != post {
			continue
		}
		if w := f.do(call{method: post, target: r.Path, token: tok, header: evil}); w.Code != http.StatusForbidden {
			t.Errorf("POST %s with bearer: %d", r.Path, w.Code)
		}
		if w := f.do(call{method: post, target: r.Path, header: map[string]string{"Cookie": cookie, "Origin": "http://evil.example"}}); w.Code != http.StatusForbidden {
			t.Errorf("POST %s with cookie: %d", r.Path, w.Code)
		}
	}
	for _, o := range []string{"null", "http://acme.localhost:7700.evil.example", "https://acme.localhost:7700", "http://acme.localhost:7700/x"} {
		wantStatus(t, f.do(call{method: post, target: "/api/v1/reconcile", token: tok, header: map[string]string{"Origin": o}}), 403)
	}
	// A cookie with no Origin and no fetch metadata is not a browser's request.
	wantStatus(t, f.do(call{method: post, target: "/api/v1/reconcile", header: map[string]string{"Cookie": cookie}}), 403)
	// WebSocket upgrade from another origin.
	wantStatus(t, f.do(call{target: "/api/graphql", token: tok, header: map[string]string{
		"Connection": "Upgrade", "Upgrade": "websocket", "Origin": "http://evil.example"}}), 403)
	// The same origin passes.
	wantStatus(t, f.do(call{method: post, target: "/api/v1/reconcile", header: map[string]string{"Cookie": cookie, "Origin": "http://" + hostA}}), 200)
	// A preflight gets no CORS grant.
	w := f.do(call{method: http.MethodOptions, target: "/api/v1/reconcile", header: map[string]string{"Origin": "http://evil.example", "Access-Control-Request-Method": "POST"}})
	if w.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Error("CORS header present")
	}
}

func TestPathTricksCannotBypassARule(t *testing.T) {
	f := newFixture(t)
	f.rules("acme", "allow / 127.0.0.1\ndeny /api/v1/admin\n")
	admin, err := f.keyA.AdminToken()
	if err != nil {
		t.Fatal(err)
	}
	wantStatus(t, f.do(call{target: "/api/v1/admin/key/list", token: admin}), 403)
	for _, p := range []string{
		"/api/v1/x/../admin/key/list",
		"/api/v1/./admin/key/list",
		"/api/v1//admin/key/list",
		"//api/v1/admin/key/list",
		"/api/v1/%61dmin/key/list", // decoded: must meet the rule, not miss it
		"/api/v1%2fadmin/key/list",
		"/api/v1%2Fadmin%2Fkey%2Flist",
		"/api/v1/%2e%2e/v1/admin/key/list",
		"/api/v1/x%2f..%2fadmin/key/list",
		"/api/v1/x/%252e%252e/admin/key/list",
		"/api/v1/admin/key/list/",
		`/api/v1\admin/key/list`,
	} {
		r := call{target: strings.ReplaceAll(p, `\`, "%5c"), token: admin}
		if w := f.do(r); w.Code == 200 {
			t.Errorf("%s reached the handler: %s", p, w.Body)
		}
	}
	// A rule matches whole segments.
	f.rules("acme", "allow /api 127.0.0.1\n")
	wantStatus(t, f.do(call{target: "/apiother", token: admin}), 403)
}

func TestMappedAddresses(t *testing.T) {
	f := newFixture(t)
	f.rules("acme", "allow / 192.0.2.0/24\n")
	tok := f.key(f.keyA, "k", allScopes...)
	wantStatus(t, f.do(call{target: "/api/v1/task/list", token: tok, peer: "[::ffff:192.0.2.5]:1"}), 200)
	wantStatus(t, f.do(call{target: "/api/v1/task/list", token: tok, peer: "[::ffff:192.0.3.5]:1"}), 403)
	// A mapped trusted proxy is the same proxy.
	wantStatus(t, f.do(call{target: "/api/v1/task/list", token: tok, peer: "[::ffff:" + proxyIP + "]:1",
		header: map[string]string{"X-Forwarded-For": "::ffff:192.0.2.9"}}), 200)
	// A rule written in mapped form matches a plain client.
	f.rules("acme", "allow / ::ffff:192.0.2.0/120\n")
	wantStatus(t, f.do(call{target: "/api/v1/task/list", token: tok, peer: "192.0.2.5:1"}), 200)
	// Mapped loopback is not local unless it is loopback.
	admin, _ := f.keyA.AdminToken()
	f.rules("acme", "allow / any\n")
	wantStatus(t, f.do(call{target: "/api/v1/admin/key/list", token: admin, peer: "[::ffff:127.0.0.1]:1"}), 200)
	wantStatus(t, f.do(call{target: "/api/v1/admin/key/list", token: admin, peer: "[::ffff:192.0.2.5]:1"}), 403)
}

func TestPinnedExpiredRevokedKeys(t *testing.T) {
	f := newFixture(t)
	f.rules("acme", "allow / any\n")
	_, pinned, err := f.keyA.Create("pinned", allScopes, []string{"192.0.2.5", "198.51.100.0/24"}, 0)
	if err != nil {
		t.Fatal(err)
	}
	wantStatus(t, f.do(call{target: "/api/v1/task/list", token: pinned, peer: "192.0.2.5:1"}), 200)
	wantStatus(t, f.do(call{target: "/api/v1/task/list", token: pinned, peer: "198.51.100.77:1"}), 200)
	wantStatus(t, f.do(call{target: "/api/v1/task/list", token: pinned, peer: "192.0.2.6:1"}), 403)

	// A session from a pinned key stays pinned.
	s := f.do(call{method: post, target: "/auth/session", token: pinned, peer: "192.0.2.5:1"})
	wantStatus(t, s, 200)
	wantStatus(t, f.do(call{target: "/api/v1/task/list", peer: "192.0.2.6:1", header: map[string]string{"Cookie": s.Result().Cookies()[0].String()}}), 403)

	now := time.Now()
	f.keyA.Now = func() time.Time { return now }
	_, short, err := f.keyA.Create("short", allScopes, nil, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	cookie := netguard.CookieName + "=" + f.session(short)
	wantStatus(t, f.do(call{target: "/api/v1/task/list", token: short}), 200)
	tn, _ := f.hub.Lookup("acme")
	tn.(*Tenant).keys.Now = func() time.Time { return now.Add(2 * time.Hour) }
	wantStatus(t, f.do(call{target: "/api/v1/task/list", token: short}), 401)
	wantStatus(t, f.do(call{target: "/api/v1/task/list", header: map[string]string{"Cookie": cookie}}), 401)
	tn.(*Tenant).keys.Now = time.Now

	rev := f.key(f.keyA, "rev", allScopes...)
	cookie = netguard.CookieName + "=" + f.session(rev)
	wantStatus(t, f.do(call{target: "/api/v1/task/list", token: rev}), 200)
	// Revoked by another process's Store: the file is the state.
	if _, err := keys.New(f.keyA.Dir()).Revoke("rev"); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, f.do(call{target: "/api/v1/task/list", token: rev}), 401)
	wantStatus(t, f.do(call{target: "/api/v1/task/list", header: map[string]string{"Cookie": cookie}}), 401)
}

func TestAdminRoutesLocalOnly(t *testing.T) {
	f := newFixture(t)
	f.rules("acme", "allow / any\n")
	admin, err := f.keyA.AdminToken()
	if err != nil {
		t.Fatal(err)
	}
	tok := f.key(f.keyA, "full", allScopes...)
	var routes []Route
	for _, r := range f.srv.Routes() {
		if r.Scope == netguard.ScopeAdmin {
			routes = append(routes, r)
		}
	}
	if len(routes) != 3 {
		t.Fatalf("admin routes: %v", routes)
	}
	for _, r := range routes {
		// From elsewhere, even with the admin token.
		if w := f.do(call{method: r.Method, target: r.Path, token: admin, peer: "203.0.113.9:1"}); w.Code != 403 {
			t.Errorf("%s remote: %d", r.Path, w.Code)
		}
		// Through a proxy on this machine: the listed proxy.
		if w := f.do(call{method: r.Method, target: r.Path, token: admin, peer: proxyPeer, header: map[string]string{"X-Forwarded-For": "127.0.0.1"}}); w.Code != 403 {
			t.Errorf("%s via proxy: %d", r.Path, w.Code)
		}
		// Through an unlisted proxy on loopback.
		if w := f.do(call{method: r.Method, target: r.Path, token: admin, header: map[string]string{"X-Forwarded-For": "203.0.113.9"}}); w.Code != 403 {
			t.Errorf("%s via unlisted proxy: %d", r.Path, w.Code)
		}
		// A key with every scope is not admin.
		if w := f.do(call{method: r.Method, target: r.Path, token: tok}); w.Code != 403 {
			t.Errorf("%s with a key: %d", r.Path, w.Code)
		}
	}
	// An admin cookie obtained locally does not work from elsewhere.
	cookie := netguard.CookieName + "=" + f.session(admin)
	wantStatus(t, f.do(call{target: "/api/v1/admin/key/list", header: map[string]string{"Cookie": cookie}}), 200)
	wantStatus(t, f.do(call{target: "/api/v1/admin/key/list", peer: "203.0.113.9:1", header: map[string]string{"Cookie": cookie}}), 403)
	// Local: create, use, revoke.
	body := `{"name":"made","scopes":["vault:read"],"ttl_seconds":3600}`
	w := f.do(call{method: post, target: "/api/v1/admin/key/create", token: admin, body: body})
	wantStatus(t, w, 200)
	var made KeyCreateBody
	if err := json.Unmarshal(w.Body.Bytes(), &made); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, f.do(call{target: "/api/v1/task/list", token: made.Token}), 200)
	if strings.Contains(w.Body.String(), `"hash"`) {
		t.Error("a hash left the server")
	}
	wantStatus(t, f.do(call{method: post, target: "/api/v1/admin/key/revoke", token: admin, body: `{"name":"made"}`}), 200)
	wantStatus(t, f.do(call{target: "/api/v1/task/list", token: made.Token}), 401)
}

func TestAdminTokenFileMode(t *testing.T) {
	f := newFixture(t)
	tok, err := f.keyA.AdminToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := chmod(f.keyA.AdminPath(), 0o644); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, f.do(call{target: "/api/v1/task/list", token: tok}), 401)
}

func TestIPRuleDeniesBeforeToken(t *testing.T) {
	f := newFixture(t)
	tok := f.key(f.keyA, "k", allScopes...)
	// Default rules: loopback only. A valid token from elsewhere is refused.
	wantStatus(t, f.do(call{target: "/api/v1/task/list", token: tok, peer: "203.0.113.9:1"}), 403)
	// The public shell is refused too.
	wantStatus(t, f.do(call{target: "/", peer: "203.0.113.9:1"}), 403)
	// Unreadable rules deny.
	if err := removeFile(filepath.Join(f.keyA.Dir(), "net.rules")); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, f.do(call{target: "/api/v1/task/list", token: tok}), 403)
}

func TestPublicSurfaceHoldsNoData(t *testing.T) {
	f := newFixture(t)
	w := f.do(call{target: "/healthz"})
	wantStatus(t, w, 200)
	if strings.TrimSpace(w.Body.String()) != `{"status":"ok"}` {
		t.Errorf("healthz: %s", w.Body)
	}
	for _, h := range []string{"Content-Security-Policy", "X-Content-Type-Options", "X-Frame-Options"} {
		if w.Header().Get(h) == "" {
			t.Errorf("no %s", h)
		}
	}
	if !strings.Contains(w.Header().Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Error("CSP lacks frame-ancestors")
	}
	wantStatus(t, f.do(call{target: "/some/client/route"}), 200)
	// A write to a UI path needs a token.
	wantStatus(t, f.do(call{method: post, target: "/some/client/route"}), 401)
	// huma's docs and spec are gone, and gat's pubsub stream is shut.
	tok := f.key(f.keyA, "k", allScopes...)
	for _, p := range []string{"/docs", "/openapi.json", "/schemas/x"} {
		if strings.Contains(f.do(call{target: p}).Body.String(), "openapi") {
			t.Errorf("%s served a spec", p)
		}
	}
	wantStatus(t, f.do(call{target: "/api/_gat/subscribe", token: tok}), 404)
	// Unmatched API paths are JSON 404s, never the shell.
	for _, p := range []string{"/api/nope", "/api/v1/nope", "/auth/nope", "/mcp"} {
		w := f.do(call{target: p, token: tok})
		wantStatus(t, w, 404)
		if !strings.Contains(w.Header().Get("Content-Type"), "json") {
			t.Errorf("%s: %s", p, w.Header().Get("Content-Type"))
		}
	}
}

func TestSessionCookie(t *testing.T) {
	f := newFixture(t)
	tok := f.key(f.keyA, "web", netguard.ScopeVaultRead)
	w := f.do(call{method: post, target: "/auth/session", token: tok})
	wantStatus(t, w, 200)
	c := w.Result().Cookies()[0]
	if !c.HttpOnly || c.SameSite != http.SameSiteStrictMode || c.Secure || c.Domain != "" || c.MaxAge <= 0 {
		t.Errorf("cookie: %+v", c)
	}
	if strings.Contains(c.Value, tok) {
		t.Error("the cookie holds the key")
	}
	// Secure when the browser used https, as the trusted proxy says.
	f.rules("acme", "allow / any\n")
	w = f.do(call{method: post, target: "/auth/session", token: tok, peer: proxyPeer,
		header: map[string]string{"X-Forwarded-For": "192.0.2.1", "X-Forwarded-Proto": "https"}})
	wantStatus(t, w, 200)
	if !w.Result().Cookies()[0].Secure {
		t.Error("no Secure over https")
	}
	// X-Forwarded-Proto from anyone else is ignored.
	w = f.do(call{method: post, target: "/auth/session", token: tok, header: map[string]string{"X-Forwarded-Proto": "https"}})
	if w.Result().Cookies()[0].Secure {
		t.Error("Secure from a forged header")
	}
	// The cookie works, says surface pwa, and logout ends it.
	cookie := netguard.CookieName + "=" + f.session(tok)
	me := f.do(call{target: "/auth/session", header: map[string]string{"Cookie": cookie}})
	wantStatus(t, me, 200)
	var v SessionView
	if err := json.Unmarshal(me.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	if v.Key != "web" || v.Vault != "acme" || v.Surface != surfacePWA || len(v.Scopes) != 1 {
		t.Errorf("%+v", v)
	}
	out := f.do(call{method: post, target: "/auth/session/delete", header: map[string]string{"Cookie": cookie, "Origin": "http://" + hostA}})
	wantStatus(t, out, 200)
	if c := out.Result().Cookies()[0]; c.MaxAge >= 0 || c.Value != "" {
		t.Errorf("clear cookie: %+v", c)
	}
	wantStatus(t, f.do(call{target: "/auth/session", header: map[string]string{"Cookie": cookie}}), 401)
}

// TestSetCookieOverGraphQL: a mutation's Set-Cookie must reach the real
// response. The cookie sink is a separate path from REST.
func TestSetCookieOverGraphQL(t *testing.T) {
	f := newFixture(t)
	tok := f.key(f.keyA, "web", netguard.ScopeVaultRead)
	w, res := f.graphql(tok, `mutation { para { v1 { authSessionCreate { key vault surface expires } } } }`, nil)
	wantStatus(t, w, 200)
	if res["errors"] != nil {
		t.Fatal(res["errors"])
	}
	cs := w.Result().Cookies()
	if len(cs) != 1 || cs[0].Name != netguard.CookieName || !cs[0].HttpOnly || cs[0].SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookies: %+v", cs)
	}
	wantStatus(t, f.do(call{target: "/api/v1/task/list", header: map[string]string{"Cookie": cs[0].Name + "=" + cs[0].Value}}), 200)
}

// TestActorAndSurfaceInEvents: events record the key name, per request, on one
// open vault.
func TestActorAndSurfaceInEvents(t *testing.T) {
	f := newFixture(t)
	one := f.key(f.keyA, "one", allScopes...)
	two := f.key(f.keyA, "two", allScopes...)
	wantStatus(t, f.do(call{method: post, target: "/api/v1/inbox/add", token: one, body: `{"text":"from one"}`}), 200)
	wantStatus(t, f.do(call{method: post, target: "/api/v1/inbox/add", token: two, body: `{"text":"from two"}`}), 200)
	w := f.do(call{target: "/api/v1/events/tail?limit=6", token: one})
	wantStatus(t, w, 200)
	var out struct {
		Events []struct {
			Actor   string `json:"actor"`
			Surface string `json:"surface"`
			Doc     string `json:"doc"`
			Op      string `json:"op"`
		} `json:"events"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	out.Events = slices.DeleteFunc(out.Events, func(e struct {
		Actor   string `json:"actor"`
		Surface string `json:"surface"`
		Doc     string `json:"doc"`
		Op      string `json:"op"`
	}) bool {
		return e.Op == "receipt" || strings.HasPrefix(e.Doc, "receipt-")
	})
	if len(out.Events) != 2 || out.Events[0].Actor != "one" || out.Events[1].Actor != "two" || out.Events[0].Surface != surfaceAPI {
		t.Errorf("%+v", out.Events)
	}
}

// TestConcurrentWritersShareOneVault: every request gets its own actor on one
// open vault, and the file lock keeps their writes apart.
func TestConcurrentWritersShareOneVault(t *testing.T) {
	f := newFixture(t)
	const n = 8
	toks := make([]string, n)
	for i := range toks {
		toks[i] = f.key(f.keyA, fmt.Sprintf("w%d", i), allScopes...)
	}
	var wg sync.WaitGroup
	for _, tok := range toks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if w := f.do(call{method: post, target: "/api/v1/inbox/add", token: tok, body: `{"text":"parallel"}`}); w.Code != 200 {
				t.Errorf("%d %s", w.Code, w.Body)
			}
		}()
	}
	wg.Wait()
	w := f.do(call{target: fmt.Sprintf("/api/v1/events/tail?limit=%d", 3*n), token: toks[0]})
	seen := map[string]bool{}
	var out struct {
		Events []struct {
			Actor string `json:"actor"`
		} `json:"events"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	for _, e := range out.Events {
		seen[e.Actor] = true
	}
	if len(seen) != n {
		t.Errorf("actors: %v", seen)
	}
}

// TestRequestSurface: a bearer key is surface api, whose request is only
// proposed. A person's browser session is surface pwa and queues it.
func TestRequestSurface(t *testing.T) {
	f := newFixture(t)
	tok := f.key(f.keyA, "k", netguard.ScopeRequestQueue)
	w := f.do(call{method: post, target: "/api/v1/request/add", token: tok, body: `{"title":"Summarize","ask":"Summarize"}`})
	wantStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"status": "proposed"`) {
		t.Errorf("bearer: %s", w.Body)
	}
	cookie := netguard.CookieName + "=" + f.session(tok)
	w = f.do(call{method: post, target: "/api/v1/request/add", body: `{"title":"Summarize","ask":"Summarize"}`,
		header: map[string]string{"Cookie": cookie, "Origin": "http://" + hostA}})
	wantStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"status": "queued"`) {
		t.Errorf("session: %s", w.Body)
	}
}

func TestErrorsHidePathsAndMapStatus(t *testing.T) {
	f := newFixture(t)
	tok := f.key(f.keyA, "k", allScopes...)
	w := f.do(call{target: "/api/v1/task/show?id=task-missing", token: tok})
	wantStatus(t, w, 404)
	if strings.Contains(w.Body.String(), f.home) || strings.Contains(w.Body.String(), "/tmp") {
		t.Errorf("path in error: %s", w.Body)
	}
	wantStatus(t, f.do(call{target: "/api/v1/task/show", token: tok}), 422)
	wantStatus(t, f.do(call{method: post, target: "/api/v1/inbox/add", token: tok, body: `{"text":"x","nope":1}`}), 422)
}

func TestVaultAddedWhileRunning(t *testing.T) {
	f := newFixture(t)
	wantStatus(t, f.do(call{target: "/healthz", host: "gamma.localhost:7700"}), http.StatusMisdirectedRequest)
	// The removed vault is gone at the next request.
	wantStatus(t, f.do(call{target: "/healthz", host: hostB}), 200)
	if err := removeFile(filepath.Join(f.home, "vaults", "beta", "vault.ref")); err != nil {
		t.Fatal(err)
	}
	wantStatus(t, f.do(call{target: "/healthz", host: hostB}), http.StatusMisdirectedRequest)
}

func TestServerGETRoutesAreOwned(t *testing.T) {
	for _, p := range ServerGETRoutes() {
		if !isAPIPath(strings.TrimSuffix(p, "/")) {
			t.Errorf("%s is not a server path", p)
		}
	}
}

// A peer on one of this machine's own addresses is local: the default rules
// (loopback only) let it in, and it may use the admin token. A header or a
// remote peer cannot borrow that.
func TestOwnAddressCountsAsLocal(t *testing.T) {
	f := newFixture(t)
	admin, err := f.keyA.AdminToken()
	if err != nil {
		t.Fatal(err)
	}
	tok := f.key(f.keyA, "k", allScopes...)
	list := "/api/v1/task/list"
	adminList := "/api/v1/admin/key/list"
	wantStatus(t, f.do(call{target: list, token: tok, peer: ownPeer}), 200)
	wantStatus(t, f.do(call{target: adminList, token: admin, peer: ownPeer}), 200)
	// A remote peer is refused whatever it claims.
	for _, claim := range []string{"127.0.0.1", ownIP, "::1"} {
		hdr := map[string]string{"X-Forwarded-For": claim}
		wantStatus(t, f.do(call{target: list, token: tok, peer: "203.0.113.9:1", header: hdr}), 403)
		wantStatus(t, f.do(call{target: adminList, token: admin, peer: "203.0.113.9:1", header: hdr}), 403)
	}
	wantStatus(t, f.do(call{target: adminList, token: admin, peer: "203.0.113.9:1"}), 403)
	// An own-address peer that forwards for someone is not local.
	hdr := map[string]string{"X-Forwarded-For": "203.0.113.9"}
	wantStatus(t, f.do(call{target: list, token: tok, peer: ownPeer, header: hdr}), 403)
	wantStatus(t, f.do(call{target: adminList, token: admin, peer: ownPeer, header: hdr}), 403)
	// The trusted proxy forwarding a claim of an own address is not local.
	wantStatus(t, f.do(call{target: adminList, token: admin, peer: proxyPeer, header: map[string]string{"X-Forwarded-For": ownIP}}), 403)
	// A rules file that names no loopback does not admit the own address.
	f.rules("acme", "allow / 198.51.100.7\n")
	wantStatus(t, f.do(call{target: list, token: tok, peer: ownPeer}), 403)
}
