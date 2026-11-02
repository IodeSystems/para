package cli

import (
	"context"
	"testing"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/ops"
)

// streamAIParityCases are the parity rows of stream AI: level-of-detail
// journals. Ids come from seedStreamAIParity.
func streamAIParityCases() map[string]parityCase {
	return map[string]parityCase{
		"journal_list":    {`{"level":"day"}`, []string{"journal", "list", "--level", "day"}, false},
		"journal_show":    {`{"id":"2026-10-20"}`, []string{"journal", "show", "2026-10-20"}, true},
		"journal_summary": {`{"id":"2026-10-21","text":"Moved the sample project."}`, []string{"journal", "summary", "2026-10-22", "--text", "Moved the sample project."}, false},
	}
}

func seedStreamAIParity(t *testing.T, deps ops.Deps) {
	t.Helper()
	for _, id := range []string{"2026-10-20", "2026-10-21", "2026-10-22"} {
		_, err := deps.Store.Create(context.Background(), model.Document{
			ID: id, Folder: model.FolderJournal, Title: "Day " + id,
			Body: "%% para:begin summary %%\nevents: 0\n%% para:end summary %%\n\n%% para:begin activity %%\nNo activity.\n%% para:end activity %%",
			Meta: &model.Journal{Common: model.Common{Type: model.TypeJournal}, Date: model.Date(id), Scope: model.JournalDay},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
