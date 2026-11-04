package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
)

// The owner answers a dossier in its blocks. proposal_draft saves answers and
// comments as a draft; nothing is decided. proposal_submit turns the answers
// into decisions: approved vault changes run at once under the proposing
// agent, approved outward items wait for an agent to claim them, and answered
// questions are recorded. Both are person ops (web app or terminal): an agent,
// a sync tool, or a hand edit of the file can write an answer, but only a
// person's submit decides it. stack_decide approves or rejects items across
// dossiers from the review stack.

// DraftAnswer is one item's answer in a draft. A nil or empty answer clears it.
type DraftAnswer struct {
	Item   string            `json:"item"`
	Answer *model.ItemAnswer `json:"answer,omitempty"`
}

// SectionComment is the owner's comment on one section; empty clears it.
type SectionComment struct {
	Section string `json:"section"`
	Comment string `json:"comment"`
}

// ProposalDraftInput is the input of proposal_draft. Revision is the revision
// the owner read; a draft of an older revision is refused.
type ProposalDraftInput struct {
	ID       string           `json:"id"`
	Revision int              `json:"revision"`
	Items    []DraftAnswer    `json:"items,omitempty"`
	Sections []SectionComment `json:"sections,omitempty"`
	Overall  *string          `json:"overall,omitempty"` // nil leaves it; empty clears it
}

// ProposalDraftOutput is the output of proposal_draft.
type ProposalDraftOutput struct {
	ID       string `json:"id"`
	Revision int    `json:"revision"`
	Drafted  int    `json:"drafted"` // items with an answer
	Total    int    `json:"total"`   // items that wait for the owner
	Rev      string `json:"rev"`
}

// answerable reports whether the owner may still answer an item.
func answerable(s model.ItemState) bool {
	return s == model.ItemPending || s == model.ItemHeld || s == model.ItemSentBack
}

// checkRevision refuses an answer to a revision that is not the current one.
// Zero means the revision in the file now: a person at a terminal who typed
// answers into the file has read that one.
func checkRevision(p *proposal, revision int) error {
	if p.dossier == nil {
		return fmt.Errorf("%s is not a dossier", p.doc.ID)
	}
	if p.meta.Status == model.ProposalSuperseded {
		return fmt.Errorf("%s is superseded", p.doc.ID)
	}
	if revision != 0 && revision != p.meta.Revision {
		return fmt.Errorf("%w: %s is at revision %d, and you answered revision %d: reload it", model.ErrConflict, p.doc.ID, p.meta.Revision, revision)
	}
	return nil
}

