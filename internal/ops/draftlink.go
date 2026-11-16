package ops

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// A draft for an item (a question, task, or obligation) is something the item
// waits on: the item depends_on the draft until the draft is sent or dropped.

// draftItem reports whether t is a type of item a draft is made for.
func draftItem(t model.Type) bool {
	return t == model.TypeQuestion || t == model.TypeTask || t == model.TypeObligation
}

// heldRequestIDs are the requests the actor holds now.
func heldRequestIDs(ctx context.Context, s store.Store, actor string) []string {
	docs, err := loadRequests(ctx, s)
	if err != nil {
		return nil
	}
	var ids []string
	for _, doc := range docs {
		if r := doc.Meta.(*model.Request); r.Status == model.RequestClaimed && r.Claimed == actor {
			ids = append(ids, doc.ID)
		}
	}
	return ids
}

// dependOnDraft makes the open item wait on the draft in doc, in memory: it
// reports false when the item is closed, already waits on it, or is no item a
// draft is for.
func dependOnDraft(item *model.Document, draft string) bool {
	if !draftItem(item.Meta.Header().Type) || lifeOf(item.Meta) != LifeOpen {
		return false
	}
	hard, optional := model.DepLists(item.Meta)
	l := model.LinkTo(draft)
	if hard == nil || slices.Contains(*hard, l) {
		return false
	}
	*hard = append(*hard, l)
	*optional = slices.DeleteFunc(*optional, func(x model.Link) bool { return x == l })
	return true
}

// linkDraftFor makes the item the draft is for wait on it: the draft's thread
// when it is a question, task, or obligation, else the first such link of the
// request the actor holds. It returns the warnings to show the writer.
func linkDraftFor(ctx context.Context, d Deps, draft string, thread model.Link) []string {
	target := ""
	if id := thread.Target(); id != "" {
		if doc, err := d.Store.Get(ctx, id); err == nil && draftItem(doc.Meta.Header().Type) {
			target = id
		}
	}
	if target == "" {
		held := heldRequestIDs(ctx, d.Store, d.Store.Info().Actor)
		switch {
		case len(held) > 1:
			return []string{fmt.Sprintf("%s was not linked to the item it is for: you hold %d requests (%s). Link it: link {from: <item>, to: %s, rel: depends_on}", draft, len(held), strings.Join(held, ", "), draft)}
		case len(held) == 1:
			if rd, err := d.Store.Get(ctx, held[0]); err == nil {
				for _, l := range rd.Meta.(*model.Request).Links {
					if doc, err := d.Store.Get(ctx, l.Target()); err == nil && draftItem(doc.Meta.Header().Type) {
						target = doc.ID
						break
					}
				}
			}
		}
	}
	if target == "" {
		return nil
	}
	doc, err := d.Store.Get(ctx, target)
	if err != nil {
		return nil
	}
	if !dependOnDraft(&doc, draft) {
		return nil
	}
	if _, err := d.Store.Update(ctx, doc); err != nil {
		return []string{fmt.Sprintf("%s was not linked to %s: %v", draft, target, err)}
	}
	addReceiptNote(ctx, fmt.Sprintf("%s now waits on the draft %s.", target, draft))
	return []string{fmt.Sprintf("linked: %s depends_on %s (it waits until the draft is sent or dropped)", target, draft)}
}

// openOutDraft reports whether id is an outgoing io item not yet sent or closed.
func openOutDraft(ctx context.Context, s store.Store, id string) bool {
	doc, err := s.Get(ctx, id)
	if err != nil {
		return false
	}
	m, ok := doc.Meta.(*model.IO)
	return ok && m.Direction == model.IOOut && m.State.Open()
}

// linkNamedDrafts makes item wait on each open outgoing draft its notes name,
// in memory. It returns the ids linked.
func linkNamedDrafts(ctx context.Context, s store.Store, item *model.Document, notes string) []string {
	var linked []string
	for _, id := range namedIDs(ctx, s, notes) {
		if id != item.ID && openOutDraft(ctx, s, id) && dependOnDraft(item, id) {
			linked = append(linked, id)
		}
	}
	return linked
}

// closeDraftDependents runs when a draft was sent. An open question that
// carries an answer (a proposal) is answered with it, citing the draft; any
// other dependent only stops waiting. It returns notes for the writer.
func closeDraftDependents(ctx context.Context, d Deps, draft string) []string {
	refs, err := d.Store.List(ctx, store.Filter{Type: model.TypeQuestion})
	if err != nil {
		return nil
	}
	var notes []string
	for _, r := range refs {
		doc, q, err := getQuestion(ctx, d.Store, r.ID)
		if err != nil || q.Status != model.QuestionOpen || !slices.Contains(targets(q.DependsOn), draft) || !q.HasProposal() {
			continue
		}
		rec := answerRecord{choice: q.ProposedChoice, text: q.ProposedAnswer, why: q.ProposedWhy, by: q.ProposedBy,
			sources: append(slices.Clone(q.ProposedSources), model.AnswerSource{Ref: draft})}
		if _, err := writeAnswer(ctx, d, doc, q, rec); err != nil {
			notes = append(notes, fmt.Sprintf("%s was sent, but %s (answered by it) was not closed: %v", draft, doc.ID, err))
			continue
		}
		addReceiptNote(ctx, fmt.Sprintf("%s closed: its answer was sent in %s.", doc.ID, draft))
		notes = append(notes, fmt.Sprintf("closed %s: its answer went out in %s", doc.ID, draft))
	}
	if un, err := unblockedBy(ctx, d.Store, draft); err == nil && len(un) > 0 {
		notes = append(notes, "no longer waiting on "+draft+": "+strings.Join(un, ", "))
	}
	return notes
}
