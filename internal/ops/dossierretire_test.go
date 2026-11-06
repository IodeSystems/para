package ops

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

// Since vault format 8 nothing an agent calls approves an outward act but
// io_approve, and that takes the owner's words: the dossier ops that took
// chat approval are gone.
func TestNothingOutwardApprovedWithoutOwnerSaid(t *testing.T) {
	for _, gone := range []string{"proposal_decide", "action_record", "proposal_approve", "proposal_submit", "stack_decide", "proposal_claim", "proposal_add"} {
		if _, ok := registry[gone]; ok {
			t.Errorf("%s is still an op", gone)
		}
	}
	var approvers []string
	for _, e := range ForAgents() {
		if strings.Contains(e.Name(), "approve") || strings.Contains(e.Name(), "decide") {
			approvers = append(approvers, e.Name())
		}
	}
	if !slices.Equal(approvers, []string{"io_approve"}) {
		t.Fatalf("agent ops that approve = %v", approvers)
	}
	f := ioFix(t)
	id := outDraft(t, f, model.ExpectsNone, "")
	if _, err := IOApprove.Run(t.Context(), f.agent, IOApproveInput{ID: id}); err == nil || !strings.Contains(err.Error(), "owner_said") {
		t.Fatalf("agent approve without owner_said = %v", err)
	}
	if _, m, _ := getIO(t.Context(), f.owner, id); m.State != model.IODraft {
		t.Fatalf("state = %s", m.State)
	}
}

// proposal_executed is an alias of io_send: on the item a vault upgrade
// moved to a draft it records the same send io_send does, and says alias_of.
func TestProposalExecutedIsIOSend(t *testing.T) {
	f := ioFix(t)
	id := outDraft(t, f, model.ExpectsNone, "")
	twin := outDraft(t, f, model.ExpectsNone, "")
	body, err := model.NewDossier(model.DossierHeader{TLDR: "Old"}, "Items", []model.ProposalItem{
		{ID: "i1", Kind: model.ItemMessage, People: []string{"person-ada-example"}, Body: "Hi.", Evidence: model.Evidence{Quote: "q", Locator: "https://acme.example/1"},
			State: model.ItemMoved, Result: id},
		{ID: "i2", Kind: model.ItemQuestion, Title: "Q", Evidence: model.Evidence{Quote: "q", Locator: "https://acme.example/1"}, State: model.ItemAnswered},
	}).Render()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.owner.Store.Create(t.Context(), model.Document{ID: "proposal-old", Folder: model.FolderProposals, Title: "Old", Body: body,
		Meta: &model.Proposal{Common: model.Common{Type: model.TypeProposal}, Status: model.ProposalSuperseded, Revision: 1, ProposedBy: "claude"}}); err != nil {
		t.Fatal(err)
	}
	raw, err := ProposalExecuted.Invoke(t.Context(), f.agent, json.RawMessage(`{"id":"proposal-old","item":"i1","result":"done","url":"https://mail.acme.example/1"}`))
	if err != nil {
		t.Fatal(err)
	}
	got := raw.(ProposalExecutedOutput)
	want := mustRun(t, IOSend, f.agent, IOSendInput{ID: twin, URL: "https://mail.acme.example/2"})
	if got.AliasOf != "io_send" || got.State != want.State || got.BecameType != want.BecameType || got.ID != id {
		t.Fatalf("alias = %+v, io_send = %+v", got, want)
	}
	if _, err := ProposalExecuted.Run(t.Context(), f.agent, ProposalExecutedInput{ID: "proposal-old", Item: "i2", Result: "done", URL: "https://x.example"}); err == nil ||
		!strings.Contains(err.Error(), "io_add") {
		t.Fatalf("an item with no draft = %v", err)
	}
	if _, err := ProposalExecuted.Run(t.Context(), f.agent, ProposalExecutedInput{ID: "proposal-old", Item: "i1", Result: "failed"}); err == nil {
		t.Fatal("failed was recorded")
	}
}
