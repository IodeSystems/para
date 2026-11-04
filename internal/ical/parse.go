package ical

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// Reading. Parse reads the VEVENTs of iCalendar text a caller already holds,
// such as a file the owner exported or text an agent fetched. It opens
// nothing. Expand lists the occurrences of a recurring event for the common
// rules (FREQ DAILY, WEEKLY, MONTHLY with INTERVAL, COUNT, UNTIL, BYDAY,
// BYMONTHDAY); any other rule is reported as not expanded.

// VEvent is one event as read.
type VEvent struct {
	UID     string
	Summary string
	// Start and End are instants. An all-day event has AllDay set, Start at
	// midnight in the reader's zone, and End the day after unless given.
	Start, End time.Time
	AllDay     bool
	Attendees  []string // "Name <mail>" or the address
	Organizer  string
	Status     string // as written: CONFIRMED, TENTATIVE, CANCELLED, or empty
	RRule      string
	ExDates    []time.Time
	// RecurrenceID is set on an event that replaces one occurrence of a
	// recurring event with the same UID.
	RecurrenceID time.Time
}

// Cancelled reports whether the event's status is CANCELLED.
func (e VEvent) Cancelled() bool { return strings.EqualFold(e.Status, "CANCELLED") }

// prop is one content line: name, parameters, value.
type prop struct {
	name   string
	params map[string]string
	value  string
}

// unfold joins continuation lines (a line starting with a space or a tab).
func unfold(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	var out []string
	for _, l := range strings.Split(text, "\n") {
		if (strings.HasPrefix(l, " ") || strings.HasPrefix(l, "\t")) && len(out) > 0 {
			out[len(out)-1] += l[1:]
			continue
		}
		out = append(out, l)
	}
	return out
}

