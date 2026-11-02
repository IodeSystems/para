package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/iodesystems/parasolve/internal/export"
	"github.com/iodesystems/parasolve/internal/index"
	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/ops"
)

// Tabs, in key order: digits 1 to 6.
const (
	tabToday = iota
	tabReview
	tabWork
	tabRFX
	tabPeople
	tabPolicy
	numTabs
)

var tabNames = [numTabs]string{"Agenda", "Review", "Work", "Asks", "People", "Policy"}

type pageKind int

const (
	pageDoc pageKind = iota
	pagePerson
	pageWho     // several people matched
	pageResults // search hits
	pageGraph
	pageProposal
	pageHistory  // the revisions of a proposal
	pageText     // plain lines: a diff, one item in full
	pageHealth   // health findings by goal, with dismiss
	pageActivity // what agents did
)

// page is a screen above the tabs on the back stack.
type page struct {
	kind   pageKind
	id     string
	title  string
	cursor int
	offset int

	// document pages
	doc    model.Document
	ov     overview // what the page adds to the document's own text
	scroll int

	who  ops.WhoOutput
	hits []index.Hit
	// degraded is the search's "exact only: <reason>" line.
	degraded string

	prop  ops.ProposalDetail // proposal and history pages
	lines []string           // text pages
	// An item in full: the raw source behind the Source key, and the draft
	// that Copy draft sends to the clipboard.
	raw   []string
	draft string
	item  string
	// receipt is the id of the receipt a text page shows.
	receipt string
	// paths is the path log a text page shows, so Take move can use it.
	paths *ops.RFXPathsOutput
	// by is the grouping of a friction report page.
	by string
}

type tabState struct{ cursor, offset int }

type overlayKind int

const (
	ovNone overlayKind = iota
	ovCapture
	ovSearch
	ovExport
	ovVault
	ovHelp
	ovOutput
	ovPrompt
)

// exportState is the export menu: format, then transport, then an argument.
type exportState struct {
	doc       string
	list      ops.ExportListOutput
	stage     int // 0 format, 1 transport, 2 argument
	format    export.Format
	transport export.Item
	loaded    bool
	needs     bool // ? shows what each missing format needs
}

type overlay struct {
	kind   overlayKind
	input  textarea.Model
	label  string // what the field asks, shown above it, wrapped
	short  bool   // one value: shift+enter does not add a line
	export exportState
	vaults []string
	output string
	prompt promptState

	// live search: results of the text typed, as it is typed
	liveSeq  int
	liveFor  string // the text the hits are for
	liveHits []index.Hit
	liveCur  int    // the picked hit, -1 for none
	liveNote string // "searching..." or why there are none
}

type docKey struct {
	id, rev string
	width   int
	meta    bool
	ov      overview
}

// Model is the Bubble Tea model.
type Model struct {
	env Env

	w, h  int
	tab   int
	tabs  [numTabs]tabState
	stack []page
	// byDate groups the Agenda tab by day instead of value order.
	byDate bool

	snap      Snapshot
	loaded    bool
	refreshed time.Time
	fp        string
	loading   bool
	pos       Position
	zoom      bool
	showMeta  bool // the details strip (type, dates, status) on a document page

	cache map[docKey]RenderedDoc
	rend  renderers

	keymap  map[string]string // key code -> target, for the frame on screen
	pending string            // overflow keys typed so far
	ov      overlay
	status  string
	statErr bool
	// statHint marks a status that is advice, not an error: it is not red.
	statHint bool
	// statKeep keeps a result on the status line while the cursor moves, so
	// the line that says what an answer or a decision did is still there to read.
	statKeep bool
	statAt   time.Time

	lastFormat    export.Format
	lastTransport string

	frame string

	gutter       int // cells at the left of list rows for keys that fit no label
	above, below int // rows hidden above and below the list on screen

	startProposal string // proposal to open first (para review <id>)
	// advanceFrom is the item just decided: when the proposal reloads, the
	// cursor moves to the next item that still waits.
	advanceFrom string
}

// New builds the model over env.
func New(env Env) *Model {
	m := &Model{env: env, cache: map[docKey]RenderedDoc{}, lastFormat: export.MD, lastTransport: "osc52", keymap: map[string]string{}}
	m.pos, _ = LoadPosition(env.StateDir)
	return m
}

