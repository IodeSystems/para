package ops

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/store"
)

// Value order. One function orders everything that needs the owner, for
// Today, next, agenda, the TUI, and the PWA. The rule, with no weights:
//
//  1. Open incidents, worst severity first.
//  2. Items with an effective due date, soonest first.
//  3. Items with no effective due date.
//
// Effective due is the earliest of the item's own date and the date of any
// open item downstream of it (anything that waits on it, through depends_on,
// and the goal a task belongs to). Ties: higher impact first, then the oldest,
// then the id. Impact is the number of open items it unblocks, through any
// chain; a goal counts twice. A deferred item, and everything reachable only
// through one, adds no impact and gives no effective due.

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

// Groups of ValueRow, in order.
const (
	GroupIncident = "incident"
	GroupDated    = "dated"
	GroupUndated  = "undated"
)

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
	Sev     string   // for an incident: sev1, sev2, sev3
	// Deferred is true when the item is put off today. DeferUntil is its date
	// or "someday".
	Deferred   bool
	DeferUntil string
	// Hidden nodes shape the order but are not listed: work for someone else.
	Hidden bool
	// Proposal marks a question with an answer to check.
	Proposal bool
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
	Why      string `json:"why"`                // in words
	Proposal bool   `json:"proposal,omitempty"` // an answer waits to be checked
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

// RankValue orders the nodes. It is a pure function: the same nodes give the
// same rows.
func RankValue(nodes []ValueNode) ValueOrder {
	by := map[string]*ValueNode{}
	for i := range nodes {
		by[nodes[i].ID] = &nodes[i]
	}
	down := map[string][]string{} // id -> items waiting on it
	for _, n := range nodes {
		for _, dep := range n.Deps {
			if by[dep] != nil {
				down[dep] = append(down[dep], n.ID)
			}
		}
		if n.Goal != "" && by[n.Goal] != nil {
			down[n.ID] = append(down[n.ID], n.Goal)
		}
	}
	// reach walks downstream of id. A deferred item is not visited and its own
	// downstream is not followed.
	reach := func(id string) []*ValueNode {
		seen := map[string]bool{id: true}
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
				if by[u].Deferred {
					continue
				}
				out = append(out, by[u])
				queue = append(queue, u)
			}
		}
		return out
	}
	var rows []ValueRow
	var held []HeldUp
	for _, n := range nodes {
		if n.Hidden {
			continue
		}
		if n.Deferred {
			// Deferred items are not listed. One that holds up earlier work is
			// reported.
			var worst *ValueNode
			for _, u := range reach(n.ID) {
				if u.Due != "" && (n.DeferUntil == model.SomedayWord || u.Due < n.DeferUntil) && (worst == nil || u.Due < worst.Due) {
					worst = u
				}
			}
			if worst != nil {
				held = append(held, HeldUp{ID: n.ID, Title: n.Title, DeferUntil: n.DeferUntil, Holds: worst.ID, HoldsTitle: worst.Title, Due: worst.Due})
			}
			continue
		}
		r := ValueRow{ID: n.ID, Kind: n.Kind, Title: n.Title, Due: n.Due, Proposal: n.Proposal}
		eff, from := n.Due, (*ValueNode)(nil)
		for _, u := range reach(n.ID) {
			r.Impact++
			if u.Kind == KindGoal {
				r.Impact += goalWeight - 1
			} else {
				r.Blocks++
			}
			if u.Due != "" && (eff == "" || u.Due < eff) {
				eff, from = u.Due, u
			}
		}
		r.EffDue = eff
		switch {
		case n.Kind == KindIncident:
			r.Group = GroupIncident
		case eff != "":
			r.Group = GroupDated
		default:
			r.Group = GroupUndated
		}
		var waits []string
		for _, dep := range n.Deps {
			if u := by[dep]; u != nil {
				waits = append(waits, cmp.Or(u.Title, u.ID))
			}
		}
		r.Why = why(n, r, from, waits)
		rows = append(rows, r)
	}
	rank := map[string]int{GroupIncident: 0, GroupDated: 1, GroupUndated: 2}
	created := map[string]string{}
	sev := map[string]string{}
	for _, n := range nodes {
		created[n.ID], sev[n.ID] = n.Created, n.Sev
	}
	slices.SortStableFunc(rows, func(a, b ValueRow) int {
		if c := cmp.Compare(rank[a.Group], rank[b.Group]); c != 0 {
			return c
		}
		if a.Group == GroupIncident {
			return cmp.Or(cmp.Compare(sev[a.ID], sev[b.ID]), cmp.Compare(created[a.ID], created[b.ID]), cmp.Compare(a.ID, b.ID))
		}
		if a.Group == GroupDated {
			if c := cmp.Compare(a.EffDue, b.EffDue); c != 0 {
				return c
			}
		}
		return cmp.Or(cmp.Compare(b.Impact, a.Impact), cmp.Compare(created[a.ID], created[b.ID]), cmp.Compare(a.ID, b.ID))
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

// why says in words why a row is where it is.
func why(n ValueNode, r ValueRow, from *ValueNode, waits []string) string {
	var parts []string
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
		n := ValueNode{ID: doc.ID, Title: doc.Title, Created: string(doc.Meta.Header().Created), Deferred: deferred[doc.ID]}
		if df := deferralOf(doc.Meta); df != nil {
			n.DeferUntil = df.DeferUntil
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
			n.Kind, n.Due = KindGoal, string(m.Due)
		case *model.Task:
			if taskTerminal(m.Status) {
				continue
			}
			n.Kind, n.Due, n.Goal, n.Hidden = KindTask, string(m.Due), m.Project.Target(), IsOther(m.Assignee)
			n.Deps = targets(m.DependsOn)
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
			if m.Status != model.MeetingScheduled {
				continue
			}
			t, err := time.Parse(time.RFC3339, m.Start)
			if err != nil {
				continue
			}
			n.Kind, n.Due = KindMeeting, t.In(time.Local).Format(dateLayout)
			n.Hidden = n.Due > horizon
			for _, l := range m.RFX {
				meetingOf[l.Target()] = append(meetingOf[l.Target()], doc.ID)
			}
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
	return RankValue(nodes), nil
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
var OrderOp = defineRead("order", "List open incidents, goals, tasks, questions, asks, meetings, and agent work in value order, each with why it is there; deferred items are left out", func(ctx context.Context, d Deps, in OrderInput) (OrderOutput, error) {
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
