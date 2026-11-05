package ops

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
	"unicode"

	"github.com/iodesystems/para/internal/ical"
	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// Calendar compare. The owner's calendar is the record of what meetings
// happen; para holds the meetings and calls it tracks work on. An agent reads
// the calendar with its own tools and passes the events, or .ics text, here.
// calendar_compare reads nothing else and writes nothing: the agent fixes each
// difference with the meeting and task ops, which leave receipts.

const (
	// calendarDefaultDays is the window of a compare with no to.
	calendarDefaultDays = 14
	// calendarMaxDays is the longest window.
	calendarMaxDays = 366
	// calendarTolerance is how far apart two starts may be and still be one
	// meeting, when the input names none.
	calendarTolerance = 15 * time.Minute
	// calendarNear is how far apart a start may be for a title match to count
	// as the same meeting at another time, rather than two meetings.
	calendarNear = 36 * time.Hour
	// titleMatch is the share of words two titles must share: two words of
	// three, not one of two.
	titleMatch = 0.6
)

// calItem is a meeting or a call in para, with its time read as an instant.
type calItem struct {
	ID, Title, Kind, Status string
	Start, End              time.Time
	AllDay                  bool
	With, RFX               []string
	UIDs                    []string // calendar events of the item, any role
	Recur                   string
	Project, Area           string
	Cancelled               bool
	Optional                bool // a meeting the owner may skip
}

// paraCalendarItems lists every meeting (either shape) and call in the vault.
// A date-only time is midnight in loc. An item with no time has a zero Start.
func paraCalendarItems(ctx context.Context, s store.Store, loc *time.Location) ([]calItem, error) {
	var out []calItem
	for _, typ := range []model.Type{model.TypeTask, model.TypeMeeting} {
		refs, err := s.List(ctx, store.Filter{Type: typ})
		if err != nil {
			return nil, err
		}
		for _, r := range refs {
			doc, err := s.Get(ctx, r.ID)
			if err != nil {
				return nil, err
			}
			it := calItem{ID: doc.ID, Title: doc.Title}
			var when, end string
			switch m := doc.Meta.(type) {
			case *model.Task:
				k := m.EffectiveKind()
				if k != model.KindMeeting && k != model.KindCall {
					continue
				}
				it.Kind, it.Status, it.Recur = string(k), string(m.Status), m.Recur
				for _, e := range m.UIDs() {
					it.UIDs = append(it.UIDs, e.UID)
				}
				it.With, it.RFX, it.Project, it.Area = targets(m.With), targets(m.RFX), m.Project.Target(), m.Area.Target()
				it.Cancelled, it.Optional = m.Status == model.TaskDropped, m.Optional
				when, end = m.Scheduled, m.End
				if k == model.KindMeeting {
					if mt, ok := readMeeting(doc); ok {
						it.Status = string(mt.Status)
					}
				}
			case *model.Meeting:
				it.Kind, it.Status = string(model.KindMeeting), string(m.Status)
				it.With, it.RFX = targets(m.With), targets(m.RFX)
				it.Cancelled = m.Status == model.MeetingCancelled
				when, end = m.Start, m.End
			default:
				continue
			}
			if t, err := time.Parse(time.RFC3339, when); err == nil {
				it.Start = t
				if e, err := time.Parse(time.RFC3339, end); err == nil {
					it.End = e
				}
			} else if t, err := time.ParseInLocation(dateLayout, when, loc); err == nil {
				it.Start, it.End, it.AllDay = t, t.AddDate(0, 0, 1), true
			}
			out = append(out, it)
		}
	}
	slices.SortStableFunc(out, func(a, b calItem) int { return cmp.Or(a.Start.Compare(b.Start), cmp.Compare(a.ID, b.ID)) })
	return out, nil
}

// inWindow reports whether an item starts in [from, to).
func (it calItem) inWindow(from, to time.Time) bool {
	return !it.Start.IsZero() && !it.Start.Before(from) && it.Start.Before(to)
}

