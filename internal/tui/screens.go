package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/iodesystems/parasolve/internal/index"
	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/ops"
	"github.com/iodesystems/parasolve/internal/today"
)

// KeyFn marks a link label with its key, or returns the label when the link
// has none (not visible, or measuring pass).
type KeyFn func(id, label string) string

// row is one line of a list screen.
type row struct {
	sel    bool   // the cursor can stop here
	target string // followed by enter
	kind   string // "need", "event", "head", ""
	id     string // document id for accept and reopen
	need   Need
	// proposed marks a question row whose answer an agent proposed: it can be
	// accepted or rejected.
	proposed bool
	event    model.Event
	hkind    string // finding kind, on a health row
	draw     func(k KeyFn) string
}

var (
	stHead   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	stDim    = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	stGood   = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	stWarn   = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	stBad    = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	stActive = lipgloss.NewStyle().Bold(true).Underline(true)
	stPrompt = lipgloss.NewStyle().Bold(true)
	stMatch  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("252"))
)

// selBG is the background of the selected row. Selection never uses reverse
// video, which belongs to key letters.
const selBG = "\x1b[48;5;238m"

func head(text string) row {
	return row{kind: "head", draw: func(KeyFn) string { return stHead.Render(text) }}
}

func plain(text string) row {
	return row{draw: func(KeyFn) string { return text }}
}

func dim(text string) row {
	return row{draw: func(KeyFn) string { return stDim.Render(text) }}
}

// narrowW is the width below which a title wins over the text around it.
const narrowW = 100

// indentOf is the leading spaces of s.
func indentOf(s string) string {
	p := stripSGR(s)
	return p[:len(p)-len(strings.TrimLeft(p, " "))]
}

// link builds a selectable row: pre, a keyed label, post. The label is cut to
// fit w cells before its key letter is chosen, so the key is always visible. On
// a narrow screen the title comes first: the text after it goes, then the text
// before it, before the title is cut.
func link(target, pre, label, post string, w int) row {
	return linkRow(target, pre, label, post, w, false)
}

// linkKeep is link for a row that starts with a tag: the tag stays and the
// title is cut instead.
func linkKeep(target, pre, label, post string, w int) row {
	return linkRow(target, pre, label, post, w, true)
}

func linkRow(target, pre, label, post string, w int, keepPre bool) row {
	return row{sel: true, target: target, draw: func(k KeyFn) string {
		p, q := pre, post
		if w < narrowW && cells(p)+cells(label)+cells(q) > w-2 {
			q = ""
			if !keepPre && cells(p)+cells(label) > w-2 {
				p = indentOf(p)
			}
		}
		room := max(w-cells(p)-cells(q)-2, 8)
		return p + k(target, truncate(label, room)) + q
	}}
}

// stateWords is the one word for a state on every screen. A task that can be
// worked is "ready", never "todo". A request is "open", and "stalled" is a
// modifier shown beside it. A silent target is "no reply". No word shows its
// snake case.
var stateWords = map[string]string{
	"todo":        "ready",
	"no_reply":    "no reply",
	"in_progress": "in progress",
}

// stateWord maps a stored state to its word.
func stateWord(s string) string {
	if w, ok := stateWords[s]; ok {
		return w
	}
	return strings.ReplaceAll(s, "_", " ")
}

// state colours a status word. One colour has one meaning: green is done or
// healthy, red is wrong or stuck, amber needs you, grey is anything in progress
// or waiting on someone else. A word that only says where something stands, such
// as sent or open, is grey.
func state(s string) string {
	s = stateWord(s)
	switch s {
	case "ready", "done", "answered", "verified", "current", "replied", "adopted", "resolved", "accepted":
		return stGood.Render(s)
	case "blocked", "failed", "stalled", "missing", "changed", "declined", "no reply", "overdue":
		return stBad.Render(s)
	case "unverified", "proposed", "drafted", "due", "pending":
		return stWarn.Render(s)
	}
	return stDim.Render(s)
}

func plural(n int, one string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %ss", n, one)
}

// openProposals are the proposals that wait for the owner's decision.
func (m *Model) openProposals() []ops.ProposalView {
	var out []ops.ProposalView
	for _, p := range m.snap.Proposals {
		if p.Status == model.ProposalOpen && p.Error == "" {
			out = append(out, p)
		}
	}
	return out
}

// todayTag is the short tag of a Today row, padded to one column.
func todayTag(word string, st lipgloss.Style) string { return st.Render(pad(word, 9)) }

