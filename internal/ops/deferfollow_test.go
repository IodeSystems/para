package ops

import (
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

func taskDeferral(t *testing.T, d Deps, id string) (model.Document, *model.Task) {
	t.Helper()
	doc, tk, err := getTask(ctx, d.Store, id)
	if err != nil {
		t.Fatal(err)
	}
	return doc, tk
}

func laterRow(t *testing.T, d Deps, id string) TaskView {
	t.Helper()
	for _, r := range must(t, TaskList, d, TaskListInput{Deferred: true}).Tasks {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("%s not in the task list", id)
	return TaskView{}
}

// until_start puts an item off until an obligation starts, until_end until
// the day after it ends; both follow a push, and a cancel brings them back
// with a note that says why. The fixture day is 2026-11-05.
func TestDeferUntilObligationStartFollows(t *testing.T) {
	d, proj, _ := obFixture(t)
	trip := must(t, ObligationAdd, d, ObligationAddInput{Kind: model.KindCommitment, Title: "Seattle trip", Goal: proj,
		Start: "2026-12-01T09:00:00-08:00", End: "2026-12-10T18:00:00-08:00"}).ID
	visit := must(t, TaskAdd, d, TaskAddInput{Goal: proj, Title: "Visit the office", DoneWhen: "Badge picked up."}).ID
	unpack := must(t, TaskAdd, d, TaskAddInput{Goal: proj, Title: "Expense the trip", DoneWhen: "Report filed."}).ID

	out := must(t, Defer, d, DeferInput{IDs: []string{visit}, UntilStart: trip})
	if out.Until != "2026-12-01" || len(out.Changed) != 1 || !strings.Contains(out.Follows, "until Seattle trip starts (2026-12-01)") {
		t.Fatalf("defer = %+v", out)
	}
	if _, tk := taskDeferral(t, d, visit); tk.DeferUntil != "2026-12-01" || tk.DeferUntilStart != trip {
		t.Fatalf("deferral = %+v", tk.Deferral)
	}
	if r := laterRow(t, d, visit); !r.Deferred || r.LaterText != "until Seattle trip starts (2026-12-01)" {
		t.Fatalf("row = %+v", r)
	}
	if c := must(t, ItemContext, d, ItemContextInput{ID: visit}); c.Later != "until Seattle trip starts (2026-12-01)" || c.LaterRef != trip {
		t.Fatalf("context later = %q %q", c.Later, c.LaterRef)
	}
	// later routes to defer with the same input.
	must(t, Later, d, LaterInput{IDs: []string{unpack}, UntilEnd: trip})
	if _, tk := taskDeferral(t, d, unpack); tk.DeferUntil != "2026-12-11" || tk.DeferUntilEnd != trip {
		t.Fatalf("until_end deferral = %+v", tk.Deferral)
	}
	if r := laterRow(t, d, unpack); r.LaterText != "until Seattle trip ends (2026-12-10)" {
		t.Fatalf("until_end row = %+v", r)
	}

	// A push moves both.
	must(t, ObligationChange, d, ObligationChangeInput{ID: trip, Action: ChangePush, Start: "2026-12-05T09:00:00-08:00", End: "2026-12-12T18:00:00-08:00", Reason: "Flights moved"})
	if _, tk := taskDeferral(t, d, visit); tk.DeferUntil != "2026-12-05" {
		t.Fatalf("after push: %+v", tk.Deferral)
	}
	if _, tk := taskDeferral(t, d, unpack); tk.DeferUntil != "2026-12-13" {
		t.Fatalf("until_end after push: %+v", tk.Deferral)
	}

	// A hand edit of the date is followed on the tick pass (followAll).
	tdoc, o := getOb(t, d, trip)
	o.Scheduled = "2026-12-07T09:00:00-08:00"
	if _, err := d.Store.Update(ctx, tdoc); err != nil {
		t.Fatal(err)
	}
	if _, err := followAll(ctx, d); err != nil {
		t.Fatal(err)
	}
	if _, tk := taskDeferral(t, d, visit); tk.DeferUntil != "2026-12-07" {
		t.Fatalf("after hand edit: %+v", tk.Deferral)
	}

	// A cancel brings them back and says why.
	must(t, ObligationChange, d, ObligationChangeInput{ID: trip, Action: ChangeCancel, Reason: "Trip is off"})
	doc, tk := taskDeferral(t, d, visit)
	if tk.DeferUntil != "" || tk.DeferUntilStart != "" {
		t.Fatalf("after cancel: %+v", tk.Deferral)
	}
	if !strings.Contains(doc.Body, "Seattle trip") || !strings.Contains(doc.Body, "cancelled") {
		t.Fatalf("body = %q", doc.Body)
	}
	if r := laterRow(t, d, visit); r.Deferred {
		t.Fatalf("still deferred: %+v", r)
	}
	if _, tk := taskDeferral(t, d, unpack); tk.DeferUntil != "" || tk.DeferUntilEnd != "" {
		t.Fatalf("until_end after cancel: %+v", tk.Deferral)
	}
}

// Undoing a push puts the follower's date back too.
func TestDeferUntilStartUndoPush(t *testing.T) {
	d, proj, _ := obFixture(t)
	trip := must(t, ObligationAdd, d, ObligationAddInput{Kind: model.KindCommitment, Title: "Trip", Goal: proj, Due: "2026-12-01"}).ID
	task := must(t, TaskAdd, d, TaskAddInput{Goal: proj, Title: "Something", DoneWhen: "Done."}).ID
	must(t, Defer, d, DeferInput{IDs: []string{task}, UntilStart: trip})
	out := must(t, ObligationChange, d, ObligationChangeInput{ID: trip, Action: ChangePush, Due: "2026-12-04", Reason: "Moved", Notify: new(bool)})
	if _, tk := taskDeferral(t, d, task); tk.DeferUntil != "2026-12-04" {
		t.Fatalf("after push: %+v", tk.Deferral)
	}
	if out.Receipt == "" {
		t.Fatal("no receipt")
	}
	must(t, ReceiptUndo, d, ReceiptUndoInput{ID: out.Receipt})
	if _, tk := taskDeferral(t, d, task); tk.DeferUntil != "2026-12-01" || tk.DeferUntilStart != trip {
		t.Fatalf("after undo: %+v", tk.Deferral)
	}
}

func TestDeferUntilStartRefuses(t *testing.T) {
	d, proj, _ := obFixture(t)
	task := must(t, TaskAdd, d, TaskAddInput{Goal: proj, Title: "Something", DoneWhen: "Done."}).ID
	trip := must(t, ObligationAdd, d, ObligationAddInput{Kind: model.KindCommitment, Title: "Trip", Goal: proj, Due: "2026-12-01"}).ID
	past := must(t, ObligationAdd, d, ObligationAddInput{Kind: model.KindCommitment, Title: "Today thing", Goal: proj, Due: "2026-11-05"}).ID
	ongoing := must(t, ObligationAdd, d, ObligationAddInput{Kind: model.KindCommitment, Title: "Upkeep", Goal: proj, Cadence: "monthly"}).ID
	for name, in := range map[string]DeferInput{
		"with until":      {IDs: []string{task}, UntilStart: trip, Until: "2026-12-20"},
		"with after":      {IDs: []string{task}, UntilStart: trip, After: trip},
		"start and end":   {IDs: []string{task}, UntilStart: trip, UntilEnd: trip},
		"not after today": {IDs: []string{task}, UntilStart: past},
		"no date":         {IDs: []string{task}, UntilStart: ongoing},
		"not obligation":  {IDs: []string{task}, UntilStart: task},
		"itself":          {IDs: []string{trip}, UntilStart: trip},
	} {
		if _, err := Defer.Run(ctx, d, in); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	// A commitment with only a due starts and ends on it.
	must(t, Defer, d, DeferInput{IDs: []string{task}, UntilEnd: trip})
	if _, tk := taskDeferral(t, d, task); tk.DeferUntil != "2026-12-02" {
		t.Fatalf("due only: %+v", tk.Deferral)
	}
}
