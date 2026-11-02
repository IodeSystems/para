package cli

import (
	"context"
	"testing"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/ops"
)

// streamBAParityCases are the parity rows of stream BA: para health.
func streamBAParityCases() map[string]parityCase {
	return map[string]parityCase{
		"health":           {`{}`, []string{"health"}, false},
		"health_dismiss":   {`{"id":"task-ba-d-a","kind":"task_no_done_when","reason":"Obvious."}`, []string{"health", "dismiss", "task-ba-d-b", "task_no_done_when", "--reason", "Obvious."}, false},
		"health_undismiss": {`{"id":"task-ba-u-a","kind":"task_no_done_when"}`, []string{"health", "undismiss", "task-ba-u-b", "task_no_done_when"}, false},
	}
}

func seedStreamBAParity(t *testing.T, deps ops.Deps) {
	t.Helper()
	ctx := context.Background()
	docs := []model.Document{
		{ID: "project-ba", Folder: model.FolderProjects, Title: "BA project",
			Meta: &model.Project{Common: model.Common{Type: model.TypeProject}, Status: model.ProjectActive, Owner: "Jordan Example"}},
	}
	for _, id := range []string{"task-ba-d-a", "task-ba-d-b"} {
		docs = append(docs, model.Document{ID: id, Folder: model.FolderProjects, Title: "BA " + id,
			Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskTodo, Project: model.LinkTo("project-ba")}})
	}
	for _, id := range []string{"task-ba-u-a", "task-ba-u-b"} {
		docs = append(docs, model.Document{ID: id, Folder: model.FolderProjects, Title: "BA " + id,
			Meta: &model.Task{Common: model.Common{Type: model.TypeTask, HealthDismissed: []model.HealthDismissal{{Kind: "task_no_done_when", Reason: "Obvious.", Date: "2026-11-01"}}},
				Status: model.TaskTodo, Project: model.LinkTo("project-ba")}})
	}
	for _, d := range docs {
		if _, err := deps.Store.Create(ctx, d); err != nil {
			t.Fatalf("seed %s: %v", d.ID, err)
		}
	}
}
