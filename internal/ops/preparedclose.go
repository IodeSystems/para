package ops

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/iodesystems/para/internal/model"
)

// Proposed close. An agent that prepares a task looks for evidence that it is
// done or no longer needed. With a source it records item_prepared kind close:
// the outcome, reason, evidence, the fields the outcome needs, and a
// confidence with its basis (model.CloseProposal). Without evidence the
// confidence is held at model.LowConfidence, the rule of cited claims
// (model.Tradeoffs.Check). The task reads as ready (Snapshot.Ready) until it
// changes. The owner approves it in one call, prepared_close_approve: it runs
// close with the proposal, and the receipt of that close says the owner
// approved the agent's proposal; receipt_revert undoes it. Only a person
// approves.

// checkCloseProposal checks a proposed close of doc and returns it as kept:
// outcome done when empty, the confidence held low without evidence (held says
// so). The outcome's own fields are checked as close would check them, so an
// approval does not fail on them later.
func checkCloseProposal(ctx context.Context, d Deps, doc model.Document, in *model.CloseProposal) (*model.CloseProposal, string, error) {
	t, ok := doc.Meta.(*model.Task)
	if !ok || t.EffectiveKind() == model.KindMeeting || t.Kind == model.KindIncident {
		return nil, "", fmt.Errorf("kind close is for a task: %s is a %s", doc.ID, typeWord(doc))
	}
	if in == nil {
		return nil, "", errors.New("kind close needs close: {outcome, reason, evidence: {quote, locator}, confidence, confidence_basis}")
	}
	cp := *in
	cp.Outcome = cmp.Or(strings.TrimSpace(cp.Outcome), string(model.OutcomeDone))
	cp.Reason, cp.ConfidenceBasis = strings.TrimSpace(cp.Reason), strings.TrimSpace(cp.ConfidenceBasis)
	if cp.Outcome == "skipped" {
		return nil, "", errors.New("close outcome skipped skips one occurrence: propose done, dropped, duplicate, superseded, declined, failed, rerouted, or partial")
	}
	if cp.Reason == "" {
		return nil, "", errors.New("close needs reason: why the task is done or no longer needed")
	}
	if cp.Confidence < 0 || cp.Confidence > 100 {
		return nil, "", errors.New("close confidence is a percent from 0 to 100")
	}
	if cp.ConfidenceBasis == "" {
		return nil, "", errors.New("close needs confidence_basis: why you are that sure")
	}
	if cp.Evidence != nil && strings.TrimSpace(cp.Evidence.Locator) == "" && cp.Evidence.Via == nil {
		cp.Evidence = nil
	}
	held := ""
	if cp.Evidence == nil && cp.Confidence > model.LowConfidence {
		held = fmt.Sprintf("no evidence, so the confidence is held at %d (was %d): cite a source the owner can open to propose more", model.LowConfidence, cp.Confidence)
		cp.Confidence = model.LowConfidence
	}
	// close checks the outcome, the evidence, and the field names.
	rt, err := closeRoute(ctx, d, closeInputOf(doc.ID, &cp))
	if err != nil {
		return nil, "", err
	}
	if rt.op == TaskResolve {
		var tr TaskResolveInput
		if err := decodeStrict(rt.input, &tr); err != nil {
			return nil, "", fmt.Errorf("close fields: %w", err)
		}
		if _, err := checkClose(ctx, d, doc.ID, tr.Outcome, tr); err != nil {
			return nil, "", fmt.Errorf("close: %w", err)
		}
		if tr.Outcome == model.OutcomeDuplicate || tr.Outcome == model.OutcomeSuperseded {
			link := normID(tr.Link)
			if link == "" || link == doc.ID {
				return nil, "", fmt.Errorf("close outcome %s needs fields.link: the id of the other item", tr.Outcome)
			}
			if _, err := d.Store.Get(ctx, link); err != nil {
				return nil, "", linkErr("fields.link", link, err)
			}
		}
	}
	return &cp, held, nil
}

// closeInputOf is the close a proposal makes.
func closeInputOf(id string, cp *model.CloseProposal) CloseInput {
	in := CloseInput{ID: id, Reason: cp.Reason, Evidence: cp.Evidence, Fields: cp.Fields}
	if cp.Outcome != string(model.OutcomeDone) {
		in.Outcome = cp.Outcome
	}
	return in
}

// proposedClose is the proposed close of an open task and whether it is
// ready: recorded, and the task not changed since.
func proposedClose(doc model.Document) (*model.Prepared, bool) {
	p := doc.Meta.Header().Prepared
	if p == nil || p.Kind != model.PreparedClose || p.Close == nil {
		return nil, false
	}
	return p, lifeOf(doc.Meta) == LifeOpen && p.Hash == model.ItemHash(doc.Meta, doc.Body)
}

