package ops

import (
	"slices"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
)

// attendFixture is a vault at 2026-11-05 12:00 UTC with a person and a goal.
func attendFixture(t *testing.T) (Deps, string, string) {
	t.Helper()
	old := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = old })
	d := newDepsAt(t, time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC), "a1")
	seedOrg(t, d)
	proj := must(t, ProjectAdd, d, ProjectAddInput{DoneWhen: "Done.", Title: "Portal"}).ID
	return d, proj, "person-ada-example"
}

func addMeeting(t *testing.T, d Deps, title, person, start, end string, optional bool) string {
	t.Helper()
	return must(t, MeetingAdd, d, MeetingAddInput{Title: title, With: []string{person}, Start: start, End: end, Optional: optional}).ID
}

func orderIDs(t *testing.T, d Deps) map[string]ValueRow {
	t.Helper()
	o, err := Order(ctx, d.Store)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]ValueRow{}
	for _, r := range o.Rows {
		out[r.ID] = r
	}
	return out
}

func healthOf(t *testing.T, d Deps, id string) []string {
	t.Helper()
	h, err := Health(ctx, d.Store)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, g := range h.Groups {
		for _, f := range g.Findings {
			if f.ID == id {
				kinds = append(kinds, f.Kind)
			}
		}
	}
	return kinds
}

func nextIDs(t *testing.T, d Deps) []string {
	t.Helper()
	var ids []string
	for _, x := range must(t, Next, d, NextInput{}).Tasks {
		ids = append(ids, x.ID)
	}
	return ids
}

// A recurrence passes optional on and starts attend from the series default,
// never from the answer of the occurrence before.
func TestMeetingAttendRecurrence(t *testing.T) {
	d, proj, person := attendFixture(t)
	doc, _ := recurSlotTask(t, d, proj, &model.Task{Kind: model.KindMeeting, Occasion: model.Occasion{Scheduled: "2026-11-05T09:00:00Z", End: "2026-11-05T09:30:00Z", With: []model.Link{model.LinkTo(person)}}})
	yes := true
	out := must(t, MeetingAttend, d, MeetingAttendInput{ID: doc.ID, Attend: "yes", Optional: &yes})
	if out.Previous.Optional || out.Previous.Attend != "" || !out.Now.Optional || out.Now.Attend != model.AttendYes || out.Now.AttendDefault != "" {
		t.Fatalf("meeting_attend = %+v", out)
	}
	must(t, MeetingHold, d, MeetingHoldInput{ID: doc.ID, Notes: "Went."})
	n1 := nextOf(t, d, doc.ID)
	if !n1.Optional || n1.Attend != "" || n1.AttendDefault != "" {
		t.Fatalf("next = optional %v attend %q default %q; want optional, attend and default unset", n1.Optional, n1.Attend, n1.AttendDefault)
	}
	id1, _ := successor(ctx, d, doc.ID)
	must(t, MeetingAttend, d, MeetingAttendInput{ID: id1, Attend: "no", AllFuture: true})
	must(t, MeetingCancel, d, MeetingCancelInput{ID: id1, Reason: "Skipped."})
	n2 := nextOf(t, d, id1)
	if !n2.Optional || n2.Attend != model.AttendNo || n2.AttendDefault != model.AttendNo {
		t.Fatalf("after all_future: optional %v attend %q default %q; want optional, no, no", n2.Optional, n2.Attend, n2.AttendDefault)
	}
	// One answer without all_future leaves the default for the one after.
	id2, _ := successor(ctx, d, id1)
	must(t, MeetingAttend, d, MeetingAttendInput{ID: id2, Attend: "yes"})
	must(t, MeetingHold, d, MeetingHoldInput{ID: id2, Notes: "Went after all."})
	if n3 := nextOf(t, d, id2); n3.Attend != model.AttendNo {
		t.Errorf("third next attend = %q; want the default no", n3.Attend)
	}
	// Bad input.
	for _, in := range []MeetingAttendInput{{ID: doc.ID}, {ID: id2, Attend: "perhaps"}, {ID: id2, AllFuture: true, Optional: &yes}} {
		if _, err := MeetingAttend.Run(ctx, d, in); err == nil {
			t.Errorf("meeting_attend %+v: no error", in)
		}
	}
}

