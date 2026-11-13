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
//  3. Owed: promises to people. An open obligation the owner owes people
//     (owed_to, with, interested, or attendees; or a reply obligation) whose
//     date (due, or the day of its start) is within order.owed_soon_days
//     (default 7), and its open prep (its children, whose parent it is, and
//     what it depends on, through any chain). Prep takes a backward date:
//     the obligation's date minus the prep's effort lead (xs and s 0 days, m
//     1, l 2; unset effort is the default below), and that date is its
//     effective due when it is earlier. Soonest effective date first. A
//     reply obligation that takes under an hour stays in the reply batch.
//  4. Reply: someone waits on the owner (an ask made to the owner, an incoming
//     message that expects a response) and it takes under an hour. Longest
//     waiting cost first: days waited times the people waiting.
//  5. Focus: the order.focus_count (default 3) items of the rest with the best
//     score, (impact+1) / effort weight (xs 1, s 2, m 4, l 8). A goal, agent
//     work, and an item that waits on another open item are not picked. An
//     item whose effective due date is within order.goal_soon_days (default
//     14) scores double, for focus and for the order of the rest.
//  6. Recheck: an item overdue by more than order.stale_days (default 30; 0 is
//     off) is not due soon. It asks "still needed?", oldest date first. An
//     incident is never rechecked.
//  7. Everything else by the same score, highest first.
//
// A tie in score goes to the item with the better reason (reason.go): owed
// before goal before upkeep before none, then by effective due date.
//
// A booked meeting after today is not yet startable itself; its prep is
// startable and rises through the owed tier.
//
// Not yet startable comes last, by the day it can start. Within each group,
// ready items (prepared by an agent and not changed since, Snapshot.Ready)
// come before raw ones; focus picks by score before that.
//
// Effort is the item's effort field. Unset, it is xs for an ask made to the
// owner, a question, and an incoming message that expects a response, and m
// for the rest. Prep marks an item an agent can prepare in full so the owner
// only approves: an ask made to the owner, a question, an outgoing draft, an
// incoming message that expects a response, and a task (an agent looks for
// evidence that it is done and proposes a close, or says there is none).
//
// Effective due is the earliest of the item's own date, the date of any open
// item downstream of it (anything that waits on it, through depends_on, and
// the goal it leads to), and the backward date of prep for a promise. The
// edges downstream are the reason walker's hops (reason.go: waitsOn, hop), so
// an item leads to the same goals here as in its Why.
// A date counts through the due soon window, and a backward date through the
// owed soon window: prep for a meeting next week rises a week ahead. Ties: the effective due date
// (none last), the owner's rank between two goals, the oldest, then the id.
// Impact is the number of open items it unblocks, through any chain; a goal
// counts twice. An item also counts toward its parent, the incident it
// follows up, and the item a question is about (none of them waits on it). A deferred item, and
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
	// KindPool is an idea pool whose trigger fired (pool.go): one row per
	// pool, id "pool:<name>", not a document. The ideas are not rows.
	KindPool = "pool"
)

// Groups of ValueRow, in order. All but not_yet are actionable now.
const (
	GroupIncident = "incident"
	GroupDueSoon  = "due_soon"
	GroupOwed     = "owed"
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

// OwedSoonKey is the vault setting of how many days ahead a promise to
// people, and its prep, rank in the owed tier. 0 is off.
const OwedSoonKey = "order.owed_soon_days"

// DefaultOwedSoonDays is the owed tier window when the vault sets none.
const DefaultOwedSoonDays = 7

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
	OwedSoonDays int    // a promise within this many days, and its prep, rank in the owed tier; 0 is off
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
	Updated string   // the last write, YYYY-MM-DD; shown, not ranked
	Deps    []string // ids this waits on
	Goal    string   // the goal it names (its project)
	Parent  string   // the task or obligation it is an action item of; not a dependency
	// Leads are the other items it leads to without waiting on them: the
	// incident a task follows up, the item a question is about, a second goal.
	// Like Parent, they count toward impact and the due date, not blocks.
	Leads []string
	Sev   string // for an incident: sev1, sev2, sev3
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
	// Reply marks a reply obligation (ioreply.go): the reply batch holds it
	// until its due date passes, though its due date is near by design.
	Reply bool
	// Promise marks an open obligation the owner owes people; PromiseDate is
	// its date (due, or the day of its start). Its prep rises with it.
	Promise     bool
	PromiseDate string
	// ReasonRank is the item's best reason for a tie in score: owed 0, goal
	// 1, upkeep 2, none 3.
	ReasonRank int
	// Effort is the item's effort field, or "" when not set.
	Effort model.Effort
	// Prep is true when an agent can prepare the item in full and the owner
	// only approves.
	Prep bool
	// Ready is set when the item is prepared and not stale (Snapshot.Ready):
	// what was prepared and the document that holds it.
	Ready *Readiness
	// NeedsYou and Reason are the predicate (NeedsYou) on the item.
	NeedsYou bool
	Reason   string
}

