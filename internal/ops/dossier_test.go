package ops

import (
	"testing"

	"github.com/iodesystems/para/internal/model"
)

func TestSummaryOfQuestionAndDocEdit(t *testing.T) {
	f := newAgentFix(t)
	z := newItemSummarizer(t.Context(), f.owner)
	if got := z.of(model.ProposalItem{Kind: model.ItemQuestion, Title: "Which vendor?"}); got != "Question: Which vendor?" {
		t.Fatalf("question = %q", got)
	}
	if got := z.of(model.ProposalItem{Kind: model.ItemDocEdit, Target: "[[" + f.source + "]]"}); got != "Edit Sync notes" {
		t.Fatalf("doc edit = %q", got)
	}
}
