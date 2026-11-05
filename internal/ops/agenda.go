package ops

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// The agenda is what has a time or a date in the next few days: meetings,
// tasks due, requests to nudge, and policy exceptions due for review. It reads
// the vault and nothing else: para does not read or write an external calendar.
// Times show in the reader's zone (this machine's, or the zone named). A date
// is a calendar day in that zone, so a meeting at 01:00 UTC is the evening
// before in New York.

const (
	// AgendaDefaultDays is the window of agenda when the caller names none.
	AgendaDefaultDays = 7
	// AgendaMaxDays is the longest window.
	AgendaMaxDays = 366
)

// AgendaKind is what an agenda item is.
type AgendaKind string

const (
	AgendaMeeting   AgendaKind = "meeting"
	AgendaDue       AgendaKind = "due"
	AgendaNudge     AgendaKind = "nudge"
	AgendaException AgendaKind = "exception"
	// AgendaGoal is a goal date that has come: a pivot date, or a signal
	// reading that is due. Detail says which and why.
	AgendaGoal AgendaKind = "goal"
	// AgendaArea is an area review that is due: one cadence after the last
	// review, or today for an area never reviewed.
	AgendaArea AgendaKind = "area"
	// AgendaRetry is a failed task whose retry_after has come.
	AgendaRetry AgendaKind = "retry"
)

// AgendaKinds lists every AgendaKind.
var AgendaKinds = []AgendaKind{AgendaMeeting, AgendaDue, AgendaNudge, AgendaException, AgendaGoal, AgendaArea, AgendaRetry}

// Enum lists the allowed values, for schema generation.
func (AgendaKind) Enum() []string {
	out := make([]string, len(AgendaKinds))
	for i, k := range AgendaKinds {
		out[i] = string(k)
	}
	return out
}

// AgendaItem is one line of the agenda. Date is the calendar day in the zone.
// A meeting has Time, At, and Until; the others fall on a date only, and a
// date before today is Overdue.
type AgendaItem struct {
	Kind    AgendaKind `json:"kind"`
	ID      string     `json:"id"`
	Title   string     `json:"title"`
	Date    string     `json:"date"`
	Time    string     `json:"time,omitempty"`  // HH:MM in the zone; meetings only
	At      string     `json:"at,omitempty"`    // RFC 3339 in the zone; meetings only
	Until   string     `json:"until,omitempty"` // RFC 3339 in the zone; meetings only
	Overdue bool       `json:"overdue"`
	Detail  string     `json:"detail,omitempty"` // task status, meeting status, exception kind
	// Why is the reason in value order (see RankValue), when the item is in it.
	Why string `json:"why,omitempty"`
	// Who is the people of a meeting, or the targets of a nudge.
	Who      []string `json:"who"`
	WhoNames []string `json:"who_names,omitempty" nameof:"Who"`
	Project  string   `json:"project,omitempty"` // a task's project or area
	// ProjectName is the title of Project, for people.
	ProjectName string `json:"project_name,omitempty" nameof:"Project"`
	// Optional and Attend are a meeting's attendance: an optional meeting
	// the owner has not said they go to shows quietly.
	Optional bool         `json:"optional,omitempty"`
	Attend   model.Attend `json:"attend,omitempty"`
}

// AgendaInput is the input of agenda.
type AgendaInput struct {
	Days int    `json:"days,omitempty" query:"days"` // from today on; default 7, at most 366
	Zone string `json:"zone,omitempty" query:"zone"` // an IANA zone such as Europe/Berlin; default this machine's
}

// AgendaOutput is the output of agenda. From and To are the first and last
// day of the window in the zone.
type AgendaOutput struct {
	From  string       `json:"from"`
	To    string       `json:"to"`
	Zone  string       `json:"zone"`
	Items []AgendaItem `json:"items"`
	// Skipped are the meetings of the window the owner does not go to
	// (attend no). They are not in Items.
	Skipped []AgendaItem `json:"skipped"`
}

// agendaZone returns the location to show times in.
func agendaZone(name string) (*time.Location, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return time.Local, nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("zone %q: want an IANA zone such as Europe/Berlin or UTC", name)
	}
	return loc, nil
}

// agendaDays checks a window length.
func agendaDays(days, def int) (int, error) {
	switch {
	case days < 0:
		return 0, errors.New("days is negative")
	case days == 0:
		return def, nil
	case days > AgendaMaxDays:
		return 0, fmt.Errorf("days is over %d", AgendaMaxDays)
	}
	return days, nil
}

// window is the days from today in loc: its first day, the first day after it,
// and the instants of midnight at both ends. Midnight is computed on the civil
// date, so a clock change inside the window does not move a day.
type window struct {
	loc          *time.Location
	today, after string // YYYY-MM-DD
	start, end   time.Time
	zone         string
	lastDay      string
}

