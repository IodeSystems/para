package ops

import (
	"slices"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
)

// Tick marks a past optional meeting skipped when the owner made no attend
// choice, with a receipt per meeting (actor tick) that undoes it; an undo
// stands. Required meetings, an answered yes or no, and a meeting not yet
// ended are left alone. The clock is fixed and moved by hand.
func TestTickSkipsPastOptionalMeetings(t *testing.T) {
	tv := newTickVault(t, "2026-11-02T09:00:00Z", "request.runner=ia")
	d := tv.open("ia", "cli")
	meeting := func(title string, optional bool, attend model.Attend) string {
		t.Helper()
		id := must(t, MeetingAdd, d, MeetingAddInput{Title: title, Attendees: []string{"sam@acme.example"}, Start: "2026-11-02T10:00:00Z", End: "2026-11-02T10:30:00Z", Optional: optional}).ID
		if attend != "" {
			must(t, MeetingAttend, d, MeetingAttendInput{ID: id, Attend: string(attend)})
		}
		return id
	}
	unset := meeting("Optional sync", true, "")
	maybe := meeting("Optional demo", true, model.AttendMaybe)
	yes := meeting("Optional review", true, model.AttendYes)
	required := meeting("Planning", false, "")
	attendOf := func(id string) model.Attend {
		t.Helper()
		_, tk, err := getTask(ctx, d.Store, id)
		if err != nil {
			t.Fatal(err)
		}
		return tk.Attend
	}

	// Before the end: nothing.
	if out := tick(t, d); len(out.MeetingsSkipped) != 0 {
		t.Fatalf("before the end: %v", out.MeetingsSkipped)
	}
	tv.now = tv.now.Add(2 * time.Hour)
	out := tick(t, d)
	if want := sorted(maybe, unset); !slices.Equal(out.MeetingsSkipped, want) {
		t.Fatalf("skipped = %v, want %v", out.MeetingsSkipped, want)
	}
	if attendOf(unset) != model.AttendNo || attendOf(maybe) != model.AttendNo || attendOf(yes) != model.AttendYes || attendOf(required) != "" {
		t.Fatalf("attend: unset %q maybe %q yes %q required %q", attendOf(unset), attendOf(maybe), attendOf(yes), attendOf(required))
	}
	// One receipt per meeting, actor tick, background.
	list := must(t, ReceiptList, d, ReceiptListInput{Actor: TickSurface})
	var mine []ReceiptView
	for _, r := range list.Items {
		if r.Op == ObligationAttend.Name() {
			mine = append(mine, r)
		}
	}
	if len(mine) != 2 || mine[0].Surface != TickSurface || !mine[0].Background || mine[0].Why != tickSkipWhy {
		t.Fatalf("receipts = %+v", mine)
	}
	// A second pass does nothing more.
	if again := tick(t, d); len(again.MeetingsSkipped) != 0 {
		t.Fatalf("second pass: %v", again.MeetingsSkipped)
	}
	// Undo one: it is back to unset, and tick does not skip it again.
	var rUnset string
	for _, r := range mine {
		if r.Doc == unset {
			rUnset = r.ID
		}
	}
	must(t, ReceiptUndo, d, ReceiptUndoInput{ID: rUnset})
	if attendOf(unset) != "" {
		t.Fatalf("after undo attend = %q", attendOf(unset))
	}
	if again := tick(t, d); len(again.MeetingsSkipped) != 0 || attendOf(unset) != "" {
		t.Fatalf("tick skipped an undone meeting again: %v", again.MeetingsSkipped)
	}
}