// ProposalDraft saves the owner's answers as a draft.
var ProposalDraft = definePerson("proposal_draft", "Save the owner's answers to a dossier as a draft: answers per item, comments per section and overall. Nothing is decided until proposal_submit. People only", func(ctx context.Context, d Deps, in ProposalDraftInput) (ProposalDraftOutput, error) {
	via := d.Store.Info().Surface
	var ids []string
	p, err := changeProposal(ctx, d, in.ID, func(p *proposal) error {
		ids = nil
		if err := checkRevision(p, in.Revision); err != nil {
			return err
		}
		for _, x := range in.Items {
			it, err := p.item(x.Item)
			if err != nil {
				return err
			}
			if !answerable(it.State) {
				return fmt.Errorf("item %s is %s: it is decided", it.ID, it.State)
			}
			if x.Answer.Empty() {
				it.Answer = nil
				ids = append(ids, it.ID)
				continue
			}
			a := *x.Answer
			a.Via, a.Stale = via, false
			a.OwnWords, a.Comment = strings.TrimSpace(a.OwnWords), strings.TrimSpace(a.Comment)
			if bad := model.CheckAnswer(*it, a); len(bad) > 0 {
				return fmt.Errorf("item %s: %s", it.ID, strings.Join(bad, "; "))
			}
			it.Answer = &a
			ids = append(ids, it.ID)
		}
		r := p.response()
		for _, c := range in.Sections {
			if !slices.ContainsFunc(p.dossier.Sections, func(s model.DossierSection) bool { return s.ID == c.Section }) {
				return fmt.Errorf("%s has no section %q", p.doc.ID, c.Section)
			}
			if r.Sections == nil {
				r.Sections = map[string]string{}
			}
			if t := strings.TrimSpace(c.Comment); t != "" {
				r.Sections[c.Section] = t
			} else {
				delete(r.Sections, c.Section)
			}
		}
		if len(r.Sections) == 0 {
			r.Sections = nil
		}
		if in.Overall != nil {
			r.Overall = strings.TrimSpace(*in.Overall)
		}
		r.State, r.Via = model.ResponseDraft, via
		return nil
	})
	if err != nil {
		return ProposalDraftOutput{}, err
	}
	for _, item := range ids {
		if err := d.Store.Record(ctx, "proposal_draft", p.doc.ID, "", item, ""); err != nil {
			return ProposalDraftOutput{}, err
		}
	}
	out := ProposalDraftOutput{ID: p.doc.ID, Revision: p.meta.Revision, Rev: p.doc.Rev}
	for _, it := range p.content.Items {
		if answerable(it.State) {
			out.Total++
			if !it.Answer.Empty() {
				out.Drafted++
			}
		}
	}
	return out, nil
})

// ProposalSubmitInput is the input of proposal_submit.
type ProposalSubmitInput struct {
	ID       string `json:"id"`
	Revision int    `json:"revision,omitempty"` // the revision answered; 0 is the one in the file now
}

// ProposalSubmitOutput is the output of proposal_submit.
type ProposalSubmitOutput struct {
	ID     string               `json:"id"`
	Status model.ProposalStatus `json:"status"`
	Items  []Decision           `json:"items"`
	Failed []string             `json:"failed,omitempty"` // approved changes whose op failed
	Rev    string               `json:"rev"`
}

// submitPlan is what a submit decides, checked before anything changes.
type submitPlan struct {
	edits, answers, approve, reject []string
}

func planSubmit(p *proposal) (submitPlan, error) {
	var plan submitPlan
	var stale []string
	bundles := map[string]string{}
	choices := map[string]string{}
	for i := range p.content.Items {
		it := &p.content.Items[i]
		a := it.Answer
		if !answerable(it.State) || a.Empty() {
			continue
		}
		if a.Stale {
			stale = append(stale, it.ID)
			continue
		}
		if bad := model.CheckAnswer(*it, *a); len(bad) > 0 {
			return plan, fmt.Errorf("item %s: %s", it.ID, strings.Join(bad, "; "))
		}
		if it.Kind == model.ItemQuestion {
			if len(a.Choices) > 0 || a.OwnWords != "" {
				plan.answers = append(plan.answers, it.ID)
			}
			continue
		}
		var verdict string
		switch a.Decision {
		case model.AnswerApprove, model.AnswerEdit:
			verdict = model.AnswerApprove
		case model.AnswerReject:
			if strings.TrimSpace(a.Comment) == "" {
				return plan, fmt.Errorf("item %s: reject needs a comment: why, so the item stays in the stack with the reason", it.ID)
			}
			verdict = model.AnswerReject
		default:
			continue // a comment only: the agent reads it
		}
		if it.Bundle != "" {
			if prev, ok := bundles[it.Bundle]; ok && prev != verdict {
				return plan, fmt.Errorf("bundle %s is decided as one: its items cannot be both approved and rejected", it.Bundle)
			}
			bundles[it.Bundle] = verdict
		}
		if verdict == model.AnswerApprove && it.Choice != "" {
			if other, ok := choices[it.Choice]; ok {
				return plan, fmt.Errorf("items %s and %s are alternatives (choice %s): approve one", other, it.ID, it.Choice)
			}
			choices[it.Choice] = it.ID
		}
		if a.Decision == model.AnswerEdit {
			plan.edits = append(plan.edits, it.ID)
		}
		if verdict == model.AnswerApprove {
			plan.approve = append(plan.approve, it.ID)
		} else {
			plan.reject = append(plan.reject, it.ID)
		}
	}
	if len(stale) > 0 {
		return plan, fmt.Errorf("the agent changed %s after you answered: read them again and answer anew", strings.Join(stale, ", "))
	}
	return plan, nil
}

