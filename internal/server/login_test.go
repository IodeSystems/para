package server

import (
	"encoding/json"
	"testing"

	"github.com/iodesystems/para/internal/keys"
	"github.com/iodesystems/para/internal/netguard"
)

// A login key (the web scope set) on a browser session captures io and pins
// it, and still cannot reach an admin route.
func TestLoginKeySessionWrites(t *testing.T) {
	f := newFixture(t)
	_, tok, err := f.keyA.CreateLogin(netguard.ScopeSets["web"], keys.LoginTTL)
	if err != nil {
		t.Fatal(err)
	}
	hdr := f.cookieHeader(tok)
	w := f.do(call{method: post, target: "/api/v1/io/add", header: hdr, body: `{"direction":"in","text":"from the web"}`})
	wantStatus(t, w, 200)
	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || out.ID == "" {
		t.Fatalf("io add: %v %s", err, w.Body.String())
	}
	wantStatus(t, f.do(call{method: post, target: "/api/v1/pin", header: hdr, body: `{"id":"` + out.ID + `"}`}), 200)
	wantStatus(t, f.do(call{method: post, target: "/api/v1/unpin", header: hdr, body: `{"id":"` + out.ID + `"}`}), 200)
	wantStatus(t, f.do(call{method: post, target: "/api/v1/close", header: hdr, body: `{"id":"` + out.ID + `","outcome":"dropped","reason":"test"}`}), 200)

	// The same over GraphQL, the path the web app uses.
	gw, res := f.graphqlAs(call{header: hdr}, `mutation { para { v1 { ioAdd(body: {direction: in, text: "via graphql"}) { id } } } }`, nil)
	wantStatus(t, gw, 200)
	if res["errors"] != nil {
		t.Errorf("graphql io add: %v", res["errors"])
	}

	// A web key is not the admin token: admin routes stay closed.
	wantStatus(t, f.do(call{target: "/api/v1/admin/key/list", header: hdr}), 403)
	web := f.key(f.keyA, "webkey", netguard.ScopeSets["web"]...)
	wantStatus(t, f.do(call{target: "/api/v1/admin/key/list", token: web}), 403)
	wantStatus(t, f.do(call{method: post, target: "/api/v1/admin/key/create", token: web, body: `{"name":"x","scopes":["vault:read"]}`}), 403)
}

// An admin-token session from this machine writes. From another address the
// admin token is refused at the token stage, for every request: the admin
// token works from this machine only.
func TestAdminSessionWrites(t *testing.T) {
	f := newFixture(t)
	admin, err := f.keyA.AdminToken()
	if err != nil {
		t.Fatal(err)
	}
	hdr := f.cookieHeader(admin)
	wantStatus(t, f.do(call{method: post, target: "/api/v1/io/add", header: hdr, body: `{"direction":"in","text":"from admin"}`}), 200)
	wantStatus(t, f.do(call{method: post, target: "/api/v1/io/add", token: admin, body: `{"direction":"in","text":"from admin bearer"}`}), 200)
	gw, res := f.graphqlAs(call{header: hdr}, `mutation { para { v1 { ioAdd(body: {direction: in, text: "admin graphql"}) { id } } } }`, nil)
	wantStatus(t, gw, 200)
	if res["errors"] != nil {
		t.Errorf("graphql: %v", res["errors"])
	}
	remote := map[string]string{"Cookie": hdr["Cookie"], "Origin": hdr["Origin"]}
	wantStatus(t, f.do(call{method: post, target: "/api/v1/io/add", peer: "203.0.113.9:1", header: remote, body: `{"direction":"in","text":"x"}`}), 403)
}
