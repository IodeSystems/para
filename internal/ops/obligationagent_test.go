package ops

import (
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

// openWorkFor lists the open agent work linked to an item.
func openWorkFor(t *testing.T, d Deps, id string) []model.Document {
	t.Helper()
	reqs, err := loadRequests(ctx, d.Store)
	if err != nil {
		t.Fatal(err)
	}
	var out []model.Document
	for _, rd := range reqs {
		r := rd.Meta.(*model.Request)
		if !openWork(rd.Title, r) {
			continue
		}
		for _, l := range r.Links {
			if l.Target() == id {
				out = append(out, rd)
			}
		}
	}
	return out
}

// A redirect to an agent queues one piece of agent work for it, linked to the
// obligation; a second redirect to the same agent does not double it; undo
// of the change cancels it.
func TestObligationRedirectToAgentQueuesWork(t *testing.T) {
	d, proj, person := obFixture(t)
	addPeople(t, d)
	ob := must(t, ObligationAdd, d, ObligationAddInput{Title: "Send the plan", OwedTo: []string{person}, Due: "2026-11-11", Project: proj, DoneWhen: "The plan is sent."})
	r := must(t, ObligationChange, d, ObligationChangeInput{ID: ob.ID, Action: ChangeRedirect, To: "agent:robin", Reason: "Robin drafts it", Notify: new(bool)})
	if len(r.Work) != 1 {
		t.Fatalf("redirect = %+v", r)
	}
	work := openWorkFor(t, d, ob.ID)
	if len(work) != 1 || work[0].ID != r.Work[0] {
		t.Fatalf("open work = %+v", work)
	}
	for _, want := range []string{"agent:robin", "Robin drafts it", "The plan is sent.", "approved draft"} {
		if !strings.Contains(work[0].Body, want) {
			t.Errorf("work lacks %q:\n%s", want, work[0].Body)
		}
	}
	again := must(t, ObligationChange, d, ObligationChangeInput{ID: ob.ID, Action: ChangeRedirect, To: "agent:robin", Reason: "Still Robin", Notify: new(bool)})
	if len(again.Work) != 0 || len(openWorkFor(t, d, ob.ID)) != 1 {
		t.Fatalf("second redirect doubled the work: %+v", again)
	}
	// Another agent gets its own.
	other := must(t, ObligationChange, d, ObligationChangeInput{ID: ob.ID, Action: ChangeRedirect, To: "agent:sam", Reason: "Sam takes it", Notify: new(bool)})
	if len(other.Work) != 1 || len(openWorkFor(t, d, ob.ID)) != 2 {
		t.Fatalf("other agent: %+v", other)
	}
	must(t, ReceiptRevert, d, ReceiptIDInput{ID: other.Receipt})
	must(t, ReceiptRevert, d, ReceiptIDInput{ID: again.Receipt})
	// The first change's undo cancels the first request.
	if first := openWorkFor(t, d, ob.ID); len(first) != 1 {
		t.Fatalf("after undoing the later changes: %+v", first)
	}
	must(t, ReceiptRevert, d, ReceiptIDInput{ID: r.Receipt})
	if left := openWorkFor(t, d, ob.ID); len(left) != 0 {
		t.Errorf("undo left open work: %+v", left)
	}
}
