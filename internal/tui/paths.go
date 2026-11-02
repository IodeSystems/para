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

// Next moves on an Ask (AP). The page lists what was tried and what to try
// next; Take move adds one as a new target with a reason. Para sends nothing.
var (
	actPaths = Target{ID: "act:paths", Label: "Next moves", Pref: 'n'}
	actTake  = Target{ID: "act:take", Label: "Take move", Pref: 't'}
)

// pathsTarget is the Ask under the cursor or the opened Ask.
func (m *Model) pathsTarget() (string, bool) {
	if p := m.top(); p != nil && p.kind == pageDoc {
		_, ok := p.doc.Meta.(*model.RFX)
		return p.id, ok
	}
	r, ok := m.current()
	if !ok || !strings.HasPrefix(r.target, "doc:rfx-") {
		return "", false
	}
	return strings.TrimPrefix(r.target, "doc:"), true
}

// showPaths opens the path log and the next moves of an Ask.
func (m *Model) showPaths() tea.Cmd {
	id, ok := m.pathsTarget()
	if !ok {
		m.setHint("Move to an ask to see its next moves.")
		return nil
	}
	out, err := ops.RFXPaths.Run(context.Background(), m.env.Deps, ops.RFXPathsInput{ID: id})
	if err != nil {
		m.setHint(err.Error())
		return nil
	}
	m.push(page{kind: pageText, id: id, title: "Next moves", lines: pathLines(out), paths: &out})
	return nil
}

func pathLines(o ops.RFXPathsOutput) []string {
	name := func(id, n string) string {
		if n != "" {
			return n
		}
		return id
	}
	l := []string{o.Title}
	if o.Stalled {
		l = append(l, stWarn.Render("  Stalled: "+o.StalledWhy+"."))
	} else {
		l = append(l, "  Not stalled. The moves below are still open to you.")
	}
	l = append(l, "", "Tried")
	if len(o.Log) == 0 {
		l = append(l, "  nobody yet")
	}
	for _, e := range o.Log {
		line := fmt.Sprintf("  %s by %s, %s", name(e.To, e.ToName), e.Medium, strings.ReplaceAll(string(e.State), "_", " "))
		if e.Sent != "" {
			line += ", sent " + e.Sent
		}
		if e.Reason != "" {
			line += ", " + strings.ReplaceAll(string(e.Reason), "_", " ")
		}
		line += fmt.Sprintf("; asked %d times in all", e.Contacts)
		if e.Last != "" {
			line += ", last " + e.Last
		}
		l = append(l, line)
		if e.Why != "" {
			l = append(l, stDim.Render("      "+e.Why))
		}
	}
	l = append(l, "", "Next moves")
	for i, mv := range o.Moves {
		line := fmt.Sprintf("  %d. %s", i+1, mv.Label)
		if mv.Hold != "" {
			line += stWarn.Render("  held back: " + mv.Hold)
		} else if mv.Contacts > 0 {
			line += stDim.Render(fmt.Sprintf("  asked %d times, last %s", mv.Contacts, mv.Last))
		}
		l = append(l, line, stDim.Render("      "+mv.Why))
	}
	return l
}

// takeMove asks which move, the reason code, and the reason, then records it.
func (m *Model) takeMove() tea.Cmd {
	p := m.top()
	if p == nil || p.paths == nil {
		return nil
	}
	o := *p.paths
	lines := []string{"  " + o.Title, "  Para records the move and sends nothing."}
	steps := []promptStep{
		{label: "move number: "},
		{label: "reason (" + reasonCodes() + "): "},
		{label: "why, in one line: "},
		{label: "held back: type override to go on, or esc: ", optional: true,
			skip: func(v []string) bool { return heldBy(o, v[0]) == "" }},
	}
	return m.askForLines("Take move", lines, steps, func(v []string) tea.Cmd {
		env := m.env
		return func() tea.Msg {
			n, err := strconv.Atoi(strings.TrimSpace(v[0]))
			if err != nil || n < 1 || n > len(o.Moves) {
				return doneMsg{err: fmt.Errorf("move %q: want a number from 1 to %d", v[0], len(o.Moves))}
			}
			mv := o.Moves[n-1]
			if mv.To == "" || mv.Medium == "" {
				return doneMsg{err: fmt.Errorf("move %d is a thing to do yourself: %s", n, mv.Label)}
			}
			in := ops.RFXPathAddInput{ID: o.ID, To: mv.To, Medium: mv.Medium, Value: mv.Value,
				Reason: model.FrictionReason(strings.TrimSpace(v[1])), Why: v[2], Override: strings.TrimSpace(v[3]) == "override"}
			if _, err := ops.RFXPathAdd.Run(context.Background(), env.Deps, in); err != nil {
				return doneMsg{err: err}
			}
			return doneMsg{text: fmt.Sprintf("Added %s by %s to the ask. Send it yourself; para sends nothing.", mv.To, mv.Medium), reload: true}
		}
	})
}

func heldBy(o ops.RFXPathsOutput, num string) string {
	n, err := strconv.Atoi(strings.TrimSpace(num))
	if err != nil || n < 1 || n > len(o.Moves) {
		return ""
	}
	return o.Moves[n-1].Hold
}

func reasonCodes() string {
	out := make([]string, len(model.FrictionReasons))
	for i, r := range model.FrictionReasons {
		out[i] = string(r)
	}
	return strings.Join(out, ", ")
}
