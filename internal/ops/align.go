package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// Align. An agent compares the vault with reality using its own tools (gh,
// jira, slack, a calendar) and fixes the differences with ops, each leaving a receipt. Para
// fetches nothing. align_scope tells the agent what to walk; align_record keeps
// a receipt of the run, so a clean run ("no change") is on record too. The
// date of the last record per goal is what Today's goal page and the optional
// not_aligned health finding read.

// AlignVault is the scope of the whole vault.
const AlignVault = "vault"

const (
	// alignMeetingsBack is how many days back align_scope lists meetings.
	alignMeetingsBack = 7
	opAlignRecord     = "align_record"
	// alignDaysKey is the vault.properties key that turns on the not_aligned
	// health finding: goals not aligned in this many days.
	alignDaysKey = "align.days"
)

// AlignScopeInput is the input of align_scope.
type AlignScopeInput struct {
	Scope string `json:"scope" query:"scope" required:"true"` // a goal id, an area id, or vault
	// From and To bound the meetings and calls listed: a date or an RFC 3339
	// time. Default: the last 7 days and the next 14.
	From string `json:"from,omitempty" query:"from" doc:"meetings from this date or time; default 7 days ago"`
	To   string `json:"to,omitempty" query:"to" doc:"meetings up to this date (included) or time; default 14 days from today"`
}

// AlignMeeting is a meeting or call in the window, to check against the
// owner's calendar (calendar_compare).
type AlignMeeting struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"` // meeting or call
	Title       string   `json:"title"`
	Status      string   `json:"status"`
	Start       string   `json:"start"`
	End         string   `json:"end,omitempty"`
	With        []string `json:"with"`
	CalendarUID string   `json:"calendar_uid,omitempty"` // the first of calendar_uids
	// CalendarUIDs are every calendar event of it (invite, room, prep).
	CalendarUIDs []string `json:"calendar_uids"`
	Recur        string   `json:"recur,omitempty"`
}

// AlignTarget is who an ask went to and where the thread is.
type AlignTarget struct {
	To     string `json:"to"`
	Medium string `json:"medium"`
	State  string `json:"state"`
	Ref    string `json:"ref,omitempty"`
	Sent   string `json:"sent,omitempty"`
}

// AlignItem is one open item to check against reality.
type AlignItem struct {
	ID        string          `json:"id"`
	Kind      string          `json:"kind"`
	Title     string          `json:"title"`
	State     string          `json:"state"`
	Due       string          `json:"due,omitempty"`
	EffDue    string          `json:"eff_due,omitempty"`
	Goal      string          `json:"goal,omitempty"`
	GoalName  string          `json:"goal_name,omitempty" nameof:"Goal"`
	DoneWhen  string          `json:"done_when,omitempty"`
	Scope     string          `json:"scope,omitempty"` // for a question: what it is about
	DependsOn []string        `json:"depends_on,omitempty"`
	Blocks    []string        `json:"blocks,omitempty"`
	Targets   []AlignTarget   `json:"targets,omitempty"`
	Links     []model.Pointer `json:"links,omitempty"`
	Why       string          `json:"why"`
}

// AlignScopeOutput is what to check: the scope, its own links, and its open
// items in value order.
type AlignScopeOutput struct {
	Scope       string          `json:"scope"`
	Title       string          `json:"title"`
	Kind        string          `json:"kind"` // vault, goal, or area
	DoneWhen    string          `json:"done_when,omitempty"`
	Links       []model.Pointer `json:"links"`
	LastAligned string          `json:"last_aligned,omitempty"`
	Items       []AlignItem     `json:"items"`
	// Meetings are the meetings and calls of the scope in the window, any
	// status: compare them with the calendar (calendar_compare). A meeting
	// with no goal or area is in the vault scope only.
	MeetingsFrom string         `json:"meetings_from"`
	MeetingsTo   string         `json:"meetings_to"`
	Meetings     []AlignMeeting `json:"meetings"`
}

