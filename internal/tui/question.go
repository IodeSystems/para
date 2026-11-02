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
	if len(m.stack) > 0 || (m.tab != tabToday && m.tab != tabWork) {
		return nil
	}
	r, ok := m.current()
	if !ok || r.kind != "question" {
		m.setStatus("move to an open question to answer it", true)
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
	return m.askForLines("Answer "+q.ID, lines, steps, func(vals []string) tea.Cmd {
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
	return func() tea.Msg {
		o, err := ops.QuestionAnswer.Run(context.Background(), env.Deps, in)
		if err != nil {
			return doneMsg{err: err}
		}
		text := "answered " + o.ID
		if len(o.Unblocked) > 0 {
			text += ", unblocked " + strings.Join(o.Unblocked, " ")
		}
		return doneMsg{text: text, reload: true}
	}
}