// An optional meeting the owner has not said they go to asks nothing: no
// health finding, no order row, not in next. Attend yes brings it back.
func TestOptionalMeetingAsksNothing(t *testing.T) {
	d, _, person := attendFixture(t)
	passed := addMeeting(t, d, "Optional sync", person, "2026-11-05T09:00:00Z", "2026-11-05T09:30:00Z", true)
	required := addMeeting(t, d, "Required sync", person, "2026-11-05T10:00:00Z", "2026-11-05T10:30:00Z", false)
	rows := orderIDs(t, d)
	if _, ok := rows[passed]; ok {
		t.Errorf("optional meeting is in the order: %+v", rows[passed])
	}
	if _, ok := rows[required]; !ok {
		t.Errorf("required meeting with attend unset is not in the order")
	}
	if k := healthOf(t, d, passed); len(k) > 0 {
		t.Errorf("optional meeting has health findings %v", k)
	}
	if slices.Contains(nextIDs(t, d), passed) {
		t.Errorf("optional meeting is in next")
	}
	// The agenda lists it apart, in optional: it is not an item and does not
	// count (needs_you).
	ag := must(t, Agenda, d, AgendaInput{Days: 1, Zone: "UTC"})
	if slices.ContainsFunc(ag.Items, func(x AgendaItem) bool { return x.ID == passed }) {
		t.Errorf("optional meeting is in the agenda items: %+v", ag.Items)
	}
	i := slices.IndexFunc(ag.Optional, func(x AgendaItem) bool { return x.ID == passed })
	if i < 0 || !ag.Optional[i].Optional || ag.Optional[i].NeedsYou {
		t.Errorf("agenda optional lacks the optional meeting: %+v", ag.Optional)
	}
	// maybe on an optional meeting still asks nothing.
	must(t, MeetingAttend, d, MeetingAttendInput{ID: passed, Attend: "maybe"})
	if _, ok := orderIDs(t, d)[passed]; ok {
		t.Errorf("optional meeting with attend maybe is in the order")
	}
	must(t, MeetingAttend, d, MeetingAttendInput{ID: passed, Attend: "yes"})
	if _, ok := orderIDs(t, d)[passed]; !ok {
		t.Errorf("attend yes: the meeting is not in the order")
	}
	if k := healthOf(t, d, passed); !slices.Contains(k, HealthTaskNoDoneWhen) {
		t.Errorf("attend yes: health findings %v; want the ones a required meeting has", k)
	}
	if !slices.Contains(nextIDs(t, d), passed) {
		t.Errorf("attend yes: the meeting is not in next")
	}
}

// attend no takes a meeting off the agenda and frees what waits on it. While
// an optional meeting has no answer, what waits on it waits (not needs_you).
func TestSkippedMeetingFreesDependents(t *testing.T) {
	d, proj, person := attendFixture(t)
	m := addMeeting(t, d, "Planning", person, "2026-11-09T10:00:00Z", "2026-11-09T11:00:00Z", true)
	task := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Write the plan", DoneWhen: "The plan is shared."}).ID
	must(t, TaskDependsAdd, d, TaskDependsInput{ID: task, DependsOn: []string{m}})
	if r, ok := orderIDs(t, d)[task]; ok {
		t.Fatalf("task waiting on an undecided optional meeting is listed: %+v", r)
	}
	must(t, MeetingAttend, d, MeetingAttendInput{ID: m, Attend: "no"})
	rows := orderIDs(t, d)
	if r := rows[task]; r.Bucket != BucketNow {
		t.Errorf("task after the meeting was skipped = %+v; want bucket now", r)
	}
	if _, ok := rows[m]; ok {
		t.Errorf("skipped meeting is in the order")
	}
	ag := must(t, Agenda, d, AgendaInput{Days: 7, Zone: "UTC"})
	if slices.ContainsFunc(ag.Items, func(x AgendaItem) bool { return x.ID == m }) {
		t.Errorf("skipped meeting is in the agenda items")
	}
	if !slices.ContainsFunc(ag.Skipped, func(x AgendaItem) bool { return x.ID == m }) {
		t.Errorf("skipped meeting is not in agenda skipped: %+v", ag.Skipped)
	}
	if k := healthOf(t, d, m); len(k) > 0 {
		t.Errorf("skipped meeting has health findings %v", k)
	}
	if _, err := TaskStart.Run(ctx, d, TaskStartInput{ID: task}); err != nil {
		t.Errorf("task_start on a task that waits on a skipped meeting: %v", err)
	}
	// A cancelled meeting is still ended by meeting_cancel.
	if o := must(t, MeetingCancel, d, MeetingCancelInput{ID: m}); o.Status != model.MeetingCancelled {
		t.Errorf("cancel = %+v", o)
	}
	if _, err := MeetingAttend.Run(ctx, d, MeetingAttendInput{ID: m, Attend: "yes"}); err == nil {
		t.Errorf("meeting_attend on a cancelled meeting: no error")
	}
}

// calendar_compare names a matched meeting whose optional flag differs from
// the calendar's, and passes optional on a calendar-only event.
func TestCalendarCompareOptional(t *testing.T) {
	d, _, person := attendFixture(t)
	m := addMeeting(t, d, "Design review", person, "2026-11-06T10:00:00Z", "2026-11-06T11:00:00Z", false)
	yes, no := true, false
	out := must(t, CalendarCompare, d, CalendarCompareInput{Zone: "UTC", Events: []CalendarEventInput{
		{Title: "Design review", Start: "2026-11-06T10:00:00Z", End: "2026-11-06T11:00:00Z", Attendees: []string{"Ada"}, Optional: &yes},
		{Title: "All hands", Start: "2026-11-07T10:00:00Z", End: "2026-11-07T11:00:00Z", Attendees: []string{"Everyone"}, Optional: &yes},
		{Title: "Lunch talk", Start: "2026-11-08T10:00:00Z", End: "2026-11-08T11:00:00Z", Attendees: []string{"Everyone"}, Optional: &no},
	}})
	if len(out.Optional) != 1 || out.Optional[0].ID != m || !out.Optional[0].Calendar || out.Optional[0].Para {
		t.Errorf("optional = %+v", out.Optional)
	}
	for _, c := range out.CalendarOnly {
		if want := c.Title == "All hands"; c.Optional != want {
			t.Errorf("calendar only %s optional = %v", c.Title, c.Optional)
		}
	}
}