// AlignScope lists what an agent checks against reality for one scope. It reads
// the vault only.
var AlignScope = defineRead("align_scope", "List what to check against reality for a goal, an area, or the vault: open items in value order with their links, dates, targets, and states, the meetings and calls in a window (check them against the owner's calendar with calendar_compare), and when the scope was last aligned", func(ctx context.Context, d Deps, in AlignScopeInput) (AlignScopeOutput, error) {
	scope := normID(in.Scope)
	if scope == "" {
		return AlignScopeOutput{}, errors.New("scope is empty: give a goal id, an area id, or vault")
	}
	out := AlignScopeOutput{Scope: scope, Title: "Vault", Kind: AlignVault, Links: []model.Pointer{}, Items: []AlignItem{}, Meetings: []AlignMeeting{}}
	from, to, err := calendarWindow(d.Store.Now(), time.Local, in.From, in.To, alignMeetingsBack, alignMeetingsBack+calendarDefaultDays)
	if err != nil {
		return out, err
	}
	out.MeetingsFrom, out.MeetingsTo = from.Format(time.RFC3339), to.Format(time.RFC3339)
	if scope != AlignVault {
		doc, err := d.Store.Get(ctx, scope)
		if err != nil {
			return out, err
		}
		out.Title = doc.Title
		switch m := doc.Meta.(type) {
		case *model.Goal:
			out.Kind, out.DoneWhen, out.Links = "goal", m.DoneWhen, append(out.Links, m.Links...)
		case *model.Area:
			out.Kind, out.Links = "area", append(out.Links, m.Links...)
		default:
			return out, fmt.Errorf("%s is a %s: give a goal, an area, or vault", doc.ID, doc.Meta.Header().Type)
		}
	}
	dates, err := alignedDates(ctx, d.Store)
	if err != nil {
		return out, err
	}
	today := d.Store.Now().Format(dateLayout)
	order, err := openWorkAt(ctx, d.Store, today)
	if err != nil {
		return out, err
	}
	docs := map[string]model.Document{}
	get := func(id string) (model.Document, bool) {
		if id == "" {
			return model.Document{}, false
		}
		if doc, ok := docs[id]; ok {
			return doc, true
		}
		doc, err := d.Store.Get(ctx, id)
		if err != nil {
			return doc, false
		}
		doc = obligationShape(doc) // an obligation is checked as its task, an incident task as an incident
		docs[id] = doc
		return doc, true
	}
	areaOfGoal := func(goal string) string {
		if gd, ok := get(goal); ok {
			if p, ok := gd.Meta.(*model.Goal); ok {
				return p.Area.Target()
			}
		}
		return ""
	}
	for _, row := range order.Rows {
		doc, ok := get(row.ID)
		if !ok {
			continue
		}
		goal, area := alignHome(doc, get, areaOfGoal)
		if scope != AlignVault && goal != scope && area != scope {
			continue
		}
		item := AlignItem{ID: row.ID, Kind: row.Kind, Title: row.Title, Due: row.Due, EffDue: row.EffDue, Goal: goal, Why: row.Why, Links: []model.Pointer{}}
		fillAlignItem(&item, doc)
		out.Items = append(out.Items, item)
	}
	meetings, err := paraCalendarItems(ctx, d.Store, time.Local)
	if err != nil {
		return out, err
	}
	for _, it := range meetings {
		if !it.inWindow(from, to) {
			continue
		}
		area := it.Area
		if area == "" {
			area = areaOfGoal(it.Project)
		}
		if scope != AlignVault && it.Project != scope && area != scope {
			continue
		}
		when, end := it.Start.Format(time.RFC3339), ""
		if it.AllDay {
			when = it.Start.Format(dateLayout)
		} else if !it.End.IsZero() {
			end = it.End.Format(time.RFC3339)
		}
		out.Meetings = append(out.Meetings, AlignMeeting{ID: it.ID, Kind: it.Kind, Title: it.Title, Status: it.Status,
			Start: when, End: end, With: nonNil(it.With), CalendarUID: firstOf(it.UIDs...), CalendarUIDs: nonNil(it.UIDs), Recur: it.Recur})
	}
	switch out.Kind {
	case "goal":
		out.LastAligned = lastAligned(dates, scope, areaOfGoal(scope))
	case "area":
		out.LastAligned = lastAligned(dates, scope, "")
	default:
		out.LastAligned = dates[AlignVault]
	}
	return out, nil
})

