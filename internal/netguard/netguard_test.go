package netguard

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
)

func req(remote string, hdr map[string]string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = remote
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	return r
}

func TestProxiesAreExactAddresses(t *testing.T) {
	if _, err := ParseProxies([]string{"10.0.0.0/8"}); err == nil {
		t.Error("a range must be refused")
	}
	p, err := ParseProxies([]string{"::ffff:10.0.0.1", " ", "2001:db8::1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p[netip.MustParseAddr("10.0.0.1")]; !ok {
		t.Errorf("mapped proxy not unmapped: %v", p)
	}
}

func TestResolve(t *testing.T) {
	p, err := ParseProxies([]string{"10.0.0.1", "10.0.0.2"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, remote, xff, want string
		local                   bool
	}{
		{"direct", "192.0.2.1:1", "", "192.0.2.1", false},
		{"direct ignores header", "192.0.2.1:1", "198.51.100.1", "192.0.2.1", false},
		{"loopback is local", "127.0.0.1:1", "", "127.0.0.1", true},
		{"v6 loopback is local", "[::1]:1", "", "::1", true},
		{"mapped loopback is local", "[::ffff:127.0.0.1]:1", "", "127.0.0.1", true},
		{"loopback with a header is not local", "127.0.0.1:1", "198.51.100.1", "127.0.0.1", false},
		{"proxy", "10.0.0.1:1", "198.51.100.1", "198.51.100.1", false},
		{"rightmost wins", "10.0.0.1:1", "6.6.6.6, 198.51.100.1", "198.51.100.1", false},
		{"skips trusted hops", "10.0.0.1:1", "198.51.100.1, 10.0.0.2", "198.51.100.1", false},
		{"mapped entry", "10.0.0.1:1", "::ffff:198.51.100.1", "198.51.100.1", false},
		{"no header is unknown", "10.0.0.1:1", "", "", false},
		{"only proxies is unknown", "10.0.0.1:1", "10.0.0.2", "", false},
		{"garbage is unknown", "10.0.0.1:1", "6.6.6.6, not-an-ip", "", false},
		{"bad remote", "nonsense", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := map[string]string{}
			if tc.xff != "" {
				h["X-Forwarded-For"] = tc.xff
			}
			c := p.Resolve(req(tc.remote, h))
			got := ""
			if c.Known() {
				got = c.Addr.String()
			}
			if got != tc.want || c.Local != tc.local {
				t.Errorf("addr %q local %v, want %q %v", got, c.Local, tc.want, tc.local)
			}
		})
	}
}

func TestLoopbackIsNotTrustedByDefault(t *testing.T) {
	p, err := ParseProxies(nil)
	if err != nil {
		t.Fatal(err)
	}
	c := p.Resolve(req("127.0.0.1:1", map[string]string{"X-Forwarded-For": "198.51.100.1", "X-Forwarded-Proto": "https"}))
	if c.Addr.String() != "127.0.0.1" || c.Proto != "http" {
		t.Errorf("%+v", c)
	}
}

func TestRouter(t *testing.T) {
	rt := NewRouter([]string{"para.vpn.example", "LAN.example:7700"}, "acme", []string{"127.0.0.1", "::1"})
	for host, want := range map[string]string{
		"acme.localhost:7700":      "acme",
		"ACME.LOCALHOST":           "acme",
		"acme.localhost.":          "acme",
		"beta.para.vpn.example":    "beta",
		"beta.lan.example:443":     "beta",
		"para.vpn.example":         "acme",
		"localhost:7700":           "acme",
		"127.0.0.1:7700":           "acme",
		"[::1]:7700":               "acme",
		"evil.example":             "",
		"a.b.localhost":            "",
		"-x.localhost":             "",
		"acme.localhost.evil.test": "",
		"..%2f.localhost":          "",
		"acme.localhost/x":         "",
		"":                         "",
	} {
		got, ok := rt.Route(host)
		if ok != (want != "") || got != want {
			t.Errorf("%q: %q %v, want %q", host, got, ok, want)
		}
	}
	bare, ok := NewRouter(nil, "", []string{"127.0.0.1"}).Route("127.0.0.1:7700")
	if ok {
		t.Errorf("no --vault: bare host routed to %q", bare)
	}
}

func client(ip string) Client { return Client{Addr: netip.MustParseAddr(ip)} }

func TestRules(t *testing.T) {
	r, err := ParseRules(`# comment
allow /            127.0.0.1 ::1 10.0.0.0/8  # trailing
allow /api/v1/open any
deny  /api/v1/admin
allow /api/v1/admin/health 10.1.2.3
`)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path, ip string
		want     bool
	}{
		{"/", "127.0.0.1", true},
		{"/x", "10.9.9.9", true},
		{"/x", "192.0.2.1", false},
		{"/api/v1/open/thing", "192.0.2.1", true},
		{"/api/v1/opener", "192.0.2.1", false},
		{"/api/v1/admin", "127.0.0.1", false},
		{"/api/v1/admin/key", "127.0.0.1", false},
		{"/api/v1/admin/health", "10.1.2.3", true},
		{"/api/v1/admin/health", "127.0.0.1", false},
	} {
		if got := r.Allow(tc.path, client(tc.ip)); got != tc.want {
			t.Errorf("%s from %s: %v", tc.path, tc.ip, got)
		}
	}
	if r.Allow("/", Client{}) {
		t.Error("an unknown client must be denied")
	}
	var nilRules *Rules
	if nilRules.Allow("/", client("127.0.0.1")) {
		t.Error("no rules must deny")
	}
	none, err := ParseRules("allow /api 127.0.0.1\n")
	if err != nil {
		t.Fatal(err)
	}
	if none.Allow("/other", client("127.0.0.1")) {
		t.Error("no match must deny")
	}
	if !none.Allow("/api/x", client("127.0.0.1")) {
		t.Error("match must allow")
	}
}

