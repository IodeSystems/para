package ops

import (
	"slices"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

func skipOf(t *testing.T, d Deps, batch string, refs ...StackItemRef) {
	t.Helper()
	must(t, StackDecide, d, StackDecideInput{Decision: model.AnswerSkip, Items: refs, Batch: batch})
}

func TestSkipIsNotInThisBatchAndNeverDefers(t *testing.T) {
	f, _ := clockFix(t)
	p := fileDossierMD(t, f, socialMD("Posts"))
	q := must(t, QuestionAdd, f.owner, QuestionAddInput{Text: "Which window?", Project: f.proj}).ID
	before := must(t, ProposalStack, f.owner, ProposalStackInput{NeedsYou: true})
	skipOf(t, f.owner, "visit-1", StackItemRef{Proposal: p, Item: "q1"})
	must(t, QuestionReview, f.owner, QuestionReviewInput{ID: q, Decision: ReviewSkip, Batch: "visit-1"})

	same := must(t, ProposalStack, f.owner, ProposalStackInput{NeedsYou: true, Batch: "visit-1"})
	if got := stackRefs(same); slices.Contains(got, p+"#q1") || slices.Contains(got, q) || !slices.Contains(got, p+"#q2") {
		t.Fatalf("same batch = %v: the skips should be hidden, q2 shown", got)
	}
	if same.NeedsYou != before.NeedsYou-2 {
		t.Fatalf("needs_you = %d, want %d", same.NeedsYou, before.NeedsYou-2)
	}
	other := must(t, ProposalStack, f.owner, ProposalStackInput{NeedsYou: true, Batch: "visit-2"})
	if got := stackRefs(other); !slices.Contains(got, p+"#q1") || !slices.Contains(got, q) || other.NeedsYou != before.NeedsYou {
		t.Fatalf("other batch = %v (needs %d)", got, other.NeedsYou)
	}
	none := must(t, ProposalStack, f.owner, ProposalStackInput{NeedsYou: true})
	if !slices.Contains(stackRefs(none), p+"#q1") {
		t.Fatal("no batch hides a skip")
	}
	for _, r := range other.Rows {
		if r.Item == "q1" && (r.Stack != StackProposed || r.State != model.ItemSkipped || r.SkipBatch != "visit-1" || r.DeferUntil != "") {
			t.Fatalf("skipped row = %+v: a skip must stay proposed with no date", r)
		}
	}
	if _, err := StackDecide.Run(t.Context(), f.owner, StackDecideInput{Decision: model.AnswerAsk, Comment: "x", Items: []StackItemRef{{Proposal: p, Item: "q2"}}, Batch: "v"}); err == nil {
		t.Fatal("batch with ask was accepted")
	}
}

func TestLaterIsInScopedViewsNotTheDefault(t *testing.T) {
	f, _ := clockFix(t)
	a := fileDossierMD(t, f, socialMD("Posts A"))
	b := fileDossierMD(t, f, socialMD("Posts B"))
	must(t, TagAdd, f.owner, TagInput{IDs: []string{a}, Tags: []string{"socials"}})
	must(t, Defer, f.owner, DeferInput{Items: []ItemRef{{Proposal: a, Item: "q1"}}, Until: "2026-11-09"})

	all := must(t, ProposalStack, f.owner, ProposalStackInput{NeedsYou: true})
	if got := stackRefs(all); slices.Contains(got, a+"#q1") || len(got) != 3 {
		t.Fatalf("unscoped needs_you = %v: later must be hidden", got)
	}
	if got := stackItems(t, f.owner, ProposalStackInput{}); slices.Contains(got, a+"#q1") {
		t.Fatalf("unscoped = %v", got)
	}
	for name, in := range map[string]ProposalStackInput{
		"tag":      {Tag: "socials"},
		"tag+need": {Tag: "socials", NeedsYou: true},
		"dossier":  {Proposal: a},
	} {
		out := must(t, ProposalStack, f.owner, in)
		var later *StackRow
		for i, r := range out.Rows {
			if r.Item == "q1" {
				later = &out.Rows[i]
			}
		}
		if later == nil || later.Stack != StackLater || later.DeferUntil != "2026-11-09" {
			t.Fatalf("%s: rows = %v, later row = %+v", name, stackRefs(out), later)
		}
		if len(out.Rows) != 2 || slices.Contains(stackRefs(out), b+"#q1") {
			t.Fatalf("%s: rows = %v", name, stackRefs(out))
		}
		if in.NeedsYou && out.NeedsYou != 1 {
			t.Fatalf("%s: needs_you = %d, later must not count", name, out.NeedsYou)
		}
	}
}
