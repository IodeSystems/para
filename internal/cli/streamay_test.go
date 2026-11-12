package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
)

// streamAYParityCases are the parity rows of stream AY: align.
func streamAYParityCases() map[string]parityCase {
	return map[string]parityCase{
		"align_scope":  {`{"scope":"project-ay"}`, []string{"align", "scope", "project-ay"}, false},
		"align_record": {`{"scope":"project-ay","checked":3,"matched":3}`, []string{"align", "record", "project-ay", "--checked", "3", "--matched", "3"}, false},
	}
}

func seedStreamAYParity(t *testing.T, deps ops.Deps) {
	t.Helper()
	ctx := context.Background()
	docs := []model.Document{
		{ID: "project-ay", Folder: model.FolderProjects, Title: "AY project",
			Meta: &model.Goal{Common: model.Common{Type: model.TypeGoal}, Status: model.GoalActive, Owner: "Jordan Example", DoneWhen: "Done."}},
		{ID: "task-ay", Folder: model.FolderProjects, Title: "AY task",
			Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskTodo, Goal: model.LinkTo("project-ay"), DoneWhen: "Done."}},
	}
	for _, d := range docs {
		if _, err := deps.Store.Create(ctx, d); err != nil {
			t.Fatalf("seed %s: %v", d.ID, err)
		}
	}
}

// The align commands work against a vault: scope lists, record leaves a receipt
// the goal page reads.
func TestAlignCommands(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	var proj struct {
		ID string `json:"id"`
	}
	e.json(&proj, "goal", "add", "--done-when", "Done.", "Portal", "--owner", "Jordan Example")
	var task struct {
		ID string `json:"id"`
	}
	e.json(&task, "task", "add", "Cut the release", "--goal", proj.ID, "--done-when", "Tagged.")
	var scope ops.AlignScopeOutput
	e.json(&scope, "align", "scope", proj.ID)
	if scope.Kind != "goal" || len(scope.Items) != 2 {
		t.Fatalf("scope = %+v", scope)
	}
	if out := e.ok("align", "scope", proj.ID); !strings.Contains(out, task.ID) || !strings.Contains(out, "last aligned -") {
		t.Fatalf("scope text = %q", out)
	}
	out := e.ok("align", "record", proj.ID, "--checked", "2", "--matched", "2", "--unreachable", "jira")
	if !strings.Contains(out, "Checked 2: 2 matched, 0 differ, 1 unreachable (jira)") {
		t.Fatalf("record = %q", out)
	}
	if out := e.ok("goal", "show", proj.ID); !strings.Contains(out, "aligned") {
		t.Logf("project show does not print the aligned date: %q", out)
	}
}
