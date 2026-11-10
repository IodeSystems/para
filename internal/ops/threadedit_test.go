package ops

import (
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
)

// An owner turn is edited while queued: the words, kind, and quote change, the
// old words stay as an Edited step, and one receipt undoes it.
func TestThreadEditQueued(t *testing.T) {
	f := newThreadFix(t)
	post := must(t, ThreadPost, f.owner, ThreadPostInput{ID: f.task, Text: "Draft the post.", Kind: model.RequestTurnModify})
	q := "Section B"
	f.rv.advance(time.Minute)
	must(t, ThreadEdit, f.owner, ThreadEditInput{ID: post.Request, Text: "Why is section B late?", Kind: model.RequestTurnAsk, Quote: &q})
	tu := f.thread(t, ThreadInput{ID: f.task}).Turns[0]
	if tu.OwnerWords != "Why is section B late?" || tu.TurnKind != model.RequestTurnAsk || tu.Quote != q || tu.Status != model.RequestQueued {
		t.Fatalf("turn = %+v", tu)
	}
	if len(tu.Edited) != 1 || !strings.Contains(tu.Edited[0].Text, "Draft the post.") || !strings.Contains(tu.Edited[0].Text, "Kind was: modify") {
		t.Fatalf("edited = %+v", tu.Edited)
	}
	doc, err := f.owner.Store.Get(t.Context(), post.Request)
	if err != nil || !strings.Contains(doc.Body, "Kind: ask:") || strings.Contains(doc.Body, "Kind: modify:") {
		t.Fatalf("body = %q, %v", doc.Body, err)
	}
	// A second edit removes the quote and adds to the history.
	f.rv.advance(time.Minute)
	empty := ""
	must(t, ThreadEdit, f.owner, ThreadEditInput{ID: post.Request, Quote: &empty, Text: "Why is B late?"})
	tu = f.thread(t, ThreadInput{ID: f.task}).Turns[0]
	if tu.Quote != "" || len(tu.Edited) != 2 || tu.OwnerWords != "Why is B late?" {
		t.Fatalf("after second edit = %+v", tu)
	}
	rc := must(t, ReceiptList, f.owner, ReceiptListInput{Op: "thread_edit"})
	if len(rc.Items) != 2 {
		t.Fatalf("receipts = %+v", rc.Items)
	}
	must(t, ReceiptUndo, f.owner, ReceiptUndoInput{ID: rc.Items[0].ID})
	if tu = f.thread(t, ThreadInput{ID: f.task}).Turns[0]; tu.OwnerWords != "Why is section B late?" {
		t.Fatalf("after undo = %+v", tu)
	}
	if _, err := ThreadEdit.Run(t.Context(), f.owner, ThreadEditInput{ID: post.Request}); err == nil {
		t.Fatal("an edit that changes nothing was accepted")
	}
}

// Once an agent holds the turn, an edit is refused with a plain message.
func TestThreadEditClaimedRefused(t *testing.T) {
	f := newThreadFix(t)
	post := must(t, ThreadPost, f.owner, ThreadPostInput{ID: f.task, Text: "Draft the post."})
	must(t, RequestNext, f.agent, RequestNextInput{})
	_, err := ThreadEdit.Run(t.Context(), f.owner, ThreadEditInput{ID: post.Request, Text: "Never mind."})
	if err == nil || !strings.Contains(err.Error(), "an agent already has it; post a follow-up turn instead") {
		t.Fatalf("err = %v", err)
	}
	if _, err := ThreadEdit.Run(t.Context(), f.agent, ThreadEditInput{ID: post.Request, Text: "x"}); err == nil {
		t.Fatal("an agent edited the owner's turn")
	}
	// Not an owner turn: a plain request is refused.
	plain := must(t, RequestAdd, f.owner, RequestAddInput{Ask: "Plain ask"})
	if _, err := ThreadEdit.Run(t.Context(), f.owner, ThreadEditInput{ID: plain.ID, Text: "x"}); err == nil {
		t.Fatal("a plain request was edited")
	}
}

// Withdraw cancels a queued turn, and a claimed one; its receipt undoes it.
func TestThreadWithdraw(t *testing.T) {
	f := newThreadFix(t)
	a := must(t, ThreadPost, f.owner, ThreadPostInput{ID: f.task, Text: "First."})
	out := must(t, ThreadWithdraw, f.owner, ThreadWithdrawInput{ID: a.Request})
	if out.Status != model.RequestCancelled {
		t.Fatalf("withdraw = %+v", out)
	}
	if got := f.thread(t, ThreadInput{ID: f.task}); got.Queued != 0 || got.Turns[0].Status != model.RequestCancelled {
		t.Fatalf("thread = %+v", got)
	}
	rc := must(t, ReceiptList, f.owner, ReceiptListInput{Op: "thread_withdraw"})
	if len(rc.Items) != 1 {
		t.Fatalf("receipts = %+v", rc.Items)
	}
	must(t, ReceiptUndo, f.owner, ReceiptUndoInput{ID: rc.Items[0].ID})
	if got := f.thread(t, ThreadInput{ID: f.task}); got.Queued != 1 {
		t.Fatalf("after undo: queued = %d", got.Queued)
	}
	must(t, RequestNext, f.agent, RequestNextInput{})
	if got := must(t, ThreadWithdraw, f.owner, ThreadWithdrawInput{ID: a.Request}); got.Status != model.RequestCancelled {
		t.Fatalf("claimed withdraw = %+v", got)
	}
	if _, err := ThreadWithdraw.Run(t.Context(), f.owner, ThreadWithdrawInput{ID: a.Request}); err == nil {
		t.Fatal("a cancelled turn was withdrawn again")
	}
	if _, err := ThreadWithdraw.Run(t.Context(), f.agent, ThreadWithdrawInput{ID: a.Request}); err == nil {
		t.Fatal("an agent withdrew the owner's turn")
	}
}
