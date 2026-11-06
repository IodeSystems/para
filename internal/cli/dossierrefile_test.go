package cli

import (
	"context"
	"testing"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
)

// seedRefileParity makes two dossiers with a staged change each, one for
// the JSON call and one for the CLI.
func seedRefileParity(t *testing.T, deps ops.Deps) {
	t.Helper()
	ev := model.Evidence{Quote: "Sample text.", Locator: "https://acme.example/line-1"}
	for _, id := range []string{"proposal-rf1", "proposal-rf2"} {
		body, err := model.NewDossier(model.DossierHeader{TLDR: "Staged"}, "Items", []model.ProposalItem{
			{ID: "c1", Kind: model.ItemPara, Op: "task_add", Input: map[string]any{"title": "Old"}, Evidence: ev, State: model.ItemPending},
		}).Render()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := deps.Store.Create(context.Background(), model.Document{ID: id, Folder: model.FolderProposals, Title: "Staged", Body: body,
			Meta: &model.Proposal{Common: model.Common{Type: model.TypeProposal}, Status: model.ProposalOpen, Revision: 1, ProposedBy: "agent:x"}}); err != nil {
			t.Fatal(err)
		}
	}
}

func refileParityCases() map[string]parityCase {
	return map[string]parityCase{
		"dossier_refile": {`{"proposal":"proposal-rf1","item":"c1","dropped":"Not needed."}`, []string{"dossier", "refile", "proposal-rf2", "c1", "--dropped", "Not needed."}, false},
	}
}
