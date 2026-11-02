package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/iodesystems/parasolve/internal/model"
)

// An agent never answers a question for the owner. It proposes an answer with
// the sources it found, and a person accepts, changes, or rejects it. Until
// then the question is open and what waits on it stays blocked.

const (
	maxQuote        = 400
	rejectedHeading = "## Rejected answers"
)

// ErrNoSource is the refusal for an agent answer with no source.
var ErrNoSource = errors.New("an answer from an agent needs a source: cite at least one URL or vault document id in sources, or ask the owner (question_ask). If you cannot find the answer, say you could not find it")

// answerSources checks sources. When need is set (an agent, or an item an
// agent proposed) at least one is required. Each is an http(s) URL or the id of
// a document that exists in the vault.
func answerSources(ctx context.Context, d Deps, in []model.AnswerSource, need bool) ([]model.AnswerSource, error) {
	if len(in) == 0 && need {
		return nil, ErrNoSource
	}
	var out []model.AnswerSource
	for i, s := range in {
		ref := strings.TrimSpace(s.Ref)
		ref = strings.TrimSuffix(strings.TrimPrefix(ref, "[["), "]]")
		ref = strings.TrimSpace(ref)
		if ref == "" {
			return nil, fmt.Errorf("sources[%d]: ref is empty: give a URL or a vault document id", i)
		}
		if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
			u, err := checkURL(ref)
			if err != nil {
				return nil, fmt.Errorf("sources[%d]: %w", i, err)
			}
			ref = u
		} else {
			id := normID(ref)
			if _, err := d.Store.Get(ctx, id); err != nil {
				return nil, fmt.Errorf("sources[%d]: %s is not a URL and not a document in this vault: %w", i, ref, err)
			}
			ref = id
		}
		quote := strings.Join(strings.Fields(s.Quote), " ")
		if len([]rune(quote)) > maxQuote {
			return nil, fmt.Errorf("sources[%d]: quote is over %d characters: quote the shortest passage that supports the answer", i, maxQuote)
		}
		if err := lintText(fmt.Sprintf("sources[%d].quote", i), quote); err != nil {
			return nil, err
		}
		dup := false
		for _, o := range out {
			dup = dup || o.Ref == ref && o.Quote == quote
		}
		if !dup {
			out = append(out, model.AnswerSource{Ref: ref, Quote: quote})
		}
	}
	return out, nil
}

// replySources checks, before anything is written, the sources a reply needs
// when it answers the question of its request. An agent that gives none but a
// reply URL cites that URL.
func replySources(ctx context.Context, d Deps, m *model.RFX, in RFXReplyInput) ([]model.AnswerSource, error) {
	qid := m.Question.Target()
	if qid == "" || in.Outcome == ReplyPartial {
		return nil, nil
	}
	if _, q, err := getQuestion(ctx, d.Store, qid); err != nil || q.Status != model.QuestionOpen {
		return nil, nil
	}
	src := in.Sources
	if len(src) == 0 && strings.TrimSpace(in.URL) != "" {
		src = []model.AnswerSource{{Ref: in.URL}}
	}
	srcs, err := answerSources(ctx, d, src, agentSurface(d) || d.Store.Info().Surface == surfaceProposal)
	if err != nil {
		return nil, fmt.Errorf("the reply answers %s: %w", qid, err)
	}
	return srcs, nil
}

func decodeInput(input map[string]any, into any) error {
	b, err := json.Marshal(input)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	return dec.Decode(into)
}

// checkProposedAnswer checks a proposal item that names question_answer: it
// must carry sources that exist. Called at proposal_add and proposal_revise.
func checkProposedAnswer(ctx context.Context, d Deps, it model.ProposalItem) error {
	if it.Kind != model.ItemOp || it.Op != "question_answer" {
		return nil
	}
	var in QuestionAnswerInput
	if err := decodeInput(it.Input, &in); err != nil {
		return err
	}
	if _, err := answerSources(ctx, d, in.Sources, true); err != nil {
		return fmt.Errorf("question_answer: %w", err)
	}
	return nil
}

// checkProposer refuses a browser session of the key that proposed the answer.
func checkProposer(d Deps, q *model.Question) error {
	info := d.Store.Info()
	if info.Surface == "pwa" && info.Actor == q.ProposedBy {
		return fmt.Errorf("%w: %s proposed this answer", ErrPersonOnly, info.Actor)
	}
	return nil
}

// QuestionAcceptInput is the input of question_accept.
type QuestionAcceptInput struct {
	ID     string `json:"id"`
	Text   string `json:"text,omitempty"`   // change the proposed text; sources stay
	Choice string `json:"choice,omitempty"` // change the proposed choice
	Why    string `json:"why,omitempty"`    // change the proposed reason
}

