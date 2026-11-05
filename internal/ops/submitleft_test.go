package ops

import (
	"testing"

	"github.com/iodesystems/para/internal/model"
)

// An answer that only holds a comment, or a skip, decides nothing: a submit
// names each such item and why, and the draft is not "answered" by them.
func TestSubmitNamesWhyADraftedItemStayedPending(t *testing.T) {
	f := newAgentFix(t)
	id := fileDossierMD(t, f, dossierMD(f))
	if _, err := draft(t, f.owner, id, 1,
		DraftAnswer{Item: "q1", Answer: &model.ItemAnswer{Comment: "Not sure yet."}},
		DraftAnswer{Item: "m1", Answer: &model.ItemAnswer{Comment: "Reword it."}},
		DraftAnswer{Item: "a1", Answer: &model.ItemAnswer{Decision: model.AnswerSkip}},
		DraftAnswer{Item: "a2", Answer: &model.ItemAnswer{Decision: model.AnswerSkip}},
	); err != nil {
		t.Fatal(err)
	}
	sub := mustRun(t, ProposalSubmit, f.owner, ProposalSubmitInput{ID: id, Revision: 1})
	for _, d := range sub.Items {
		if d.State != model.ItemSkipped {
			t.Fatalf("decided %+v: only the two skips were recorded", d)
		}
	}
	if len(sub.Items) != 2 || len(sub.Left) != 2 || sub.Left[0].Item != "q1" || sub.Left[1].Item != "m1" || sub.Left[0].Why == "" {
		t.Fatalf("items = %+v, left = %+v", sub.Items, sub.Left)
	}
	// Submitting again: the skips already took effect, so nothing is decided and all four are named.
	again := mustRun(t, ProposalSubmit, f.owner, ProposalSubmitInput{ID: id, Revision: 1})
	if len(again.Items) != 0 || len(again.Left) != 4 {
		t.Fatalf("again: items = %+v, left = %+v", again.Items, again.Left)
	}
}
