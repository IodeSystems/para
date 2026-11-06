package cli

import (
	"context"
	"testing"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
)

// seedStreamOCParity: open questions to review, a dossier with actions the
// owner did, and outbox drafts the owner sent. Each case runs twice.
func seedStreamOCParity(t *testing.T, deps ops.Deps) {
	t.Helper()
	ctx := context.Background()
	docs := []model.Document{{ID: "project-oc-seed", Folder: model.FolderProjects, Title: "OC project",
		Meta: &model.Project{Common: model.Common{Type: model.TypeProject}, Status: model.ProjectActive, Owner: "Jordan Example"}}}
	for _, id := range []string{"question-oc-a", "question-oc-b"} {
		docs = append(docs, model.Document{ID: id, Folder: model.FolderQuestions, Title: "Q " + id,
			Meta: &model.Question{Common: model.Common{Type: model.TypeQuestion}, Status: model.QuestionOpen, Scope: model.LinkTo("project-oc-seed")}})
	}
	for _, id := range []string{"io-oc-a", "io-oc-b"} {
		docs = append(docs, model.Document{ID: id, Folder: model.FolderIO, Title: "Draft " + id, Body: "Draft " + id,
			Meta: &model.IO{Common: model.Common{Type: model.TypeIO}, Direction: model.IOOut, Medium: model.MediumEmail, State: model.IODraft,
				To: []string{"[[person-seed]]"}, Expects: model.ExpectsNone}})
	}
	ev := model.Evidence{Quote: "OC source", Locator: "https://acme.example/title"}
	act := func(id string) model.ProposalItem {
		return model.ProposalItem{ID: id, Kind: model.ItemAction, What: "Book room " + id, How: "calendar", Evidence: ev, State: model.ItemPending, Rev: 1}
	}
	body, err := model.NewDossier(model.DossierHeader{TLDR: "OC parity"}, "Items", []model.ProposalItem{act("a1"), act("a2")}).Render()
	if err != nil {
		t.Fatal(err)
	}
	docs = append(docs, model.Document{ID: "proposal-oc", Folder: model.FolderProposals, Title: "OC parity", Body: body,
		Meta: &model.Proposal{Common: model.Common{Type: model.TypeProposal}, Status: model.ProposalOpen, Revision: 1, ProposedBy: "user"}})
	for _, doc := range docs {
		if _, err := deps.Store.Create(ctx, doc); err != nil {
			t.Fatalf("seed %s: %v", doc.ID, err)
		}
	}
}

func streamOCParityCases() map[string]parityCase {
	return map[string]parityCase{
		"question_review": {`{"id":"question-oc-a","decision":"skip","note":"Later."}`, []string{"question", "review", "question-oc-b", "skip", "--note", "Later."}, false},
		"io_done":         {`{"id":"io-oc-a","url":"https://mail.acme.example/1"}`, []string{"io", "done", "io-oc-b", "--url", "https://mail.acme.example/2"}, false},
	}
}
