package cli

import (
	"context"
	"testing"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
)

// streamBIParityCases are the parity rows of stream BI: resolve with an outcome,
// and give a task to an agent. Ids come from seedStreamBIParity.
func streamBIParityCases() map[string]parityCase {
	return map[string]parityCase{
		"task_resolve":  {`{"id":"task-bi-r-a","outcome":"partial","note":"Half done.","follow_ups":["Finish it"]}`, []string{"task", "resolve", "task-bi-r-b", "--outcome", "partial", "--note", "Half done.", "--follow-up", "Finish it"}, false},
		"task_split":    {`{"id":"task-bi-s-a","done":[{"title":"Draft it"}],"remaining":[{"title":"Review it"}],"reason":"Draft done."}`, []string{"task", "split", "task-bi-s-b", "--done", "Draft it", "--remaining", "Review it", "--reason", "Draft done."}, false},
		"task_update":   {`{"id":"task-bi-d-a","text":"Met Sam; he will review."}`, []string{"task", "update", "task-bi-d-b", "Met", "Sam;", "he", "will", "review."}, false},
		"task_delegate": {`{"id":"task-bi-d-a","agent":"scribe","hint":"start here"}`, []string{"task", "delegate", "task-bi-d-b", "--agent", "scribe", "--hint", "start here"}, false},
	}
}

func seedStreamBIParity(t *testing.T, deps ops.Deps) {
	t.Helper()
	docs := []model.Document{{ID: "project-bi", Folder: model.FolderProjects, Title: "BI project",
		Meta: &model.Goal{Common: model.Common{Type: model.TypeGoal}, Status: model.GoalActive, Owner: "Jordan Example"}}}
	for _, id := range []string{"task-bi-r-a", "task-bi-r-b", "task-bi-d-a", "task-bi-d-b", "task-bi-s-a", "task-bi-s-b"} {
		docs = append(docs, model.Document{ID: id, Folder: model.FolderProjects, Title: "BI " + id,
			Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskTodo, Goal: model.LinkTo("project-bi")}})
	}
	for _, d := range docs {
		if _, err := deps.Store.Create(context.Background(), d); err != nil {
			t.Fatalf("seed %s: %v", d.ID, err)
		}
	}
}
