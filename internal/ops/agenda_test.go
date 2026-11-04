package ops

import (
	"slices"
	"strings"
	"testing"
	"time"

	_ "time/tzdata" // the zones below, on a machine with no zoneinfo

	"github.com/iodesystems/para/internal/egress"
	"github.com/iodesystems/para/internal/model"
)

func utc(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

// agendaVault is a vault whose clock stands at now, with one person.
func agendaVault(t *testing.T, now string) (Deps, string) {
	t.Helper()
	d := newDepsAt(t, utc(now), "m1")
	seedOrg(t, d)
	return d, "person-ada-example"
}

func meet(t *testing.T, d Deps, who, title, start, end string) string {
	t.Helper()
	return must(t, MeetingAdd, d, MeetingAddInput{Title: title, With: []string{who}, Start: start, End: end}).ID
}

func agendaOf(t *testing.T, d Deps, in AgendaInput) AgendaOutput {
	t.Helper()
	return must(t, Agenda, d, in)
}

func ids(items []AgendaItem) []string {
	out := []string{}
	for _, it := range items {
		out = append(out, it.ID)
	}
	return out
}

// A meeting at 01:00 UTC on Nov 5 is the evening of Nov 4 in New York, and the
// task due Nov 4 is overdue or not depending on whose day it is.
func TestAgendaDaysFollowTheZone(t *testing.T) {
	d, ada := agendaVault(t, "2026-11-05T03:00:00Z") // 22:00 on Nov 4 in New York, 04:00 on Nov 5 in Berlin
	m := meet(t, d, ada, "Late call", "2026-11-05T01:00:00Z", "2026-11-05T02:00:00Z")
	proj := must(t, ProjectAdd, d, ProjectAddInput{DoneWhen: "Done.", Title: "Portal"}).ID
	due := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "File the form", Due: "2026-11-04"}).ID

	ny := agendaOf(t, d, AgendaInput{Zone: "America/New_York", Days: 2})
	if ny.From != "2026-11-04" || ny.To != "2026-11-05" || ny.Zone != "America/New_York" {
		t.Errorf("window = %s..%s %s", ny.From, ny.To, ny.Zone)
	}
	byID := map[string]AgendaItem{}
	for _, it := range ny.Items {
		byID[it.ID] = it
	}
	if got := byID[m]; got.Date != "2026-11-04" || got.Time != "20:00" || got.At != "2026-11-04T20:00:00-05:00" || got.Until != "2026-11-04T21:00:00-05:00" {
		t.Errorf("meeting in New York = %+v", got)
	}
	if byID[due].Overdue {
		t.Errorf("due Nov 4 is today in New York, not overdue: %+v", byID[due])
	}

	be := agendaOf(t, d, AgendaInput{Zone: "Europe/Berlin", Days: 2})
	byID = map[string]AgendaItem{}
	for _, it := range be.Items {
		byID[it.ID] = it
	}
	if got := byID[m]; got.Date != "2026-11-05" || got.Time != "02:00" {
		t.Errorf("meeting in Berlin = %+v", got)
	}
	// 01:00 UTC on Nov 5 is 02:00 CET, before "now" (04:00 CET) but in the
	// window, which starts at Berlin midnight.
	if !byID[due].Overdue || byID[due].Date != "2026-11-04" {
		t.Errorf("due Nov 4 is overdue in Berlin: %+v", byID[due])
	}
	if be.From != "2026-11-05" {
		t.Errorf("Berlin from = %s", be.From)
	}
}

