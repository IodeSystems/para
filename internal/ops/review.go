package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// Everything the owner may review or answer is one stack: dossier items,
// staged writes (items too), and open vault questions. Each takes the same
// actions: approve or answer (with edits), decline (a note is required),
// skip (an optional note), and ask agent (a note, required, and refs). Ask
// agent sends the item back: it waits as sent_back, para queues agent work
// linked to it, and the agent answers by revising the item (a dossier item)
// or by answering with sources (a vault question), which brings it back.
// Outward items also take done by me: the owner did it, so the item is done
// with no agent claim.

// askRefs checks the refs of an ask: each names a document in the vault.
func askRefs(ctx context.Context, d Deps, refs []string) ([]string, error) {
	var out []string
	for _, r := range refs {
		id := normID(r)
		if id == "" {
			continue
		}
		if _, err := d.Store.Get(ctx, id); err != nil {
			return nil, linkErr("refs", id, err)
		}
		if !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out, nil
}

// askInstruction is the agent work an ask queues: what the owner wants, in
// the owner's words, with the refs.
func askInstruction(what, how, note string, refs []string) string {
	body := "The owner sent " + what + " back to you with a note.\n\nRead the note and the refs. " + how +
		" Gather context with your own tools; stay read-only outside para. Every claim cites a source (a URL or a document id, with a quote): a guess is not a source. " +
		"If the note says the item is a duplicate or already handled, say so with the ref."
	body = appendSection(body, "Ask", note)
	if len(refs) > 0 {
		body = appendSection(body, "Refs", "- "+strings.Join(wikiList(refs), "\n- "))
	}
	return body
}

func wikiList(ids []string) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = "[[" + id + "]]"
	}
	return out
}

// sendBackItem sends one dossier item back to its agent: the answer keeps the
// note and refs (decision ask), the item waits as sent_back, and agent work is
// queued, linked to the dossier and the refs.
func sendBackItem(ctx context.Context, d Deps, prop, item, note string, refs []string) (Decision, string, error) {
	note = strings.TrimSpace(note)
	if note == "" {
		return Decision{}, "", errors.New("ask agent needs a note: what the agent should do")
	}
	refs, err := askRefs(ctx, d, refs)
	if err != nil {
		return Decision{}, "", err
	}
	var title string
	p, err := changeProposal(ctx, d, prop, func(p *proposal) error {
		it, err := p.item(item)
		if err != nil {
			return err
		}
		if !answerable(it.State) {
			return fmt.Errorf("item %s is %s: it is decided", it.ID, it.State)
		}
		a := model.ItemAnswer{Decision: model.AnswerAsk, Comment: note, Refs: refs, Via: d.Store.Info().Surface}
		it.Answer, it.State = &a, model.ItemSentBack
		title = firstOf(it.Title, it.What, it.Subject, string(it.Kind)+" "+it.ID)
		return nil
	})
	if err != nil {
		return Decision{}, "", err
	}
	it, _ := p.item(item)
	hash, _ := it.Hash()
	if err := record(ctx, d, "proposal_ask", p.doc.ID, item, model.ItemPending, model.ItemSentBack, hash); err != nil {
		return Decision{}, "", err
	}
	ask := askInstruction("item "+item+" of dossier "+p.doc.ID, "Then revise the item with proposal_revise (the dossier keeps the owner's other answers), or reply with proposal_reply when no change is right.", note, refs)
	r, err := RequestAdd.Run(ctx, d, RequestAddInput{Title: askPrefix + title, Ask: ask, Links: []string{p.doc.ID}, Context: refs})
	if err != nil {
		return Decision{}, "", fmt.Errorf("item %s is sent back, but no agent work was queued: %w", item, err)
	}
	return decisions([]model.ProposalItem{*it})[0], r.ID, nil
}

// skipItem leaves one item undecided, with the note.
func skipItem(ctx context.Context, d Deps, prop, item, note string) (Decision, error) {
	p, err := changeProposal(ctx, d, prop, func(p *proposal) error {
		it, err := p.item(item)
		if err != nil {
			return err
		}
		if !answerable(it.State) {
			return fmt.Errorf("item %s is %s: it is decided", it.ID, it.State)
		}
		a := model.ItemAnswer{Decision: model.AnswerSkip, Comment: strings.TrimSpace(note), Via: d.Store.Info().Surface}
		it.Answer, it.State = &a, model.ItemSkipped
		return nil
	})
	if err != nil {
		return Decision{}, err
	}
	it, _ := p.item(item)
	hash, _ := it.Hash()
	if err := record(ctx, d, "proposal_skip", p.doc.ID, item, model.ItemPending, model.ItemSkipped, hash); err != nil {
		return Decision{}, err
	}
	return decisions([]model.ProposalItem{*it})[0], nil
}