// applyEdit puts the owner's edited text in the item.
func applyEdit(it *model.ProposalItem, a model.ItemAnswer) error {
	switch it.Kind {
	case model.ItemPara:
		if err := checkOpItem(it.Op, a.Input); err != nil {
			return err
		}
		it.Input = a.Input
	case model.ItemDocEdit:
		it.New = a.New
	default:
		if a.Body != "" {
			it.Body = a.Body
		}
		if a.Subject != "" {
			it.Subject = a.Subject
		}
	}
	it.Edited = true
	return nil
}

// ProposalSubmit turns the owner's answers into decisions.
var ProposalSubmit = definePerson("proposal_submit", "Submit the owner's answers to a dossier: approved and edited vault changes run at once under the proposing agent, approved messages and actions wait for an agent to claim them, rejected items close, and answered questions are recorded (with for, as the answer of that question). Refused when revision is not the current one or an answer is stale. People only", func(ctx context.Context, d Deps, in ProposalSubmitInput) (ProposalSubmitOutput, error) {
	p, err := loadProposal(ctx, d, in.ID)
	if err != nil {
		return ProposalSubmitOutput{}, err
	}
	if err := checkRevision(p, in.Revision); err != nil {
		return ProposalSubmitOutput{}, err
	}
	plan, err := planSubmit(p)
	if err != nil {
		return ProposalSubmitOutput{}, err
	}
	out := ProposalSubmitOutput{ID: p.doc.ID, Items: []Decision{}, Status: p.meta.Status, Rev: p.doc.Rev}
	rec := newDecision(d, model.DecideApprove, "")
	// Edits and answers first, in one write, with the revision checked again.
	var answered []model.ProposalItem
	p, err = changeProposal(ctx, d, in.ID, func(p *proposal) error {
		answered = nil
		if err := checkRevision(p, in.Revision); err != nil {
			return err
		}
		for _, id := range plan.edits {
			it, err := p.item(id)
			if err != nil {
				return err
			}
			if err := applyEdit(it, *it.Answer); err != nil {
				return fmt.Errorf("item %s: %w", id, err)
			}
		}
		for _, id := range plan.answers {
			it, err := p.item(id)
			if err != nil {
				return err
			}
			r := rec
			r.Decision = "answer"
			it.State, it.Decided, it.Reason = model.ItemAnswered, &r, ""
			answered = append(answered, *it)
		}
		return nil
	})
	if err != nil {
		return out, err
	}
	for _, id := range plan.edits {
		it, _ := p.item(id)
		hash, _ := it.Hash()
		if err := record(ctx, d, "proposal_edit", p.doc.ID, id, it.State, it.State, hash); err != nil {
			return out, err
		}
	}
	for _, it := range answered {
		hash, _ := it.Hash()
		if err := record(ctx, d, "proposal_answer", p.doc.ID, it.ID, model.ItemPending, model.ItemAnswered, hash); err != nil {
			return out, err
		}
		dec := decisions([]model.ProposalItem{it})[0]
		if it.For != "" {
			if p, err = answerFor(ctx, d, p.doc.ID, it); err != nil {
				return out, err
			}
			got, _ := p.item(it.ID)
			dec = decisions([]model.ProposalItem{*got})[0]
		}
		out.Items = append(out.Items, dec)
	}
	if len(plan.approve) > 0 {
		r, err := approveItems(ctx, d, ProposalApproveInput{ID: p.doc.ID, Items: plan.approve}, rec)
		if err != nil {
			return out, err
		}
		out.Items, out.Failed = append(out.Items, r.Items...), r.Failed
	}
	deny := newDecision(d, model.DecideDeny, "")
	for _, id := range plan.reject {
		cur, err := loadProposal(ctx, d, p.doc.ID)
		if err != nil {
			return out, err
		}
		it, err := cur.item(id)
		if err != nil {
			return out, err
		}
		if !answerable(it.State) {
			continue // a bundle or a choice closed it already
		}
		reason := strings.TrimSpace(it.Answer.Comment)
		r, err := denyItem(ctx, d, p.doc.ID, id, reason, deny)
		if err != nil {
			return out, err
		}
		out.Items = append(out.Items, r.Items...)
	}
	via := d.Store.Info().Surface
	p, err = changeProposal(ctx, d, in.ID, func(p *proposal) error {
		r := p.response()
		r.State, r.Via, r.SubmittedAt = model.ResponseSubmitted, via, d.Store.Now().UTC().Format(time.RFC3339)
		return nil
	})
	if err != nil {
		return out, err
	}
	if err := d.Store.Record(ctx, "proposal_submit", p.doc.ID, "", fmt.Sprint(p.meta.Revision), ""); err != nil {
		return out, err
	}
	out.Status, out.Rev = p.meta.Status, p.doc.Rev
	return out, nil
})

