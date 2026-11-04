package ops

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
)

const compareICS = `BEGIN:VCALENDAR
VERSION:2.0
PRODID:-//acme.example//test//EN
BEGIN:VEVENT
UID:standup@acme.example
SUMMARY:Platform standup
DTSTART:20261102T093000Z
DTEND:20261102T094500Z
RRULE:FREQ=WEEKLY;BYDAY=MO,WE
END:VEVENT
BEGIN:VEVENT
UID:review@acme.example
SUMMARY:Design review
DTSTART:20261103T153000Z
DTEND:20261103T163000Z
END:VEVENT
BEGIN:VEVENT
UID:ada@acme.example
SUMMARY:Call with Ada
DTSTART:20261105T100500Z
DTEND:20261105T102000Z
END:VEVENT
BEGIN:VEVENT
UID:budget@acme.example
SUMMARY:Budget sync
STATUS:CANCELLED
DTSTART:20261106T090000Z
DTEND:20261106T093000Z
END:VEVENT
BEGIN:VEVENT
UID:one-on-one@acme.example
SUMMARY:1:1 Jordan
DTSTART:20261105T160000Z
DTEND:20261105T163000Z
RRULE:FREQ=WEEKLY
ATTENDEE;CN=Jordan Example:mailto:jordan@acme.example
END:VEVENT
BEGIN:VEVENT
UID:yearly@acme.example
SUMMARY:Annual planning
DTSTART:20261110T140000Z
DTEND:20261110T150000Z
RRULE:FREQ=YEARLY
END:VEVENT
END:VCALENDAR
`

