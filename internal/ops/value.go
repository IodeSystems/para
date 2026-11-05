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
// Today, next, agenda, and the PWA. It is a rule an agent does not redo by
// reasoning:
//
// Every listed item is either actionable now or not yet startable. Not yet
// startable is a booked meeting whose day is after today, a call or work with
// someone scheduled after today, and anything that depends (depends_on, through
// any chain) on one of those. Actionable now comes first:
//
//  1. Open incidents, worst severity first.
//  2. Items overdue or due within order.due_soon_days (default 2) by their
//     effective due date, soonest first.
//  3. Reply: someone waits on the owner (an ask made to the owner, an incoming
//     message that expects a response) and it takes under an hour. Longest
//     waiting cost first: days waited times the people waiting.
//  4. Focus: the order.focus_count (default 3) items of the rest with the best
//     score, (impact+1) / effort weight (xs 1, s 2, m 4, l 8). A goal, agent
//     work, and an item that waits on another open item are not picked. An
//     item whose effective due date is within order.goal_soon_days (default
//     14) scores double, for focus and for the order of the rest.
//  5. Recheck: an item overdue by more than order.stale_days (default 30; 0 is
//     off) is not due soon. It asks "still needed?", oldest date first. An
//     incident is never rechecked.
//  6. Everything else by the same score, highest first.
//
// Not yet startable comes last, by the day it can start.
//
// Effort is the item's effort field. Unset, it is xs for an ask made to the
// owner, a question, and an incoming message that expects a response, and m
// for the rest. Prep marks an item an agent can prepare in full so the owner
// only approves: an ask made to the owner, a question, an outgoing draft, and
// an incoming message that expects a response.
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
	// KindIO is an io item: an incoming message that expects a response, or
	// an outgoing draft.
	KindIO = "io"
)

// Groups of ValueRow, in order. All but not_yet are actionable now.
const (
	GroupIncident = "incident"
	GroupDueSoon  = "due_soon"
	GroupReply    = "reply"
	GroupFocus    = "focus"
	GroupRecheck  = "recheck"
	GroupImpact   = "impact"
	GroupNotYet   = "not_yet"
)

// FocusCountKey is the vault setting of how many items the focus group holds.
const FocusCountKey = "order.focus_count"

// DefaultFocusCount is the size of the focus group when the vault sets none.
const DefaultFocusCount = 3

// StaleDaysKey is the vault setting of how many days overdue an item is
// before it is rechecked, not due soon. 0 is off.
const StaleDaysKey = "order.stale_days"

// DefaultStaleDays is the recheck window when the vault sets none.
const DefaultStaleDays = 30

// GoalSoonKey is the vault setting of how many days ahead a due date doubles
// the focus score. 0 is off.
const GoalSoonKey = "order.goal_soon_days"

// DefaultGoalSoonDays is the goal boost window when the vault sets none.
const DefaultGoalSoonDays = 14

// effortWeight is the divisor of the score; effortMinutes is the estimate of
// a reply, which only takes xs and s.
var (
	effortWeight  = map[model.Effort]int{model.EffortXS: 1, model.EffortS: 2, model.EffortM: 4, model.EffortL: 8}
	effortMinutes = map[model.Effort]int{model.EffortXS: 10, model.EffortS: 30}
	effortWords   = map[model.Effort]string{model.EffortXS: "about 10 min", model.EffortS: "about 30 min", model.EffortM: "half a day", model.EffortL: "a day or more"}
)

// checkEffort refuses an effort that is not xs, s, m, or l. Empty is not set.
func checkEffort(e model.Effort) error {
	if e != "" && !slices.Contains(model.Efforts, e) {
		return fmt.Errorf("effort %q: want xs (under 15 min), s (under an hour), m (half a day), or l (a day or more)", e)
	}
	return nil
}

