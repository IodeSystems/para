package ops

import (
	"slices"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
)

func TestSentListShowsOnlyWhatTheOwnerSent(t *testing.T) {
	f, rv := clockFix(t)
	p := fileDossierMD(t, f, dossierMD(f))
	task := must(t, TaskAdd, f.owner, TaskAddInput{Project: f.proj, Title: "Book the venue"}).ID
	q := must(t, QuestionAdd, f.owner, QuestionAddInput{Text: "Which venue?", Project: f.proj}).ID

	asked := must(t, StackDecide, f.owner, StackDecideInput{Items: []StackItemRef{{Proposal: p, Item: "q1"}}, Decision: "ask", Comment: "Check the vendor's price list.\nThen revise."})
	rv.advance(time.Second)
	qr := must(t, QuestionReview, f.owner, QuestionReviewInput{ID: q, Decision: ReviewAsk, Note: "Ask Sam which one."})
	rv.advance(time.Second)
	sent := must(t, AgentSend, f.owner, AgentSendInput{ID: task, Text: "Met Sam; booked room A."})
	// Agent work of other kinds is not the owner's Send to agent.
	must(t, RequestAdd, f.owner, RequestAddInput{Title: "Tidy the wiki", Ask: "Tidy it."})

	out := must(t, SentList, f.owner, SentListInput{}).Items
	var ids []string
	for _, it := range out {
		ids = append(ids, it.Request)
	}
	want := []string{sent.Request, qr.Request, asked.Requests[0]}
	if !slices.Equal(ids, want) {
		t.Fatalf("sent = %v, want newest first %v", ids, want)
	}
	if out[0].Item != task || out[0].Note != "Met Sam; booked room A." || !out[0].Undoable || out[0].Status != model.RequestQueued {
		t.Fatalf("composer row = %+v", out[0])
	}
	if out[2].Item != p || out[2].Note != "Check the vendor's price list." {
		t.Fatalf("stack row = %+v", out[2])
	}
	must(t, AgentUnsend, f.owner, AgentUnsendInput{Request: sent.Request})
	if got := must(t, SentList, f.owner, SentListInput{}).Items[0]; got.Undoable {
		t.Fatalf("after unsend = %+v", got)
	}
}

func TestStackNeedsYouLeavesOutSentBackAndDecided(t *testing.T) {
	f, _ := clockFix(t)
	p := fileDossierMD(t, f, dossierMD(f))
	must(t, StackDecide, f.owner, StackDecideInput{Items: []StackItemRef{{Proposal: p, Item: "q1"}}, Decision: "ask", Comment: "Check the price list."})
	must(t, StackDecide, f.owner, StackDecideInput{Items: []StackItemRef{{Proposal: p, Item: "m1"}}, Decision: "approve"})
	out := must(t, ProposalStack, f.owner, ProposalStackInput{NeedsYou: true})
	for _, r := range out.Rows {
		if r.Proposal == p && (r.Item == "q1" || r.Item == "m1") {
			t.Fatalf("needs_you lists %s (%s, %s)", r.Item, r.Stack, r.State)
		}
		if r.Stack != StackProposed || r.State == model.ItemSentBack {
			t.Fatalf("needs_you row = %+v", r)
		}
	}
	if out.NeedsYou != len(out.Rows) || out.NeedsYou == 0 {
		t.Fatalf("needs_you count %d, rows %d", out.NeedsYou, len(out.Rows))
	}
}
