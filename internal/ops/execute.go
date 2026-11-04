package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
)

// The owner directs and agents act with their own tools: gh, slackcli, acli, a
// browser. Para stays the record. An approved message or action waits as
// approved, not done, until the agent reports it with proposal_executed or
// action_record. Para sends nothing.

const (
	// ExecDone and ExecFailed are the results an agent reports.
	ExecDone   = "done"
	ExecFailed = "failed"
)

// ProposalExecutedInput is the input of proposal_executed.
type ProposalExecutedInput struct {
	ID      string `json:"id"`
	Item    string `json:"item"`
	Result  string `json:"result"`            // done or failed
	URL     string `json:"url,omitempty"`     // where the result is
	Locator string `json:"locator,omitempty"` // where the result is, when it has no URL
	Reason  string `json:"reason,omitempty"`  // required when failed
}

// ProposalExecutedOutput is the output of proposal_executed.
type ProposalExecutedOutput struct {
	ID           string          `json:"id"`
	Item         string          `json:"item"`
	State        model.ItemState `json:"state"`
	Receipt      string          `json:"receipt"`
	SelfReported bool            `json:"self_reported"`
	Halted       []string        `json:"halted,omitempty"` // items of the bundle that will not run
	Rev          string          `json:"rev"`
}

// ProposalExecuted records that an agent carried out an approved item. What it
// reports is self-reported: para cannot see the act. Only placeholder values
// may differ from the approved text, and they are kept in the receipt, with the
// owner's words from the decision.
var ProposalExecuted = defineSafe("proposal_executed", "Report that you carried out an approved message or action, with the result and its URL or locator. A later item can use them as {{i1.url}}", func(ctx context.Context, d Deps, in ProposalExecutedInput) (ProposalExecutedOutput, error) {
	if in.Result != ExecDone && in.Result != ExecFailed {
		return ProposalExecutedOutput{}, fmt.Errorf("result %q: want done or failed", in.Result)
	}
	reason := strings.TrimSpace(in.Reason)
	if in.Result == ExecFailed && reason == "" {
		return ProposalExecutedOutput{}, errors.New("reason is empty: say why it failed")
	}
	if in.Result == ExecDone && strings.TrimSpace(in.URL) == "" && strings.TrimSpace(in.Locator) == "" {
		return ProposalExecutedOutput{}, errors.New("give url or locator: where the owner can see the result")
	}
	info, err := identity(d)
	if err != nil {
		return ProposalExecutedOutput{}, err
	}
	var from model.ItemState
	var exec model.Execution
	var said string
	p, err := changeProposal(ctx, d, in.ID, func(p *proposal) error {
		it, err := p.item(in.Item)
		if err != nil {
			return err
		}
		if !it.Kind.Outward() {
			return fmt.Errorf("item %s is a vault change: para ran it", it.ID)
		}
		if it.State != model.ItemApproved {
			return fmt.Errorf("item %s is %s, not approved", it.ID, it.State)
		}
		if now, err := it.Hash(); err != nil || now != it.ApprovedHash {
			return fmt.Errorf("item %s changed since it was approved", it.ID)
		}
		if !afterDone(p, it) {
			return fmt.Errorf("item %s runs after %s, which is not done", it.ID, it.After)
		}
		from = it.State
		if it.Decided != nil {
			said = it.Decided.OwnerSaid
		}
		exec = model.Execution{Result: in.Result, URL: strings.TrimSpace(in.URL), Locator: strings.TrimSpace(in.Locator), By: info.Actor,
			At: d.Store.Now().UTC().Format(time.RFC3339), SelfReported: true, Filled: filledValues(p, *it)}
		it.Execution = &exec
		it.State, it.Reason = model.ItemDone, ""
		if in.Result == ExecFailed {
			it.State, it.Reason = model.ItemFailed, reason
		}
		return nil
	})
	if err != nil {
		return ProposalExecutedOutput{}, err
	}
	it, err := p.item(in.Item)
	if err != nil {
		return ProposalExecutedOutput{}, err
	}
	if err := record(ctx, d, "proposal_executed", p.doc.ID, it.ID, from, it.State, it.ApprovedHash); err != nil {
		return ProposalExecutedOutput{}, err
	}
	sources := []string{}
	if exec.URL != "" {
		sources = append(sources, exec.URL)
	}
	if exec.Locator != "" {
		sources = append(sources, exec.Locator)
	}
	receipt, err := createReceipt(ctx, d, receiptInput{op: "proposal_executed", doc: p.doc.ID, title: p.doc.Title + " " + it.ID,
		meta:    WriteMeta{Why: firstOf(reason, it.What, it.How, it.Subject, "Approved item "+it.ID), Sources: sources, Verify: firstOf(exec.URL, exec.Locator), Request: heldRequest(ctx, d.Store, info.Actor)},
		content: model.ReceiptContent{After: firstOf(exec.Locator, exec.URL, it.ID) + filledLine(exec.Filled)}, selfReported: true, ownerSaid: said})
	if err != nil {
		return ProposalExecutedOutput{}, err
	}
	p, err = changeProposal(ctx, d, in.ID, func(p *proposal) error {
		x, err := p.item(in.Item)
		if err != nil {
			return err
		}
		if x.Execution != nil {
			x.Execution.Receipt = receipt
		}
		return nil
	})
	if err != nil {
		return ProposalExecutedOutput{}, err
	}
	out := ProposalExecutedOutput{ID: p.doc.ID, Item: in.Item, State: it.State, Receipt: receipt, SelfReported: true}
	if it.State == model.ItemFailed {
		if p, err = haltAfterFailure(ctx, d, in.ID, *it); err != nil {
			return out, err
		}
		for _, o := range p.content.Items {
			if o.State == model.ItemHalted {
				out.Halted = append(out.Halted, o.ID)
			}
		}
	} else if p, err = runReady(ctx, d, in.ID); err != nil {
		return out, err
	}
	out.Rev = p.doc.Rev
	return out, nil
})

