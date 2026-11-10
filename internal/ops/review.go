package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/iodesystems/para/internal/model"
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

// withNote joins two notes of an output.
func withNote(a, b string) string {
	if a == "" {
		return b
	}
	return a + "; " + b
}

// Notes of an input that is accepted and ignored for one release (D10).
const (
	skipNote  = "skip is not stored: it is navigation in one web visit, and nothing changed"
	batchNote = "batch is ignored: a skip is not stored (batch goes in a later release)"
)

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
	// Batch is accepted and ignored (a skip is not stored); it goes in a later
	// release.
	Batch string `json:"batch,omitempty"`
}

// QuestionReviewOutput is the output of question_review.
type QuestionReviewOutput struct {
	ID      string               `json:"id"`
	Status  model.QuestionStatus `json:"status"`
	Review  string               `json:"review,omitempty"`  // skipped or sent_back
	Request string               `json:"request,omitempty"` // the agent work an ask queued
	Rev     string               `json:"rev"`
	Note    string               `json:"note,omitempty"` // an input that was ignored, and why
}

// QuestionReview answers, declines, skips, or sends back an open vault
// question, as the owner.
var QuestionReview = definePerson("question_review", "The owner's action on an open vault question from the review stack: answer (choices, added or edited options, own words; note kept as the reason), decline (note required: the question is dropped), skip (changes nothing: a skip is navigation in one web visit, stored nowhere), or ask (note required, optional refs: queues agent work and marks it sent back until the agent answers with sources). People only", func(ctx context.Context, d Deps, in QuestionReviewInput) (QuestionReviewOutput, error) {
	doc, q, err := getQuestion(ctx, d.Store, in.ID)
	if err != nil {
		return QuestionReviewOutput{}, err
	}
	if q.Status != model.QuestionOpen {
		return QuestionReviewOutput{}, fmt.Errorf("%s is already %s", doc.ID, q.Status)
	}
	note := strings.TrimSpace(in.Note)
	out := QuestionReviewOutput{ID: doc.ID}
	if in.Batch != "" {
		out.Note = batchNote
	}
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
		out.Status, out.Rev, out.Note = q.Status, doc.Rev, withNote(out.Note, skipNote)
		return out, nil
	case ReviewAsk:
		if note == "" {
			return out, errors.New("ask needs a note: what the agent should do")
		}
		refs, err := askRefs(ctx, d, in.Refs)
		if err != nil {
			return out, err
		}
		ask := askInstruction("question "+doc.ID, "Then answer it with question_answer and sources, or tell the owner why you cannot.", note, refs)
		r, err := RequestAdd.Run(ctx, d, RequestAddInput{Title: askPrefix + doc.Title, Ask: ask, Links: []string{doc.ID}, Context: refs, Kind: SentKind})
		if err != nil {
			return out, err
		}
		if doc, q, err = getQuestion(ctx, d.Store, doc.ID); err != nil {
			return out, err
		}
		q.ReviewState, q.ReviewNote, q.ReviewRequest = string(model.ItemSentBack), note, model.LinkTo(r.ID)
		q.ReviewAt = ""
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
	return out, nil
})

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

// IODoneInput is the input of io_done: the owner sent or applied an outbox
// draft, or a variation of it, themself. ID is the variation that went out.
type IODoneInput struct {
	ID      string `json:"id"`
	URL     string `json:"url,omitempty"`
	Locator string `json:"locator,omitempty"`
	Text    string `json:"text,omitempty"` // what was sent, when it differs: kept as the next revision
	At      string `json:"at,omitempty"`   // RFC 3339; default now
}

// IODone marks an outbox draft sent by the owner, as io_send does, from the
// web app. A final text that differs is saved as a revision first, and the
// other variations of its set close as superseded (sendIO). One receipt.
var IODone = withOneReceipt(definePerson("io_done", "The owner sent or applied an outbox draft, or a variation of it, themself: id is the variation that went out. It is marked sent and filed as io_send does (expects none: an interaction; expects response: an Ask), and the other variations of its set (variant_of) close as superseded. Optional url or locator, text (what was sent, when it differs from the draft: kept as its next revision), at. One receipt undoes it. People only", func(ctx context.Context, d Deps, in IODoneInput) (IOSendOutput, error) {
	doc, m, err := getIO(ctx, d, in.ID)
	if err != nil {
		return IOSendOutput{}, err
	}
	if text := cleanText(in.Text); text != "" && text != doc.Body && m.Direction == model.IOOut && m.State.Open() {
		prior, err := model.Render(doc)
		if err != nil {
			return IOSendOutput{}, err
		}
		if err := saveRevision(ctx, d, doc.ID, m, prior); err != nil {
			return IOSendOutput{}, err
		}
		doc.Body = text
		doc.Title = ioTitle(m.Subject, doc.Body)
		if _, err := d.Store.Update(ctx, doc); err != nil {
			return IOSendOutput{}, err
		}
	}
	return sendIO(ctx, d, IOSendInput{ID: doc.ID, URL: in.URL, Locator: firstOf(ioLine(in.Locator), "sent by the owner"), At: in.At})
}))