// answerFor records an answered question as the answer of the vault question
// it is for, as the owner. A failure is kept on the item, not returned.
func answerFor(ctx context.Context, d Deps, id string, it model.ProposalItem) (*proposal, error) {
	a := it.Answer
	var parts []string
	if len(a.Choices) > 0 {
		parts = append(parts, strings.Join(a.Choices, ", "))
	}
	if a.OwnWords != "" {
		parts = append(parts, a.OwnWords)
	}
	text := strings.Join(parts, ". ")
	in := QuestionAnswerInput{ID: it.For, Text: text, Why: firstOf(a.Comment, "Answered in dossier "+id+" item "+it.ID)}
	if len(a.Choices) == 1 && a.OwnWords == "" {
		in.Choice, in.Text = a.Choices[0], ""
	}
	_, opErr := QuestionAnswer.Run(ctx, d, in)
	if opErr != nil && in.Choice != "" {
		in.Choice, in.Text = "", text
		_, opErr = QuestionAnswer.Run(ctx, d, in)
	}
	return changeProposal(ctx, d, id, func(p *proposal) error {
		x, err := p.item(it.ID)
		if err != nil {
			return err
		}
		if opErr != nil {
			x.State, x.Reason = model.ItemFailed, PublicError(opErr, nil)
			return nil
		}
		x.Result = it.For
		return nil
	})
}

// StackItemRef names one item of one dossier.
type StackItemRef struct {
	Proposal string `json:"proposal"`
	Item     string `json:"item"`
}

// StackDecideInput is the input of stack_decide. Give items, or group (with
// proposal to keep to one dossier).
type StackDecideInput struct {
	Items    []StackItemRef `json:"items,omitempty"`
	Group    string         `json:"group,omitempty"`
	Proposal string         `json:"proposal,omitempty"`
	Decision string         `json:"decision"` // approve or reject
	Comment  string         `json:"comment,omitempty"`
}

// StackDecideOutput is the output of stack_decide.
type StackDecideOutput struct {
	Proposals []ProposalReviewOutput `json:"proposals"`
}

