package ops

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// Value order. One function orders everything that needs the owner, for
// Today, next, agenda, and the PWA. The rule, with no weights:
//
// Every listed item is either actionable now or not yet startable. Not yet
// startable is a booked meeting whose day is after today, a call or work with
// someone scheduled after today, and anything that depends (depends_on, through
// any chain) on one of those. Actionable now comes first:
//
//  1. Open incidents, worst severity first.
//  2. Items overdue or due within order.due_soon_days (default 2) by their
//     effective due date, soonest first.
//  3. Everything else by impact, highest first.
//
// Not yet startable comes last, by the day it can start.
//
// Effective due is the earliest of the item's own date and the date of any
// open item downstream of it (anything that waits on it, through depends_on,
// and the goal a task belongs to). It counts only through the due soon window:
// prep for a meeting next week ranks by impact. Ties: the effective due date
// (none last), the owner's rank between two goals, the oldest, then the id.
// Impact is the number of open items it unblocks, through any chain; a goal
// counts twice. A task also counts toward its parent task (an action item is
// not a dependency: the parent does not wait on it). A deferred item, and
// everything reachable only through one, adds no impact and gives no effective
// due.

// Kinds of ValueRow.
const (
	KindIncident = "incident"
	KindGoal     = "goal"
	KindTask     = "task"
	KindQuestion = "question"
	KindAsk      = "ask"
	KindMeeting  = "meeting"
	KindWork     = "work"
)

// Groups of ValueRow, in order. The first three are actionable now.
const (
	GroupIncident = "incident"
	GroupDueSoon  = "due_soon"
	GroupImpact   = "impact"
	GroupNotYet   = "not_yet"
)

// Buckets of ValueRow.
const (
	BucketNow    = "now"
	BucketNotYet = "not_yet"
)

// DueSoonKey is the vault setting of the due soon window, in days.
const DueSoonKey = "order.due_soon_days"

// DefaultDueSoonDays is the due soon window when the vault sets none.
const DefaultDueSoonDays = 2

// RankOptions is what RankValue needs besides the nodes.
type RankOptions struct {
	Today       string // YYYY-MM-DD
	DueSoonDays int    // overdue or due within this many days of Today ranks by date
}

// goalWeight is how many items a goal counts for in impact.
const goalWeight = 2

// ValueNode is one open item of the graph the order reads.
type ValueNode struct {
	ID      string
	Kind    string
	Title   string
	Due     string   // own date, YYYY-MM-DD, or ""
	Created string   // for the tie-break
	Deps    []string // ids this waits on
	Goal    string   // for a task: the goal it belongs to
	Parent  string   // for a task: the task it is an action item of; not a dependency
	Sev     string   // for an incident: sev1, sev2, sev3
	// Deferred is true when the item is put off today. DeferUntil is its date
	// or "someday".
	Deferred   bool
	DeferUntil string
	// WaitOnly is true when open items in WaitDeps are the only reason the item
	// is deferred. Its downstream still counts for those items, so the work it
	// waits on rises with what the item blocks.
	WaitOnly bool
	WaitDeps []string
	// ReadyWhy is "ready: X done" for the days after a wait resolved.
	ReadyWhy string
	// Hidden nodes shape the order but are not listed: work for someone else.
	Hidden bool
	// Proposal marks a question with an answer to check.
	Proposal bool
	// Rank is the owner's rank of a goal, 1 first; 0 is none. It breaks ties
	// between two goals after their dates.
	Rank int
	// Start is the day the item can start, YYYY-MM-DD, or "": a booked
	// meeting's day, or the day a call or work with someone is scheduled.
	Start string
}

// ValueRow is one listed item with the reason it is where it is.
type ValueRow struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	Title    string `json:"title"`
	Group    string `json:"group"`
	Due      string `json:"due,omitempty"`      // own date
	EffDue   string `json:"eff_due,omitempty"`  // effective date
	Impact   int    `json:"impact"`             // weighted count
	Blocks   int    `json:"blocks"`             // open items it unblocks, goals not counted
	Items    int    `json:"items,omitempty"`    // open action items (children) of a task
	Parent   string `json:"parent,omitempty"`   // the task this is an action item of
	Why      string `json:"why"`                // in words
	Proposal bool   `json:"proposal,omitempty"` // an answer waits to be checked
	// Bucket is now (actionable now) or not_yet (not yet startable).
	Bucket string `json:"bucket"`
	// Starts is the day a not_yet item can start. After is the item it waits
	// on, when that and not the item itself sets the day.
	Starts string `json:"starts,omitempty"`
	After  string `json:"after,omitempty"`
}

