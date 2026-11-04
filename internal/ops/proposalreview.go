package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
)

// Decisions. proposal_approve and proposal_deny are definePerson: a person at a
// terminal. proposal_decide is how an agent records what the owner said in the
// chat: the owner's words are stored on each item, marked self-reported. The
// web app only reads proposals.

// ProposalReviewOutput is the output of a review op. Items also shows what a
// bundle's failure left: each halted item is listed.
type ProposalReviewOutput struct {
	ID     string               `json:"id"`
	Status model.ProposalStatus `json:"status"`
	Items  []Decision           `json:"items"`            // the items this call touched
	Failed []string             `json:"failed,omitempty"` // items whose op failed
	Rev    string               `json:"rev"`
}

// changeProposal loads a revision, applies fn, and saves. It reads again when another
// writer got in first, so a review never overwrites a change. fn runs once per
// try, so it must not write anything else.
func changeProposal(ctx context.Context, d Deps, id string, fn func(p *proposal) error) (*proposal, error) {
	for range claimRetries {
		p, err := loadProposal(ctx, d, id)
		if err != nil {
			return nil, err
		}
		if err := fn(p); err != nil {
			return nil, err
		}
		err = p.save(ctx, d)
		if errors.Is(err, model.ErrConflict) {
			continue
		}
		return p, err
	}
	return nil, fmt.Errorf("could not save %s after %d tries: it keeps changing", id, claimRetries)
}

// record logs one item's state change: the item id goes with its states.
func record(ctx context.Context, d Deps, op, doc, item string, from, to model.ItemState, hash string) error {
	return d.Store.Record(ctx, op, doc, item+":"+string(from), item+":"+string(to), hash)
}

// ProposalApproveInput is the input of proposal_approve. Reason is optional and
// is kept on the alternatives that a pick closes.
type ProposalApproveInput struct {
	ID     string   `json:"id"`
	Items  []string `json:"items,omitempty"`
	Group  string   `json:"group,omitempty"` // approve every waiting item of this group
	Reason string   `json:"reason,omitempty"`
}

// ProposalApprove approves items. An outward item records the hash of its text,
// and nothing runs. A para item runs through ops under the actor that proposed
// it, when its after is done. A failed op marks that item failed with the
// message; other items continue, but the rest of its bundle halts. Approving an
// item of a bundle approves the bundle; approving an item of a choice closes
// the alternatives.
var ProposalApprove = definePerson("proposal_approve", "Approve proposal items, by id or by group. Runs para items under the proposing agent, in after order; records the hash of messages and actions. An item of a bundle approves the bundle; an item of a choice closes the others. People only", func(ctx context.Context, d Deps, in ProposalApproveInput) (ProposalReviewOutput, error) {
	return approveItems(ctx, d, in, newDecision(d, model.DecideApprove, ""))
})

// newDecision is the record of one decision by the caller. ownerSaid is the
// owner's words an agent relayed; it makes the record self-reported.
func newDecision(d Deps, decision, ownerSaid string) model.ItemDecision {
	info := d.Store.Info()
	return model.ItemDecision{Decision: decision, By: info.Actor, Surface: info.Surface, At: d.Store.Now().UTC().Format(time.RFC3339),
		OwnerSaid: ownerSaid, SelfReported: ownerSaid != ""}
}

// approveItems approves items with one decision record.
func approveItems(ctx context.Context, d Deps, in ProposalApproveInput, rec model.ItemDecision) (ProposalReviewOutput, error) {
	if len(in.Items) == 0 && in.Group == "" {
		return ProposalReviewOutput{}, errors.New("no items")
	}
	out := ProposalReviewOutput{ID: normID(in.ID), Items: []Decision{}}
	check, err := loadProposal(ctx, d, in.ID)
	if err != nil {
		return out, err
	}
	if in.Group != "" {
		// A group approves what can be approved: an item waiting on a note or a
		// hold stays for the owner, and says so in the item.
		n := len(in.Items)
		for i := range check.content.Items {
			it := &check.content.Items[i]
			if it.Group == in.Group && slices.Contains(check.actions(it), ActionApprove) {
				in.Items = append(in.Items, it.ID)
			}
		}
		if len(in.Items) == n {
			return out, fmt.Errorf("group %q has no item that can be approved", in.Group)
		}
	}
	order, err := approvalOrder(check, in.Items)
	if err != nil {
		return out, err
	}
	var last *proposal
	halted := map[string]string{} // bundle -> failed item
	for _, id := range order {
		it, err := check.item(id)
		if err != nil {
			return out, err
		}
		if by := halted[it.Bundle]; it.Bundle != "" && by != "" {
			last, err = haltItem(ctx, d, in.ID, id, by)
			if err != nil {
				return out, err
			}
			got, _ := last.item(id)
			out.Items = append(out.Items, decisions([]model.ProposalItem{*got})[0])
			continue
		}
		p, dec, err := approveOne(ctx, d, in.ID, id, strings.TrimSpace(in.Reason), rec)
		if err != nil {
			return out, err
		}
		out.Items = append(out.Items, dec)
		if dec.State == model.ItemFailed {
			out.Failed = append(out.Failed, id)
			if it.Bundle != "" {
				halted[it.Bundle] = id
			}
		}
		last = p
	}
	out.Status, out.Rev = last.meta.Status, last.doc.Rev
	return out, nil
}

