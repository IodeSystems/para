package ops

import (
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

func itemState(t *testing.T, f *agentFix, id, item string) model.ItemState {
	t.Helper()
	for _, it := range mustRun(t, ProposalShow, f.owner, ProposalIDInput{ID: id}).Items {
		if it.ID == item {
			return it.State
		}
	}
	t.Fatalf("no item %s", item)
	return ""
}

// stack_undo puts a decision back: a decision only recorded resets, an executed
// vault change is undone from its receipts, what an agent claimed is refused.
func TestStackUndo(t *testing.T) {
	f := newAgentFix(t)
	id := fileDossierMD(t, f, dossierMD(f))
	ref := func(item string) []StackItemRef { return []StackItemRef{{Proposal: id, Item: item}} }
	decide := func(in StackDecideInput) Decision {
		t.Helper()
		out := mustRun(t, StackDecide, f.owner, in)
		return out.Proposals[0].Items[0]
	}

	// Skip and decline only record a decision: undo resets.
	if d := decide(StackDecideInput{Decision: "skip", Items: ref("m1")}); d.Undo != UndoReset {
		t.Fatalf("skip undo = %q", d.Undo)
	}
	mustRun(t, StackUndo, f.owner, StackUndoInput{Items: ref("m1")})
	if s := itemState(t, f, id, "m1"); s != model.ItemPending {
		t.Fatalf("after undo of skip: %s", s)
	}
	if d := decide(StackDecideInput{Decision: "reject", Items: ref("m1"), Comment: "No."}); d.Undo != UndoReset {
		t.Fatalf("decline undo = %q", d.Undo)
	}
	mustRun(t, StackUndo, f.owner, StackUndoInput{Items: ref("m1")})
	if s := itemState(t, f, id, "m1"); s != model.ItemPending {
		t.Fatalf("after undo of decline: %s", s)
	}

	// Ask queues agent work; undo cancels it.
	ask := mustRun(t, StackDecide, f.owner, StackDecideInput{Decision: "ask", Items: ref("m1"), Comment: "Check the tone."})
	if len(ask.Requests) != 1 || ask.Proposals[0].Items[0].Undo != UndoReset {
		t.Fatalf("ask = %+v", ask)
	}
	mustRun(t, StackUndo, f.owner, StackUndoInput{Items: ref("m1"), Requests: ask.Requests})
	if s := itemState(t, f, id, "m1"); s != model.ItemPending {
		t.Fatalf("after undo of ask: %s", s)
	}
	if _, r, err := getRequest(t.Context(), f.owner.Store, ask.Requests[0]); err != nil || r.Status != model.RequestCancelled {
		t.Fatalf("request = %v %v", r, err)
	}

	// An approved vault change ran: its receipts come back out, bundle and all.
	out := mustRun(t, StackDecide, f.owner, StackDecideInput{Decision: "approve", Group: "tasks"})
	for _, d := range out.Proposals[0].Items {
		if d.Undo != UndoReceipts || len(d.Receipts) == 0 {
			t.Fatalf("approve %s: undo %q receipts %v", d.Item, d.Undo, d.Receipts)
		}
	}
	if n := len(mustRun(t, TaskList, f.owner, TaskListInput{}).Tasks); n != 2 {
		t.Fatalf("tasks = %d", n)
	}
	res := mustRun(t, StackUndo, f.owner, StackUndoInput{Items: ref("c1")})
	if len(res.Undone) < 2 || len(res.Items) != 2 {
		t.Fatalf("undo = %+v", res)
	}
	if n := len(mustRun(t, TaskList, f.owner, TaskListInput{}).Tasks); n != 0 {
		t.Fatalf("tasks after undo = %d", n)
	}
	for _, it := range []string{"c1", "c2"} {
		if s := itemState(t, f, id, it); s != model.ItemPending {
			t.Fatalf("%s after undo: %s", it, s)
		}
	}

	// An approved message waits for an agent: undo resets. Once claimed, it cannot.
	if d := decide(StackDecideInput{Decision: "approve", Items: ref("m1")}); d.Undo != UndoReset {
		t.Fatalf("approve message undo = %q", d.Undo)
	}
	mustRun(t, StackUndo, f.owner, StackUndoInput{Items: ref("m1")})
	decide(StackDecideInput{Decision: "approve", Items: ref("m1")})
	mustRun(t, ProposalClaim, f.agent, ProposalClaimInput{ID: id, Items: []string{"m1"}, LeaseSeconds: 600})
	if _, err := StackUndo.Run(t.Context(), f.owner, StackUndoInput{Items: ref("m1")}); err == nil || !strings.Contains(err.Error(), "cannot be undone") {
		t.Fatalf("undo of a claimed item: %v", err)
	}
	// A choice: the alternative an approval closed comes back.
	decide(StackDecideInput{Decision: "approve", Items: ref("a1")})
	if s := itemState(t, f, id, "a2"); s != model.ItemDenied {
		t.Fatalf("a2 = %s", s)
	}
	mustRun(t, StackUndo, f.owner, StackUndoInput{Items: ref("a1")})
	if s := itemState(t, f, id, "a2"); s != model.ItemPending {
		t.Fatalf("a2 after undo = %s", s)
	}
}

// A vault question skipped or sent to an agent goes back; an answer cannot.
func TestStackUndoQuestion(t *testing.T) {
	f := newAgentFix(t)
	q := mustRun(t, QuestionAdd, f.owner, QuestionAddInput{Text: "Which vendor?", Root: true, Options: []string{"A", "B"}})
	out := mustRun(t, QuestionReview, f.owner, QuestionReviewInput{ID: q.ID, Decision: ReviewSkip})
	if out.Undo != UndoReset || out.Review != string(model.ItemSkipped) {
		t.Fatalf("skip = %+v", out)
	}
	mustRun(t, StackUndo, f.owner, StackUndoInput{Question: q.ID})
	if _, got, err := getQuestion(t.Context(), f.owner.Store, q.ID); err != nil || got.ReviewState != "" {
		t.Fatalf("review state %q %v", got.ReviewState, err)
	}
	ans := mustRun(t, QuestionReview, f.owner, QuestionReviewInput{ID: q.ID, Decision: ReviewAnswer, Choices: []string{"A"}})
	if ans.Undo != "" {
		t.Fatalf("answer undo = %q", ans.Undo)
	}
	if _, err := StackUndo.Run(t.Context(), f.owner, StackUndoInput{Question: q.ID}); err == nil {
		t.Fatal("undid an answer")
	}
}