// HeldUp says a deferred item holds up work that is due before it returns.
type HeldUp struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	DeferUntil string `json:"defer_until"`
	Holds      string `json:"holds"`       // the waiting item with the earlier date
	HoldsTitle string `json:"holds_title"` // its title
	Due        string `json:"due"`
}

// ValueOrder is the order of a vault.
type ValueOrder struct {
	Rows []ValueRow `json:"rows"`
	Held []HeldUp   `json:"held_up"`
}

// ShortDate renders YYYY-MM-DD as "Nov 12".
func ShortDate(d string) string {
	t, err := time.Parse(dateLayout, d)
	if err != nil {
		return d
	}
	return t.Format("Jan 2")
}

// RankValue orders the nodes. It is a pure function: the same nodes and
// options give the same rows.
func RankValue(nodes []ValueNode, opt RankOptions) ValueOrder {
	by := map[string]*ValueNode{}
	for i := range nodes {
		by[nodes[i].ID] = &nodes[i]
	}
	down := map[string][]string{} // id -> items waiting on it
	// parentEdge marks "child parent" pairs: the parent does not wait on the
	// child, so it counts toward impact and the due date but not toward blocks.
	parentEdge := map[string]bool{}
	for _, n := range nodes {
		for _, dep := range n.Deps {
			if by[dep] != nil {
				down[dep] = append(down[dep], n.ID)
			}
		}
		if n.Goal != "" && by[n.Goal] != nil {
			down[n.ID] = append(down[n.ID], n.Goal)
		}
		if n.Parent != "" && by[n.Parent] != nil {
			down[n.ID] = append(down[n.ID], n.Parent)
			parentEdge[n.ID+" "+n.Parent] = true
		}
	}
	// reach walks downstream of id. A deferred item is not visited and its own
	// downstream is not followed.
	reach := func(id string) ([]*ValueNode, map[string]bool) {
		seen := map[string]bool{id: true}
		viaParent := map[string]bool{}
		queue := []string{id}
		var out []*ValueNode
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			for _, u := range down[cur] {
				if seen[u] {
					continue
				}
				seen[u] = true
				if by[u].Deferred && (!by[u].WaitOnly || !slices.Contains(by[u].WaitDeps, cur)) {
					continue
				}
				out = append(out, by[u])
				viaParent[u] = parentEdge[cur+" "+u]
				queue = append(queue, u)
			}
		}
		return out, viaParent
	}
	starts := notYet(nodes, by, opt.Today)
	soon := opt.Today
	if t, err := time.Parse(dateLayout, opt.Today); err == nil {
		soon = t.AddDate(0, 0, opt.DueSoonDays).Format(dateLayout)
	}
	var rows []ValueRow
	var held []HeldUp
	items := map[string]int{}
	for _, n := range nodes {
		if n.Parent != "" && by[n.Parent] != nil {
			items[n.Parent]++
		}
	}
	for _, n := range nodes {
		if n.Hidden {
			continue
		}
		if n.Deferred {
			// Deferred items are not listed. One that holds up earlier work is
			// reported.
			var worst *ValueNode
			reached, _ := reach(n.ID)
			for _, u := range reached {
				if u.Due != "" && (n.DeferUntil == model.SomedayWord || u.Due < n.DeferUntil) && (worst == nil || u.Due < worst.Due) {
					worst = u
				}
			}
			if worst != nil {
				held = append(held, HeldUp{ID: n.ID, Title: n.Title, DeferUntil: n.DeferUntil, Holds: worst.ID, HoldsTitle: worst.Title, Due: worst.Due})
			}
			continue
		}
		r := ValueRow{ID: n.ID, Kind: n.Kind, Title: n.Title, Due: n.Due, Proposal: n.Proposal, Items: items[n.ID]}
		if by[n.Parent] != nil {
			r.Parent = n.Parent
		}
		eff, from := n.Due, (*ValueNode)(nil)
		reached, viaParent := reach(n.ID)
		for _, u := range reached {
			r.Impact++
			if u.Kind == KindGoal {
				r.Impact += goalWeight - 1
			} else if !viaParent[u.ID] {
				r.Blocks++
			}
			if u.Due != "" && (eff == "" || u.Due < eff) {
				eff, from = u.Due, u
			}
		}
		r.EffDue = eff
		r.Bucket = BucketNow
		st, late := starts[n.ID]
		switch {
		case late:
			r.Group, r.Bucket, r.Starts = GroupNotYet, BucketNotYet, st.day
			if st.root != n.ID {
				r.After = st.root
			}
		case n.Kind == KindIncident:
			r.Group = GroupIncident
		case eff != "" && eff <= soon:
			r.Group = GroupDueSoon
		default:
			r.Group = GroupImpact
		}
		var waits []string
		for _, dep := range n.Deps {
			if u := by[dep]; u != nil {
				waits = append(waits, cmp.Or(u.Title, u.ID))
			}
		}
		r.Why = why(n, r, from, waits)
		switch {
		case r.Group == GroupNotYet && r.After != "":
			u := by[r.After]
			what := shorten(cmp.Or(u.Title, u.ID))
			if u.Kind == KindMeeting {
				what = "meeting " + what
			}
			r.Why = "not yet, waits on " + what + " on " + ShortDate(r.Starts) + "; " + r.Why
		case r.Group == GroupNotYet:
			r.Why = "not yet, starts " + ShortDate(r.Starts) + "; " + r.Why
		case r.Group == GroupDueSoon && eff < opt.Today:
			r.Why = "overdue; " + r.Why
		case r.Group == GroupDueSoon:
			r.Why = "due soon; " + r.Why
		}
		rows = append(rows, r)
	}
	rank := map[string]int{GroupIncident: 0, GroupDueSoon: 1, GroupImpact: 2, GroupNotYet: 3}
	created := map[string]string{}
	sev := map[string]string{}
	goalRank := map[string]int{}
	for _, n := range nodes {
		created[n.ID], sev[n.ID] = n.Created, n.Sev
		if n.Kind == KindGoal && n.Rank > 0 {
			goalRank[n.ID] = n.Rank
		}
	}
	// due compares effective dates, an item with none last.
	due := func(a, b ValueRow) int { return cmp.Compare(dueKey(a.EffDue), dueKey(b.EffDue)) }
	slices.SortStableFunc(rows, func(a, b ValueRow) int {
		if c := cmp.Compare(rank[a.Group], rank[b.Group]); c != 0 {
			return c
		}
		var c int
		switch a.Group {
		case GroupIncident:
			return cmp.Or(cmp.Compare(sev[a.ID], sev[b.ID]), cmp.Compare(created[a.ID], created[b.ID]), cmp.Compare(a.ID, b.ID))
		case GroupDueSoon:
			c = cmp.Or(due(a, b), cmp.Compare(b.Impact, a.Impact))
		case GroupImpact:
			c = cmp.Or(cmp.Compare(b.Impact, a.Impact), due(a, b))
		case GroupNotYet:
			c = cmp.Or(cmp.Compare(a.Starts, b.Starts), due(a, b), cmp.Compare(b.Impact, a.Impact))
		}
		if c != 0 {
			return c
		}
		if a.Kind == KindGoal && b.Kind == KindGoal {
			// The owner's rank settles a tie between goals; an unranked goal
			// goes after a ranked one.
			if c := cmp.Compare(rankKey(goalRank[a.ID]), rankKey(goalRank[b.ID])); c != 0 {
				return c
			}
		}
		return cmp.Or(cmp.Compare(created[a.ID], created[b.ID]), cmp.Compare(a.ID, b.ID))
	})
	slices.SortFunc(held, func(a, b HeldUp) int { return cmp.Or(cmp.Compare(a.Due, b.Due), cmp.Compare(a.ID, b.ID)) })
	if rows == nil {
		rows = []ValueRow{}
	}
	if held == nil {
		held = []HeldUp{}
	}
	return ValueOrder{Rows: rows, Held: held}
}

