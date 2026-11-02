package tui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/iodesystems/parasolve/internal/ops"
)

// An agent never answers a question: it proposes an answer with its sources,
// and the owner accepts, changes, or rejects it. These are "Answers to check".

// proposedQuestions are the open questions with an answer that waits for the
// owner, in the order of the question list.
func (m *Model) proposedQuestions() []ops.QuestionView {
	var out []ops.QuestionView
	for _, q := range m.snap.Questions {
		if q.HasProposal {
			out = append(out, q)
		}
	}
	return out
}

func (m *Model) answersToCheck() int { return len(m.proposedQuestions()) }

// proposedText is what the agent proposed: its choice and words.
func proposedText(q ops.QuestionView) string {
	return strings.TrimSpace(strings.TrimSpace(q.ProposedChoice) + " " + strings.TrimSpace(q.ProposedAnswer))
}

// rowsAnswersToCheck lists each proposed answer with its sources. A source
// that is a document follows like any link; a URL is shown to copy.
func (m *Model) rowsAnswersToCheck(w int) []row {
	qs := m.proposedQuestions()
	r := []row{head(fmt.Sprintf("Answers to check (%d)", len(qs)))}
	if len(qs) == 0 {
		return append(r, dim("  nothing to check"))
	}
	for _, q := range qs {
		x := linkKeep("doc:"+q.ID, "  "+todayTag("check", stWarn)+" ", titleOr(q.Title, q.ID),
			stDim.Render("  from "+q.ProposedBy), w)
		x.kind, x.id, x.proposed = "question", q.ID, true
		r = append(r, x)
		r = append(r, plain("      "+truncate("proposed: "+proposedText(q), max(w-8, 20))))
		if q.ProposedWhy != "" {
			r = append(r, dim("      "+truncate("why: "+q.ProposedWhy, max(w-8, 20))))
		}
		for _, s := range q.ProposedSources {
			quote := ""
			if s.Quote != "" {
				quote = stDim.Render(`  "` + s.Quote + `"`)
			}
			if strings.HasPrefix(s.Ref, "http") {
				r = append(r, plain("      source "+truncate(s.Ref, max(w-14, 20))+quote))
				continue
			}
			ref := s.Ref
			y := link("doc:"+ref, "      source ", titleOr(m.snap.name(ref), ref), quote, w)
			r = append(r, y)
		}
	}
	return r
}

// acceptAnswer asks for the text to record, prefilled with the agent's, then
// accepts. Changing the text keeps the sources.
func (m *Model) acceptAnswer(q ops.QuestionView) tea.Cmd {
	lines := []string{"  " + titleOr(q.Title, q.ID), "  proposed by " + q.ProposedBy}
	for _, s := range q.ProposedSources {
		ref := s.Ref
		if !strings.HasPrefix(ref, "http") {
			ref = titleOr(m.snap.name(ref), ref)
		}
		lines = append(lines, "  source "+ref)
	}
	steps := []promptStep{{label: "accept as (edit or enter): ", value: proposedText(q)}}
	return m.askForLines("Accept the answer", lines, steps, func(vals []string) tea.Cmd {
		env := m.env
		snap := m.snap
		in := ops.QuestionAcceptInput{ID: q.ID}
		if typed := strings.TrimSpace(vals[0]); typed != proposedText(q) {
			ai := answerInput(q, typed, "")
			in.Text, in.Choice = ai.Text, ai.Choice
		}
		return func() tea.Msg {
			o, err := ops.QuestionAccept.Run(context.Background(), env.Deps, in)
			if err != nil {
				return doneMsg{err: err}
			}
			text := fmt.Sprintf("Accepted the answer for %q.", titleOr(q.Title, "the question"))
			if len(o.Unblocked) > 0 {
				var names []string
				for _, id := range o.Unblocked {
					names = append(names, titleOr(snap.name(id), "a task"))
				}
				text += " Unblocked " + joinNamesAnd(names) + "."
			}
			return doneMsg{text: text, reload: true}
		}
	})
}

// rejectAnswer asks for the reason, which the agent reads.
func (m *Model) rejectAnswer() tea.Cmd {
	q, ok := m.cursorProposal()
	if !ok {
		m.setHint("Move to an answer to check to reject it.")
		return nil
	}
	steps := []promptStep{{label: "why not (the agent reads this): "}}
	return m.askForLines("Reject the answer", []string{"  " + titleOr(q.Title, q.ID)}, steps, func(vals []string) tea.Cmd {
		env := m.env
		reason := strings.TrimSpace(vals[0])
		return func() tea.Msg {
			if reason == "" {
				return doneMsg{err: fmt.Errorf("a reason is required")}
			}
			if _, err := ops.QuestionReject.Run(context.Background(), env.Deps, ops.QuestionRejectInput{ID: q.ID, Reason: reason}); err != nil {
				return doneMsg{err: err}
			}
			return doneMsg{text: fmt.Sprintf("Rejected the answer for %q. The agent can read why.", titleOr(q.Title, "the question")), reload: true}
		}
	})
}

// cursorProposal is the question under the cursor, when it has a proposal.
func (m *Model) cursorProposal() (ops.QuestionView, bool) {
	if len(m.stack) > 0 {
		return ops.QuestionView{}, false
	}
	r, ok := m.current()
	if !ok || r.kind != "question" || !r.proposed {
		return ops.QuestionView{}, false
	}
	for _, q := range m.snap.Questions {
		if q.ID == r.id && q.HasProposal {
			return q, true
		}
	}
	return ops.QuestionView{}, false
}
