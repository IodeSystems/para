package cli

import (
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/ops"
)

// TestCLIActor: --actor makes a CLI write an agent write, as over MCP.
func TestCLIActor(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	var area ops.AreaAddOutput
	e.json(&area, "area", "add", "Acme LLC")
	var task ops.TaskAddOutput
	e.json(&task, "task", "add", "Check the filing", "--done-when", "Checked.", "--area", area.ID)

	// Default actor: owner, no why needed, no receipt.
	var rl ops.ReceiptListOutput
	e.json(&rl, "receipt", "list")
	if len(rl.Items) != 0 {
		t.Fatalf("owner write filed a receipt: %+v", rl.Items)
	}
	e.ok("task", "edit", task.ID, "--done-when", "Checked twice.")
	e.json(&rl, "receipt", "list")
	if len(rl.Items) != 0 {
		t.Fatalf("owner edit filed a receipt: %+v", rl.Items)
	}

	// Agent actor without why: refused, nothing written.
	_, _, err := e.run("--actor", "claude", "task", "edit", task.ID, "--done-when", "Agent text.")
	if err == nil || !strings.Contains(err.Error(), "--why is required") {
		t.Fatalf("no why: err = %v", err)
	}
	var sh ops.TaskShowOutput
	e.json(&sh, "task", "show", task.ID)
	if sh.Task.DoneWhen != "Checked twice." {
		t.Fatalf("refused write changed the task: %q", sh.Task.DoneWhen)
	}

	// With why: written, receipt names actor and why. PARA_ACTOR works too.
	e.vars["PARA_ACTOR"] = "claude"
	e.ok("--why", "The owner asked for it.", "task", "edit", task.ID, "--done-when", "Agent text.")
	delete(e.vars, "PARA_ACTOR")
	e.json(&rl, "receipt", "list")
	if len(rl.Items) != 1 || rl.Items[0].Actor != "claude" || rl.Items[0].Why != "The owner asked for it." || rl.Items[0].Surface != cliAgentSurface {
		t.Fatalf("receipts = %+v", rl.Items)
	}

	// Reads need no why.
	e.ok("--actor", "claude", "task", "show", task.ID)

	// Owner-only op: refused for an agent, with or without why.
	_, _, err = e.run("--actor", "claude", "--why", "Tidy.", "defer", task.ID)
	if err == nil || !strings.Contains(err.Error(), ops.ErrPersonOnly.Error()) {
		t.Fatalf("agent defer: err = %v", err)
	}
	e.ok("defer", task.ID)
}
