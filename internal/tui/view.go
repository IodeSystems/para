package tui

import (
	"fmt"
	"strings"
	"unicode"
)

// Letters of the document actions. Link keys in a document avoid them, so a
// document's keys do not depend on anything but its text.
const docActionLetters = "zxycv"

var (
	actCapture = Target{ID: "act:capture", Label: "Capture", Pref: 'c'}
	actVault   = Target{ID: "act:vault", Label: "Vault", Pref: 'v'}
	actZoom    = Target{ID: "act:zoom", Label: "Zoom", Pref: 'z'}
	actExport  = Target{ID: "act:export", Label: "Export", Pref: 'x'}
	actCopy    = Target{ID: "act:copy", Label: "Copy", Pref: 'y'}
	actAccept  = Target{ID: "act:accept", Label: "Accept", Pref: 'a'}
	actReopen  = Target{ID: "act:reopen", Label: "Reopen", Pref: 'r'}
	actGraph   = Target{ID: "act:graph", Label: "Graph"}
)

// markFor shows a key on a label. The label may differ from the one the key
// was chosen on (the same target twice), so the letter is found again.
func markFor(label string, k Key) string {
	lab := []rune(label)
	if k.At >= 0 && k.At < len(lab) && strings.EqualFold(string(lab[k.At]), k.Code) {
		return Mark(label, k)
	}
	if len(k.Code) == 1 {
		for i, r := range lab {
			if unicode.ToLower(r) == rune(k.Code[0]) {
				return Mark(label, Key{Code: k.Code, At: i})
			}
		}
	}
	return Mark(label, Key{Code: k.Code, At: -1})
}

// listing is one list screen drawn: its lines and the key map.
type listing struct {
	lines  []string
	keys   map[string]Key // target id -> key
	footer []Target       // actions shown in the footer
}

// bodyHeight is the lines available to the body.
func (m *Model) bodyHeight() int {
	if m.zoom && m.docPage() != nil {
		return m.h
	}
	return max(m.h-3, 1)
}

func (m *Model) docPage() *page {
	if p := m.top(); p != nil && p.kind == pageDoc {
		return p
	}
	return nil
}

// listState is the cursor of the screen showing.
func (m *Model) listState() (cursor, offset *int) {
	if p := m.top(); p != nil {
		return &p.cursor, &p.offset
	}
	return &m.tabs[m.tab].cursor, &m.tabs[m.tab].offset
}

// rows returns the rows and footer actions of the list screen showing.
func (m *Model) rows(w int) ([]row, []Target) {
	acts := []Target{actCapture, actVault}
	if p := m.top(); p != nil {
		switch p.kind {
		case pagePerson:
			return m.rowsPerson(p, w), acts
		case pageWho:
			return m.rowsWho(p, w), acts
		case pageResults:
			return m.rowsResults(p, w), acts
		case pageGraph:
			return m.rowsGraph(p, w), acts
		}
		return nil, acts
	}
	switch m.tab {
	case tabReview:
		return m.rowsReview(w), append([]Target{actAccept, actReopen}, acts...)
	case tabWork:
		return m.rowsWork(w), append([]Target{actGraph}, acts...)
	case tabRFX:
		return m.rowsRFX(w), acts
	case tabPeople:
		return m.rowsPeople(w), acts
	case tabPolicy:
		return m.rowsPolicy(w), acts
	}
	return m.rowsToday(w), acts
}

// drawList draws the visible rows and assigns keys to the actions and to the
// links of visible rows.
func (m *Model) drawList(h int) listing {
	rows, acts := m.rows(m.w)
	cur, off := m.listState()
	// Keep the cursor on a selectable row, and in view.
	*cur = clampSel(rows, *cur)
	if *cur < *off {
		*off = *cur
	}
	if *cur >= *off+h {
		*off = *cur - h + 1
	}
	*off = max(0, min(*off, max(len(rows)-h, 0)))
	end := min(*off+h, len(rows))
	vis := rows[*off:end]

	var links []Target
	measure := func(id, label string) string {
		links = append(links, Target{ID: id, Label: label})
		return label
	}
	for _, r := range vis {
		r.draw(measure)
	}
	keys := Assign(acts, links, "")
	use := func(id, label string) string {
		if k, ok := keys[id]; ok {
			return markFor(label, k)
		}
		return label
	}
	out := listing{keys: keys, footer: acts}
	for i, r := range vis {
		line := r.draw(use)
		if r.sel && *off+i == *cur {
			line = selected(line, m.w)
		} else {
			line = truncateLine(line, m.w)
		}
		out.lines = append(out.lines, line)
	}
	return out
}

// selected paints a row with the selection background. A reset inside the
// line would end the background, so the background is set again after each.
func selected(line string, w int) string {
	line = truncateLine(line, w)
	line = strings.ReplaceAll(line, "\x1b[0m", "\x1b[0m"+selBG)
	line = strings.ReplaceAll(line, "\x1b[m", "\x1b[m"+selBG)
	return selBG + pad(line, w) + "\x1b[0m"
}

func truncateLine(line string, w int) string {
	if cells(line) <= w {
		return line
	}
	return truncate(line, w)
}

func clampSel(rows []row, cur int) int {
	if len(rows) == 0 {
		return 0
	}
	cur = max(0, min(cur, len(rows)-1))
	if rows[cur].sel {
		return cur
	}
	for i := cur; i < len(rows); i++ {
		if rows[i].sel {
			return i
		}
	}
	for i := cur; i >= 0; i-- {
		if rows[i].sel {
			return i
		}
	}
	return cur
}

