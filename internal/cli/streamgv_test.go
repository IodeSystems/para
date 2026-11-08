package cli

import (
	"context"
	"testing"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
)

// seedStreamGVParity: tasks for the generic verbs, one per path (Invoke and
// the CLI), and two closed ones to reopen.
func seedStreamGVParity(t *testing.T, deps ops.Deps) {
	t.Helper()
	ctx := context.Background()
	docs := []model.Document{{ID: "project-gv-seed", Folder: model.FolderProjects, Title: "GV project",
		Meta: &model.Project{Common: model.Common{Type: model.TypeProject}, Status: model.ProjectActive, Owner: "Jordan Example"}}}
	task := func(id string, status model.TaskStatus) model.Document {
		return model.Document{ID: id, Folder: model.FolderProjects, Title: "Task " + id,
			Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: status, Project: model.LinkTo("project-gv-seed")}}
	}
	for _, id := range []string{"task-gv-close-a", "task-gv-close-b", "task-gv-later-a", "task-gv-later-b", "task-gv-send", "task-gv-bulk-a", "task-gv-bulk-b"} {
		docs = append(docs, task(id, model.TaskTodo))
	}
	docs = append(docs, task("task-gv-done-a", model.TaskDone), task("task-gv-done-b", model.TaskDone))
	// Outgoing drafts for io_approve (stage 3), one per path.
	for _, id := range []string{"io-gt-out-a", "io-gt-out-b"} {
		docs = append(docs, model.Document{ID: id, Folder: model.FolderIO, Title: "Draft " + id, Body: "Draft " + id,
			Meta: &model.IO{Common: model.Common{Type: model.TypeIO}, Direction: model.IOOut, Medium: model.MediumEmail, State: model.IODraft,
				To: []string{"[[person-seed]]"}, Expects: model.ExpectsNone}})
	}
	for _, doc := range docs {
		if _, err := deps.Store.Create(ctx, doc); err != nil {
			t.Fatalf("seed %s: %v", doc.ID, err)
		}
	}
}

func streamGVParityCases() map[string]parityCase {
	return map[string]parityCase{
		"close":         {`{"id":"task-gv-close-a","reason":"Sent."}`, []string{"close", "task-gv-close-b", "--reason", "Sent."}, false},
		"later":         {`{"id":"task-gv-later-a","until":"2999-01-01"}`, []string{"later", "task-gv-later-b", "--until", "2999-01-01"}, false},
		"reopen":        {`{"id":"task-gv-done-a","reason":"Not done."}`, []string{"reopen", "task-gv-done-b", "--reason", "Not done."}, false},
		"send_to_agent": {`{"id":"task-gv-send","note":"Find the owner."}`, []string{"send-to-agent", "task-gv-send", "Find the owner."}, false},
		"show":          {`{"id":"task-gv-send"}`, []string{"show", "task-gv-send"}, false},
		"list":          {`{"type":"project"}`, []string{"list", "project"}, true},
		"io_approve":    {`{"id":"io-gt-out-a"}`, []string{"io", "approve", "io-gt-out-b"}, false},
		"bulk":          {`{"verb":"close","ids":["task-gv-bulk-a"],"reason":"Sent."}`, []string{"bulk", "close", "task-gv-bulk-b", "--reason", "Sent."}, false},
	}
}