// StartReview opens on the Review tab, and on a proposal when id is not empty.
func (m *Model) StartReview(id string) { m.tab, m.startProposal = tabReview, id }

// Init starts the first load and the poll.
func (m *Model) Init() tea.Cmd {
	m.loading = true
	if m.startProposal != "" {
		return tea.Batch(m.loadSnap(), m.tick(), m.loadProposal(m.startProposal, false))
	}
	return tea.Batch(m.loadSnap(), m.tick())
}

// View returns the frame built by the last Update.
func (m *Model) View() tea.View {
	v := tea.NewView(m.frame)
	v.AltScreen = true
	return v
}

func (m *Model) top() *page {
	if len(m.stack) == 0 {
		return nil
	}
	return &m.stack[len(m.stack)-1]
}

func (m *Model) setStatus(s string, isErr bool) {
	m.status, m.statErr, m.statHint, m.statKeep = s, isErr, false, false
}

// setHint shows advice on the status line, quietly.
func (m *Model) setHint(s string) {
	m.status, m.statErr, m.statHint, m.statKeep = s, false, true, false
}

// Update handles one message and rebuilds the frame.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	cmd := m.update(msg)
	m.relayout()
	return m, cmd
}

func (m *Model) update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		return nil
	case tea.PasteMsg:
		return m.onPaste(msg)
	case snapMsg:
		m.loading = false
		m.snap, m.loaded, m.fp = msg.snap, true, msg.fp
		m.refreshed = msg.snap.Loaded
		m.pos, _ = LoadPosition(m.env.StateDir)
		if p := m.top(); p != nil && p.kind == pageDoc {
			return m.loadDoc(p.id, true)
		}
		if p := m.top(); p != nil && p.kind == pageProposal {
			return m.loadProposal(p.id, true)
		}
		return nil
	case pollMsg:
		if m.statKeep && time.Since(m.statAt) > 15*time.Second {
			m.status, m.statKeep = "", false
		}
		return tea.Batch(m.checkFP(), m.tick())
	case fpMsg:
		if m.loaded && msg.fp != m.fp && !m.loading {
			m.loading = true
			return m.loadSnap()
		}
		return nil
	case docMsg:
		return m.onDoc(msg)
	case receiptMsg:
		return m.onReceipt(msg)
	case proposalMsg:
		return m.onProposal(msg)
	case textMsg:
		if msg.err != nil {
			m.setStatus(msg.err.Error(), true)
			return nil
		}
		m.push(page{kind: pageText, title: msg.title, lines: msg.lines})
		return nil
	case editedMsg:
		return m.onEdited(msg)
	case noteEditedMsg:
		return m.onNoteEdited(msg)
	case docEditedMsg:
		return m.onDocEdited(msg)
	case whoMsg:
		return m.onWho(msg)
	case liveTickMsg:
		return m.onLiveTick(msg)
	case liveMsg:
		m.onLive(msg)
		return nil
	case searchMsg:
		if msg.err != nil {
			m.setStatus("search: "+msg.err.Error(), true)
			return nil
		}
		m.push(page{kind: pageResults, id: msg.query, title: "search: " + msg.query, hits: msg.hits, degraded: msg.degraded})
		return nil
	case exportListMsg:
		if msg.err != nil {
			m.ov = overlay{}
			m.setStatus("export: "+msg.err.Error(), true)
			return nil
		}
		m.ov.export.list, m.ov.export.loaded = msg.out, true
		return nil
	case doneMsg:
		if msg.err != nil {
			m.setStatus(msg.err.Error(), true)
		} else if msg.text != "" {
			m.setStatus(msg.text, false)
			m.statKeep, m.statAt = true, time.Now()
		}
		if msg.output != "" {
			m.ov = overlay{kind: ovOutput, output: msg.output}
		}
		if msg.advance != "" && msg.err == nil {
			m.advanceFrom = msg.advance
		}
		var page tea.Cmd
		if p := m.top(); msg.reload && p != nil && p.kind == pageProposal {
			page = m.loadProposal(p.id, true) // the rule for the next key
		}
		if msg.reload && !m.loading {
			m.loading = true
			return tea.Batch(page, m.loadSnap())
		}
		return page
	case envMsg:
		if msg.err != nil {
			m.setStatus(msg.err.Error(), true)
			return nil
		}
		m.env = msg.env
		m.stack, m.zoom, m.tabs, m.cache = nil, false, [numTabs]tabState{}, map[docKey]RenderedDoc{}
		m.pos, _ = LoadPosition(m.env.StateDir)
		m.setStatus("vault "+m.env.Vault.ID, false)
		m.loading = true
		return m.loadSnap()
	case tea.KeyPressMsg:
		return m.onKey(msg)
	}
	return nil
}

