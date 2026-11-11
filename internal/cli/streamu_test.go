package cli

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/guide"
	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
	"github.com/iodesystems/para/internal/recur"
	"github.com/iodesystems/para/internal/today"
)

// streamUParityCases are the parity rows of stream U: assignees, recurrence,
// incidents. Mutating ops use one document for Invoke (a) and one for the CLI
// (b). Ids come from seedStreamUParity.
func streamUParityCases() map[string]parityCase {
	return map[string]parityCase{
		"task_assign":          {`{"id":"task-u-a","assignee":"person-u-seed"}`, []string{"task", "assign", "task-u-b", "person-u-seed"}, false},
		"incident_add":         {`{"title":"Cache outage","severity":"sev2","area":"area-u-seed"}`, []string{"incident", "add", "Cache", "outage", "--severity", "sev2", "--area", "area-u-seed"}, false},
		"incident_list":        {`{}`, []string{"incident", "list"}, true},
		"incident_show":        {`{"id":"incident-u-show"}`, []string{"incident", "show", "incident-u-show"}, true},
		"incident_note":        {`{"id":"incident-u-a","text":"Failed over."}`, []string{"incident", "note", "incident-u-b", "Failed", "over."}, false},
		"incident_status":      {`{"id":"incident-u-a","status":"mitigated"}`, []string{"incident", "status", "incident-u-b", "mitigated"}, false},
		"incident_link_add":    {`{"id":"incident-u-a","label":"Dashboard","kind":"dashboard","target":"https://grafana.acme.example/d/u"}`, []string{"incident", "link", "add", "incident-u-b", "Dashboard", "--kind", "dashboard", "--target", "https://grafana.acme.example/d/u"}, false},
		"incident_link_remove": {`{"id":"incident-u-c","label":"Runbook"}`, []string{"incident", "link", "remove", "incident-u-d", "Runbook"}, false},
		"waiting":              {`{"today":"2026-11-01"}`, []string{"waiting", "--today", "2026-11-01"}, false},
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
	for _, id := range []string{"incident-u-show", "incident-u-a", "incident-u-b", "incident-u-c", "incident-u-d"} {
		inc := &model.Incident{Common: model.Common{Type: model.TypeIncident}, Severity: model.Sev3, Status: model.IncidentOpen,
			Started: "2026-11-01T09:00:00Z", Area: model.LinkTo("area-u-seed")}
		if id > "incident-u-b" {
			inc.Links = []model.Pointer{{Label: "Runbook", Kind: model.PointerRunbook, Target: "~/doc/rb.md"}}
		}
		if _, err := deps.Store.Create(ctx, model.Document{ID: id, Folder: model.FolderIncidents, Title: "U incident " + id,
			Body: "## Timeline\n\n- 2026-11-01T09:00:00Z opened sev3", Meta: inc}); err != nil {
			t.Fatalf("seed %s: %v", id, err)
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
	e.json(&odd, "task", "add", "Renew certificate", "--area", area.ID, "--recur", "biennially")
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
	e.json(&proj, "project", "add", "--done-when", "Done.", "Move rack", "--owner", "Jordan Example")
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

func TestIncidentFlow(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	var area ops.AreaAddOutput
	e.json(&area, "area", "add", "Atlas service")
	e.ok("area", "link", "add", area.ID, "Runbook", "--kind", "runbook", "--target", "~/doc/atlas/runbook.md")

	if _, _, err := e.run("incident", "add", "No severity", "--area", area.ID); err == nil {
		t.Fatal("add without severity: want an error")
	}
	if _, _, err := e.run("incident", "add", "Bad time", "--severity", "sev1", "--started", "yesterday"); err == nil {
		t.Fatal("add with a bad time: want an error")
	}
	var minor, major ops.IncidentAddOutput
	e.json(&minor, "incident", "add", "Slow search", "--severity", "sev3", "--area", area.ID, "--started", "2026-11-01T08:00:00Z", "--body", "Search p99 is high.")
	e.json(&major, "incident", "add", "Atlas down", "--severity", "sev1", "--area", area.ID, "--started", "2026-11-01T09:00:00Z")
	// A new vault is at format 10: an incident is a task of kind incident in projects/.
	if !strings.HasPrefix(major.ID, "task-") || major.Folder != model.FolderProjects {
		t.Fatalf("add = %+v", major)
	}

	e.ok("incident", "note", major.ID, "Pager", "fired", "for", "zebrafish-latency", "--at", "2026-11-01T09:05:00Z")
	e.ok("incident", "note", major.ID, "Rolled back the 14:00 deploy")
	var sh ops.IncidentShowOutput
	e.json(&sh, "incident", "show", major.ID)
	var lines []string
	for _, l := range strings.Split(sh.Body, "\n") {
		if strings.HasPrefix(l, "- ") {
			lines = append(lines, l)
		}
	}
	if len(lines) != 3 || lines[0] != "- 2026-11-01T09:00:00Z opened sev1" || lines[1] != "- 2026-11-01T09:05:00Z Pager fired for zebrafish-latency" ||
		!strings.HasSuffix(lines[2], "Z Rolled back the 14:00 deploy") {
		t.Fatalf("timeline = %q\nbody:\n%s", lines, sh.Body)
	}
	if !strings.Contains(e.ok("incident", "show", minor.ID), "Search p99 is high.\n\n## Timeline\n\n- 2026-11-01T08:00:00Z opened sev3") {
		t.Fatalf("minor body:\n%s", e.ok("incident", "show", minor.ID))
	}

	// Open incidents are listed worst first; Today shows them before anything else.
	var list ops.IncidentListOutput
	e.json(&list, "incident", "list")
	if len(list.Incidents) != 2 || list.Incidents[0].ID != major.ID {
		t.Fatalf("list = %+v", list.Incidents)
	}
	var rep today.Report
	e.json(&rep, "today")
	v := rep.Vaults[0]
	if v.Incidents.Count != 2 || v.Incidents.Items[0].ID != major.ID || v.Incidents.Items[0].Detail != "sev1 open" {
		t.Fatalf("today incidents = %+v", v.Incidents)
	}
	out := e.ok("today")
	lines0 := strings.Split(out, "\n")
	if !strings.Contains(lines0[0], "incidents 2") || !strings.HasPrefix(lines0[1], "  incident\t"+major.ID+"\tsev1 open\t") {
		t.Fatalf("today:\n%s", out)
	}

	// Resolving warns when nothing follows the incident up.
	var res ops.IncidentChangeOutput
	e.json(&res, "incident", "status", major.ID, "mitigated", "--note", "rolled back")
	if res.Status != model.IncidentMitigated || res.Ended != "" || len(res.Warnings) != 0 {
		t.Fatalf("mitigated = %+v", res)
	}
	e.json(&res, "incident", "status", major.ID, "resolved")
	if res.Status != model.IncidentResolved || res.Ended == "" || len(res.Warnings) != 1 || res.FollowUps != 0 {
		t.Fatalf("resolved without follow-up = %+v", res)
	}
	if _, _, err := e.run("incident", "status", major.ID, "resolved"); err == nil {
		t.Fatal("resolve twice: want an error")
	}
	// Reopen clears the end; a follow-up task silences the warning.
	res = ops.IncidentChangeOutput{}
	e.json(&res, "incident", "status", major.ID, "open")
	if res.Ended != "" {
		t.Fatalf("reopened = %+v", res)
	}
	var fu ops.TaskAddOutput
	e.json(&fu, "task", "add", "Add a canary stage", "--incident", major.ID)
	var task ops.TaskShowOutput
	e.json(&task, "task", "show", fu.ID)
	if task.Task.Incident != major.ID || task.Task.Area != area.ID {
		t.Fatalf("follow-up = %+v", task.Task)
	}
	e.json(&res, "incident", "status", major.ID, "resolved", "--note", "canary task filed")
	if len(res.Warnings) != 0 || res.FollowUps != 1 {
		t.Fatalf("resolved with follow-up = %+v", res)
	}
	var tl ops.TaskListOutput
	e.json(&tl, "task", "list", "--incident", major.ID)
	if len(tl.Tasks) != 1 || tl.Tasks[0].ID != fu.ID {
		t.Fatalf("task list --incident = %+v", tl.Tasks)
	}
	if _, _, err := e.run("task", "add", "Orphan", "--incident", minor.ID); err != nil {
		t.Fatalf("follow-up in the incident area: %v", err)
	}

	// Resolved incidents leave the default list and Today, and stay searchable.
	e.json(&list, "incident", "list")
	if len(list.Incidents) != 1 || list.Incidents[0].ID != minor.ID {
		t.Fatalf("list after resolve = %+v", list.Incidents)
	}
	e.json(&list, "incident", "list", "--all")
	if len(list.Incidents) != 2 || list.Incidents[1].ID != major.ID {
		t.Fatalf("list --all = %+v", list.Incidents)
	}
	e.json(&rep, "today")
	if rep.Vaults[0].Incidents.Count != 1 {
		t.Fatalf("today after resolve = %+v", rep.Vaults[0].Incidents)
	}
	var hits ops.SearchOutput
	e.json(&hits, "search", "zebrafish-latency")
	if len(hits.Hits) == 0 || hits.Hits[0].ID != major.ID {
		t.Fatalf("search = %+v", hits.Hits)
	}
	e.ok("incident", "link", "add", major.ID, "Dashboard", "--kind", "dashboard", "--target", "https://grafana.acme.example/d/atlas")
	var lint ops.LintOutput
	e.json(&lint, "lint")
	if lint.Errors != 0 || lint.Warnings != 0 {
		t.Fatalf("lint = %+v", lint)
	}
}

// Every recur value and incident severity the guide shows is one para accepts.
func TestGuideStreamUExamples(t *testing.T) {
	var recurs, incidents int
	for _, text := range []string{guide.Extended(), guide.Orient(), guide.Bootstrap()} {
		for _, words := range guideCommands(text) {
			for i, w := range words {
				switch {
				case w == "--recur" && i+1 < len(words):
					recurs++
					if _, err := recur.Parse(words[i+1]); err != nil {
						t.Errorf("guide: %v", err)
					}
				case len(words) > 2 && words[1] == "incident" && words[2] == "add":
					incidents++
				}
			}
		}
	}
	if recurs < 3 || incidents < 1 {
		t.Errorf("guide shows %d recur values and %d incident add lines: want recurrence and incident examples", recurs, incidents)
	}
	text := strings.Join(strings.Fields(guide.Orient()), " ")
	for _, want := range []string{"alerts stay in monitoring", "task_assign", "the owner opens an incident", "incident_note", "`close` resolves it"} {
		if !strings.Contains(text, want) {
			t.Errorf("orient lacks %q", want)
		}
	}
	if !strings.Contains(strings.Join(strings.Fields(guide.Intro()), " "), "Alerts stay in monitoring") {
		t.Error("intro lacks the monitoring line")
	}
}
