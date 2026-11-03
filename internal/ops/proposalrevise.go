package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/iodesystems/parasolve/internal/model"
)

// ProposalReviseInput is the input of proposal_revise.
type ProposalReviseInput struct {
	ID       string      `json:"id"`
	TLDR     string      `json:"tldr,omitempty"`     // replaces the tldr
	Context  string      `json:"context,omitempty"`  // replaces the context
	Items    []ItemInput `json:"items,omitempty"`    // replacements (with id) and new items (no id)
	Withdraw []string    `json:"withdraw,omitempty"` // item ids to drop
}

// revisable reports whether an agent may replace or withdraw an item. A final
// decision of the owner is not the agent's to change.
func revisable(s model.ItemState) bool {
	return slices.Contains([]model.ItemState{model.ItemPending, model.ItemSentBack, model.ItemHalted, model.ItemFailed, model.ItemRunning}, s)
}

// reviseItems builds the items of the next revision. Items sent back or
// halted wait for the owner again, and a replaced or carried item loses its
// edited mark: the agent has read it.
func reviseItems(ctx context.Context, d Deps, prev []model.ProposalItem, in ProposalReviseInput) ([]model.ProposalItem, error) {
	out := slices.Clone(prev)
	rules, err := loadProposalRules(d)
	if err != nil {
		return nil, err
	}
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
			it, err := newItem(ctx, d, raw, itemID(n), rules)
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
		it, err := newItem(ctx, d, raw, raw.ID, rules)
		if err != nil {
			return nil, fmt.Errorf("item %s: %w", raw.ID, err)
		}
		it.Thread = out[i].Thread
		out[i] = it
	}
	for i := range out {
		switch out[i].State {
		case model.ItemSentBack, model.ItemHalted:
			out[i].State, out[i].Reason = model.ItemPending, ""
		}
		out[i].Edited = false
	}
	return out, checkPlan(out)
}

// ProposalRevise files the next revision as a new document that supersedes
// this one. The old file stays as history.
var ProposalRevise = defineSafe("proposal_revise", "File the next revision of a proposal: replace items (with id), add items (no id), withdraw items, or change tldr or context. Items the owner decided stay as they are", func(ctx context.Context, d Deps, in ProposalReviseInput) (ProposalFiled, error) {
	if len(in.Items) == 0 && len(in.Withdraw) == 0 && in.TLDR == "" && in.Context == "" {
		return ProposalFiled{}, errors.New("nothing to revise: give items, withdraw, tldr, or context")
	}
	doc, err := fileRevision(ctx, d, in.ID, func(p *proposal) (model.ProposalContent, error) {
		next := model.ProposalContent{TLDR: p.content.TLDR, Context: p.content.Context}
		if t := strings.TrimSpace(in.TLDR); t != "" {
			next.TLDR = t
		}
		if t := strings.TrimSpace(in.Context); t != "" {
			next.Context = t
		}
		var err error
		next.Items, err = reviseItems(ctx, d, p.content.Items, in)
		return next, err
	})
	if err != nil {
		return ProposalFiled{}, err
	}
	return filed(d, doc)
})

// fileRevision files the next revision of id with the content build returns.
// It marks the old revision first, with Rev compared, so a decision that got in
// first makes it read again.
func fileRevision(ctx context.Context, d Deps, id string, build func(p *proposal) (model.ProposalContent, error)) (model.Document, error) {
	for range claimRetries {
		p, err := loadProposal(ctx, d, id)
		if err != nil {
			return model.Document{}, err
		}
		if p.meta.Status == model.ProposalSuperseded {
			return model.Document{}, fmt.Errorf("%s is superseded: revise the latest revision", p.doc.ID)
		}
		next, err := build(p)
		if err != nil {
			return model.Document{}, err
		}
		body, err := model.RenderProposalBody(next)
		if err != nil {
			return model.Document{}, err
		}
		prev := p.meta.Status
		p.meta.Status = model.ProposalSuperseded
		old, err := d.Store.Update(ctx, p.doc)
		if errors.Is(err, model.ErrConflict) {
			continue
		}
		if err != nil {
			return model.Document{}, err
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
				return model.Document{}, errors.Join(err, undo)
			}
			return model.Document{}, err
		}
		return doc, nil
	}
	return model.Document{}, fmt.Errorf("could not revise %s after %d tries: it keeps changing", id, claimRetries)
}
