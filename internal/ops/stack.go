package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"github.com/iodesystems/para/internal/model"
)

// The review stack is every item of every current dossier, by stack state:
//
//	proposed:  waits for the owner (pending, held, sent_back)
//	decided:   approved outward item not claimed yet, approved change that
//	           waits its turn, answered question with no for
//	executing: running, claimed
//	done:      done, or a vault change para ran, or a question para recorded
//	failed:    failed, halted
//	closed:    denied, withdrawn (listed only when asked)
const (
	StackProposed  = "proposed"
	StackDecided   = "decided"
	StackExecuting = "executing"
	StackDone      = "done"
	StackFailed    = "failed"
	StackClosed    = "closed"
)

// StackStates lists every stack state, in order.
var StackStates = []string{StackProposed, StackDecided, StackExecuting, StackDone, StackFailed, StackClosed}

// stackState is the stack state of an item.
func stackState(it model.ProposalItem) string {
	switch it.State {
	case model.ItemPending, model.ItemHeld, model.ItemSentBack:
		return StackProposed
	case model.ItemApproved:
		if it.Kind.Outward() || it.Waits {
			return StackDecided
		}
		return StackDone
	case model.ItemAnswered:
		if it.For == "" {
			return StackDecided
		}
		return StackDone
	case model.ItemRunning, model.ItemClaimed:
		return StackExecuting
	case model.ItemDone:
		return StackDone
	case model.ItemFailed, model.ItemHalted:
		return StackFailed
	}
	return StackClosed
}

// StackRow is one item in the stack.
type StackRow struct {
	Proposal  string          `json:"proposal"`
	Title     string          `json:"title"` // the dossier's title
	Item      string          `json:"item"`
	Kind      model.ItemKind  `json:"kind"`
	State     model.ItemState `json:"state"`
	Stack     string          `json:"stack"`
	Group     string          `json:"group,omitempty"`
	Bundle    string          `json:"bundle,omitempty"`
	Actor     string          `json:"actor"` // who proposed it
	Summary   string          `json:"summary"`
	Drafted   bool            `json:"drafted,omitempty"` // the owner wrote an answer, not submitted yet
	ClaimedBy string          `json:"claimed_by,omitempty"`
	URL       string          `json:"url,omitempty"`     // where an outward result is
	Receipt   string          `json:"receipt,omitempty"` // the receipt of an outward result
	Result    string          `json:"result,omitempty"`  // what a vault change made
	Reason    string          `json:"reason,omitempty"`
}

// StackGroup is the progress of one group of one dossier.
type StackGroup struct {
	Proposal string `json:"proposal"`
	Group    string `json:"group"`
	Done     int    `json:"done"`
	Total    int    `json:"total"`
	Proposed int    `json:"proposed"` // still waiting for the owner
}

// StackCount is how many items are in one stack state.
type StackCount struct {
	State string `json:"state"`
	Count int    `json:"count"`
}

// ProposalStackInput is the input of proposal_stack. Every filter is optional.
type ProposalStackInput struct {
	State    string `json:"state,omitempty" query:"state"` // a stack state; default every state but closed
	Kind     string `json:"kind,omitempty" query:"kind"`
	Group    string `json:"group,omitempty" query:"group"`
	Proposal string `json:"proposal,omitempty" query:"proposal"`
	Actor    string `json:"actor,omitempty" query:"actor"`
	Cursor   string `json:"cursor,omitempty" query:"cursor"` // from next_cursor
	Limit    int    `json:"limit,omitempty" query:"limit"`   // default 200
}

// ProposalStackOutput is the output of proposal_stack. Counts and groups cover
// every filter but state.
type ProposalStackOutput struct {
	Counts     []StackCount `json:"counts"`
	Groups     []StackGroup `json:"groups"`
	Rows       []StackRow   `json:"rows"`
	Total      int          `json:"total"` // rows that match, over all pages
	NextCursor string       `json:"next_cursor,omitempty"`
}

const defaultStackLimit = 200

// ProposalStack lists the review stack. It reads the dossier files; a vault
// with many thousands of items may need an index (not measured yet).
var ProposalStack = defineRead("proposal_stack", "List the review stack: every item of every current dossier with its stack state (proposed, decided, executing, done, failed, closed), counts per state, and progress per group. Filter by state, kind, group, proposal, or actor; page with cursor", func(ctx context.Context, d Deps, in ProposalStackInput) (ProposalStackOutput, error) {
	if in.State != "" && !slices.Contains(StackStates, in.State) {
		return ProposalStackOutput{}, fmt.Errorf("state %q: want one of %v", in.State, StackStates)
	}
	if in.Limit < 0 {
		return ProposalStackOutput{}, errors.New("limit is negative")
	}
	limit := in.Limit
	if limit == 0 {
		limit = defaultStackLimit
	}
	start := 0
	if in.Cursor != "" {
		n, err := strconv.Atoi(in.Cursor)
		if err != nil || n < 0 {
			return ProposalStackOutput{}, fmt.Errorf("cursor %q: give next_cursor of the page before", in.Cursor)
		}
		start = n
	}
	all, err := allProposals(ctx, d)
	if err != nil {
		return ProposalStackOutput{}, err
	}
	out := ProposalStackOutput{Counts: []StackCount{}, Groups: []StackGroup{}, Rows: []StackRow{}}
	counts := map[string]int{}
	sum := newItemSummarizer(ctx, d)
	var rows []StackRow
	for _, p := range all {
		if p.meta.Status == model.ProposalSuperseded || p.broken != nil {
			continue
		}
		if in.Proposal != "" && p.doc.ID != normID(in.Proposal) || in.Actor != "" && p.meta.ProposedBy != in.Actor {
			continue
		}
		groups := map[string]*StackGroup{}
		var order []string
		for _, it := range p.content.Items {
			if in.Kind != "" && string(it.Kind) != in.Kind || in.Group != "" && it.Group != in.Group {
				continue
			}
			st := stackState(it)
			counts[st]++
			if it.Group != "" && st != StackClosed {
				g, ok := groups[it.Group]
				if !ok {
					g = &StackGroup{Proposal: p.doc.ID, Group: it.Group}
					groups[it.Group] = g
					order = append(order, it.Group)
				}
				g.Total++
				if st == StackDone {
					g.Done++
				}
				if st == StackProposed {
					g.Proposed++
				}
			}
			if in.State == "" && st == StackClosed || in.State != "" && st != in.State {
				continue
			}
			row := StackRow{Proposal: p.doc.ID, Title: p.doc.Title, Item: it.ID, Kind: it.Kind, State: it.State, Stack: st, Group: it.Group,
				Bundle: it.Bundle, Actor: p.meta.ProposedBy, Summary: firstOf(it.Title, sum.of(it)), Drafted: !it.Answer.Empty() && st == StackProposed,
				ClaimedBy: it.ClaimedBy, Result: it.Result, Reason: it.Reason}
			if it.Execution != nil {
				row.URL, row.Receipt = firstOf(it.Execution.URL, it.Execution.Locator), it.Execution.Receipt
			}
			rows = append(rows, row)
		}
		for _, g := range order {
			out.Groups = append(out.Groups, *groups[g])
		}
	}
	for _, s := range StackStates {
		out.Counts = append(out.Counts, StackCount{State: s, Count: counts[s]})
	}
	out.Total = len(rows)
	if start < len(rows) {
		end := min(start+limit, len(rows))
		out.Rows = append(out.Rows, rows[start:end]...)
		if end < len(rows) {
			out.NextCursor = strconv.Itoa(end)
		}
	}
	return out, nil
})
