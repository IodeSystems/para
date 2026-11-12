package ops

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
)

func orderHas(t *testing.T, d Deps, id string) bool {
	t.Helper()
	o, err := Order(ctx, d.Store)
	if err != nil {
		t.Fatal(err)
	}
	return slices.ContainsFunc(o.Rows, func(r ValueRow) bool { return r.ID == id })
}

func agendaHas(t *testing.T, d Deps, id string) bool {
	t.Helper()
	a := must(t, Agenda, d, AgendaInput{Days: 1, Zone: "UTC"})
	return slices.ContainsFunc(a.Items, func(i AgendaItem) bool { return i.ID == id })
}

// A task marked done leaves the value order and the agenda at once: nothing
// reads a stale list.
func TestDoneLeavesAgenda(t *testing.T) {
	d := newDepsAt(t, time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC), "m1")
	proj := must(t, GoalAdd, d, GoalAddInput{DoneWhen: "Done.", Title: "Portal"}).ID
	id := must(t, TaskAdd, d, TaskAddInput{Goal: proj, Title: "File the form", Due: "2026-11-05"}).ID
	if !orderHas(t, d, id) || !agendaHas(t, d, id) {
		t.Fatal("open task due today is not listed")
	}
	must(t, ItemClose, d, ItemCloseInput{ID: id})
	if orderHas(t, d, id) || agendaHas(t, d, id) {
		t.Fatal("done task still listed")
	}
}

// A recurring task finished late does not come back today with the same
// title: its next occurrence is after today.
func TestRecurringDoneLateNotDueToday(t *testing.T) {
	d := newDepsAt(t, time.Date(2026, 11, 5, 12, 0, 0, 0, time.Local), "m1")
	proj := must(t, GoalAdd, d, GoalAddInput{DoneWhen: "Done.", Title: "Portal"}).ID
	id := must(t, TaskAdd, d, TaskAddInput{Goal: proj, Title: "Water plants", Due: "2026-10-01", Recur: "daily"}).ID
	out := must(t, TaskDone, d, TaskDoneInput{ID: id})
	if out.Next == "" {
		t.Fatal("no next occurrence")
	}
	_, nt, _ := getTask(ctx, d.Store, out.Next)
	if nt.Due != "2026-11-06" {
		t.Fatalf("next due = %s, want 2026-11-06", nt.Due)
	}
}

// An ask the owner made closes from the page; one made to the owner needs a
// playbook, so it is refused here; a closed ask leaves the nudges.
func TestItemCloseAsk(t *testing.T) {
	d := newDepsAt(t, time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC), "m1")
	seedOrg(t, d)
	out := addRFX(t, d, RFXAddInput{NextNudge: "2026-11-05"})
	if !agendaHas(t, d, out) {
		t.Fatal("ask due for a nudge is not listed")
	}
	if got := must(t, ItemClose, d, ItemCloseInput{ID: out}); got.By != "rfx_close" {
		t.Fatalf("by = %s", got.By)
	}
	if agendaHas(t, d, out) || orderHas(t, d, out) {
		t.Fatal("closed ask still listed")
	}
	in := addRFX(t, d, RFXAddInput{Direction: model.RFXIn, NextNudge: "2026-11-05"})
	if _, err := ItemClose.Run(ctx, d, ItemCloseInput{ID: in}); err == nil || !strings.Contains(err.Error(), "send it to an agent") {
		t.Fatalf("inbound ask closed: %v", err)
	}
}

// The calendar agenda and the value order list no task put off (until a
// future date, or someday), and no meeting held or cancelled.
func TestAgendaLeavesDeferredAndHeld(t *testing.T) {
	d := newDepsAt(t, time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC), "m1")
	proj := must(t, GoalAdd, d, GoalAddInput{DoneWhen: "Done.", Title: "Portal"}).ID
	add := func(title string) string {
		return must(t, TaskAdd, d, TaskAddInput{Goal: proj, Title: title, Due: "2026-11-05"}).ID
	}
	open, until, someday := add("Open"), add("Until"), add("Someday")
	must(t, Defer, d, DeferInput{IDs: []string{until}, Until: "2026-12-01"})
	must(t, Defer, d, DeferInput{IDs: []string{someday}})
	for id, want := range map[string]bool{open: true, until: false, someday: false} {
		if agendaHas(t, d, id) != want || orderHas(t, d, id) != want {
			t.Errorf("%s: agenda %v, order %v, want %v", id, agendaHas(t, d, id), orderHas(t, d, id), want)
		}
	}
	sam := must(t, OrgPersonAdd, d, OrgAddInput{Name: "Sam Example", Org: "acme.example"})
	m := must(t, MeetingAdd, d, MeetingAddInput{Title: "Board review", With: []string{sam.ID}, Start: "2026-11-05T15:00:00Z", End: "2026-11-05T15:30:00Z"})
	if !agendaHas(t, d, m.ID) || !orderHas(t, d, m.ID) {
		t.Fatal("a meeting today is not listed")
	}
	must(t, MeetingHold, d, MeetingHoldInput{ID: m.ID})
	if agendaHas(t, d, m.ID) || orderHas(t, d, m.ID) {
		t.Fatal("a held meeting is still listed")
	}
}