// Midnight is taken on the calendar date, so a 25-hour day (clocks go back) and
// a 23-hour day (clocks go forward) both hold the right meetings, and two
// meetings that read 01:30 on the clock keep their real order.
func TestAgendaAcrossClockChanges(t *testing.T) {
	t.Run("fall back", func(t *testing.T) {
		d, ada := agendaVault(t, "2026-11-01T12:00:00Z") // Nov 1 in New York, the day clocks go back
		first := meet(t, d, ada, "First 1:30", "2026-11-01T01:30:00-04:00", "2026-11-01T01:45:00-04:00")
		second := meet(t, d, ada, "Second 1:30", "2026-11-01T01:30:00-05:00", "2026-11-01T01:45:00-05:00")
		inside := meet(t, d, ada, "Last minute", "2026-11-02T23:30:00-05:00", "2026-11-02T23:59:00-05:00") // 04:30Z Nov 3
		outside := meet(t, d, ada, "Next day", "2026-11-03T00:30:00-05:00", "2026-11-03T01:00:00-05:00")
		got := agendaOf(t, d, AgendaInput{Zone: "America/New_York", Days: 2})
		if want := []string{first, second, inside}; !slices.Equal(ids(got.Items), want) {
			t.Errorf("items = %v, want %v (outside = %s)", ids(got.Items), want, outside)
		}
		if got.Items[0].Time != "01:30" || got.Items[1].Time != "01:30" {
			t.Errorf("times = %s %s", got.Items[0].Time, got.Items[1].Time)
		}
		if got.Items[0].At != "2026-11-01T01:30:00-04:00" || got.Items[1].At != "2026-11-01T01:30:00-05:00" {
			t.Errorf("offsets = %s %s", got.Items[0].At, got.Items[1].At)
		}
		if got.To != "2026-11-02" {
			t.Errorf("to = %s", got.To)
		}
	})
	t.Run("spring forward", func(t *testing.T) {
		d, ada := agendaVault(t, "2026-03-08T12:00:00Z") // Mar 8 in New York, the day clocks go forward
		before := meet(t, d, ada, "Before the gap", "2026-03-08T01:30:00-05:00", "2026-03-08T01:45:00-05:00")
		after := meet(t, d, ada, "After the gap", "2026-03-08T03:30:00-04:00", "2026-03-08T03:45:00-04:00")
		lastOfDay := meet(t, d, ada, "Edge in", "2026-03-08T23:59:00-04:00", "2026-03-09T00:30:00-04:00")
		firstNext := meet(t, d, ada, "Edge out", "2026-03-09T00:00:00-04:00", "2026-03-09T00:30:00-04:00")
		got := agendaOf(t, d, AgendaInput{Zone: "America/New_York", Days: 1})
		if want := []string{before, after, lastOfDay}; !slices.Equal(ids(got.Items), want) {
			t.Errorf("items = %v, want %v (firstNext = %s)", ids(got.Items), want, firstNext)
		}
		// 01:30 EST and 03:30 EDT are one hour apart.
		a, _ := time.Parse(time.RFC3339, got.Items[0].At)
		b, _ := time.Parse(time.RFC3339, got.Items[1].At)
		if b.Sub(a) != time.Hour {
			t.Errorf("gap = %v", b.Sub(a))
		}
	})
	t.Run("a meeting that began before the window and still runs", func(t *testing.T) {
		d, ada := agendaVault(t, "2026-11-05T09:00:00Z")
		running := meet(t, d, ada, "Long workshop", "2026-11-04T23:00:00Z", "2026-11-05T01:00:00Z")
		ended := meet(t, d, ada, "Ended", "2026-11-04T20:00:00Z", "2026-11-04T23:00:00Z")
		got := agendaOf(t, d, AgendaInput{Zone: "UTC", Days: 1})
		if want := []string{running}; !slices.Equal(ids(got.Items), want) {
			t.Errorf("items = %v, want %v (ended = %s)", ids(got.Items), want, ended)
		}
		if got.Items[0].Date != "2026-11-04" {
			t.Errorf("date = %s: a meeting is on the day it starts", got.Items[0].Date)
		}
	})
}

