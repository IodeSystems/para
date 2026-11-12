package cli

import (
	"context"
	"testing"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
)

// streamBBParityCases are the parity rows of stream BB: goal validation.
// Ids come from seedStreamBBParity.
func streamBBParityCases() map[string]parityCase {
	return map[string]parityCase{
		"goal_signal_add": {`{"id":"project-bb","what":"Pilots","value":"2","source":"note-bb","date":"2026-11-02"}`, []string{"goal", "signal", "add", "project-bb", "Pilots", "--value", "2", "--source", "note-bb", "--date", "2026-11-02"}, false},
		"goal_rank":       {`{"id":"project-bb","rank":1}`, []string{"goal", "rank", "project-bb", "1"}, true},
		"goal_compare":    {`{"ids":"project-bb"}`, []string{"goal", "compare", "project-bb"}, false},
	}
}

func seedStreamBBParity(t *testing.T, deps ops.Deps) {
	t.Helper()
	d := model.Document{ID: "project-bb", Folder: model.FolderProjects, Title: "BB goal",
		Meta: &model.Goal{Common: model.Common{Type: model.TypeGoal}, Status: model.GoalActive, Owner: "Jordan Example",
			GoalBrief: model.GoalBrief{Signals: []model.Signal{{What: "Pilots", How: "Count them", EveryDays: 14, Expected: "3"}}}}}
	if _, err := deps.Store.Create(context.Background(), d); err != nil {
		t.Fatalf("seed %s: %v", d.ID, err)
	}
}