func newWindow(now time.Time, loc *time.Location, days int) window {
	n := now.In(loc)
	y, m, d := n.Date()
	start := time.Date(y, m, d, 0, 0, 0, 0, loc)
	end := time.Date(y, m, d+days, 0, 0, 0, 0, loc)
	last := time.Date(y, m, d+days-1, 12, 0, 0, 0, loc)
	zone := loc.String()
	if loc == time.Local || zone == "Local" {
		zone = n.Format("-07:00")
	}
	return window{loc: loc, today: start.Format(dateLayout), after: end.Format(dateLayout), start: start, end: end,
		zone: zone, lastDay: last.Format(dateLayout)}
}

// Agenda lists what is dated in the next days, earliest first. Items with only
// a date come before the timed meetings of their day; an item dated before
// today is overdue and comes first.
var Agenda = defineRead("agenda", "List meetings, tasks due, requests to nudge, and exceptions due for review in the next days, by local time: a calendar, earliest first; within a day, value order (see order). days: 1 is today only (from equals to); days: 7 is today and the six days after", func(ctx context.Context, d Deps, in AgendaInput) (AgendaOutput, error) {
	days, err := agendaDays(in.Days, AgendaDefaultDays)
	if err != nil {
		return AgendaOutput{}, err
	}
	loc, err := agendaZone(in.Zone)
	if err != nil {
		return AgendaOutput{}, err
	}
	w := newWindow(d.Store.Now(), loc, days)
	items, skipped, err := agendaItems(ctx, d, w, true)
	if err != nil {
		return AgendaOutput{}, err
	}
	return AgendaOutput{From: w.today, To: w.lastDay, Zone: w.zone, Items: items, Skipped: skipped}, nil
})

