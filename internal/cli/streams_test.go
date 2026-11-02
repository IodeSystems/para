package cli

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/ops"
	"github.com/iodesystems/parasolve/internal/today"
)

// streamSParityCases are the parity rows of stream S: areas, notes, due dates,
// dependency edits. Mutating ops use one document for Invoke (a) and one for
// the CLI (b). Ids come from seedStreamSParity.
func streamSParityCases() map[string]parityCase {
	return map[string]parityCase{
		"area_add":            {`{"title":"Health"}`, []string{"area", "add", "Money"}, false},
		"area_list":           {`{}`, []string{"area", "list"}, true},
		"area_show":           {`{"id":"area-s-seed"}`, []string{"area", "show", "area-s-seed"}, true},
		"note_add":            {`{"title":"Recipe","area":"area-s-seed","tags":["food"]}`, []string{"note", "add", "Recipe", "--area", "area-s-seed", "--tag", "food"}, false},
		"note_list":           {`{"area":"area-s-seed"}`, []string{"note", "list", "--area", "area-s-seed"}, true},
		"task_due":            {`{"id":"task-s-due-a","date":"2026-12-01","recur":"monthly"}`, []string{"task", "due", "task-s-due-b", "2026-12-01", "--recur", "monthly"}, false},
		"task_depends_add":    {`{"id":"task-s-due-a","depends_on":["task-s-due-b"]}`, []string{"task", "depends", "add", "task-s-dep-c", "task-s-due-b"}, false},
		"task_depends_remove": {`{"id":"task-s-dep-d","depends_on":["task-s-due-b"]}`, []string{"task", "depends", "remove", "task-s-dep-e", "task-s-due-b"}, false},
		"note_show":           {`{"id":"note-s-seed"}`, []string{"note", "show", "note-s-seed"}, true},
	}
}

// seedStreamSParity creates the fixed-id documents the rows above refer to.
func seedStreamSParity(t *testing.T, deps ops.Deps) {
	t.Helper()
	ctx := context.Background()
	for _, d := range []model.Document{
		{ID: "area-s-seed", Folder: model.FolderAreas, Title: "Seed area",
			Meta: &model.Area{Common: model.Common{Type: model.TypeArea}, Status: model.AreaActive}},
		{ID: "note-s-seed", Folder: model.FolderResources, Title: "Seed note", Body: "Seed text.",
			Meta: &model.Note{Common: model.Common{Type: model.TypeNote, Tags: []string{"food"}}, Area: model.LinkTo("area-s-seed")}},
		{ID: "task-s-due-a", Folder: model.FolderProjects, Title: "Due a",
			Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskTodo, Area: model.LinkTo("area-s-seed")}},
		{ID: "task-s-due-b", Folder: model.FolderProjects, Title: "Due b",
			Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskTodo, Area: model.LinkTo("area-s-seed")}},
		{ID: "task-s-dep-c", Folder: model.FolderProjects, Title: "Dep c",
			Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskTodo, Area: model.LinkTo("area-s-seed")}},
		{ID: "task-s-dep-d", Folder: model.FolderProjects, Title: "Dep d",
			Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskTodo, Area: model.LinkTo("area-s-seed"), DependsOn: []model.Link{model.LinkTo("task-s-due-b")}}},
		{ID: "task-s-dep-e", Folder: model.FolderProjects, Title: "Dep e",
			Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskTodo, Area: model.LinkTo("area-s-seed"), DependsOn: []model.Link{model.LinkTo("task-s-due-b")}}},
		{ID: "project-s-seed", Folder: model.FolderProjects, Title: "Seed project",
			Meta: &model.Project{Common: model.Common{Type: model.TypeProject}, Status: model.ProjectActive, Owner: "self", Area: model.LinkTo("area-s-seed")}},
	} {
		if _, err := deps.Store.Create(ctx, d); err != nil {
			t.Fatalf("seed %s: %v", d.ID, err)
		}
	}
}