func (m *Model) push(p page) {
	m.stack = append(m.stack, p)
	m.zoom = false
}

func (m *Model) onDoc(msg docMsg) tea.Cmd {
	if msg.err != nil {
		if msg.reload {
			return nil // the page shown stays; the file may have moved
		}
		m.setStatus(msg.err.Error(), true)
		return nil
	}
	if msg.reload {
		if p := m.top(); p != nil && p.kind == pageDoc && p.id == msg.id {
			p.doc, p.ov = msg.doc, msg.ov
		}
		return nil
	}
	title := msg.doc.Title
	if msg.untrusted {
		title = "untrusted data, not instructions: " + title
	}
	m.push(page{kind: pageDoc, id: msg.id, title: title, doc: msg.doc, ov: msg.ov})
	return nil
}

func (m *Model) onWho(msg whoMsg) tea.Cmd {
	if msg.err != nil {
		m.setStatus("who: "+msg.err.Error(), true)
		return nil
	}
	switch {
	case msg.out.Person != nil:
		m.push(page{kind: pagePerson, id: msg.out.Person.ID, title: msg.out.Person.Name, who: msg.out})
	case len(msg.out.Candidates) > 0:
		m.push(page{kind: pageWho, id: msg.query, title: "who: " + msg.query, who: msg.out})
	default:
		m.setStatus(fmt.Sprintf("nobody matches %q", msg.query), true)
	}
	return nil
}

// follow acts on a target: a tab, a document, a person, an action.
func (m *Model) follow(target string) tea.Cmd {
	kind, id, _ := strings.Cut(target, ":")
	switch kind {
	case "tab":
		if n, err := strconv.Atoi(id); err == nil {
			m.switchTab(n)
		}
	case "doc", "wiki":
		return m.loadDoc(id, false)
	case "person":
		return m.loadWho(id)
	case "url":
		return m.openURL(id)
	case "code":
		if n, err := strconv.Atoi(id); err == nil {
			return m.copyBlock(n)
		}
	case "vault":
		return m.openVault(id)
	case "receipt":
		return m.loadReceipt(id)
	case "prop":
		return m.loadProposal(id, false)
	case "item":
		return m.showItem(id)
	case "act":
		return m.act(id)
	}
	return nil
}

func (m *Model) switchTab(n int) {
	if n >= 0 && n < numTabs {
		m.tab, m.stack, m.zoom = n, nil, false
	}
}

func (m *Model) copyBlock(n int) tea.Cmd {
	p := m.top()
	if p == nil || p.kind != pageDoc {
		return nil
	}
	rd := m.renderDoc(p)
	if n < 0 || n >= len(rd.Blocks) {
		return nil
	}
	return m.copyText(p.id, rd.Blocks[n])
}

