package tui

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/lipgloss/v2"

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
	event  model.Event
	draw   func(k KeyFn) string
}

var (
	stHead   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	stDim    = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	stGood   = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	stWarn   = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	stBad    = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	stActive = lipgloss.NewStyle().Bold(true).Underline(true)
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

// link builds a selectable row: pre, a keyed label, post. The label is cut to
// fit w cells before its key letter is chosen, so the key is always visible.
func link(target, pre, label, post string, w int) row {
	return row{sel: true, target: target, draw: func(k KeyFn) string {
		room := max(w-cells(pre)-cells(post)-2, 8)
		return pre + k(target, truncate(label, room)) + post
	}}
}

func state(s string) string {
	switch s {
	case "ready", "done", "answered", "verified", "current", "replied", "queued", "claimed", "adopted", "resolved", "accepted":
		return stGood.Render(s)
	case "blocked", "failed", "stalled", "missing", "changed", "declined", "no_reply", "overdue":
		return stBad.Render(s)
	case "waiting", "unverified", "proposed", "drafted", "sent", "open", "due", "todo", "doing":
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

// rowsToday is the home screen.
func (m *Model) rowsToday(w int) []row {
	s := m.snap
	var r []row
	if !m.loaded {
		return []row{dim("loading...")}
	}
	r = append(r, head(fmt.Sprintf("Today %s   vault %s", s.Date, s.Vault)))
	section := func(title string, items []row) {
		r = append(r, head(fmt.Sprintf("%s (%d)", title, len(items))))
		if len(items) == 0 {
			r = append(r, dim("  none"))
		}
		r = append(r, items...)
	}
	dated := func(items []today.Item) []row {
		var rows []row
		for _, it := range items {
			rows = append(rows, link("doc:"+it.ID, "  ", titleOr(it.Title, it.ID), "  "+stDim.Render(it.Due), w))
		}
		return rows
	}
	if len(s.Today.Incidents.Items) > 0 { // incidents come first, and only when there are some
		var inc []row
		for _, it := range s.Today.Incidents.Items {
			inc = append(inc, link("doc:"+it.ID, "  "+stBad.Render(it.Detail)+"  ", titleOr(it.Title, it.ID), "", w))
		}
		section("Incidents", inc)
	}
	section("Overdue", dated(s.Today.Overdue.Items))
	section("Due soon", dated(s.Today.DueSoon.Items))
	var nudge []row
	for _, it := range s.Today.Nudges.Items {
		nudge = append(nudge, link("doc:"+it.ID, "  ", titleOr(it.Title, it.ID), "", w))
	}
	section("Nudge due", nudge)
	var ready []row
	for _, t := range s.Next {
		ready = append(ready, link("doc:"+t.ID, "  ", titleOr(t.Title, t.ID), "  "+stDim.Render(t.Project), w))
	}
	section("Ready tasks", ready)
	var blocked []row
	for _, it := range s.Today.Blocked.Items {
		blocked = append(blocked, link("doc:"+it.ID, "  ", titleOr(it.Title, it.ID), "", w))
	}
	section("Blocked", blocked)
	r = append(r, head(fmt.Sprintf("Waiting on others (%d)", s.Today.Waiting)))
	var exp []row
	for _, it := range s.Today.Expiring.Items {
		exp = append(exp, link("doc:"+it.ID, "  ", titleOr(it.Title, it.ID), "", w))
	}
	section("Expiring exceptions", exp)
	var due []row
	for _, it := range s.Today.Due.Items {
		due = append(due, link("doc:"+it.ID, "  ", titleOr(it.Title, it.ID), "", w))
	}
	section("Constraints due for review", due)
	var inbox []row
	for _, it := range s.Inbox {
		inbox = append(inbox, link("doc:"+it.ID, "  ", titleOr(it.Title, it.ID), "  "+stDim.Render(string(it.Created)), w))
	}
	section("Inbox", inbox)
	lint := stGood.Render("lint clean")
	if s.LintErrors > 0 {
		lint = stBad.Render(plural(s.LintErrors, "lint error"))
	}
	r = append(r, head("Vault"))
	r = append(r, plain("  "+lint+"   "+plural(s.Today.Unverified, "unverified summary")))
	for _, c := range s.Today.SyncConflicts {
		r = append(r, plain("  "+stBad.Render(truncate("sync conflict  "+c, w-4))))
	}
	for _, d := range s.Today.LostUpdates {
		r = append(r, plain("  "+stBad.Render(truncate("lost update  "+d, w-4))))
	}
	for _, e := range s.Errs {
		r = append(r, plain("  "+stBad.Render(truncate(e, w-4))))
	}
	if s.Today.Error != "" {
		r = append(r, plain("  "+stBad.Render(truncate("vault "+s.Vault+": "+s.Today.Error, w-4))))
	}
	for _, o := range s.Others {
		if o.Error != "" {
			r = append(r, plain("  "+stBad.Render(truncate("vault "+o.Vault+": "+o.Error, w-4))))
			continue
		}
		r = append(r, dim(fmt.Sprintf("  vault %s: incidents %d, due %d, nudges %d, blocked %d, inbox %d", o.Vault, o.Incidents.Count, o.Due.Count, o.Nudges.Count, o.Blocked.Count, o.Inbox)))
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
	pos := "the beginning"
	if !m.pos.At.IsZero() {
		pos = m.pos.At.Local().Format("2006-01-02 15:04")
	}
	r = append(r, head(fmt.Sprintf("Changed by others since %s (%d)", pos, rv.Count)))
	if rv.Count == 0 {
		r = append(r, dim("  nothing new"))
	}
	for _, g := range rv.Groups {
		r = append(r, plain("  "+stHead.Render(g.Actor)+stDim.Render(fmt.Sprintf("  %d events", len(g.Events)))))
		for _, e := range g.Events {
			e := e
			doc := e.Doc
			x := row{sel: doc != "", kind: "event", id: doc, event: e}
			if doc != "" {
				x.target = "doc:" + doc
			}
			x.draw = func(k KeyFn) string {
				pre := "    " + stDim.Render(e.TS.Local().Format("01-02 15:04")) + " " + e.Op + " "
				if doc == "" {
					return pre
				}
				return pre + k(x.target, truncate(doc, max(w-cells(pre)-2, 8)))
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
	r = append(r, head(fmt.Sprintf("Requests (%d open)", reqs)))
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
	return r
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
	taskRow := func(t ops.TaskView) row {
		label := state(st[t.ID])
		if st[t.ID] == "" {
			label = state(string(t.Status))
		}
		if t.Due != "" {
			label += stDim.Render("  due " + string(t.Due))
		}
		tx := link("doc:"+t.ID, "      ", titleOr(t.Title, t.ID), "  "+label, w)
		tx.id = t.ID
		return tx
	}
	linkRows := func(indent string, links []model.Pointer) {
		for _, k := range links {
			r = append(r, dim(truncate(fmt.Sprintf("%s%s: %s  %s", indent, k.Kind, k.Label, k.Target), w)))
		}
	}
	projectRows := func(p ops.ProjectView) {
		x := link("doc:"+p.ID, "  ", titleOr(p.Title, p.ID), "  "+state(string(p.Status))+stDim.Render(fmt.Sprintf("  %d tasks", len(byProject[p.ID]))), w)
		x.id = p.ID
		r = append(r, x)
		linkRows("      ", p.Links)
		for _, t := range byProject[p.ID] {
			r = append(r, taskRow(t))
		}
	}
	r = append(r, head(fmt.Sprintf("Areas (%d)", len(s.Areas))))
	if len(s.Areas) == 0 {
		r = append(r, dim("  none"))
	}
	for _, a := range s.Areas {
		x := link("doc:"+a.ID, "  ", titleOr(a.Title, a.ID), "  "+state(string(a.Status)), w)
		x.id = a.ID
		r = append(r, x)
		linkRows("    ", a.Links)
		for _, p := range s.Projects {
			if p.Area == a.ID {
				projectRows(p)
			}
		}
		for _, t := range byArea[a.ID] {
			r = append(r, taskRow(t))
		}
	}
	inArea := map[string]bool{}
	for _, a := range s.Areas {
		inArea[a.ID] = true
	}
	r = append(r, head(fmt.Sprintf("Projects with no area (%d)", countNoArea(s.Projects, inArea))))
	for _, p := range s.Projects {
		if !inArea[p.Area] {
			projectRows(p)
		}
	}
	if len(s.Cycles) > 0 {
		r = append(r, head("Cycles"))
		for _, c := range s.Cycles {
			r = append(r, plain("  "+stBad.Render(strings.Join(c, " -> ")+" -> "+c[0])))
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
	rows := m.rowsWork(m.w)
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
	status := map[string]model.RFXStatus{}
	for _, x := range s.RFX {
		status[x.ID] = x.Status
	}
	for _, it := range s.Waiting {
		flags := ""
		switch {
		case it.Stalled:
			flags += "  " + state("stalled")
		case it.Due:
			flags += "  " + state("due")
		}
		if it.NextNudge != "" {
			flags += stDim.Render("  nudge " + it.NextNudge)
		}
		if _, ok := s.Drafts[it.ID]; ok {
			flags += "  " + state("drafted")
		}
		x := link("doc:"+it.ID, "  "+stDim.Render(string(it.Direction))+"  ", titleOr(it.Title, it.ID), flags, w)
		x.id = it.ID
		r = append(r, x)
		for _, t := range it.Targets {
			to := t.To.Target()
			r = append(r, link("person:"+to, "      to ", to, "  "+stDim.Render(string(t.Medium))+"  "+state(string(t.State)), w))
		}
		if len(it.BlockedOn) > 0 {
			r = append(r, dim("      after "+strings.Join(it.BlockedOn, ", ")))
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
		o := link("doc:"+x.ID, "  "+stDim.Render(string(x.Direction))+"  ", titleOr(x.Title, x.ID), "  "+state(string(x.Status)), w)
		o.id = x.ID
		other = append(other, o)
	}
	r = append(r, head(fmt.Sprintf("Other requests (%d)", len(other))))
	r = append(r, other...)
	return r
}

func (m *Model) rowsPeople(w int) []row {
	if !m.loaded {
		return []row{dim("loading...")}
	}
	r := []row{head(fmt.Sprintf("People (%d)   / searches by name, alias, or contact", len(m.snap.People)))}
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
		r = append(r, link("doc:"+ru.Policy, "  "+stDim.Render(ru.ID+" "+ru.Policy)+"  ", ru.Text, "", w))
	}
	r = append(r, head(fmt.Sprintf("Exceptions (%d)", len(s.Exceptions))))
	for _, e := range s.Exceptions {
		review := ""
		if e.ReviewBy != "" {
			review = stDim.Render("  review " + string(e.ReviewBy))
		}
		r = append(r, link("doc:"+e.ID, "  "+state(string(e.Status))+"  ", titleOr(e.Title, e.ID), stDim.Render("  "+string(e.Kind))+review, w))
	}
	r = append(r, head(fmt.Sprintf("Constraints (%d)", len(s.Constrs))))
	for _, c := range s.Constrs {
		review := ""
		if c.ReviewBy != "" {
			review = stDim.Render("  review " + string(c.ReviewBy))
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
	r = append(r, link("doc:"+wp.ID, "  ", "open "+wp.ID, "", w))
	if len(wp.Aliases) > 0 {
		r = append(r, plain("  aka "+strings.Join(wp.Aliases, ", ")))
	}
	if wp.Role != nil {
		r = append(r, link("doc:"+wp.Role.ID, "  role  ", titleOr(wp.Role.Title, wp.Role.ID), "", w))
	}
	if wp.Team != nil {
		r = append(r, link("doc:"+wp.Team.ID, "  team  ", titleOr(wp.Team.Title, wp.Team.ID), "", w))
	}
	r = append(r, head(fmt.Sprintf("Contacts (%d)", len(wp.Contacts))))
	for _, c := range wp.Contacts {
		pref := ""
		if c.Preferred {
			pref = stGood.Render("  preferred")
		}
		r = append(r, plain(fmt.Sprintf("  %-10s %s%s", c.Method, c.Value, pref)))
	}
	r = append(r, head(fmt.Sprintf("Groups (%d)", len(wp.Groups))))
	for _, g := range wp.Groups {
		r = append(r, link("doc:"+g.ID, "  ", titleOr(g.Title, g.ID), "", w))
	}
	r = append(r, head(fmt.Sprintf("Notes (%d)", len(wp.Notes))))
	for _, n := range wp.Notes {
		r = append(r, plain("  "+truncate(n, w-4)))
	}
	r = append(r, head(fmt.Sprintf("Open RFX (%d)", len(wp.OpenRFX))))
	for _, x := range wp.OpenRFX {
		var ts []string
		for _, t := range x.Targets {
			ts = append(ts, string(t.Medium)+" "+state(string(t.State)))
		}
		r = append(r, link("doc:"+x.ID, "  "+stDim.Render(string(x.Direction))+"  ", titleOr(x.Title, x.ID), "  "+strings.Join(ts, ", "), w))
	}
	r = append(r, head(fmt.Sprintf("Assigned tasks (%d)", len(wp.OpenTasks))))
	for _, t := range wp.OpenTasks {
		r = append(r, link("doc:"+t.ID, "  "+stDim.Render(string(t.Status))+"  ", titleOr(t.Title, t.ID), "  "+stDim.Render(string(t.Due)), w))
	}
	r = append(r, head(fmt.Sprintf("Interactions (%d)", len(wp.Interactions))))
	for _, i := range wp.Interactions {
		r = append(r, link("doc:"+i.ID, "  "+stDim.Render(i.Date+" "+string(i.Medium))+"  ", titleOr(i.Title, i.ID), "", w))
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
	for _, h := range p.hits {
		flag := ""
		if h.Unverified {
			flag = "  " + state("unverified")
		}
		r = append(r, link("doc:"+h.ID, "  "+stDim.Render(h.Kind)+"  ", titleOr(h.Title, h.ID), flag, w))
		if h.Passage != "" {
			r = append(r, dim("      "+truncate(oneLine(h.Passage), max(w-8, 10))))
		}
	}
	return r
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
			nodes = append(nodes, DNode{ID: n.ID, Label: titleOr(n.Title, n.ID)})
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
		title = "Graph: " + p.id
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
	for _, n := range nrows {
		r = append(r, link("doc:"+n.ID, "  "+state(n.State)+"  ", titleOr(n.Title, n.ID), "", w))
	}
	return r
}
