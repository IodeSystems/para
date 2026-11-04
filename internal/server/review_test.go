package server

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/netguard"
	"github.com/iodesystems/para/internal/ops"
)

// review:decide decides on a browser session and authors nothing: it cannot
// add a task, a bearer key cannot use it, and an approved change runs under it
// though the key holds no vault:write.
func TestReviewDecideScope(t *testing.T) {
	f := newFixture(t)
	viewer := f.key(f.keyA, "viewer", netguard.ScopeVaultRead, netguard.ScopeReviewDecide)
	session := f.cookieHeader(viewer)
	wantStatus(t, f.do(call{method: post, target: "/api/v1/task/add", header: session,
		body: `{"project":"project-sample","title":"Nope","done_when":"Never."}`}), 403)
	draft := `{"id":"proposal-sample","revision":1,"items":[{"item":"i1","answer":{"decision":"approve"}}]}`
	wantStatus(t, f.do(call{method: post, target: "/api/v1/proposal/draft", token: viewer, body: draft}), 403)
	wantStatus(t, f.do(call{method: post, target: "/api/v1/proposal/draft", header: session, body: draft}), 200)
	w := f.do(call{method: post, target: "/api/v1/proposal/submit", header: session, body: `{"id":"proposal-sample","revision":1}`})
	wantStatus(t, w, 200)
	var out ops.ProposalSubmitOutput
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil || len(out.Items) == 0 || out.Items[0].Result == "" || len(out.Failed) != 0 {
		t.Fatalf("submit = %s", w.Body.String())
	}
	// A vault:write key cannot decide.
	writer := f.key(f.keyA, "writer", netguard.ScopeVaultRead, netguard.ScopeVaultWrite)
	wantStatus(t, f.do(call{method: post, target: "/api/v1/stack/decide", header: f.cookieHeader(writer),
		body: `{"items":[{"proposal":"proposal-sample","item":"i3"}],"decision":"reject"}`}), 403)
}

// The gate answers a held-back REST write with 202 and the staged item; the
// rating headers let it through.
func TestGateOverREST(t *testing.T) {
	f := newFixture(t)
	if err := f.store(t).SetConfig(contextBackground(), "gate.require", ""); err != nil {
		t.Fatal(err)
	}
	agent := f.key(f.keyA, "claude", netguard.ScopeVaultRead, netguard.ScopeVaultWrite)
	why := map[string]string{"X-Para-Why": "The owner asked."}
	w := f.do(call{method: post, target: "/api/v1/inbox/add", token: agent, header: why, body: `{"text":"held capture"}`})
	wantStatus(t, w, 202)
	if !strings.Contains(w.Body.String(), `"staged": true`) || !strings.Contains(w.Body.String(), `"op": "inbox_add"`) {
		t.Fatalf("body = %s", w.Body.String())
	}
	rated := map[string]string{"X-Para-Why": "The owner asked.", "X-Para-Minor": "true", "X-Para-Obvious": "true"}
	wantStatus(t, f.do(call{method: post, target: "/api/v1/inbox/add", token: agent, header: rated, body: `{"text":"rated capture"}`}), 200)
}
