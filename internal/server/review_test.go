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

// An agent's undoable REST write applies at once; rating headers are ignored
// and named in X-Para-Note. A write that cannot be undone is the owner's: 403.
func TestGateOverREST(t *testing.T) {
	f := newFixture(t)
	agent := f.key(f.keyA, "claude", netguard.ScopeVaultRead, netguard.ScopeVaultWrite)
	why := map[string]string{"X-Para-Why": "The owner asked."}
	w := f.do(call{method: post, target: "/api/v1/io/add", token: agent, header: why, body: `{"direction":"in","text":"plain capture"}`})
	wantStatus(t, w, 200)
	if w.Header().Get("X-Para-Note") != "" {
		t.Fatalf("note on an unrated write: %q", w.Header().Get("X-Para-Note"))
	}
	rated := map[string]string{"X-Para-Why": "The owner asked.", "X-Para-Minor": "true", "X-Para-Obvious": "true"}
	w = f.do(call{method: post, target: "/api/v1/io/add", token: agent, header: rated, body: `{"direction":"in","text":"rated capture"}`})
	wantStatus(t, w, 200)
	if note := w.Header().Get("X-Para-Note"); !strings.HasPrefix(note, "minor, obvious ignored") {
		t.Fatalf("X-Para-Note = %q", note)
	}
}