// QuestionAccept accepts the proposed answer of a question: the answer is
// recorded with the agent as proposer and the person as answerer, and what
// waited on the question can start.
var QuestionAccept = definePerson("question_accept", "Accept an agent's proposed answer, optionally changing its text first; the sources stay. Answers the question and frees what waited on it. People only", func(ctx context.Context, d Deps, in QuestionAcceptInput) (QuestionAnswerOutput, error) {
	doc, q, err := getQuestion(ctx, d.Store, in.ID)
	if err != nil {
		return QuestionAnswerOutput{}, err
	}
	if !q.HasProposal() {
		return QuestionAnswerOutput{}, fmt.Errorf("%s has no proposed answer to accept", doc.ID)
	}
	if err := checkProposer(d, q); err != nil {
		return QuestionAnswerOutput{}, err
	}
	pick := QuestionAnswerInput{Choice: in.Choice, Text: in.Text, Why: in.Why}
	if strings.TrimSpace(pick.Choice) == "" && strings.TrimSpace(pick.Text) == "" {
		pick.Choice, pick.Text = q.ProposedChoice, q.ProposedAnswer
	} else if strings.TrimSpace(pick.Choice) == "" {
		pick.Choice = q.ProposedChoice
	} else if strings.TrimSpace(pick.Text) == "" {
		pick.Text = q.ProposedAnswer
	}
	if strings.TrimSpace(pick.Why) == "" {
		pick.Why = q.ProposedWhy
	}
	choice, text, why, err := answerParts(q, pick)
	if err != nil {
		return QuestionAnswerOutput{}, err
	}
	if err := lintText("text", text); err != nil {
		return QuestionAnswerOutput{}, err
	}
	r := answerRecord{choice: choice, text: text, why: why, sources: q.ProposedSources, by: d.Store.Info().Actor}
	q.RejectedReason, q.RejectedBy = "", ""
	return writeAnswer(ctx, d, doc, q, r)
})

// QuestionRejectInput is the input of question_reject.
type QuestionRejectInput struct {
	ID     string `json:"id"`
	Reason string `json:"reason"` // required; the agent reads it
}

// QuestionReject rejects the proposed answer. The question stays open, and the
// reason stays on it for the agent.
var QuestionReject = definePerson("question_reject", "Reject an agent's proposed answer, with a reason the agent can read. The question stays open. People only", func(ctx context.Context, d Deps, in QuestionRejectInput) (QuestionAnswerOutput, error) {
	doc, q, err := getQuestion(ctx, d.Store, in.ID)
	if err != nil {
		return QuestionAnswerOutput{}, err
	}
	if !q.HasProposal() {
		return QuestionAnswerOutput{}, fmt.Errorf("%s has no proposed answer to reject", doc.ID)
	}
	if err := checkProposer(d, q); err != nil {
		return QuestionAnswerOutput{}, err
	}
	reason, err := requireReason(strings.Join(strings.Fields(in.Reason), " "))
	if err != nil {
		return QuestionAnswerOutput{}, err
	}
	if err := lintText("reason", reason); err != nil {
		return QuestionAnswerOutput{}, err
	}
	by := d.Store.Info().Actor
	line := fmt.Sprintf("- %s: %s rejected the answer proposed by %s: %s", d.Store.Now().Format(dateLayout), by, q.ProposedBy, reason)
	doc.Body = appendToSection(doc.Body, rejectedHeading, line)
	q.RejectedReason, q.RejectedBy = reason, by
	q.ProposedAnswer, q.ProposedChoice, q.ProposedBy, q.ProposedSources, q.ProposedWhy, q.Proposed = "", "", "", nil, "", ""
	return saveQuestion(ctx, d, doc, q)
})

// QuestionDependsInput is the input of question_depends_add and _remove.
type QuestionDependsInput struct {
	ID        string   `json:"id"`
	DependsOn []string `json:"depends_on"` // task, rfx, or question ids
}

// QuestionDependsOutput is the output of question_depends_add and _remove.
type QuestionDependsOutput struct {
	ID        string   `json:"id"`
	DependsOn []string `json:"depends_on"` // all dependencies after the change
	Rev       string   `json:"rev"`
}

func openQuestionDoc(ctx context.Context, d Deps, id string) (model.Document, *model.Question, error) {
	doc, q, err := getQuestion(ctx, d.Store, id)
	if err != nil {
		return doc, q, err
	}
	if q.Status != model.QuestionOpen {
		return doc, q, fmt.Errorf("%s is already %s", doc.ID, q.Status)
	}
	return doc, q, nil
}