func TestAreaCommands(t *testing.T) {
	e := newEnv(t)
	e.init("life")
	var a ops.AreaAddOutput
	e.json(&a, "area", "add", "Health", "--status", "paused")
	if a.ID != "area-health" || a.Folder != model.FolderAreas {
		t.Fatalf("area = %+v", a)
	}
	if _, _, err := e.run("area", "add", "Health"); err == nil {
		t.Error("duplicate area id must fail")
	}
	if _, _, err := e.run("area", "add", "x", "--status", "done"); err == nil {
		t.Error("bad status must fail")
	}
	if _, _, err := e.run("area", "show", "project-nope"); err == nil {
		t.Error("show of a missing id must fail")
	}
	var list ops.AreaListOutput
	e.json(&list, "area", "list")
	if len(list.Areas) != 1 || list.Areas[0].Status != model.AreaPaused {
		t.Fatalf("list = %+v", list)
	}
	var lint ops.LintOutput
	e.json(&lint, "lint")
	if lint.Errors != 0 {
		t.Fatalf("lint = %+v", lint)
	}
	if out := e.ok("area", "show", a.ID); !strings.HasPrefix(out, "area-health\tpaused\tHealth\n") {
		t.Fatalf("show = %q", out)
	}
}

func TestNoteCommands(t *testing.T) {
	e := newEnv(t)
	e.init("life")
	var a ops.AreaAddOutput
	e.json(&a, "area", "add", "Home")
	var p ops.ProjectAddOutput
	e.json(&p, "project", "add", "Move", "--owner", "self")
	var n ops.NoteAddOutput
	e.json(&n, "note", "add", "Paint colours", "--area", a.ID, "-p", p.ID, "--tag", "house", "--tag", "house", "--body", "Hall: grey.")
	if !strings.HasPrefix(n.ID, "note-") || n.Folder != model.FolderResources {
		t.Fatalf("note = %+v", n)
	}
	var bare ops.NoteAddOutput
	e.json(&bare, "note", "add", "Loose thought")
	if _, _, err := e.run("note", "add", "x", "--area", "project-nope"); err == nil {
		t.Error("unknown area must fail")
	}
	if _, _, err := e.run("note", "add", "x", "--area", p.ID); err == nil || !strings.Contains(err.Error(), "not an area") {
		t.Errorf("a project as area: err = %v", err)
	}
	var list ops.NoteListOutput
	e.json(&list, "note", "list")
	if len(list.Notes) != 2 {
		t.Fatalf("list = %+v", list)
	}
	e.json(&list, "note", "list", "--tag", "house")
	if len(list.Notes) != 1 || list.Notes[0].ID != n.ID || len(list.Notes[0].Tags) != 1 {
		t.Fatalf("tag list = %+v", list)
	}
	var show ops.AreaShowOutput
	e.json(&show, "area", "show", a.ID)
	if len(show.Notes) != 1 || show.Notes[0].ID != n.ID {
		t.Fatalf("area show notes = %+v", show.Notes)
	}
	var lint ops.LintOutput
	e.json(&lint, "lint")
	if lint.Errors != 0 || lint.Warnings != 0 {
		t.Fatalf("lint = %+v", lint)
	}
}

func TestTriageToNoteDocument(t *testing.T) {
	e := newEnv(t)
	e.init("life")
	var a ops.AreaAddOutput
	e.json(&a, "area", "add", "Home")
	var in ops.InboxAddOutput
	e.json(&in, "inbox", "add", "Plumber: Sam, 555-0100")
	var out ops.InboxTriageOutput
	e.json(&out, "inbox", "triage", in.ID, "--to", "note", "--area", a.ID)
	if !strings.HasPrefix(out.Created, "note-") || out.Folder != model.FolderArchive {
		t.Fatalf("out = %+v", out)
	}
	var show ops.NoteShowOutput
	e.json(&show, "note", "show", out.Created)
	if show.Note.Area != a.ID || show.Note.Folder != model.FolderResources {
		t.Fatalf("note = %+v", show.Note)
	}
	// With a person the old behaviour stays.
	e.ok("org", "person", "add", "Sam Example", "--org", "home")
	e.json(&in, "inbox", "add", "Prefers calls")
	e.json(&out, "inbox", "triage", in.ID, "--to", "note", "--person", "person-sam-example")
	if out.Created != "person-sam-example" {
		t.Fatalf("person note out = %+v", out)
	}
}

