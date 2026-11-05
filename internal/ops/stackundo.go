package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/iodesystems/para/internal/model"
)

// How stack_undo reverses a decision, as Decision.Undo and
// QuestionReviewOutput.Undo say.
const (
	// UndoReset puts the item or question back to waiting for a decision. Nothing
	// was executed, so nothing else needs to change.
	UndoReset = "reset"
	// UndoReceipts first undoes the vault writes para ran when the item was
	// approved (the same undo as receipt_revert, on each of its receipts), then
	// puts the item back.
	UndoReceipts = "receipts"
)

const notChosenPrefix = "Not chosen: "

// openReceipts maps item ids of one dossier to the receipts of what para ran for
// them that are not undone yet, newest first.
func openReceipts(ctx context.Context, d Deps, id string) (map[string][]string, error) {
	list, err := listReceipts(ctx, d.Store)
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	prefix := id + "#"
	for _, r := range list {
		if r.Undone != "" {
			continue
		}
		for _, s := range r.Sources {
			if item, ok := strings.CutPrefix(s, prefix); ok {
				out[item] = append(out[item], r.ID)
			}
		}
	}
	return out, nil
}

// undoKind says how an item's decision can be reversed, or "" when it cannot:
// an outward item an agent claimed or the owner did, a failed or running item,
// an answered question, a denial that only follows another item's approval.
func undoKind(it *model.ProposalItem, receipts []string) string {
	switch it.State {
	case model.ItemSkipped, model.ItemSentBack:
		return UndoReset
	case model.ItemDenied:
		if it.Decided != nil && it.Decided.Decision == model.DecideDeny && !strings.HasPrefix(it.Reason, notChosenPrefix) {
			return UndoReset
		}
	case model.ItemApproved:
		if it.Decided == nil || it.Decided.Decision != model.DecideApprove {
			return ""
		}
		switch {
		case it.Kind.Outward():
			if it.Execution == nil && len(receipts) == 0 {
				return UndoReset
			}
		case it.Waits:
			return UndoReset
		case len(receipts) > 0:
			return UndoReceipts
		}
	}
	return ""
}

// markUndo fills Undo and Receipts on the decisions of one dossier. A failure to
// read leaves them empty: the decision stands, with no undo offered.
func markUndo(ctx context.Context, d Deps, prop string, decs []Decision) []Decision {
	p, err := loadProposal(ctx, d, prop)
	if err != nil {
		return decs
	}
	receipts, err := openReceipts(ctx, d, p.doc.ID)
	if err != nil {
		return decs
	}
	for i := range decs {
		it, err := p.item(decs[i].Item)
		if err != nil {
			continue
		}
		decs[i].Undo = undoKind(it, receipts[it.ID])
		if decs[i].Undo == UndoReceipts {
			decs[i].Receipts = receipts[it.ID]
		}
	}
	return decs
}

// StackUndoInput is the input of stack_undo: dossier items, or one open vault
// question. Requests are the agent work an ask queued (the requests of the
// stack_decide output); they are cancelled.
type StackUndoInput struct {
	Items    []StackItemRef `json:"items,omitempty"`
	Question string         `json:"question,omitempty"`
	Requests []string       `json:"requests,omitempty"`
}

// StackUndoOutput is the output of stack_undo.
type StackUndoOutput struct {
	Items    []Decision `json:"items"`              // the items, back to pending
	Question string     `json:"question,omitempty"` // the question put back
	Undone   []string   `json:"undone,omitempty"`   // the receipts undone
}

// StackUndo reverses a decision of the review stack. A skipped, sent-back,
// declined item, or an approved one that nothing ran yet, goes back to pending.
// An approved vault change is first undone from its receipts (refused, with the
// reason, when a document changed since). A bundle or the alternatives a pick
// closed come back with the item. What cannot be reversed is refused.
var StackUndo = definePerson("stack_undo", "Undo the owner's decision on items of the review stack, or on a vault question that was skipped or sent to an agent: the item returns to pending. An approved vault change is undone from its receipts first; refused, with the reason, when a document changed since. An item an agent claimed, or the owner did, and an answered question cannot be undone. People only", func(ctx context.Context, d Deps, in StackUndoInput) (StackUndoOutput, error) {
	out := StackUndoOutput{Items: []Decision{}}
	if in.Question != "" {
		if len(in.Items) > 0 {
			return out, errors.New("give items or question, not both")
		}
		if err := undoQuestion(ctx, d, in.Question); err != nil {
			return out, err
		}
		out.Question = normID(in.Question)
		cancelRequests(ctx, d, in.Requests)
		return out, nil
	}
	if len(in.Items) == 0 {
		return out, errors.New("give items or question")
	}
	var order []string
	byProp := map[string][]string{}
	for _, r := range in.Items {
		prop := normID(r.Proposal)
		if _, ok := byProp[prop]; !ok {
			order = append(order, prop)
		}
		byProp[prop] = append(byProp[prop], r.Item)
	}
	for _, prop := range order {
		decs, undone, err := undoItems(ctx, d, prop, byProp[prop])
		out.Items = append(out.Items, decs...)
		out.Undone = append(out.Undone, undone...)
		if err != nil {
			return out, err
		}
	}
	cancelRequests(ctx, d, in.Requests)
	return out, nil
})