// agendaItems gathers the items of the window, and apart from them the
// meetings the owner skips. Overdue dated items are included when overdue is
// set.
func agendaItems(ctx context.Context, d Deps, w window, overdue bool) (items, skipped []AgendaItem, err error) {
	items, skipped = []AgendaItem{}, []AgendaItem{}
	meetings, err := loadMeetings(ctx, d.Store)
	if err != nil {
		return nil, nil, err
	}
	for _, v := range meetings {
		if v.Status != model.MeetingScheduled {
			continue
		}
		start, serr := time.Parse(time.RFC3339, v.Start)
		end, eerr := time.Parse(time.RFC3339, v.End)
		if serr != nil || eerr != nil {
			continue // lint reports a bad time; the agenda skips it
		}
		if !end.After(w.start) || !start.Before(w.end) {
			continue
		}
		s := start.In(w.loc)
		it := AgendaItem{
			Kind: AgendaMeeting, ID: v.ID, Title: v.Title, Date: s.Format(dateLayout), Time: s.Format("15:04"),
			At: s.Format(time.RFC3339), Until: end.In(w.loc).Format(time.RFC3339), Detail: string(v.Status), Who: v.With,
			Optional: v.Optional, Attend: v.Attend,
		}
		if v.Attend == model.AttendNo {
			skipped = append(skipped, it)
			continue
		}
		items = append(items, it)
	}
	inWindow := func(date string) (late, ok bool) {
		switch {
		case date >= w.after:
			return false, false
		case date < w.today:
			return true, overdue
		}
		return false, true
	}
	tasks, err := listTasks(ctx, d.Store, "", "")
	if err != nil {
		return nil, nil, err
	}
	deferred, err := deferredSet(ctx, d.Store, d.Store.Now().Format(dateLayout))
	if err != nil {
		return nil, nil, err
	}
	for _, t := range tasks {
		if t.Outcome == model.OutcomeFailed && t.RetryAfter != "" && !IsOther(t.Assignee) {
			if late, ok := inWindow(string(t.RetryAfter)); ok {
				items = append(items, AgendaItem{Kind: AgendaRetry, ID: t.ID, Title: t.Title, Date: string(t.RetryAfter), Overdue: late,
					Detail: "failed; retry after", Who: []string{}, Project: cmp.Or(t.Project, t.Area)})
			}
		}
		// A call or work with someone shows on the day it is scheduled, apart
		// from its due date. A meeting shows above, with its time.
		date := string(t.Due)
		if (t.Kind == model.KindCall || t.Kind == model.KindWorkWith) && t.Scheduled != "" {
			if day, ok := meetingDay(t.Scheduled); ok {
				date = day
			}
		}
		if taskTerminal(t.Status) || IsOther(t.Assignee) || date == "" || t.Kind == model.KindMeeting || deferred[t.ID].any() {
			continue
		}
		late, ok := inWindow(date)
		if !ok {
			continue
		}
		detail := ""
		switch {
		case t.Stuck:
			detail = "stuck"
		case t.Status == model.TaskBlocked || len(t.BlockedBy) > 0:
			detail = "blocked"
		case t.Kind == model.KindCall || t.Kind == model.KindWorkWith:
			detail = string(t.Kind)
		}
		items = append(items, AgendaItem{Kind: AgendaDue, ID: t.ID, Title: t.Title, Date: date, Overdue: late,
			Detail: detail, Who: []string{}, Project: cmp.Or(t.Project, t.Area)})
	}
	rfx, err := loadRFX(ctx, d)
	if err != nil {
		return nil, nil, err
	}
	for _, r := range rfx {
		if r.Status != model.RFXOpen || r.NextNudge == "" {
			continue
		}
		late, ok := inWindow(r.NextNudge)
		if !ok {
			continue
		}
		who := []string{}
		for _, t := range r.Targets {
			who = append(who, t.To.Target())
		}
		items = append(items, AgendaItem{Kind: AgendaNudge, ID: r.ID, Title: r.Title, Date: r.NextNudge, Overdue: late, Who: who})
	}
	excs, err := allExceptions(ctx, d)
	if err != nil {
		return nil, nil, err
	}
	for _, e := range excs {
		if e.Status == model.ExceptionResolved || e.ReviewBy == "" {
			continue
		}
		late, ok := inWindow(string(e.ReviewBy))
		if !ok {
			continue
		}
		items = append(items, AgendaItem{Kind: AgendaException, ID: e.ID, Title: e.Title, Date: string(e.ReviewBy), Overdue: late,
			Detail: string(e.Kind), Who: []string{}})
	}
	watches, err := goalWatches(ctx, d.Store, d.Store.Now().Format(dateLayout))
	if err != nil {
		return nil, nil, err
	}
	for _, g := range watches {
		late, ok := inWindow(g.Date)
		if !ok {
			continue
		}
		items = append(items, AgendaItem{Kind: AgendaGoal, ID: g.ID, Title: g.Title, Date: g.Date, Overdue: late,
			Detail: g.Reason, Who: []string{}})
	}
	areas, err := d.Store.List(ctx, store.Filter{Type: model.TypeArea})
	if err != nil {
		return nil, nil, err
	}
	for _, r := range areas {
		doc, a, err := getArea(ctx, d.Store, r.ID)
		if err != nil {
			return nil, nil, err
		}
		if a.Status != model.AreaActive || a.Cadence == "" {
			continue
		}
		date, detail := nextReview(a), "review "+a.Cadence
		if a.LastReviewed == "" {
			date, detail = w.today, "review "+a.Cadence+"; never reviewed"
		}
		if date == "" {
			continue // a cadence para cannot read: lint warns
		}
		late, ok := inWindow(date)
		if !ok {
			continue
		}
		items = append(items, AgendaItem{Kind: AgendaArea, ID: doc.ID, Title: doc.Title, Date: date, Overdue: late,
			Detail: detail, Who: []string{}})
	}
	order, err := Order(ctx, d.Store)
	if err != nil {
		return nil, nil, err
	}
	pos := map[string]int{}
	for i, r := range order.Rows {
		if r.Kind != KindMeeting { // meetings keep their time order
			pos[r.ID] = i
		}
		for j := range items {
			if items[j].ID == r.ID {
				items[j].Why = r.Why
			}
		}
	}
	sortAgenda(items, pos)
	return items, skipped, nil
}

// sortAgenda orders by day, date-only items before timed ones, timed items by
// instant (so two meetings at "01:30" on the day the clocks go back keep their
// real order), then kind and id.
func sortAgenda(items []AgendaItem, pos map[string]int) {
	rank := map[AgendaKind]int{AgendaDue: 0, AgendaNudge: 1, AgendaException: 2, AgendaGoal: 3, AgendaArea: 4, AgendaMeeting: 5}
	instant := func(it AgendaItem) time.Time {
		t, _ := time.Parse(time.RFC3339, it.At)
		return t
	}
	slices.SortStableFunc(items, func(a, b AgendaItem) int {
		return cmp.Or(
			cmp.Compare(a.Date, b.Date),
			cmp.Compare(rank[a.Kind], rank[b.Kind]),
			cmp.Compare(posOf(pos, a.ID), posOf(pos, b.ID)), // value order within a day
			instant(a).Compare(instant(b)),
			cmp.Compare(a.ID, b.ID))
	})
}

func posOf(pos map[string]int, id string) int {
	if p, ok := pos[id]; ok {
		return p
	}
	return 1 << 30
}