// alignHome finds the goal and the area an open item belongs to.
func alignHome(doc model.Document, get func(string) (model.Document, bool), areaOfGoal func(string) string) (goal, area string) {
	switch m := doc.Meta.(type) {
	case *model.Goal:
		return doc.ID, m.Area.Target()
	case *model.Task:
		goal, area = m.Goal.Target(), m.Area.Target()
	case *model.RFX:
		goal = m.Goal.Target()
	case *model.Question:
		if m.IsRoot() {
			return "", ""
		}
		sc := m.Scope.Target()
		sd, ok := get(sc)
		if !ok {
			return "", ""
		}
		switch s := sd.Meta.(type) {
		case *model.Goal:
			goal = sc
		case *model.Area:
			return "", sc
		case *model.Task:
			goal, area = s.Goal.Target(), s.Area.Target()
		}
	case *model.Incident:
		return "", m.Area.Target()
	}
	if area == "" {
		area = areaOfGoal(goal)
	}
	return goal, area
}

func fillAlignItem(it *AlignItem, doc model.Document) {
	switch m := doc.Meta.(type) {
	case *model.Goal:
		it.State, it.DoneWhen = string(m.Status), m.DoneWhen
		it.Links = append(it.Links, m.Links...)
	case *model.Task:
		it.State, it.DoneWhen, it.DependsOn = string(m.Status), m.DoneWhen, targets(m.DependsOn)
	case *model.Question:
		it.State, it.Scope, it.DependsOn = string(m.Status), m.Scope.Target(), targets(m.DependsOn)
	case *model.RFX:
		it.State, it.DependsOn, it.Blocks = string(m.Status)+" "+string(m.Direction), targets(m.DependsOn), targets(m.Blocks)
		for _, t := range m.Targets {
			it.Targets = append(it.Targets, AlignTarget{To: t.To.Target(), Medium: string(t.Medium), State: string(t.State), Ref: t.Ref, Sent: string(t.Sent)})
		}
	case *model.Incident:
		it.State = string(m.Status)
		it.Links = append(it.Links, m.Links...)
	case *model.Meeting:
		it.State = string(m.Status)
		if it.Due == "" {
			it.Due = m.Start
		}
	case *model.Request:
		it.State = string(m.Status)
	}
}

// alignedDates maps a scope (goal id, area id, or vault) to the date of its
// latest align_record receipt.
func alignedDates(ctx context.Context, s store.Store) (map[string]string, error) {
	all, err := listReceipts(ctx, s)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, r := range all {
		if r.Op == opAlignRecord && string(r.Created) > out[r.Doc] {
			out[r.Doc] = string(r.Created)
		}
	}
	return out, nil
}

// lastAligned is the latest date among the goal or area itself, its area, and
// the whole vault: a vault-wide run aligns every goal.
func lastAligned(dates map[string]string, scope, area string) string {
	best := ""
	for _, k := range []string{scope, area, AlignVault} {
		if k != "" && dates[k] > best {
			best = dates[k]
		}
	}
	return best
}

// LastAligned returns the date a goal was last aligned, or "".
func LastAligned(ctx context.Context, s store.Store, goal string) (string, error) {
	dates, err := alignedDates(ctx, s)
	if err != nil {
		return "", err
	}
	area := ""
	if d, err := s.Get(ctx, goal); err == nil {
		if p, ok := d.Meta.(*model.Goal); ok {
			area = p.Area.Target()
		}
	}
	return lastAligned(dates, goal, area), nil
}

// AlignRecordInput is the input of align_record.
type AlignRecordInput struct {
	Scope       string   `json:"scope"`
	Checked     int      `json:"checked"`
	Matched     int      `json:"matched"`
	Differ      int      `json:"differ"`
	Unreachable []string `json:"unreachable,omitempty"` // sources that could not be read
}

// AlignRecordOutput is the output of align_record.
type AlignRecordOutput struct {
	Scope   string `json:"scope"`
	Receipt string `json:"receipt"`
	Date    string `json:"date"`
	Summary string `json:"summary"`
}

