package cli

import (
	"context"
	"testing"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/ops"
)

func streamANParityCases() map[string]parityCase {
	return map[string]parityCase{
		"task_attempt": {`{"id":"call-an-a","result":"no_answer","retry":"2026-12-01"}`,
			[]string{"task", "attempt", "call-an-b", "--result", "no_answer", "--retry", "2026-12-01"}, false},
	}
}

func seedStreamANParity(t *testing.T, deps ops.Deps) {
	t.Helper()
	ctx := context.Background()
	for _, s := range []string{"a", "b"} {
		d := model.Document{ID: "call-an-" + s, Folder: model.FolderProjects, Title: "AN call " + s,
			Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskTodo, Kind: model.KindCall,
				Project: model.LinkTo("project-bg"), Scheduled: "2026-11-20"}}
		if _, err := deps.Store.Create(ctx, d); err != nil {
			t.Fatalf("seed %s: %v", d.ID, err)
		}
	}
}