// StackDecide approves or rejects items across dossiers.
var StackDecide = definePerson("stack_decide", "Approve or reject items of the review stack across dossiers, by item or by group. reject needs comment, the reason; rejected items stay in the stack as denied with it. A bundle is decided as one; approving an item of a choice closes the others; questions are answered in their dossier. People only", func(ctx context.Context, d Deps, in StackDecideInput) (StackDecideOutput, error) {
	if in.Decision != model.AnswerApprove && in.Decision != model.AnswerReject {
		return StackDecideOutput{}, fmt.Errorf("decision %q: want approve or reject", in.Decision)
	}
	if (len(in.Items) == 0) == (in.Group == "") {
		return StackDecideOutput{}, errors.New("give items or group")
	}
	if in.Decision == model.AnswerReject && strings.TrimSpace(in.Comment) == "" {
		return StackDecideOutput{}, errors.New("reject needs a comment: why; the items stay in the stack as denied with it")
	}
	byProposal := map[string][]string{}
	var order []string
	add := func(prop, item string) {
		prop = normID(prop)
		if _, ok := byProposal[prop]; !ok {
			order = append(order, prop)
		}
		if !slices.Contains(byProposal[prop], item) {
			byProposal[prop] = append(byProposal[prop], item)
		}
	}
	if in.Group != "" {
		all, err := allProposals(ctx, d)
		if err != nil {
			return StackDecideOutput{}, err
		}
		for _, p := range all {
			if p.meta.Status == model.ProposalSuperseded || p.broken != nil || in.Proposal != "" && p.doc.ID != normID(in.Proposal) {
				continue
			}
			for _, it := range p.content.Items {
				if it.Group == in.Group && it.Kind != model.ItemQuestion && answerable(it.State) {
					add(p.doc.ID, it.ID)
				}
			}
		}
		if len(order) == 0 {
			return StackDecideOutput{}, fmt.Errorf("group %q has no item that waits for a decision", in.Group)
		}
	}
	for _, r := range in.Items {
		add(r.Proposal, r.Item)
	}
	// Check every dossier before any change.
	for _, prop := range order {
		p, err := loadProposal(ctx, d, prop)
		if err != nil {
			return StackDecideOutput{}, err
		}
		choices := map[string]string{}
		for _, id := range byProposal[prop] {
			it, err := p.item(id)
			if err != nil {
				return StackDecideOutput{}, err
			}
			if it.Kind == model.ItemQuestion {
				return StackDecideOutput{}, fmt.Errorf("%s item %s is a question: answer it in the dossier", prop, id)
			}
			if !answerable(it.State) {
				return StackDecideOutput{}, fmt.Errorf("%s item %s is %s: it is decided", prop, id, it.State)
			}
			if in.Decision == model.AnswerApprove && it.Choice != "" {
				if other, ok := choices[it.Choice]; ok {
					return StackDecideOutput{}, fmt.Errorf("%s items %s and %s are alternatives (choice %s): approve one", prop, other, id, it.Choice)
				}
				choices[it.Choice] = id
			}
		}
	}
	out := StackDecideOutput{Proposals: []ProposalReviewOutput{}}
	comment := strings.TrimSpace(in.Comment)
	for _, prop := range order {
		if in.Decision == model.AnswerApprove {
			r, err := approveItems(ctx, d, ProposalApproveInput{ID: prop, Items: byProposal[prop], Reason: comment}, newDecision(d, model.DecideApprove, ""))
			if err != nil {
				return out, err
			}
			out.Proposals = append(out.Proposals, r)
			continue
		}
		res := ProposalReviewOutput{ID: prop, Items: []Decision{}}
		for _, id := range byProposal[prop] {
			cur, err := loadProposal(ctx, d, prop)
			if err != nil {
				return out, err
			}
			if it, err := cur.item(id); err != nil || !answerable(it.State) {
				continue // its bundle closed it already
			}
			r, err := denyItem(ctx, d, prop, id, comment, newDecision(d, model.DecideDeny, ""))
			if err != nil {
				return out, err
			}
			res.Items, res.Status, res.Rev = append(res.Items, r.Items...), r.Status, r.Rev
		}
		out.Proposals = append(out.Proposals, res)
	}
	return out, nil
})
