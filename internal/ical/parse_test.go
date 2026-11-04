package ical

import (
	"errors"
	"strings"
	"testing"
	"time"
)

const sampleICS = "BEGIN:VCALENDAR\r\n" +
	"VERSION:2.0\r\n" +
	"PRODID:-//acme.example//test//EN\r\n" +
	"BEGIN:VEVENT\r\n" +
	"UID:standup@acme.example\r\n" +
	"SUMMARY:Platform standup\r\n" +
	"DTSTART;TZID=America/New_York:20261102T093000\r\n" +
	"DTEND;TZID=America/New_York:20261102T094500\r\n" +
	"RRULE:FREQ=WEEKLY;BYDAY=MO,WE;COUNT=6\r\n" +
	"EXDATE;TZID=America/New_York:20261104T093000\r\n" +
	"ATTENDEE;CN=Jordan Example:mailto:jordan@acme.example\r\n" +
	"ATTENDEE:mailto:sam@acme.example\r\n" +
	"BEGIN:VALARM\r\n" +
	"TRIGGER:-PT10M\r\n" +
	"SUMMARY:not the event\r\n" +
	"END:VALARM\r\n" +
	"END:VEVENT\r\n" +
	"BEGIN:VEVENT\r\n" +
	"UID:standup@acme.example\r\n" +
	"RECURRENCE-ID;TZID=America/New_York:20261109T093000\r\n" +
	"SUMMARY:Platform standup (moved)\r\n" +
	"DTSTART;TZID=America/New_York:20261109T110000\r\n" +
	"DURATION:PT15M\r\n" +
	"END:VEVENT\r\n" +
	"BEGIN:VEVENT\r\n" +
	"UID:offsite@acme.example\r\n" +
	"SUMMARY:Team offsite\\, day one\r\n" +
	"DTSTART;VALUE=DATE:20261105\r\n" +
	"END:VEVENT\r\n" +
	"BEGIN:VEVENT\r\n" +
	"UID:review@acme.example\r\n" +
	"SUMMARY:Quarterly review with a very long title that the calendar fol\r\n" +
	" ded onto a second line\r\n" +
	"DTSTART:20261103T150000Z\r\n" +
	"DTEND:20261103T160000Z\r\n" +
	"RRULE:FREQ=YEARLY\r\n" +
	"END:VEVENT\r\n" +
	"END:VCALENDAR\r\n"

func TestParseReadsEvents(t *testing.T) {
	ev, warns, err := Parse(sampleICS, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 0 {
		t.Errorf("warnings = %v", warns)
	}
	if len(ev) != 4 {
		t.Fatalf("events = %d", len(ev))
	}
	s := ev[0]
	ny, _ := time.LoadLocation("America/New_York")
	if s.Summary != "Platform standup" || !s.Start.Equal(time.Date(2026, 11, 2, 9, 30, 0, 0, ny)) || s.End.Sub(s.Start) != 15*time.Minute {
		t.Errorf("standup = %+v", s)
	}
	if len(s.Attendees) != 2 || s.Attendees[0] != "Jordan Example <jordan@acme.example>" || s.Attendees[1] != "sam@acme.example" {
		t.Errorf("attendees = %v", s.Attendees)
	}
	if ev[1].RecurrenceID.IsZero() || ev[1].End.Sub(ev[1].Start) != 15*time.Minute {
		t.Errorf("override = %+v", ev[1])
	}
	if !ev[2].AllDay || ev[2].Summary != "Team offsite, day one" || ev[2].End.Sub(ev[2].Start) != 24*time.Hour {
		t.Errorf("all day = %+v", ev[2])
	}
	if !strings.HasSuffix(ev[3].Summary, "folded onto a second line") {
		t.Errorf("folded = %q", ev[3].Summary)
	}
}

func TestExpandWeeklyWithCountAndExdate(t *testing.T) {
	ev, _, err := Parse(sampleICS, time.UTC)
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 1, 0)
	got, err := Expand(ev[0], from, to)
	if err != nil {
		t.Fatal(err)
	}
	// COUNT=6 from Mon 2 Nov: 2, 4, 9, 11, 16, 18; 4 is an EXDATE.
	want := []int{2, 9, 11, 16, 18}
	if len(got) != len(want) {
		t.Fatalf("occurrences = %v", got)
	}
	for i, d := range want {
		if got[i].Day() != d || got[i].Hour() != 9 || got[i].Minute() != 30 {
			t.Errorf("occurrence %d = %v, want day %d 09:30", i, got[i], d)
		}
	}
	if _, err := Expand(ev[3], from, to); !errors.Is(err, ErrUnexpanded) {
		t.Errorf("YEARLY: %v", err)
	}
}

func TestExpandMonthlyAndUntil(t *testing.T) {
	start := time.Date(2026, 1, 6, 14, 0, 0, 0, time.UTC) // a Tuesday
	// The first Tuesday of each month, until April.
	e := VEvent{Start: start, End: start.Add(time.Hour), RRule: "FREQ=MONTHLY;BYDAY=1TU;UNTIL=20260430T235959Z"}
	got, err := Expand(e, start, start.AddDate(1, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	var days []string
	for _, g := range got {
		days = append(days, g.Format("01-02"))
	}
	if strings.Join(days, " ") != "01-06 02-03 03-03 04-07" {
		t.Errorf("first Tuesdays = %v", days)
	}
	// The last Friday, every other month, three times.
	e = VEvent{Start: time.Date(2026, 1, 30, 9, 0, 0, 0, time.UTC), RRule: "FREQ=MONTHLY;INTERVAL=2;BYDAY=-1FR;COUNT=3"}
	e.End = e.Start
	got, _ = Expand(e, e.Start, e.Start.AddDate(1, 0, 0))
	days = days[:0]
	for _, g := range got {
		days = append(days, g.Format("01-02"))
	}
	if strings.Join(days, " ") != "01-30 03-27 05-29" {
		t.Errorf("last Fridays = %v", days)
	}
	// Daily every third day in a window that starts later.
	e = VEvent{Start: start, End: start.Add(time.Hour), RRule: "FREQ=DAILY;INTERVAL=3"}
	got, _ = Expand(e, start.AddDate(0, 0, 5), start.AddDate(0, 0, 12))
	if len(got) != 2 || got[0].Day() != 12 || got[1].Day() != 15 {
		t.Errorf("daily = %v", got)
	}
	for _, bad := range []string{"FREQ=WEEKLY;BYSETPOS=1", "FREQ=HOURLY", "FREQ=WEEKLY;BYDAY=1MO"} {
		if _, err := Expand(VEvent{Start: start, End: start, RRule: bad}, start, start.AddDate(0, 1, 0)); !errors.Is(err, ErrUnexpanded) {
			t.Errorf("%s: %v", bad, err)
		}
	}
}

func TestParseUnknownZoneWarnsAndBadTextFails(t *testing.T) {
	text := "BEGIN:VCALENDAR\nBEGIN:VEVENT\nUID:x\nSUMMARY:Sync\nDTSTART;TZID=Eastern Standard Time:20261104T100000\nEND:VEVENT\nEND:VCALENDAR\n"
	ev, warns, err := Parse(text, time.UTC)
	if err != nil || len(ev) != 1 || len(warns) != 1 || !strings.Contains(warns[0], "Eastern Standard Time") {
		t.Errorf("ev=%v warns=%v err=%v", ev, warns, err)
	}
	if _, _, err := Parse("not a calendar", time.UTC); err == nil {
		t.Error("text with no calendar was accepted")
	}
}
