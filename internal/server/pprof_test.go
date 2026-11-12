package server

import (
	"net/http"
	"testing"
)

func TestPprofOffByDefault(t *testing.T) {
	f := newFixture(t)
	admin, err := f.keyA.AdminToken()
	if err != nil {
		t.Fatal(err)
	}
	wantStatus(t, f.do(call{target: "/api/debug/pprof/", token: admin}), http.StatusNotFound)
	wantStatus(t, f.do(call{target: "/api/debug/pprof/"}), http.StatusUnauthorized)
	wantStatus(t, f.do(call{target: "/api/debug/pprof/heap", token: admin}), http.StatusNotFound)
}

func TestPprofOn(t *testing.T) {
	f := newFixtureWith(t, func(o *Options) { o.Pprof = true })
	admin, err := f.keyA.AdminToken()
	if err != nil {
		t.Fatal(err)
	}
	wantStatus(t, f.do(call{target: "/api/debug/pprof/", token: admin}), 200)
	wantStatus(t, f.do(call{target: "/api/debug/pprof/heap?debug=1", token: admin}), 200)
	wantStatus(t, f.do(call{target: "/api/debug/pprof/cmdline", token: admin}), 200)
	wantStatus(t, f.do(call{target: "/api/debug/pprof/"}), http.StatusUnauthorized)
	// Agent and review keys: the admin scope is never granted to a key.
	for name, scopes := range map[string][]string{
		"agent":  {"vault:read", "vault:write", "request:queue", "request:run"},
		"review": {"vault:read", "review:decide"},
		"all":    allScopes,
	} {
		tok := f.key(f.keyA, name, scopes...)
		wantStatus(t, f.do(call{target: "/api/debug/pprof/", token: tok}), 403)
	}
	// The admin token from a non-loopback client of this machine.
	wantStatus(t, f.do(call{target: "/api/debug/pprof/", token: admin, peer: ownPeer}), 403)
	// Through a trusted proxy, the client is not local.
	wantStatus(t, f.do(call{target: "/api/debug/pprof/", token: admin, peer: proxyPeer,
		header: map[string]string{"X-Forwarded-For": "127.0.0.1"}}), 403)
}
