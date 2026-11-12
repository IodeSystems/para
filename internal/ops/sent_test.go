package ops

import (
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
)

func TestSentListShowsOnlyWhatTheOwnerSent(t *testing.T) {
	f, rv := clockFix(t)
	task := must(t, TaskAdd, f.owner, TaskAddInput{Goal: f.proj, Title: "Book the venue"}).ID
	q := must(t, QuestionAdd, f.owner, QuestionAddInput{Text: "Which venue?", Goal: f.proj}).ID

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
	want := []string{sent.Request, qr.Request}
	if !slices.Equal(ids, want) {
		t.Fatalf("sent = %v, want newest first %v", ids, want)
	}
	if out[0].Item != task || out[0].Note != "Met Sam; booked room A." || !out[0].Undoable || out[0].Status != model.RequestQueued {
		t.Fatalf("composer row = %+v", out[0])
	}
	if out[1].Item != q || out[1].Note != "Ask Sam which one." {
		t.Fatalf("question row = %+v", out[1])
	}
	// Taken back (Close on the request, as the composer does), it is no longer undoable.
	if _, err := Close.Invoke(t.Context(), f.owner, json.RawMessage(`{"id":"`+sent.Request+`"}`)); err != nil {
		t.Fatal(err)
	}
	if got := must(t, SentList, f.owner, SentListInput{}).Items[0]; got.Undoable || got.Status != model.RequestCancelled {
		t.Fatalf("after take back = %+v", got)
	}
}