// rowsToday is the home screen: one list, most urgent first, one short tag on
// each row. A thin divider sits before the lower-priority rows. A row appears
// once, whichever way it qualifies.
func (m *Model) rowsToday(w int) []row {
	s := m.snap
	if !m.loaded {
		return []row{dim("loading...")}
	}
	if m.byDate {
		return append(m.rowsAgenda(w), m.vaultRows(w)...)
	}
	inbox := "inbox 0"
	if n := len(s.Inbox); n > 0 {
		inbox = fmt.Sprintf("inbox %d to sort", n)
	}
	title := fmt.Sprintf("Agenda, %s", dateWords(s.Date, s.Loaded))
	if w >= 60 {
		title += "   vault " + s.Vault
	}
	if n := m.answersToCheck(); n > 0 {
		inbox += fmt.Sprintf(", %s to check", plural(n, "answer"))
	}
	if n := len(s.Undone); n > 0 {
		inbox += fmt.Sprintf(", %d approved, not done", n)
	}
	r := []row{head(title + stDim.Render("   "+inbox))}
	shown := map[string]bool{}
	// add builds a row for an item the first time it appears on the screen.
	add := func(id string, build func() row) {
		if shown[id] {
			return
		}
		shown[id] = true
		r = append(r, build())
	}
	item := func(id, tag string, st lipgloss.Style, title, post string) {
		add(id, func() row { return linkKeep("doc:"+id, "  "+todayTag(tag, st)+" ", titleOr(title, id), post, w) })
	}
	nudge := map[string]ops.WaitingItem{}
	for _, it := range s.Waiting {
		nudge[it.ID] = it
	}
	for _, p := range m.openProposals() {
		add(p.ID, func() row {
			x := linkKeep("prop:"+p.ID, "  "+todayTag("proposal", stWarn)+" ", titleOr(p.Title, p.ID), stDim.Render(fmt.Sprintf("  %d of %d to decide, from %s", p.Pending, p.ItemCount, p.ProposedBy)), w)
			x.kind, x.id = "proposal", p.ID
			return x
		})
	}
	// One list in value order (ops.RankValue): incidents, then what has an
	// effective due date, then the rest. Each row says why it is there.
	for _, it := range s.Today.Agenda.Items {
		tag, st := it.Kind, stDim
		why := stDim.Render("  " + it.Why)
		switch it.Kind {
		case ops.KindIncident:
			st = stBad
		case ops.KindQuestion:
			tag, st = "question", stWarn
			if it.Proposal {
				tag = "check"
			}
		case ops.KindTask:
			tag, st = "ready", stGood
			if it.Due != "" && it.Due < s.Date {
				tag, st = "late", stBad
			} else if it.Due != "" {
				tag, st = "due", stWarn
			}
		case ops.KindAsk:
			tag, st = "follow up", stWarn
			if x, ok := nudge[it.ID]; ok && len(x.TargetNames) > 0 {
				why += stDim.Render("  to " + joinNames(x.TargetNames))
			}
		case ops.KindMeeting:
			tag, st = "meeting", stGood
		}
		if q, ok := m.findQuestion(it.ID); ok && it.Kind == ops.KindQuestion {
			if q.Kind != model.KindQuestion {
				why = "  " + stDim.Render(string(q.Kind)) + why
			}
			if q.ScopeName != "" {
				why += stDim.Render("  " + q.ScopeName)
			}
		}
		add(it.ID, func() row {
			x := linkKeep("doc:"+it.ID, "  "+todayTag(tag, st)+" ", titleOr(it.Title, it.ID), why, w)
			switch it.Kind {
			case ops.KindQuestion:
				x.kind, x.id, x.proposed = "question", it.ID, it.Proposal
			case ops.KindTask:
				x.kind, x.id = "task", it.ID
			}
			return x
		})
	}
	if n := s.Today.Health; n > 0 {
		r = append(r, link("act:health", "  ", plural(n, "health finding"), stDim.Render("  review"), w))
	}
	if n := s.Today.UpgradeSteps; n > 0 {
		r = append(r, plain("  "+plural(n, "vault upgrade step")+" pending"+stDim.Render("  run para vault upgrade status")))
	}
	if n := s.Today.OtherOpen; n > 0 {
		r = append(r, link("tab:2", "  ", plural(n, "other open question"), stDim.Render("  on Work"), w))
	}
	dated := func(it today.Item, tag string, st lipgloss.Style) {
		post := "  " + st.Render(dateWords(it.Due, s.Loaded))
		if it.ProjectName != "" {
			post += "  " + stDim.Render(it.ProjectName)
		}
		item(it.ID, tag, st, it.Title, post)
	}
	for _, it := range s.Today.Overdue.Items {
		dated(it, "late", stBad)
	}
	for _, it := range s.Today.Nudges.Items {
		post := ""
		if x, ok := nudge[it.ID]; ok {
			if len(x.TargetNames) > 0 {
				post += "  " + stDim.Render("to "+joinNames(x.TargetNames))
			}
			post += "  " + stDim.Render("follow up "+dateWords(x.NextNudge, s.Loaded))
		}
		item(it.ID, "follow up", stWarn, it.Title, post)
	}
	for _, it := range s.Today.DueSoon.Items {
		dated(it, "due", stDim)
	}
	urgent := len(r)
	if n := s.Today.Deferred; n > 0 {
		r = append(r, dim(fmt.Sprintf("  %d put off (Later); questions are listed on Work", n)))
	}
	for _, t := range s.Next {
		post := ""
		if t.ProjectName != "" {
			post = "  " + stDim.Render(t.ProjectName)
		}
		item(t.ID, "ready", stGood, t.Title, post)
	}
	for _, it := range s.Today.Blocked.Items {
		item(it.ID, "blocked", stBad, it.Title, "  "+stDim.Render(it.ProjectName))
	}
	for _, it := range s.Waiting {
		post := ""
		if len(it.TargetNames) > 0 {
			post = "  " + stDim.Render("to "+joinNames(it.TargetNames))
		}
		if it.NextNudge != "" {
			post += "  " + stDim.Render("follow up "+dateWords(it.NextNudge, s.Loaded))
		}
		item(it.ID, "waiting", stDim, it.Title, post)
	}
	for _, t := range s.Tasks {
		if t.Assignee == "" || t.Assignee == "self" || t.Status == model.TaskDone || t.Status == model.TaskDropped {
			continue
		}
		post := ""
		if t.AssigneeName != "" {
			post = "  " + stDim.Render("with "+t.AssigneeName)
		}
		item(t.ID, "waiting", stDim, t.Title, post)
	}
	for _, it := range s.Today.Expiring.Items {
		item(it.ID, "expiring", stWarn, it.Title, "")
	}
	for _, it := range s.Today.Due.Items {
		item(it.ID, "review", stWarn, it.Title, "")
	}
	for _, it := range s.Inbox {
		item(it.ID, "inbox", stDim, it.Title, "  "+stDim.Render(dateWords(string(it.Created), s.Loaded)))
	}
	if len(r) == 1 {
		r = append(r, dim("  Nothing needs you today."))
	}
	if urgent > 1 && len(r) > urgent {
		r = append(r[:urgent], append([]row{dim("  " + strings.Repeat("─", max(min(w-4, 40), 4)))}, r[urgent:]...)...)
	}
	r = append(r, m.vaultRows(w)...)
	return r
}