// parseWindowEdge reads a from or to: a date (midnight in loc) or an RFC 3339
// time.
func parseWindowEdge(field, s string, loc *time.Location) (time.Time, error) {
	s = strings.TrimSpace(s)
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if t, err := time.ParseInLocation(dateLayout, s, loc); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("%s %q: want a date YYYY-MM-DD or a time with a zone", field, s)
}

// calendarWindow reads from and to. From defaults to the start of today in
// loc, minus back days; to defaults to days after from.
func calendarWindow(now time.Time, loc *time.Location, from, to string, back, days int) (time.Time, time.Time, error) {
	n := now.In(loc)
	y, m, d := n.Date()
	f := time.Date(y, m, d-back, 0, 0, 0, 0, loc)
	var err error
	if strings.TrimSpace(from) != "" {
		if f, err = parseWindowEdge("from", from, loc); err != nil {
			return f, f, err
		}
	}
	t := f.AddDate(0, 0, days)
	if strings.TrimSpace(to) != "" {
		if t, err = parseWindowEdge("to", to, loc); err != nil {
			return f, t, err
		}
		if len(strings.TrimSpace(to)) == len(dateLayout) {
			t = t.AddDate(0, 0, 1) // a date to includes its day
		}
	}
	if !t.After(f) {
		return f, t, errors.New("to must be after from")
	}
	if t.Sub(f) > calendarMaxDays*24*time.Hour {
		return f, t, fmt.Errorf("the window is over %d days", calendarMaxDays)
	}
	return f, t, nil
}

// titleWordsAll returns the lowercase words of a title, without articles.
func titleWordsAll(s string) []string {
	stop := map[string]bool{"a": true, "an": true, "the": true, "and": true, "with": true, "for": true, "of": true, "to": true, "re": true}
	var out []string
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if !stop[w] && !slices.Contains(out, w) {
			out = append(out, w)
		}
	}
	return out
}

// titleSimilarity is the share of words two titles share, over the longer.
func titleSimilarity(a, b string) float64 {
	wa, wb := titleWordsAll(a), titleWordsAll(b)
	if len(wa) == 0 || len(wb) == 0 {
		return 0
	}
	n := 0
	for _, w := range wa {
		if slices.Contains(wb, w) {
			n++
		}
	}
	return float64(n) / float64(max(len(wa), len(wb)))
}

// CalendarEventInput is one event from the owner's calendar.
type CalendarEventInput struct {
	UID   string `json:"uid,omitempty"`
	Title string `json:"title"`
	Start string `json:"start"`         // RFC 3339 with a zone, or a date for an all-day event
	End   string `json:"end,omitempty"` // RFC 3339 with a zone
	// Attendees are names or addresses, as the calendar shows them.
	Attendees []string `json:"attendees,omitempty"`
	// Recurrence is an RRULE such as FREQ=WEEKLY;BYDAY=TU, when the event is
	// a series rather than one occurrence.
	Recurrence string `json:"recurrence,omitempty"`
	// Status is the calendar's status; cancelled leaves the event out.
	Status string `json:"status,omitempty"`
	// Optional is whether the owner is an optional attendee of the event, as
	// the calendar shows it. Absent is not known.
	Optional *bool `json:"optional,omitempty"`
}

// CalendarCompareInput is the input of calendar_compare.
type CalendarCompareInput struct {
	Events []CalendarEventInput `json:"events,omitempty"`
	// ICS is iCalendar text the caller already holds.
	ICS  string `json:"ics,omitempty"`
	From string `json:"from,omitempty"` // date or RFC 3339; default today
	To   string `json:"to,omitempty"`   // date (included) or RFC 3339; default 14 days after from
	Zone string `json:"zone,omitempty"` // IANA zone for dates and floating times; default this machine's
	// ToleranceMinutes is how far apart two starts may be and still match;
	// default 15.
	ToleranceMinutes int `json:"tolerance_minutes,omitempty"`
}