func TestAgendaKindsAndOrder(t *testing.T) {
	d, ada := agendaVault(t, "2026-11-02T14:00:00Z")
	proj := must(t, ProjectAdd, d, ProjectAddInput{DoneWhen: "Done.", Title: "Portal"}).ID
	task := func(title, due string, extra TaskAddInput) string {
		extra.Project, extra.Title, extra.Due = proj, title, due
		return must(t, TaskAdd, d, extra).ID
	}
	late := task("Late one", "2026-10-30", TaskAddInput{})
	today := task("Today one", "2026-11-02", TaskAddInput{})
	soon := task("Soon one", "2026-11-04", TaskAddInput{})
	far := task("Far one", "2026-12-25", TaskAddInput{})
	others := task("Theirs", "2026-11-03", TaskAddInput{Assignee: ada})
	nodue := task("No date", "", TaskAddInput{})
	done := task("Done one", "2026-11-03", TaskAddInput{})
	must(t, TaskDone, d, TaskDoneInput{ID: done})
	rfx := must(t, RFXAdd, d, RFXAddInput{Direction: model.RFXOut, Ask: "Chase the audit", Route: "domain-logging", SelfServe: "x", NextNudge: "2026-11-03",
		Targets: []TargetInput{{To: ada, Medium: model.MediumEmail}}}).ID
	closed := must(t, RFXAdd, d, RFXAddInput{Direction: model.RFXOut, Ask: "Old", Route: "domain-logging", SelfServe: "x", NextNudge: "2026-11-03"}).ID
	must(t, RFXClose, d, RFXCloseInput{ID: closed})
	createDoc(t, d, model.Document{ID: "policy-x", Folder: model.FolderPolicies, Title: "Policy X", Meta: &model.Policy{Common: model.Common{Type: model.TypePolicy}, Source: src()}})
	exc := must(t, ExceptionAdd, d, ExceptionAddInput{Policy: "policy-x", Kind: model.ExceptionBug, By: "Jordan", ReviewBy: "2026-11-03"}).ID
	m1 := meet(t, d, ada, "Standup", "2026-11-03T15:00:00Z", "2026-11-03T15:15:00Z")
	m0 := meet(t, d, ada, "Early", "2026-11-03T09:00:00Z", "2026-11-03T09:30:00Z")
	asked := must(t, MeetingAdd, d, MeetingAddInput{Title: "Not yet", With: []string{ada}}).ID

	got := agendaOf(t, d, AgendaInput{Zone: "UTC", Days: 7})
	// Nov 3 holds the nudge, the exception, then the meetings by time; Nov 4 the task.
	want := []string{late, today, rfx, exc, m0, m1, soon}
	if !slices.Equal(ids(got.Items), want) {
		t.Errorf("order = %v\n want  %v\n (far %s, others %s, nodue %s, closed %s, asked %s excluded)", ids(got.Items), want, far, others, nodue, closed, asked)
	}
	byID := map[string]AgendaItem{}
	for _, it := range got.Items {
		byID[it.ID] = it
	}
	if !byID[late].Overdue || byID[today].Overdue || byID[soon].Overdue {
		t.Errorf("overdue flags wrong: %+v %+v %+v", byID[late], byID[today], byID[soon])
	}
	if it := byID[rfx]; it.Kind != AgendaNudge || len(it.WhoNames) != 1 || it.WhoNames[0] != "Ada Example" {
		t.Errorf("nudge = %+v", it)
	}
	if it := byID[m1]; it.Kind != AgendaMeeting || it.WhoNames[0] != "Ada Example" || it.Time != "15:00" {
		t.Errorf("meeting = %+v", it)
	}
	if it := byID[exc]; it.Kind != AgendaException || it.Detail != "bug" {
		t.Errorf("exception = %+v", it)
	}
	// One day: only today's, and what is overdue.
	one := agendaOf(t, d, AgendaInput{Zone: "UTC", Days: 1})
	if want := []string{late, today}; !slices.Equal(ids(one.Items), want) {
		t.Errorf("one day = %v, want %v", ids(one.Items), want)
	}
}

func TestAgendaInputs(t *testing.T) {
	d, _ := agendaVault(t, "2026-11-02T14:00:00Z")
	for _, c := range []struct {
		in   AgendaInput
		want string
	}{
		{AgendaInput{Days: -1}, "negative"},
		{AgendaInput{Days: 400}, "over 366"},
		{AgendaInput{Zone: "Mars/Olympus"}, "IANA zone"},
	} {
		if _, err := Agenda.Run(ctx, d, c.in); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%+v: err = %v, want %q", c.in, err, c.want)
		}
	}
	empty := agendaOf(t, d, AgendaInput{})
	if empty.Items == nil || len(empty.Items) != 0 || empty.To != "2026-11-08" {
		t.Errorf("empty agenda = %+v", empty)
	}
}

