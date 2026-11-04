package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/iodesystems/para/internal/model"
)

// Tracked documents in dossiers. A para-doc-edit block whose target is a
// tracked document must quote, in old, text that is in its latest version:
// an edit of text that is no longer there is refused when the agent files
// it. When the edit is approved, an agent carries it out with its own tools
// and reports it with proposal_executed and the document's new text, which
// becomes the next version, so the edit is recorded and its diff visible.
//
// A para-upstream block shows the change of a tracked document from version
// from to version to, with the comments it lists. Para runs it when the
// owner approves it: the document is marked seen up to to, and each comment
// the owner answered becomes an Ask, a question, or a commitment, or is
// resolved or dropped.

// trackedTarget returns the tracked document a block's target names, or nil
// when it names something else.
func trackedTarget(ctx context.Context, d Deps, target string) (model.Document, *model.TrackedDoc, bool) {
	id := normID(target)
	if id == "" || strings.Contains(id, "/") {
		return model.Document{}, nil, false
	}
	doc, err := d.Store.Get(ctx, id)
	if err != nil {
		return model.Document{}, nil, false
	}
	m, ok := doc.Meta.(*model.TrackedDoc)
	return doc, m, ok
}

// checkDocEdit refuses a doc edit of a tracked document whose old text is
// not in the latest version.
func checkDocEdit(ctx context.Context, d Deps, it *model.ProposalItem) error {
	doc, m, ok := trackedTarget(ctx, d, it.Target)
	if !ok {
		return nil
	}
	it.Target = string(model.LinkTo(doc.ID))
	if strings.TrimSpace(it.Old) == "" {
		return fmt.Errorf("a doc edit of tracked document %s needs old: the text it replaces, as it is in version %d", doc.ID, m.Latest)
	}
	v, err := readVersion(ctx, d, doc.ID, m.Latest)
	if err != nil {
		return err
	}
	if _, _, found := locateQuote(v.text, it.Old, 0); !found {
		return fmt.Errorf("old is not in the latest version (%d) of %s: read the document again (doc_version_add) or quote its words exactly", m.Latest, doc.ID)
	}
	return nil
}

// checkUpstream checks an upstream block and fills its versions: to is the
// latest unless given, from the last version seen unless given.
func checkUpstream(ctx context.Context, d Deps, it *model.ProposalItem) error {
	doc, m, ok := trackedTarget(ctx, d, it.Target)
	if !ok {
		return fmt.Errorf("target %q is not a tracked document", it.Target)
	}
	it.Target = string(model.LinkTo(doc.ID))
	if it.To == 0 {
		it.To = m.Latest
	}
	if it.From == 0 {
		it.From = m.LastSeen
	}
	switch {
	case it.To > m.Latest:
		return fmt.Errorf("to %d: %s has versions 1 to %d", it.To, doc.ID, m.Latest)
	case it.From >= it.To:
		return fmt.Errorf("from %d is not before to %d: %s has nothing new to show (last seen %d)", it.From, it.To, doc.ID, m.LastSeen)
	}
	for _, c := range it.Comments {
		if _, err := findComment(m, c); err != nil {
			return fmt.Errorf("%s: %w", doc.ID, err)
		}
	}
	return nil
}

// runUpstream carries out an approved upstream item: seen, then each comment
// the owner answered. It returns what it did, in words.
func runUpstream(ctx context.Context, d Deps, proposal string, it model.ProposalItem, actor string) (string, error) {
	seen, err := markSeen(ctx, d, DocSeenInput{ID: it.Target, Version: it.To})
	if err != nil {
		return "", err
	}
	done := []string{fmt.Sprintf("%s seen to version %d", seen.ID, seen.LastSeen)}
	if it.Answer == nil {
		return done[0], nil
	}
	ctx = context.WithValue(ctx, itemRefKey{}, proposalRef(proposal, it.ID))
	for _, c := range it.Answer.Comments {
		sub := model.ProposalItem{ID: it.ID, Evidence: it.Evidence, Decided: it.Decided,
			How: fmt.Sprintf("The owner answered comment %s of %s in %s: %s.", c.ID, seen.ID, proposal, c.To)}
		switch c.To {
		case CommentToAsk, CommentToQuestion, CommentToCommitment:
			sub.Op, sub.Input = "doc_comment_file", map[string]any{"id": seen.ID, "comment": c.ID, "to": c.To}
			if c.Due != "" {
				sub.Input["due"] = c.Due
			}
		case string(model.CommentResolved), string(model.CommentDropped):
			sub.Op, sub.Input = "doc_comment_resolve", map[string]any{"id": seen.ID, "comment": c.ID, "resolution": c.To,
				"note": firstOf(ioLine(c.Note), "The owner "+c.To+" it in "+proposal+".")}
		default:
			return strings.Join(done, "; "), fmt.Errorf("comment %s: to %q: want one of %v", c.ID, c.To, model.CommentTargets)
		}
		result, err := runItem(ctx, d, sub, actor)
		if err != nil {
			return strings.Join(done, "; "), fmt.Errorf("comment %s: %w", c.ID, err)
		}
		done = append(done, fmt.Sprintf("%s %s %s", c.ID, c.To, result))
	}
	return strings.Join(done, "; "), nil
}

// finishUpstream runs a marked upstream item and records the result on it.
func finishUpstream(ctx context.Context, d Deps, id string, run model.ProposalItem, by string) (*proposal, Decision, error) {
	result, opErr := runUpstream(ctx, d, id, run, by)
	p, err := changeProposal(ctx, d, id, func(p *proposal) error {
		it, err := p.item(run.ID)
		if err != nil {
			return err
		}
		it.State, it.Result = model.ItemApproved, result
		if opErr != nil {
			it.State, it.Reason = model.ItemFailed, PublicError(opErr, nil)
		}
		return nil
	})
	if err != nil {
		return nil, Decision{}, err
	}
	it, err := p.item(run.ID)
	if err != nil {
		return nil, Decision{}, err
	}
	if err := record(ctx, d, "proposal_run", p.doc.ID, it.ID, model.ItemRunning, it.State, it.ApprovedHash); err != nil {
		return nil, Decision{}, err
	}
	if it.State == model.ItemFailed {
		if p, err = haltAfterFailure(ctx, d, id, *it); err != nil {
			return nil, Decision{}, err
		}
	} else if p, err = runReady(ctx, d, id); err != nil {
		return nil, Decision{}, err
	}
	if it, err = p.item(run.ID); err != nil {
		return nil, Decision{}, err
	}
	return p, decisions([]model.ProposalItem{*it})[0], nil
}

// recordEditVersion stores the text an agent read after carrying out an
// approved doc edit of a tracked document as its next version.
func recordEditVersion(ctx context.Context, d Deps, proposal string, it model.ProposalItem, text string) (*DocVersionAddOutput, error) {
	doc, _, ok := trackedTarget(ctx, d, it.Target)
	if !ok {
		if strings.TrimSpace(text) != "" {
			return nil, errors.New("text is for a doc edit of a tracked document")
		}
		return nil, nil
	}
	if strings.TrimSpace(text) == "" {
		return nil, fmt.Errorf("item %s edits tracked document %s: give text, the document as you read it after the edit", it.ID, doc.ID)
	}
	out, err := addVersion(ctx, d, DocVersionAddInput{ID: doc.ID, Text: text, Proposal: proposal, Item: it.ID})
	if err != nil {
		return nil, err
	}
	return &out, nil
}