// Question review decisions (question_review).
const (
	ReviewAnswer  = "answer"
	ReviewDecline = "decline"
	ReviewSkip    = "skip"
	ReviewAsk     = "ask"
)

// QuestionReviewInput is the input of question_review: the owner's action on
// an open vault question from the review stack.
type QuestionReviewInput struct {
	ID       string `json:"id"`
	Decision string `json:"decision"` // answer, decline, skip, or ask
	// Choices are the options picked (more than one joins them); AddedOptions
	// are options the owner added; EditedOptions rewords options, by option.
	Choices       []string          `json:"choices,omitempty"`
	AddedOptions  []string          `json:"added_options,omitempty"`
	EditedOptions map[string]string `json:"edited_options,omitempty"`
	OwnWords      string            `json:"own_words,omitempty"`
	// Note is the owner's note: required to decline or ask, optional
	// otherwise (kept with the answer as its reason).
	Note string   `json:"note,omitempty"`
	Refs []string `json:"refs,omitempty"` // ask: vault ids the agent should read
}

// QuestionReviewOutput is the output of question_review.
type QuestionReviewOutput struct {
	ID      string               `json:"id"`
	Status  model.QuestionStatus `json:"status"`
	Review  string               `json:"review,omitempty"`  // skipped or sent_back
	Request string               `json:"request,omitempty"` // the agent work an ask queued
	Undo    string               `json:"undo,omitempty"`    // UndoReset when stack_undo can put the question back; empty when it cannot
	Rev     string               `json:"rev"`
}

// QuestionReview answers, declines, skips, or sends back an open vault
// question, as the owner.
var QuestionReview = definePerson("question_review", "The owner's action on an open vault question from the review stack: answer (choices, added or edited options, own words; note kept as the reason), decline (note required: the question is dropped), skip (optional note: it stays open, marked skipped), or ask (note required, optional refs: queues agent work and marks it sent back until the agent answers with sources). People only", func(ctx context.Context, d Deps, in QuestionReviewInput) (QuestionReviewOutput, error) {
	doc, q, err := getQuestion(ctx, d.Store, in.ID)
	if err != nil {
		return QuestionReviewOutput{}, err
	}
	if q.Status != model.QuestionOpen {
		return QuestionReviewOutput{}, fmt.Errorf("%s is already %s", doc.ID, q.Status)
	}
	note := strings.TrimSpace(in.Note)
	out := QuestionReviewOutput{ID: doc.ID}
	switch in.Decision {
	case ReviewAnswer:
		if len(in.EditedOptions) > 0 || len(in.AddedOptions) > 0 {
			opts := slices.Clone(q.Options)
			for i, o := range opts {
				if v := strings.Join(strings.Fields(in.EditedOptions[o]), " "); v != "" {
					opts[i] = v
				}
			}
			for _, a := range in.AddedOptions {
				if a = strings.Join(strings.Fields(a), " "); a != "" && !slices.Contains(opts, a) {
					opts = append(opts, a)
				}
			}
			q.Options = opts
			if q.Recommended != "" && !slices.Contains(opts, q.Recommended) {
				q.Recommended = ""
			}
			if doc, err = d.Store.Update(ctx, doc); err != nil {
				return out, err
			}
			if doc, q, err = getQuestion(ctx, d.Store, doc.ID); err != nil {
				return out, err
			}
		}
		choices := make([]string, 0, len(in.Choices))
		for _, c := range in.Choices {
			if v := strings.Join(strings.Fields(in.EditedOptions[c]), " "); v != "" {
				c = v
			}
			if c = strings.TrimSpace(c); c != "" {
				choices = append(choices, c)
			}
		}
		own := strings.TrimSpace(in.OwnWords)
		if len(choices) == 0 && own == "" {
			return out, errors.New("answer needs choices or own_words")
		}
		ans := QuestionAnswerInput{ID: doc.ID, Why: note}
		if len(choices) == 1 && own == "" {
			ans.Choice = choices[0]
		} else {
			ans.Text = strings.Join(append(choices, own), ". ")
			ans.Text = strings.Trim(ans.Text, ". ")
		}
		if q.AsKind() == model.KindDecision && ans.Why == "" {
			ans.Why = "The owner chose it on the review stack."
		}
		r, err := resolveQuestion(ctx, d, ans)
		if err != nil {
			return out, err
		}
		out.Status, out.Rev = r.Status, r.Rev
		return out, nil
	case ReviewDecline:
		if note == "" {
			return out, errors.New("decline needs a note: why the question is not worth answering")
		}
		r, err := QuestionDrop.Run(ctx, d, QuestionDropInput{ID: doc.ID, Reason: note})
		if err != nil {
			return out, err
		}
		out.Status, out.Rev = r.Status, r.Rev
		return out, nil
	case ReviewSkip:
		q.ReviewState, q.ReviewNote, q.ReviewRefs, q.ReviewRequest = string(model.ItemSkipped), note, nil, ""
	case ReviewAsk:
		if note == "" {
			return out, errors.New("ask needs a note: what the agent should do")
		}
		refs, err := askRefs(ctx, d, in.Refs)
		if err != nil {
			return out, err
		}
		ask := askInstruction("question "+doc.ID, "Then answer it with question_answer and sources, or tell the owner why you cannot.", note, refs)
		r, err := RequestAdd.Run(ctx, d, RequestAddInput{Title: askPrefix + doc.Title, Ask: ask, Links: []string{doc.ID}, Context: refs})
		if err != nil {
			return out, err
		}
		if doc, q, err = getQuestion(ctx, d.Store, doc.ID); err != nil {
			return out, err
		}
		q.ReviewState, q.ReviewNote, q.ReviewRequest = string(model.ItemSentBack), note, model.LinkTo(r.ID)
		q.ReviewRefs = nil
		for _, id := range refs {
			q.ReviewRefs = append(q.ReviewRefs, model.LinkTo(id))
		}
		out.Request = r.ID
	default:
		return out, fmt.Errorf("decision %q: want answer, decline, skip, or ask", in.Decision)
	}
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return out, err
	}
	out.Status, out.Review, out.Rev = q.Status, q.ReviewState, saved.Rev
	out.Undo = UndoReset
	return out, nil
})