// cancelRequests cancels the agent work of an ask. One that is gone or already
// finished stays as it is.
func cancelRequests(ctx context.Context, d Deps, ids []string) {
	for _, id := range ids {
		if id != "" {
			_, _ = RequestCancel.Run(ctx, d, RequestCancelInput{ID: id})
		}
	}
}

// undoItems reverses the decisions of items of one dossier.
func undoItems(ctx context.Context, d Deps, prop string, items []string) ([]Decision, []string, error) {
	p, err := loadProposal(ctx, d, prop)
	if err != nil {
		return nil, nil, err
	}
	if p.meta.Status == model.ProposalSuperseded {
		return nil, nil, fmt.Errorf("%s is superseded: nothing to undo on it", p.doc.ID)
	}
	receipts, err := openReceipts(ctx, d, p.doc.ID)
	if err != nil {
		return nil, nil, err
	}
	// The set grows to every item of a bundle: it was decided as one.
	var set []string
	add := func(id string) {
		if !slices.Contains(set, id) {
			set = append(set, id)
		}
	}
	for _, id := range items {
		it, err := p.item(id)
		if err != nil {
			return nil, nil, err
		}
		add(id)
		if it.Bundle != "" {
			for i := range p.content.Items {
				if o := &p.content.Items[i]; o.Bundle == it.Bundle && o.State != model.ItemWithdrawn {
					add(o.ID)
				}
			}
		}
	}
	for _, id := range set {
		it, _ := p.item(id)
		if undoKind(it, receipts[id]) == "" {
			return nil, nil, fmt.Errorf("item %s is %s: it cannot be undone", id, it.State)
		}
		for _, o := range p.content.Items {
			if o.After == id && !slices.Contains(set, o.ID) && o.State != model.ItemPending && o.State != model.ItemSkipped && o.State != model.ItemDenied && o.State != model.ItemWithdrawn {
				return nil, nil, fmt.Errorf("item %s is %s and follows item %s: undo it first", o.ID, o.State, id)
			}
		}
	}
	list, err := listReceipts(ctx, d.Store)
	if err != nil {
		return nil, nil, err
	}
	var ids []string // receipts to undo, newest first
	for _, r := range list {
		for _, id := range set {
			if slices.Contains(receipts[id], r.ID) && !slices.Contains(ids, r.ID) {
				ids = append(ids, r.ID)
			}
		}
	}
	var undone []string
	if len(ids) > 0 {
		res, err := undoReceipts(ctx, d, ids)
		for _, u := range res.Undone {
			undone = append(undone, u.Receipt)
		}
		if err != nil {
			return nil, undone, fmt.Errorf("the vault changes cannot be undone, so the item stays decided: %w", err)
		}
	}
	p, err = changeProposal(ctx, d, prop, func(p *proposal) error {
		for _, id := range set {
			it, err := p.item(id)
			if err != nil {
				return err
			}
			if it.Decided != nil && it.Decided.Decision == model.DecideApprove {
				// An approval closed the alternatives of its choice: they come back.
				for i := range p.content.Items {
					o := &p.content.Items[i]
					if o.ID != id && it.Choice != "" && o.Choice == it.Choice && o.State == model.ItemDenied &&
						o.Decided != nil && o.Decided.At == it.Decided.At && strings.HasPrefix(o.Reason, notChosenPrefix) {
						resetItem(o)
					}
				}
			}
			resetItem(it)
		}
		return nil
	})
	if err != nil {
		return nil, undone, err
	}
	var out []Decision
	for _, id := range set {
		it, _ := p.item(id)
		if err := record(ctx, d, "stack_undo", p.doc.ID, id, model.ItemApproved, it.State, ""); err != nil {
			return out, undone, err
		}
		out = append(out, decisions([]model.ProposalItem{*it})[0])
	}
	return out, undone, nil
}

// resetItem puts an item back to waiting for a decision. A draft answer stays;
// a skip or an ask goes, as it was the decision.
func resetItem(it *model.ProposalItem) {
	if it.Answer != nil && (it.Answer.Decision == model.AnswerSkip || it.Answer.Decision == model.AnswerAsk) {
		it.Answer = nil
	}
	it.State, it.Reason, it.Decided, it.ApprovedHash, it.Result, it.Waits, it.Execution =
		model.ItemPending, "", nil, "", "", false, nil
}

// undoQuestion puts an open vault question the owner skipped or sent to an
// agent back to unreviewed.
func undoQuestion(ctx context.Context, d Deps, id string) error {
	doc, q, err := getQuestion(ctx, d.Store, id)
	if err != nil {
		return err
	}
	if q.Status != model.QuestionOpen || q.ReviewState == "" {
		return fmt.Errorf("%s is %s: only a question that was skipped or sent to an agent can be undone", doc.ID, firstOf(q.ReviewState, string(q.Status)))
	}
	q.ReviewState, q.ReviewNote, q.ReviewRefs, q.ReviewRequest = "", "", nil, ""
	_, err = d.Store.Update(ctx, doc)
	return err
}