// approvalOrder grows ids to whole bundles, checks that the rule allows each,
// and sorts them so an item comes after the item it names. A bundle is approved
// as one, so one item that cannot be refuses all of them.
func approvalOrder(p *proposal, ids []string) ([]string, error) {
	var want []string
	add := func(id string) {
		if !slices.Contains(want, id) {
			want = append(want, id)
		}
	}
	for _, id := range ids {
		it, err := p.item(id)
		if err != nil {
			return nil, err
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
	for _, id := range want {
		it, err := p.item(id)
		if err != nil {
			return nil, err
		}
		if err := p.allow(it, ActionApprove); err != nil {
			return nil, err
		}
	}
	var out []string
	var visit func(id string, depth int)
	visit = func(id string, depth int) {
		if slices.Contains(out, id) || depth > len(want) {
			return
		}
		if it, err := p.item(id); err == nil && slices.Contains(want, it.After) {
			visit(it.After, depth+1)
		}
		out = append(out, id)
	}
	for _, id := range want {
		visit(id, 0)
	}
	return out, nil
}

// afterDone reports whether the item an item runs after has finished.
func afterDone(p *proposal, it *model.ProposalItem) bool {
	if it.After == "" {
		return true
	}
	dep, err := p.item(it.After)
	if err != nil {
		return false
	}
	if dep.Kind.Outward() {
		return dep.State == model.ItemDone
	}
	return dep.State == model.ItemApproved && !dep.Waits
}

// approveOne marks the item, then runs its op. The mark comes first and Rev is
// compared, so of two approvals one wins and an op runs at most once. A crash
// during the op leaves the item running, for a person to look at.
func approveOne(ctx context.Context, d Deps, id, item, reason string, rec model.ItemDecision) (*proposal, Decision, error) {
	var run model.ProposalItem
	var from model.ItemState
	var closed []string
	p, err := changeProposal(ctx, d, id, func(p *proposal) error {
		it, err := p.item(item)
		if err != nil {
			return err
		}
		if err := p.allow(it, ActionApprove); err != nil {
			return err
		}
		hash, err := it.Hash()
		if err != nil {
			return err
		}
		from = it.State
		it.ApprovedHash, it.Reason = hash, ""
		it.Decided = &rec
		run = model.ProposalItem{}
		closed = closeAlternatives(p, it, reason)
		if it.Kind.Outward() {
			it.State = model.ItemApproved
			return nil
		}
		if err := checkOpItem(it.Op, it.Input); err != nil {
			it.State, it.Reason = model.ItemFailed, err.Error()
			return nil
		}
		if d.Authorize != nil {
			if err := d.Authorize(it.Op); err != nil {
				return err
			}
		}
		if !afterDone(p, it) {
			it.State, it.Waits = model.ItemApproved, true
			return nil
		}
		it.State = model.ItemRunning
		run = *it
		return nil
	})
	if err != nil {
		return nil, Decision{}, err
	}
	it, err := p.item(item)
	if err != nil {
		return nil, Decision{}, err
	}
	if err := record(ctx, d, "proposal_approve", p.doc.ID, item, from, it.State, it.ApprovedHash); err != nil {
		return nil, Decision{}, err
	}
	for _, o := range closed {
		if err := record(ctx, d, "proposal_deny", p.doc.ID, o, model.ItemPending, model.ItemDenied, ""); err != nil {
			return nil, Decision{}, err
		}
	}
	if run.Op == "" {
		return p, decisions([]model.ProposalItem{*it})[0], nil
	}
	return finishRun(ctx, d, id, run, p.meta.ProposedBy)
}

// closeAlternatives denies the other items of the picked item's choice, with
// the reason kept, and returns their ids.
func closeAlternatives(p *proposal, it *model.ProposalItem, reason string) []string {
	if it.Choice == "" {
		return nil
	}
	why := "Not chosen: picked " + it.ID
	if reason != "" {
		why += ". " + reason
	}
	var out []string
	for i := range p.content.Items {
		o := &p.content.Items[i]
		if o.ID != it.ID && o.Choice == it.Choice && o.State == model.ItemPending {
			o.State, o.Reason, o.Decided = model.ItemDenied, why, it.Decided
			out = append(out, o.ID)
		}
	}
	return out
}

// finishRun runs a marked para item, records the result, and starts what waited
// for it.
func finishRun(ctx context.Context, d Deps, id string, run model.ProposalItem, by string) (*proposal, Decision, error) {
	item := run.ID
	current, err := loadProposal(ctx, d, id)
	if err != nil {
		return nil, Decision{}, err
	}
	run.Input = fillInput(current, run.Input)
	result, opErr := runItem(ctx, d, run, by)
	p, err := changeProposal(ctx, d, id, func(p *proposal) error {
		it, err := p.item(item)
		if err != nil {
			return err
		}
		it.State, it.Result, it.Waits = model.ItemApproved, result, false
		if opErr != nil {
			it.State, it.Reason = model.ItemFailed, PublicError(opErr, nil)
		}
		return nil
	})
	if err != nil {
		return nil, Decision{}, err
	}
	it, err := p.item(item)
	if err != nil {
		return nil, Decision{}, err
	}
	if err := record(ctx, d, "proposal_run", p.doc.ID, item, model.ItemRunning, it.State, it.ApprovedHash); err != nil {
		return nil, Decision{}, err
	}
	if it.State == model.ItemFailed {
		if p, err = haltAfterFailure(ctx, d, id, *it); err != nil {
			return nil, Decision{}, err
		}
	} else if p, err = runReady(ctx, d, id); err != nil {
		return nil, Decision{}, err
	}
	if it, err = p.item(item); err != nil {
		return nil, Decision{}, err
	}
	return p, decisions([]model.ProposalItem{*it})[0], nil
}

// runReady runs para items that waited for an after that is now done.
func runReady(ctx context.Context, d Deps, id string) (*proposal, error) {
	for {
		var run model.ProposalItem
		p, err := changeProposal(ctx, d, id, func(p *proposal) error {
			run = model.ProposalItem{}
			for i := range p.content.Items {
				if it := &p.content.Items[i]; it.Waits && afterDone(p, it) {
					it.State, it.Waits = model.ItemRunning, false
					run = *it
					return nil
				}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		if run.Op == "" {
			return p, nil
		}
		if p, _, err = finishRun(ctx, d, id, run, p.meta.ProposedBy); err != nil {
			return nil, err
		}
		if run.Bundle != "" {
			if done, err := p.item(run.ID); err == nil && done.State == model.ItemFailed {
				return p, nil
			}
		}
	}
}

// haltItem marks one item of a bundle halted, because by failed.
func haltItem(ctx context.Context, d Deps, id, item, by string) (*proposal, error) {
	var from model.ItemState
	p, err := changeProposal(ctx, d, id, func(p *proposal) error {
		it, err := p.item(item)
		if err != nil {
			return err
		}
		from = it.State
		it.State, it.Reason, it.Waits = model.ItemHalted, "Halted: "+by+" failed. This did not run.", false
		return nil
	})
	if err != nil {
		return nil, err
	}
	return p, record(ctx, d, "proposal_halt", p.doc.ID, item, from, model.ItemHalted, "")
}

// haltAfterFailure stops what has not run in the failed item's bundle.
func haltAfterFailure(ctx context.Context, d Deps, id string, failed model.ProposalItem) (*proposal, error) {
	p, err := loadProposal(ctx, d, id)
	if err != nil || failed.Bundle == "" {
		return p, err
	}
	for _, it := range p.content.Items {
		if it.Bundle != failed.Bundle || it.ID == failed.ID {
			continue
		}
		if it.Waits || (it.Kind.Outward() && it.State == model.ItemApproved) {
			if p, err = haltItem(ctx, d, id, it.ID, failed.ID); err != nil {
				return nil, err
			}
		}
	}
	return p, nil
}

// itemValue is an earlier item's result, for a placeholder.
func itemValue(p *proposal, item, field string) (string, bool) {
	it, err := p.item(item)
	if err != nil {
		return "", false
	}
	if field == "result" && it.Result != "" {
		return it.Result, true
	}
	if it.Execution == nil {
		return "", false
	}
	switch field {
	case "url":
		return it.Execution.URL, it.Execution.URL != ""
	case "locator":
		return it.Execution.Locator, it.Execution.Locator != ""
	case "result":
		return it.Execution.Result, true
	}
	return "", false
}

// fillText fills the placeholders of text from earlier results.
func fillText(p *proposal, text string) string {
	return model.FillPlaceholders(text, func(item, field string) (string, bool) { return itemValue(p, item, field) })
}

// fillInput fills the placeholders of an op's input. Only string values change.
func fillInput(p *proposal, in map[string]any) map[string]any {
	raw := inputJSON(in)
	if len(model.Placeholders(raw)) == 0 {
		return in
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		if s, ok := v.(string); ok {
			out[k] = fillText(p, s)
		} else {
			out[k] = v
		}
	}
	return out
}

// runItem runs an op under the proposing actor, through Invoke as any surface
// does: lint and events apply. It returns the id the op reports, if any. A panic
// in an op fails the item and not the review.
func runItem(ctx context.Context, d Deps, it model.ProposalItem, actor string) (result string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("op %s panicked", it.Op)
		}
	}()
	raw, err := json.Marshal(it.Input)
	if err != nil {
		return "", err
	}
	m := WriteMeta{Why: firstOf(it.How, it.Evidence.Quote), Sources: []string{it.Evidence.Locator}, NotDuplicate: it.NotDuplicate}
	if it.Decided != nil {
		m.OwnerSaid = it.Decided.OwnerSaid
	}
	// The item's op is an outermost write of its own: it files its receipt even
	// when proposal_decide, a recorded flow op, runs it.
	ctx = context.WithValue(WithWrite(ctx, m), recordingKey{}, nil)
	out, err := registry[it.Op].Invoke(ctx, Deps{Store: d.Store.ActAs(actor, surfaceProposal)}, raw)
	if err != nil {
		return "", err
	}
	if f, ok := out.(Failing); ok {
		if err := f.Failed(); err != nil {
			return "", err
		}
	}
	return outputID(out), nil
}

func firstOf(list ...string) string {
	for _, s := range list {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func outputID(out any) string {
	b, err := json.Marshal(out)
	if err != nil {
		return ""
	}
	var head struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(b, &head) != nil {
		return ""
	}
	return head.ID
}

// ProposalReasonInput is the input of proposal_deny.
type ProposalReasonInput struct {
	ID     string `json:"id"`
	Item   string `json:"item"`
	Reason string `json:"reason"`
}

// decide moves one item to state with a reason.
func decide(ctx context.Context, d Deps, op, action, id, item, reason string, state model.ItemState, rec model.ItemDecision) (*proposal, Decision, error) {
	var from model.ItemState
	p, err := changeProposal(ctx, d, id, func(p *proposal) error {
		it, err := p.item(item)
		if err != nil {
			return err
		}
		if err := p.allow(it, action); err != nil {
			return err
		}
		from = it.State
		it.State, it.Reason, it.Decided = state, reason, &rec
		return nil
	})
	if err != nil {
		return nil, Decision{}, err
	}
	if err := record(ctx, d, op, p.doc.ID, item, from, state, ""); err != nil {
		return nil, Decision{}, err
	}
	it, err := p.item(item)
	if err != nil {
		return nil, Decision{}, err
	}
	return p, decisions([]model.ProposalItem{*it})[0], nil
}

func requireReason(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", errors.New("reason is empty")
	}
	return s, nil
}

// ProposalDeny closes out an item, with a reason. The denial is final. An item
// of a bundle takes the bundle with it.
var ProposalDeny = definePerson("proposal_deny", "Deny a proposal item, with a reason. A bundle is denied as one. People only", func(ctx context.Context, d Deps, in ProposalReasonInput) (ProposalReviewOutput, error) {
	reason, err := requireReason(in.Reason)
	if err != nil {
		return ProposalReviewOutput{}, err
	}
	return denyItem(ctx, d, in.ID, in.Item, reason, newDecision(d, model.DecideDeny, ""))
})

// denyItem denies an item, and the rest of its bundle, with one record.
func denyItem(ctx context.Context, d Deps, id, item, reason string, rec model.ItemDecision) (ProposalReviewOutput, error) {
	check, err := loadProposal(ctx, d, id)
	if err != nil {
		return ProposalReviewOutput{}, err
	}
	ids := []string{item}
	if it, err := check.item(item); err == nil && it.Bundle != "" {
		for i := range check.content.Items {
			o := &check.content.Items[i]
			if o.Bundle == it.Bundle && o.ID != it.ID && o.State != model.ItemWithdrawn {
				if err := check.allow(o, ActionDeny); err != nil {
					return ProposalReviewOutput{}, err
				}
				ids = append(ids, o.ID)
			}
		}
	}
	out := ProposalReviewOutput{ID: check.doc.ID, Items: []Decision{}}
	for _, x := range ids {
		p, dec, err := decide(ctx, d, "proposal_deny", ActionDeny, id, x, reason, model.ItemDenied, rec)
		if err != nil {
			return ProposalReviewOutput{}, err
		}
		out.Items = append(out.Items, dec)
		out.Status, out.Rev = p.meta.Status, p.doc.Rev
	}
	return out, nil
}
