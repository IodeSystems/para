package ops

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

// From format 11 a project is the ongoing project: add, show, list, and
// close {outcome: retired} work through the verbs; a goal names its project.
func TestOngoingProject(t *testing.T) {
	rv := newReqVault(t)
	cli := rv.as("user", "cli", "install-a")
	ctx := writeCtx(t)

	// Below format 11 every project is a goal.
	if _, err := ProjectAdd.Run(ctx, cli, ProjectAddInput{Title: "Home"}); err == nil || !strings.Contains(err.Error(), "below format 11") {
		t.Fatalf("below 11: %v", err)
	}
	if err := cli.Store.SetConfig(ctx, "format_version", strconv.Itoa(model.GoalsFormat)); err != nil {
		t.Fatal(err)
	}
	if _, err := ProjectAdd.Run(ctx, cli, ProjectAddInput{Title: "Home", DoneWhen: "Done."}); err == nil || !strings.Contains(err.Error(), "goal_add") {
		t.Fatalf("done_when on a project: %v", err)
	}
	got, err := Add.Invoke(ctx, cli, json.RawMessage(`{"type":"project","title":"Home","fields":{"state_line":"Roof first."}}`))
	if err != nil {
		t.Fatal(err)
	}
	p := got.(AddOutput)
	if p.Type != model.TypeProject || p.Op != "project_add" {
		t.Fatalf("add project = %+v", p)
	}
	doc, err := cli.Store.Get(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if m, ok := doc.Meta.(*model.Project); !ok || doc.Folder != model.FolderProjects || m.StateLine != "Roof first." || m.StateDate == "" || !m.Active() {
		t.Fatalf("project = %s %+v", doc.Folder, doc.Meta)
	}
	gr, err := Add.Invoke(ctx, cli, json.RawMessage(`{"type":"goal","title":"Fix the roof","parent":"`+p.ID+`","fields":{"done_when":"No leak."}}`))
	if err != nil {
		t.Fatal(err)
	}
	g := gr.(AddOutput)
	gd, err := cli.Store.Get(ctx, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if m, ok := gd.Meta.(*model.Goal); !ok || gd.Folder != model.FolderGoals || m.Project.Target() != p.ID || !strings.HasPrefix(g.ID, "goal-") {
		t.Fatalf("goal = %s %s %+v", g.ID, gd.Folder, gd.Meta)
	}
	sh := must(t, ProjectShow, cli, ProjectShowInput{ID: p.ID})
	if len(sh.Goals) != 1 || sh.Goals[0].ID != g.ID {
		t.Fatalf("show = %+v", sh)
	}
	if _, err := ProjectShow.Run(ctx, cli, ProjectShowInput{ID: g.ID}); err == nil || !strings.Contains(err.Error(), "a goal since 0.6.0") {
		t.Fatalf("project_show on a goal: %v", err)
	}
	ls := must(t, List, cli, ListInput{Type: model.TypeProject})
	if len(ls.Rows) != 1 || ls.Rows[0].ID != p.ID || ls.Rows[0].Status != string(model.ProjectActive) {
		t.Fatalf("list = %+v", ls)
	}
	if _, err := Close.Invoke(ctx, cli, json.RawMessage(`{"id":"`+p.ID+`","outcome":"done"}`)); err == nil {
		t.Fatal("a project closed as done")
	}
	if _, err := Close.Invoke(ctx, cli, json.RawMessage(`{"id":"`+p.ID+`","outcome":"retired"}`)); err != nil {
		t.Fatal(err)
	}
	if doc, _ := cli.Store.Get(ctx, p.ID); doc.Meta.(*model.Project).Status != model.ProjectRetired {
		t.Fatalf("retired = %+v", doc.Meta)
	}
}

// The project ops that meant a goal are removed in 0.6.0 and say what to use.
func TestProjectOpsRemovedIn060(t *testing.T) {
	for _, name := range []string{"project_edit", "project_close", "project_list", "project_link_add", "project_link_remove"} {
		v, use, ok := RemovedIn(name)
		if !ok || v != "0.6.0" || use == "" {
			t.Errorf("%s: %q %q %v", name, v, use, ok)
		}
		if _, ok := Lookup(name); ok {
			t.Errorf("%s is still defined", name)
		}
	}
	for _, name := range []string{"goal_add", "goal_list", "goal_show", "goal_close", "goal_edit", "goal_link_add", "goal_link_remove", "project_add", "project_show", "project_retire"} {
		if _, ok := Lookup(name); !ok {
			t.Errorf("%s is not defined", name)
		}
	}
}