func TestTaskInAnArea(t *testing.T) {
	e := newEnv(t)
	path := e.init("life")
	var a ops.AreaAddOutput
	e.json(&a, "area", "add", "Home")
	// Owner default: the vault property, else self.
	var p1, p2 ops.ProjectAddOutput
	e.json(&p1, "project", "add", "Move")
	cfg := filepath.Join(path, ".para", "vault.properties")
	raw, err := os.ReadFile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, append(raw, "owner=Jordan Example\n"...), 0o600); err != nil {
		t.Fatal(err)
	}
	e.json(&p2, "project", "add", "Paint", "--area", a.ID)
	var projects ops.ProjectListOutput
	e.json(&projects, "project", "list")
	owners := map[string]string{}
	for _, p := range projects.Projects {
		owners[p.ID] = p.Owner
	}
	if owners[p1.ID] != "self" || owners[p2.ID] != "Jordan Example" {
		t.Fatalf("owners = %v", owners)
	}

	var own, inProject, both ops.TaskAddOutput
	e.json(&own, "task", "add", "Water plants", "--area", a.ID)
	// A dependency may sit in another project or area.
	e.json(&inProject, "task", "add", "Buy paint", "-p", p2.ID, "--depends-on", own.ID)
	e.json(&both, "task", "add", "Call landlord", "-p", p1.ID, "--area", a.ID, "--depends-on", inProject.ID)
	var sh ops.TaskShowOutput
	e.json(&sh, "task", "show", both.ID)
	if sh.Task.Project != p1.ID || sh.Task.Area != a.ID || len(sh.Task.DependsOn) != 1 {
		t.Fatalf("task = %+v", sh.Task)
	}
	e.json(&sh, "task", "show", own.ID)
	if sh.Task.Project != "" || sh.Task.Area != a.ID {
		t.Fatalf("task = %+v", sh.Task)
	}
	var list ops.TaskListOutput
	e.json(&list, "task", "list", "--area", a.ID)
	if len(list.Tasks) != 2 {
		t.Fatalf("area list = %+v", list)
	}
	var show ops.AreaShowOutput
	e.json(&show, "area", "show", a.ID)
	if len(show.Tasks) != 3 || len(show.Projects) != 1 {
		t.Fatalf("area show = %+v", show)
	}
	var next ops.NextOutput
	e.json(&next, "next")
	if len(next.Tasks) != 1 || next.Tasks[0].ID != own.ID {
		t.Fatalf("next = %+v", next)
	}
	var lint ops.LintOutput
	e.json(&lint, "lint")
	if lint.Errors != 0 {
		t.Fatalf("lint = %+v", lint)
	}
	e.ok("task", "done", own.ID)
	e.ok("project", "close", p2.ID, "--force")
}