// vaultQuestionRows are the open vault questions as stack rows: not put off
// (Later), every one proposed unless the owner sent it back. Reviewing a
// question is the owner's, so the rows have no actor.
func vaultQuestionRows(ctx context.Context, d Deps) ([]StackRow, error) {
	refs, err := d.Store.List(ctx, store.Filter{Type: model.TypeQuestion})
	if err != nil {
		return nil, err
	}
	deferred, err := deferredSet(ctx, d.Store, d.Store.Now().Format(dateLayout))
	if err != nil {
		return nil, err
	}
	var out []StackRow
	for _, r := range refs {
		doc, q, err := getQuestion(ctx, d.Store, r.ID)
		if err != nil {
			continue
		}
		if q.Status != model.QuestionOpen {
			continue
		}
		stack := StackProposed
		if deferred[doc.ID].any() {
			stack = StackLater
		}
		state := model.ItemPending
		switch q.ReviewState {
		case string(model.ItemSkipped):
			state = model.ItemSkipped
		case string(model.ItemSentBack):
			state = model.ItemSentBack
		}
		out = append(out, StackRow{Question: doc.ID, Item: doc.ID, Title: doc.Title, Kind: model.ItemQuestion, State: state, Stack: stack, DeferUntil: q.DeferUntil,
			Summary: doc.Title, Reason: q.ReviewNote, Refs: targets(q.ReviewRefs), Request: q.ReviewRequest.Target(), Proposed: q.ProposedBy != "",
			Tags: normTags(q.Tags)})
	}
	return out, nil
}

// ProposalDoneInput is the input of proposal_done: the owner did an outward
// item themself.
type ProposalDoneInput struct {
	ID      string `json:"id"`
	Item    string `json:"item"`
	URL     string `json:"url,omitempty"`     // where the result is
	Locator string `json:"locator,omitempty"` // where it is, when it has no URL
	// Text is what was sent (a message; default the draft as edited) or the
	// document after the edit (a doc edit of a tracked document: stored as its
	// next version).
	Text string `json:"text,omitempty"`
	Note string `json:"note,omitempty"`
	At   string `json:"at,omitempty"` // RFC 3339; default now
}

