package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/ops"
)

// seedWork creates fixed-id documents the parity cases refer to. Mutating ops
// get two documents: one for Invoke, one for the CLI.
func seedWork(t *testing.T, d ops.Deps) {
	t.Helper()
	mk := func(id, title string, meta model.Meta) {
		t.Helper()
		if _, err := d.Store.Create(t.Context(), model.Document{ID: id, Folder: model.FolderProjects, Title: title, Meta: meta}); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{"project-a", "project-c1", "project-c2"} {
		mk(id, "Project "+id, &model.Project{Common: model.Common{Type: model.TypeProject}, Status: model.ProjectActive, Owner: "Jordan Example"})
	}
	for _, id := range []string{"task-s1", "task-s2", "task-d1", "task-d2", "task-d3", "task-b1", "task-b2"} {
		mk(id, "Task "+id, &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskTodo, Project: model.LinkTo("project-a")})
	}
}

func TestWorkFlow(t *testing.T) {
	e := newEnv(t)
	path := e.init("acme")

	var p ops.ProjectAddOutput
	e.json(&p, "project", "add", "Portal rewrite", "--owner", "Jordan Example")
	if !strings.HasPrefix(p.ID, "project-") || p.Folder != model.FolderProjects {
		t.Fatalf("project = %+v", p)
	}
	add := func(title string, deps ...string) string {
		args := []string{"task", "add", title, "-p", p.ID}
		for _, d := range deps {
			args = append(args, "--depends-on", d)
		}
		var o ops.TaskAddOutput
		e.json(&o, args...)
		return o.ID
	}
	a := add("design")
	b := add("build", a)
	c := add("ship", a, b)

	next := func() []string {
		var o ops.NextOutput
		e.json(&o, "next", p.ID)
		var ids []string
		for _, t := range o.Tasks {
			ids = append(ids, t.ID)
		}
		return ids
	}
	if got := next(); !reflect.DeepEqual(got, []string{a}) {
		t.Fatalf("next = %v", got)
	}
	if _, _, err := e.run("task", "start", b); err == nil || !strings.Contains(err.Error(), a) {
		t.Fatalf("start with open dependency: err = %v", err)
	}
	e.ok("task", "start", a)
	e.ok("task", "done", a)
	if got := next(); !reflect.DeepEqual(got, []string{b}) {
		t.Fatalf("next after done = %v", got)
	}

	e.ok("task", "block", b, "--reason", "Asha said wait for legal, acme only")
	var sh ops.TaskShowOutput
	e.json(&sh, "task", "show", b)
	if sh.Task.Status != model.TaskBlocked || !reflect.DeepEqual(sh.Task.BlockedBy, []string{"Asha said wait for legal, acme only"}) {
		t.Fatalf("show = %+v", sh.Task)
	}
	if got := next(); len(got) != 0 {
		t.Fatalf("next with blocked = %v", got)
	}

	var g ops.GraphOutput
	e.json(&g, "graph", p.ID)
	states := map[string]string{}
	for _, n := range g.Nodes {
		states[n.ID] = n.State
	}
	want := map[string]string{a: "done", b: "blocked", c: "waiting"}
	if !reflect.DeepEqual(states, want) || len(g.Edges) != 3 || len(g.Cycles) != 0 {
		t.Fatalf("graph = %+v", g)
	}
	if out := e.ok("graph", p.ID, "--mermaid"); !strings.HasPrefix(out, "flowchart TD\n") || !strings.Contains(out, "-->") {
		t.Fatalf("mermaid:\n%s", out)
	}

	e.ok("task", "start", b, "--force")
	e.ok("task", "done", b)
	e.ok("task", "done", c)
	if _, _, err := e.run("task", "done", c); err == nil {
		t.Fatal("done twice")
	}

	// Lint stays clean with depends_on in frontmatter.
	var lint ops.LintOutput
	e.json(&lint, "lint")
	if lint.Errors != 0 {
		t.Fatalf("lint = %+v", lint)
	}

	e.ok("project", "close", p.ID)
	if _, err := os.Stat(filepath.Join(path, "archive", p.ID+".md")); err != nil {
		t.Fatal(err)
	}
	var list ops.ProjectListOutput
	e.json(&list, "project", "list")
	if len(list.Projects) != 0 {
		t.Fatalf("active list = %+v", list)
	}
	e.json(&list, "project", "list", "--all")
	if len(list.Projects) != 1 || list.Projects[0].Status != model.ProjectDone || list.Projects[0].Folder != model.FolderArchive {
		t.Fatalf("all list = %+v", list)
	}
}

func TestProjectCloseRefusesOpenTasks(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	var p ops.ProjectAddOutput
	e.json(&p, "project", "add", "P", "--owner", "x")
	e.ok("task", "add", "open", "-p", p.ID)
	if _, _, err := e.run("project", "close", p.ID); err == nil || !strings.Contains(err.Error(), "open tasks") {
		t.Fatalf("err = %v", err)
	}
	e.ok("project", "close", p.ID, "--force", "--dropped")
}

func TestCycleIsReported(t *testing.T) {
	e := newEnv(t)
	path := e.init("acme")
	var p ops.ProjectAddOutput
	e.json(&p, "project", "add", "P", "--owner", "x")
	var x, y ops.TaskAddOutput
	e.json(&x, "task", "add", "x", "-p", p.ID)
	e.json(&y, "task", "add", "y", "-p", p.ID, "--depends-on", x.ID)
	// A hand edit closes the loop; add cannot.
	file := filepath.Join(path, "projects", x.ID+".md")
	raw, _ := os.ReadFile(file)
	edited := strings.Replace(string(raw), "project: ", "depends_on:\n  - \"[["+y.ID+"]]\"\nproject: ", 1)
	if err := os.WriteFile(file, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range []string{"graph", "next"} {
		out, _, err := e.run(cmd, p.ID, "--json")
		if err == nil || !strings.Contains(err.Error(), "dependency cycle") {
			t.Fatalf("%s: err = %v", cmd, err)
		}
		if !strings.Contains(out, x.ID) || !strings.Contains(out, `"cycles"`) {
			t.Fatalf("%s: stdout lacks cycles:\n%s", cmd, out)
		}
	}
}

func TestTaskAddRefusals(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	var p ops.ProjectAddOutput
	e.json(&p, "project", "add", "P", "--owner", "x")
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no project or area", []string{"task", "add", "t"}, "needs a project or an area"},
		{"unknown area", []string{"task", "add", "t", "--area", "area-nope"}, "not found"},
		{"project is not an area", []string{"task", "add", "t", "--area", p.ID}, "not an area"},
		{"unknown project", []string{"task", "add", "t", "-p", "project-nope"}, "not found"},
		{"project is not a task", []string{"task", "add", "t", "-p", p.ID, "--depends-on", p.ID}, "want a task or rfx"},
		{"unknown dependency", []string{"task", "add", "t", "-p", p.ID, "--depends-on", "task-nope"}, "not found"},
		{"block without reason", []string{"task", "block", "task-nope"}, "reason is empty"},
		{"bad status filter", []string{"task", "list", "--status", "nope"}, "unknown status"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, _, err := e.run(c.args...); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}
