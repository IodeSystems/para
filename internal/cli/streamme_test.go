package cli

import (
	"context"
	"testing"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
)

// seedStreamMEParity: notes to merge and archive, and tasks to write notes
// on. Each case runs twice, so each write has its own pair of documents.
func seedStreamMEParity(t *testing.T, deps ops.Deps) {
	t.Helper()
	ctx := context.Background()
	var docs []model.Document
	for _, id := range []string{"note-me-keep", "note-me-a", "note-me-b", "note-me-c", "note-me-d"} {
		docs = append(docs, model.Document{ID: id, Folder: model.FolderResources, Title: "ME " + id, Body: "Text of " + id + ".",
			Meta: &model.Note{Common: model.Common{Type: model.TypeNote}}})
	}
	for _, id := range []string{"task-me-a", "task-me-b", "task-me-c", "task-me-d"} {
		docs = append(docs, model.Document{ID: id, Folder: model.FolderProjects, Title: "ME " + id,
			Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskTodo, Project: model.LinkTo("project-me-seed")}})
	}
	docs = append(docs, model.Document{ID: "project-me-seed", Folder: model.FolderProjects, Title: "ME project",
		Meta: &model.Project{Common: model.Common{Type: model.TypeProject}, Status: model.ProjectActive, Owner: "Jordan Example"}})
	for _, doc := range docs {
		if _, err := deps.Store.Create(ctx, doc); err != nil {
			t.Fatalf("seed %s: %v", doc.ID, err)
		}
	}
}

func streamMEParityCases() map[string]parityCase {
	return map[string]parityCase{
		"doc_merge":   {`{"keep":"note-me-keep","alias":"note-me-a"}`, []string{"doc", "merge", "note-me-keep", "note-me-b"}, false},
		"doc_archive": {`{"id":"note-me-c","reason":"Old."}`, []string{"doc", "archive", "note-me-d", "--reason", "Old."}, false},
		"doc_merges":  {`{"id":"note-me-keep"}`, []string{"doc", "merges", "note-me-keep"}, true},
		"notes_read":  {`{"id":"task-me-a"}`, []string{"notes", "read", "task-me-a"}, true},
		"notes_edit":  {`{"id":"task-me-a","md":"A."}`, []string{"notes", "edit", "task-me-b", "--md", "B."}, false},
		"notes_save":  {`{"id":"task-me-c","md":"C."}`, []string{"notes", "save", "task-me-d", "--md", "D."}, false},
	}
}
