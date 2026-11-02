package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/ops"
)

// streamSParityCases are the parity rows of stream S: areas, notes, due dates,
// dependency edits. Mutating ops use one document for Invoke (a) and one for
// the CLI (b). Ids come from seedStreamSParity.
func streamSParityCases() map[string]parityCase {
	return map[string]parityCase{
		"area_add":  {`{"title":"Health"}`, []string{"area", "add", "Money"}, false},
		"area_list": {`{}`, []string{"area", "list"}, true},
		"area_show": {`{"id":"area-s-seed"}`, []string{"area", "show", "area-s-seed"}, true},
		"note_add":  {`{"title":"Recipe","area":"area-s-seed","tags":["food"]}`, []string{"note", "add", "Recipe", "--area", "area-s-seed", "--tag", "food"}, false},
		"note_list": {`{"area":"area-s-seed"}`, []string{"note", "list", "--area", "area-s-seed"}, true},
		"note_show": {`{"id":"note-s-seed"}`, []string{"note", "show", "note-s-seed"}, true},
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
