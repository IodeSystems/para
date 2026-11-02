package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/iodesystems/parasolve/internal/export"
)

// onOverlayKey routes keys while an overlay is open.
func (m *Model) onOverlayKey(k tea.KeyPressMsg) tea.Cmd {
	s := k.String()
	switch m.ov.kind {
	case ovCapture, ovSearch:
		switch s {
		case "esc":
			m.ov = overlay{}
			return nil
		case "enter":
			text := strings.TrimSpace(m.ov.input.Value())
			kind := m.ov.kind
			m.ov = overlay{}
			if text == "" {
				return nil
			}
			if kind == ovCapture {
				return m.capture(text)
			}
			if m.tab == tabPeople && len(m.stack) == 0 {
				return m.loadWho(text)
			}
			return m.loadSearch(text)
		}
		var cmd tea.Cmd
		m.ov.input, cmd = m.ov.input.Update(k)
		return cmd
	case ovHelp, ovOutput:
		if s == "esc" || s == "q" || s == "enter" || s == "?" {
			m.ov = overlay{}
		}
		return nil
	case ovVault:
		if s == "esc" {
			m.ov = overlay{}
			return nil
		}
		if t, ok := m.keymap[s]; ok && strings.HasPrefix(t, "vault:") {
			m.ov = overlay{}
			return m.follow(t)
		}
		return nil
	case ovExport:
		return m.onExportKey(k)
	}
	return nil
}

func (m *Model) onExportKey(k tea.KeyPressMsg) tea.Cmd {
	s := k.String()
	e := &m.ov.export
	if e.stage == 2 {
		switch s {
		case "esc":
			e.stage = 1
			return nil
		case "enter":
			arg := strings.TrimSpace(m.ov.input.Value())
			if arg == "" {
				return nil
			}
			return m.finishExport(e.format, e.transport.Name+":"+arg)
		}
		var cmd tea.Cmd
		m.ov.input, cmd = m.ov.input.Update(k)
		return cmd
	}
	if s == "esc" {
		if e.stage == 1 {
			e.stage = 0
			return nil
		}
		m.ov = overlay{}
		return nil
	}
	if !e.loaded {
		return nil
	}
	t, ok := m.keymap[s]
	if !ok {
		return nil
	}
	name := strings.TrimPrefix(t, "opt:")
	if name == t {
		return nil
	}
	if e.stage == 0 {
		for _, it := range e.list.Formats {
			if it.Name == name && it.Available {
				e.format, e.stage = export.Format(name), 1
			}
		}
		return nil
	}
	for _, it := range e.list.Transports {
		if it.Name != name || !it.Available {
			continue
		}
		e.transport = it
		if it.Arg != "" { // file:<path>, ssh:<host>:<path>
			def := ""
			if it.Name == "file" {
				def = e.doc + "." + extFor(e.format)
			}
			m.ov.input = newInput(it.Arg+" ", def)
			e.stage = 2
			return m.ov.input.Focus()
		}
		return m.finishExport(e.format, it.Name)
	}
	return nil
}

func extFor(f export.Format) string {
	switch f {
	case export.MD:
		return "md"
	case export.HTML:
		return "html"
	case export.PDF:
		return "pdf"
	case export.DOCX:
		return "docx"
	}
	return "txt"
}

func (m *Model) finishExport(f export.Format, to string) tea.Cmd {
	doc := m.ov.export.doc
	m.lastFormat = f
	if !strings.HasPrefix(to, "file:") && !strings.HasPrefix(to, "ssh:") {
		m.lastTransport = to
	}
	m.ov = overlay{}
	return m.runExport(doc, f, to)
}

// drawOverlay paints the overlay over the body and adds its keys.
func (m *Model) drawOverlay(body []string, keymap map[string]string, h int) ([]string, map[string]string) {
	var lines []string
	switch m.ov.kind {
	case ovCapture, ovSearch:
		lines = []string{stHead.Render("enter saves, esc cancels"), m.ov.input.View()}
	case ovHelp:
		lines = helpLines()
	case ovOutput:
		lines = append(strings.Split(strings.TrimRight(m.ov.output, "\n"), "\n"), "", stDim.Render("esc closes"))
	case ovVault:
		keymap = map[string]string{}
		var targets []Target
		for _, id := range m.ov.vaults {
			targets = append(targets, Target{ID: "vault:" + id, Label: id})
		}
		keys := Assign(nil, targets, "")
		lines = []string{stHead.Render("Switch vault (this session only)")}
		for _, t := range targets {
			cur := ""
			if t.Label == m.env.Vault.ID {
				cur = stDim.Render("  current")
			}
			lines = append(lines, "  "+Mark(t.Label, keys[t.ID])+cur)
			keymap[keys[t.ID].Code] = t.ID
		}
		lines = append(lines, "", stDim.Render("esc cancels"))
	case ovExport:
		var l []string
		l, keymap = m.exportLines()
		lines = l
	}
	// Overlay replaces the top of the body, full width.
	out := make([]string, 0, h)
	for _, l := range lines {
		out = append(out, " "+truncateLine(l, m.w-1))
	}
	for len(out) < h {
		out = append(out, "")
	}
	if len(out) > h {
		out = out[:h]
	}
	return out, keymap
}

func (m *Model) exportLines() ([]string, map[string]string) {
	e := m.ov.export
	keymap := map[string]string{}
	if !e.loaded {
		return []string{"loading export options..."}, keymap
	}
	if e.stage == 2 {
		return []string{
			stHead.Render(fmt.Sprintf("Export %s as %s to %s", e.doc, e.format, e.transport.Name)),
			m.ov.input.View(), "", stDim.Render("enter sends, esc goes back"),
		}, keymap
	}
	items := e.list.Formats
	title := fmt.Sprintf("Export %s: pick a format", e.doc)
	if e.stage == 1 {
		items = e.list.Transports
		title = fmt.Sprintf("Export %s as %s: pick a transport", e.doc, e.format)
	}
	var targets []Target
	for _, it := range items {
		if it.Available {
			targets = append(targets, Target{ID: "opt:" + it.Name, Label: it.Name})
		}
	}
	keys := Assign(nil, targets, "")
	lines := []string{stHead.Render(title)}
	for _, it := range items {
		if !it.Available {
			lines = append(lines, stDim.Render(fmt.Sprintf("  %s  needs %s", it.Name, strings.Join(it.Needs, " or "))))
			continue
		}
		id := "opt:" + it.Name
		extra := ""
		if it.Found != "" {
			extra = stDim.Render("  " + it.Found)
		}
		if it.Arg != "" {
			extra += stDim.Render("  " + it.Arg)
		}
		lines = append(lines, "  "+Mark(it.Name, keys[id])+extra)
		keymap[keys[id].Code] = id
	}
	lines = append(lines, "", stDim.Render("esc goes back"))
	return lines, keymap
}

func helpLines() []string {
	return []string{
		stHead.Render("Keys"),
		"  1-6        tabs: Today Review Work RFX People Policy",
		"  " + keyed("x") + "          each link and action shows one reversed letter; press it to follow",
		"  j k        move the cursor, enter opens the row",
		"  g G        top, bottom        space, ctrl+d  page down",
		"  esc        back (or leave full screen)",
		"  ;ab        a link with a two letter key after ;",
		"  /          search (on People: who)",
		"  c          capture a note to the inbox",
		"  v          switch vault for this session",
		"  document:  z full screen, x export menu, y copy with the last format",
		"  review:    a accept, r reopen",
		"  q          quit",
		"",
		stDim.Render("para never sends anything. esc closes this."),
	}
}
