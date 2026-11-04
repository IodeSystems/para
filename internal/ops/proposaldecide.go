package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/iodesystems/para/internal/model"
)

// proposal_decide is how the owner decides: in the chat with the agent, who
// records their words. The owner reads the proposal in the web app
// (proposal_open) and answers in the chat; the web app decides nothing. Para
// cannot see the chat, so each decision carries the owner's words and is
// marked self-reported.

// DecideItem is one item's decision. Text is the change for change (a message
// or an action: the new body; a para item: the new input as a JSON object),
// and the reason for deny.
type DecideItem struct {
	Item     string `json:"item"`
	Decision string `json:"decision"` // approve, deny, or change
	Text     string `json:"text,omitempty"`
}

// ProposalDecideInput is the input of proposal_decide.
type ProposalDecideInput struct {
	ID    string       `json:"id"`
	Items []DecideItem `json:"items"`
	// OwnerSaid is the owner's words from the chat, verbatim. Required.
	OwnerSaid string `json:"owner_said"`
}

// ProposalDecideOutput is the output of proposal_decide: the decided items,
// and the new revision when an item was changed.
type ProposalDecideOutput struct {
	ID       string               `json:"id"`
	Status   model.ProposalStatus `json:"status"`
	Items    []Decision           `json:"items"`
	Failed   []string             `json:"failed,omitempty"`   // approved para items whose op failed
	Revision string               `json:"revision,omitempty"` // the new revision, when an item was changed
	Rev      string               `json:"rev"`
}

var decideKinds = []string{model.DecideApprove, model.DecideDeny, model.DecideChange}

// ProposalDecide records the owner's decisions from the chat. approve runs a
// para item at once under the proposing agent, with a receipt that carries the
// owner's words; an approved message or action stays approved, not done, until
// proposal_executed or action_record. deny closes the item. change revises the
// item from text as a new revision; it stays pending for the owner.
var ProposalDecide = defineSafe("proposal_decide", "Record the owner's decision from the chat on proposal items: approve, deny, or change (text is the new body of a message or action, or the new input of a para item as a JSON object; for deny, the reason). owner_said is required: the owner's words from the chat, verbatim; it is stored and marked self-reported. Approved para items run at once; approved messages and actions wait for you to send them with your own tool, then proposal_executed", func(ctx context.Context, d Deps, in ProposalDecideInput) (ProposalDecideOutput, error) {
	said := strings.TrimSpace(in.OwnerSaid)
	if said == "" {
		return ProposalDecideOutput{}, errors.New("owner_said is empty: give the owner's words from the chat, verbatim; without them nothing is decided")
	}
	if err := lintText("owner_said", said); err != nil {
		return ProposalDecideOutput{}, err
	}
	if d.Store.Info().Surface == "pwa" {
		return ProposalDecideOutput{}, fmt.Errorf("%w: the web app reads proposals; the owner decides in the chat or with para proposal approve", ErrPersonOnly)
	}
	if len(in.Items) == 0 {
		return ProposalDecideOutput{}, errors.New("no items")
	}
	p, err := loadProposal(ctx, d, in.ID)
	if err != nil {
		return ProposalDecideOutput{}, err
	}
	if err := checkDecisions(p, in.Items); err != nil {
		return ProposalDecideOutput{}, err
	}
	out := ProposalDecideOutput{ID: p.doc.ID, Items: []Decision{}, Status: p.meta.Status, Rev: p.doc.Rev}
	var approve, changes []DecideItem
	for _, x := range in.Items {
		switch x.Decision {
		case model.DecideApprove:
			approve = append(approve, x)
		case model.DecideChange:
			changes = append(changes, x)
		case model.DecideDeny:
			reason := firstOf(strings.TrimSpace(x.Text), "Owner said: "+said)
			r, err := denyItem(ctx, d, p.doc.ID, x.Item, reason, newDecision(d, model.DecideDeny, said))
			if err != nil {
				return out, err
			}
			out.Items = append(out.Items, r.Items...)
			out.Status, out.Rev = r.Status, r.Rev
		}
	}
	if len(approve) > 0 {
		ids := make([]string, 0, len(approve))
		for _, x := range approve {
			ids = append(ids, x.Item)
		}
		r, err := approveItems(ctx, d, ProposalApproveInput{ID: p.doc.ID, Items: ids}, newDecision(d, model.DecideApprove, said))
		if err != nil {
			return out, err
		}
		out.Items = append(out.Items, r.Items...)
		out.Failed = r.Failed
		out.Status, out.Rev = r.Status, r.Rev
	}
	if len(changes) > 0 {
		rec := newDecision(d, model.DecideChange, said)
		doc, err := fileRevision(ctx, d, p.doc.ID, func(cur *proposal) (model.ProposalContent, error) {
			next := model.ProposalContent{TLDR: cur.content.TLDR, Context: cur.content.Context, Items: slices.Clone(cur.content.Items)}
			for _, x := range changes {
				i := slices.IndexFunc(next.Items, func(it model.ProposalItem) bool { return it.ID == x.Item })
				if i < 0 {
					return next, fmt.Errorf("%s has no item %q", cur.doc.ID, x.Item)
				}
				it := next.Items[i]
				if err := changeItem(&it, x.Text); err != nil {
					return next, fmt.Errorf("item %s: %w", it.ID, err)
				}
				it.State, it.Reason, it.Edited, it.ApprovedHash, it.Decided = model.ItemPending, "", true, "", &rec
				next.Items[i] = it
			}
			return next, checkPlan(next.Items)
		})
		if err != nil {
			return out, err
		}
		np, err := loadProposal(ctx, d, doc.ID)
		if err != nil {
			return out, err
		}
		for _, x := range changes {
			it, err := np.item(x.Item)
			if err != nil {
				return out, err
			}
			hash, err := it.Hash()
			if err != nil {
				return out, err
			}
			if err := record(ctx, d, "proposal_change", np.doc.ID, it.ID, it.State, it.State, hash); err != nil {
				return out, err
			}
			out.Items = append(out.Items, decisions([]model.ProposalItem{*it})[0])
		}
		out.Revision, out.Status, out.Rev = np.doc.ID, np.meta.Status, np.doc.Rev
	}
	return out, nil
})

