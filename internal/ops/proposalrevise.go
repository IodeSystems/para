package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/parasolve/internal/model"
)

// NoteAnswer answers one open note. Outcome is applied, declined, or failed: a
// change or investigation the agent could not complete. Declined and failed need a reason.
type NoteAnswer struct {
	Annotation string          `json:"annotation"` // the note's id
	Outcome    model.TurnState `json:"outcome"`
	Reason     string          `json:"reason,omitempty"`
}

// ProposalReviseInput is the input of proposal_revise.
type ProposalReviseInput struct {
	ID       string       `json:"id"`
	TLDR     string       `json:"tldr,omitempty"`     // replaces the tldr
	Context  string       `json:"context,omitempty"`  // replaces the context
	Items    []ItemInput  `json:"items,omitempty"`    // replacements (with id) and new items (no id)
	Withdraw []string     `json:"withdraw,omitempty"` // item ids to drop
	Answers  []NoteAnswer `json:"answers,omitempty"`  // one for each open note
}

// revisable reports whether an agent may replace or withdraw an item. A final
// decision of the owner is not the agent's to change.
func revisable(s model.ItemState) bool {
	return slices.Contains([]model.ItemState{model.ItemPending, model.ItemHeld, model.ItemSentBack, model.ItemHalted, model.ItemFailed, model.ItemRunning}, s)
}

// answerNotes applies answers to a copy of items. It fails on any open note left
// unanswered, and on an answer to anything but an open note. Resolved lists the
// changes and investigations answered, with their items.
func answerNotes(list []model.ProposalItem, answers []NoteAnswer) ([]model.ProposalItem, []resolvedTurn, error) {
	out := make([]model.ProposalItem, len(list))
	for i, it := range list {
		out[i] = it
		out[i].Thread = slices.Clone(it.Thread)
	}
	var done []resolvedTurn
	seen := map[string]bool{}
	for _, ans := range answers {
		var t *model.Turn
		var holder *model.ProposalItem
		for i := range out {
			for j := range out[i].Thread {
				if ans.Annotation != "" && out[i].Thread[j].ID == ans.Annotation {
					t, holder = &out[i].Thread[j], &out[i]
				}
			}
		}
		reason := strings.TrimSpace(ans.Reason)
		switch {
		case t == nil:
			return nil, nil, fmt.Errorf("no note %q", ans.Annotation)
		case seen[ans.Annotation]:
			return nil, nil, fmt.Errorf("note %s is answered twice", ans.Annotation)
		case t.State != model.TurnOpen:
			return nil, nil, fmt.Errorf("note %s is already %s", ans.Annotation, t.State)
		case !slices.Contains([]model.TurnState{model.TurnApplied, model.TurnDeclined, model.TurnFailed}, ans.Outcome):
			return nil, nil, fmt.Errorf("note %s: outcome %q: want applied, declined, or failed", ans.Annotation, ans.Outcome)
		case ans.Outcome == model.TurnFailed && !isDirective(t.Kind):
			return nil, nil, fmt.Errorf("note %s: failed is for a change or an investigation", ans.Annotation)
		case ans.Outcome != model.TurnApplied && reason == "":
			return nil, nil, fmt.Errorf("note %s: %s needs a reason", ans.Annotation, ans.Outcome)
		}
		seen[ans.Annotation] = true
		t.State, t.Answer = ans.Outcome, reason
		if isDirective(t.Kind) {
			done = append(done, resolvedTurn{item: holder.ID, target: holder.For, turn: *t})
		}
	}
	var open []string
	for _, it := range out {
		for _, t := range it.Thread {
			if t.State == model.TurnOpen {
				open = append(open, t.ID)
			}
		}
	}
	if len(open) > 0 {
		return nil, nil, fmt.Errorf("open notes have no answer: %s: answer each as applied, or declined with a reason", strings.Join(open, ", "))
	}
	return out, done, nil
}

// isDirective reports whether a turn asks the agent to do something, not to
// explain. Redirect is the old word for change.
func isDirective(k model.TurnKind) bool {
	return k == model.TurnChange || k == model.TurnRedirect || k == model.TurnInvestigate
}

// resolvedTurn is a redirect the agent answered.
type resolvedTurn struct {
	item   string
	target string
	turn   model.Turn
}

// recordAttempts writes each redirect's result on the item's task or Ask, or on
// the proposal's source when that is one. A failure to resolve is recorded too.
// A redirect with neither has nowhere to go and stays on the item's thread.
func recordAttempts(ctx context.Context, d Deps, source string, list []resolvedTurn) error {
	for _, r := range list {
		target := r.target
		if target == "" {
			target = source
		}
		doc, err := d.Store.Get(ctx, target)
		if err != nil {
			if errors.Is(err, model.ErrNotFound) {
				continue
			}
			return err
		}
		if t := doc.Meta.Header().Type; t != model.TypeTask && t != model.TypeRFX {
			continue
		}
		line := fmt.Sprintf("- %s %s: %s %q on %s: %s", d.Store.Now().UTC().Format(time.RFC3339), d.Store.Info().Actor, r.turn.Kind,
			strings.Join(strings.Fields(r.turn.Text), " "), r.item, r.turn.State)
		if r.turn.Answer != "" {
			line += ": " + strings.Join(strings.Fields(r.turn.Answer), " ")
		}
		if strings.Contains(doc.Body, "\n## Attempts\n") || strings.HasPrefix(doc.Body, "## Attempts\n") {
			doc.Body += "\n" + line
		} else {
			doc.Body = appendSection(doc.Body, "Attempts", line)
		}
		if _, err := d.Store.Update(ctx, doc); err != nil {
			return err
		}
	}
	return nil
}

// reviseItems builds the items of the next revision. Items sent back or
// returned for rework wait for the owner again. A replaced item keeps its hold,
// and a replaced or carried item loses its edited mark: the agent has read it.
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
		if out[i].State == model.ItemHeld {
			it.State, it.HeldBy = model.ItemHeld, out[i].HeldBy
		}
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
// this one. The old file stays as history. It marks the old revision first,
// with Rev compared, so a review that got in first makes it fail and read again.
var ProposalRevise = defineSafe("proposal_revise", "File the next revision of a proposal. Answer each open note as applied, or declined with a reason; a change or an investigation you could not complete is failed, with a reason. It fails if a note has no answer", func(ctx context.Context, d Deps, in ProposalReviseInput) (ProposalFiled, error) {
	if len(in.Items) == 0 && len(in.Withdraw) == 0 && len(in.Answers) == 0 && in.TLDR == "" && in.Context == "" {
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
		next := model.ProposalContent{TLDR: p.content.TLDR, Context: p.content.Context}
		if t := strings.TrimSpace(in.TLDR); t != "" {
			next.TLDR = t
		}
		if t := strings.TrimSpace(in.Context); t != "" {
			next.Context = t
		}
		answered, resolved, err := answerNotes(p.content.Items, in.Answers)
		if err != nil {
			return ProposalFiled{}, err
		}
		if next.Items, err = reviseItems(ctx, d, answered, in); err != nil {
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
		if err := recordAttempts(ctx, d, p.meta.Source.Target(), resolved); err != nil {
			return ProposalFiled{}, err
		}
		return filed(d, doc)
	}
	return ProposalFiled{}, fmt.Errorf("could not revise %s after %d tries: it keeps changing", in.ID, claimRetries)
})
