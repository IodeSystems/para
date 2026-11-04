package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/iodesystems/para/internal/model"
)

// An agent answers a question with the sources it found. The owner's words in
// the chat are a source too (ref "owner in chat", with the quote). Answers an
// agent proposed before this rule stay readable on the question.

const (
	maxQuote        = 400
	rejectedHeading = "## Rejected answers"
	// OwnerInChat is the source ref for the owner's words in the chat. The
	// quote holds the words; para cannot see the chat, so it is self-reported.
	OwnerInChat = "owner in chat"
)

// ErrNoSource is the refusal for an agent answer with no source.
var ErrNoSource = errors.New("an answer from an agent needs a source: cite at least one URL or vault document id in sources, or the owner's words in the chat (ref \"owner in chat\" with the quote), or ask the owner (question_ask). If you cannot find the answer, say you could not find it")

// maxFreeSource is the longest free-text source a person may give.
const maxFreeSource = 200

// answerSources checks sources. When need is set (an agent, or an item an
// agent proposed) at least one is required. Each is an http(s) URL or the id of
// a document that exists in the vault. A person may also give free text, such
// as the name of a meeting.
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
		if need && strings.EqualFold(ref, OwnerInChat) {
			ref = OwnerInChat
			if strings.TrimSpace(s.Quote) == "" {
				return nil, fmt.Errorf("sources[%d]: %q needs the quote: the owner's words, verbatim", i, OwnerInChat)
			}
		} else if strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") {
			u, err := checkURL(ref)
			if err != nil {
				return nil, fmt.Errorf("sources[%d]: %w", i, err)
			}
			ref = u
		} else {
			id := normID(ref)
			_, err := d.Store.Get(ctx, id)
			switch {
			case err == nil:
				ref = id
			case !need && !agentSurface(d):
				// A person may cite free text, such as a meeting.
				ref = strings.Join(strings.Fields(ref), " ")
				if len([]rune(ref)) > maxFreeSource {
					return nil, fmt.Errorf("sources[%d]: the text is over %d characters", i, maxFreeSource)
				}
				if err := lintText(fmt.Sprintf("sources[%d].ref", i), ref); err != nil {
					return nil, err
				}
			default:
				return nil, fmt.Errorf("sources[%d]: %s is not a URL and not a document in this vault: %w", i, ref, err)
			}
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
	srcs, err := answerSources(ctx, d, src, agentSurface(d))
	if err != nil {
		return nil, fmt.Errorf("the reply answers %s: %w", qid, err)
	}
	return srcs, nil
}

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
var QuestionDependsAdd = defineSafe("question_depends_add", "Make an open question wait on tasks, rfx, or other questions; a cycle is refused", func(ctx context.Context, d Deps, in QuestionDependsInput) (QuestionDependsOutput, error) {
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
var QuestionDependsRemove = defineSafe("question_depends_remove", "Stop a question waiting on other work", func(ctx context.Context, d Deps, in QuestionDependsInput) (QuestionDependsOutput, error) {
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
	Context          string   `json:"context,omitempty"`           // replaces what is needed to decide
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
	q.Options, q.Recommended = opts, rec
	if c := strings.TrimSpace(in.Context); c != "" {
		q.Context = c
	}
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return QuestionEditOutput{}, err
	}
	return QuestionEditOutput{ID: doc.ID, Title: doc.Title, Options: append([]string{}, q.Options...), Recommended: q.Recommended, Rev: saved.Rev}, nil
})