func filledLine(m map[string]any) string {
	if len(m) == 0 {
		return ""
	}
	return " " + inputJSON(m)
}

// filledValues lists the placeholders of an item with the values they took.
func filledValues(p *proposal, it model.ProposalItem) map[string]any {
	out := map[string]any{}
	for _, ph := range itemPlaceholders(it) {
		if v, ok := itemValue(p, ph[0], ph[1]); ok {
			out[ph[0]+"."+ph[1]] = v
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// UndoneItem is an approved outward item that nobody has reported done.
type UndoneItem struct {
	Proposal string `json:"proposal"`
	Title    string `json:"title"`
	Item     string `json:"item"`
	Summary  string `json:"summary"`
	Target   string `json:"target,omitempty"`
}

// ProposalUndoneInput has no fields.
type ProposalUndoneInput struct{}

// ProposalUndoneOutput is the output of proposal_undone.
type ProposalUndoneOutput struct {
	Items []UndoneItem `json:"items"`
}

// ProposalUndone lists what the owner approved and nobody has carried out.
var ProposalUndone = defineRead("proposal_undone", "List messages and actions the owner approved that are not done yet", func(ctx context.Context, d Deps, _ ProposalUndoneInput) (ProposalUndoneOutput, error) {
	all, err := allProposals(ctx, d)
	if err != nil {
		return ProposalUndoneOutput{}, err
	}
	out := ProposalUndoneOutput{Items: []UndoneItem{}}
	sum := newItemSummarizer(ctx, d)
	for _, p := range all {
		if p.meta.Status == model.ProposalSuperseded {
			continue
		}
		for _, it := range p.content.Items {
			if it.Kind.Outward() && it.State == model.ItemApproved {
				out.Items = append(out.Items, UndoneItem{Proposal: p.doc.ID, Title: p.doc.Title, Item: it.ID, Summary: sum.of(it), Target: it.GateTarget()})
			}
		}
	}
	return out, nil
})
