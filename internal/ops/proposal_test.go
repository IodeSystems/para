package ops

import (
	"testing"

	"github.com/iodesystems/para/internal/model"
)

// propFix is the proposal tests' name for agentFix.
type propFix = agentFix

func newPropFix(t *testing.T) *propFix { return newAgentFix(t) }

var ev = model.Evidence{Quote: "Ship it.", Locator: "https://acme.example/line-1"}

func mailItem(draft string) ItemInput {
	return ItemInput{Kind: model.ItemExternal, Medium: "email", Target: "ada@acme.example", Draft: draft, Evidence: ev}
}

func (f *propFix) taskItem(title string) ItemInput {
	return ItemInput{Kind: model.ItemOp, Op: "task_add", Input: map[string]any{"project": f.proj, "title": title, "done_when": "It exists."}, Evidence: ev}
}

// reviewItem is a message with every required field.
func reviewItem(body string) ItemInput {
	return ItemInput{Kind: model.ItemMessage, Subtype: "email", People: []string{"ada@acme.example"}, Body: body,
		Reversible: model.ReversibleNo, Evidence: model.Evidence{Quote: "Ship it.", Locator: "https://acme.example/line-1"}}
}

func paraTask(f *propFix, title string) ItemInput {
	return ItemInput{Kind: model.ItemPara, Op: "task_add", How: "Adds a task.", Impact: "One more task.", Reversible: model.ReversibleYes,
		Input:    map[string]any{"project": f.proj, "title": title, "done_when": "It exists."},
		Evidence: model.Evidence{Quote: "Ship it.", Locator: "https://acme.example/line-1"}}
}

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
