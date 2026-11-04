package upgrade

import (
	"testing"

	"github.com/iodesystems/para/internal/model"
)

// The proposal-dossier step rewrites an open old-form proposal as a dossier
// with the same items, and leaves superseded revisions alone.
func TestProposalDossierStep(t *testing.T) {
	var step Step
	for _, s := range steps() {
		if s.ID == "proposal-dossier" {
			step = s
		}
	}
	body, err := model.RenderProposalBody(model.ProposalContent{TLDR: "Send it", Items: []model.ProposalItem{
		{ID: "i1", Kind: model.ItemMessage, People: []string{"ada@acme.example"}, Body: "Hi.", Evidence: model.Evidence{Quote: "q", Locator: "l"}, State: model.ItemPending}}})
	if err != nil {
		t.Fatal(err)
	}
	doc := model.Document{ID: "proposal-a", Title: "Plan", Body: body, Meta: &model.Proposal{Common: model.Common{Type: model.TypeProposal}, Status: model.ProposalOpen, Revision: 2}}
	if !step.Select(doc) {
		t.Fatal("open old-form proposal not selected")
	}
	old := doc
	old.Meta = &model.Proposal{Common: model.Common{Type: model.TypeProposal}, Status: model.ProposalSuperseded, Revision: 1}
	if step.Select(old) {
		t.Fatal("superseded revision selected")
	}
	got, err := step.Rewrite(doc)
	if err != nil {
		t.Fatal(err)
	}
	if !model.IsDossier(got.Body) || step.Select(got) {
		t.Fatalf("not a dossier:\n%s", got.Body)
	}
	c, err := model.ParseProposalBody(got.Body)
	if err != nil || len(c.Items) != 1 || c.Items[0].Body != "Hi." || c.TLDR != "Send it" {
		t.Fatalf("content = %+v %v", c, err)
	}
}
