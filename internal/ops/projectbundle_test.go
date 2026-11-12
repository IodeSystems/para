package ops

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/paramd"
)

// format11 sets a synthetic vault to format 11, where projects exist.
func format11(t *testing.T, d Deps) {
	t.Helper()
	if err := d.Store.SetConfig(writeCtx(t), "format_version", strconv.Itoa(model.GoalsFormat)); err != nil {
		t.Fatal(err)
	}
}

func invoke(t *testing.T, e Entry, d Deps, in string) any {
	t.Helper()
	out, err := e.Invoke(writeCtx(t), d, json.RawMessage(in))
	if err != nil {
		t.Fatalf("%s %s: %v", e.Name(), in, err)
	}
	return out
}

// The project bundle: goals in flight with open tasks, what it waits on, the
// icebox, direct links, and history; the state line dates itself; links
// follow the rules (tasks only through a goal).
func TestProjectBundle(t *testing.T) {
	rv := newReqVault(t)
	cli := rv.as("user", "cli", "install-a")
	ctx := writeCtx(t)
	format11(t, cli)

	p := must(t, ProjectAdd, cli, ProjectAddInput{Title: "Garden", StateLine: "Beds dug."})
	g1 := must(t, GoalAdd, cli, GoalAddInput{Title: "Plant beds", DoneWhen: "Beds planted.", Project: p.ID, Due: "2026-12-01"})
	g2 := must(t, GoalAdd, cli, GoalAddInput{Title: "Build shed", DoneWhen: "Shed stands.", Project: p.ID})
	g3 := must(t, GoalAdd, cli, GoalAddInput{Title: "Pond", DoneWhen: "Pond holds water.", Project: p.ID})
	g4 := must(t, GoalAdd, cli, GoalAddInput{Title: "Fence", DoneWhen: "Fence up.", Project: p.ID})
	t1 := must(t, TaskAdd, cli, TaskAddInput{Goal: g1.ID, Title: "Buy seed", DoneWhen: "Seed bought."})
	t2 := must(t, TaskAdd, cli, TaskAddInput{Goal: g1.ID, Title: "Get compost", DoneWhen: "Compost here.", Due: "2026-11-05"})
	must(t, TaskBlock, cli, TaskBlockInput{ID: t2.ID, Reason: "supplier closed until Monday"})
	ask := must(t, RFXAdd, cli, RFXAddInput{Direction: model.RFXOut, Ask: "Can you lend the trailer", Goal: g2.ID, Route: "domain-x", SelfServe: "none found"})
	direct := must(t, RFXAdd, cli, RFXAddInput{Direction: model.RFXOut, Ask: "Is a permit needed", Project: p.ID, Route: "domain-x", SelfServe: "none found"})
	note := must(t, NoteAdd, cli, NoteAddInput{Title: "Soil test", Project: p.ID})

	// Park one goal (the icebox), close another (history).
	invoke(t, Edit, cli, `{"id":"`+g3.ID+`","fields":{"status":"parked","resume_when":"after the dry season"}}`)
	if _, err := Edit.Invoke(ctx, cli, json.RawMessage(`{"id":"`+g2.ID+`","fields":{"status":"parked"}}`)); err == nil || !strings.Contains(err.Error(), "resume_when") {
		t.Fatalf("parked with no resume_when: %v", err)
	}
	must(t, GoalClose, cli, GoalCloseInput{ID: g4.ID})

	// The state line: edit dates it today.
	rv.advance(48 * time.Hour)
	invoke(t, Edit, cli, `{"id":"`+p.ID+`","fields":{"state_line":"Seed ordered; compost late."}}`)
	doc, _ := cli.Store.Get(ctx, p.ID)
	if m := doc.Meta.(*model.Project); m.StateLine != "Seed ordered; compost late." || m.StateDate != "2026-11-03" {
		t.Fatalf("state = %q %s", m.StateLine, m.StateDate)
	}
	if _, err := Edit.Invoke(ctx, cli, json.RawMessage(`{"id":"`+p.ID+`","fields":{"state_date":"2026-01-01"}}`)); err == nil {
		t.Fatal("state_date set by hand")
	}

	sh := must(t, ProjectShow, cli, ProjectShowInput{ID: p.ID})
	if sh.Project.StateLine == "" || sh.StateAgeDays != 0 || sh.LastActivity != "2026-11-03" {
		t.Fatalf("header = %+v age %d last %s", sh.Project, sh.StateAgeDays, sh.LastActivity)
	}
	if len(sh.Goals) != 2 || sh.Goals[0].ID != g1.ID || sh.Goals[1].ID != g2.ID {
		t.Fatalf("goals = %+v", sh.Goals)
	}
	if g := sh.Goals[0]; len(g.Tasks) != 2 || g.Tasks[0].ID != t2.ID || g.Next != t1.ID {
		t.Fatalf("goal tasks = %+v next %s", g.Tasks, g.Next)
	}
	ids := func(items []BundleItem) []string {
		var out []string
		for _, it := range items {
			out = append(out, it.ID)
		}
		return out
	}
	if w := ids(sh.WaitsOn); len(w) != 3 || !slices.Contains(w, t2.ID) || !slices.Contains(w, ask.ID) || !slices.Contains(w, direct.ID) {
		t.Fatalf("waits_on = %+v", sh.WaitsOn)
	}
	if len(sh.Icebox) != 1 || sh.Icebox[0].ID != g3.ID || sh.Icebox[0].Note != "after the dry season" {
		t.Fatalf("icebox = %+v", sh.Icebox)
	}
	if d := ids(sh.Direct); len(d) != 1 || d[0] != note.ID {
		t.Fatalf("direct = %+v", sh.Direct)
	}
	if len(sh.History) == 0 || !slices.ContainsFunc(sh.History, func(e BundleEvent) bool { return e.ID == g4.ID && e.Kind == "goal_done" }) {
		t.Fatalf("history = %+v", sh.History)
	}
	if len(sh.More) != 0 {
		t.Fatalf("more = %v", sh.More)
	}
	b, _ := json.MarshalIndent(sh, "", " ")
	t.Logf("bundle (%d bytes):\n%s", bundleSize(&sh), b)

	// show {id} on a project is the bundle.
	so := invoke(t, Show, cli, `{"id":"`+p.ID+`"}`).(ShowOutput)
	if so.Op != "project_show" || so.Detail["waits_on"] == nil {
		t.Fatalf("show = %+v", so)
	}

	// The list: active projects by default; the icebox by status.
	p2 := must(t, ProjectAdd, cli, ProjectAddInput{Title: "Old car"})
	must(t, ProjectRetire, cli, ProjectRetireInput{ID: p2.ID})
	if ls := must(t, List, cli, ListInput{Type: model.TypeProject}); len(ls.Rows) != 1 || ls.Rows[0].ID != p.ID {
		t.Fatalf("list projects = %+v", ls.Rows)
	}
	if ls := must(t, List, cli, ListInput{Type: model.TypeProject, State: "all"}); len(ls.Rows) != 2 {
		t.Fatalf("list projects all = %+v", ls.Rows)
	}
	if ls := must(t, List, cli, ListInput{Type: model.TypeGoal, Status: "parked"}); len(ls.Rows) != 1 || ls.Rows[0].ID != g3.ID {
		t.Fatalf("icebox = %+v", ls.Rows)
	}

	// Links: notes, asks, obligations, and docs link a project directly; a
	// goal joins a project by part_of; a task does not.
	n2 := must(t, NoteAdd, cli, NoteAddInput{Title: "Shed plans"})
	invoke(t, Link, cli, `{"from":"`+n2.ID+`","to":"`+p.ID+`"}`)
	invoke(t, Link, cli, `{"from":"`+ask.ID+`","to":"`+p.ID+`","rel":"part_of"}`)
	g5 := must(t, GoalAdd, cli, GoalAddInput{Title: "Path", DoneWhen: "Path laid."})
	invoke(t, Link, cli, `{"from":"`+g5.ID+`","to":"`+p.ID+`"}`)
	for id, want := range map[string]string{n2.ID: p.ID, ask.ID: p.ID, g5.ID: p.ID} {
		d, _ := cli.Store.Get(ctx, id)
		got := ""
		if g, ok := d.Meta.(*model.Goal); ok {
			got = g.Project.Target()
		} else {
			_, got = linksOf(d.Meta)
		}
		if got != want {
			t.Errorf("%s project = %q", id, got)
		}
	}
	if _, err := Link.Invoke(ctx, cli, json.RawMessage(`{"from":"`+t1.ID+`","to":"`+p.ID+`","rel":"part_of"}`)); err == nil || !strings.Contains(err.Error(), "through a goal") {
		t.Fatalf("task part_of a project: %v", err)
	}
	if _, err := Edit.Invoke(ctx, cli, json.RawMessage(`{"id":"`+t1.ID+`","fields":{"project":"`+p.ID+`"}}`)); err == nil || !strings.Contains(err.Error(), "through a goal") {
		t.Fatalf("task edit project: %v", err)
	}
	if _, err := Add.Invoke(ctx, cli, json.RawMessage(`{"type":"task","title":"Dig","parent":"`+p.ID+`","fields":{"done_when":"Dug."}}`)); err == nil || !strings.Contains(err.Error(), "through a goal") {
		t.Fatalf("task add under a project: %v", err)
	}
	if _, err := Edit.Invoke(ctx, cli, json.RawMessage(`{"id":"`+ask.ID+`","fields":{"project":"`+g1.ID+`"}}`)); err == nil {
		t.Fatal("an Ask's project set to a goal")
	}
	invoke(t, Link, cli, `{"from":"`+p.ID+`","to":"https://example.test/garden","label":"Plan","kind":"plan"}`)
	doc, _ = cli.Store.Get(ctx, p.ID)
	if l := doc.Meta.(*model.Project).Links; len(l) != 1 || l[0].Target != "https://example.test/garden" {
		t.Fatalf("project links = %+v", l)
	}
}

