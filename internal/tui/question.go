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

// answer handles "a" on Today and Work: answer the open question under the
// cursor through question_answer, as the tui surface. With options, the answer
// is the number of an option or free text. A decision also asks for its reason.
// Dependents change at once because they read the question's state.
func (m *Model) answer() tea.Cmd {
	if len(m.stack) > 0 || (m.tab != tabToday && m.tab != tabWork && m.tab != tabReview) {
		return nil
	}
	r, ok := m.current()
	if !ok || r.kind != "question" {
		m.setHint("Move to an open question to answer it.")
		return nil
	}
	var q ops.QuestionView
	for _, x := range m.snap.Questions {
		if x.ID == r.id {
			q = x
		}
	}
	if q.ID == "" {
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
		for i, o := range q.Options {
			rec := ""
			if o == q.Recommended {
				rec = stGood.Render("  recommended")
				value = strconv.Itoa(i + 1)
			}
			lines = append(lines, fmt.Sprintf("  %d  %s%s", i+1, o, rec))
		}
	}
	steps := []promptStep{
		{label: label, value: value},
		{label: "why: ", skip: func([]string) bool { return q.Kind != model.KindDecision }},
	}
	return m.askForLines("Answer: "+titleOr(q.Title, q.ID), lines, steps, func(vals []string) tea.Cmd {
		return m.runAnswer(q, vals[0], vals[1])
	})
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

func (m *Model) runAnswer(q ops.QuestionView, typed, why string) tea.Cmd {
	env := m.env
	in := answerInput(q, typed, why)
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
