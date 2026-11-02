package tui

import (
	"fmt"
	"strings"

	"github.com/iodesystems/parasolve/internal/model"
)

// Letters of the document actions. Link keys in a document avoid them, so a
// document's keys do not depend on anything but its text.
const docActionLetters = "zxycvd"

var (
	actCapture = Target{ID: "act:capture", Label: "Capture", Pref: 'c'}
	actVault   = Target{ID: "act:vault", Label: "Switch vault", Pref: 'v'}
	actZoom    = Target{ID: "act:zoom", Label: "Zoom", Pref: 'z'}
	actExport  = Target{ID: "act:export", Label: "Export", Pref: 'x'}
	actCopy    = Target{ID: "act:copy", Label: "Copy", Pref: 'y'}
	actAccept  = Target{ID: "act:accept", Label: "Mark reviewed", Pref: 'm'}
	actReopen  = Target{ID: "act:reopen", Label: "Reopen", Pref: 'r'}
	actGraph   = Target{ID: "act:graph", Label: "Graph"}
	actAnswer  = Target{ID: "act:answer", Label: "Answer", Pref: 'a'}
	actReject  = Target{ID: "act:reject", Label: "Reject answer", Pref: 'e'}
	actMeta    = Target{ID: "act:meta", Label: "Details", Pref: 'd'}
	actAgenda  = Target{ID: "act:agenda", Label: "Agenda", Pref: 'n'}
)

// listing is one list screen drawn: its lines and the key map.
type listing struct {
	lines  []string
	keys   map[string]Key // target id -> key
	footer []Target       // actions shown in the footer
}

