package ops

import (
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
)

// Conversations lists io items in with agent turns, newest activity first;
// an io with no turn is no conversation. A finished, seen turn on a filed io
// is done: listed only with include_done.
func TestConversationsOrderAndFields(t *testing.T) {
	f := newThreadFix(t)
	asked := must(t, IOAdd, f.owner, IOAddInput{Direction: model.IOIn, Text: "Who owns the vendor contract renewal?"}).ID
	must(t, IOAdd, f.owner, IOAddInput{Direction: model.IOIn, Text: "Just a note, no turn."})
	turn := must(t, ThreadPost, f.owner, ThreadPostInput{ID: asked, Text: "Find the owner.", Kind: model.RequestTurnResearch})
	f.rv.advance(time.Minute)
	must(t, RequestNext, f.agent, RequestNextInput{})
	reply := "Sam   Example owns it;\nrenewal is due in March."
	must(t, RequestDone, f.agent, RequestDoneInput{ID: turn.Request, Resolution: model.ResolutionDone, Result: "Found the owner.", Reply: reply})
	f.rv.advance(time.Minute)
	queuedIO := must(t, IOAdd, f.owner, IOAddInput{Direction: model.IOIn, Text: "Check the portal budget."}).ID
	must(t, ThreadPost, f.owner, ThreadPostInput{ID: queuedIO, Text: "Look it up."})

	got := must(t, Conversations, f.owner, ConversationsInput{}).Items
	if len(got) != 2 || got[0].ID != queuedIO || got[1].ID != asked {
		t.Fatalf("conversations = %+v", got)
	}
	q, d := got[0], got[1]
	if q.Title != "Check the portal budget." || q.LastStatus != model.RequestQueued || q.LastKind != model.RequestTurnOther ||
		q.Queued != 1 || q.Claimed != 0 || q.Turns != 1 || q.LastReply != "" || !q.WaitingNoAgent || q.Created == "" || q.LastAt == "" {
		t.Fatalf("queued row = %+v", q)
	}
	if d.LastStatus != model.RequestDone || d.LastKind != model.RequestTurnResearch || d.Queued != 0 || d.WaitingNoAgent ||
		d.LastReply != "Sam Example owns it; renewal is due in March." || d.LastAt >= q.LastAt {
		t.Fatalf("done row = %+v", d)
	}
	if lim := must(t, Conversations, f.owner, ConversationsInput{Limit: 1}).Items; len(lim) != 1 || lim[0].ID != queuedIO {
		t.Fatalf("limit 1 = %+v", lim)
	}

	// Filed, and the result seen: done.
	note := must(t, NoteAdd, f.owner, NoteAddInput{Title: "Vendor contract owner"}).ID
	must(t, IOFile, f.owner, IOFileInput{ID: asked, Existing: note})
	must(t, RequestSeen, f.owner, RequestSeenInput{ID: turn.Request})
	if open := must(t, Conversations, f.owner, ConversationsInput{}).Items; len(open) != 1 || open[0].ID != queuedIO {
		t.Fatalf("open = %+v", open)
	}
	all := must(t, Conversations, f.owner, ConversationsInput{IncludeDone: true}).Items
	if len(all) != 2 || all[1].ID != asked || all[1].State != model.IOFiled {
		t.Fatalf("include_done = %+v", all)
	}
}