func TestCalendarCompare(t *testing.T) {
	d := newDepsAt(t, time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC), "install-a")
	seedOrg(t, d)
	who := []string{"person-ada-example"}
	standup := must(t, MeetingAdd, d, MeetingAddInput{Title: "Platform standup", With: who, Start: "2026-11-02T09:30:00Z", End: "2026-11-02T09:45:00Z", CalendarUID: "standup@acme.example"}).ID
	review := must(t, MeetingAdd, d, MeetingAddInput{Title: "Design review", With: who, Start: "2026-11-03T15:00:00Z", End: "2026-11-03T16:00:00Z"}).ID
	vendor := must(t, MeetingAdd, d, MeetingAddInput{Title: "Vendor call", With: who, Start: "2026-11-04T13:00:00Z", End: "2026-11-04T13:30:00Z"}).ID
	call := must(t, TaskAdd, d, TaskAddInput{Kind: "call", Title: "Call Ada", With: who, Scheduled: "2026-11-05T10:00:00Z", End: "2026-11-05T10:15:00Z"}).ID
	budget := must(t, MeetingAdd, d, MeetingAddInput{Title: "Budget sync", With: who, Start: "2026-11-06T09:00:00Z", End: "2026-11-06T09:30:00Z"}).ID
	unscheduled := must(t, MeetingAdd, d, MeetingAddInput{Title: "Jordan 1:1", With: who}).ID

	out := must(t, CalendarCompare, d, CalendarCompareInput{ICS: compareICS, From: "2026-11-02", To: "2026-11-12", Zone: "UTC"})
	matched := map[string]string{}
	for _, m := range out.Matched {
		matched[m.ID] = m.MatchedBy
	}
	if matched[standup] != "uid" || matched[call] != "title" || len(out.Matched) != 2 {
		t.Errorf("matched = %+v", out.Matched)
	}
	if len(out.Mismatched) != 1 || out.Mismatched[0].ID != review || out.Mismatched[0].What != "start" || out.Mismatched[0].DeltaMinutes != 30 {
		t.Errorf("mismatched = %+v", out.Mismatched)
	}
	paraOnly := map[string]ParaOnly{}
	for _, p := range out.ParaOnly {
		paraOnly[p.ID] = p
	}
	if p, ok := paraOnly[vendor]; !ok || !p.HasAttendees || !strings.Contains(p.Reason, "no calendar event") {
		t.Errorf("vendor = %+v", p)
	}
	if p := paraOnly[budget]; p.Reason != "cancelled in the calendar" {
		t.Errorf("budget = %+v", p)
	}
	if len(out.ParaOnly) != 2 {
		t.Errorf("para only = %+v", out.ParaOnly)
	}
	var series, oneOnOne int
	for _, c := range out.CalendarOnly {
		switch c.Title {
		case "Platform standup":
			series++
			if c.SeriesInPara != standup || !c.Recurring {
				t.Errorf("standup occurrence = %+v", c)
			}
		case "1:1 Jordan":
			oneOnOne++
			if !slices.Contains(c.ParaMaybe, unscheduled) || len(c.Attendees) != 1 {
				t.Errorf("1:1 = %+v", c)
			}
		case "Annual planning":
		default:
			t.Errorf("calendar only = %+v", c)
		}
	}
	// Standup on 4, 9, and 11 Nov; the 1:1 on 5 and 12 Nov.
	if series != 3 || oneOnOne != 2 {
		t.Errorf("standups %d, 1:1s %d: %+v", series, oneOnOne, out.CalendarOnly)
	}
	if len(out.Unexpanded) != 1 || out.Unexpanded[0].Title != "Annual planning" {
		t.Errorf("unexpanded = %+v", out.Unexpanded)
	}
	if out.Para != 5 || out.Calendar != 9 {
		t.Errorf("para %d calendar %d", out.Para, out.Calendar)
	}

	// Events passed as a list match the same way; a uid on the para side that
	// the calendar lacks is said so. An event with no attendees that overlaps
	// a para meeting is an alias of it, not missing from para.
	ev := must(t, CalendarCompare, d, CalendarCompareInput{From: "2026-11-02", To: "2026-11-02", Zone: "UTC", Events: []CalendarEventInput{
		{UID: "other@acme.example", Title: "Platform standup", Start: "2026-11-02T09:30:00Z", End: "2026-11-02T09:45:00Z"},
	}})
	if len(ev.ParaOnly) != 1 || ev.ParaOnly[0].ID != standup || !strings.Contains(ev.ParaOnly[0].Reason, "standup@acme.example") || len(ev.CalendarOnly) != 0 ||
		len(ev.Aliases) != 1 || ev.Aliases[0].Of != standup || ev.Aliases[0].Note != "alias of Platform standup?" {
		t.Errorf("events = %+v", ev)
	}
	if _, err := CalendarCompare.Run(t.Context(), d, CalendarCompareInput{}); err == nil {
		t.Error("no calendar was accepted")
	}
	if _, err := CalendarCompare.Run(t.Context(), d, CalendarCompareInput{ICS: compareICS, From: "2026-11-05", To: "2026-11-01"}); err == nil {
		t.Error("to before from was accepted")
	}

	// align_scope lists the meetings and calls of the window.
	al := must(t, AlignScope, d, AlignScopeInput{Scope: AlignVault, From: "2026-11-02", To: "2026-11-05"})
	var ids []string
	for _, m := range al.Meetings {
		ids = append(ids, m.ID)
	}
	if !slices.Equal(ids, []string{standup, review, vendor, call}) {
		t.Errorf("align meetings = %v", ids)
	}
	if al.Meetings[0].CalendarUID != "standup@acme.example" || al.Meetings[3].Kind != "call" {
		t.Errorf("align meeting fields = %+v", al.Meetings)
	}
}