// effortOf is the node's effort and whether it was set.
func effortOf(n ValueNode) (model.Effort, bool) {
	if effortWeight[n.Effort] > 0 {
		return n.Effort, true
	}
	if n.Owed || n.Kind == KindQuestion {
		return model.EffortXS, false
	}
	return model.EffortM, false
}

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
	Today        string // YYYY-MM-DD
	DueSoonDays  int    // overdue or due within this many days of Today ranks by date
	FocusCount   int    // how many items the focus group holds; 0 is none
	StaleDays    int    // overdue by more than this many days is recheck, not due soon; 0 is off
	GoalSoonDays int    // a date within this many days doubles the focus score; 0 is off
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
	// Owed is true when someone waits on the owner: an ask made to the owner
	// (rfx direction in), or an incoming message that expects a response.
	Owed bool
	// Arrived is the day the owner's wait began, YYYY-MM-DD; "" is Created.
	// Waiters is how many people wait; under 1 counts as 1. Both are read
	// only when Owed.
	Arrived string
	Waiters int
	// Effort is the item's effort field, or "" when not set.
	Effort model.Effort
	// Prep is true when an agent can prepare the item in full and the owner
	// only approves.
	Prep bool
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
	// Owed is true when someone waits on the owner: an ask made to the owner,
	// or an incoming message that expects a response.
	Owed bool `json:"owed,omitempty"`
	// Effort is xs, s, m, or l; EffortSet is false when it is the default.
	Effort    model.Effort `json:"effort"`
	EffortSet bool         `json:"effort_set,omitempty"`
	// WaitingDays is the waiting cost of an owed item: days since the wait
	// began times the people waiting.
	WaitingDays int `json:"waiting_days,omitempty"`
	// Prep is true when an agent can prepare it in full and the owner only
	// approves.
	Prep bool `json:"prep,omitempty"`
}

// OrderSummary sums up the reply group: how many items, about how many
// minutes in all (xs 10, s 30), and how many an agent can prepare.
type OrderSummary struct {
	Reply        int `json:"reply"`
	ReplyMinutes int `json:"reply_minutes"`
	ReplyPrep    int `json:"reply_prep"`
	Focus        int `json:"focus"`
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
	Rows    []ValueRow   `json:"rows"`
	Held    []HeldUp     `json:"held_up"`
	Summary OrderSummary `json:"summary"`
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
	goalSoon := ""
	if t, err := time.Parse(dateLayout, opt.Today); err == nil && opt.GoalSoonDays > 0 {
		goalSoon = t.AddDate(0, 0, opt.GoalSoonDays).Format(dateLayout)
	}
	boosted := map[string]bool{} // impact rows whose date is within the goal window
	var rows []ValueRow
	var held []HeldUp
	focusable := map[string]func() string{} // impact rows focus may pick, to their reason
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
		r := ValueRow{ID: n.ID, Kind: n.Kind, Title: n.Title, Due: n.Due, Proposal: n.Proposal, Items: items[n.ID], Owed: n.Owed, Prep: n.Prep}
		r.Effort, r.EffortSet = effortOf(n)
		waitDays := 0
		if n.Owed {
			waitDays = daysBetween(cmp.Or(n.Arrived, n.Created), opt.Today)
			r.WaitingDays = waitDays * max(n.Waiters, 1)
		}
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
		case eff != "" && opt.StaleDays > 0 && daysBetween(eff, opt.Today) > opt.StaleDays:
			r.Group = GroupRecheck
		case eff != "" && eff <= soon:
			r.Group = GroupDueSoon
		case n.Owed && effortWeight[r.Effort] <= effortWeight[model.EffortS]:
			r.Group = GroupReply
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
		boost := r.Group == GroupImpact && goalSoon != "" && eff != "" && eff <= goalSoon
		boosted[n.ID] = boost
		if r.Group == GroupImpact && len(waits) == 0 && n.Kind != KindGoal && n.Kind != KindWork {
			// Focus picks among these below; the reason is set then.
			focusable[n.ID] = func() string {
				lead := "focus: " + effortWords[r.Effort]
				if r.Blocks > 0 {
					lead = fmt.Sprintf("focus: unblocks %d, %s", r.Blocks, effortWords[r.Effort])
				}
				rest, rn, rfrom := r, n, from
				rest.Blocks = 0
				if boost {
					// The date is named up front, so the tail does not repeat it.
					label := "due " + ShortDate(eff)
					if from != nil {
						label = "goal due " + ShortDate(eff)
						if from.Kind != KindGoal {
							label = shorten(from.Title) + " due " + ShortDate(eff)
						}
					}
					lead = strings.Replace(lead, "focus: ", "focus: "+label+"; ", 1)
					rn.Due, rfrom = "", nil
				}
				if w := why(rn, rest, rfrom, waits); w != whyNone {
					lead += "; " + w
				}
				return lead
			}
		}
		switch {
		case r.Group == GroupReply:
			who := "someone waits"
			if n.Waiters > 1 {
				who = fmt.Sprintf("%d people wait", n.Waiters)
			}
			lead := fmt.Sprintf("%s %s; %s", who, plural(waitDays, "day"), effortWords[r.Effort])
			if r.Why != whyNone {
				lead += "; " + r.Why
			}
			r.Why = lead
		case r.Group == GroupNotYet && r.After != "":
			u := by[r.After]
			what := shorten(cmp.Or(u.Title, u.ID))
			if u.Kind == KindMeeting {
				what = "meeting " + what
			}
			r.Why = "not yet, waits on " + what + " on " + ShortDate(r.Starts) + "; " + r.Why
		case r.Group == GroupNotYet:
			r.Why = "not yet, starts " + ShortDate(r.Starts) + "; " + r.Why
		case r.Group == GroupRecheck:
			r.Why = "overdue since " + ShortDate(eff) + " (" + ageWords(daysBetween(eff, opt.Today)) + "); recheck: still needed?"
		case r.Group == GroupDueSoon && eff < opt.Today:
			r.Why = "overdue; " + r.Why
		case r.Group == GroupDueSoon:
			r.Why = "due soon; " + r.Why
		}
		rows = append(rows, r)
	}
	rank := map[string]int{GroupIncident: 0, GroupDueSoon: 1, GroupReply: 2, GroupFocus: 3, GroupRecheck: 4, GroupImpact: 5, GroupNotYet: 6}
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
	// weigh is impact plus one, doubled for a date within the goal window.
	weigh := func(r ValueRow) int {
		if boosted[r.ID] {
			return (r.Impact + 1) * 2
		}
		return r.Impact + 1
	}
	// score puts the better (impact+1) / effort weight first, without division.
	score := func(a, b ValueRow) int {
		return cmp.Compare(weigh(b)*effortWeight[a.Effort], weigh(a)*effortWeight[b.Effort])
	}
	order := func(a, b ValueRow) int {
		if c := cmp.Compare(rank[a.Group], rank[b.Group]); c != 0 {
			return c
		}
		var c int
		switch a.Group {
		case GroupIncident:
			return cmp.Or(cmp.Compare(sev[a.ID], sev[b.ID]), cmp.Compare(created[a.ID], created[b.ID]), cmp.Compare(a.ID, b.ID))
		case GroupDueSoon:
			c = cmp.Or(due(a, b), cmp.Compare(b.Impact, a.Impact))
		case GroupRecheck:
			c = cmp.Or(due(a, b), cmp.Compare(b.Impact, a.Impact))
		case GroupReply:
			c = cmp.Or(cmp.Compare(b.WaitingDays, a.WaitingDays), due(a, b))
		case GroupFocus, GroupImpact:
			c = cmp.Or(score(a, b), due(a, b))
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
	}
	slices.SortStableFunc(rows, order)
	// Focus takes the best scored impact rows it may pick; the order within
	// stays the same, so one more sort moves them up as a group.
	var sum OrderSummary
	for i := range rows {
		if sum.Focus < opt.FocusCount && rows[i].Group == GroupImpact && focusable[rows[i].ID] != nil {
			rows[i].Group, rows[i].Why = GroupFocus, focusable[rows[i].ID]()
			sum.Focus++
		}
	}
	slices.SortStableFunc(rows, order)
	for _, r := range rows {
		if r.Group == GroupReply {
			sum.Reply++
			sum.ReplyMinutes += effortMinutes[r.Effort]
			if r.Prep {
				sum.ReplyPrep++
			}
		}
	}
	slices.SortFunc(held, func(a, b HeldUp) int { return cmp.Or(cmp.Compare(a.Due, b.Due), cmp.Compare(a.ID, b.ID)) })
	if rows == nil {
		rows = []ValueRow{}
	}
	if held == nil {
		held = []HeldUp{}
	}
	return ValueOrder{Rows: rows, Held: held, Summary: sum}
}

