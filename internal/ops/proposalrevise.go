package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/iodesystems/para/internal/model"
)

// ProposalReviseInput is the input of proposal_revise. Markdown is the whole
// new dossier body; the other fields change items one by one.
type ProposalReviseInput struct {
	ID       string      `json:"id"`
	Markdown string      `json:"markdown,omitempty"` // the new dossier body; then leave the other fields out
	TLDR     string      `json:"tldr,omitempty"`     // replaces the tldr
	Context  string      `json:"context,omitempty"`  // replaces the context
	Items    []ItemInput `json:"items,omitempty"`    // replacements (with id) and new items (no id)
	Withdraw []string    `json:"withdraw,omitempty"` // item ids to drop
}

// revisable reports whether an agent may replace or withdraw an item. A final
// decision of the owner is not the agent's to change.
func revisable(s model.ItemState) bool {
	return slices.Contains([]model.ItemState{model.ItemPending, model.ItemSentBack, model.ItemHalted, model.ItemFailed, model.ItemRunning, model.ItemSkipped}, s)
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
		// As in a markdown revision: unchanged text keeps para's fields; a
		// changed item keeps the owner's answer, marked stale.
		oldHash, _ := out[i].Hash()
		newHash, _ := it.Hash()
		switch {
		case oldHash == newHash:
			keepParaOwned(&it, out[i])
		case out[i].Answer != nil:
			a := *out[i].Answer
			a.Stale = true
			it.Answer = &a
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

// ProposalRevise rewrites a proposal in place as its next revision. The body
// before is kept in a receipt.
var ProposalRevise = defineSafe("proposal_revise", "Revise a dossier in place: give markdown, the whole new body, or change items one by one (items with id replace that item, without id are appended, withdraw drops, or a new tldr or context). "+
	"Items are matched by id only, so keep each item's id: in markdown an item with an id the dossier lacks is a new item, an item you leave out is withdrawn, and an item with the same title but another id does not replace the old one. "+
	"An item whose text is unchanged keeps the owner's answer; a changed one waits for the owner again with its answer marked stale. Items the owner decided cannot change. The old body is kept in a receipt; proposal_diff shows the change", func(ctx context.Context, d Deps, in ProposalReviseInput) (ProposalFiled, error) {
	markdown := strings.TrimSpace(in.Markdown) != ""
	if markdown && (len(in.Items) > 0 || len(in.Withdraw) > 0 || in.TLDR != "" || in.Context != "") {
		return ProposalFiled{}, errors.New("give markdown, or items, withdraw, tldr, and context; not both")
	}
	if !markdown && len(in.Items) == 0 && len(in.Withdraw) == 0 && in.TLDR == "" && in.Context == "" {
		return ProposalFiled{}, errors.New("nothing to revise: give markdown, items, withdraw, tldr, or context")
	}
	p, err := reviseInPlace(ctx, d, in.ID, "", func(p *proposal, rev int) error {
		if markdown {
			next, err := parseAgentDossier(ctx, d, in.Markdown, false)
			if err != nil {
				return err
			}
			return mergeRevision(p, next, rev)
		}
		if t := strings.TrimSpace(in.TLDR); t != "" {
			p.content.TLDR = t
		}
		if t := strings.TrimSpace(in.Context); t != "" {
			p.content.Context = t
		}
		before := map[string]string{}
		for _, it := range p.content.Items {
			before[it.ID], _ = it.Hash()
		}
		items, err := reviseItems(ctx, d, p.content.Items, in)
		if err != nil {
			return err
		}
		for i := range items {
			if h, _ := items[i].Hash(); before[items[i].ID] != h {
				items[i].Rev = rev
			}
		}
		p.content.Items = items
		return nil
	})
	if err != nil {
		return ProposalFiled{}, err
	}
	return filed(d, p.doc)
})
