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
//	proposed:  waits for the owner (pending, held, sent_back, skipped)
//	decided:   approved outward item not claimed yet, approved change that
//	           waits its turn, answered question with no for
//	executing: running, claimed
//	done:      done, or a vault change para ran, or a question para recorded
//	failed:    failed, halted
//	closed:    denied (listed, with the owner's reason), withdrawn (listed
//	           only when asked)
//	later:     waits for the owner, put off until a date or someday. It only
//	           surfaces later: the unscoped needs-you view hides it until its
//	           date, but a view scoped by tag, dossier, or group lists it
//	           (and with state later). It returns to proposed on its date.
//
// A skip is not a deferral: a skipped item stays proposed. It is only left out
// of the needs-you view of the batch (one visit) that skipped it.
const (
	StackProposed  = "proposed"
	StackDecided   = "decided"
	StackExecuting = "executing"
	StackDone      = "done"
	StackFailed    = "failed"
	StackClosed    = "closed"
	StackLater     = "later"
)

// StackStates lists every stack state, in order.
var StackStates = []string{StackProposed, StackLater, StackDecided, StackExecuting, StackDone, StackFailed, StackClosed}

// stackState is the stack state of an item on the date today.
func stackState(it model.ProposalItem, today string) string {
	switch it.State {
	case model.ItemPending, model.ItemHeld, model.ItemSentBack, model.ItemSkipped:
		if it.Deferred(today) {
			return StackLater
		}
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
	// DeferUntil is the date a later item returns, or someday.
	DeferUntil string `json:"defer_until,omitempty"`
	// Tags are the item's topics: its own and its dossier's.
	Tags []string `json:"tags,omitempty"`
	// Question is set on a row that is an open vault question, not a dossier
	// item: Proposal is empty and Item is the question id. Refs and Request
	// are what the owner sent it back with; Proposed says an agent proposed an
	// answer.
	Question string   `json:"question,omitempty"`
	Refs     []string `json:"refs,omitempty"`
	Request  string   `json:"request,omitempty"`
	Proposed bool     `json:"proposed,omitempty"`
	// NeedsYou is true when the row waits on the owner: for a vault question,
	// ops.NeedsYou (open, not put off, not an agent's); for a dossier item,
	// proposed and not sent back, and not for a vault question that no
	// longer needs the owner. NeedsYouReason says why, in words, on a vault
	// question.
	NeedsYou       bool   `json:"needs_you"`
	NeedsYouReason string `json:"needs_you_reason,omitempty"`
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
	State    string `json:"state,omitempty" query:"state"` // a stack state; default every state, and of closed the denied items only
	Kind     string `json:"kind,omitempty" query:"kind"`
	Group    string `json:"group,omitempty" query:"group"`
	Proposal string `json:"proposal,omitempty" query:"proposal"`
	Actor    string `json:"actor,omitempty" query:"actor"`
	Tag      string `json:"tag,omitempty" query:"tag"` // a topic: dossier items and vault questions with this tag
	// NeedsYou lists only what waits on the owner: proposed, and not sent
	// back to an agent. The web app's default view. With a tag, proposal, or
	// group filter it also lists later items (put off), which only surface
	// later and may still be relevant to the scope.
	NeedsYou bool `json:"needs_you,omitempty" query:"needs_you"`
	// Batch is accepted and ignored: a skip is not stored, so no visit hides
	// what it skipped here. It goes in a later release.
	Batch  string `json:"batch,omitempty" query:"batch"`
	Cursor string `json:"cursor,omitempty" query:"cursor"` // from next_cursor
	Limit  int    `json:"limit,omitempty" query:"limit"`   // default 200
}

// ProposalStackOutput is the output of proposal_stack. Counts and groups cover
// every filter but state.
type ProposalStackOutput struct {
	Counts []StackCount `json:"counts"`
	Groups []StackGroup `json:"groups"`
	Rows   []StackRow   `json:"rows"`
	Total  int          `json:"total"` // rows that match, over all pages
	// NeedsYou counts the items that wait on the owner (needs_you), over
	// every filter but state: not later.
	NeedsYou int `json:"needs_you"`
	// Requests is agent work that ended stuck, blocked, or
	// needs_clarification and waits for the owner's response, newest first.
	// Listed and counted in NeedsYou only in the unscoped view (no kind,
	// group, proposal, actor, or tag); listed with needs_you on the first page.
	Requests   []SentItem `json:"requests"`
	NextCursor string     `json:"next_cursor,omitempty"`
	Note       string     `json:"note,omitempty"` // an input that was ignored, and why
}

const defaultStackLimit = 200

// ProposalStack lists the review stack. It reads the dossier files; a vault
// with many thousands of items may need an index (not measured yet).
var ProposalStack = defineRead("proposal_stack", "List the review stack: every item of every current dossier, and every open vault question (question set, proposal empty), with its stack state (proposed, later, decided, executing, done, failed, closed; a denied item is listed with the owner's reason, a withdrawn one only with state closed, an item put off only with state later), counts per state, and progress per group. Filter by state, kind, group, proposal, actor, or tag (a topic: tagged dossier items, items of a tagged dossier, and tagged vault questions); needs_you lists only what waits on the owner (proposed, not sent to an agent, and not put off; a skip is navigation in one web visit and is not stored); a view scoped by tag, proposal, or group also lists later items, which may be relevant (bring one back with stack_return); unscoped, requests lists agent work that ended stuck, blocked, or needs_clarification and waits for the owner (counted in needs_you); page with cursor", func(ctx context.Context, d Deps, in ProposalStackInput) (ProposalStackOutput, error) {
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
	tag := normTag(in.Tag)
	scoped := tag != "" || in.Proposal != "" || in.Group != ""
	sn, err := NewSnapshot(ctx, d.Store)
	if err != nil {
		return ProposalStackOutput{}, err
	}
	today := sn.Today
	all, err := allProposals(ctx, d)
	if err != nil {
		return ProposalStackOutput{}, err
	}
	out := ProposalStackOutput{Counts: []StackCount{}, Groups: []StackGroup{}, Rows: []StackRow{}, Requests: []SentItem{}}
	if in.Batch != "" {
		out.Note = batchNote
	}
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
			tags := normTags(model.ItemTags(p.meta.Tags, it))
			if tag != "" && !slices.Contains(tags, tag) {
				continue
			}
			st := stackState(it, today)
			counts[st]++
			needs := needsYou(st, it.State) && forNeedsYou(sn, it)
			if needs {
				out.NeedsYou++
			}
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
			if hide(in, st, it.State, needs, scoped) {
				continue
			}
			row := StackRow{Proposal: p.doc.ID, Title: p.doc.Title, Item: it.ID, Kind: it.Kind, State: it.State, Stack: st, Group: it.Group,
				Bundle: it.Bundle, Actor: p.meta.ProposedBy, Summary: firstOf(it.Title, sum.of(it)), Drafted: !it.Answer.Empty() && st == StackProposed,
				ClaimedBy: it.ClaimedBy, Result: it.Result, Reason: it.Reason, Tags: tags, NeedsYou: needs}
			if st == StackLater {
				row.DeferUntil = it.DeferUntil
			}
			if it.Execution != nil {
				row.URL, row.Receipt = firstOf(it.Execution.URL, it.Execution.Locator), it.Execution.Receipt
			}
			rows = append(rows, row)
		}
		for _, g := range order {
			out.Groups = append(out.Groups, *groups[g])
		}
	}
	if in.Proposal == "" && in.Actor == "" && in.Group == "" && (in.Kind == "" || in.Kind == string(model.ItemQuestion)) {
		for _, r := range vaultQuestionRows(sn) {
			if tag != "" && !slices.Contains(r.Tags, tag) {
				continue
			}
			counts[r.Stack]++
			needs := r.NeedsYou
			if needs {
				out.NeedsYou++
			}
			if !hide(in, r.Stack, r.State, needs, scoped) {
				rows = append(rows, r)
			}
		}
	}
	if in.Proposal == "" && in.Actor == "" && in.Group == "" && in.Kind == "" && tag == "" {
		waits := workWaitingOnOwner(ctx, d, sn)
		out.NeedsYou += len(waits)
		if in.NeedsYou && start == 0 {
			out.Requests = waits
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

// workWaitingOnOwner lists agent work that ended and waits for the owner,
// newest first: a turn that ended stuck, blocked, or needs_clarification
// waits for a response, and a result not yet seen waits to be checked
// (NeedsYou). Agent work proposed, also the owner's, is accepted on the
// requests page and listed in Today's work.
func workWaitingOnOwner(ctx context.Context, d Deps, sn *Snapshot) []SentItem {
	docs := sn.Docs()
	out := []SentItem{}
	for i := len(docs) - 1; i >= 0; i-- {
		r, ok := docs[i].Meta.(*model.Request)
		if ok && (r.Status == model.RequestDone || r.Status == model.RequestFailed) && sn.Need(docs[i].ID).Yes {
			out = append(out, askedItem(ctx, d, docs[i], r, sn.Now))
		}
	}
	return out
}

// forNeedsYou is false for a dossier question for a vault question that no
// longer needs the owner: answered, put off, or an agent has it.
func forNeedsYou(sn *Snapshot, it model.ProposalItem) bool {
	if it.Kind != model.ItemQuestion || it.For == "" {
		return true
	}
	if _, err := sn.Get(it.For); err != nil {
		return true
	}
	return sn.Need(it.For).Yes
}

// needsYou is true for an item that waits on the owner: proposed, and not
// sent back to an agent.
func needsYou(stack string, state model.ItemState) bool {
	return stack == StackProposed && state != model.ItemSentBack
}

// hide says whether the stack leaves a row out, by the state filter, the
// needs-you view, and the scope. With no state, a closed item that was not
// denied is out; a later item is out unless the view is scoped. In the
// needs-you view, what does not need the owner is out, except a later item
// of a scoped view.
func hide(in ProposalStackInput, st string, state model.ItemState, needs, scoped bool) bool {
	if in.State != "" && st != in.State {
		return true
	}
	if in.State == "" {
		if st == StackClosed && state != model.ItemDenied || st == StackLater && !scoped {
			return true
		}
	}
	return in.NeedsYou && !needs && (!scoped || st != StackLater)
}