// startOf is the day an item can start and the item that sets it.
type startOf struct{ day, root string }

// notYet finds the items that cannot start today: those whose own Start is
// after today, and those that depend on one, through any chain. Each gets the
// latest such day and the item it comes from. A deferred dependency is left to
// the deferral.
func notYet(nodes []ValueNode, by map[string]*ValueNode, today string) map[string]startOf {
	out := map[string]startOf{}
	if today == "" {
		return out
	}
	done := map[string]bool{}
	var visit func(id string) (startOf, bool)
	visit = func(id string) (startOf, bool) {
		if done[id] {
			s, ok := out[id]
			return s, ok
		}
		done[id] = true // a cycle reads as no start
		n := by[id]
		var best startOf
		if n.Start > today {
			best = startOf{n.Start, id}
		}
		for _, dep := range n.Deps {
			if u := by[dep]; u == nil || u.Deferred {
				continue
			}
			if s, ok := visit(dep); ok && s.day > best.day {
				best = s
			}
		}
		if best.day == "" {
			return best, false
		}
		out[id] = best
		return best, true
	}
	for _, n := range nodes {
		visit(n.ID)
	}
	return out
}

// why says in words why a row is where it is.
func why(n ValueNode, r ValueRow, from *ValueNode, waits []string) string {
	var parts []string
	if n.ReadyWhy != "" {
		parts = append(parts, n.ReadyWhy)
	}
	switch {
	case n.Kind == KindIncident:
		parts = append(parts, n.Sev+" open")
	case n.Kind == KindMeeting && n.Due != "":
		parts = append(parts, "meeting "+ShortDate(n.Due))
	case n.Due != "" && n.Kind == KindGoal:
		parts = append(parts, "goal due "+ShortDate(n.Due))
	case n.Due != "":
		parts = append(parts, "due "+ShortDate(n.Due))
	}
	if r.Blocks > 0 {
		parts = append(parts, fmt.Sprintf("blocks %d", r.Blocks))
	}
	if r.Items > 0 {
		parts = append(parts, fmt.Sprintf("%d items", r.Items))
	}
	if from != nil && n.Kind != KindIncident {
		name := shorten(from.Title)
		if from.Kind == KindGoal {
			name = "goal"
		}
		parts = append(parts, name+" due "+ShortDate(from.Due))
	}
	if len(waits) > 0 {
		w := shorten(waits[0])
		if len(waits) > 1 {
			w += fmt.Sprintf(" and %d more", len(waits)-1)
		}
		parts = append(parts, "waits on "+w)
	}
	if n.Proposal {
		parts = append(parts, "answer to check")
	}
	if len(parts) == 0 {
		return "blocks nothing, no date"
	}
	return strings.Join(parts, ", ")
}

