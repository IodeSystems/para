package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/parasolve/internal/ops"
)

// A real `para mcp` blocks proposal_wait on its own watcher; a separate
// process (the CLI) writes the decision and the call returns at once.
func TestMCPProposalWaitWakesOnEvent(t *testing.T) {
	e := newProposalEnv(t)
	filed := tool[ops.ProposalFiled](t, e.agent, "proposal_add", map[string]any{"title": "Blocks", "source": e.source,
		"items": []any{extItem("email", "ada@acme.example", "Hi.")}})
	type result struct {
		out ops.ProposalWaitOutput
		at  time.Time
	}
	done := make(chan result, 1)
	go func() {
		out := tool[ops.ProposalWaitOutput](t, e.agent, "proposal_wait", map[string]any{"id": filed.ID, "timeout_seconds": 30})
		done <- result{out, time.Now()}
	}()
	time.Sleep(500 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("proposal_wait returned with no decision")
	default:
	}
	e.ok("proposal", "deny", filed.ID, "i1", "--reason", "No")
	wrote := time.Now()
	select {
	case r := <-done:
		if r.out.Pending || len(r.out.Decisions) != 1 || r.out.Decisions[0].State != "denied" {
			t.Fatalf("wait = %+v", r.out)
		}
		t.Logf("proposal_wait woke %s after the CLI returned", r.at.Sub(wrote))
		if r.at.Sub(wrote) > 3*time.Second {
			t.Errorf("woke after %s", r.at.Sub(wrote))
		}
	case <-time.After(10 * time.Second):
		t.Fatal("proposal_wait did not wake on the event")
	}
	// Nothing to decide: the bounded wait returns pending.
	again := tool[ops.ProposalFiled](t, e.agent, "proposal_add", map[string]any{"title": "Stays", "source": e.source,
		"items": []any{extItem("email", "ada@acme.example", "Hi.")}})
	start := time.Now()
	if w := tool[ops.ProposalWaitOutput](t, e.agent, "proposal_wait", map[string]any{"id": again.ID, "timeout_seconds": 1}); !w.Pending {
		t.Errorf("wait = %+v, want pending", w)
	}
	if d := time.Since(start); d < 900*time.Millisecond {
		t.Errorf("pending returned after %s", d)
	}
}

func TestMCPRequestWaitWakesOnEvent(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	agent := startMCP(t, e, "--actor", "claude-one")
	type result struct {
		claimable bool
		at        time.Time
	}
	done := make(chan result, 1)
	go func() {
		out := tool[struct {
			Claimable bool `json:"claimable"`
		}](t, agent, "request_wait", map[string]any{"timeout_seconds": 30})
		done <- result{out.Claimable, time.Now()}
	}()
	time.Sleep(500 * time.Millisecond)
	select {
	case <-done:
		t.Fatal("request_wait returned with nothing queued")
	default:
	}
	e.ok("request", "add", "Wake the agent")
	wrote := time.Now()
	select {
	case r := <-done:
		if !r.claimable {
			t.Fatal("claimable = false")
		}
		t.Logf("request_wait woke %s after the CLI returned", r.at.Sub(wrote))
		if r.at.Sub(wrote) > 3*time.Second {
			t.Errorf("woke after %s", r.at.Sub(wrote))
		}
	case <-time.After(10 * time.Second):
		t.Fatal("request_wait did not wake on the event")
	}
	text, isErr := callText(t, agent, "request_wait", map[string]any{"timeout_seconds": 61})
	if !isErr || !strings.Contains(text, "over 60") {
		t.Errorf("61 s: %v %q", isErr, text)
	}
}