func TestCalendarUIDOnlyOnMeetingsAndCalls(t *testing.T) {
	d := newDepsAt(t, time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC), "install-a")
	seedOrg(t, d)
	proj := must(t, ProjectAdd, d, ProjectAddInput{Title: "Portal", DoneWhen: "Done."}).ID
	if _, err := TaskAdd.Run(t.Context(), d, TaskAddInput{Project: proj, Title: "Plain", CalendarUID: "x@acme.example"}); err == nil {
		t.Error("calendar_uid on a plain task was accepted")
	}
	m := must(t, MeetingAdd, d, MeetingAddInput{Title: "Sync", With: []string{"person-ada-example"}})
	must(t, MeetingSchedule, d, MeetingScheduleInput{ID: m.ID, Start: "2026-11-03T10:00:00Z", End: "2026-11-03T10:30:00Z", CalendarUID: "sync@acme.example"})
	_, mt, err := getMeeting(t.Context(), d.Store, m.ID)
	if err != nil || len(mt.t.UIDs()) != 1 || mt.t.UIDs()[0] != (model.CalendarEvent{UID: "sync@acme.example", Role: model.RolePrimary}) {
		t.Errorf("uid after schedule = %+v %v", mt, err)
	}
}

func TestMeetingWithSeveralCalendarEvents(t *testing.T) {
	d := newDepsAt(t, time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC), "install-a")
	seedOrg(t, d)
	m := must(t, MeetingAdd, d, MeetingAddInput{Title: "Vendor review", With: []string{"person-ada-example"},
		Start: "2026-11-03T10:00:00Z", End: "2026-11-03T11:00:00Z", CalendarUID: "invite@acme.example",
		CalendarUIDs: []model.CalendarEvent{{UID: "room@acme.example", Role: "room"}}}).ID
	if _, err := MeetingAdd.Run(t.Context(), d, MeetingAddInput{Title: "Twice", With: []string{"person-ada-example"}, CalendarUID: "a@x",
		CalendarUIDs: []model.CalendarEvent{{UID: "a@x"}}}); err == nil {
		t.Error("a uid given twice was accepted")
	}
	// meeting_schedule adds a prep event and keeps the others.
	must(t, MeetingSchedule, d, MeetingScheduleInput{ID: m, Start: "2026-11-03T10:00:00Z", End: "2026-11-03T11:00:00Z",
		CalendarUIDs: []model.CalendarEvent{{UID: "prep@acme.example", Role: "internal prep"}}})
	_, mt, _ := getMeeting(t.Context(), d.Store, m)
	if got := mt.t.UIDs(); len(got) != 3 || got[0].Role != model.RolePrimary || got[2].Role != "internal prep" {
		t.Fatalf("uids = %+v", got)
	}
	out := must(t, CalendarCompare, d, CalendarCompareInput{From: "2026-11-03", To: "2026-11-03", Zone: "UTC", Events: []CalendarEventInput{
		{UID: "room@acme.example", Title: "Room 4 booking", Start: "2026-11-03T10:00:00Z", End: "2026-11-03T11:00:00Z"},
		{UID: "prep@acme.example", Title: "Prep", Start: "2026-11-03T09:30:00Z", End: "2026-11-03T10:00:00Z"},
		{UID: "hold@acme.example", Title: "Focus hold", Start: "2026-11-03T10:30:00Z", End: "2026-11-03T12:00:00Z"},
		{UID: "lunch@acme.example", Title: "Lunch with Bo", Start: "2026-11-03T12:00:00Z", End: "2026-11-03T13:00:00Z", Attendees: []string{"Bo"}},
	}})
	if len(out.Matched) != 2 || out.Matched[0].MatchedBy != "uid" || len(out.ParaOnly) != 0 {
		t.Fatalf("matched = %+v para only = %+v", out.Matched, out.ParaOnly)
	}
	if len(out.Aliases) != 1 || out.Aliases[0].UID != "hold@acme.example" || out.Aliases[0].Of != m {
		t.Fatalf("aliases = %+v", out.Aliases)
	}
	if len(out.CalendarOnly) != 1 || out.CalendarOnly[0].UID != "lunch@acme.example" {
		t.Fatalf("calendar only = %+v", out.CalendarOnly)
	}
}
