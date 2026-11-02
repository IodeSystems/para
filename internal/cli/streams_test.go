package cli

import (
	"context"
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
	}
}

// seedStreamSParity creates the fixed-id documents the rows above refer to.
func seedStreamSParity(t *testing.T, deps ops.Deps) {
	t.Helper()
	ctx := context.Background()
	for _, d := range []model.Document{
		{ID: "area-s-seed", Folder: model.FolderAreas, Title: "Seed area",
			Meta: &model.Area{Common: model.Common{Type: model.TypeArea}, Status: model.AreaActive}},
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