// CalendarMatch is a para item and a calendar occurrence that agree.
type CalendarMatch struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	UID       string `json:"uid,omitempty"`
	Start     string `json:"start"`
	MatchedBy string `json:"matched_by"` // uid or title
}

// CalendarOnly is a calendar occurrence para does not hold.
type CalendarOnly struct {
	UID       string   `json:"uid,omitempty"`
	Title     string   `json:"title"`
	Start     string   `json:"start"`
	End       string   `json:"end,omitempty"`
	AllDay    bool     `json:"all_day,omitempty"`
	Attendees []string `json:"attendees"`
	Recurring bool     `json:"recurring,omitempty"`
	// SeriesInPara is a para item that matched another occurrence of this
	// series, such as a recurring task that holds only the next one.
	SeriesInPara string `json:"series_in_para,omitempty"`
	// ParaMaybe are para meetings or calls with a similar title that matched
	// nothing: unscheduled, cancelled, or outside the window.
	ParaMaybe []string `json:"para_maybe"`
	// Optional is true when the owner is an optional attendee: add it with
	// meeting_add optional. Attendees pass on as meeting_add attendees.
	Optional bool `json:"optional,omitempty"`
}

// CalendarOptional is a para meeting whose optional flag differs from the
// calendar's: the owner is an optional attendee in one and not the other.
type CalendarOptional struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	UID      string `json:"uid,omitempty"`
	Calendar bool   `json:"calendar"` // the calendar's optional flag
	Para     bool   `json:"para"`     // para's
	Fix      string `json:"fix"`
}

// ParaOnly is a para meeting or call in the window that the calendar lacks.
type ParaOnly struct {
	ID     string   `json:"id"`
	Kind   string   `json:"kind"`
	Title  string   `json:"title"`
	Status string   `json:"status"`
	Start  string   `json:"start"`
	End    string   `json:"end,omitempty"`
	With   []string `json:"with"`
	RFX    []string `json:"rfx"`
	// HasAttendees is whether para knows who it is with, so an invite can
	// be sent from it.
	HasAttendees bool `json:"has_attendees"`
	// CalendarUID is the first of CalendarUIDs, the item's calendar events.
	CalendarUID  string   `json:"calendar_uid,omitempty"`
	CalendarUIDs []string `json:"calendar_uids"`
	Recur        string   `json:"recur,omitempty"`
	Reason       string   `json:"reason"`
}

// CalendarAlias is a calendar occurrence with no attendees that overlaps a
// para meeting or call: likely another event of it (a room booking, a prep
// slot), reported as "alias of X?" rather than as missing from para. Add its
// uid to the item's calendar_uids (meeting_schedule) when it is.
type CalendarAlias struct {
	UID     string `json:"uid,omitempty"`
	Title   string `json:"title"`
	Start   string `json:"start"`
	End     string `json:"end,omitempty"`
	Of      string `json:"of"`
	OfTitle string `json:"of_title"`
	Note    string `json:"note"` // "alias of <title>?"
}

// CalendarMismatch is a para item and a calendar occurrence that are one
// meeting at different times.
type CalendarMismatch struct {
	ID            string `json:"id"`
	Title         string `json:"title"`
	UID           string `json:"uid,omitempty"`
	What          string `json:"what"` // start, end, or all_day
	ParaStart     string `json:"para_start"`
	ParaEnd       string `json:"para_end,omitempty"`
	CalendarStart string `json:"calendar_start"`
	CalendarEnd   string `json:"calendar_end,omitempty"`
	DeltaMinutes  int    `json:"delta_minutes"` // calendar start minus para start
	MatchedBy     string `json:"matched_by"`
}

// CalendarUnexpanded is a recurring event whose rule para did not expand.
// Only its first occurrence, when in the window, takes part in the compare.
type CalendarUnexpanded struct {
	UID   string `json:"uid,omitempty"`
	Title string `json:"title"`
	Start string `json:"start"`
	Rule  string `json:"rule"`
	Why   string `json:"why"`
}