func TestDueDates(t *testing.T) {
	e := newEnv(t)
	e.init("life")
	var a ops.AreaAddOutput
	e.json(&a, "area", "add", "Home")
	add := func(title string, args ...string) string {
		var o ops.TaskAddOutput
		e.json(&o, append([]string{"task", "add", title, "--area", a.ID}, args...)...)
		return o.ID
	}
	late := add("Renew passport", "--due", "2026-10-01")
	soon := add("Pay rent", "--due", "2026-11-05", "--recur", "monthly")
	far := add("Service car", "--due", "2027-03-01")
	none1 := add("Sort photos")
	none2 := add("Read book")
	mid := add("Dentist")
	e.ok("task", "due", mid, "2026-11-20")
	done := add("Old", "--due", "2026-09-01")
	e.ok("task", "done", done)

	for _, bad := range [][]string{
		{"task", "add", "x", "--area", a.ID, "--due", "11/05/2026"},
		{"task", "add", "x", "--area", a.ID, "--due", "2026-11-05T10:00"},
		{"task", "due", mid},
		{"task", "due", mid, "nope"},
		{"task", "due", mid, "2026-11-01", "--clear"},
		{"task", "due", done, "2026-12-01"},
	} {
		if _, _, err := e.run(bad...); err == nil {
			t.Errorf("para %v must fail", bad)
		}
	}

	var next ops.NextOutput
	e.json(&next, "next")
	var got []string
	for _, tk := range next.Tasks {
		got = append(got, tk.ID)
	}
	// Due first (earliest first), then undated by age: ids sort by creation.
	want := []string{late, soon, mid, far}
	if !reflect.DeepEqual(got[:4], want) || len(got) != 6 {
		t.Fatalf("next = %v, want prefix %v", got, want)
	}
	if got[4] != none1 || got[5] != none2 {
		t.Fatalf("undated order = %v, want %s then %s", got[4:], none1, none2)
	}

	var sh ops.TaskShowOutput
	e.json(&sh, "task", "show", soon)
	if sh.Task.Due != "2026-11-05" || sh.Task.Recur != "monthly" {
		t.Fatalf("task = %+v", sh.Task)
	}
	e.ok("task", "due", soon, "--clear")
	var cleared ops.TaskShowOutput
	e.json(&cleared, "task", "show", soon)
	if cleared.Task.Due != "" || cleared.Task.Recur != "" {
		t.Fatalf("cleared task = %+v", cleared.Task)
	}
	e.ok("task", "due", soon, "2026-11-05")

	var lint ops.LintOutput
	e.json(&lint, "lint")
	if lint.Errors != 0 {
		t.Fatalf("lint = %+v", lint)
	}
}

func TestTodayOverdueAndDueSoon(t *testing.T) {
	e := newEnv(t)
	e.init("life")
	var a ops.AreaAddOutput
	e.json(&a, "area", "add", "Home")
	day := func(n int) string { return time.Now().AddDate(0, 0, n).Format("2006-01-02") }
	add := func(title, due string) string {
		var o ops.TaskAddOutput
		e.json(&o, "task", "add", title, "--area", a.ID, "--due", due)
		return o.ID
	}
	late := add("Late", day(-3))
	later := add("Later", day(-1))
	edge := add("Due today", day(0))
	soon := add("Soon", day(DueSoonDaysForTest))
	add("Far", day(DueSoonDaysForTest+1))
	done := add("Done late", day(-5))
	e.ok("task", "done", done)

	var rep today.Report
	e.json(&rep, "today")
	v := rep.Vaults[0]
	ids := func(g today.Group) []string {
		out := []string{}
		for _, it := range g.Items {
			out = append(out, it.ID)
		}
		return out
	}
	if got := ids(v.Overdue); !reflect.DeepEqual(got, []string{late, later}) || v.Overdue.Count != 2 {
		t.Errorf("overdue = %v", got)
	}
	if got := ids(v.DueSoon); !reflect.DeepEqual(got, []string{edge, soon}) {
		t.Errorf("due soon = %v", got)
	}
	if out := e.ok("today"); !strings.Contains(out, "overdue 2\tsoon 2") || !strings.Contains(out, "  overdue\t"+late+"\t"+day(-3)) {
		t.Errorf("today = %q", out)
	}
}

// DueSoonDaysForTest mirrors today.DueSoonDays so a change to the window shows here.
const DueSoonDaysForTest = today.DueSoonDays

