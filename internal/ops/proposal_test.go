package ops

import (
	"testing"

	"github.com/iodesystems/para/internal/model"
)

// propFix is the proposal tests' name for agentFix.
type propFix = agentFix

func newPropFix(t *testing.T) *propFix { return newAgentFix(t) }

func TestOldProposalFileReads(t *testing.T) {
	f := newPropFix(t)
	body := "Written by para.\n\n````yaml para-proposal\nitems:\n  - id: i1\n    kind: external\n    medium: email\n    target: a@acme.example\n    draft: Hi\n    evidence:\n      quote: q\n      locator: https://acme.example/l\n    state: rework\n    reason: again\n  - id: i2\n    kind: op\n    op: task_start\n    input:\n      id: x\n    evidence:\n      quote: q\n      locator: https://acme.example/l\n    state: pending\nannotations:\n  - id: a1\n    item: i2\n    quote: task\n    note: Why?\n    by: user\n    revision: 1\n    state: open\n````"
	doc, err := f.owner.Store.Create(t.Context(), model.Document{ID: "proposal-old", Folder: model.FolderProposals, Title: "Old", Body: body,
		Meta: &model.Proposal{Common: model.Common{Type: model.TypeProposal}, Source: model.LinkTo(f.source), Status: model.ProposalOpen, Revision: 1, ProposedBy: "claude"}})
	if err != nil {
		t.Fatal(err)
	}
	show, err := ProposalShow.Run(t.Context(), f.owner, ProposalIDInput{ID: doc.ID})
	if err != nil {
		t.Fatal(err)
	}
	a, b := show.Items[0], show.Items[1]
	if a.Kind != model.ItemAction || a.Body != "Hi" || a.Subtype != "email" || a.State != model.ItemSentBack {
		t.Fatalf("external = %+v", a)
	}
	if b.Kind != model.ItemPara || len(b.Thread) != 1 || b.Thread[0].State != model.TurnOpen || b.State != model.ItemPending {
		t.Fatalf("op = %+v", b)
	}
	if a.Hash != model.ExternalHash("a@acme.example", "Hi") {
		t.Fatalf("old hash changed: %s", a.Hash)
	}
}
