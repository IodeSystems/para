package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/iodesystems/parasolve/internal/model"
)

// AnnotationAnswer answers one open annotation. A declined one needs a reason.
type AnnotationAnswer struct {
	Annotation string                `json:"annotation"`
	Outcome    model.AnnotationState `json:"outcome"` // applied or declined
	Reason     string                `json:"reason,omitempty"`
}

// ProposalReviseInput is the input of proposal_revise.
type ProposalReviseInput struct {
	ID       string             `json:"id"`
	Items    []ItemInput        `json:"items,omitempty"`    // replacements (with id) and new items (no id)
	Withdraw []string           `json:"withdraw,omitempty"` // item ids to drop
	Answers  []AnnotationAnswer `json:"answers,omitempty"`  // one for each open annotation
}

// revisable reports whether an agent may replace or withdraw an item. A final
// decision of the owner is not the agent's to change.
func revisable(s model.ItemState) bool {
	return slices.Contains([]model.ItemState{model.ItemPending, model.ItemHeld, model.ItemRework, model.ItemSentBack, model.ItemFailed, model.ItemRunning}, s)
}

// answerAnnotations applies answers. It fails on any open annotation left
// unanswered, and on an answer to anything but an open annotation.
func answerAnnotations(list []model.Annotation, answers []AnnotationAnswer) ([]model.Annotation, error) {
	out := slices.Clone(list)
	seen := map[string]bool{}
	for _, ans := range answers {
		i := slices.IndexFunc(out, func(a model.Annotation) bool { return a.ID == ans.Annotation })
		switch {
		case i < 0:
			return nil, fmt.Errorf("no annotation %q", ans.Annotation)
		case seen[ans.Annotation]:
			return nil, fmt.Errorf("annotation %s is answered twice", ans.Annotation)
		case out[i].State != model.AnnotationOpen:
			return nil, fmt.Errorf("annotation %s is already %s", ans.Annotation, out[i].State)
		case ans.Outcome != model.AnnotationApplied && ans.Outcome != model.AnnotationDeclined:
			return nil, fmt.Errorf("annotation %s: outcome %q: want applied or declined", ans.Annotation, ans.Outcome)
		case ans.Outcome == model.AnnotationDeclined && strings.TrimSpace(ans.Reason) == "":
			return nil, fmt.Errorf("annotation %s: declined needs a reason", ans.Annotation)
		}
		seen[ans.Annotation] = true
		out[i].State, out[i].Answer = ans.Outcome, strings.TrimSpace(ans.Reason)
	}
	var open []string
	for _, a := range out {
		if a.State == model.AnnotationOpen {
			open = append(open, a.ID)
		}
	}
	if len(open) > 0 {
		return nil, fmt.Errorf("open annotations have no answer: %s: answer each as applied, or declined with a reason", strings.Join(open, ", "))
	}
	return out, nil
}

// reviseItems builds the items of the next revision. Items sent back or
// returned for rework wait for the owner again. A replaced item keeps its hold,
// and a replaced or carried item loses its edited mark: the agent has read it.
func reviseItems(ctx context.Context, d Deps, prev []model.ProposalItem, in ProposalReviseInput) ([]model.ProposalItem, error) {
	out := slices.Clone(prev)
	n := 0
	for _, it := range out {
		var v int
		if _, err := fmt.Sscanf(it.ID, "i%d", &v); err == nil {
			n = max(n, v)
		}
	}
	find := func(id string) int {
		return slices.IndexFunc(out, func(it model.ProposalItem) bool { return it.ID == id })
	}
	for _, id := range in.Withdraw {
		i := find(id)
		if i < 0 {
			return nil, fmt.Errorf("no item %q", id)
		}
		if !revisable(out[i].State) {
			return nil, fmt.Errorf("item %s is %s: the owner has decided it", id, out[i].State)
		}
		out[i].State = model.ItemWithdrawn
	}
	for _, raw := range in.Items {
		if raw.ID == "" {
			n++
			it, err := newItem(ctx, d, raw, itemID(n))
			if err != nil {
				return nil, fmt.Errorf("new item: %w", err)
			}
			out = append(out, it)
			continue
		}
		i := find(raw.ID)
		if i < 0 {
			return nil, fmt.Errorf("no item %q: leave id out for a new item", raw.ID)
		}
		if !revisable(out[i].State) {
			return nil, fmt.Errorf("item %s is %s: the owner has decided it", raw.ID, out[i].State)
		}
		it, err := newItem(ctx, d, raw, raw.ID)
		if err != nil {
			return nil, fmt.Errorf("item %s: %w", raw.ID, err)
		}
		if out[i].State == model.ItemHeld {
			it.State, it.HeldBy = model.ItemHeld, out[i].HeldBy
		}
		out[i] = it
	}
	for i := range out {
		switch out[i].State {
		case model.ItemRework, model.ItemSentBack:
			out[i].State, out[i].Reason = model.ItemPending, ""
		}
		out[i].Edited = false
	}
	return out, nil
}

// ProposalRevise files the next revision as a new document that supersedes
// this one. The old file stays as history. It marks the old revision first,
// with Rev compared, so a review that got in first makes it fail and read again.
var ProposalRevise = define("proposal_revise", "File the next revision of a proposal. Answer each open annotation as applied, or declined with a reason, or it fails", func(ctx context.Context, d Deps, in ProposalReviseInput) (ProposalFiled, error) {
	if len(in.Items) == 0 && len(in.Withdraw) == 0 && len(in.Answers) == 0 {
		return ProposalFiled{}, errors.New("nothing to revise: give items, withdraw, or answers")
	}
	for range claimRetries {
		p, err := loadProposal(ctx, d, in.ID)
		if err != nil {
			return ProposalFiled{}, err
		}
		if p.meta.Status == model.ProposalSuperseded {
			return ProposalFiled{}, fmt.Errorf("%s is superseded: revise the latest revision", p.doc.ID)
		}
		next := model.ProposalContent{}
		if next.Annotations, err = answerAnnotations(p.content.Annotations, in.Answers); err != nil {
			return ProposalFiled{}, err
		}
		if next.Items, err = reviseItems(ctx, d, p.content.Items, in); err != nil {
			return ProposalFiled{}, err
		}
		body, err := model.RenderProposalBody(next)
		if err != nil {
			return ProposalFiled{}, err
		}
		prev := p.meta.Status
		p.meta.Status = model.ProposalSuperseded
		old, err := d.Store.Update(ctx, p.doc)
		if errors.Is(err, model.ErrConflict) {
			continue
		}
		if err != nil {
			return ProposalFiled{}, err
		}
		doc, err := d.Store.Create(ctx, model.Document{
			Folder: model.FolderProposals, Title: p.doc.Title, Body: body,
			Meta: &model.Proposal{
				Common: model.Common{Type: model.TypeProposal, Tags: p.meta.Tags}, Source: p.meta.Source,
				Status: proposalStatus(next.Items), Revision: p.meta.Revision + 1,
				Supersedes: model.LinkTo(p.doc.ID), ProposedBy: p.meta.ProposedBy,
			},
		})
		if err != nil {
			// Lint refused the new text: this revision stays the current one.
			old.Meta.(*model.Proposal).Status = prev
			if _, undo := d.Store.Update(ctx, old); undo != nil {
				return ProposalFiled{}, errors.Join(err, undo)
			}
			return ProposalFiled{}, err
		}
		return filed(d, doc)
	}
	return ProposalFiled{}, fmt.Errorf("could not revise %s after %d tries: it keeps changing", in.ID, claimRetries)
})