// laterWords says when a put-off item returns.
// laterNote is the Later note of a row: the open waits, then the date.
func laterNote(until string, waits, names []string) string {
	var parts []string
	if len(waits) > 0 {
		w := make([]string, len(waits))
		for i, id := range waits {
			w[i] = id
			if i < len(names) && names[i] != "" {
				w[i] = names[i]
			}
		}
		parts = append(parts, "waits on: "+strings.Join(w, ", "))
	}
	if until != "" {
		parts = append(parts, "back "+laterWords(until))
	}
	if len(parts) == 0 {
		return "later"
	}
	return strings.Join(parts, ", ")
}

func laterWords(until string) string {
	if until == model.SomedayWord {
		return "someday"
	}
	return "on " + until
}

func joinNames(ns []string) string {
	var out []string
	for _, n := range ns {
		if n != "" {
			out = append(out, n)
		}
	}
	return strings.Join(out, ", ")
}

// vaultRows is the vault's own health: lint, sync trouble, load errors.
func (m *Model) vaultRows(w int) []row {
	s := m.snap
	var r []row
	var notes []string
	if s.LintErrors > 0 {
		notes = append(notes, stBad.Render(plural(s.LintErrors, "lint error")))
	}
	if s.Today.Unverified > 0 {
		notes = append(notes, stWarn.Render(plural(s.Today.Unverified, "unverified summary")))
	}
	if len(notes) > 0 {
		r = append(r, head("Vault"), plain("  "+strings.Join(notes, "   ")))
	}
	bad := func(text string) {
		if len(r) == 0 {
			r = append(r, head("Vault"))
		}
		r = append(r, plain("  "+stBad.Render(truncate(text, w-4))))
	}
	for _, c := range s.Today.SyncConflicts {
		bad("sync conflict  " + c)
	}
	for _, d := range s.Today.LostUpdates {
		bad("lost update  " + d)
	}
	for _, e := range s.Errs {
		bad(e)
	}
	if s.Today.Error != "" {
		bad("vault " + s.Vault + ": " + s.Today.Error)
	}
	for _, o := range s.Others {
		if o.Error != "" {
			bad("vault " + o.Vault + ": " + o.Error)
			continue
		}
		r = append(r, dim(fmt.Sprintf("  vault %s: incidents %d, due %d, follow-ups %d, blocked %d, inbox %d", o.Vault, o.Incidents.Count, o.Due.Count, o.Nudges.Count, o.Blocked.Count, o.Inbox)))
	}
	return r
}

// questionRows lists questions: kind, what each is about, and what it blocks.
// The row actions apply to the row under the cursor.
func (m *Model) questionRows(w int, qs []ops.QuestionView) []row {
	var rows []row
	for _, q := range qs {
		pre := "  " + stWarn.Render(string(q.Kind)) + "  "
		post := ""
		if q.ScopeName != "" {
			post = "  " + stDim.Render(q.ScopeName)
		}
		if q.Blocking > 0 {
			post += "  " + stBad.Render(fmt.Sprintf("blocks %d", q.Blocking))
		}
		if q.Deferred {
			post += "  " + stDim.Render(laterNote(q.DeferUntil, q.WaitsOn, q.WaitsOnNames))
		}
		x := link("doc:"+q.ID, pre, titleOr(q.Title, q.ID), post, w)
		x.kind, x.id, x.proposed = "question", q.ID, q.HasProposal
		rows = append(rows, x)
	}
	return rows
}

