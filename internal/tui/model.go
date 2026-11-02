package tui

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

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

var tabNames = [numTabs]string{"Today", "Review", "Work", "RFX", "People", "Policy"}

type pageKind int

const (
	pageDoc pageKind = iota
	pagePerson
	pageWho     // several people matched
	pageResults // search hits
	pageGraph
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
	scroll int

	who  ops.WhoOutput
	hits []index.Hit
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
)

// exportState is the export menu: format, then transport, then an argument.
type exportState struct {
	doc       string
	list      ops.ExportListOutput
	stage     int // 0 format, 1 transport, 2 argument
	format    export.Format
	transport export.Item
	loaded    bool
}

type overlay struct {
	kind   overlayKind
	input  textinput.Model
	export exportState
	vaults []string
	output string
}

type docKey struct {
	id, rev string
	width   int
}

// Model is the Bubble Tea model.
type Model struct {
	env Env

	w, h  int
	tab   int
	tabs  [numTabs]tabState
	stack []page

	snap      Snapshot
	loaded    bool
	refreshed time.Time
	fp        string
	loading   bool
	pos       Position
	zoom      bool

	cache map[docKey]RenderedDoc
	rend  renderers

	keymap  map[string]string // key code -> target, for the frame on screen
	pending string            // overflow keys typed so far
	ov      overlay
	status  string
	statErr bool

	lastFormat    export.Format
	lastTransport string

	frame string
}

// New builds the model over env.
func New(env Env) *Model {
	m := &Model{env: env, cache: map[docKey]RenderedDoc{}, lastFormat: export.MD, lastTransport: "osc52", keymap: map[string]string{}}
	m.pos, _ = LoadPosition(env.StateDir)
	return m
}

// Init starts the first load and the poll.
func (m *Model) Init() tea.Cmd {
	m.loading = true
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

func (m *Model) setStatus(s string, isErr bool) { m.status, m.statErr = s, isErr }

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
	case snapMsg:
		m.loading = false
		m.snap, m.loaded, m.fp = msg.snap, true, msg.fp
		m.refreshed = msg.snap.Loaded
		m.pos, _ = LoadPosition(m.env.StateDir)
		if p := m.top(); p != nil && p.kind == pageDoc {
			return m.loadDoc(p.id, true)
		}
		return nil
	case pollMsg:
		return tea.Batch(m.checkFP(), m.tick())
	case fpMsg:
		if m.loaded && msg.fp != m.fp && !m.loading {
			m.loading = true
			return m.loadSnap()
		}
		return nil
	case docMsg:
		return m.onDoc(msg)
	case whoMsg:
		return m.onWho(msg)
	case searchMsg:
		if msg.err != nil {
			m.setStatus("search: "+msg.err.Error(), true)
			return nil
		}
		m.push(page{kind: pageResults, id: msg.query, title: "search: " + msg.query, hits: msg.hits})
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
		}
		if msg.output != "" {
			m.ov = overlay{kind: ovOutput, output: msg.output}
		}
		if msg.reload && !m.loading {
			m.loading = true
			return m.loadSnap()
		}
		return nil
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
			p.doc = msg.doc
		}
		return nil
	}
	m.push(page{kind: pageDoc, id: msg.id, title: msg.doc.Title, doc: msg.doc})
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
		m.ov = overlay{kind: ovCapture, input: newInput("capture: ", "")}
		return m.ov.input.Focus()
	case "vault":
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
	}
	return nil
}

func newInput(prompt, value string) textinput.Model {
	ti := textinput.New()
	ti.Prompt = prompt
	ti.SetValue(value)
	ti.CharLimit = 2000
	return ti
}

// Run-time check that Model satisfies tea.Model.
var _ tea.Model = (*Model)(nil)