// CalendarCompareOutput is the output of calendar_compare.
type CalendarCompareOutput struct {
	From         string               `json:"from"`
	To           string               `json:"to"`
	Calendar     int                  `json:"calendar"` // occurrences in the window
	Para         int                  `json:"para"`     // para meetings and calls in the window
	Matched      []CalendarMatch      `json:"matched"`
	CalendarOnly []CalendarOnly       `json:"calendar_only"`
	Aliases      []CalendarAlias      `json:"aliases"`
	ParaOnly     []ParaOnly           `json:"para_only"`
	Mismatched   []CalendarMismatch   `json:"mismatched"`
	Unexpanded   []CalendarUnexpanded `json:"unexpanded"`
	// Optional lists matched meetings whose optional flag differs from the
	// calendar's, for events that passed optional.
	Optional []CalendarOptional `json:"optional"`
	Warnings []string           `json:"warnings"`
}

// occ is one calendar occurrence in the window.
type occ struct {
	UID, Title string
	Start, End time.Time
	AllDay     bool
	Attendees  []string
	Recurring  bool
	Cancelled  bool
	Optional   *bool
	used       bool
}

// occurrences expands events into the occurrences in [from, to). An event
// that replaces one occurrence of a series (RECURRENCE-ID) takes its place.
// optional is the optional flag of each event, by index; nil is not known.
func occurrences(events []ical.VEvent, optional []*bool, from, to time.Time) ([]*occ, []CalendarUnexpanded) {
	overrides := map[string][]time.Time{}
	for _, e := range events {
		if !e.RecurrenceID.IsZero() && e.UID != "" {
			overrides[e.UID] = append(overrides[e.UID], e.RecurrenceID)
		}
	}
	var out []*occ
	var unexpanded []CalendarUnexpanded
	for i, e := range events {
		var opt *bool
		if i < len(optional) {
			opt = optional[i]
		}
		starts, err := ical.Expand(e, from, to)
		if err != nil {
			unexpanded = append(unexpanded, CalendarUnexpanded{UID: e.UID, Title: e.Summary, Start: e.Start.Format(time.RFC3339), Rule: e.RRule, Why: err.Error()})
			one := e
			one.RRule = ""
			starts, _ = ical.Expand(one, from, to)
		}
		length := e.End.Sub(e.Start)
		for _, s := range starts {
			if e.RecurrenceID.IsZero() && e.RRule != "" && slices.ContainsFunc(overrides[e.UID], s.Equal) {
				continue
			}
			o := &occ{UID: e.UID, Title: e.Summary, Start: s, AllDay: e.AllDay, Attendees: e.Attendees,
				Recurring: e.RRule != "" || !e.RecurrenceID.IsZero(), Cancelled: e.Cancelled(), Optional: opt}
			if length > 0 {
				o.End = s.Add(length)
			}
			out = append(out, o)
		}
	}
	slices.SortStableFunc(out, func(a, b *occ) int { return cmp.Or(a.Start.Compare(b.Start), cmp.Compare(a.Title, b.Title)) })
	return out, unexpanded
}