// questionSections is the question list in three sections: Blocking (something
// waits on it, or an answer waits to be checked), Other, and Later.
func (m *Model) questionSections(w int) []row {
	var blocking, other []ops.QuestionView
	for _, q := range m.snap.Questions {
		if q.Blocking > 0 || q.HasProposal {
			blocking = append(blocking, q)
		} else {
			other = append(other, q)
		}
	}
	var r []row
	for _, sec := range []struct {
		name string
		qs   []ops.QuestionView
	}{{"Blocking", blocking}, {"Other", other}, {"Later", m.snap.Later}} {
		if len(sec.qs) == 0 {
			continue
		}
		r = append(r, head(fmt.Sprintf("Questions: %s (%d)", sec.name, len(sec.qs))))
		r = append(r, m.questionRows(w, sec.qs)...)
	}
	return r
}

// review returns the derived Review data for the current snapshot.
func (m *Model) review() ReviewData { return BuildReview(m.snap, m.pos) }

func (m *Model) rowsReview(w int) []row {
	if !m.loaded {
		return []row{dim("loading...")}
	}
	rv := m.review()
	var r []row
	r = append(r, m.rowsAnswersToCheck(w)...)
	r = append(r, m.rowsProposals(w)...)
	r = append(r, m.rowsUndone(w)...)
	r = append(r, m.rowsReceipts(w)...)
	r = append(r, head(fmt.Sprintf("Needs you (%d)", len(rv.Needs))))
	if len(rv.Needs) == 0 {
		r = append(r, dim("  nothing"))
	}
	for _, n := range rv.Needs {
		n := n
		x := link("doc:"+n.ID, "  "+stWarn.Render(n.Kind)+"  ", titleOr(n.Title, n.ID), "", w)
		x.kind, x.id, x.need = "need", n.ID, n
		r = append(r, x)
	}
	since := "since your last review"
	if !m.pos.At.IsZero() {
		since += ", " + whenWords(m.pos.At, m.snap.Loaded)
	}
	r = append(r, head(fmt.Sprintf("Changed by others %s (%d)", since, rv.Count)))
	if rv.Count == 0 {
		r = append(r, dim("  nothing new"))
	}
	for _, g := range rv.Groups {
		r = append(r, plain("  "+stHead.Render(g.Actor)+stDim.Render("  "+plural(len(g.Events), "event"))))
		for _, e := range g.Events {
			e := e
			doc := e.Doc
			x := row{sel: doc != "", kind: "event", id: doc, event: e}
			if doc != "" {
				x.target = "doc:" + doc
			}
			name := titleOr(m.snap.name(doc), "(untitled)")
			x.draw = func(k KeyFn) string {
				pre := "    " + stDim.Render(whenWords(e.TS, m.snap.Loaded)) + " " + strings.ReplaceAll(e.Op, "_", " ") + " "
				if doc == "" {
					return pre
				}
				return pre + k(x.target, truncate(name, max(w-cells(pre)-2, 8)))
			}
			r = append(r, x)
		}
	}
	reqs := 0
	for _, q := range m.snap.Requests {
		if q.Status != model.RequestDone && q.Status != model.RequestCancelled {
			reqs++
		}
	}
	r = append(r, head(fmt.Sprintf("Agent work (%d open)", reqs)))
	for _, q := range m.snap.Requests {
		if q.Status == model.RequestDone || q.Status == model.RequestCancelled {
			continue
		}
		x := link("doc:"+q.ID, "  "+state(string(q.Status))+"  ", titleOr(q.Title, q.ID), "", w)
		x.id = q.ID
		if q.Status == model.RequestProposed {
			x.kind, x.need = "need", Need{NeedProposed, q.ID, q.Title}
		}
		r = append(r, x)
	}
	r = append(r, m.rowsFinished(w)...)
	return r
}

// finishedRows is how many finished items Review offers to respond to.
const finishedRows = 10

// rowsFinished lists finished agent work, so the owner can respond.
func (m *Model) rowsFinished(w int) []row {
	var r []row
	for i := len(m.snap.Requests) - 1; i >= 0 && len(r) < finishedRows; i-- {
		q := m.snap.Requests[i]
		if q.Status != model.RequestDone && q.Status != model.RequestFailed {
			continue
		}
		label := string(q.Resolution)
		if label == "" {
			label = string(q.Status)
		}
		x := link("doc:"+q.ID, "  "+state(label)+"  ", titleOr(q.Title, q.ID), "", w)
		x.kind, x.id = "work", q.ID
		r = append(r, x)
	}
	if len(r) == 0 {
		return nil
	}
	return append([]row{head("Finished agent work")}, r...)
}

