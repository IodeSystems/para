package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/iodesystems/para/internal/model"
)

// Decisions. proposal_approve and proposal_deny are definePerson: a person at a
// terminal. proposal_decide is how an agent records what the owner said in the
// chat: the owner's words are stored on each item, marked self-reported. The
// web app only reads proposals.

// changeProposal loads a revision, applies fn, and saves. It reads again when another
// writer got in first, so a review never overwrites a change. fn runs once per
// try, so it must not write anything else.
func changeProposal(ctx context.Context, d Deps, id string, fn func(p *proposal) error) (*proposal, error) {
	for range claimRetries {
		p, err := loadProposal(ctx, d, id)
		if err != nil {
			return nil, err
		}
		if err := fn(p); err != nil {
			return nil, err
		}
		err = p.save(ctx, d)
		if errors.Is(err, model.ErrConflict) {
			continue
		}
		return p, err
	}
	return nil, fmt.Errorf("could not save %s after %d tries: it keeps changing", id, claimRetries)
}

// itemValue is an earlier item's result, for a placeholder.
func itemValue(p *proposal, item, field string) (string, bool) {
	it, err := p.item(item)
	if err != nil {
		return "", false
	}
	if field == "result" && it.Result != "" {
		return it.Result, true
	}
	if it.Execution == nil {
		return "", false
	}
	switch field {
	case "url":
		return it.Execution.URL, it.Execution.URL != ""
	case "locator":
		return it.Execution.Locator, it.Execution.Locator != ""
	case "result":
		return it.Execution.Result, true
	}
	return "", false
}

// fillText fills the placeholders of text from earlier results.
func fillText(p *proposal, text string) string {
	return model.FillPlaceholders(text, func(item, field string) (string, bool) { return itemValue(p, item, field) })
}

func firstOf(list ...string) string {
	for _, s := range list {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