func TestEditDependencies(t *testing.T) {
	e := newEnv(t)
	e.init("life")
	var a, b ops.AreaAddOutput
	e.json(&a, "area", "add", "Home")
	e.json(&b, "area", "add", "Money")
	var p ops.ProjectAddOutput
	e.json(&p, "project", "add", "Move", "--area", a.ID)
	task := func(title string, args ...string) string {
		var o ops.TaskAddOutput
		e.json(&o, append([]string{"task", "add", title}, args...)...)
		return o.ID
	}
	one := task("Sign lease", "-p", p.ID)
	two := task("Book movers", "--area", a.ID)
	three := task("Pay deposit", "--area", b.ID)
	other := task("Unrelated", "--area", b.ID)

	// Edges cross projects and areas.
	var out ops.TaskDependsOutput
	e.json(&out, "task", "depends", "add", two, one)
	e.json(&out, "task", "depends", "add", three, two, one)
	if !reflect.DeepEqual(out.DependsOn, []string{two, one}) {
		t.Fatalf("depends = %v", out.DependsOn)
	}
	e.json(&out, "task", "depends", "add", three, one) // already there: no change
	if len(out.DependsOn) != 2 {
		t.Fatalf("repeat add = %v", out.DependsOn)
	}

	// Refusals change nothing.
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"task", "depends", "add", one, three}, "dependency cycle"},
		{[]string{"task", "depends", "add", one, one}, "itself"},
		{[]string{"task", "depends", "add", one, other, three}, "dependency cycle"},
		{[]string{"task", "depends", "add", one, "task-nope"}, "not found"},
		{[]string{"task", "depends", "add", one, p.ID}, "want a task or rfx"},
		{[]string{"task", "depends", "remove", one, two}, "does not depend"},
	} {
		if _, _, err := e.run(c.args...); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("para %v: err = %v, want %q", c.args, err, c.want)
		}
	}
	var sh ops.TaskShowOutput
	e.json(&sh, "task", "show", one)
	if len(sh.Task.DependsOn) != 0 {
		t.Fatalf("a refused add changed the task: %+v", sh.Task)
	}

	e.json(&out, "task", "depends", "remove", three, one)
	if !reflect.DeepEqual(out.DependsOn, []string{two}) {
		t.Fatalf("after remove = %v", out.DependsOn)
	}
	e.ok("task", "done", one)
	if _, _, err := e.run("task", "depends", "add", one, other); err == nil {
		t.Error("a done task takes no new dependency")
	}

	// graph: the whole vault with no arguments, or one area.
	var all, home, money ops.GraphOutput
	e.json(&all, "graph")
	e.json(&home, "graph", "--area", a.ID)
	e.json(&money, "graph", "--area", b.ID)
	ids := func(g ops.GraphOutput) []string {
		var out []string
		for _, n := range g.Nodes {
			out = append(out, n.ID)
		}
		sort.Strings(out)
		return out
	}
	if len(all.Nodes) != 4 {
		t.Errorf("whole vault = %v", ids(all))
	}
	// Home: its project's task and its own task; Money's tasks pull in what they wait on.
	if got := ids(home); !reflect.DeepEqual(got, sortedCopy(one, two)) {
		t.Errorf("home graph = %v", got)
	}
	if got := ids(money); !reflect.DeepEqual(got, sortedCopy(one, two, three, other)) {
		t.Errorf("money graph = %v", got)
	}
	if _, _, err := e.run("graph", p.ID, "--area", a.ID); err == nil {
		t.Error("project and area together must fail")
	}
	if _, _, err := e.run("graph", "--area", p.ID); err == nil {
		t.Error("a project as area must fail")
	}
	var lint ops.LintOutput
	e.json(&lint, "lint")
	if lint.Errors != 0 {
		t.Fatalf("lint = %+v", lint)
	}
}

func sortedCopy(ids ...string) []string {
	out := append([]string(nil), ids...)
	sort.Strings(out)
	return out
}