func (m *Model) rowsWork(w int) []row {
	if !m.loaded {
		return []row{dim("loading...")}
	}
	s := m.snap
	var r []row
	byProject := map[string][]ops.TaskView{}
	byArea := map[string][]ops.TaskView{} // tasks with an area and no project
	for _, t := range s.Tasks {
		if t.Project != "" {
			byProject[t.Project] = append(byProject[t.Project], t)
		} else {
			byArea[t.Area] = append(byArea[t.Area], t)
		}
	}
	st := map[string]string{}
	for _, n := range s.Graph.Nodes {
		st[n.ID] = n.State
	}
	taskRow := func(indent string, t ops.TaskView) row {
		label := state(st[t.ID])
		if st[t.ID] == "" {
			label = state(string(t.Status))
		}
		if t.Outcome != "" && t.Outcome != model.OutcomeDone {
			label = stDim.Render(string(t.Outcome)) // how it ended, when not simply done
		}
		if t.Due != "" {
			label += stDim.Render("  due " + dateWords(string(t.Due), m.snap.Loaded))
		}
		if t.Deferred {
			label += stDim.Render("  " + laterNote(t.DeferUntil, t.WaitsOn, t.WaitsOnNames))
		}
		tx := link("doc:"+t.ID, indent, titleOr(t.Title, t.ID), "  "+label, w)
		tx.id = t.ID
		tx.kind = "task"
		return tx
	}
	linkRows := func(indent string, links []model.Pointer) {
		for _, k := range links {
			r = append(r, dim(truncate(fmt.Sprintf("%s%s: %s", indent, k.Kind, k.Label), w)))
		}
	}
	// An area holds its links, the tasks that belong to it directly, and its
	// projects. Each level is indented once more, so a task sits under the
	// project or area it belongs to.
	projectRows := func(indent string, p ops.ProjectView) {
		n := len(byProject[p.ID])
		count := "no tasks"
		if n > 0 {
			count = plural(n, "task")
		}
		x := link("doc:"+p.ID, indent, titleOr(p.Title, p.ID), "  "+state(string(p.Status))+stDim.Render("  "+count), w)
		x.id = p.ID
		r = append(r, x)
		linkRows(indent+"  ", p.Links)
		for _, t := range byProject[p.ID] {
			r = append(r, taskRow(indent+"    ", t))
		}
	}
	r = append(r, head(fmt.Sprintf("Areas (%d)", len(s.Areas))))
	for _, a := range s.Areas {
		x := link("doc:"+a.ID, "  ", titleOr(a.Title, a.ID), "  "+state(string(a.Status)), w)
		x.id = a.ID
		r = append(r, x)
		linkRows("      ", a.Links)
		for _, p := range s.Projects {
			if p.Area == a.ID {
				projectRows("    ", p)
			}
		}
		if own := byArea[a.ID]; len(own) > 0 {
			r = append(r, dim("    tasks in the area itself, with no goal"))
			for _, t := range own {
				r = append(r, taskRow("      ", t))
			}
		}
	}
	inArea := map[string]bool{}
	for _, a := range s.Areas {
		inArea[a.ID] = true
	}
	if n := countNoArea(s.Projects, inArea); n > 0 {
		r = append(r, head(fmt.Sprintf("Goals with no area (%d)", n)))
		for _, p := range s.Projects {
			if !inArea[p.Area] {
				projectRows("  ", p)
			}
		}
	}
	r = append(r, m.questionSections(w)...)
	if len(s.Cycles) > 0 {
		r = append(r, head("Cycles"))
		for _, c := range s.Cycles {
			var names []string
			for _, id := range append(append([]string{}, c...), c[0]) {
				names = append(names, titleOr(s.name(id), id))
			}
			r = append(r, plain("  "+stBad.Render(truncate(strings.Join(names, " -> "), w-4))))
		}
	}
	return r
}

func countNoArea(projects []ops.ProjectView, inArea map[string]bool) int {
	n := 0
	for _, p := range projects {
		if !inArea[p.Area] {
			n++
		}
	}
	return n
}

// graphProject is the project or area of the row under the cursor on Work, or
// all.
func (m *Model) graphProject() string {
	if m.tab != tabWork || len(m.stack) > 0 {
		return ""
	}
	rows := m.rowsWork(m.listWidth())
	c := m.tabs[tabWork].cursor
	if c < 0 || c >= len(rows) {
		return ""
	}
	id := rows[c].id
	for _, a := range m.snap.Areas {
		if a.ID == id {
			return id
		}
	}
	for _, p := range m.snap.Projects {
		if p.ID == id {
			return id
		}
	}
	for _, t := range m.snap.Tasks {
		if t.ID == id {
			if t.Project != "" {
				return t.Project
			}
			return t.Area
		}
	}
	return ""
}

// dirWords says who asked whom, in words.
func dirWords(d model.RFXDirection) string {
	if d == model.RFXIn {
		return "asks you"
	}
	return "you asked"
}

