package cli

import (
	"context"
	"testing"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
)

// streamAZParityCases are the parity rows of stream AZ: Later, find evidence,
// cutting an edge, and the value order. Ids come from seedStreamAZParity.
func streamAZParityCases() map[string]parityCase {
	return map[string]parityCase{
		"defer":       {`{"ids":["task-az-d-a"],"until":"2999-01-01","reason":"After the audit."}`, []string{"defer", "task-az-d-b", "--until", "2999-01-01", "--reason", "After the audit."}, false},
		"undefer":     {`{"ids":["task-az-u-a"]}`, []string{"undefer", "task-az-u-b"}, false},
		"depends_cut": {`{"id":"task-az-c-a","dep":"question-az-c","reason":"Assume the default."}`, []string{"depends", "cut", "task-az-c-b", "question-az-c", "--reason", "Assume the default."}, false},
		"investigate": {`{"id":"question-az-i-a","hint":"check the wiki"}`, []string{"investigate", "question-az-i-b", "--hint", "check the wiki"}, false},
		"order":       {`{}`, []string{"order"}, false},
	}
}

func seedStreamAZParity(t *testing.T, deps ops.Deps) {
	t.Helper()
	ctx := context.Background()
	docs := []model.Document{
		{ID: "project-az", Folder: model.FolderProjects, Title: "AZ project",
			Meta: &model.Project{Common: model.Common{Type: model.TypeProject}, Status: model.ProjectActive, Owner: "Jordan Example"}},
		{ID: "question-az-c", Folder: model.FolderQuestions, Title: "AZ blocker",
			Meta: &model.Question{Common: model.Common{Type: model.TypeQuestion}, Status: model.QuestionOpen, Scope: model.LinkTo("project-az")}},
	}
	for _, id := range []string{"question-az-i-a", "question-az-i-b"} {
		docs = append(docs, model.Document{ID: id, Folder: model.FolderQuestions, Title: "AZ " + id,
			Meta: &model.Question{Common: model.Common{Type: model.TypeQuestion}, Status: model.QuestionOpen, Scope: model.LinkTo("project-az")}})
	}
	for _, id := range []string{"task-az-d-a", "task-az-d-b"} {
		docs = append(docs, model.Document{ID: id, Folder: model.FolderProjects, Title: "AZ " + id,
			Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskTodo, Project: model.LinkTo("project-az")}})
	}
	for _, id := range []string{"task-az-u-a", "task-az-u-b"} {
		docs = append(docs, model.Document{ID: id, Folder: model.FolderProjects, Title: "AZ " + id,
			Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskTodo, Project: model.LinkTo("project-az"),
				Deferral: model.Deferral{DeferUntil: model.SomedayWord}}})
	}
	for _, id := range []string{"task-az-c-a", "task-az-c-b"} {
		docs = append(docs, model.Document{ID: id, Folder: model.FolderProjects, Title: "AZ " + id,
			Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskTodo, Project: model.LinkTo("project-az"),
				DependsOn: []model.Link{model.LinkTo("question-az-c")}}})
	}
	for _, d := range docs {
		if _, err := deps.Store.Create(ctx, d); err != nil {
			t.Fatalf("seed %s: %v", d.ID, err)
		}
	}
}
