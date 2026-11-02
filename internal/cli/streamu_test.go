package cli

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/ops"
	"github.com/iodesystems/parasolve/internal/today"
)

// streamUParityCases are the parity rows of stream U: assignees, recurrence,
// incidents. Mutating ops use one document for Invoke (a) and one for the CLI
// (b). Ids come from seedStreamUParity.
func streamUParityCases() map[string]parityCase {
	return map[string]parityCase{
		"task_assign": {`{"id":"task-u-a","assignee":"person-u-seed"}`, []string{"task", "assign", "task-u-b", "person-u-seed"}, false},
		"waiting":     {`{"today":"2026-11-01"}`, []string{"waiting", "--today", "2026-11-01"}, false},
	}
}

// seedStreamUParity creates the fixed-id documents the rows above refer to.
func seedStreamUParity(t *testing.T, deps ops.Deps) {
	t.Helper()
	ctx := context.Background()
	for _, d := range []model.Document{
		{ID: "person-u-seed", Folder: model.FolderOrg, Title: "Uma Example",
			Meta: &model.Person{Common: model.Common{Type: model.TypePerson}, Org: "acme.example"}},
		{ID: "area-u-seed", Folder: model.FolderAreas, Title: "Atlas service",
			Meta: &model.Area{Common: model.Common{Type: model.TypeArea}, Status: model.AreaActive}},
		{ID: "task-u-a", Folder: model.FolderProjects, Title: "U task a",
			Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskTodo, Area: model.LinkTo("area-u-seed")}},
		{ID: "task-u-b", Folder: model.FolderProjects, Title: "U task b",
			Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskTodo, Area: model.LinkTo("area-u-seed")}},
	} {
		if _, err := deps.Store.Create(ctx, d); err != nil {
			t.Fatalf("seed %s: %v", d.ID, err)
		}
	}
}

func TestAssignee(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	var area ops.AreaAddOutput
	e.json(&area, "area", "add", "Atlas service")
	var person ops.OrgAddOutput
	e.json(&person, "org", "person", "add", "Uma Example", "--org", "acme.example")
	add := func(title string, extra ...string) string {
		var o ops.TaskAddOutput
		e.json(&o, append([]string{"task", "add", title, "--area", area.ID}, extra...)...)
		return o.ID
	}
	mine := add("mine")
	theirs := add("theirs", "--due", "2026-11-05")
	bot := add("bot work", "--assignee", "agent:Scribe")
	e.ok("task", "assign", theirs, person.ID)

	ids := func(tasks []ops.TaskView) []string {
		out := []string{}
		for _, t := range tasks {
			out = append(out, t.ID)
		}
		return out
	}
	var next ops.NextOutput
	e.json(&next, "next")
	if got := ids(next.Tasks); !reflect.DeepEqual(got, []string{mine}) {
		t.Fatalf("next = %v, want only %s", got, mine)
	}
	var list ops.TaskListOutput
	e.json(&list, "task", "list", "--assignee", person.ID)
	if got := ids(list.Tasks); !reflect.DeepEqual(got, []string{theirs}) {
		t.Fatalf("list --assignee person = %v", got)
	}
	e.json(&list, "task", "list", "--assignee", "agent:scribe")
	if got := ids(list.Tasks); !reflect.DeepEqual(got, []string{bot}) {
		t.Fatalf("list --assignee agent = %v", got)
	}
	e.json(&list, "task", "list", "--assignee", "self")
	if got := ids(list.Tasks); !reflect.DeepEqual(got, []string{mine}) {
		t.Fatalf("list --assignee self = %v", got)
	}

	var who ops.WhoOutput
	e.json(&who, "who", "Uma")
	if who.Person == nil || len(who.Person.OpenTasks) != 1 || who.Person.OpenTasks[0].ID != theirs {
		t.Fatalf("who = %+v", who.Person)
	}

	var wait ops.WaitingOutput
	e.json(&wait, "waiting", "--today", "2026-11-01")
	if wait.Count != 2 || !reflect.DeepEqual(ids(wait.Tasks), []string{theirs, bot}) {
		t.Fatalf("waiting = %+v", wait)
	}
	var rfx ops.RFXAddOutput
	e.json(&rfx, "rfx", "add", "Can X launch?", "--route", "domain-logging")
	e.json(&wait, "waiting", "--today", "2026-11-01")
	if wait.Count != 3 || len(wait.RFX) != 1 || wait.RFX[0].ID != rfx.ID {
		t.Fatalf("waiting with rfx = %+v", wait)
	}

	// Refusals.
	for _, bad := range []string{"person-nobody", "agent:", "agent:Bad Name", area.ID} {
		if _, _, err := e.run("task", "assign", mine, bad); err == nil {
			t.Errorf("assign %q: want an error", bad)
		}
	}
	// Back to self; a done task cannot be reassigned.
	e.ok("task", "assign", theirs, "self")
	e.json(&next, "next")
	if len(next.Tasks) != 2 {
		t.Fatalf("next after self = %v", ids(next.Tasks))
	}
	e.ok("task", "done", mine)
	if _, _, err := e.run("task", "assign", mine, person.ID); err == nil {
		t.Error("assign a done task: want an error")
	}

	var lint ops.LintOutput
	e.json(&lint, "lint")
	if lint.Errors != 0 {
		t.Fatalf("lint = %+v", lint)
	}

	// Today counts what is waited on.
	var rep today.Report
	e.json(&rep, "today")
	if got := rep.Vaults[0].Waiting; got != 2 { // bot task + the rfx
		t.Fatalf("today waiting = %d", got)
	}
	if !strings.Contains(e.ok("today"), "waiting 2") {
		t.Fatalf("today text lacks the waiting count")
	}
}