func (m *Model) rowsRFX(w int) []row {
	if !m.loaded {
		return []row{dim("loading...")}
	}
	s := m.snap
	var r []row
	r = append(r, head(fmt.Sprintf("Waiting (%d)", len(s.Waiting))))
	if len(s.Waiting) == 0 {
		r = append(r, dim("  nothing is waiting"))
	}
	for _, it := range s.Waiting {
		flags := ""
		flags += "  " + state("open")
		switch {
		case it.Stalled:
			flags += "  " + state("stalled")
		case it.Due:
			flags += "  " + state("due")
		}
		if it.NextNudge != "" {
			flags += stDim.Render("  follow up " + dateWords(it.NextNudge, m.snap.Loaded))
		}
		if _, ok := s.Drafts[it.ID]; ok {
			flags += "  " + state("drafted")
		}
		x := link("doc:"+it.ID, "  "+stDim.Render(dirWords(it.Direction))+"  ", titleOr(it.Title, it.ID), flags, w)
		x.id = it.ID
		r = append(r, x)
		for i, t := range it.Targets {
			name := ""
			if i < len(it.TargetNames) {
				name = it.TargetNames[i]
			}
			if name == "" {
				name = "someone"
			}
			r = append(r, link("person:"+t.To.Target(), "      to ", name, "  "+stDim.Render(string(t.Medium))+"  "+state(string(t.State)), w))
		}
		if len(it.BlockedOnNames) > 0 || len(it.BlockedOn) > 0 {
			r = append(r, dim("      after "+joinNames(it.BlockedOnNames)))
		}
		if d, ok := s.Drafts[it.ID]; ok {
			first, _, _ := strings.Cut(strings.TrimSpace(d), "\n")
			r = append(r, dim("      draft: "+truncate(first, max(w-14, 10))))
		}
	}
	var other []row
	for _, x := range s.RFX {
		waiting := false
		for _, it := range s.Waiting {
			if it.ID == x.ID {
				waiting = true
			}
		}
		if waiting {
			continue
		}
		o := link("doc:"+x.ID, "  "+stDim.Render(dirWords(x.Direction))+"  ", titleOr(x.Title, x.ID), "  "+state(string(x.Status)), w)
		o.id = x.ID
		other = append(other, o)
	}
	if len(other) > 0 {
		r = append(r, head(fmt.Sprintf("Other agent work (%d)", len(other))))
		r = append(r, other...)
	}
	return r
}

func (m *Model) rowsPeople(w int) []row {
	if !m.loaded {
		return []row{dim("loading...")}
	}
	r := []row{head(fmt.Sprintf("People (%d)", len(m.snap.People)) + stDim.Render("   / then person:<name> finds one by name, alias, or contact"))}
	if len(m.snap.People) == 0 {
		r = append(r, dim("  none"))
	}
	for _, p := range m.snap.People {
		r = append(r, link("person:"+p.ID, "  ", titleOr(p.Title, p.ID), "", w))
	}
	return r
}

func (m *Model) rowsPolicy(w int) []row {
	if !m.loaded {
		return []row{dim("loading...")}
	}
	s := m.snap
	var r []row
	r = append(r, head(fmt.Sprintf("Policies (%d)", len(s.Policies))))
	for _, p := range s.Policies {
		v := state("verified")
		if !p.Verified {
			v = state("unverified")
		}
		src := ""
		if p.SourceStatus != "" && p.SourceStatus != "current" {
			src = "  " + state(p.SourceStatus)
		}
		r = append(r, link("doc:"+p.ID, "  ", titleOr(p.Title, p.ID), "  "+v+src+stDim.Render(fmt.Sprintf("  %s", plural(p.Rules, "rule"))), w))
	}
	r = append(r, head(fmt.Sprintf("Rules (%d)", len(s.Rules))))
	for _, ru := range s.Rules {
		r = append(r, link("doc:"+ru.Policy, "  "+stDim.Render(ru.ID)+"  ", ru.Text, "  "+stDim.Render(ru.PolicyName), w))
	}
	r = append(r, head(fmt.Sprintf("Exceptions (%d)", len(s.Exceptions))))
	for _, e := range s.Exceptions {
		review := ""
		if e.ReviewBy != "" {
			review = stDim.Render("  review " + dateWords(string(e.ReviewBy), m.snap.Loaded))
		}
		r = append(r, link("doc:"+e.ID, "  "+state(string(e.Status))+"  ", titleOr(e.Title, e.ID), stDim.Render("  "+string(e.Kind))+review, w))
	}
	r = append(r, head(fmt.Sprintf("Constraints (%d)", len(s.Constrs))))
	for _, c := range s.Constrs {
		review := ""
		if c.ReviewBy != "" {
			review = stDim.Render("  review " + dateWords(string(c.ReviewBy), m.snap.Loaded))
		}
		r = append(r, link("doc:"+c.ID, "  ", titleOr(c.Says, c.ID), stDim.Render("  by "+c.By)+review, w))
	}
	return r
}

