package cli

import (
	"slices"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/ops"
)

// TestCLIWriteMetaSourcesAndRequest: --source and --request reach the
// receipt of an agent write, as sources and request do over MCP. The owner
// files no receipt, so a write as the owner refuses them.
func TestCLIWriteMetaSourcesAndRequest(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	var area ops.AreaAddOutput
	e.json(&area, "area", "add", "Acme LLC")
	var task ops.TaskAddOutput
	e.json(&task, "task", "add", "Check the filing", "--done-when", "Checked.", "--area", area.ID)

	e.ok("--actor", "claude", "--why", "The filing page says so.", "--source", "https://wiki.acme.example/filing", "--source", area.ID,
		"--request", "request-synthetic", "task", "edit", task.ID, "--done-when", "Checked on the page.")
	var rl ops.ReceiptListOutput
	e.json(&rl, "receipt", "list")
	if len(rl.Items) != 1 {
		t.Fatalf("receipts = %+v", rl.Items)
	}
	r := rl.Items[0]
	if !slices.Equal(r.Sources, []string{"https://wiki.acme.example/filing", area.ID}) || r.Request != "request-synthetic" {
		t.Fatalf("receipt = %+v", r)
	}

	_, _, err := e.run("--source", "https://wiki.acme.example/filing", "task", "edit", task.ID, "--done-when", "Owner text.")
	if err == nil || !strings.Contains(err.Error(), "need --actor") {
		t.Fatalf("owner --source: err = %v", err)
	}
	_, _, err = e.run("--request", "request-synthetic", "task", "edit", task.ID, "--done-when", "Owner text.")
	if err == nil || !strings.Contains(err.Error(), "need --actor") {
		t.Fatalf("owner --request: err = %v", err)
	}
	// A read takes no receipt fields and is not refused.
	e.ok("--source", "x", "task", "show", task.ID)
}

// TestCLINamedFlags: depends set takes --on, and task add takes --title, as
// the op inputs name them.
func TestCLINamedFlags(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	var area ops.AreaAddOutput
	e.json(&area, "area", "add", "Acme LLC")
	var a, b ops.TaskAddOutput
	e.json(&a, "task", "add", "--title", "File the return", "--area", area.ID)
	e.json(&b, "task", "add", "Gather receipts", "--area", area.ID)
	var sh ops.TaskShowOutput
	e.json(&sh, "task", "show", a.ID)
	if sh.Task.Title != "File the return" {
		t.Fatalf("title = %q", sh.Task.Title)
	}
	if _, _, err := e.run("task", "add", "One", "--title", "Two", "--area", area.ID); err == nil || !strings.Contains(err.Error(), "not both") {
		t.Fatalf("both titles: err = %v", err)
	}
	if _, _, err := e.run("task", "add", "--area", area.ID); err == nil || !strings.Contains(err.Error(), "title is required") {
		t.Fatalf("no title: err = %v", err)
	}

	var ds ops.DependsSetOutput
	e.json(&ds, "depends", "set", a.ID, "--on", b.ID)
	if !slices.Equal(ds.DependsOn, []string{b.ID}) {
		t.Fatalf("depends = %+v", ds)
	}
	if _, _, err := e.run("depends", "set", a.ID, b.ID, "--on", area.ID); err == nil || !strings.Contains(err.Error(), "not both") {
		t.Fatalf("both on: err = %v", err)
	}
}

// TestTaskAddDependsOnObligation: task add --depends-on takes what
// depends_set takes, an obligation among them.
func TestTaskAddDependsOnObligation(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	var area ops.AreaAddOutput
	e.json(&area, "area", "add", "Acme LLC")
	var o ops.ObligationAddOutput
	e.json(&o, "obligation", "add", "Send the plan", "--kind", "commitment", "--due", "2999-01-01", "--area", area.ID)
	var task ops.TaskAddOutput
	e.json(&task, "task", "add", "Draft the plan", "--area", area.ID, "--depends-on", o.ID)
	var sh ops.TaskShowOutput
	e.json(&sh, "task", "show", task.ID)
	if !slices.Equal(sh.Task.DependsOn, []string{o.ID}) {
		t.Fatalf("depends_on = %v", sh.Task.DependsOn)
	}
	var more ops.TaskAddOutput
	e.json(&more, "task", "add", "Review the plan", "--area", area.ID)
	e.ok("task", "depends", "add", more.ID, o.ID)
}
