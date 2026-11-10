package ops

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/iodesystems/para/internal/model"
)

// The owner changes or takes back a turn made by thread_post: while no agent
// has claimed it (thread_edit), or until it is done (thread_withdraw). Both
// leave one receipt, so receipt_undo restores the turn.

// ThreadEditInput changes an owner turn that no agent has claimed. Empty
// text and kind and a nil quote keep what the turn has; an empty quote
// removes it.
type ThreadEditInput struct {
	ID    string                `json:"id"` // the request that carries the turn
	Text  string                `json:"text,omitempty"`
	Kind  model.RequestTurnKind `json:"kind,omitempty"`
	Quote *string               `json:"quote,omitempty"`
}

// ThreadEditOutput is the edited turn.
type ThreadEditOutput struct {
	Request string              `json:"request"`
	Status  model.RequestStatus `json:"status"`
	Rev     string              `json:"rev"`
}

// ownerTurnRequest loads a request and says it is a turn the owner posted
// in a thread: kind sent-to-agent with a turn kind.
func ownerTurnRequest(ctx context.Context, d Deps, id string) (model.Document, *model.Request, error) {
	doc, r, err := getRequest(ctx, d.Store, id)
	if err != nil {
		return doc, nil, err
	}
	if requestKind(doc.Title, r) != SentKind || r.TurnKind == "" {
		return doc, nil, fmt.Errorf("%s is not a turn you posted in a thread", doc.ID)
	}
	return doc, r, nil
}

// kindLine finds the "Kind: ..." line of a turn body.
var kindLine = regexp.MustCompile(`(?m)^Kind: .*$`)

// removeSection takes the "## heading" section out of body.
func removeSection(body, heading string) string {
	lines := strings.Split(body, "\n")
	at := slices.IndexFunc(lines, func(l string) bool { return strings.TrimRight(l, " \t") == heading })
	if at < 0 {
		return body
	}
	end := sectionEnd(lines, at)
	out := append(slices.Clone(lines[:at]), lines[end:]...)
	return strings.Trim(strings.Join(out, "\n"), "\n")
}

// ThreadEdit changes a queued owner turn and keeps the old words in an
// "## Edited" section, shown in the thread.
var ThreadEdit = withOneReceipt(definePerson("thread_edit", "Change the owner's turn (request id from thread_post): text, kind, quote (empty removes it). Only while no agent has claimed it; the old words stay in the request under Edited. Once claimed it is refused: post a follow-up turn. One receipt undoes it. People only", func(ctx context.Context, d Deps, in ThreadEditInput) (ThreadEditOutput, error) {
	doc, r, err := ownerTurnRequest(ctx, d, in.ID)
	if err != nil {
		return ThreadEditOutput{}, err
	}
	switch r.Status {
	case model.RequestQueued, model.RequestProposed:
	case model.RequestClaimed:
		return ThreadEditOutput{}, errors.New("an agent already has it; post a follow-up turn instead")
	default:
		return ThreadEditOutput{}, fmt.Errorf("%s is %s: post a follow-up turn instead", doc.ID, r.Status)
	}
	oldWords, oldQuote, oldKind := "", r.Quote, r.TurnKind
	_, secs := bodyParts(doc.Body)
	hasAtts := false
	for _, s := range secs {
		switch s.Heading {
		case "Owner's words":
			oldWords = s.Text
		case "Attachments":
			hasAtts = true
		}
	}
	words := oldWords
	if t := strings.TrimSpace(strings.ReplaceAll(in.Text, "\r\n", "\n")); t != "" {
		words = t
	}
	quote := oldQuote
	if in.Quote != nil {
		quote = strings.TrimSpace(strings.ReplaceAll(*in.Quote, "\r\n", "\n"))
	}
	kind := oldKind
	if in.Kind != "" {
		if !slices.Contains(model.RequestTurnKinds, in.Kind) {
			return ThreadEditOutput{}, fmt.Errorf("kind %q: want one of %v", in.Kind, model.RequestTurnKinds)
		}
		kind = in.Kind
	}
	if words == "" && !hasAtts {
		return ThreadEditOutput{}, errors.New("the turn needs words or an attachment")
	}
	if words == oldWords && quote == oldQuote && kind == oldKind {
		return ThreadEditOutput{}, errors.New("nothing changed")
	}
	for name, v := range map[string]string{"text": words, "quote": quote} {
		if v != "" {
			if err := lintText(name, v); err != nil {
				return ThreadEditOutput{}, err
			}
		}
	}
	body := kindLine.ReplaceAllLiteralString(doc.Body, "Kind: "+turnKindNote(kind))
	if words == "" {
		words = "(none: see the attachments)"
	}
	body = setSection(body, "## Owner's words", flattenHeadings(words))
	body = removeSection(body, "## Quote")
	if quote != "" {
		body = strings.Replace(body, "\n\n## Owner's words", "\n\n## Quote\n\n"+quoteBlock(quote)+"\n\n## Owner's words", 1)
	}
	was := []string{oldWords}
	if kind != oldKind {
		was = append(was, "Kind was: "+string(oldKind))
	}
	if quote != oldQuote {
		was = append(was, "Quote was: "+firstOf(oldQuote, "(none)"))
	}
	doc.Body = appendSection(body, "Edited "+nowString(d.Store), strings.Join(was, "\n\n"))
	r.TurnKind, r.Quote = kind, quote
	doc, err = d.Store.Update(ctx, doc)
	if err != nil {
		return ThreadEditOutput{}, err
	}
	return ThreadEditOutput{Request: doc.ID, Status: r.Status, Rev: doc.Rev}, nil
}))

// ThreadWithdrawInput names the turn to withdraw.
type ThreadWithdrawInput struct {
	ID string `json:"id"` // the request that carries the turn
}

// ThreadWithdraw cancels an owner turn before it is done.
var ThreadWithdraw = withOneReceipt(definePerson("thread_withdraw", "Withdraw the owner's turn (request id from thread_post) while it is queued or claimed: it is cancelled, and a claimed turn's agent fails on its next note or done. One receipt undoes it. People only", func(ctx context.Context, d Deps, in ThreadWithdrawInput) (RequestChangeOutput, error) {
	doc, _, err := ownerTurnRequest(ctx, d, in.ID)
	if err != nil {
		return RequestChangeOutput{}, err
	}
	return RequestCancel.Run(ctx, d, RequestCancelInput{ID: doc.ID})
}))