// renderDoc renders (or fetches from cache) the document of a page.
func (m *Model) renderDoc(p *page) RenderedDoc {
	rev := p.doc.Rev
	k := docKey{id: p.id, rev: rev, width: m.w}
	if rd, ok := m.cache[k]; ok && rev != "" {
		return rd
	}
	pre := PreRender(docSource(p.doc), PreOpts{Width: m.w, Reserved: docActionLetters})
	rd := m.rend.render(pre, m.w)
	if len(m.cache) > 64 {
		m.cache = map[docKey]RenderedDoc{}
	}
	m.cache[k] = rd
	return rd
}

var docActions = []Target{actZoom, actExport, actCopy, actCapture, actVault}

// relayout rebuilds the frame and the key map from the current state.
func (m *Model) relayout() {
	if m.w < 24 || m.h < 6 {
		m.frame = "terminal too small"
		m.keymap = map[string]string{}
		return
	}
	var body []string
	var footer string
	keymap := map[string]string{}
	h := m.bodyHeight()

	if p := m.docPage(); p != nil {
		rd := m.renderDoc(p)
		p.scroll = max(0, min(p.scroll, max(len(rd.Lines)-h, 0)))
		end := min(p.scroll+h, len(rd.Lines))
		body = append(body, rd.Lines[p.scroll:end]...)
		for id, k := range rd.Keys {
			keymap[k.Code] = id
		}
		akeys := Assign(docActions, nil, "")
		for _, a := range docActions {
			keymap[akeys[a.ID].Code] = a.ID
		}
		footer = m.footerLine(docActions, akeys)
		if len(rd.Lines) > h {
			footer += stDim.Render(fmt.Sprintf("  %d%%", 100*min(p.scroll+h, len(rd.Lines))/len(rd.Lines)))
		}
	} else {
		l := m.drawList(h)
		body = l.lines
		for id, k := range l.keys {
			keymap[k.Code] = id
		}
		footer = m.footerLine(l.footer, l.keys)
	}
	for len(body) < h {
		body = append(body, "")
	}
	for i, t := range []string{"1", "2", "3", "4", "5", "6"} {
		keymap[t] = fmt.Sprintf("tab:%d", i)
	}
	if m.ov.kind != ovNone {
		body, keymap = m.drawOverlay(body, keymap, h)
	}
	m.keymap = keymap

	if m.zoom && m.docPage() != nil && m.ov.kind == ovNone {
		m.frame = strings.Join(body, "\n")
		return
	}
	var b strings.Builder
	b.WriteString(m.topLine())
	b.WriteByte('\n')
	b.WriteString(strings.Join(body, "\n"))
	b.WriteByte('\n')
	b.WriteString(m.statusLine())
	b.WriteByte('\n')
	b.WriteString(truncateLine(footer, m.w))
	m.frame = b.String()
}

// topLine is the vault name, the tabs, and the refresh clock. Narrow
// screens keep only the digits of inactive tabs, then drop the clock.
func (m *Model) topLine() string {
	build := func(short bool) string {
		var tabs []string
		for i, n := range tabNames {
			t := keyed(fmt.Sprint(i+1)) + " " + n
			switch {
			case i == m.tab:
				t = keyed(fmt.Sprint(i+1)) + " " + stActive.Render(n)
			case short:
				t = keyed(fmt.Sprint(i + 1))
			}
			tabs = append(tabs, t)
		}
		sep := "  "
		if short {
			sep = " "
		}
		return stHead.Render(" para ") + stDim.Render(m.env.Vault.ID) + "  " + strings.Join(tabs, sep)
	}
	clock := ""
	if !m.refreshed.IsZero() {
		mark := "refreshed "
		if m.loading {
			mark = "refreshing "
		}
		clock = stDim.Render(mark + m.refreshed.Format("15:04:05"))
	}
	for _, short := range []bool{false, true} {
		left := build(short)
		if gap := m.w - cells(left) - cells(clock); gap >= 1 {
			return left + strings.Repeat(" ", gap) + clock
		}
		if cells(left) <= m.w {
			return left
		}
	}
	return truncateLine(build(true), m.w)
}

func (m *Model) statusLine() string {
	s := m.status
	if m.pending != "" {
		s = "key " + keyed(m.pending) + " ..."
	}
	if s == "" {
		if p := m.top(); p != nil {
			s = stDim.Render(p.title)
			if len(m.stack) > 1 {
				s = stDim.Render(fmt.Sprintf("%s  (%d deep)", p.title, len(m.stack)))
			}
		}
		return truncateLine(s, m.w)
	}
	if m.statErr {
		return truncateLine(stBad.Render(s), m.w)
	}
	return truncateLine(stGood.Render(s), m.w)
}

// footerLine shows the actions with their keys, then the fixed keys.
func (m *Model) footerLine(acts []Target, keys map[string]Key) string {
	var parts []string
	for _, a := range acts {
		if k, ok := keys[a.ID]; ok {
			parts = append(parts, Mark(a.Label, k))
		}
	}
	tail := "j/k move  enter open  esc back  ? help  q quit"
	if m.top() == nil {
		tail = "j/k move  enter open  / search  ? help  q quit"
	}
	return " " + strings.Join(parts, "  ") + "   " + stDim.Render(tail)
}