// A big project is cut to projectBundleMax bytes: the header stays, each
// cut list says how many more and how to read them.
func TestProjectBundleCap(t *testing.T) {
	rv := newReqVault(t)
	cli := rv.as("user", "cli", "install-a")
	format11(t, cli)
	p := must(t, ProjectAdd, cli, ProjectAddInput{Title: "Big", StateLine: "Many things in flight."})
	for i := range 6 {
		g := must(t, GoalAdd, cli, GoalAddInput{Title: fmt.Sprintf("Goal number %d with a long enough title", i), DoneWhen: "It is done and checked by a person.", Project: p.ID})
		for j := range 8 {
			must(t, TaskAdd, cli, TaskAddInput{Goal: g.ID, Title: fmt.Sprintf("Task %d of goal %d, a task with a long title to fill bytes", j, i), DoneWhen: "Done."})
		}
	}
	for i := range 12 {
		must(t, NoteAdd, cli, NoteAddInput{Title: fmt.Sprintf("Reference note %d about the big project", i), Project: p.ID})
	}
	sh := must(t, ProjectShow, cli, ProjectShowInput{ID: p.ID})
	if n := bundleSize(&sh); n > projectBundleMax {
		t.Fatalf("bundle is %d bytes, cap %d", n, projectBundleMax)
	}
	if sh.Project.ID != p.ID || sh.Project.StateLine == "" {
		t.Fatalf("header cut: %+v", sh.Project)
	}
	if len(sh.More) == 0 || !slices.ContainsFunc(sh.More, func(s string) bool { return strings.HasPrefix(s, "tasks: ") && strings.Contains(s, " more: ") }) {
		t.Fatalf("more = %v", sh.More)
	}
	cut := 0
	for _, g := range sh.Goals {
		cut += g.MoreTasks
	}
	if cut == 0 {
		t.Fatalf("no goal says it has more tasks: %+v", sh.Goals)
	}
	t.Logf("capped: %d bytes, more %v", bundleSize(&sh), sh.More)
}