// ValueRow is one listed item with the reason it is where it is.
type ValueRow struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Title  string `json:"title"`
	Group  string `json:"group"`
	Due    string `json:"due,omitempty"`     // own date
	EffDue string `json:"eff_due,omitempty"` // effective date
	Impact int    `json:"impact"`            // weighted count
	Blocks int    `json:"blocks"`            // open items it unblocks, goals not counted
	Items  int    `json:"items,omitempty"`   // open action items (children) of a task
	Parent string `json:"parent,omitempty"`  // the task or obligation this is an action item of
	// For is the promise this is prep for, in the owed tier; Back is the
	// backward date it falls due by.
	For      string `json:"for,omitempty"`
	Back     string `json:"back,omitempty"`
	Why      string `json:"why"`                // in words
	Proposal bool   `json:"proposal,omitempty"` // an answer waits to be checked
	// Bucket is now (actionable now) or not_yet (not yet startable).
	Bucket string `json:"bucket"`
	// PartOf is what the item is part of, nearest first (theme.go).
	PartOf []PartOfRef `json:"part_of,omitempty" partof:"ID"`
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
	// Ready is true when an agent prepared it and it did not change since
	// (Snapshot.Ready); ReadyKind is what was prepared (draft, answer,
	// fields, brief, plan) and ReadyRef the document that holds it, "" when
	// it is on the item. Within a group, ready rows come first.
	Ready     bool               `json:"ready,omitempty"`
	ReadyKind model.PreparedKind `json:"ready_kind,omitempty"`
	ReadyRef  string             `json:"ready_ref,omitempty"`
	// NeedsYou is true on every row of order: it lists only what needs the
	// owner. Reason says why, in words.
	NeedsYou bool   `json:"needs_you"`
	Reason   string `json:"reason,omitempty"`
	// Created and Updated are the item's own dates, YYYY-MM-DD.
	Created string `json:"created,omitempty"`
	Updated string `json:"updated,omitempty"`
	// boost is true when the row's effective date is within the goal window:
	// its score doubles (scoreOf).
	boost bool
}

// scoreOf is a row's score as a fraction: (impact+1), doubled for a date
// within the goal window, over the effort weight. The order compares two
// without division (scoreCmp); pools show it as a number.
func scoreOf(r ValueRow) (num, den int) {
	num = r.Impact + 1
	if r.boost {
		num *= 2
	}
	return num, max(effortWeight[r.Effort], 1)
}

