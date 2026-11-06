package ops

import (
	"slices"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

// A skip is navigation in one web visit: stack_decide and question_review
// with skip change nothing, batch is accepted and ignored with a note, and the
// stack lists the skipped items for any visit.
func TestSkipIsNotStored(t *testing.T) {
	f, _ := clockFix(t)
	p := fileDossierMD(t, f, socialMD("Posts"))
	q := must(t, QuestionAdd, f.owner, QuestionAddInput{Text: "Which window?", Project: f.proj}).ID
	text := func(id string) string {
		doc, err := f.owner.Store.Get(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		return renderText(doc)
	}
	beforeP, beforeQ := text(p), text(q)
	before := must(t, ProposalStack, f.owner, ProposalStackInput{NeedsYou: true})
	dec := must(t, StackDecide, f.owner, StackDecideInput{Decision: model.AnswerSkip, Items: []StackItemRef{{Proposal: p, Item: "q1"}}, Batch: "visit-1"})
	if !strings.Contains(dec.Note, "skip is not stored") || !strings.Contains(dec.Note, "batch is ignored") {
		t.Fatalf("stack_decide note = %q", dec.Note)
	}
	rev := must(t, QuestionReview, f.owner, QuestionReviewInput{ID: q, Decision: ReviewSkip, Batch: "visit-1"})
	if !strings.Contains(rev.Note, "skip is not stored") {
		t.Fatalf("question_review note = %q", rev.Note)
	}
	if text(p) != beforeP || text(q) != beforeQ {
		t.Fatal("a skip wrote to the vault")
	}
	same := must(t, ProposalStack, f.owner, ProposalStackInput{NeedsYou: true, Batch: "visit-1"})
	if got := stackRefs(same); !slices.Contains(got, p+"#q1") || !slices.Contains(got, q) || same.NeedsYou != before.NeedsYou {
		t.Fatalf("stack after skip = %v (needs %d, was %d)", got, same.NeedsYou, before.NeedsYou)
	}
	if !strings.Contains(same.Note, "batch is ignored") {
		t.Fatalf("proposal_stack note = %q", same.Note)
	}
	// ask with a batch is accepted: batch is ignored.
	if _, err := StackDecide.Run(t.Context(), f.owner, StackDecideInput{Decision: model.AnswerAsk, Comment: "x", Items: []StackItemRef{{Proposal: p, Item: "q2"}}, Batch: "v"}); err != nil {
		t.Fatal(err)
	}
}

// A skip an older para stored (state skipped, skip_batch) is read and
// ignored: the item waits for the owner, and the keys stay on disk.
func TestOldSkipBatchIsReadAndIgnored(t *testing.T) {
	f, _ := clockFix(t)
	p := fileDossierMD(t, f, socialMD("Posts"))
	if _, err := changeProposal(t.Context(), f.owner, p, func(pp *proposal) error {
		it, err := pp.item("q1")
		if err != nil {
			return err
		}
		it.State, it.SkippedAt, it.SkipBatch = model.ItemSkipped, "2026-11-01T09:00:00Z", "visit-old"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	raw := func() string {
		doc, err := f.owner.Store.Get(t.Context(), p)
		if err != nil {
			t.Fatal(err)
		}
		return renderText(doc)
	}
	if !strings.Contains(raw(), "skip_batch: visit-old") {
		t.Fatalf("fixture has no skip_batch:\n%s", raw())
	}
	out := must(t, ProposalStack, f.owner, ProposalStackInput{NeedsYou: true, Batch: "visit-old"})
	if !slices.Contains(stackRefs(out), p+"#q1") {
		t.Fatalf("an old skip hides the item: %v", stackRefs(out))
	}
	must(t, StackDecide, f.owner, StackDecideInput{Decision: model.AnswerAsk, Comment: "Check the date.", Items: []StackItemRef{{Proposal: p, Item: "q2"}}})
	if !strings.Contains(raw(), "skip_batch: visit-old") {
		t.Fatal("a later write dropped the old skip_batch key")
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
