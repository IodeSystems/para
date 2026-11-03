package tui

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/iodesystems/parasolve/internal/export"
	"github.com/iodesystems/parasolve/internal/index"
	"github.com/iodesystems/parasolve/internal/ops"
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
		case "down", "ctrl+n":
			if m.ov.kind == ovSearch {
				m.ov.liveCur = min(m.ov.liveCur+1, min(len(m.ov.liveHits), liveRows)-1)
				return nil
			}
		case "up", "ctrl+p":
			if m.ov.kind == ovSearch {
				m.ov.liveCur = max(m.ov.liveCur-1, -1)
				return nil
			}
		case "enter":
			if m.ov.kind == ovSearch && m.ov.liveCur >= 0 && m.ov.liveCur < len(m.ov.liveHits) {
				id := m.ov.liveHits[m.ov.liveCur].ID
				m.ov = overlay{}
				return m.follow("doc:" + id)
			}
			text := strings.TrimSpace(m.ov.input.Value())
			kind := m.ov.kind
			m.ov = overlay{}
			if text == "" {
				return nil
			}
			if kind == ovCapture {
				return m.capture(text)
			}
			if who, ok := strings.CutPrefix(text, personPrefix); ok {
				return m.loadWho(strings.TrimSpace(who))
			}
			return m.loadSearch(text)
		}
		cmd := m.inputKey(k)
		if m.ov.kind == ovSearch {
			return tea.Batch(cmd, m.liveSearch())
		}
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
	case ovPrompt:
		return m.onPromptKey(k)
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
		return m.inputKey(k)
	}
	if s == "?" {
		e.needs = !e.needs
		return nil
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
			m.setInput(it.Arg+" ", def, true)
			e.stage = 2
			return nil
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

// wrapped is text broken at spaces to the overlay width, one style per line.
func (m *Model) wrapped(st lipgloss.Style, text string) []string {
	var out []string
	for _, l := range wrapText(text, max(m.w-2, 10)) {
		out = append(out, st.Render(l))
	}
	return out
}

// drawOverlay paints the overlay over the body and adds its keys.
func (m *Model) drawOverlay(body []string, keymap map[string]string, h int) ([]string, map[string]string) {
	var lines []string
	switch m.ov.kind {
	case ovCapture, ovSearch:
		hint := "enter adds it to the inbox, shift+enter or ctrl+j adds a line, esc cancels"
		if m.ov.kind == ovSearch {
			hint = "down picks a result, enter opens it or shows every result, esc cancels. person:<name> finds a person"
		}
		lines = append(m.wrapped(stPrompt, "▸ "+hint), m.inputLines()...)
		if m.ov.kind == ovSearch {
			lines = append(lines, m.liveLines()...)
		}
	case ovPrompt:
		lines = m.wrapped(stPrompt, "▸ "+m.ov.prompt.title)
		lines = append(lines, m.wrapped(stDim, m.ov.prompt.hint())...)
		for _, l := range m.ov.prompt.lines {
			for _, w := range wrapText(strings.TrimSpace(l), max(m.w-4, 10)) {
				lines = append(lines, "  "+w)
			}
		}
		lines = append(lines, m.inputLines()...)
	case ovHelp:
		lines = m.helpLines()
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

// exportName is the title of the document being exported, never its id.
func (m *Model) exportName(id string) string {
	if p := m.docPage(); p != nil && p.id == id && strings.TrimSpace(p.doc.Title) != "" {
		return p.doc.Title
	}
	return titleOr(m.snap.name(id), "this page")
}

func (m *Model) exportLines() ([]string, map[string]string) {
	e := m.ov.export
	keymap := map[string]string{}
	if !e.loaded {
		return []string{"loading export options..."}, keymap
	}
	if e.stage == 2 {
		return append(append([]string{
			stHead.Render(fmt.Sprintf("Export %s as %s to %s", m.exportName(e.doc), e.format, e.transport.Name)),
		}, m.inputLines()...), "", stDim.Render("enter sends, esc goes back")), keymap
	}
	items := e.list.Formats
	title := fmt.Sprintf("Export %s: pick a format", m.exportName(e.doc))
	if e.stage == 1 {
		items = e.list.Transports
		title = fmt.Sprintf("Export %s as %s: pick a transport", m.exportName(e.doc), e.format)
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
			if e.needs {
				lines = append(lines, stDim.Render(fmt.Sprintf("  %s needs %s", it.Name, strings.Join(it.Needs, " or "))))
			} else {
				lines = append(lines, stDim.Render(fmt.Sprintf("  %s (not installed)", it.Name)))
			}
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
	hint := "esc goes back"
	if slices.ContainsFunc(items, func(it export.Item) bool { return !it.Available }) {
		hint += "   ? shows what a missing one needs"
	}
	lines = append(lines, "", stDim.Render(hint))
	return lines, keymap
}

// actionHelp says what each action does, for the help sheet.
var actionHelp = map[string]string{
	"act:vault":     "switch vault, for this session only (Agenda only; other screens show the vault read only)",
	"act:bydate":    "group by date: dated items by day with meeting hours; again for value order",
	"act:answer":    "answer the question; then give sources (a URL, a document id, or words such as a meeting) if you have them",
	"act:reject":    "reject the proposed answer, with a reason the agent reads",
	"act:drop":      "drop the question, with a reason: tasks that waited on it stop waiting",
	"act:ask":       "ask a person: makes an ask; para sends nothing",
	"act:find":      "ask an agent about the item (task, meeting, question, ask, goal, agent work); the text may be empty, then it investigates. The agent answers in words or proposes a fix; you check every proposal. A second press adds the text to the open work. When it replies, p responds",
	"act:later":     "put it off until a date or someday; it leaves the Agenda and next and still blocks what depends on it. On a put-off row it brings it back",
	"act:cut":       "cut one dependency so the work can go ahead without it; the reason is recorded as an assumption",
	"act:resolve":   "close the task: done (enter keeps it), partial, dropped, duplicate, or superseded; partial and the last two can make follow-up tasks that waiting items move to",
	"act:start":     "start the task",
	"act:block":     "block the task, with a reason: who said it and where it applies",
	"act:assign":    "assign the task to self, a person, or agent:<name>; para sends nothing",
	"act:update":    "add free text to the task: kept whole under Updates, then an agent sorts it into one proposal for you to approve",
	"act:delegate":  "give the task to an agent: queues agent work linked to it, with its done condition and an optional hint; a second press adds the hint",
	"act:due":       "set or remove the due date",
	"act:graph":     "graph of the goal or area under the cursor",
	"act:accept":    "mark reviewed: logged changes up to the event under the cursor. Proposals are decided one item at a time",
	"act:reopen":    "reopen: undo the last mark, or reopen closed agent work",
	"act:zoom":      "full screen",
	"act:export":    "export menu",
	"act:copy":      "copy with the last format",
	"act:meta":      "show or hide the details strip (type, dates, status)",
	"act:approve":   "approve this item",
	"act:deny":      "deny, with a reason the agent reads",
	"act:hold":      "needs first: hold this until something else is done",
	"act:request":   "request changes, a clarification, or an investigation from the agent",
	"act:editnote":  "edit the title and body of this note in your editor",
	"act:edit":      "change the text yourself before approving",
	"act:source":    "the source the agent worked from (untrusted)",
	"act:diff":      "what changed since the previous revision",
	"act:history":   "every revision",
	"act:copydraft": "copy the draft of an approved email",
}

// helpLines is the help sheet for the screen showing: the keys that always
// work, then the actions of this screen with the keys they have now. A key is
// listed once, so a letter never has two meanings on the sheet.
func (m *Model) helpLines() []string {
	code := map[string]string{}
	for c, id := range m.keymap {
		code[id] = c
	}
	out := []string{
		stHead.Render("Keys"),
		"  1-6        tabs: Agenda Review Work Asks People Policy",
		"  j k        move the cursor      g G  top, bottom      space  page down",
		"  enter      open the row         esc  back",
		"  /          search as you type; person:<name> finds a person",
		"  c          add a line to the inbox, to sort later",
		"  q          quit",
		"  ;ab        two-letter keys start with ;",
		"  letter     the dim letter at the left of a row opens it",
	}
	var here []string
	add := func(ids ...string) {
		for _, id := range ids {
			if c := code[id]; c != "" && actionHelp[id] != "" {
				here = append(here, fmt.Sprintf("  %-10s %s", c, actionHelp[id]))
			}
		}
	}
	switch p := m.top(); {
	case p != nil && p.kind == pageProposal:
		add("act:approve", "act:deny", "act:request", "act:hold", "act:edit", "act:copydraft", "act:source", "act:diff", "act:history")
		here = append(here, "  enter      the item in full")
	case p != nil && p.kind == pageDoc:
		add("act:editnote", "act:answer", "act:reject", "act:drop", "act:ask", "act:resolve", "act:start", "act:block", "act:assign", "act:delegate", "act:due", "act:update", "act:find", "act:later", "act:cut", "act:zoom", "act:export", "act:copy", "act:meta")
	case p != nil && p.kind == pageText:
		add("act:source", "act:copydraft")
	case p != nil:
	case m.tab == tabReview:
		add("act:accept", "act:reopen", "act:answer", "act:reject", "act:drop", "act:ask", "act:resolve", "act:start", "act:block", "act:assign", "act:delegate", "act:due", "act:update", "act:find", "act:later", "act:cut")
	case m.tab == tabToday:
		add("act:vault", "act:bydate", "act:answer", "act:reject", "act:drop", "act:ask", "act:resolve", "act:start", "act:block", "act:assign", "act:delegate", "act:due", "act:update", "act:find", "act:later", "act:cut")
	case m.tab == tabWork:
		add("act:graph", "act:answer", "act:reject", "act:drop", "act:ask", "act:resolve", "act:start", "act:block", "act:assign", "act:delegate", "act:due", "act:update", "act:find", "act:later", "act:cut")
	}
	if len(here) > 0 {
		out = append(out, "", stHead.Render("On this screen"))
		out = append(out, here...)
	}
	return append(out, "", stDim.Render("para never sends anything. esc closes this."))
}

// personPrefix scopes a search to people: "person:ada" finds a person.
const personPrefix = "person:"

// liveRows is how many results the search overlay shows as you type.
const liveRows = 8

// liveDebounce is how long typing pauses before the search runs.
const liveDebounce = 200 * time.Millisecond

type (
	// liveTickMsg ends a pause in typing. It is stale when seq moved on.
	liveTickMsg struct {
		seq  int
		text string
	}
	// liveMsg is the hits of one live search.
	liveMsg struct {
		seq      int
		text     string
		hits     []index.Hit
		degraded string
		err      error
	}
)

// liveSearch starts the debounce after a key in the search box.
func (m *Model) liveSearch() tea.Cmd {
	text := strings.TrimSpace(m.ov.input.Value())
	m.ov.liveSeq++
	seq := m.ov.liveSeq
	if text == "" || strings.HasPrefix(text, personPrefix) {
		m.ov.liveFor, m.ov.liveHits, m.ov.liveNote = "", nil, ""
		return nil
	}
	if text != m.ov.liveFor {
		m.ov.liveNote = "searching..."
	}
	return tea.Tick(liveDebounce, func(time.Time) tea.Msg { return liveTickMsg{seq: seq, text: text} })
}

func (m *Model) onLiveTick(msg liveTickMsg) tea.Cmd {
	if m.ov.kind != ovSearch || msg.seq != m.ov.liveSeq {
		return nil
	}
	env := m.env
	return func() tea.Msg {
		in := ops.ParseQuery(msg.text)
		in.Limit, in.Prefix, in.ExactOnly = liveRows, true, true
		o, err := ops.Search.Run(context.Background(), env.Deps, in)
		return liveMsg{seq: msg.seq, text: msg.text, hits: o.Hits, degraded: o.Degraded, err: err}
	}
}

func (m *Model) onLive(msg liveMsg) {
	if m.ov.kind != ovSearch || msg.seq != m.ov.liveSeq {
		return
	}
	m.ov.liveFor, m.ov.liveHits, m.ov.liveNote = msg.text, msg.hits, ""
	m.ov.liveCur = -1
	switch {
	case msg.err != nil:
		m.ov.liveNote = "search failed: " + oneLine(msg.err.Error())
	case len(msg.hits) == 0:
		m.ov.liveNote = "nothing matches yet"
	}
}

// liveLines are the results under the search box.
func (m *Model) liveLines() []string {
	var out []string
	if m.ov.liveNote != "" {
		out = append(out, stDim.Render("  "+m.ov.liveNote))
	}
	for i, h := range m.ov.liveHits {
		if i >= liveRows {
			break
		}
		mark := "  "
		title := titleOr(h.Title, "(untitled)")
		if i == m.ov.liveCur {
			mark, title = "▸ ", stPrompt.Render(title)
		}
		out = append(out, mark+stDim.Render(fmt.Sprintf("%-9s", truncate(h.Kind, 9)))+" "+title)
	}
	return out
}