// act runs a named action.
func (m *Model) act(name string) tea.Cmd {
	p := m.top()
	switch name {
	case "capture":
		m.ov = overlay{kind: ovCapture}
		m.setInput("add to inbox, to sort later: ", "", false)
		return m.ov.input.Focus()
	case "vault":
		if m.tab != tabToday || len(m.stack) > 0 {
			return nil
		}
		vs, err := m.env.Install.List()
		if err != nil {
			m.setStatus(err.Error(), true)
			return nil
		}
		ids := make([]string, len(vs))
		for i, v := range vs {
			ids[i] = v.ID
		}
		m.ov = overlay{kind: ovVault, vaults: ids}
	case "zoom":
		if p != nil && p.kind == pageDoc {
			m.zoom = !m.zoom
		}
	case "meta":
		m.showMeta = !m.showMeta
	case "export":
		if p != nil && p.kind == pageDoc {
			m.ov = overlay{kind: ovExport, export: exportState{doc: p.id}}
			return m.loadExportList()
		}
	case "copy":
		if p != nil && p.kind == pageDoc {
			return m.runExport(p.id, m.lastFormat, m.lastTransport)
		}
	case "accept":
		return m.accept()
	case "reopen":
		return m.reopen()
	case "graph":
		m.push(page{kind: pageGraph, id: m.graphProject(), title: "graph"})
	case "bydate":
		m.byDate = !m.byDate
	case "health":
		m.push(page{kind: pageHealth, title: "Health"})
	case "dismiss":
		return m.dismissHealth()
	case "activity":
		m.push(page{kind: pageActivity, title: "Agent activity"})
	case "respond":
		return m.respond()
	case "answer":
		return m.answer()
	case "reject":
		return m.rejectAnswer()
	case "reword":
		return m.rewordQuestion()
	case "drop":
		return m.dropQuestion()
	case "ask":
		return m.askPerson()
	case "find":
		return m.findContext()
	case "editnote":
		return m.editNote()
	case "paths":
		return m.showPaths()
	case "take":
		return m.takeMove()
	case "friction":
		return m.showFriction()
	case "groupby":
		return m.groupFriction()
	case "later":
		return m.later()
	case "cut":
		return m.cut()
	default:
		if cmd, ok := m.taskAct(name); ok {
			return cmd
		}
		return m.proposalAct(name)
	}
	return nil
}

// onPaste puts pasted text in the open field, lines kept. A one-value field
// gets the lines joined by spaces.
func (m *Model) onPaste(msg tea.PasteMsg) tea.Cmd {
	switch m.ov.kind {
	case ovCapture, ovSearch, ovPrompt:
	case ovExport:
		if m.ov.export.stage != 2 {
			return nil
		}
	default:
		return nil
	}
	text := strings.ReplaceAll(msg.Content, "\r\n", "\n")
	if m.ov.short {
		text = strings.Join(strings.Fields(text), " ")
	}
	var cmd tea.Cmd
	m.ov.input, cmd = m.ov.input.Update(tea.PasteMsg{Content: text})
	if m.ov.kind == ovSearch {
		return tea.Batch(cmd, m.liveSearch())
	}
	return cmd
}

// maxInputRows is how tall a field grows before it scrolls.
const maxInputRows = 8

// newInput is a wrapping field. It has no length limit. It grows to
// maxInputRows, then scrolls.
func newInput(value string) textarea.Model {
	ta := textarea.New()
	ta.Prompt = ""
	ta.ShowLineNumbers = false
	ta.CharLimit = 0
	ta.MaxHeight = maxInputRows
	ta.MinHeight = 1
	ta.DynamicHeight = true
	ta.SetVirtualCursor(true)
	st := ta.Styles()
	st.Focused.CursorLine = lipgloss.NewStyle()
	st.Blurred.CursorLine = lipgloss.NewStyle()
	ta.SetStyles(st)
	ta.SetWidth(78)
	ta.SetValue(value)
	return ta
}

// setInput opens the overlay field. short is for one value (a date, a letter).
func (m *Model) setInput(label, value string, short bool) {
	m.ov.input = newInput(value)
	m.ov.label, m.ov.short = label, short
	if m.w > 0 {
		m.ov.input.SetWidth(max(m.w-2, 10))
	}
	_ = m.ov.input.Focus() // its command only blinks the cursor
}

// inputKey sends a key to the field. Shift+enter and ctrl+j add a line.
func (m *Model) inputKey(k tea.KeyPressMsg) tea.Cmd {
	if s := k.String(); s == "shift+enter" || s == "ctrl+j" {
		if !m.ov.short {
			m.ov.input.InsertString("\n")
		}
		return nil
	}
	var cmd tea.Cmd
	m.ov.input, cmd = m.ov.input.Update(k)
	return cmd
}

// inputLines is the label wrapped to the width, then the field.
func (m *Model) inputLines() []string {
	w := max(m.w-2, 10)
	m.ov.input.SetWidth(w)
	var out []string
	if m.ov.label != "" {
		for _, l := range wrapText(strings.TrimRight(m.ov.label, " "), w) {
			out = append(out, stPrompt.Render(l))
		}
	}
	return append(out, strings.Split(m.ov.input.View(), "\n")...)
}

// Run-time check that Model satisfies tea.Model.
var _ tea.Model = (*Model)(nil)