// valueNodes reads the open items of the vault.
func valueNodes(ctx context.Context, s store.Store, today string) ([]ValueNode, error) {
	deferred, err := deferredSet(ctx, s, today)
	if err != nil {
		return nil, err
	}
	refs, err := s.List(ctx, store.Filter{})
	if err != nil {
		return nil, err
	}
	var nodes []ValueNode
	rfxBlocks := map[string][]string{} // rfx id -> items it blocks
	meetingOf := map[string][]string{} // rfx id -> meetings that serve it
	horizon := today
	if t, err := time.Parse(dateLayout, today); err == nil {
		horizon = t.AddDate(0, 0, 14).Format(dateLayout)
	}
	for _, r := range refs {
		switch r.Type {
		case model.TypeIncident, model.TypeProject, model.TypeTask, model.TypeQuestion, model.TypeRFX, model.TypeMeeting, model.TypeRequest:
		default:
			continue
		}
		doc, err := s.Get(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		n := ValueNode{ID: doc.ID, Title: doc.Title, Created: string(doc.Meta.Header().Created), Deferred: deferred[doc.ID].any()}
		if df := deferralOf(doc.Meta); df != nil {
			n.DeferUntil = df.DeferUntil
			h := deferred[doc.ID]
			n.WaitOnly, n.WaitDeps, n.ReadyWhy = h.waitOnly(), h.Waits, h.Ready
		}
		// A booked meeting, in either shape, is a meeting node. A task meeting
		// that is not booked is a task below; an older one that is not booked
		// is left out.
		if mt, ok := readMeeting(doc); ok && mt.Status == model.MeetingScheduled {
			day, ok := meetingDay(mt.Start)
			if !ok {
				continue
			}
			n.Kind, n.Due, n.Start = KindMeeting, day, day
			n.Hidden = n.Due > horizon
			if t, isTask := doc.Meta.(*model.Task); isTask {
				n.Hidden = n.Hidden || IsOther(t.Assignee)
				n.Parent, n.Deps = t.Parent.Target(), targets(t.DependsOn)
			}
			for _, l := range mt.RFX {
				meetingOf[l.Target()] = append(meetingOf[l.Target()], doc.ID)
			}
			nodes = append(nodes, n)
			continue
		}
		switch m := doc.Meta.(type) {
		case *model.Incident:
			if m.Status == model.IncidentResolved {
				continue
			}
			n.Kind, n.Sev = KindIncident, string(m.Severity)
		case *model.Project:
			if m.Status != model.ProjectActive {
				continue
			}
			n.Kind, n.Due, n.Rank = KindGoal, string(m.Due), m.Rank
		case *model.Task:
			if taskTerminal(m.Status) {
				continue
			}
			n.Kind, n.Due, n.Goal, n.Hidden = KindTask, string(m.Due), m.Project.Target(), IsOther(m.Assignee)
			n.Parent = m.Parent.Target()
			n.Deps = targets(m.DependsOn)
			if (m.Kind == model.KindCall || m.Kind == model.KindWorkWith) && m.Scheduled != "" {
				n.Start, _ = meetingDay(m.Scheduled)
			}
		case *model.Question:
			if m.Status != model.QuestionOpen {
				continue
			}
			n.Kind, n.Deps, n.Proposal = KindQuestion, targets(m.DependsOn), m.HasProposal()
		case *model.RFX:
			if m.Status != model.RFXOpen {
				continue
			}
			n.Kind, n.Due, n.Deps = KindAsk, string(m.NextNudge), targets(m.DependsOn)
			n.Hidden = m.Direction != model.RFXOut
			for _, l := range m.Blocks {
				rfxBlocks[doc.ID] = append(rfxBlocks[doc.ID], l.Target())
			}
		case *model.Meeting:
			continue
		case *model.Request:
			if m.Status != model.RequestQueued && m.Status != model.RequestClaimed {
				continue
			}
			n.Kind = KindWork
		}
		nodes = append(nodes, n)
	}
	// An ask waits on the meeting that serves it; what an ask blocks waits on it.
	idx := map[string]int{}
	for i, n := range nodes {
		idx[n.ID] = i
	}
	for rfx, ms := range meetingOf {
		if i, ok := idx[rfx]; ok {
			nodes[i].Deps = append(nodes[i].Deps, ms...)
		}
	}
	for rfx, blocked := range rfxBlocks {
		for _, b := range blocked {
			if i, ok := idx[b]; ok {
				nodes[i].Deps = append(nodes[i].Deps, rfx)
			}
		}
	}
	return nodes, nil
}

// Order reads the vault and returns its value order. Every surface calls it.
func Order(ctx context.Context, s store.Store) (ValueOrder, error) {
	return OrderAt(ctx, s, s.Now().Format(dateLayout))
}

// OrderAt is Order for the local date today (YYYY-MM-DD).
func OrderAt(ctx context.Context, s store.Store, today string) (ValueOrder, error) {
	nodes, err := valueNodes(ctx, s, today)
	if err != nil {
		return ValueOrder{}, err
	}
	days, err := dueSoonDays(s)
	if err != nil {
		return ValueOrder{}, err
	}
	return RankValue(nodes, RankOptions{Today: today, DueSoonDays: days}), nil
}

// dueSoonDays reads order.due_soon_days; unset or not a number is the default.
func dueSoonDays(s store.Store) (int, error) {
	v, err := s.Config(DueSoonKey)
	if err != nil {
		return 0, err
	}
	if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n >= 0 {
		return n, nil
	}
	return DefaultDueSoonDays, nil
}

// OrderInput is the input of order.
type OrderInput struct {
	Kind string `json:"kind,omitempty" query:"kind"` // keep one kind: incident, goal, task, question, ask, meeting, work
}

// OrderOutput is the output of order.
type OrderOutput struct {
	Rows []ValueRow `json:"rows"`
	// HeldUp lists put-off items that hold up work due before they return.
	HeldUp []HeldUp `json:"held_up"`
}

// OrderOp lists what needs doing in value order.
var OrderOp = defineRead("order", "List open incidents, goals, tasks, questions, asks, meetings, and agent work in value order, each with why it is there and its bucket. Actionable now comes first: incidents, then items overdue or due within order.due_soon_days (default 2) by date, then the rest by impact (what it unblocks). Not yet startable comes last, by the day it can start: a meeting after today, a call scheduled after today, and anything that depends on one. Deferred items are left out", func(ctx context.Context, d Deps, in OrderInput) (OrderOutput, error) {
	o, err := Order(ctx, d.Store)
	if err != nil {
		return OrderOutput{}, err
	}
	out := OrderOutput{Rows: []ValueRow{}, HeldUp: o.Held}
	for _, r := range o.Rows {
		if in.Kind == "" || r.Kind == in.Kind {
			out.Rows = append(out.Rows, r)
		}
	}
	return out, nil
})

// shorten cuts a title to 32 characters for a reason line.
func shorten(s string) string {
	if r := []rune(s); len(r) > 32 {
		return string(r[:31]) + "…"
	}
	return s
}

// meetingDay is the local calendar day of a meeting's start: a time with a
// zone, or a date.
func meetingDay(start string) (string, bool) {
	if t, err := time.Parse(time.RFC3339, start); err == nil {
		return t.In(time.Local).Format(dateLayout), true
	}
	if _, err := time.Parse(dateLayout, start); err == nil {
		return start, true
	}
	return "", false
}