func TestCalendarExport(t *testing.T) {
	d, ada := agendaVault(t, "2026-11-02T14:00:00Z")
	proj := must(t, ProjectAdd, d, ProjectAddInput{DoneWhen: "Done.", Title: "Portal"}).ID
	must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "File the form, then; call", Due: "2026-11-05"})
	must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Overdue one", Due: "2026-10-01"})
	must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Not mine", Due: "2026-11-05", Assignee: ada})
	long := "Quarterly review of the portal launch plan with a very long title that has to fold over a line, with \u00e9\u00e8\u00ea\u00eb accents to cross a fold boundary"
	meet(t, d, ada, long, "2026-11-04T10:00:00-05:00", "2026-11-04T10:30:00-05:00")
	must(t, MeetingAdd, d, MeetingAddInput{Title: "Requested only", With: []string{ada}})

	out := must(t, CalendarExport, d, CalendarExportInput{Days: 30})
	if out.Ext != "ics" || out.To != "stdout" || out.Destination != "export:stdout" || !out.EgressAllowed || out.Events != 2 {
		t.Fatalf("out = %+v", out)
	}
	txt := out.Text
	if !strings.HasPrefix(txt, "BEGIN:VCALENDAR\r\n") || !strings.HasSuffix(txt, "END:VCALENDAR\r\n") {
		t.Errorf("not a calendar:\n%q", txt)
	}
	if strings.Contains(strings.ReplaceAll(txt, "\r\n", ""), "\n") {
		t.Error("a bare LF in the feed")
	}
	for _, l := range strings.Split(strings.TrimSuffix(txt, "\r\n"), "\r\n") {
		if len(l) > 75 {
			t.Errorf("line over 75 octets (%d): %q", len(l), l)
		}
	}
	// Unfold, then look for the properties.
	flat := strings.ReplaceAll(txt, "\r\n ", "")
	for _, want := range []string{
		"DTSTART:20261104T150000Z", "DTEND:20261104T153000Z", // 10:00 -05:00
		"SUMMARY:" + strings.ReplaceAll(long, ",", `\,`),
		"DESCRIPTION:With: Ada Example",
		"DTSTART;VALUE=DATE:20261105", "DTEND;VALUE=DATE:20261106",
		`SUMMARY:Due: File the form\, then\; call`,
		"UID:", "@acme.para", "X-PARA-MARKING:UNMARKED", "DTSTAMP:20261102T140000Z",
	} {
		if !strings.Contains(flat, want) {
			t.Errorf("feed lacks %q:\n%s", want, flat)
		}
	}
	for _, not := range []string{"Overdue one", "Not mine", "Requested only"} {
		if strings.Contains(flat, not) {
			t.Errorf("feed holds %q:\n%s", not, flat)
		}
	}
	// The export is an egress event, with the transport and no content.
	evs, err := d.Store.Events(ctx, 20)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range evs {
		if e.Op == egress.Op && e.To == "export:stdout" && e.Doc == calendarDoc {
			found = true
		}
		if strings.Contains(e.From+e.To+e.Doc, "File the form") {
			t.Errorf("event holds content: %+v", e)
		}
	}
	if !found {
		t.Errorf("no egress event: %+v", evs)
	}

	// The vault's export allowlist applies, and a window past a limit is refused.
	if _, err := CalendarExport.Run(ctx, d, CalendarExportInput{To: "wormhole", Days: 400}); err == nil {
		t.Error("days over the limit must fail")
	}
	if _, err := CalendarExport.Run(ctx, d, CalendarExportInput{To: "ftp"}); err == nil {
		t.Error("an unknown transport must fail")
	}
	if _, err := CalendarExport.Run(ctx, d, CalendarExportInput{Zone: "Nowhere/Land"}); err == nil {
		t.Error("an unknown zone must fail")
	}
	if got := must(t, CalendarExport, d, CalendarExportInput{Days: 1}); got.Events != 0 || strings.Contains(got.Text, "VEVENT") {
		t.Errorf("a window with nothing in it holds events: %+v", got)
	}
}
