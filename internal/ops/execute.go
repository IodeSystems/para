package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/iodesystems/parasolve/internal/model"
)

// The owner directs and agents act with their own tools: gh, slackcli, acli, a
// browser. Para stays the record and the gate. An approved message or action
// waits as approved, not done, until the agent reports it with
// proposal_executed. Para sends nothing.

const (
	// ExecDone and ExecFailed are the results an agent reports.
	ExecDone   = "done"
	ExecFailed = "failed"

	opGatePass = "gate_pass"
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
// reports is self-reported unless a harness hook recorded the act: the hook's
// gate check leaves a gate_pass event. Only placeholder values may differ from
// the approved text, and they are kept in the receipt.
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
	var hook bool
	var exec model.Execution
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
		if hook, err = gatePassed(ctx, d, p.doc.ID, it.ID); err != nil {
			return err
		}
		from = it.State
		exec = model.Execution{Result: in.Result, URL: strings.TrimSpace(in.URL), Locator: strings.TrimSpace(in.Locator), By: info.Actor,
			At: d.Store.Now().UTC().Format(time.RFC3339), SelfReported: !hook, Filled: filledValues(p, *it)}
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
		content: model.ReceiptContent{After: firstOf(exec.Locator, exec.URL, it.ID) + filledLine(exec.Filled)}, selfReported: !hook})
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
	out := ProposalExecutedOutput{ID: p.doc.ID, Item: in.Item, State: it.State, Receipt: receipt, SelfReported: !hook}
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

// gatePassed reports whether a hook's gate check recorded this item.
func gatePassed(ctx context.Context, d Deps, doc, item string) (bool, error) {
	evs, err := d.Store.Events(ctx, activityWindow)
	if err != nil {
		return false, err
	}
	for _, e := range evs {
		if e.Op == opGatePass && e.Doc == doc && e.From == item {
			return true, nil
		}
	}
	return false, nil
}

// GateCheckInput is the input of gate_check.
type GateCheckInput struct {
	Kind    string `json:"kind,omitempty"` // the subtype, such as pr or email; empty matches any
	Target  string `json:"target"`
	Subject string `json:"subject,omitempty"`
	Body    string `json:"body"`
}

// GateCheckOutput says whether the exact text is approved. It reports failure
// when it is not, so a harness hook reads the exit code.
type GateCheckOutput struct {
	Approved bool   `json:"approved"`
	Hash     string `json:"hash"`
	Proposal string `json:"proposal,omitempty"`
	Item     string `json:"item,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// Failed implements Failing.
func (o GateCheckOutput) Failed() error {
	if o.Approved {
		return nil
	}
	return fmt.Errorf("not approved: %s", o.Reason)
}

// GateCheck passes only text the owner approved. Placeholders in the approved
// text are filled from earlier results before the compare, and nothing else may
// differ. A pass leaves an event, which marks the act as recorded by a hook.
var GateCheck = defineRead("gate_check", "Check whether the owner approved this exact target and body, for a harness hook", func(ctx context.Context, d Deps, in GateCheckInput) (GateCheckOutput, error) {
	want := model.GateHash(in.Target, in.Subject, in.Body)
	out := GateCheckOutput{Hash: want, Reason: "no approved item has this exact text"}
	all, err := allProposals(ctx, d)
	if err != nil {
		return out, err
	}
	for _, p := range all {
		if p.meta.Status == model.ProposalSuperseded {
			continue
		}
		for i := range p.content.Items {
			it := &p.content.Items[i]
			if !it.Kind.Outward() || it.State != model.ItemApproved {
				continue
			}
			if in.Kind != "" && !strings.EqualFold(in.Kind, it.Subtype) {
				continue
			}
			if now, err := it.Hash(); err != nil || now != it.ApprovedHash {
				continue
			}
			if model.GateHash(fillText(p, it.GateTarget()), fillText(p, it.Subject), fillText(p, it.GateBody())) != want {
				continue
			}
			if !afterDone(p, it) {
				out.Reason = fmt.Sprintf("item %s runs after %s, which is not done", it.ID, it.After)
				continue
			}
			out.Approved, out.Proposal, out.Item, out.Reason = true, p.doc.ID, it.ID, ""
			return out, d.Store.Record(ctx, opGatePass, p.doc.ID, it.ID, "", want)
		}
	}
	return out, nil
})

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

const activityWindow = 5000

// ActivityRow is one thing an agent did: when, under which request, with its
// receipt. Why is the agent's word.
type ActivityRow struct {
	TS           time.Time `json:"ts"`
	Actor        string    `json:"actor"`
	Op           string    `json:"op"`
	Doc          string    `json:"doc"`
	DocName      string    `json:"doc_name,omitempty" nameof:"Doc"`
	Request      string    `json:"request,omitempty"`
	Receipt      string    `json:"receipt"`
	Why          string    `json:"why,omitempty"`
	SelfReported bool      `json:"self_reported,omitempty"`
}

// AgentActivityInput is the input of agent_activity.
type AgentActivityInput struct {
	Actor string `json:"actor,omitempty" query:"actor"`
	Since string `json:"since,omitempty" query:"since"` // RFC 3339, or YYYY-MM-DD
	Limit int    `json:"limit,omitempty" query:"limit"` // 0 means 200
}

// AgentActivityOutput is the output of agent_activity, newest first.
type AgentActivityOutput struct {
	Items []ActivityRow `json:"items"`
}

// AgentActivity lists what each agent did, from events and receipts.
var AgentActivity = defineRead("agent_activity", "Show what each agent did: when, under which request, with its receipt", func(ctx context.Context, d Deps, in AgentActivityInput) (AgentActivityOutput, error) {
	limit := in.Limit
	if limit <= 0 {
		limit = 200
	}
	var since time.Time
	if in.Since != "" {
		t, err := time.Parse(time.RFC3339, in.Since)
		if err != nil {
			if t, err = time.Parse(dateLayout, in.Since); err != nil {
				return AgentActivityOutput{}, errors.New("since: want an RFC 3339 time or YYYY-MM-DD")
			}
		}
		since = t
	}
	rows, err := activityRows(ctx, d, in.Actor, since, time.Time{})
	if err != nil {
		return AgentActivityOutput{}, err
	}
	if len(rows) > limit {
		rows = rows[:limit]
	}
	return AgentActivityOutput{Items: rows}, nil
})

func activityRows(ctx context.Context, d Deps, actor string, since, until time.Time) ([]ActivityRow, error) {
	evs, err := d.Store.Events(ctx, activityWindow)
	if err != nil {
		return nil, err
	}
	out := []ActivityRow{}
	for i := len(evs) - 1; i >= 0; i-- {
		e := evs[i]
		if e.Op != "receipt" || (actor != "" && e.Actor != actor) || e.TS.Before(since) || (!until.IsZero() && !e.TS.Before(until)) {
			continue
		}
		row := ActivityRow{TS: e.TS, Actor: e.Actor, Doc: e.Doc, Request: e.From, Receipt: e.To}
		if _, r, err := getReceipt(ctx, d.Store, e.To); err == nil {
			row.Op, row.Why, row.SelfReported = r.Op, r.Why, r.SelfReported
		}
		out = append(out, row)
	}
	return out, nil
}
