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
			Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskTodo, Goal: model.LinkTo("project-a")}}); err != nil {
			t.Fatal(err)
		}
	}
}

// seedStreamTHTurns: owner turns to edit and withdraw. They are proposed, not queued, so
// request_next does not claim them.
func seedStreamTHTurns(t *testing.T, deps ops.Deps) {
	t.Helper()
	for _, id := range []string{"request-th-e1", "request-th-e2", "request-th-w1", "request-th-w2"} {
		if _, err := deps.Store.Create(context.Background(), model.Document{ID: id, Folder: model.FolderRequests, Title: "Owner turn " + id,
			Body: "The owner wrote a turn in the thread task-th-a.\n\nKind: ask: answer.\n\n## Owner's words\n\nWhy section B?",
			Meta: &model.Request{Common: model.Common{Type: model.TypeRequest}, Status: model.RequestProposed, Runner: deps.Store.Info().Install,
				Kind: ops.SentKind, Thread: "task-th-a", TurnKind: model.RequestTurnAsk, Queued: "2026-11-01T09:00:00Z"}}); err != nil {
			t.Fatal(err)
		}
	}
}

func streamTHParityCases() map[string]parityCase {
	return map[string]parityCase{
		"thread":          {`{"id":"task-th-a"}`, []string{"thread", "task-th-a"}, true},
		"thread_post":     {`{"id":"task-th-b","text":"Why section B?","kind":"ask"}`, []string{"thread", "post", "task-th-c", "Why", "section", "B?", "--kind", "ask"}, false},
		"thread_edit":     {`{"id":"request-th-e1","text":"Why is B late?","kind":"redirect"}`, []string{"thread", "edit", "request-th-e2", "Why", "is", "B", "late?", "--kind", "redirect"}, false},
		"thread_withdraw": {`{"id":"request-th-w1"}`, []string{"thread", "withdraw", "request-th-w2"}, false},
	}
}
