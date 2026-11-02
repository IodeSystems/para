package cli

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/ops"
	"github.com/iodesystems/parasolve/internal/recur"
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

func TestRecurrence(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	var area ops.AreaAddOutput
	e.json(&area, "area", "add", "Atlas service")
	now := time.Now()
	day := func(t time.Time) string { return t.Format(recur.Layout) }
	count := func() int {
		var l ops.TaskListOutput
		e.json(&l, "task", "list")
		return len(l.Tasks)
	}
	show := func(id string) ops.TaskView {
		var o ops.TaskShowOutput
		e.json(&o, "task", "show", id)
		return o.Task
	}

	// Done makes the next task, once, however many times it is repeated.
	var added ops.TaskAddOutput
	e.json(&added, "task", "add", "Rotate logs", "--area", area.ID, "--due", day(now), "--recur", "weekly", "--body", "Check the disk.")
	var done ops.TaskDoneOutput
	e.json(&done, "task", "done", added.ID)
	if done.Status != model.TaskDone || done.Next == "" || done.Next == added.ID {
		t.Fatalf("done = %+v", done)
	}
	next := show(done.Next)
	if next.Previous != added.ID || next.Status != model.TaskTodo || next.Title != "Rotate logs" ||
		next.Recur != "weekly" || next.Area != area.ID || string(next.Due) != day(now.AddDate(0, 0, 7)) {
		t.Fatalf("next = %+v", next)
	}
	var sh ops.TaskShowOutput
	e.json(&sh, "task", "show", done.Next)
	if sh.Body != "Check the disk." {
		t.Fatalf("body = %q", sh.Body)
	}
	var again ops.TaskDoneOutput
	e.json(&again, "task", "done", added.ID)
	if again.Next != done.Next || again.Status != model.TaskDone || count() != 2 {
		t.Fatalf("second done = %+v, tasks %d", again, count())
	}

	// Skip drops this one and makes the next. A late task moves to a future date.
	var late ops.TaskAddOutput
	e.json(&late, "task", "add", "Pay the domain", "--area", area.ID, "--due", "2020-01-31", "--recur", "monthly")
	var skipped ops.TaskDoneOutput
	e.json(&skipped, "task", "done", late.ID, "--skip")
	if skipped.Status != model.TaskDropped || skipped.Next == "" {
		t.Fatalf("skip = %+v", skipped)
	}
	rule, _ := recur.Parse("monthly")
	from, _ := time.ParseInLocation(recur.Layout, "2020-01-31", time.Local)
	if got, want := string(show(skipped.Next).Due), day(rule.Next(from, now)); got != want || got < day(now) {
		t.Fatalf("late next due = %s, want %s (not before today)", got, want)
	}
	if _, _, err := e.run("task", "done", skipped.Next, "--skip"); err != nil {
		t.Fatalf("skip the next: %v", err)
	}

	// No due date: the next one is a step from today.
	var undated ops.TaskAddOutput
	e.json(&undated, "task", "add", "Sweep", "--area", area.ID, "--recur", "every 3 days")
	var ud ops.TaskDoneOutput
	e.json(&ud, "task", "done", undated.ID)
	if got := string(show(ud.Next).Due); got != day(now.AddDate(0, 0, 3)) {
		t.Fatalf("undated next due = %s", got)
	}

	// Skip needs a recurring task; a task that does not recur is done once.
	var once ops.TaskAddOutput
	e.json(&once, "task", "add", "One time", "--area", area.ID)
	if _, _, err := e.run("task", "done", once.ID, "--skip"); err == nil {
		t.Error("skip on a task that does not recur: want an error")
	}
	e.ok("task", "done", once.ID)
	if _, _, err := e.run("task", "done", once.ID); err == nil {
		t.Error("done twice on a task that does not recur: want an error")
	}

	// An unknown recur is stored with a warning, lint warns, and nothing recurs.
	var odd ops.TaskAddOutput
	e.json(&odd, "task", "add", "Renew certificate", "--area", area.ID, "--recur", "yearly")
	if len(odd.Warnings) != 1 {
		t.Fatalf("warnings = %v", odd.Warnings)
	}
	var lint ops.LintOutput
	e.json(&lint, "lint")
	if lint.Errors != 0 || lint.Warnings != 1 || lint.Findings[0].Code != "recur" {
		t.Fatalf("lint = %+v", lint)
	}
	var od ops.TaskDoneOutput
	e.json(&od, "task", "done", odd.ID)
	if od.Next != "" || od.Status != model.TaskDone {
		t.Fatalf("unknown recur done = %+v", od)
	}

	// A closed project gets no next task.
	var proj ops.ProjectAddOutput
	e.json(&proj, "project", "add", "Move rack", "--owner", "Jordan Example")
	var pt ops.TaskAddOutput
	e.json(&pt, "task", "add", "Dust", "-p", proj.ID, "--recur", "daily")
	e.ok("project", "close", proj.ID, "--force")
	var pd ops.TaskDoneOutput
	e.json(&pd, "task", "done", pt.ID)
	if pd.Next != "" || len(pd.Warnings) != 1 || !strings.Contains(pd.Warnings[0], "closed") {
		t.Fatalf("closed project done = %+v", pd)
	}
}

func TestRecurringAgentTask(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	var area ops.AreaAddOutput
	e.json(&area, "area", "add", "Atlas service")
	var plain, bot ops.TaskAddOutput
	e.json(&plain, "task", "add", "Not recurring", "--area", area.ID, "--assignee", "agent:scribe")
	e.json(&bot, "task", "add", "Summarize alerts", "--area", area.ID, "--assignee", "agent:scribe", "--recur", "daily", "--due", time.Now().Format(recur.Layout))
	if plain.Request != "" || bot.Request == "" {
		t.Fatalf("requests: plain %q, recurring %q", plain.Request, bot.Request)
	}
	var done ops.TaskDoneOutput
	e.json(&done, "task", "done", bot.ID)
	if done.Next == "" || done.Request == "" || done.Request == bot.Request {
		t.Fatalf("done = %+v", done)
	}
	var reqs ops.RequestListOutput
	e.json(&reqs, "request", "list")
	if len(reqs.Items) != 2 || reqs.Items[1].Links[0] != done.Next || reqs.Items[1].Status != model.RequestQueued {
		t.Fatalf("requests = %+v", reqs.Items)
	}
	var again ops.TaskDoneOutput
	e.json(&again, "task", "done", bot.ID)
	e.json(&reqs, "request", "list")
	if again.Next != done.Next || len(reqs.Items) != 2 {
		t.Fatalf("repeat made a request: %+v, %d requests", again, len(reqs.Items))
	}
}
