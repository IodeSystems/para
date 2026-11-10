package cli

import (
	"context"
	"testing"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
)

// seedStreamTHParity: tasks to read and post a thread on.
func seedStreamTHParity(t *testing.T, deps ops.Deps) {
	t.Helper()
	for _, id := range []string{"task-th-a", "task-th-b", "task-th-c"} {
		if _, err := deps.Store.Create(context.Background(), model.Document{ID: id, Folder: model.FolderProjects, Title: "TH " + id,
			Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskTodo, Project: model.LinkTo("project-a")}}); err != nil {
			t.Fatal(err)
		}
	}
}

func streamTHParityCases() map[string]parityCase {
	return map[string]parityCase{
		"thread":      {`{"id":"task-th-a"}`, []string{"thread", "task-th-a"}, true},
		"thread_post": {`{"id":"task-th-b","text":"Why section B?","kind":"ask"}`, []string{"thread", "post", "task-th-c", "Why", "section", "B?", "--kind", "ask"}, false},
	}
}
