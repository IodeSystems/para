package vault

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

// Below format 11 a goal is written as type project in projects/, and a
// task links it with project:; both read back in the format-11 form.
func TestLegacyGoalBelowFormat11(t *testing.T) {
	ctx := context.Background()
	v, root := newVault(t)
	g, err := v.Create(ctx, model.Document{Folder: model.FolderGoals, Title: "Ship X",
		Meta: &model.Goal{Common: model.Common{Type: model.TypeGoal}, Status: model.GoalActive, Owner: "self", DoneWhen: "It ships."}})
	if err != nil {
		t.Fatal(err)
	}
	if g.Folder != model.FolderProjects || !strings.HasPrefix(g.ID, "goal-") {
		t.Fatalf("goal %s in %s", g.ID, g.Folder)
	}
	if _, err := v.Create(ctx, model.Document{ID: "task-a", Folder: model.FolderGoals, Title: "Write it",
		Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskTodo, Goal: model.LinkTo(g.ID)}}); err != nil {
		t.Fatal(err)
	}
	raw := func(rel string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	if s := raw("projects/" + g.ID + ".md"); !strings.Contains(s, "type: project\n") {
		t.Errorf("goal file:\n%s", s)
	}
	if s := raw("projects/task-a.md"); !strings.Contains(s, "project: '[["+g.ID+"]]'") || strings.Contains(s, "goal:") {
		t.Errorf("task file:\n%s", s)
	}
	got, err := v.Get(ctx, g.ID)
	if err != nil {
		t.Fatal(err)
	}
	if m, ok := got.Meta.(*model.Goal); !ok || m.Type != model.TypeGoal || m.DoneWhen != "It ships." {
		t.Fatalf("read %T %+v", got.Meta, got.Meta)
	}
	td, err := v.Get(ctx, "task-a")
	if err != nil {
		t.Fatal(err)
	}
	if tm := td.Meta.(*model.Task); tm.Goal.Target() != g.ID || tm.Project != "" {
		t.Fatalf("task %+v", tm)
	}
	refs, err := v.List(ctx, "", model.TypeGoal)
	if err != nil || len(refs) != 1 || refs[0].ID != g.ID {
		t.Fatalf("list goals %+v %v", refs, err)
	}
	// An update keeps the old form.
	if _, err := v.Update(ctx, td); err != nil {
		t.Fatal(err)
	}
	if s := raw("projects/task-a.md"); strings.Contains(s, "goal:") {
		t.Errorf("task file after update:\n%s", s)
	}
	if fs, err := v.Check(ctx); err != nil {
		t.Fatal(err)
	} else {
		for _, f := range fs {
			if f.Severity == model.SeverityError {
				t.Errorf("lint: %s", f)
			}
		}
	}

	// Natively the same file is a project, as an upgrade step reads it.
	if nd, err := v.Native().Get(ctx, g.ID); err != nil {
		t.Fatal(err)
	} else if _, ok := nd.Meta.(*model.Project); !ok {
		t.Fatalf("native read %T", nd.Meta)
	}

	// At format 11 a goal is type goal in goals/ and type project is a project.
	if err := v.SetConfig(ctx, "format_version", strconv.Itoa(model.GoalsFormat)); err != nil {
		t.Fatal(err)
	}
	if nd, err := v.Get(ctx, g.ID); err != nil {
		t.Fatal(err)
	} else if _, ok := nd.Meta.(*model.Project); !ok {
		t.Fatalf("format 11 read %T", nd.Meta)
	}
	g2, err := v.Create(ctx, model.Document{Folder: model.FolderGoals, Title: "Ship Y",
		Meta: &model.Goal{Common: model.Common{Type: model.TypeGoal}, Status: model.GoalActive, Owner: "self"}})
	if err != nil {
		t.Fatal(err)
	}
	if s := raw("goals/" + g2.ID + ".md"); !strings.Contains(s, "type: goal\n") {
		t.Errorf("format 11 goal file:\n%s", s)
	}
}

// Step goals sets goals_step: the vault then reads natively, also while an
// earlier judgement step holds format_version below 11.
func TestGoalsStepKeyEndsLegacy(t *testing.T) {
	ctx := context.Background()
	v, root := newVault(t)
	if err := os.WriteFile(filepath.Join(root, "projects", "project-p.md"), []byte("---\ntype: project\ncreated: 2026-11-11\nstatus: active\n---\n\n# P\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if d, err := v.Get(ctx, "project-p"); err != nil || d.Meta.Header().Type != model.TypeGoal {
		t.Fatalf("below 11: %v %v", d.Meta, err)
	}
	if err := v.SetConfig(ctx, model.GoalsStepKey, "up-goals-1"); err != nil {
		t.Fatal(err)
	}
	if d, err := v.Get(ctx, "project-p"); err != nil || d.Meta.Header().Type != model.TypeProject {
		t.Fatalf("after step goals: %v %v", d.Meta, err)
	}
}
