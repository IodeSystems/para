package tui

import (
	"context"
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/iodesystems/parasolve/internal/ops"
)

// The Health page lists the findings of `para health` by goal and lets the
// owner dismiss one with a reason. It is a page above the tabs, opened from
// the Health line on Today.
var actDismiss = Target{ID: "act:dismiss", Label: "Dismiss", Pref: 'd'}

func (m *Model) rowsHealth(w int) []row {
	h := m.snap.Health
	r := []row{head(fmt.Sprintf("Health: %s, %d dismissed", plural(h.Count, "finding"), h.Dismissed))}
	if len(h.Groups) == 0 {
		return append(r, dim("  Nothing to flag."))
	}
	for _, g := range h.Groups {
		name := g.Title
		if g.Due != "" {
			name += "  due " + dateWords(g.Due, m.snap.Loaded)
		}
		r = append(r, head(name))
		for _, f := range g.Findings {
			x := link("doc:"+f.ID, "  "+stWarn.Render(pad(f.Kind, 20))+" ", titleOr(f.Title, f.ID), stDim.Render("  "+f.Message), w)
			x.kind, x.id, x.hkind = "health", f.ID, f.Kind
			r = append(r, x)
		}
	}
	return r
}

// dismissHealth asks why, then dismisses the finding under the cursor.
func (m *Model) dismissHealth() tea.Cmd {
	r, ok := m.current()
	if p := m.top(); p == nil || p.kind != pageHealth || !ok || r.kind != "health" {
		m.setHint("Move to a finding to dismiss it.")
		return nil
	}
	id, kind := r.id, r.hkind
	steps := []promptStep{{label: "why it can stay as it is: "}}
	return m.askForLines("Dismiss "+kind+": "+id, []string{"  It stays out of health until you bring it back."}, steps, func(v []string) tea.Cmd {
		env := m.env
		return func() tea.Msg {
			if _, err := ops.HealthDismiss.Run(context.Background(), env.Deps, ops.HealthDismissInput{ID: id, Kind: kind, Reason: v[0]}); err != nil {
				return doneMsg{err: err}
			}
			return doneMsg{text: "Dismissed " + kind + ".", reload: true}
		}
	})
}