func TestRulesParseErrors(t *testing.T) {
	for _, text := range []string{
		"permit / any",
		"allow /",
		"allow x any",
		"allow /a/../b any",
		"allow // any",
		"allow / not-an-ip",
		"deny / 127.0.0.1",
		"allow /a any\ndeny /a",
	} {
		if _, err := ParseRules(text); err == nil {
			t.Errorf("%q parsed", text)
		}
	}
	if _, err := ParseRules(DefaultRules); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRules("/nonexistent/net.rules"); err == nil {
		t.Error("a missing file must be an error")
	}
}

func TestCanonical(t *testing.T) {
	for _, tc := range []struct {
		esc, dec, want string
		ok             bool
	}{
		{"/", "/", "/", true},
		{"/a/b", "/a/b", "/a/b", true},
		{"/a/b/", "/a/b/", "/a/b", true},
		{"/a%20b", "/a b", "/a b", true},
		{"/a//b", "/a//b", "", false},
		{"/a/../b", "/a/../b", "", false},
		{"/a/./b", "/a/./b", "", false},
		{"/a%2fb", "/a/b", "", false},
		{"/a%2Fb", "/a/b", "", false},
		{"/a%5cb", `/a\b`, "", false},
		{"/%2e%2e/a", "/../a", "", false},
		{"/a%252fb", "/a%2fb", "", false},
		{"/a%00", "/a\x00", "", false},
		{"a", "a", "", false},
		{"*", "*", "", false},
		{"", "", "", false},
	} {
		got, ok := Canonical(tc.esc, tc.dec)
		if got != tc.want || ok != tc.ok {
			t.Errorf("%q: %q %v", tc.esc, got, ok)
		}
	}
}

func TestPrincipalHas(t *testing.T) {
	p := Principal{Name: "k", Scopes: []string{ScopeVaultRead}}
	if !p.Has(ScopeVaultRead) || p.Has(ScopeVaultWrite) || p.Has(ScopeAdmin) || !p.Has(ScopeAnyKey) {
		t.Errorf("key: %+v", p)
	}
	if a := (Principal{Name: "admin", Admin: true}); !a.Has(ScopeAdmin) || !a.Has(ScopeRFXSend) {
		t.Error("admin")
	}
	if (Principal{}).Has(ScopeAnyKey) {
		t.Error("nobody has any-key")
	}
}

func TestBearerFromHeader(t *testing.T) {
	for h, want := range map[string]string{"Bearer abc": "abc", "bearer abc": "abc", "Basic abc": "", "Bearer": "", "Bearer  ": ""} {
		got, ok := BearerFromHeader(h)
		if got != want || ok != (want != "") {
			t.Errorf("%q: %q %v", h, got, ok)
		}
	}
}

func TestMalformedAuthorizationDoesNotFallBackToCookie(t *testing.T) {
	r := req("127.0.0.1:1", map[string]string{"Authorization": "Basic x", "Cookie": CookieName + "=s"})
	if _, ok := credential(r); ok {
		t.Error("a bad header must not pass over to the cookie")
	}
}

func TestOriginOK(t *testing.T) {
	c := Client{Proto: "http"}
	mk := func(method string, hdr map[string]string) *http.Request {
		r := httptest.NewRequest(method, "/", nil)
		r.Host = "acme.localhost:7700"
		for k, v := range hdr {
			r.Header.Set(k, v)
		}
		return r
	}
	same := map[string]string{"Origin": "http://acme.localhost:7700"}
	cases := []struct {
		name   string
		r      *http.Request
		cookie bool
		want   bool
	}{
		{"read", mk("GET", nil), false, true},
		{"cookie read same site", mk("GET", map[string]string{"Sec-Fetch-Site": "same-origin"}), true, true},
		{"cookie read cross site", mk("GET", map[string]string{"Sec-Fetch-Site": "cross-site"}), true, false},
		{"bearer write no origin", mk("POST", nil), false, true},
		{"cookie write no origin", mk("POST", nil), true, false},
		{"cookie write same origin metadata", mk("POST", map[string]string{"Sec-Fetch-Site": "same-origin"}), true, true},
		{"write same origin", mk("POST", same), true, true},
		{"write other port", mk("POST", map[string]string{"Origin": "http://acme.localhost:7701"}), false, false},
		{"write other vault", mk("POST", map[string]string{"Origin": "http://beta.localhost:7700"}), true, false},
		{"write https from http", mk("POST", map[string]string{"Origin": "https://acme.localhost:7700"}), false, false},
		{"write null", mk("POST", map[string]string{"Origin": "null"}), false, false},
		{"ws same", mk("GET", map[string]string{"Upgrade": "websocket", "Origin": "http://acme.localhost:7700"}), true, true},
		{"ws other", mk("GET", map[string]string{"Upgrade": "websocket", "Origin": "http://evil.example"}), false, false},
	}
	for _, tc := range cases {
		if got := originOK(tc.r, c, tc.cookie); got != tc.want {
			t.Errorf("%s: %v", tc.name, got)
		}
	}
}