// scoreCmp puts the better score first.
func scoreCmp(a, b ValueRow) int {
	an, ad := scoreOf(a)
	bn, bd := scoreOf(b)
	return cmp.Compare(bn*ad, an*bd)
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
	by, down, parentEdge := valueGraph(nodes)
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
	back, prepFor := backDates(nodes, by, opt)
	soon := opt.Today
	if t, err := time.Parse(dateLayout, opt.Today); err == nil {
		soon = t.AddDate(0, 0, opt.DueSoonDays).Format(dateLayout)
	}
	goalSoon := ""
	if t, err := time.Parse(dateLayout, opt.Today); err == nil && opt.GoalSoonDays > 0 {
		goalSoon = t.AddDate(0, 0, opt.GoalSoonDays).Format(dateLayout)
	}
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
		r := ValueRow{ID: n.ID, Kind: n.Kind, Title: n.Title, Due: n.Due, Proposal: n.Proposal, Items: items[n.ID], Owed: n.Owed, Prep: n.Prep,
			NeedsYou: n.NeedsYou, Reason: n.Reason, Created: n.Created, Updated: n.Updated}
		if n.Ready != nil {
			r.Ready, r.ReadyKind, r.ReadyRef = true, n.Ready.Kind, n.Ready.Ref
		}
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
		if bd, ok := back[n.ID]; ok {
			r.For, r.Back = prepFor[n.ID], bd
			if eff == "" || bd < eff {
				eff = bd
			}
		}
		owedTier := r.For != "" || n.Promise && owedSoon(n.PromiseDate, opt)
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
		case n.Reply && effortWeight[r.Effort] <= effortWeight[model.EffortS] && (eff == "" || eff >= opt.Today):
			r.Group = GroupReply
		case eff != "" && eff <= soon:
			r.Group = GroupDueSoon
		case owedTier:
			r.Group = GroupOwed
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
		r.boost = boost
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
		case r.Group == GroupOwed && r.For != "":
			o := by[r.For]
			r.Why = "prep for " + shorten(cmp.Or(o.Title, o.ID)) + " on " + ShortDate(o.PromiseDate) + ", by " + ShortDate(r.Back) + "; " + r.Why
		case r.Group == GroupOwed:
			r.Why = "owed " + ShortDate(n.PromiseDate) + "; " + r.Why
		}
		rows = append(rows, r)
	}
	rank := map[string]int{GroupIncident: 0, GroupDueSoon: 1, GroupOwed: 2, GroupReply: 3, GroupFocus: 4, GroupRecheck: 5, GroupImpact: 6, GroupNotYet: 7}
	created := map[string]string{}
	reasonRank := map[string]int{}
	sev := map[string]string{}
	goalRank := map[string]int{}
	for _, n := range nodes {
		created[n.ID], sev[n.ID], reasonRank[n.ID] = n.Created, n.Sev, n.ReasonRank
		if n.Kind == KindGoal && n.Rank > 0 {
			goalRank[n.ID] = n.Rank
		}
	}
	// due compares effective dates, an item with none last.
	due := func(a, b ValueRow) int { return cmp.Compare(dueKey(a.EffDue), dueKey(b.EffDue)) }
	// readyFirst puts ready rows first within a group. It is off while focus
	// picks, so focus picks by score alone.
	readyFirst := false
	order := func(a, b ValueRow) int {
		if c := cmp.Compare(rank[a.Group], rank[b.Group]); c != 0 {
			return c
		}
		if readyFirst && a.Ready != b.Ready {
			if a.Ready {
				return -1
			}
			return 1
		}
		var c int
		switch a.Group {
		case GroupIncident:
			return cmp.Or(cmp.Compare(sev[a.ID], sev[b.ID]), cmp.Compare(created[a.ID], created[b.ID]), cmp.Compare(a.ID, b.ID))
		case GroupDueSoon, GroupOwed:
			c = cmp.Or(due(a, b), cmp.Compare(b.Impact, a.Impact))
		case GroupRecheck:
			c = cmp.Or(due(a, b), cmp.Compare(b.Impact, a.Impact))
		case GroupReply:
			c = cmp.Or(cmp.Compare(b.WaitingDays, a.WaitingDays), due(a, b))
		case GroupFocus, GroupImpact:
			c = cmp.Or(scoreCmp(a, b), cmp.Compare(reasonRank[a.ID], reasonRank[b.ID]), due(a, b))
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
	readyFirst = true
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

// valueGraph indexes the nodes and their downstream edges: down maps an id
// to the items downstream of it (what waits on it, its goal, its parent, its
// leads), and parentEdge marks the "item next" pairs where next does not wait
// on item, so they count toward impact and the due date but not toward blocks.
func valueGraph(nodes []ValueNode) (by map[string]*ValueNode, down map[string][]string, parentEdge map[string]bool) {
	by, down, parentEdge = map[string]*ValueNode{}, map[string][]string{}, map[string]bool{}
	for i := range nodes {
		by[nodes[i].ID] = &nodes[i]
	}
	for _, n := range nodes {
		for _, dep := range n.Deps {
			if by[dep] != nil {
				down[dep] = append(down[dep], n.ID)
			}
		}
		if n.Goal != "" && by[n.Goal] != nil {
			down[n.ID] = append(down[n.ID], n.Goal)
		}
		for _, l := range append([]string{n.Parent}, n.Leads...) {
			if l != "" && by[l] != nil && !parentEdge[n.ID+" "+l] {
				down[n.ID] = append(down[n.ID], l)
				parentEdge[n.ID+" "+l] = true
			}
		}
	}
	return by, down, parentEdge
}

// owedSoon reports whether a promise's date is within the owed soon window:
// on or before today plus order.owed_soon_days.
func owedSoon(date string, opt RankOptions) bool {
	if date == "" || opt.OwedSoonDays <= 0 {
		return false
	}
	t, err := time.Parse(dateLayout, opt.Today)
	return err == nil && date <= t.AddDate(0, 0, opt.OwedSoonDays).Format(dateLayout)
}

// backDates gives the open prep of each promise in the owed soon window its
// backward date: the promise's date minus the prep's effort lead. Prep is
// the promise's children (Parent) and what it depends on (Deps), through any
// chain; a deferred item is not walked. An item that is prep for more than
// one promise takes the earliest date. prepFor names the promise.
func backDates(nodes []ValueNode, by map[string]*ValueNode, opt RankOptions) (back, prepFor map[string]string) {
	back, prepFor = map[string]string{}, map[string]string{}
	kids := map[string][]string{}
	for _, n := range nodes {
		if n.Parent != "" && by[n.Parent] != nil {
			kids[n.Parent] = append(kids[n.Parent], n.ID)
		}
	}
	for _, o := range nodes {
		if !o.Promise || o.Deferred || !owedSoon(o.PromiseDate, opt) {
			continue
		}
		seen := map[string]bool{o.ID: true}
		queue := []string{o.ID}
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			for _, id := range append(slices.Clone(by[cur].Deps), kids[cur]...) {
				p := by[id]
				if p == nil || seen[id] || p.Deferred {
					continue
				}
				seen[id] = true
				queue = append(queue, id)
				e, _ := effortOf(*p)
				bd := backDate(o.PromiseDate, e)
				if old, ok := back[id]; !ok || bd < old || bd == old && o.ID < prepFor[id] {
					back[id], prepFor[id] = bd, o.ID
				}
			}
		}
	}
	return back, prepFor
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

func plural(n int, word string) string {
	if n == 1 {
		return "1 " + word
	}
	return fmt.Sprintf("%d %ss", n, word)
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

// valueNodes reads the open items of the vault. Every open item is a node, so
// impact and dates flow through the whole graph; only what needs the owner
// (NeedsYou) is listed: the rest is Hidden, and what is put off is Deferred.
// With all set, every open item the owner works is listed (goals, tasks that
// wait, asks that wait), for views that rank open work and not what needs the
// owner: health and align.
func valueNodes(sn *Snapshot, all bool) ([]ValueNode, error) {
	horizon := sn.Today
	if t, err := time.Parse(dateLayout, sn.Today); err == nil {
		horizon = t.AddDate(0, 0, 14).Format(dateLayout)
	}
	var nodes []ValueNode
	reasons := newReasonGraph(sn.docs)
	for _, doc := range sn.Docs() {
		switch doc.Meta.Header().Type {
		case model.TypeIncident, model.TypeGoal, model.TypeTask, model.TypeQuestion, model.TypeRFX, model.TypeMeeting, model.TypeRequest, model.TypeIO, model.TypeObligation:
		default:
			continue
		}
		// An obligation is a node of its task kind; one owed to people is owed.
		// An ask to the owner folded into an obligation (format 7) keeps the
		// ask's signals: when it came, who waits, and what it blocks (its kept
		// keys targets and blocks).
		owed, askIn := false, false
		reply := ""
		promise, promiseDate := false, ""
		var askTargets []model.Target
		if o, ok := doc.Meta.(*model.Obligation); ok {
			owed = len(o.OwedTo) > 0 || o.ReplyTo != "" // a reply is owed to its sender, linked or not
			reply = o.ReplyTo.Target()
			// A promise to people the owner keeps: its prep rises with it.
			promise = o.Open() && len(o.OwedBy) == 0 && promised(o) && o.Attend != model.AttendNo
			promiseDate = obligationDate(o)
			if o.FoldedFrom == model.TypeRFX {
				askIn = true
				doc.Kept("targets", &askTargets)
			}
		}
		stored := doc.Meta.Header().Type
		doc = obligationShape(doc) // a task of kind incident is an incident node: incidents come first
		need := sn.Need(doc.ID)
		if need.Life == LifeClosed {
			continue
		}
		hold := sn.Later(doc.ID)
		var ready *Readiness
		if rd, ok := sn.Ready(doc.ID); ok {
			ready = &rd
		}
		n := ValueNode{ID: doc.ID, Title: doc.Title, Created: string(doc.Meta.Header().Created), Updated: string(doc.Meta.Header().Updated), Deferred: hold.any(),
			Hidden: !all && need.Life == LifeOpen && !need.Yes, NeedsYou: need.Yes, Reason: need.Reason, Ready: ready,
			Promise: promise && promiseDate != "", PromiseDate: promiseDate, ReasonRank: reasons.reasonRank(doc.ID)}
		// What it waits on and where it leads come from the reason walker
		// (reason.go): the order and the Why lead to the same goals.
		n.Deps = reasons.waitsOn[doc.ID]
		hp := reasons.hop(doc.ID)
		for _, t := range hp.ends {
			switch {
			case !reasons.isGoal(t):
			case n.Goal == "":
				n.Goal = t
			default:
				n.Leads = append(n.Leads, t)
			}
		}
		for _, st := range hp.next {
			switch {
			case st.How == StepUnblocks: // the waiter's Deps hold it
			case st.How == StepActionItemOf && n.Parent == "":
				n.Parent = st.ID
			default:
				n.Leads = append(n.Leads, st.ID)
			}
		}
		if df := deferralOf(doc.Meta); df != nil {
			n.DeferUntil = df.DeferUntil
			n.WaitOnly, n.WaitDeps, n.ReadyWhy = hold.waitOnly(), hold.Waits, hold.Ready
		}
		// A booked meeting, in either shape, is a meeting node. A task meeting
		// that is not booked is a task below; an older one that is not booked
		// is left out.
		if mt, ok := readMeeting(doc); ok && mt.Status == model.MeetingScheduled {
			day, ok := meetingDay(mt.Start)
			if !ok || meetingSkipped(mt.t) {
				// A meeting the owner skips is not listed and holds nothing up:
				// what depends on it, and the asks it serves, can start.
				continue
			}
			// An optional meeting the owner has not said they go to is not
			// listed (NeedsYou); it still sets the day what depends on it can
			// start.
			n.Kind, n.Due, n.Start = KindMeeting, day, day
			if all {
				n.Hidden = n.Due > horizon || meetingQuiet(mt.t)
			}
			if t, isTask := doc.Meta.(*model.Task); isTask {
				n.Hidden = n.Hidden || all && IsOther(t.Assignee)
			}
			nodes = append(nodes, n)
			continue
		}
		switch m := doc.Meta.(type) {
		case *model.Incident:
			n.Kind, n.Sev = KindIncident, string(m.Severity)
		case *model.Goal:
			// A goal shapes the order and is never listed: it asks nothing.
			n.Kind, n.Due, n.Rank = KindGoal, string(m.Due), m.Rank
			if m.Status != model.GoalActive {
				continue
			}
		case *model.Task:
			if meetingSkipped(m) {
				continue
			}
			n.Kind, n.Due, n.Owed = KindTask, string(m.Due), owed
			// A task an agent can prepare: find it done or no longer needed
			// (a proposed close), or say there is no obvious close.
			n.Prep = stored == model.TypeTask
			if reply != "" {
				n.Reply, n.Prep, n.Waiters = true, true, 1
				if rd, err := sn.Get(reply); err == nil {
					if im, ok := rd.Meta.(*model.IO); ok {
						n.Arrived, _ = meetingDay(im.At)
					}
				}
			}
			if all {
				n.Hidden = IsOther(m.Assignee) || meetingQuiet(m)
			}
			n.Effort = m.Effort
			if (m.Kind == model.KindCall || m.Kind == model.KindWorkWith) && m.Scheduled != "" {
				n.Start, _ = meetingDay(m.Scheduled)
			}
			if askIn {
				n.Owed, n.Prep, n.Waiters = true, true, max(len(askTargets), 1)
				n.Arrived = askArrived(&model.RFX{Targets: askTargets})
			}
		case *model.Question:
			n.Kind, n.Proposal = KindQuestion, m.HasProposal()
			n.Effort, n.Prep = m.Effort, true
		case *model.RFX:
			n.Kind, n.Due, n.Effort = KindAsk, string(m.NextNudge), m.Effort
			// An ask to the owner is owed; an ask the owner made waits on
			// someone else and is listed when it needs a follow-up.
			if m.Direction == model.RFXIn {
				n.Owed, n.Prep, n.Waiters, n.Arrived = true, true, len(m.Targets), askArrived(m)
			}
		case *model.Meeting:
			continue
		case *model.Request:
			// Agent work is a node while open: listed when it waits on the
			// owner (to accept, or it ended needing them).
			n.Kind = KindWork
			if all && !agentHas(m) {
				continue
			}
		case *model.IO:
			// Listed: an incoming message whose sender waits for a reply or that
			// the inbox verdict says needs the owner, an outgoing draft, and an
			// approved draft changed since. An agent can prepare both. Other incoming
			// messages are sorted in the inbox.
			prep := true
			switch {
			case m.Direction == model.IOIn && (m.State == model.IOUnread || m.State == model.IORead) && m.Expects == model.ExpectsResponse && m.Reply == "":
				// With a reply obligation, the obligation is the row.
				n.Owed, n.Waiters = true, 1
				n.Arrived, _ = meetingDay(m.At)
			case m.Direction == model.IOIn && (m.State == model.IOUnread || m.State == model.IORead) && m.Reply == "" && sn.inboundNeedsOwner(m):
				// The inbox verdict says it needs the owner (triage, a
				// high-priority sender or source): the same rule as the
				// Input inbox "needs" tab. A digest stays out. Nothing to
				// prepare: it needs the owner's look, so no prepare request.
				n.Arrived, _ = meetingDay(m.At)
				prep = false
			case m.Direction == model.IOOut && m.State == model.IODraft:
			case m.Direction == model.IOOut && m.State == model.IOReady && !m.Approved(doc.Body):
				// Approved, then changed: it is the owner's turn again, to
				// approve it again; an agent has nothing to prepare.
				prep = false
			default:
				continue
			}
			n.Kind, n.Effort, n.Prep = KindIO, m.Effort, prep
		}
		if n.Kind == KindGoal && !all {
			n.Hidden = !n.Deferred
		}
		nodes = append(nodes, n)
	}
	return nodes, nil
}

// Order reads the vault and returns its value order. Every surface calls it.
func Order(ctx context.Context, s store.Store) (ValueOrder, error) {
	return OrderAt(ctx, s, s.Now().Format(dateLayout))
}

// OrderAt is Order for the local date today (YYYY-MM-DD).
func OrderAt(ctx context.Context, s store.Store, today string) (ValueOrder, error) {
	sn, err := SnapshotAt(ctx, s, today)
	if err != nil {
		return ValueOrder{}, err
	}
	return OrderOf(sn)
}

// OrderOf is the value order of a snapshot: what needs the owner.
func OrderOf(sn *Snapshot) (ValueOrder, error) { return orderOf(sn, false) }

// openWorkAt ranks every open item the owner works, needed now or not (goals,
// tasks that wait, asks that wait), in value order: for health and align,
// which look at open work, not at what needs the owner.
func openWorkAt(ctx context.Context, s store.Store, today string) (ValueOrder, error) {
	sn, err := SnapshotAt(ctx, s, today)
	if err != nil {
		return ValueOrder{}, err
	}
	return orderOf(sn, true)
}

func orderOf(sn *Snapshot, all bool) (ValueOrder, error) {
	nodes, err := valueNodes(sn, all)
	if err != nil {
		return ValueOrder{}, err
	}
	if !all {
		// A firing idea pool is one row; its ideas stay put off (pool.go).
		nodes = append(nodes, poolNodes(sn)...)
		// A KPI the owner owns that is off target or stale is one row (kpi.go).
		nodes = append(nodes, kpiNodes(sn)...)
	}
	opt, err := rankOptions(sn)
	if err != nil {
		return ValueOrder{}, err
	}
	return RankValue(nodes, opt), nil
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
var OrderOp = defineRead("order", "List what needs the owner (needs_you: open, not put off, not an agent's turn, the owner's turn) in value order: incidents, tasks, questions, asks, meetings, agent work that waits on the owner, and io items (an incoming message that expects a response or that the inbox says needs the owner, an outgoing draft, an approved draft changed since); goals shape the order and are not listed. Each row comes with why it is there (why) and why it needs the owner (reason), its group, bucket, effort, waiting cost, and whether an agent can prepare it (prep). This is the ranking: use it as it is, do not re-rank by reasoning. Actionable now comes first: incidents; items overdue or due within order.due_soon_days (default 2) by date; owed, promises to people within order.owed_soon_days (default 7) and their prep, which falls due by the promise's date minus its effort lead (m 1 day, l 2), soonest first (rows carry for, the promise, and back, that date); reply, where someone waits on the owner and it takes under an hour, longest waiting cost first; focus, the order.focus_count (default 3) best by (impact+1) per effort, doubled when the date, or the date of the goal it feeds, is within order.goal_soon_days (default 14); recheck, overdue by more than order.stale_days (default 30), oldest first, to ask if it is still needed; then the rest by that score, a tie going to owed before goal before upkeep. Not yet startable comes last, by the day it can start: a meeting after today, a call scheduled after today, and anything that depends on one. Deferred items are left out. Within each group, ready rows (ready: an agent prepared it and it has not changed since; ready_kind and ready_ref say what and where) come before raw ones. The summary counts the reply batch, its minutes, and how many have prep", func(ctx context.Context, d Deps, in OrderInput) (OrderOutput, error) {
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