// rowsPerson is the person screen.
func (m *Model) rowsPerson(p *page, w int) []row {
	wp := p.who.Person
	if wp == nil {
		return nil
	}
	var r []row
	r = append(r, head(wp.Name+stDim.Render("  "+wp.Org)))
	r = append(r, link("doc:"+wp.ID, "  ", "Open the full page", "", w))
	if len(wp.Aliases) > 0 {
		r = append(r, plain("  aka "+strings.Join(wp.Aliases, ", ")))
	}
	if wp.Role != nil {
		r = append(r, link("doc:"+wp.Role.ID, "  role  ", titleOr(wp.Role.Title, wp.Role.ID), "", w))
	}
	if wp.Team != nil {
		r = append(r, link("doc:"+wp.Team.ID, "  team  ", titleOr(wp.Team.Title, wp.Team.ID), "", w))
	}
	if len(wp.Contacts) > 0 {
		r = append(r, head(fmt.Sprintf("Contacts (%d)", len(wp.Contacts))))
	}
	for _, c := range wp.Contacts {
		pref := ""
		if c.Preferred {
			pref = stGood.Render("  preferred")
		}
		r = append(r, plain(fmt.Sprintf("  %-10s %s%s", c.Method, c.Value, pref)))
	}
	if len(wp.Groups) > 0 {
		r = append(r, head(fmt.Sprintf("Groups (%d)", len(wp.Groups))))
	}
	for _, g := range wp.Groups {
		r = append(r, link("doc:"+g.ID, "  ", titleOr(g.Title, g.ID), "", w))
	}
	if len(wp.Notes) > 0 {
		r = append(r, head(fmt.Sprintf("Notes (%d)", len(wp.Notes))))
	}
	for _, n := range wp.Notes {
		r = append(r, plain("  "+truncate(n, w-4)))
	}
	if len(wp.OpenRFX) > 0 {
		r = append(r, head(fmt.Sprintf("Open asks (%d)", len(wp.OpenRFX))))
	}
	for _, x := range wp.OpenRFX {
		var ts []string
		for _, t := range x.Targets {
			ts = append(ts, string(t.Medium)+" "+state(string(t.State)))
		}
		r = append(r, link("doc:"+x.ID, "  "+stDim.Render(string(x.Direction))+"  ", titleOr(x.Title, x.ID), "  "+strings.Join(ts, ", "), w))
	}
	if len(wp.OpenTasks) > 0 {
		r = append(r, head(fmt.Sprintf("Assigned tasks (%d)", len(wp.OpenTasks))))
	}
	for _, t := range wp.OpenTasks {
		r = append(r, link("doc:"+t.ID, "  "+stDim.Render(stateWord(string(t.Status)))+"  ", titleOr(t.Title, t.ID), "  "+stDim.Render(dateWords(string(t.Due), m.snap.Loaded)), w))
	}
	if len(wp.Interactions) > 0 {
		r = append(r, head(fmt.Sprintf("Interactions (%d)", len(wp.Interactions))))
	}
	for _, i := range wp.Interactions {
		r = append(r, link("doc:"+i.ID, "  "+stDim.Render(dateWords(i.Date, m.snap.Loaded)+" "+string(i.Medium))+"  ", titleOr(i.Title, i.ID), "", w))
	}
	return r
}

func (m *Model) rowsWho(p *page, w int) []row {
	r := []row{head(fmt.Sprintf("%d people match %q", len(p.who.Candidates), p.id))}
	for _, c := range p.who.Candidates {
		r = append(r, link("person:"+c.ID, "  ", titleOr(c.Title, c.ID), "", w))
	}
	return r
}

func (m *Model) rowsResults(p *page, w int) []row {
	r := []row{head(fmt.Sprintf("%s (%d)", p.title, len(p.hits)))}
	if p.degraded != "" {
		r = append(r, dim("  Meaning search did not answer: "+strings.TrimPrefix(p.degraded, "exact only: ")))
	}
	if len(p.hits) == 0 {
		r = append(r, dim("  Nothing matches. Try fewer words, or search by a name."))
	}
	for _, h := range p.hits {
		flag := ""
		if h.Unverified {
			flag = "  " + state("unverified")
		}
		if h.Match == "meaning" {
			flag += "  " + stDim.Render("found by meaning")
		}
		r = append(r, link("doc:"+h.ID, "  "+stDim.Render(h.Kind)+"  ", titleOr(h.Title, h.ID), flag, w))
		if h.Via != "" {
			if n := m.snap.name(h.Via); n != "" {
				r = append(r, dim("      text from "+n))
			}
		}
		if h.Caveat != "" {
			r = append(r, dim("      caveat: "+truncate(oneLine(h.Caveat), max(w-16, 10))))
		}
		if h.Snippet != "" {
			r = append(r, plain("      "+truncate(highlight(h.Snippet, h.Marks), max(w-8, 10))))
		}
	}
	return r
}

// highlight bolds the matched spans of a snippet.
func highlight(snippet string, marks []index.Span) string {
	rs := []rune(snippet)
	var b strings.Builder
	pos := 0
	for _, mk := range marks {
		if mk.Start < pos || mk.End > len(rs) || mk.Start >= mk.End {
			continue
		}
		b.WriteString(stDim.Render(string(rs[pos:mk.Start])))
		b.WriteString(stMatch.Render(string(rs[mk.Start:mk.End])))
		pos = mk.End
	}
	b.WriteString(stDim.Render(string(rs[pos:])))
	return b.String()
}

