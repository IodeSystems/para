package tui

import (
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/iodesystems/parasolve/internal/model"
)

// onKey handles a key press. Global keys come first, then the link keys of
// the frame on screen.
func (m *Model) onKey(k tea.KeyPressMsg) tea.Cmd {
	s := k.String()
	if s == "ctrl+c" {
		return tea.Quit
	}
	if m.ov.kind != ovNone {
		return m.onOverlayKey(k)
	}
	if m.pending != "" {
		return m.onOverflowKey(s)
	}
	moves := slices.Contains([]string{"j", "k", "up", "down", "g", "G", "pgdown", "pgup", "space", " "}, s)
	if !m.statKeep || m.status == "" || !moves {
		m.status, m.statKeep = "", false
	}
	switch s {
	case "q":
		return tea.Quit
	case "esc":
		switch {
		case m.zoom:
			m.zoom = false
		case len(m.stack) > 0:
			m.stack = m.stack[:len(m.stack)-1]
		}
		return nil
	case "?":
		m.ov = overlay{kind: ovHelp}
		return nil
	case "/":
		m.ov = overlay{kind: ovSearch, input: newInput("search: ", ""), liveCur: -1}
		return m.ov.input.Focus()
	case "j", "down":
		m.move(1)
		return nil
	case "k", "up":
		m.move(-1)
		return nil
	case "g", "home":
		m.jump(false)
		return nil
	case "G", "end":
		m.jump(true)
		return nil
	case "pgdown", "ctrl+d", "space", " ":
		m.move(max(m.bodyHeight()-1, 1))
		return nil
	case "pgup", "ctrl+u":
		m.move(-max(m.bodyHeight()-1, 1))
		return nil
	case "enter":
		return m.activate()
	case "tab":
		m.switchTab((m.tab + 1) % numTabs)
		return nil
	case "shift+tab":
		m.switchTab((m.tab + numTabs - 1) % numTabs)
		return nil
	case overflowPrefix:
		m.pending = overflowPrefix
		return nil
	}
	if t, ok := m.keymap[s]; ok {
		return m.follow(t)
	}
	return nil
}

// onOverflowKey collects the two letters after ";".
func (m *Model) onOverflowKey(s string) tea.Cmd {
	if s == "esc" || len(s) != 1 || s[0] < 'a' || s[0] > 'z' {
		m.pending = ""
		return nil
	}
	m.pending += s
	if len(m.pending) < 3 {
		return nil
	}
	code := m.pending
	m.pending = ""
	if t, ok := m.keymap[code]; ok {
		return m.follow(t)
	}
	return nil
}

// move moves the cursor on a list, or scrolls a document.
func (m *Model) move(delta int) {
	if p := m.docPage(); p != nil {
		p.scroll = max(0, p.scroll+delta)
		return
	}
	rows, _ := m.rows(m.listWidth())
	cur, _ := m.listState()
	step := 1
	if delta < 0 {
		step = -1
	}
	for n := abs(delta); n > 0; n-- {
		next := *cur
		for {
			next += step
			if next < 0 || next >= len(rows) {
				next = -1
				break
			}
			if rows[next].sel {
				break
			}
		}
		if next < 0 {
			break
		}
		*cur = next
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func (m *Model) jump(end bool) {
	if p := m.docPage(); p != nil {
		if end {
			p.scroll = 1 << 30
		} else {
			p.scroll = 0
		}
		return
	}
	rows, _ := m.rows(m.listWidth())
	cur, off := m.listState()
	if !end {
		*cur, *off = 0, 0
		*cur = clampSel(rows, 0)
		return
	}
	*cur = clampSel(rows, len(rows)-1)
}

// activate follows the row under the cursor.
func (m *Model) activate() tea.Cmd {
	if m.docPage() != nil {
		return nil
	}
	rows, _ := m.rows(m.listWidth())
	cur, _ := m.listState()
	if *cur < 0 || *cur >= len(rows) || !rows[*cur].sel {
		return nil
	}
	return m.follow(rows[*cur].target)
}

func (m *Model) current() (row, bool) {
	rows, _ := m.rows(m.listWidth())
	cur, _ := m.listState()
	if *cur < 0 || *cur >= len(rows) {
		return row{}, false
	}
	return rows[*cur], true
}

// accept handles "a" on Review: accept the item, or move the position to the
// event under the cursor.
func (m *Model) accept() tea.Cmd {
	if m.tab != tabReview || len(m.stack) > 0 {
		return nil
	}
	r, ok := m.current()
	if !ok {
		return nil
	}
	switch r.kind {
	case "need":
		return m.acceptNeed(r.need)
	case "proposal":
		return m.loadProposal(r.id, false)
	case "event":
		next := m.pos.Accept(r.event.TS)
		if next == m.pos {
			return nil
		}
		// Events of other actors older than this one are accepted too: the
		// position is one time per machine.
		skipped := 0
		for _, g := range m.review().Groups {
			for _, e := range g.Events {
				if !e.TS.After(r.event.TS) && e.Actor != r.event.Actor {
					skipped++
				}
			}
		}
		m.pos = next
		msg := "position moved to " + r.event.TS.Local().Format("15:04:05")
		if skipped > 0 {
			msg += " (" + plural(skipped, "older event") + " of other actors included)"
		}
		m.setStatus(msg, false)
		return m.savePosition(next)
	}
	m.setStatus("move to an event or an item that needs you", true)
	return nil
}

// reopen handles "r" on Review: set a closed request back to open, or undo
// the last accept of the position.
func (m *Model) reopen() tea.Cmd {
	if m.tab != tabReview || len(m.stack) > 0 {
		return nil
	}
	if r, ok := m.current(); ok && r.id != "" {
		for _, x := range m.snap.RFX {
			if x.ID == r.id && x.Status != model.RFXOpen {
				return m.reopenRFX(r.id)
			}
		}
	}
	if m.pos.Previous.Equal(m.pos.At) {
		m.setStatus("nothing to reopen", true)
		return nil
	}
	m.pos = m.pos.Reopen()
	m.setStatus("position moved back to "+m.pos.At.Local().Format("15:04:05"), false)
	return m.savePosition(m.pos)
}

var _ = strings.TrimSpace