// bodyHeight is the lines available to the body.
func (m *Model) bodyHeight() int {
	if m.zoom && m.docPage() != nil {
		return max(m.h-1, 1) // the last row is the way out
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

// rows returns the rows and footer actions of the list screen showing. An item
// row says "agent looking" while open agent work looks for context for it.
func (m *Model) rows(w int) ([]row, []Target) {
	rs, acts := m.rowsRaw(w)
	for i, r := range rs {
		id := strings.TrimPrefix(r.target, "doc:")
		if r.draw == nil || id == r.target || r.kind == "event" || !m.snap.looking(id) {
			continue
		}
		draw := r.draw
		rs[i].draw = func(k KeyFn) string { return draw(k) + "  " + stGood.Render("agent looking") }
	}
	return rs, acts
}

func (m *Model) rowsRaw(w int) ([]row, []Target) {
	acts := []Target{actCapture, actVault}
	if p := m.top(); p != nil {
		switch p.kind {
		case pagePerson:
			return m.rowsPerson(p, w), acts
		case pageWho:
			return m.rowsWho(p, w), acts
		case pageResults:
			return m.rowsResults(p, w), append([]Target{actFind}, acts...)
		case pageGraph:
			return m.rowsGraph(p, w), acts
		case pageProposal:
			return m.rowsProposal(p, w), append(m.proposalActs(p, w), acts...)
		case pageText:
			return m.rowsText(p), append(m.textActs(p), acts...)
		case pageHistory:
			return m.rowsHistory(p, w), acts
		case pageAgenda:
			return m.rowsAgenda(w), acts
		case pageHealth:
			return m.rowsHealth(w), append([]Target{actDismiss}, acts...)
		}
		return nil, acts
	}
	switch m.tab {
	case tabReview:
		return m.rowsReview(w), append(append(append([]Target{actAccept, actReopen}, questionActs...), actFind), acts...)
	case tabWork:
		return m.rowsWork(w), append(append(append([]Target{actGraph}, questionActs...), actFind), acts...)
	case tabRFX:
		return m.rowsRFX(w), append([]Target{actFind}, acts...)
	case tabPeople:
		return m.rowsPeople(w), acts
	case tabPolicy:
		return m.rowsPolicy(w), acts
	}
	return m.rowsToday(w), append(append(append(append([]Target{}, questionActs...), actFind), actAgenda), acts...)
}

// listWidth is the width rows may use: the screen less the key gutter.
func (m *Model) listWidth() int { return max(m.w-m.gutter, 20) }

// drawList draws the visible rows and assigns keys to the actions and to the
// links of visible rows. A row whose label has no free letter shows its key in
// a dim column at the left; the column exists only while some visible row needs
// it, and then every row keeps clear of it.
func (m *Model) drawList(h int) listing {
	for pass := 0; ; pass++ {
		l, gw := m.drawListOnce(h)
		if gw == m.gutter || pass == 1 {
			return l
		}
		m.gutter = gw
	}
}

func (m *Model) drawListOnce(h int) (listing, int) {
	rows, acts := m.rows(m.listWidth())
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
	m.above, m.below = *off, len(rows)-end

	var links []Target
	measure := func(id, label string) string {
		links = append(links, Target{ID: id, Label: label})
		return label
	}
	for _, r := range vis {
		r.draw(measure)
	}
	// Hidden actions have keys but no footer entry, and Answer has its key
	// always so the letters of the links do not move with the cursor.
	keys := Assign(append(append([]Target{}, m.extraActs()...), acts...), links, "")
	// The footer shows the question actions that fit the row under the cursor.
	footer := m.shownActs(acts)
	inGutter := map[string]string{} // target id -> key code
	use := func(id, label string) string {
		if k, ok := keys[id]; ok {
			inGutter[id] = k.Code
		}
		return label
	}
	out := listing{keys: keys, footer: footer}
	body := make([]string, len(vis))
	for i, r := range vis {
		body[i] = r.draw(use)
	}
	// A key sits in the row's own indent, next to the text. Only when the
	// indent is too small does every row move right to make room.
	lead := func(s string) int { return len(s) - len(strings.TrimLeft(s, " ")) }
	gw := 0
	for i, r := range vis {
		if c := inGutter[r.target]; r.sel && c != "" {
			gw = max(gw, cells(c)+1-lead(body[i]))
		}
	}
	for i, r := range vis {
		line := strings.Repeat(" ", gw) + body[i]
		if c := inGutter[r.target]; r.sel && c != "" {
			at := gw + lead(body[i]) - cells(c) - 1
			line = line[:at] + stDim.Render(c) + line[at+cells(c):]
		}
		if r.sel && *off+i == *cur {
			line = selected(line, m.w)
		} else {
			line = truncateLine(line, m.w)
		}
		out.lines = append(out.lines, line)
	}
	return out, gw
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
	k := docKey{id: p.id, rev: rev, width: m.w, meta: m.showMeta, ov: p.ov}
	if rd, ok := m.cache[k]; ok && rev != "" {
		return rd
	}
	src := withOverview(docSource(p.doc), p.doc.Title, p.ov)
	reserved := docActionLetters
	if _, ok := p.doc.Meta.(*model.Question); ok {
		reserved += questionActLetters
	} else if findableID(p.id) {
		reserved += "f"
	}
	pre := PreRender(src, PreOpts{Width: m.w, Reserved: reserved, Meta: m.showMeta})
	rd := m.rend.render(pre, m.w)
	if len(m.cache) > 64 {
		m.cache = map[docKey]RenderedDoc{}
	}
	m.cache[k] = rd
	return rd
}

var docActions = []Target{actZoom, actExport, actCopy, actMeta, actCapture, actVault}

// docActionsFor are the actions of a document page: an opened question also
// has the question actions.
func (m *Model) docActionsFor(p *page) []Target {
	acts := docActions
	if _, ok := p.doc.Meta.(*model.Question); ok {
		acts = append(append([]Target{}, questionActs...), docActions...)
	}
	if findableID(p.id) {
		acts = append(append([]Target{}, actFind), acts...)
	}
	return acts
}

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
		all := m.docActionsFor(p)
		akeys := Assign(all, nil, "")
		for _, a := range all {
			keymap[akeys[a.ID].Code] = a.ID
		}
		footer = m.footerLine(m.shownActs(all), akeys)
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
		leave := "z"
		for c, id := range keymap {
			if id == actZoom.ID {
				leave = c
			}
		}
		hint := " " + keyed(leave) + stDim.Render(" leave zoom   j/k scroll   esc back   ? help")
		m.frame = strings.Join(body, "\n") + "\n" + truncateLine(hint, m.w)
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

// shortNames are the tab names for a narrow screen: three letters keep a name.
var shortNames = [numTabs]string{"Tdy", "Rev", "Wrk", "Ask", "Ppl", "Pol"}

// topLine is the vault name, the tabs, and the refresh clock. A narrow screen
// shortens the tab names before it drops them, then drops the vault name and
// the clock. Review carries the count of proposals that wait for a decision.
func (m *Model) topLine() string {
	badge := ""
	if n := len(m.openProposals()); n > 0 && m.loaded {
		badge = " " + stWarn.Render(fmt.Sprint(n))
	}
	build := func(short, withVault, brand bool) string {
		var tabs []string
		for i, n := range tabNames {
			name := n
			if short {
				name = shortNames[i]
			}
			t := keyed(fmt.Sprint(i+1)) + " " + name
			if i == m.tab {
				t = keyed(fmt.Sprint(i+1)) + " " + stActive.Render(name)
			}
			if i == tabReview {
				t += badge
			}
			tabs = append(tabs, t)
		}
		sep := "  "
		if short {
			sep = " "
		}
		left := " "
		if brand {
			left = stHead.Render(" para ")
		}
		if withVault {
			left += stDim.Render(m.env.Vault.ID) + "  "
		}
		return left + strings.Join(tabs, sep)
	}
	clock := ""
	if !m.refreshed.IsZero() {
		mark := "refreshed "
		if m.loading {
			mark = "refreshing "
		}
		clock = stDim.Render(mark + m.refreshed.Format("15:04:05"))
	}
	for _, f := range []struct{ short, vault, brand bool }{{false, true, true}, {true, true, true}, {true, false, true}, {true, false, false}} {
		left := build(f.short, f.vault, f.brand)
		if gap := m.w - cells(left) - cells(clock); gap >= 1 {
			return left + strings.Repeat(" ", gap) + clock
		}
		if cells(left) <= m.w {
			return left
		}
	}
	return truncateLine(build(true, false, false), m.w)
}

func (m *Model) statusLine() string {
	s := m.status
	if m.pending != "" {
		s = "key " + keyed(m.pending) + " ..."
	}
	cue := m.moreCue()
	room := m.w - cells(cue)
	if cue != "" {
		room--
	}
	var left string
	switch {
	case s == "":
		if hint := m.proposalHint(); hint != "" {
			left = stDim.Render(truncate(hint, room))
		} else if p := m.top(); p != nil {
			t := p.title
			if len(m.stack) > 1 && m.w >= 60 {
				t = fmt.Sprintf("%s  (%d deep)", t, len(m.stack))
			}
			left = stDim.Render(truncate(t, room))
		}
	case m.statErr:
		left = stBad.Render(truncate(s, room))
	case m.statHint:
		left = stDim.Render(truncate(s, room))
	default:
		left = stGood.Render(truncate(s, room))
	}
	if cue == "" {
		return left
	}
	return left + strings.Repeat(" ", max(m.w-cells(left)-cells(cue), 1)) + cue
}

// moreCue says that the list continues above or below the screen.
func (m *Model) moreCue() string {
	if m.docPage() != nil {
		return ""
	}
	switch {
	case m.above > 0 && m.below > 0:
		if m.w < 60 {
			return stDim.Render("↑↓ more")
		}
		return stDim.Render(fmt.Sprintf("↑ %d above  ↓ %d more", m.above, m.below))
	case m.below > 0:
		if m.w < 60 {
			return stDim.Render("↓ more")
		}
		return stDim.Render(fmt.Sprintf("↓ %d more", m.below))
	case m.above > 0:
		if m.w < 60 {
			return stDim.Render("↑ more")
		}
		return stDim.Render(fmt.Sprintf("↑ %d above", m.above))
	}
	return ""
}

// footerLine shows the actions with their keys, then the fixed keys. Whole items
// only: what does not fit is left out, from the right, and the help key stays.
func (m *Model) footerLine(acts []Target, keys map[string]Key) string {
	var parts []string
	for _, a := range acts {
		if k, ok := keys[a.ID]; ok {
			if a.Dim {
				parts = append(parts, stDim.Render(Mark(a.Label, k)))
			} else {
				parts = append(parts, Mark(a.Label, k))
			}
		}
	}
	tails := []string{"j/k move  enter open  / search  ? help  q quit", "enter open  ? help  q quit", "? help  q quit", "? help"}
	if p := m.top(); p != nil && p.kind == pageProposal {
		tails = []string{"enter item in full  ? more actions  esc back  q quit", "? more actions  esc back  q quit", "? more  esc back", "? more"}
	} else if m.top() != nil {
		tails = []string{"j/k move  enter open  esc back  ? help  q quit", "enter open  esc back  ? help  q quit", "esc back  ? help  q quit", "esc back  ? help", "? help"}
	}
	for n := len(parts); n >= 0; n-- {
		for _, tail := range tails {
			line := " " + strings.Join(parts[:n], "  ")
			if n > 0 {
				line += "   "
			}
			line += stDim.Render(tail)
			if cells(line) <= m.w {
				return line
			}
		}
	}
	return truncateLine(" "+stDim.Render("? help"), m.w)
}