// rowsGraph is the DAG followed by one keyed row per node.
func (m *Model) rowsGraph(p *page, w int) []row {
	g := m.snap.Graph
	keep := map[string]bool{}
	projectArea := map[string]string{}
	for _, pr := range m.snap.Projects {
		projectArea[pr.ID] = pr.Area
	}
	for _, n := range g.Nodes {
		if p.id == "" || n.Project == p.id || n.Area == p.id || (n.Project != "" && projectArea[n.Project] == p.id) {
			keep[n.ID] = true
		}
	}
	var nodes []DNode
	var nrows []ops.GraphNode
	for _, n := range g.Nodes {
		if keep[n.ID] {
			label := titleOr(n.Title, n.ID)
			if n.Type == model.TypeQuestion {
				label = "? " + label
			}
			nodes = append(nodes, DNode{ID: n.ID, Label: label})
			nrows = append(nrows, n)
		}
	}
	var edges []DEdge
	for _, e := range g.Edges {
		if keep[e.From] && keep[e.To] {
			// A prerequisite sits above what depends on it.
			edges = append(edges, DEdge{From: e.To, To: e.From})
		}
	}
	var cycles [][]string
	for _, c := range g.Cycles {
		in := true
		for _, id := range c {
			in = in && keep[id]
		}
		if in {
			cycles = append(cycles, c)
		}
	}
	title := "Graph: all tasks"
	if p.id != "" {
		title = "Graph: " + titleOr(m.snap.name(p.id), "one goal")
	}
	r := []row{head(title)}
	if len(nodes) == 0 {
		return append(r, dim("  no tasks"))
	}
	for _, l := range strings.Split(strings.TrimRight(RenderDAG(nodes, edges, cycles, w-2), "\n"), "\n") {
		r = append(r, plain(" "+l))
	}
	sort.SliceStable(nrows, func(i, j int) bool { return nrows[i].ID < nrows[j].ID })
	r = append(r, head("Nodes"))
	if hasQuestion(nrows) {
		r = append(r, dim("  ? marks a question that holds back what depends on it"))
	}
	for _, n := range nrows {
		pre := "  " + state(n.State) + "  "
		if n.Type == model.TypeQuestion {
			pre = "  " + stWarn.Render("? "+string(n.Kind)) + "  " + state(n.State) + "  "
		}
		r = append(r, link("doc:"+n.ID, pre, titleOr(n.Title, n.ID), "", w))
	}
	return r
}

func hasQuestion(ns []ops.GraphNode) bool {
	for _, n := range ns {
		if n.Type == model.TypeQuestion {
			return true
		}
	}
	return false
}

// rowsAgenda is the Agenda page: dated items by day in local time, meetings
// with their hours, and a link to the document on each.
func (m *Model) rowsAgenda(w int) []row {
	a := m.snap.Agenda
	r := []row{head(fmt.Sprintf("Agenda by date, %s to %s   %s", dateWords(a.From, m.snap.Loaded), dateWords(a.To, m.snap.Loaded), a.Zone))}
	if len(a.Items) == 0 {
		return append(r, dim("  Nothing is dated in the next "+fmt.Sprint(AgendaDays)+" days."))
	}
	day := ""
	for _, it := range a.Items {
		if it.Date != day {
			day = it.Date
			name := dateWords(day, m.snap.Loaded)
			if t, err := time.Parse("2006-01-02", day); err == nil {
				name = t.Weekday().String()[:3] + " " + name
			}
			if day == m.snap.Date {
				name += "  today"
			}
			r = append(r, head(name))
		}
		when := string(it.Kind)
		switch it.Kind {
		case ops.AgendaMeeting:
			when = it.Time
			if t, err := time.Parse(time.RFC3339, it.Until); err == nil {
				when += "-" + t.Format("15:04")
			}
		case ops.AgendaDue:
			when = "due"
		}
		pre := "  " + stDim.Render(pad(when, 12)) + " "
		post := ""
		if names := joinNames(it.WhoNames); names != "" {
			post += "  " + stDim.Render("with "+names)
		}
		if it.ProjectName != "" {
			post += "  " + stDim.Render(it.ProjectName)
		}
		if it.Overdue {
			post += "  " + stBad.Render("overdue")
		}
		if it.Kind == ops.AgendaDue && it.Detail != "" {
			post += "  " + stWarn.Render(it.Detail)
		}
		x := link("doc:"+it.ID, pre, titleOr(it.Title, it.ID), post, w)
		// A dated row keeps the actions of its kind: the same as in value order.
		if q, ok := m.findQuestion(it.ID); ok {
			x.kind, x.id, x.proposed = "question", it.ID, q.HasProposal
		} else if _, ok := m.findTask(it.ID); ok {
			x.kind, x.id = "task", it.ID
		}
		r = append(r, x)
	}
	return r
}