// Health: a parked goal with no resume_when, a project with no state line,
// and a stale one (one cadence, or 14 days with none).
func TestProjectHealth(t *testing.T) {
	rv := newReqVault(t)
	cli := rv.as("user", "cli", "install-a")
	ctx := writeCtx(t)
	format11(t, cli)
	bare := must(t, ProjectAdd, cli, ProjectAddInput{Title: "Bare"})
	fresh := must(t, ProjectAdd, cli, ProjectAddInput{Title: "Fresh", StateLine: "Going."})
	weekly := must(t, ProjectAdd, cli, ProjectAddInput{Title: "Weekly", StateLine: "Going."})
	invoke(t, Edit, cli, `{"id":"`+weekly.ID+`","fields":{"cadence":"weekly"}}`)
	g := must(t, GoalAdd, cli, GoalAddInput{Title: "Someday thing", DoneWhen: "Done.", Project: fresh.ID})
	doc, _ := cli.Store.Get(ctx, g.ID)
	doc.Meta.(*model.Goal).Status = model.GoalParked // as upgrade step goals leaves a someday goal with no reason
	if _, err := cli.Store.Update(ctx, doc); err != nil {
		t.Fatal(err)
	}
	kinds := func() map[string][]string {
		out := map[string][]string{}
		h := must(t, HealthOp, cli, HealthInput{})
		for _, gr := range h.Groups {
			for _, f := range gr.Findings {
				out[f.Kind] = append(out[f.Kind], f.ID)
			}
		}
		return out
	}
	k := kinds()
	if !slices.Contains(k[HealthGoalParkedNoResume], g.ID) || !slices.Contains(k[HealthProjectNoStateLine], bare.ID) || len(k[HealthProjectStateStale]) != 0 {
		t.Fatalf("day 0: %v", k)
	}
	rv.advance(8 * 24 * time.Hour)
	if k = kinds(); !slices.Equal(k[HealthProjectStateStale], []string{weekly.ID}) {
		t.Fatalf("day 8: %v", k[HealthProjectStateStale])
	}
	rv.advance(7 * 24 * time.Hour)
	if k = kinds(); len(k[HealthProjectStateStale]) != 2 {
		t.Fatalf("day 15: %v", k[HealthProjectStateStale])
	}
}