// ProposalDone marks an outward item done by the owner: no agent claim. A
// message that names an io draft marks the draft sent and files it as io_send
// does; a doc edit with text stores the next version; a receipt says the
// owner did it.
var ProposalDone = definePerson("proposal_done", "The owner did an outward item (message, action, doc edit) themself: it becomes done with exec.by the owner and no agent claim. Optional url or locator, text (what was sent, or the document after a doc edit), note, at. A message's io draft is marked sent and filed; a doc edit's text becomes the next version. People only", func(ctx context.Context, d Deps, in ProposalDoneInput) (ProposalExecutedOutput, error) {
	info := d.Store.Info()
	at := d.Store.Now().UTC().Format(time.RFC3339)
	if s := strings.TrimSpace(in.At); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return ProposalExecutedOutput{}, errors.New("at: want an RFC 3339 time")
		}
		at = t.UTC().Format(time.RFC3339)
	}
	url := strings.TrimSpace(in.URL)
	if url != "" {
		var err error
		if url, err = checkURL(url); err != nil {
			return ProposalExecutedOutput{}, err
		}
	}
	locator := firstOf(ioLine(in.Locator), "done by the owner")
	text := cleanText(in.Text)
	var from model.ItemState
	p, err := changeProposal(ctx, d, in.ID, func(p *proposal) error {
		it, err := p.item(in.Item)
		if err != nil {
			return err
		}
		if !it.Kind.Outward() {
			return fmt.Errorf("item %s is not a message, action, or doc edit", it.ID)
		}
		if !answerable(it.State) && it.State != model.ItemApproved && it.State != model.ItemClaimed {
			return fmt.Errorf("item %s is %s", it.ID, it.State)
		}
		from = it.State
		if text != "" && it.Kind != model.ItemDocEdit {
			it.Body, it.Edited = text, true
		}
		rec := newDecision(d, model.DecideApprove, "")
		it.Decided = &rec
		if h, err := it.Hash(); err == nil {
			it.ApprovedHash = h
		}
		it.Execution = &model.Execution{Result: ExecDone, URL: url, Locator: locator, By: info.Actor, At: at}
		it.State, it.Reason, it.ClaimedBy, it.LeaseUntil = model.ItemDone, "", "", ""
		return nil
	})
	if err != nil {
		return ProposalExecutedOutput{}, err
	}
	it, err := p.item(in.Item)
	if err != nil {
		return ProposalExecutedOutput{}, err
	}
	if err := record(ctx, d, "proposal_done", p.doc.ID, it.ID, from, it.State, it.ApprovedHash); err != nil {
		return ProposalExecutedOutput{}, err
	}
	sources := []string{}
	if url != "" {
		sources = append(sources, url)
	}
	receipt, err := createReceipt(ctx, d, receiptInput{op: "proposal_done", doc: p.doc.ID, title: p.doc.Title + " " + it.ID,
		meta:    WriteMeta{Why: firstOf(strings.TrimSpace(in.Note), "Done by the owner"), Sources: sources, Verify: firstOf(url, locator)},
		content: model.ReceiptContent{After: firstOf(url, locator)}})
	if err != nil {
		return ProposalExecutedOutput{}, err
	}
	p, err = changeProposal(ctx, d, in.ID, func(p *proposal) error {
		x, err := p.item(in.Item)
		if err == nil && x.Execution != nil {
			x.Execution.Receipt = receipt
		}
		return err
	})
	if err != nil {
		return ProposalExecutedOutput{}, err
	}
	out := ProposalExecutedOutput{ID: p.doc.ID, Item: it.ID, State: it.State, Receipt: receipt}
	if it.IO != "" {
		sent, err := sendIO(ctx, d, IOSendInput{ID: it.IO, URL: url, Locator: locator, At: at})
		if err != nil {
			return out, fmt.Errorf("item %s is done, but io %s was not marked sent: %w", it.ID, normID(it.IO), err)
		}
		out.IO = &sent
	}
	if it.Kind == model.ItemDocEdit && text != "" {
		if _, _, tracked := trackedTarget(ctx, d, it.Target); tracked {
			v, err := recordEditVersion(ctx, d, p.doc.ID, *it, text)
			if err != nil {
				return out, fmt.Errorf("item %s is done, but the new text was not stored: %w", it.ID, err)
			}
			out.Version = v
		}
	}
	if p, err = runReady(ctx, d, in.ID); err != nil {
		return out, err
	}
	out.Rev = p.doc.Rev
	return out, nil
})

// IODoneInput is the input of io_done: the owner sent an outbox draft.
type IODoneInput struct {
	ID      string `json:"id"`
	URL     string `json:"url,omitempty"`
	Locator string `json:"locator,omitempty"`
	Text    string `json:"text,omitempty"` // what was sent; default the draft
	At      string `json:"at,omitempty"`   // RFC 3339; default now
}

// IODone marks an outbox draft sent by the owner, as io_send does, from the
// web app.
var IODone = definePerson("io_done", "The owner sent an outbox draft themself: it is marked sent and filed as io_send does (expects none: an interaction; expects response: an Ask). Optional url or locator, text (what was sent), at. People only", func(ctx context.Context, d Deps, in IODoneInput) (IOSendOutput, error) {
	doc, m, err := getIO(ctx, d, in.ID)
	if err != nil {
		return IOSendOutput{}, err
	}
	if text := cleanText(in.Text); text != "" && text != doc.Body && m.Direction == model.IOOut && m.State.Open() {
		doc.Body = text
		doc.Title = ioTitle(m.Subject, doc.Body)
		if _, err := d.Store.Update(ctx, doc); err != nil {
			return IOSendOutput{}, err
		}
	}
	return sendIO(ctx, d, IOSendInput{ID: doc.ID, URL: in.URL, Locator: firstOf(ioLine(in.Locator), "sent by the owner"), At: in.At})
})