// PreparedCloseInput is the input of prepared_close and prepared_close_approve.
type PreparedCloseInput struct {
	ID string `json:"id" query:"id" required:"true"`
}

// PreparedCloseOutput is the proposed close of a task. Proposed is false when
// none is recorded; Ready is false when the task changed since (stale) or is
// closed. High is true from model.HighCloseConfidence.
type PreparedCloseOutput struct {
	ID       string               `json:"id"`
	Proposed bool                 `json:"proposed"`
	Ready    bool                 `json:"ready"`
	Stale    bool                 `json:"stale,omitempty"`
	By       string               `json:"by,omitempty"`
	At       string               `json:"at,omitempty"`
	Close    *model.CloseProposal `json:"close,omitempty"`
	High     bool                 `json:"high,omitempty"`
	// Check says how the owner can check the evidence: url, vault, file,
	// via, or "" when there is none.
	Check string `json:"check,omitempty"`
}

// PreparedClose shows the proposed close of a task.
var PreparedClose = defineRead("prepared_close", "Show the close an agent proposed for a task (item_prepared kind close): outcome, reason, evidence, fields, confidence and its basis, who and when; ready is false when the task changed since (stale) or is closed", func(ctx context.Context, d Deps, in PreparedCloseInput) (PreparedCloseOutput, error) {
	doc, err := d.Store.Get(ctx, normID(in.ID))
	if err != nil {
		return PreparedCloseOutput{}, linkErr("id", in.ID, err)
	}
	out := PreparedCloseOutput{ID: doc.ID}
	p, ready := proposedClose(doc)
	if p == nil {
		return out, nil
	}
	out.Proposed, out.Ready, out.Stale = true, ready, !ready && lifeOf(doc.Meta) == LifeOpen
	out.By, out.At, out.Close = p.By, p.At, p.Close
	out.High = p.Close.Confidence >= model.HighCloseConfidence
	if p.Close.Evidence != nil {
		out.Check = p.Close.Evidence.Check()
	}
	return out, nil
})

// PreparedCloseApproveOutput is what an approval closed: the task, the
// outcome, who proposed it, and the close's own output (op, receipt).
type PreparedCloseApproveOutput struct {
	ID         string `json:"id"`
	Outcome    string `json:"outcome"`
	ProposedBy string `json:"proposed_by"`
	Op         string `json:"op"`
	// Receipt undoes the close (receipt_revert): the task opens again with
	// the proposal on it.
	Receipt string `json:"receipt,omitempty"`
}

// ErrCloseStale refuses an approval of a proposal the task outgrew.
var ErrCloseStale = errors.New("the task changed after the close was proposed")

// PreparedCloseApprove closes a task as an agent proposed it, as the owner.
var PreparedCloseApprove = definePerson("prepared_close_approve", "Approve the close an agent proposed for a task (prepared_close): closes it with that outcome, reason, evidence, and fields, in one call. The receipt says the owner approved the agent's proposal; receipt_revert undoes it. Refused when the task changed since the proposal. People only: an agent proposes a close with item_prepared kind close and never approves it", func(ctx context.Context, d Deps, in PreparedCloseInput) (PreparedCloseApproveOutput, error) {
	doc, err := d.Store.Get(ctx, normID(in.ID))
	if err != nil {
		return PreparedCloseApproveOutput{}, linkErr("id", in.ID, err)
	}
	p, ready := proposedClose(doc)
	switch {
	case p == nil:
		return PreparedCloseApproveOutput{}, fmt.Errorf("%s has no proposed close: close it yourself, or send it to an agent", doc.ID)
	case lifeOf(doc.Meta) != LifeOpen:
		return PreparedCloseApproveOutput{}, fmt.Errorf("%s is not open: nothing to close", doc.ID)
	case !ready:
		return PreparedCloseApproveOutput{}, fmt.Errorf("%s: %w by %s: check it again, close it yourself, or ask the agent again", doc.ID, ErrCloseStale, p.By)
	}
	cp := p.Close
	m := writeMeta(ctx)
	m.Why = strings.TrimSpace(fmt.Sprintf("owner approved the close %s proposed (%s, %d%%: %s). %s", p.By, cp.Outcome, cp.Confidence, cp.ConfidenceBasis, m.Why))
	if cp.Evidence != nil && strings.TrimSpace(cp.Evidence.Locator) != "" {
		m.Sources = append(m.Sources, cp.Evidence.Locator)
	}
	res, err := Close.Run(WithWrite(ctx, m), d, closeInputOf(doc.ID, cp))
	if err != nil {
		return PreparedCloseApproveOutput{}, err
	}
	return PreparedCloseApproveOutput{ID: doc.ID, Outcome: cp.Outcome, ProposedBy: p.By, Op: res.Op, Receipt: res.Receipt}, nil
})
