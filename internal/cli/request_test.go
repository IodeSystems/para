package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/paradize/internal/model"
	"github.com/iodesystems/paradize/internal/ops"
)

// requestParityCases are the request rows of the parity table. A mutating op
// uses one document for Invoke and one for the CLI. seedRequestParity makes
// them. Acting as "user" on the cli surface, which holds the claims.
var requestParityCases = map[string]parityCase{
	"request_add":    {`{"ask":"Parity ask"}`, []string{"request", "add", "Parity ask"}, false},
	"request_list":   {`{}`, []string{"request", "list"}, true},
	"request_show":   {`{"id":"request-show"}`, []string{"request", "show", "request-show"}, true},
	"request_cancel": {`{"id":"request-c1"}`, []string{"request", "cancel", "request-c2"}, false},
	"request_next":   {`{}`, []string{"request", "next"}, false},
	"request_note":   {`{"id":"request-n1","text":"progress"}`, []string{"request", "note", "request-n2", "progress"}, false},
	"request_done":   {`{"id":"request-d1","result":"Done."}`, []string{"request", "done", "request-d2", "Done."}, false},
	"request_fail":   {`{"id":"request-f1","reason":"Blocked."}`, []string{"request", "fail", "request-f2", "Blocked."}, false},
}

// seedRequestParity runs last in the parity table. Ids sort so that
// request_next claims a queued one that no other case needs.
func seedRequestParity(t *testing.T, deps ops.Deps) {
	t.Helper()
	install := deps.Store.Info().Install
	lease := time.Now().Add(time.Hour).UTC().Format(time.RFC3339Nano)
	mk := func(id string, status model.RequestStatus, claimed bool) {
		t.Helper()
		r := &model.Request{Common: model.Common{Type: model.TypeRequest}, Status: status, Runner: install, Queued: "2026-11-01T09:00:00Z"}
		if claimed {
			r.Claimed, r.Lease = "user", lease
		}
		if _, err := deps.Store.Create(t.Context(), model.Document{ID: id, Folder: model.FolderRequests, Title: "Request " + id, Meta: r}); err != nil {
			t.Fatal(err)
		}
	}
	mk("request-show", model.RequestCancelled, false)
	for _, id := range []string{"request-c1", "request-c2", "request-q1", "request-q2"} {
		mk(id, model.RequestQueued, false)
	}
	for _, id := range []string{"request-n1", "request-n2", "request-d1", "request-d2", "request-f1", "request-f2"} {
		mk(id, model.RequestClaimed, true)
	}
}

func TestRequestFlow(t *testing.T) {
	e := newEnv(t)
	e.init("acme")

	var added ops.RequestAddOutput
	// A bad link is refused; no document is written.
	if _, _, err := e.run("request", "add", "Summarize inbox", "--link", "project-none"); err == nil {
		t.Fatal("unknown link accepted")
	}
	e.json(&added, "request", "add", "Summarize inbox")
	if added.Status != model.RequestQueued || !strings.HasPrefix(added.ID, "request-") || added.Runner == "" {
		t.Fatalf("added = %+v", added)
	}
	var next ops.RequestNextOutput
	e.json(&next, "request", "next", "--lease", "60")
	if next.Request == nil || next.Request.ID != added.ID || next.Request.ClaimedBy != "user" {
		t.Fatalf("next = %+v", next)
	}
	e.ok("request", "note", added.ID, "half way")
	e.ok("request", "done", added.ID, "All", "done")
	if out := e.ok("request", "show", added.ID); !strings.Contains(out, "status\tdone") || !strings.Contains(out, "## Result\n\nAll done") {
		t.Fatalf("show:\n%s", out)
	}
	var lint ops.LintOutput
	e.json(&lint, "lint")
	if lint.Errors != 0 || lint.Warnings != 0 {
		t.Fatalf("lint = %+v", lint)
	}
	if out := e.ok("request", "next"); strings.TrimSpace(out) != "none" {
		t.Fatalf("next on empty = %q", out)
	}
}

// accept is for people only: it is not an op, so no MCP tool carries it.
func TestRequestAcceptIsCLIOnly(t *testing.T) {
	for _, op := range ops.All() {
		if strings.Contains(op.Name(), "accept") || op.Name() == "request_wait" {
			t.Errorf("op %s is reachable from an agent surface", op.Name())
		}
	}
	e := newEnv(t)
	e.init("acme")
	l := &loader{env: func(k string) string {
		if k == "PARA_HOME" {
			return e.home
		}
		return ""
	}}
	if err := l.load(); err != nil {
		t.Fatal(err)
	}
	deps, err := l.deps()
	if err != nil {
		t.Fatal(err)
	}
	// A proposal as the MCP surface leaves it.
	if _, err := deps.Store.Create(t.Context(), model.Document{ID: "request-prop", Folder: model.FolderRequests, Title: "Proposed",
		Meta: &model.Request{Common: model.Common{Type: model.TypeRequest}, Status: model.RequestProposed,
			Runner: deps.Store.Info().Install, Queued: "2026-11-01T09:00:00Z"}}); err != nil {
		t.Fatal(err)
	}
	if out := e.ok("request", "next"); strings.TrimSpace(out) != "none" {
		t.Fatalf("claimed a proposal: %q", out)
	}
	var acc ops.RequestChangeOutput
	e.json(&acc, "request", "accept", "request-prop")
	if acc.Status != model.RequestQueued {
		t.Fatalf("accept = %+v", acc)
	}
	if _, _, err := e.run("request", "accept", "request-prop"); err == nil || !strings.Contains(err.Error(), "not proposed") {
		t.Fatalf("second accept: %v", err)
	}
	if out := e.ok("request", "next"); !strings.HasPrefix(out, "request-prop") {
		t.Fatalf("next = %q", out)
	}
}

func TestRequestWait(t *testing.T) {
	e := newEnv(t)
	e.init("acme")

	// Nothing queued: wait times out and exits non-zero.
	start := time.Now()
	if _, _, err := e.run("request", "wait", "--timeout", "150ms", "--interval", "20ms"); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("empty wait: %v", err)
	}
	if time.Since(start) < 140*time.Millisecond {
		t.Errorf("returned early after %s", time.Since(start))
	}

	// A request that appears wakes it, and wait claims nothing.
	done := make(chan error, 1)
	go func() {
		_, _, err := e.run("request", "wait", "--timeout", "10s", "--interval", "20ms")
		done <- err
	}()
	time.Sleep(100 * time.Millisecond)
	select {
	case err := <-done:
		t.Fatalf("woke with nothing queued: %v", err)
	default:
	}
	var added ops.RequestAddOutput
	e.json(&added, "request", "add", "Wake the agent")
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("wait did not wake")
	}
	var list ops.RequestListOutput
	e.json(&list, "request", "list")
	if len(list.Items) != 1 || list.Items[0].Status != model.RequestQueued {
		t.Fatalf("wait changed state: %+v", list)
	}
	// Already claimable: returns at once.
	if _, _, err := e.run("request", "wait", "--timeout", "5s"); err != nil {
		t.Fatal(err)
	}
	// Bad flags.
	if _, _, err := e.run("request", "wait", "--interval", "0s"); err == nil {
		t.Error("zero interval accepted")
	}
}