// parseLine splits NAME;P=V;P="V":value. A colon inside a quoted parameter
// value does not end the name part.
func parseLine(l string) (prop, bool) {
	inQuote := false
	end := -1
	for i, r := range l {
		switch {
		case r == '"':
			inQuote = !inQuote
		case r == ':' && !inQuote:
			end = i
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 {
		return prop{}, false
	}
	head, value := l[:end], l[end+1:]
	parts := splitParams(head)
	p := prop{name: strings.ToUpper(strings.TrimSpace(parts[0])), params: map[string]string{}, value: value}
	for _, kv := range parts[1:] {
		k, v, _ := strings.Cut(kv, "=")
		p.params[strings.ToUpper(k)] = strings.Trim(v, `"`)
	}
	return p, p.name != ""
}

func splitParams(head string) []string {
	var out []string
	inQuote, start := false, 0
	for i, r := range head {
		switch {
		case r == '"':
			inQuote = !inQuote
		case r == ';' && !inQuote:
			out = append(out, head[start:i])
			start = i + 1
		}
	}
	return append(out, head[start:])
}

// Unescape reads a TEXT value.
func Unescape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			i++
			switch s[i] {
			case 'n', 'N':
				b.WriteByte('\n')
			default:
				b.WriteByte(s[i])
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// zoneOf resolves a TZID. A name the system does not know (such as a Windows
// zone name) falls back to def, and the name is returned as a warning.
func zoneOf(tzid string, def *time.Location) (*time.Location, string) {
	if tzid == "" {
		return def, ""
	}
	if loc, err := time.LoadLocation(tzid); err == nil {
		return loc, ""
	}
	return def, tzid
}

// parseTime reads a DATE or DATE-TIME value with its parameters.
func parseTime(p prop, def *time.Location) (t time.Time, allDay bool, warn string, err error) {
	v := strings.TrimSpace(p.value)
	if strings.EqualFold(p.params["VALUE"], "DATE") || len(v) == 8 {
		t, err = time.ParseInLocation("20060102", v, def)
		return t, true, "", err
	}
	if strings.HasSuffix(v, "Z") {
		t, err = time.Parse("20060102T150405Z", v)
		return t, false, "", err
	}
	loc, unknown := zoneOf(p.params["TZID"], def)
	if unknown != "" {
		warn = fmt.Sprintf("zone %q is unknown here; read as %s", unknown, def)
	}
	t, err = time.ParseInLocation("20060102T150405", v, loc)
	return t, false, warn, err
}

var durationRe = regexp.MustCompile(`^([+-])?P(?:(\d+)W)?(?:(\d+)D)?(?:T(?:(\d+)H)?(?:(\d+)M)?(?:(\d+)S)?)?$`)

// parseDuration reads an RFC 5545 DURATION such as PT1H30M or P1D.
func parseDuration(s string) (time.Duration, error) {
	m := durationRe.FindStringSubmatch(strings.ToUpper(strings.TrimSpace(s)))
	if m == nil || s == "P" {
		return 0, fmt.Errorf("duration %q", s)
	}
	n := func(i int) time.Duration { v, _ := strconv.Atoi(m[i]); return time.Duration(v) }
	d := n(2)*7*24*time.Hour + n(3)*24*time.Hour + n(4)*time.Hour + n(5)*time.Minute + n(6)*time.Second
	if m[1] == "-" {
		d = -d
	}
	return d, nil
}

// attendee reads an ATTENDEE or ORGANIZER value as "Name <mail>" or the mail.
func attendee(p prop) string {
	addr := strings.TrimSpace(p.value)
	if len(addr) > 7 && strings.EqualFold(addr[:7], "mailto:") {
		addr = addr[7:]
	}
	if cn := strings.TrimSpace(p.params["CN"]); cn != "" && !strings.EqualFold(cn, addr) {
		if addr == "" {
			return cn
		}
		return cn + " <" + addr + ">"
	}
	return addr
}

// Parse reads the VEVENTs of a calendar. loc is the zone of floating times
// and of all-day dates. Warnings say what was read in a fallback way; an event
// that cannot be read at all is skipped with a warning.
func Parse(text string, loc *time.Location) ([]VEvent, []string, error) {
	if loc == nil {
		loc = time.UTC
	}
	lines := unfold(text)
	var (
		events []VEvent
		warns  []string
		cur    *VEvent
		depth  int // components open inside the current VEVENT (VALARM)
		dur    string
		bad    string
		sawCal bool
	)
	warn := func(s string) {
		if s != "" && !slices.Contains(warns, s) {
			warns = append(warns, s)
		}
	}
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		p, ok := parseLine(l)
		if !ok {
			continue
		}
		switch {
		case p.name == "BEGIN" && strings.EqualFold(p.value, "VCALENDAR"):
			sawCal = true
			continue
		case p.name == "BEGIN" && strings.EqualFold(p.value, "VEVENT") && cur == nil:
			cur, depth, dur, bad = &VEvent{}, 0, "", ""
			continue
		case cur == nil:
			continue
		case p.name == "BEGIN":
			depth++
			continue
		case p.name == "END" && depth > 0:
			depth--
			continue
		case p.name == "END" && strings.EqualFold(p.value, "VEVENT"):
			e := *cur
			cur = nil
			name := e.Summary
			if name == "" {
				name = e.UID
			}
			switch {
			case bad != "":
				warn(fmt.Sprintf("event %q skipped: %s", name, bad))
				continue
			case e.Start.IsZero():
				warn(fmt.Sprintf("event %q skipped: no DTSTART", name))
				continue
			}
			if e.End.IsZero() && dur != "" {
				if d, err := parseDuration(dur); err == nil {
					e.End = e.Start.Add(d)
				} else {
					warn(fmt.Sprintf("event %q: bad DURATION %q", name, dur))
				}
			}
			if e.End.IsZero() && e.AllDay {
				e.End = e.Start.AddDate(0, 0, 1)
			}
			events = append(events, e)
			continue
		}
		if depth > 0 {
			continue
		}
		switch p.name {
		case "UID":
			cur.UID = strings.TrimSpace(p.value)
		case "SUMMARY":
			cur.Summary = strings.TrimSpace(Unescape(p.value))
		case "STATUS":
			cur.Status = strings.ToUpper(strings.TrimSpace(p.value))
		case "RRULE":
			cur.RRule = strings.TrimSpace(p.value)
		case "DURATION":
			dur = p.value
		case "ATTENDEE":
			if a := attendee(p); a != "" {
				cur.Attendees = append(cur.Attendees, a)
			}
		case "ORGANIZER":
			cur.Organizer = attendee(p)
		case "DTSTART", "DTEND", "RECURRENCE-ID":
			t, allDay, w, err := parseTime(p, loc)
			warn(w)
			if err != nil {
				bad = fmt.Sprintf("bad %s %q", p.name, p.value)
				continue
			}
			switch p.name {
			case "DTSTART":
				cur.Start, cur.AllDay = t, allDay
			case "DTEND":
				cur.End = t
			default:
				cur.RecurrenceID = t
			}
		case "EXDATE":
			for _, v := range strings.Split(p.value, ",") {
				q := p
				q.value = v
				t, _, w, err := parseTime(q, loc)
				warn(w)
				if err == nil {
					cur.ExDates = append(cur.ExDates, t)
				}
			}
		}
	}
	if !sawCal && len(events) == 0 {
		return nil, warns, errors.New("ics: no VCALENDAR in the text")
	}
	return events, warns, nil
}

// maxSteps caps how far Expand walks a rule, so an open rule far in the past
// still ends.
const maxSteps = 20000

var weekdays = map[string]time.Weekday{"SU": time.Sunday, "MO": time.Monday, "TU": time.Tuesday, "WE": time.Wednesday, "TH": time.Thursday, "FR": time.Friday, "SA": time.Saturday}

type byDay struct {
	n   int // 0: every; 1, 2, -1: the nth (from the end) in the month
	day time.Weekday
}

type rule struct {
	freq     string
	interval int
	count    int
	until    time.Time
	byDay    []byDay
	byMonDay []int
}

// ErrUnexpanded is a rule Expand does not handle.
var ErrUnexpanded = errors.New("rule not expanded")

func parseRule(s string, loc *time.Location) (rule, error) {
	r := rule{interval: 1}
	for _, part := range strings.Split(s, ";") {
		k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
		k, v = strings.ToUpper(k), strings.ToUpper(v)
		switch k {
		case "":
		case "FREQ":
			r.freq = v
		case "INTERVAL":
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 {
				return r, fmt.Errorf("%w: INTERVAL %q", ErrUnexpanded, v)
			}
			r.interval = n
		case "COUNT":
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 {
				return r, fmt.Errorf("%w: COUNT %q", ErrUnexpanded, v)
			}
			r.count = n
		case "UNTIL":
			t, _, _, err := parseTime(prop{value: v, params: map[string]string{}}, loc)
			if err != nil {
				return r, fmt.Errorf("%w: UNTIL %q", ErrUnexpanded, v)
			}
			if len(v) == 8 {
				t = t.AddDate(0, 0, 1).Add(-time.Second) // the whole last day
			}
			r.until = t
		case "BYDAY":
			for _, d := range strings.Split(v, ",") {
				if len(d) < 2 {
					return r, fmt.Errorf("%w: BYDAY %q", ErrUnexpanded, v)
				}
				wd, ok := weekdays[d[len(d)-2:]]
				if !ok {
					return r, fmt.Errorf("%w: BYDAY %q", ErrUnexpanded, v)
				}
				n := 0
				if num := d[:len(d)-2]; num != "" {
					var err error
					if n, err = strconv.Atoi(strings.TrimPrefix(num, "+")); err != nil || n == 0 || n > 5 || n < -5 {
						return r, fmt.Errorf("%w: BYDAY %q", ErrUnexpanded, v)
					}
				}
				r.byDay = append(r.byDay, byDay{n: n, day: wd})
			}
		case "BYMONTHDAY":
			for _, d := range strings.Split(v, ",") {
				n, err := strconv.Atoi(d)
				if err != nil || n < 1 || n > 31 {
					return r, fmt.Errorf("%w: BYMONTHDAY %q", ErrUnexpanded, v)
				}
				r.byMonDay = append(r.byMonDay, n)
			}
		case "WKST":
		default:
			return r, fmt.Errorf("%w: %s", ErrUnexpanded, k)
		}
	}
	switch r.freq {
	case "DAILY":
		if len(r.byDay) > 0 || len(r.byMonDay) > 0 {
			return r, fmt.Errorf("%w: DAILY with BYDAY or BYMONTHDAY", ErrUnexpanded)
		}
	case "WEEKLY":
		if len(r.byMonDay) > 0 || slices.ContainsFunc(r.byDay, func(b byDay) bool { return b.n != 0 }) {
			return r, fmt.Errorf("%w: WEEKLY with BYMONTHDAY or a numbered BYDAY", ErrUnexpanded)
		}
	case "MONTHLY":
		if len(r.byDay) > 0 && len(r.byMonDay) > 0 {
			return r, fmt.Errorf("%w: MONTHLY with both BYDAY and BYMONTHDAY", ErrUnexpanded)
		}
	default:
		return r, fmt.Errorf("%w: FREQ %q", ErrUnexpanded, r.freq)
	}
	return r, nil
}

// at is a day of a month at the clock time of start, in start's zone.
func at(start time.Time, y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, start.Hour(), start.Minute(), start.Second(), 0, start.Location())
}

// monthDays lists the days of month y-m a MONTHLY rule picks, in order.
func (r rule) monthDays(start time.Time, y int, m time.Month) []int {
	last := time.Date(y, m+1, 0, 0, 0, 0, 0, time.UTC).Day()
	var out []int
	switch {
	case len(r.byDay) > 0:
		for d := 1; d <= last; d++ {
			wd := time.Date(y, m, d, 0, 0, 0, 0, time.UTC).Weekday()
			for _, b := range r.byDay {
				if b.day != wd {
					continue
				}
				nth, fromEnd := (d-1)/7+1, -((last-d)/7 + 1)
				if b.n == 0 || b.n == nth || b.n == fromEnd {
					out = append(out, d)
					break
				}
			}
		}
	case len(r.byMonDay) > 0:
		for _, d := range r.byMonDay {
			if d <= last {
				out = append(out, d)
			}
		}
		slices.Sort(out)
		out = slices.Compact(out)
	default:
		if d := start.Day(); d <= last {
			out = append(out, d)
		}
	}
	return out
}

// Expand lists the start times of the event's occurrences that start before
// to and end after from (an event with no rule is its one occurrence). EXDATE
// times are left out. It returns an error wrapping ErrUnexpanded for a rule it
// does not handle.
func Expand(e VEvent, from, to time.Time) ([]time.Time, error) {
	length := e.End.Sub(e.Start)
	in := func(t time.Time) bool { return t.Before(to) && t.Add(length).After(from) || t.Equal(from) }
	if e.RRule == "" {
		if in(e.Start) {
			return []time.Time{e.Start}, nil
		}
		return nil, nil
	}
	r, err := parseRule(e.RRule, e.Start.Location())
	if err != nil {
		return nil, err
	}
	var out []time.Time
	n := 0
	excluded := func(t time.Time) bool {
		return slices.ContainsFunc(e.ExDates, func(x time.Time) bool { return x.Equal(t) })
	}
	// emit counts one occurrence and reports whether to go on.
	emit := func(t time.Time) bool {
		if t.Before(e.Start) {
			return true
		}
		if !r.until.IsZero() && t.After(r.until) || !t.Before(to) {
			return false
		}
		n++
		if r.count > 0 && n > r.count {
			return false
		}
		if !excluded(t) && in(t) {
			out = append(out, t)
		}
		return true
	}
	s := e.Start
	for step := 0; step < maxSteps; step++ {
		switch r.freq {
		case "DAILY":
			if !emit(s.AddDate(0, 0, step*r.interval)) {
				return out, nil
			}
		case "WEEKLY":
			days := r.byDay
			if len(days) == 0 {
				days = []byDay{{day: s.Weekday()}}
			}
			// The week starts on the Monday on or before the start (WKST=MO).
			monday := s.AddDate(0, 0, -((int(s.Weekday()) + 6) % 7))
			week := monday.AddDate(0, 0, 7*step*r.interval)
			var ts []time.Time
			for _, b := range days {
				off := (int(b.day) + 6) % 7
				ts = append(ts, week.AddDate(0, 0, off))
			}
			slices.SortFunc(ts, func(a, b time.Time) int { return a.Compare(b) })
			for _, t := range ts {
				if !emit(t) {
					return out, nil
				}
			}
		case "MONTHLY":
			y, m := s.Year(), s.Month()+time.Month(step*r.interval)
			first := time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
			for _, d := range r.monthDays(s, first.Year(), first.Month()) {
				if !emit(at(s, first.Year(), first.Month(), d)) {
					return out, nil
				}
			}
		}
	}
	return out, nil
}