// eventsFromInput turns the events of the input into calendar events.
func eventsFromInput(in []CalendarEventInput, loc *time.Location) ([]ical.VEvent, error) {
	var out []ical.VEvent
	for i, e := range in {
		title := strings.TrimSpace(e.Title)
		if title == "" {
			return nil, fmt.Errorf("events[%d]: title is empty", i)
		}
		ev := ical.VEvent{UID: strings.TrimSpace(e.UID), Summary: title, Attendees: e.Attendees, Status: strings.ToUpper(strings.TrimSpace(e.Status))}
		ev.RRule = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(e.Recurrence), "RRULE:"))
		if t, err := time.Parse(time.RFC3339, strings.TrimSpace(e.Start)); err == nil {
			ev.Start = t
		} else if t, err := time.ParseInLocation(dateLayout, strings.TrimSpace(e.Start), loc); err == nil {
			ev.Start, ev.AllDay, ev.End = t, true, t.AddDate(0, 0, 1)
		} else {
			return nil, fmt.Errorf("events[%d] %s: start %q: want RFC 3339 with a zone, or a date", i, title, e.Start)
		}
		if s := strings.TrimSpace(e.End); s != "" && !ev.AllDay {
			t, err := time.Parse(time.RFC3339, s)
			if err != nil {
				return nil, fmt.Errorf("events[%d] %s: end %q: want RFC 3339 with a zone", i, title, e.End)
			}
			if t.Before(ev.Start) {
				return nil, fmt.Errorf("events[%d] %s: end is before start", i, title)
			}
			ev.End = t
		}
		if ev.End.IsZero() {
			ev.End = ev.Start
		}
		out = append(out, ev)
	}
	return out, nil
}