// daysBetween is the whole days from day a to day b, both YYYY-MM-DD; 0 when
// either does not parse or b is not after a.
func daysBetween(a, b string) int {
	ta, err1 := time.Parse(dateLayout, a)
	tb, err2 := time.Parse(dateLayout, b)
	if err1 != nil || err2 != nil || !tb.After(ta) {
		return 0
	}
	return int(tb.Sub(ta).Hours() / 24)
}

// ageWords is a rough age: days under two weeks, weeks under two months,
// then months.
func ageWords(days int) string {
	switch {
	case days < 14:
		return plural(days, "day")
	case days < 60:
		return plural(days/7, "week")
	}
	return plural(days/30, "month")
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

// whyNone is the reason of a row with nothing else to say.
const whyNone = "blocks nothing, no date"

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
	if n.Owed {
		parts = append(parts, "asked of you")
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
		return whyNone
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
		case model.TypeIncident, model.TypeProject, model.TypeTask, model.TypeQuestion, model.TypeRFX, model.TypeMeeting, model.TypeRequest, model.TypeIO:
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
			n.Parent, n.Effort = m.Parent.Target(), m.Effort
			n.Deps = targets(m.DependsOn)
			if (m.Kind == model.KindCall || m.Kind == model.KindWorkWith) && m.Scheduled != "" {
				n.Start, _ = meetingDay(m.Scheduled)
			}
		case *model.Question:
			if m.Status != model.QuestionOpen {
				continue
			}
			n.Kind, n.Deps, n.Proposal = KindQuestion, targets(m.DependsOn), m.HasProposal()
			n.Effort, n.Prep = m.Effort, true
		case *model.RFX:
			if m.Status != model.RFXOpen {
				continue
			}
			n.Kind, n.Due, n.Deps, n.Effort = KindAsk, string(m.NextNudge), targets(m.DependsOn), m.Effort
			// An ask to the owner is owed; an ask the owner made waits on
			// someone else and is listed for its nudge date.
			if m.Direction == model.RFXIn {
				n.Owed, n.Prep, n.Waiters, n.Arrived = true, true, len(m.Targets), askArrived(m)
			}
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
		case *model.IO:
			// Listed: an incoming message whose sender waits for a reply, and
			// an outgoing draft. An agent can prepare both.
			switch {
			case m.Direction == model.IOIn && (m.State == model.IOUnread || m.State == model.IORead) && m.Expects == model.ExpectsResponse:
				n.Owed, n.Waiters = true, 1
				n.Arrived, _ = meetingDay(m.At)
			case m.Direction == model.IOOut && m.State == model.IODraft:
			default:
				continue
			}
			n.Kind, n.Effort, n.Prep = KindIO, m.Effort, true
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
	days, err := configInt(s, DueSoonKey, DefaultDueSoonDays)
	if err != nil {
		return ValueOrder{}, err
	}
	focus, err := configInt(s, FocusCountKey, DefaultFocusCount)
	if err != nil {
		return ValueOrder{}, err
	}
	stale, err := configInt(s, StaleDaysKey, DefaultStaleDays)
	if err != nil {
		return ValueOrder{}, err
	}
	goalSoon, err := configInt(s, GoalSoonKey, DefaultGoalSoonDays)
	if err != nil {
		return ValueOrder{}, err
	}
	return RankValue(nodes, RankOptions{Today: today, DueSoonDays: days, FocusCount: focus, StaleDays: stale, GoalSoonDays: goalSoon}), nil
}

// configInt reads a vault setting that is a count; unset or not a number is
// def.
func configInt(s store.Store, key string, def int) (int, error) {
	v, err := s.Config(key)
	if err != nil {
		return 0, err
	}
	if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n >= 0 {
		return n, nil
	}
	return def, nil
}

// askArrived is the day an ask to the owner came: the earliest date a target
// sent it, else the earliest date one was added, else "" (its created date).
func askArrived(m *model.RFX) string {
	sent, added := "", ""
	for _, t := range m.Targets {
		if s := string(t.Sent); s != "" && (sent == "" || s < sent) {
			sent = s
		}
		if a := string(t.Added); a != "" && (added == "" || a < added) {
			added = a
		}
	}
	return cmp.Or(sent, added)
}

// OrderInput is the input of order.
type OrderInput struct {
	Kind string `json:"kind,omitempty" query:"kind"` // keep one kind: incident, goal, task, question, ask, meeting, work, io
}

// OrderOutput is the output of order.
type OrderOutput struct {
	Rows []ValueRow `json:"rows"`
	// HeldUp lists put-off items that hold up work due before they return.
	HeldUp []HeldUp `json:"held_up"`
	// Summary sums up the reply group and counts the focus group, over all
	// rows whatever the kind filter.
	Summary OrderSummary `json:"summary"`
}

// OrderOp lists what needs doing in value order.
var OrderOp = defineRead("order", "List open incidents, goals, tasks, questions, asks, meetings, agent work, and io items (an incoming message that expects a response, an outgoing draft) in value order, each with why it is there, its group, bucket, effort, waiting cost, and whether an agent can prepare it (prep). This is the ranking: use it as it is, do not re-rank by reasoning. Actionable now comes first: incidents; items overdue or due within order.due_soon_days (default 2) by date; reply, where someone waits on the owner and it takes under an hour, longest waiting cost first; focus, the order.focus_count (default 3) best by (impact+1) per effort, doubled when the date, or the date of the goal it feeds, is within order.goal_soon_days (default 14); recheck, overdue by more than order.stale_days (default 30), oldest first, to ask if it is still needed; then the rest by that score. Not yet startable comes last, by the day it can start: a meeting after today, a call scheduled after today, and anything that depends on one. Deferred items are left out. The summary counts the reply batch, its minutes, and how many have prep", func(ctx context.Context, d Deps, in OrderInput) (OrderOutput, error) {
	o, err := Order(ctx, d.Store)
	if err != nil {
		return OrderOutput{}, err
	}
	out := OrderOutput{Rows: []ValueRow{}, HeldUp: o.Held, Summary: o.Summary}
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
