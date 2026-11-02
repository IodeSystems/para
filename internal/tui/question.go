package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/ops"
)

// answer handles "a" on Today, Work, Review, and an opened question: answer
// the open question through question_answer, as the tui surface. With options,
// the answer is the number of an option or free text. A decision also asks for
// its reason. Then up to three sources, each with an optional quote: a URL, a
// document id, or words such as a meeting. Sources are optional for a person.
// Dependents change at once because they read the question's state.
func (m *Model) answer() tea.Cmd {
	q, ok := m.targetQuestion("answer")
	if !ok {
		return nil
	}
	if q.HasProposal {
		return m.acceptAnswer(q)
	}
	label, value := "answer: ", ""
	var lines []string
	if len(q.Options) > 0 {
		label = "answer (number, or your own words): "
		lines = append(lines, "  "+titleOr(q.Title, q.ID))
		if q.Context != "" {
			lines = append(lines, wrapText("  "+q.Context, max(m.w-6, 20))...)
		}
		for i, o := range q.Options {
			rec := ""
			if o == q.Recommended {
				rec = stGood.Render("  recommended")
				value = strconv.Itoa(i + 1)
			}
			lines = append(lines, fmt.Sprintf("  %d  %s%s", i+1, o, rec))
			lines = append(lines, optionCard(q, o, max(m.w-10, 20))...)
		}
	}
	steps := []promptStep{
		{label: label, value: value},
		{label: "why: ", skip: func([]string) bool { return q.Kind != model.KindDecision }},
	}
	steps = append(steps, sourceSteps(2)...)
	return m.askForLines("Answer: "+titleOr(q.Title, q.ID), lines, steps, func(vals []string) tea.Cmd {
		return m.runAnswer(q, vals[0], vals[1], sourcesFrom(vals[2:]))
	})
}

// sourceSteps are three optional (source, quote) pairs. A pair is asked only
// when the pair before it was given. first is the index in the chain of the
// first source step.
func sourceSteps(first int) []promptStep {
	var steps []promptStep
	for i := range 3 {
		at := first + 2*i
		label := "source (a URL, a document id, or words such as a meeting; enter for none): "
		if i > 0 {
			label = "another source (enter to finish): "
		}
		var skipSrc func([]string) bool
		if i > 0 {
			skipSrc = func(v []string) bool { return v[at-2] == "" }
		}
		steps = append(steps,
			promptStep{label: label, optional: true, skip: skipSrc},
			promptStep{label: "quote from it (optional): ", optional: true, skip: func(v []string) bool { return v[at] == "" }})
	}
	return steps
}

// sourcesFrom reads the (source, quote) pairs that sourceSteps collected.
func sourcesFrom(vals []string) []model.AnswerSource {
	var out []model.AnswerSource
	for i := 0; i+1 < len(vals); i += 2 {
		if r := strings.TrimSpace(vals[i]); r != "" {
			out = append(out, model.AnswerSource{Ref: r, Quote: strings.TrimSpace(vals[i+1])})
		}
	}
	return out
}

// optionCard is the weighing of one option, as lines under its name.
func optionCard(q ops.QuestionView, name string, w int) []string {
	var out []string
	for _, o := range q.OptionDetails {
		if o.Name != name {
			continue
		}
		add := func(head, text string) {
			for i, l := range wrapText(head+": "+text, w) {
				if i > 0 {
					l = "  " + l
				}
				out = append(out, "     "+l)
			}
		}
		claim := func(head string, c model.Claim) { add(head, c.Text+" ["+c.Source+"]") }
		for _, c := range o.Pros {
			claim("pro", c)
		}
		for _, c := range o.Cons {
			claim("con", c)
		}
		if o.Cost != nil {
			claim("cost", *o.Cost)
		}
		if o.Benefit != nil {
			claim("benefit", *o.Benefit)
		}
		for _, e := range o.Examples {
			add(e.Kind, e.Text+" ["+e.Source+"]")
		}
		if o.Confidence > 0 {
			add("confidence", fmt.Sprintf("%d%% %s", o.Confidence, o.ConfidenceBasis))
		}
		if o.Recommendation > 0 {
			add("recommended", fmt.Sprintf("%d%%", o.Recommendation))
		}
	}
	return out
}

// askForLines is askFor with lines of context above the input.
func (m *Model) askForLines(title string, lines []string, steps []promptStep, done func([]string) tea.Cmd) tea.Cmd {
	cmd := m.askFor(title, steps, done)
	if m.ov.kind == ovPrompt {
		m.ov.prompt.lines = lines
	}
	return cmd
}

// answerInput turns what was typed into the op input: the number or the text
// of an option is a choice, anything else is the answer in words.
func answerInput(q ops.QuestionView, typed, why string) ops.QuestionAnswerInput {
	in := ops.QuestionAnswerInput{ID: q.ID, Why: why}
	typed = strings.TrimSpace(typed)
	if n, err := strconv.Atoi(typed); err == nil && n >= 1 && n <= len(q.Options) {
		in.Choice = q.Options[n-1]
		return in
	}
	for _, o := range q.Options {
		if strings.EqualFold(o, typed) {
			in.Choice = o
			return in
		}
	}
	in.Text = typed
	return in
}

func (m *Model) runAnswer(q ops.QuestionView, typed, why string, sources []model.AnswerSource) tea.Cmd {
	env := m.env
	in := answerInput(q, typed, why)
	in.Sources = sources
	snap := m.snap
	return func() tea.Msg {
		o, err := ops.QuestionAnswer.Run(context.Background(), env.Deps, in)
		if err != nil {
			return doneMsg{err: err}
		}
		// One way to say done, as on the web: what was recorded, what it unblocked.
		said := in.Choice
		if said == "" {
			said = in.Text
		}
		text := fmt.Sprintf("Recorded %q for %q.", said, titleOr(q.Title, "the question"))
		if len(o.Unblocked) == 0 {
			text += " Nothing else was waiting only on this."
		} else {
			var names []string
			for _, id := range o.Unblocked {
				names = append(names, titleOr(snap.name(id), "a task"))
			}
			text += " Unblocked " + joinNamesAnd(names) + "."
		}
		return doneMsg{text: text, reload: true}
	}
}

// rewordQuestion changes the title and the context of an open question through
// question_edit. It is the question's edit: e is Reject answer on a question.
func (m *Model) rewordQuestion() tea.Cmd {
	q, ok := m.targetQuestion("reword")
	if !ok {
		return nil
	}
	if q.Status != model.QuestionOpen {
		m.setHint("Only an open question can be reworded.")
		return nil
	}
	steps := []promptStep{
		{label: "title: ", value: q.Title},
		{label: "context, what is needed to decide (empty keeps it): ", value: strings.ReplaceAll(q.Context, "\n", " "), optional: true},
	}
	return m.askForLines("Reword", []string{"  Options and the recommendation stay as they are."}, steps, func(v []string) tea.Cmd {
		env := m.env
		return func() tea.Msg {
			in := ops.QuestionEditInput{ID: q.ID}
			if t := strings.TrimSpace(v[0]); t != q.Title {
				in.Title = t
			}
			if c := strings.TrimSpace(v[1]); c != strings.TrimSpace(strings.ReplaceAll(q.Context, "\n", " ")) {
				in.Context = c
			}
			if in.Title == "" && in.Context == "" {
				return doneMsg{text: "Nothing changed."}
			}
			if _, err := ops.QuestionEdit.Run(context.Background(), env.Deps, in); err != nil {
				return doneMsg{err: err}
			}
			return doneMsg{text: "Reworded " + titleOr(in.Title, q.Title) + ".", reload: true}
		}
	})
}