// QuestionDependsAdd makes an open question wait on tasks, requests, or other
// questions. A cycle is refused, as for tasks.
var QuestionDependsAdd = define("question_depends_add", "Make an open question wait on tasks, rfx, or other questions; a cycle is refused", func(ctx context.Context, d Deps, in QuestionDependsInput) (QuestionDependsOutput, error) {
	doc, q, err := openQuestionDoc(ctx, d, in.ID)
	if err != nil {
		return QuestionDependsOutput{}, err
	}
	if len(in.DependsOn) == 0 {
		return QuestionDependsOutput{}, errors.New("depends_on is empty")
	}
	for _, raw := range in.DependsOn {
		l, err := dependencyLink(ctx, d.Store, raw)
		if err != nil {
			return QuestionDependsOutput{}, err
		}
		if slices.Contains(q.DependsOn, l) {
			continue
		}
		if l.Target() == doc.ID {
			return QuestionDependsOutput{}, fmt.Errorf("%s cannot depend on itself", doc.ID)
		}
		back, err := dependencyPath(ctx, d.Store, l.Target(), doc.ID)
		if err != nil {
			return QuestionDependsOutput{}, err
		}
		if back != nil {
			return QuestionDependsOutput{}, fmt.Errorf("%s -> %s would make a dependency cycle: %s -> %s",
				doc.ID, l.Target(), doc.ID, strings.Join(back, " -> "))
		}
		q.DependsOn = append(q.DependsOn, l)
	}
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return QuestionDependsOutput{}, err
	}
	return QuestionDependsOutput{ID: doc.ID, DependsOn: targets(q.DependsOn), Rev: saved.Rev}, nil
})

// QuestionDependsRemove removes dependencies from a question.
var QuestionDependsRemove = define("question_depends_remove", "Stop a question waiting on other work", func(ctx context.Context, d Deps, in QuestionDependsInput) (QuestionDependsOutput, error) {
	doc, q, err := getQuestion(ctx, d.Store, in.ID)
	if err != nil {
		return QuestionDependsOutput{}, err
	}
	if len(in.DependsOn) == 0 {
		return QuestionDependsOutput{}, errors.New("depends_on is empty")
	}
	for _, raw := range in.DependsOn {
		l := model.LinkTo(normID(raw))
		if !slices.Contains(q.DependsOn, l) {
			return QuestionDependsOutput{}, fmt.Errorf("%s does not depend on %s", doc.ID, normID(raw))
		}
		q.DependsOn = slices.DeleteFunc(q.DependsOn, func(x model.Link) bool { return x == l })
	}
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return QuestionDependsOutput{}, err
	}
	return QuestionDependsOutput{ID: doc.ID, DependsOn: targets(q.DependsOn), Rev: saved.Rev}, nil
})

// QuestionEditInput is the input of question_edit. An empty field is left as
// it is; Clear* removes the value.
type QuestionEditInput struct {
	ID               string   `json:"id"`
	Title            string   `json:"title,omitempty"`
	Options          []string `json:"options,omitempty"`           // replaces the options
	ClearOptions     bool     `json:"clear_options,omitempty"`     // remove all options (and the recommendation)
	Recommended      string   `json:"recommended,omitempty"`       // one of the options
	ClearRecommended bool     `json:"clear_recommended,omitempty"` // remove the recommendation
}

// QuestionEditOutput is the output of question_edit.
type QuestionEditOutput struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Options     []string `json:"options"`
	Recommended string   `json:"recommended,omitempty"`
	Rev         string   `json:"rev"`
}

// QuestionEdit changes the title, options, and recommendation of an open
// question, so a converted question need not be dropped and made again.
var QuestionEdit = define("question_edit", "Change the title, options, or recommended option of an open question", func(ctx context.Context, d Deps, in QuestionEditInput) (QuestionEditOutput, error) {
	doc, q, err := openQuestionDoc(ctx, d, in.ID)
	if err != nil {
		return QuestionEditOutput{}, err
	}
	if t := strings.TrimSpace(in.Title); t != "" {
		title, err := oneLine("title", t)
		if err != nil {
			return QuestionEditOutput{}, err
		}
		doc.Title = title
	}
	options, rec := q.Options, q.Recommended
	switch {
	case in.ClearOptions && len(in.Options) > 0:
		return QuestionEditOutput{}, errors.New("options and clear_options conflict")
	case in.ClearOptions:
		options, rec = nil, ""
	case len(in.Options) > 0:
		options = in.Options
	}
	switch {
	case in.ClearRecommended:
		rec = ""
	case strings.TrimSpace(in.Recommended) != "":
		rec = in.Recommended
	}
	if !slices.Equal(options, q.Options) && rec == q.Recommended && !slices.Contains(options, rec) {
		rec = "" // the old recommendation went with the old options
	}
	opts, rec, err := checkOptions(q.Kind, options, rec)
	if err != nil {
		return QuestionEditOutput{}, err
	}
	if q.ProposedChoice != "" && !slices.Contains(opts, q.ProposedChoice) {
		return QuestionEditOutput{}, fmt.Errorf("the proposed answer picks %q, which is not in the new options: reject the proposal first", q.ProposedChoice)
	}
	q.Options, q.Recommended = opts, rec
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return QuestionEditOutput{}, err
	}
	return QuestionEditOutput{ID: doc.ID, Title: doc.Title, Options: append([]string{}, q.Options...), Recommended: q.Recommended, Rev: saved.Rev}, nil
})