func absDur(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

func sameDay(a, b time.Time, loc *time.Location) bool {
	return a.In(loc).Format(dateLayout) == b.In(loc).Format(dateLayout)
}

// CalendarCompare compares the owner's calendar with para's meetings and
// calls in a window. It writes nothing.
var CalendarCompare = defineRead("calendar_compare", "Compare the owner's calendar with para's meetings and calls in a window (default the next 14 days): pass events you read from the calendar, or .ics text you hold. Lists what the calendar has that para lacks, what para has that the calendar lacks (and whether para knows who it is with), and time mismatches. Matches by calendar_uid, else by title and start. Writes nothing: fix differences with the meeting and task ops", func(ctx context.Context, d Deps, in CalendarCompareInput) (CalendarCompareOutput, error) {
	out := CalendarCompareOutput{Matched: []CalendarMatch{}, CalendarOnly: []CalendarOnly{}, Aliases: []CalendarAlias{}, ParaOnly: []ParaOnly{},
		Mismatched: []CalendarMismatch{}, Unexpanded: []CalendarUnexpanded{}, Optional: []CalendarOptional{}, Warnings: []string{}}
	if len(in.Events) == 0 && strings.TrimSpace(in.ICS) == "" {
		return out, errors.New("give events or ics: the calendar to compare with")
	}
	if in.ToleranceMinutes < 0 {
		return out, errors.New("tolerance_minutes is negative")
	}
	tol := calendarTolerance
	if in.ToleranceMinutes > 0 {
		tol = time.Duration(in.ToleranceMinutes) * time.Minute
	}
	loc, err := agendaZone(in.Zone)
	if err != nil {
		return out, err
	}
	from, to, err := calendarWindow(d.Store.Now(), loc, in.From, in.To, 0, calendarDefaultDays)
	if err != nil {
		return out, err
	}
	out.From, out.To = from.Format(time.RFC3339), to.Format(time.RFC3339)
	events, err := eventsFromInput(in.Events, loc)
	if err != nil {
		return out, err
	}
	if strings.TrimSpace(in.ICS) != "" {
		parsed, warns, err := ical.Parse(in.ICS, loc)
		if err != nil {
			return out, err
		}
		events = append(events, parsed...)
		out.Warnings = append(out.Warnings, warns...)
	}
	// The events of the input come first; ics events after them are not known.
	optional := make([]*bool, len(in.Events))
	for i, e := range in.Events {
		optional[i] = e.Optional
	}
	all, unexpanded := occurrences(events, optional, from, to)
	out.Unexpanded = append(out.Unexpanded, unexpanded...)
	var live, cancelled []*occ
	for _, o := range all {
		if o.Cancelled {
			cancelled = append(cancelled, o)
		} else {
			live = append(live, o)
		}
	}
	out.Calendar = len(live)
	items, err := paraCalendarItems(ctx, d.Store, loc)
	if err != nil {
		return out, err
	}
	var window []calItem
	for _, it := range items {
		if !it.Cancelled && it.inWindow(from, to) {
			window = append(window, it)
		}
	}
	out.Para = len(window)
	// An item with a calendar_uid matches first: it is exact.
	slices.SortStableFunc(window, func(a, b calItem) int {
		return cmp.Compare(boolRank(len(a.UIDs) == 0), boolRank(len(b.UIDs) == 0))
	})
	f := func(t time.Time, allDay bool) string {
		if allDay {
			return t.In(loc).Format(dateLayout)
		}
		return t.In(loc).Format(time.RFC3339)
	}
	fEnd := func(t time.Time, allDay bool) string {
		if t.IsZero() || allDay {
			return ""
		}
		return t.In(loc).Format(time.RFC3339)
	}
	// pick returns the occurrence a para item is, by uid or by title.
	pick := func(it calItem, list []*occ) (*occ, string) {
		var best *occ
		bestSim := 0.0
		by := "title"
		for _, o := range list {
			if o.used {
				continue
			}
			sim := 0.0
			switch {
			case o.UID != "" && slices.Contains(it.UIDs, o.UID):
				sim = 2 // a uid match beats any title
			case len(it.UIDs) > 0 && o.UID != "":
				continue
			default:
				sim = titleSimilarity(it.Title, o.Title)
				if sim < titleMatch {
					continue
				}
			}
			if best == nil || sim > bestSim || sim == bestSim && absDur(o.Start.Sub(it.Start)) < absDur(best.Start.Sub(it.Start)) {
				best, bestSim = o, sim
			}
		}
		if bestSim == 2 {
			by = "uid"
		}
		return best, by
	}
	series := map[string]string{} // calendar uid to the para item that matched it
	matchedPara := map[string]bool{}
	for _, it := range window {
		o, by := pick(it, live)
		if o != nil && by == "title" && absDur(o.Start.Sub(it.Start)) > calendarNear && !sameDay(o.Start, it.Start, loc) {
			o = nil
		}
		if o == nil {
			reason := "no calendar event with a similar title near this time"
			if len(it.UIDs) > 0 {
				reason = "calendar_uids " + strings.Join(it.UIDs, ", ") + ": none is in the calendar in this window"
			}
			if c, _ := pick(it, cancelled); c != nil && (absDur(c.Start.Sub(it.Start)) <= tol || c.UID != "" && slices.Contains(it.UIDs, c.UID)) {
				reason = "cancelled in the calendar"
			}
			out.ParaOnly = append(out.ParaOnly, ParaOnly{ID: it.ID, Kind: it.Kind, Title: it.Title, Status: it.Status,
				Start: f(it.Start, it.AllDay), End: fEnd(it.End, it.AllDay), With: nonNil(it.With), RFX: nonNil(it.RFX),
				HasAttendees: len(it.With) > 0, CalendarUID: firstOf(it.UIDs...), CalendarUIDs: nonNil(it.UIDs), Recur: it.Recur, Reason: reason})
			continue
		}
		o.used, matchedPara[it.ID] = true, true
		if o.Optional != nil && *o.Optional != it.Optional && it.Kind == string(model.KindMeeting) {
			out.Optional = append(out.Optional, CalendarOptional{ID: it.ID, Title: it.Title, UID: o.UID, Calendar: *o.Optional, Para: it.Optional,
				Fix: fmt.Sprintf("meeting_attend {id: %s, optional: %t}", it.ID, *o.Optional)})
		}
		if o.UID != "" {
			series[o.UID] = it.ID
		}
		// The item's other events (a room, a prep slot) near it are its own.
		for _, x := range live {
			if !x.used && x.UID != "" && x.UID != o.UID && slices.Contains(it.UIDs, x.UID) && absDur(x.Start.Sub(o.Start)) <= calendarNear {
				x.used = true
				series[x.UID] = it.ID
				out.Matched = append(out.Matched, CalendarMatch{ID: it.ID, Title: it.Title, UID: x.UID, Start: f(x.Start, x.AllDay), MatchedBy: "uid"})
			}
		}
		mm := CalendarMismatch{ID: it.ID, Title: it.Title, UID: o.UID, ParaStart: f(it.Start, it.AllDay), ParaEnd: fEnd(it.End, it.AllDay),
			CalendarStart: f(o.Start, o.AllDay), CalendarEnd: fEnd(o.End, o.AllDay),
			DeltaMinutes: int(math.Round(o.Start.Sub(it.Start).Minutes())), MatchedBy: by}
		switch {
		case it.AllDay != o.AllDay:
			mm.What = "all_day"
		case it.AllDay:
			if !sameDay(o.Start, it.Start, loc) {
				mm.What = "start"
			}
		case absDur(o.Start.Sub(it.Start)) > tol:
			mm.What = "start"
		case !it.End.IsZero() && !o.End.IsZero() && absDur(o.End.Sub(it.End)) > tol:
			mm.What = "end"
		}
		if mm.What != "" {
			out.Mismatched = append(out.Mismatched, mm)
			continue
		}
		out.Matched = append(out.Matched, CalendarMatch{ID: it.ID, Title: it.Title, UID: o.UID, Start: f(o.Start, o.AllDay), MatchedBy: by})
	}
	for _, o := range live {
		if o.used {
			continue
		}
		co := CalendarOnly{UID: o.UID, Title: o.Title, Start: f(o.Start, o.AllDay), End: fEnd(o.End, o.AllDay), AllDay: o.AllDay,
			Attendees: nonNil(o.Attendees), Recurring: o.Recurring, ParaMaybe: []string{}, Optional: o.Optional != nil && *o.Optional}
		if o.UID != "" {
			co.SeriesInPara = series[o.UID]
		}
		if len(o.Attendees) == 0 && co.SeriesInPara == "" {
			if it, ok := overlapping(o, window); ok {
				out.Aliases = append(out.Aliases, CalendarAlias{UID: o.UID, Title: o.Title, Start: co.Start, End: co.End, Of: it.ID, OfTitle: it.Title,
					Note: "alias of " + it.Title + "?"})
				continue
			}
		}
		for _, it := range items {
			if matchedPara[it.ID] || it.ID == co.SeriesInPara {
				continue
			}
			if o.UID != "" && slices.Contains(it.UIDs, o.UID) || titleSimilarity(it.Title, o.Title) >= titleMatch {
				co.ParaMaybe = append(co.ParaMaybe, it.ID)
			}
		}
		out.CalendarOnly = append(out.CalendarOnly, co)
	}
	slices.SortStableFunc(out.ParaOnly, func(a, b ParaOnly) int { return cmp.Or(cmp.Compare(a.Start, b.Start), cmp.Compare(a.ID, b.ID)) })
	slices.SortStableFunc(out.Matched, func(a, b CalendarMatch) int { return cmp.Or(cmp.Compare(a.Start, b.Start), cmp.Compare(a.ID, b.ID)) })
	slices.SortStableFunc(out.Mismatched, func(a, b CalendarMismatch) int {
		return cmp.Or(cmp.Compare(a.ParaStart, b.ParaStart), cmp.Compare(a.ID, b.ID))
	})
	return out, nil
})

// overlapping finds the para item whose time overlaps the occurrence. An
// end that is not known counts as a minute.
func overlapping(o *occ, items []calItem) (calItem, bool) {
	end := func(s, e time.Time) time.Time {
		if e.After(s) {
			return e
		}
		return s.Add(time.Minute)
	}
	for _, it := range items {
		if it.AllDay || o.AllDay {
			continue
		}
		if o.Start.Before(end(it.Start, it.End)) && it.Start.Before(end(o.Start, o.End)) {
			return it, true
		}
	}
	return calItem{}, false
}

func boolRank(b bool) int {
	if b {
		return 1
	}
	return 0
}