// AlignRecord stores the outcome of an align run as a receipt. A run that found
// nothing is recorded too. What it says is the agent's word, so it is marked
// self-reported.
var AlignRecord = defineSafe(opAlignRecord, "Record the outcome of an align run for a goal, an area, or the vault: how many items you checked, matched, and differed, and the sources you could not reach. A run with no change is recorded too", func(ctx context.Context, d Deps, in AlignRecordInput) (AlignRecordOutput, error) {
	scope := normID(in.Scope)
	if scope == "" {
		return AlignRecordOutput{}, errors.New("scope is empty: give a goal id, an area id, or vault")
	}
	title := "Vault"
	if scope != AlignVault {
		doc, err := d.Store.Get(ctx, scope)
		if err != nil {
			return AlignRecordOutput{}, err
		}
		switch doc.Meta.(type) {
		case *model.Goal, *model.Area:
			title = doc.Title
		default:
			return AlignRecordOutput{}, fmt.Errorf("%s is a %s: give a goal, an area, or vault", doc.ID, doc.Meta.Header().Type)
		}
	}
	if in.Checked < 0 || in.Matched < 0 || in.Differ < 0 {
		return AlignRecordOutput{}, errors.New("checked, matched, and differ are counts: none may be negative")
	}
	if in.Matched+in.Differ > in.Checked {
		return AlignRecordOutput{}, fmt.Errorf("matched %d and differ %d are more than checked %d", in.Matched, in.Differ, in.Checked)
	}
	var unreachable []string
	for _, u := range in.Unreachable {
		if u = strings.TrimSpace(u); u != "" && !slices.Contains(unreachable, u) {
			unreachable = append(unreachable, u)
		}
	}
	if in.Checked == 0 && len(unreachable) == 0 {
		return AlignRecordOutput{}, errors.New("checked is 0 and no source is unreachable: nothing was compared, so there is nothing to record")
	}
	info, err := identity(d)
	if err != nil {
		return AlignRecordOutput{}, err
	}
	res := &model.AlignResult{Checked: in.Checked, Matched: in.Matched, Differ: in.Differ, Unreachable: unreachable}
	summary := alignSummary(res)
	id, err := createReceipt(ctx, d, receiptInput{op: opAlignRecord, doc: scope, title: title,
		meta:    WriteMeta{Why: summary, Request: heldRequest(ctx, d.Store, info.Actor)},
		content: model.ReceiptContent{After: summary}, selfReported: true, align: res})
	if err != nil {
		return AlignRecordOutput{}, err
	}
	return AlignRecordOutput{Scope: scope, Receipt: id, Date: d.Store.Now().Format(dateLayout), Summary: summary}, nil
})

// alignSummary is the one line a run leaves: "Checked 12: 10 matched, 2 differ,
// 1 source unreachable (gh)".
func alignSummary(r *model.AlignResult) string {
	s := fmt.Sprintf("Checked %d: %d matched, %d differ", r.Checked, r.Matched, r.Differ)
	if n := len(r.Unreachable); n > 0 {
		s += fmt.Sprintf(", %d unreachable (%s)", n, strings.Join(r.Unreachable, ", "))
	}
	if r.Differ == 0 && len(r.Unreachable) == 0 {
		s += ": no change"
	}
	return s
}

// notAligned adds a not_aligned finding to each active goal that was not
// aligned in align.days days. The vault turns it on by setting align.days in
// vault.properties; unset, zero, or not a number leaves it off.
func (h *healther) notAligned(ctx context.Context, s store.Store) error {
	conf, err := s.Config(alignDaysKey)
	if err != nil {
		return err
	}
	days, err := strconv.Atoi(strings.TrimSpace(conf))
	if err != nil || days <= 0 {
		return nil
	}
	today, err := time.Parse(dateLayout, h.today)
	if err != nil {
		return nil
	}
	dates, err := alignedDates(ctx, s)
	if err != nil {
		return err
	}
	for id := range h.openGoals {
		d := h.docs[id]
		if h.deferred(d.Meta) {
			continue
		}
		area := ""
		if p, ok := d.Meta.(*model.Goal); ok {
			area = p.Area.Target()
		}
		last := lastAligned(dates, id, area)
		fix := fmt.Sprintf("run /para align %s: align_scope {scope: %s}, then align_record", id, id)
		if last == "" {
			h.add(HealthNotAligned, id, id, "goal has never been aligned with reality", fix)
			continue
		}
		if t, err := time.Parse(dateLayout, last); err == nil && int(today.Sub(t).Hours()/24) > days {
			h.add(HealthNotAligned, id, id, fmt.Sprintf("goal was last aligned %s, more than %d days ago", last, days), fix)
		}
	}
	return nil
}