// checkDecisions refuses the whole call before anything changes: an unknown
// item or decision, an item named twice, an item that is not pending, a change
// with no text, or a bundle decided two ways. A bundle is decided as one.
func checkDecisions(p *proposal, list []DecideItem) error {
	seen := map[string]bool{}
	bundles := map[string]string{}
	for _, x := range list {
		if !slices.Contains(decideKinds, x.Decision) {
			return fmt.Errorf("item %s: decision %q: want approve, deny, or change", x.Item, x.Decision)
		}
		if seen[x.Item] {
			return fmt.Errorf("item %s is decided twice", x.Item)
		}
		seen[x.Item] = true
		it, err := p.item(x.Item)
		if err != nil {
			return err
		}
		if err := p.allow(it, x.Decision); err != nil {
			return err
		}
		if x.Decision == model.DecideChange && strings.TrimSpace(x.Text) == "" {
			return fmt.Errorf("item %s: change needs text: the new body, or for a para item the new input as JSON", x.Item)
		}
		if it.Bundle == "" {
			continue
		}
		if x.Decision == model.DecideChange {
			return fmt.Errorf("item %s is in bundle %s, which is approved or denied as one: change it with proposal_revise", x.Item, it.Bundle)
		}
		if prev, ok := bundles[it.Bundle]; ok && prev != x.Decision {
			return fmt.Errorf("bundle %s is decided as one: its items cannot be both approved and denied", it.Bundle)
		}
		bundles[it.Bundle] = x.Decision
	}
	return nil
}

// changeItem revises an item from the owner's text. A message or an action
// takes it as its new body. A para item takes it as its new input, a JSON
// object checked against the op; anything more is proposal_revise.
func changeItem(it *model.ProposalItem, text string) error {
	if it.Kind != model.ItemPara {
		it.Body = text
		return nil
	}
	var in map[string]any
	if err := json.Unmarshal([]byte(text), &in); err != nil || in == nil {
		return errors.New("a para item changes its input: give text as a JSON object of the op's input; for other changes call proposal_revise")
	}
	if err := checkOpItem(it.Op, in); err != nil {
		return err
	}
	it.Input = in
	return nil
}