// Lint: a task with project: is refused (a task reaches a project through a
// goal); a parked goal with no resume_when is a warning; goal: and project:
// name their own type.
func TestProjectLint(t *testing.T) {
	task := paramd.Parse("goals/task-a.md", []byte("---\ntype: task\nstatus: todo\ncreated: 2026-11-01\nproject: \"[[project-a]]\"\n---\n# A\n"))
	if !slices.ContainsFunc(paramd.Lint(task), func(f paramd.Finding) bool {
		return f.Severity == paramd.Error && strings.Contains(f.Msg, "through a goal")
	}) {
		t.Fatalf("task project: %v", paramd.Lint(task))
	}
	goal := paramd.Parse("goals/goal-a.md", []byte("---\ntype: goal\nstatus: parked\nowner: self\ncreated: 2026-11-01\n---\n# A\n"))
	if !slices.ContainsFunc(paramd.Lint(goal), func(f paramd.Finding) bool {
		return f.Severity == paramd.Warn && strings.Contains(f.Msg, "resume_when")
	}) {
		t.Fatalf("parked goal: %v", paramd.Lint(goal))
	}
	note := paramd.Parse("resources/note-a.md", []byte("---\ntype: note\ncreated: 2026-11-01\nproject: \"[[goal-a]]\"\n---\n# A\n"))
	if !slices.ContainsFunc(paramd.CheckVault([]*paramd.File{goal, note}), func(f paramd.Finding) bool { return strings.Contains(f.Msg, "want a project") }) {
		t.Fatalf("note project names a goal: %v", paramd.CheckVault([]*paramd.File{goal, note}))
	}
}
