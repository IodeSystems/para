package server

import (
	"testing"

	"github.com/iodesystems/para/internal/netguard"
)

// An agent's undoable REST write applies at once; rating headers are ignored
// and named in X-Para-Note. A write that cannot be undone is the owner's: 403.
// review:decide decides on a browser session and authors nothing: it cannot
// add a task, a bearer key cannot use it, and a vault:write key cannot decide.
func TestReviewDecideScope(t *testing.T) {
	f := newFixture(t)
	viewer := f.key(f.keyA, "viewer", netguard.ScopeVaultRead, netguard.ScopeReviewDecide)
	session := f.cookieHeader(viewer)
	wantStatus(t, f.do(call{method: post, target: "/api/v1/task/add", header: session,
		body: `{"goal":"goal-sample","title":"Nope","done_when":"Never."}`}), 403)
	review := `{"id":"question-sample","decision":"skip"}`
	wantStatus(t, f.do(call{method: post, target: "/api/v1/question/review", token: viewer, body: review}), 403)
	wantStatus(t, f.do(call{method: post, target: "/api/v1/question/review", header: session, body: review}), 200)
	writer := f.key(f.keyA, "writer", netguard.ScopeVaultRead, netguard.ScopeVaultWrite)
	wantStatus(t, f.do(call{method: post, target: "/api/v1/question/review", header: f.cookieHeader(writer), body: review}), 403)
}

func TestGateOverREST(t *testing.T) {
	f := newFixture(t)
	agent := f.key(f.keyA, "claude", netguard.ScopeVaultRead, netguard.ScopeVaultWrite)
	why := map[string]string{"X-Para-Why": "The owner asked."}
	w := f.do(call{method: post, target: "/api/v1/io/add", token: agent, header: why, body: `{"direction":"in","text":"plain capture"}`})
	wantStatus(t, w, 200)
	if w.Header().Get("X-Para-Note") != "" {
		t.Fatalf("note on an unrated write: %q", w.Header().Get("X-Para-Note"))
	}
	// Rating headers are gone (D4): ignored, with no note.
	rated := map[string]string{"X-Para-Why": "The owner asked.", "X-Para-Minor": "true", "X-Para-Obvious": "true"}
	w = f.do(call{method: post, target: "/api/v1/io/add", token: agent, header: rated, body: `{"direction":"in","text":"rated capture"}`})
	wantStatus(t, w, 200)
	if note := w.Header().Get("X-Para-Note"); note != "" {
		t.Fatalf("X-Para-Note = %q", note)
	}
}
