package tui

import (
	"context"
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/ops"
)

// Row actions on a question, and Later and Cut on a task. They work from a
// list row and from an opened question, on Today, Work, and Review.
var (
	actDrop  = Target{ID: "act:drop", Label: "Drop", Pref: 'p'}
	actAsk   = Target{ID: "act:ask", Label: "Ask person", Pref: 's'}
	actFind  = Target{ID: "act:find", Label: "Investigate", Pref: 'f', Fixed: true}
	actLater = Target{ID: "act:later", Label: "Later", Pref: 'l'}
	actBack  = Target{ID: "act:later", Label: "Not later", Pref: 'l'}
	actCut   = Target{ID: "act:cut", Label: "Cut", Pref: 'u'}
)

// questionActLetters are the preferred letters of the question actions. A
// document page keeps its link keys off them.
const questionActLetters = "aepsfluw"

// questionActs are the actions of a question row, in footer order.
var questionActs = []Target{actAnswer, actReject, actReword, actDrop, actAsk, actLater, actCut}

// actTarget is what a row action applies to: an open question or a task.
type actTarget struct {
	kind string // "question" or "task"
	id   string
	q    ops.QuestionView
	t    ops.TaskView
}

func (m *Model) findQuestion(id string) (ops.QuestionView, bool) {
	for _, list := range [][]ops.QuestionView{m.snap.Questions, m.snap.Later} {
		for _, q := range list {
			if q.ID == id {
				return q, true
			}
		}
	}
	return ops.QuestionView{}, false
}

func (m *Model) findTask(id string) (ops.TaskView, bool) {
	for _, t := range m.snap.Tasks {
		if t.ID == id {
			return t, true
		}
	}
	for _, t := range m.snap.Next {
		if t.ID == id {
			return t, true
		}
	}
	return ops.TaskView{}, false
}

// cursorTarget is the question or task the row actions apply to: the opened
// question, or the row under the cursor on a tab.
func (m *Model) cursorTarget() (actTarget, bool) {
	if p := m.top(); p != nil {
		if p.kind != pageDoc {
			return actTarget{}, false
		}
		if _, ok := p.doc.Meta.(*model.Task); ok {
			if t, ok := m.findTask(p.id); ok {
				return actTarget{kind: "task", id: p.id, t: t}, true
			}
			o, err := ops.TaskShow.Run(context.Background(), m.env.Deps, ops.TaskShowInput{ID: p.id})
			return actTarget{kind: "task", id: p.id, t: o.Task}, err == nil
		}
		if _, ok := p.doc.Meta.(*model.Question); !ok {
			return actTarget{}, false
		}
		q, ok := m.findQuestion(p.id)
		return actTarget{kind: "question", id: p.id, q: q}, ok
	}
	if m.tab != tabToday && m.tab != tabWork && m.tab != tabReview {
		return actTarget{}, false
	}
	r, ok := m.current()
	if !ok {
		return actTarget{}, false
	}
	switch r.kind {
	case "question":
		q, ok := m.findQuestion(r.id)
		return actTarget{kind: "question", id: r.id, q: q}, ok
	case "task":
		t, ok := m.findTask(r.id)
		return actTarget{kind: "task", id: r.id, t: t}, ok
	}
	return actTarget{}, false
}

// targetQuestion is the open question an action applies to, with a hint when
// there is none.
func (m *Model) targetQuestion(verb string) (ops.QuestionView, bool) {
	t, ok := m.cursorTarget()
	if !ok || t.kind != "question" {
		m.setHint("Move to an open question to " + verb + " it.")
		return ops.QuestionView{}, false
	}
	return t.q, true
}

// shownActs filters the actions of a list for the row the cursor is on: a
// question gets its actions, a task gets Later and Cut, other rows none. Keys
// do not change with the cursor; only the footer does.
func (m *Model) shownActs(acts []Target) []Target {
	var keep func(t Target) bool
	tg, ok := m.cursorTarget()
	switch {
	case ok && tg.kind == "question":
		keep = func(t Target) bool {
			switch t.ID {
			case actReject.ID:
				return tg.q.HasProposal
			case actAnswer.ID:
				return true
			case actCut.ID:
				return len(tg.q.Blocks) > 0 || len(tg.q.WaitingOn) > 0
			case actLater.ID:
				return true
			}
			return true
		}
	case ok && tg.kind == "task":
		keep = func(t Target) bool {
			if slicesContainsID(taskActs, t.ID) {
				return keepTaskAct(t, tg.t)
			}
			return t.ID == actLater.ID || t.ID == actCut.ID && len(m.taskWaits(tg.t)) > 0
		}
	}
	_, _, canFind := m.findTarget()
	var out []Target
	for _, t := range acts {
		isQ := slicesContainsID(questionActs, t.ID) || slicesContainsID(taskActs, t.ID)
		switch {
		case t.ID == actFind.ID:
			if canFind {
				out = append(out, t)
			}
		case !isQ:
			out = append(out, t)
		case keep != nil && keep(t):
			if t.ID == actLater.ID && (tg.q.Deferred || tg.t.Deferred) {
				t = actBack
			}
			out = append(out, t)
		}
	}
	return out
}

func slicesContainsID(ts []Target, id string) bool {
	for _, t := range ts {
		if t.ID == id {
			return true
		}
	}
	return false
}

// taskWaits lists what a task waits on that is not resolved.
func (m *Model) taskWaits(t ops.TaskView) []string {
	for _, n := range m.snap.Graph.Nodes {
		if n.ID == t.ID {
			return n.WaitingOn
		}
	}
	return nil
}

func (m *Model) name(id string) string { return titleOr(m.snap.name(id), id) }

// drop asks why, then drops the question.
func (m *Model) dropQuestion() tea.Cmd {
	q, ok := m.targetQuestion("drop")
	if !ok {
		return nil
	}
	steps := []promptStep{{label: "why nobody needs the answer: "}}
	return m.askForLines("Drop: "+titleOr(q.Title, q.ID), []string{"  Tasks that wait on it stop waiting."}, steps, func(v []string) tea.Cmd {
		env := m.env
		return func() tea.Msg {
			if _, err := ops.QuestionDrop.Run(context.Background(), env.Deps, ops.QuestionDropInput{ID: q.ID, Reason: v[0]}); err != nil {
				return doneMsg{err: err}
			}
			return doneMsg{text: fmt.Sprintf("Dropped %q.", titleOr(q.Title, "the question")), reload: true}
		}
	})
}

// askPerson makes an outbound request for the question.
func (m *Model) askPerson() tea.Cmd {
	q, ok := m.targetQuestion("ask a person about")
	if !ok {
		return nil
	}
	if q.RFX != "" {
		m.setHint("This question was asked already: see Asks.")
		return nil
	}
	lines := []string{"  " + titleOr(q.Title, q.ID)}
	route := ""
	if len(m.snap.Domains) == 1 {
		route = m.snap.Domains[0].ID
	}
	if len(m.snap.Domains) > 0 {
		var ds []string
		for _, d := range m.snap.Domains {
			ds = append(ds, d.ID)
		}
		lines = append(lines, "  routes: "+strings.Join(ds, ", "))
	}
	steps := []promptStep{
		{short: true, label: "ask whom (a person's name or id): "},
		{short: true, label: "by (email, slack, ...): "},
		{short: true, label: "route (the domain): ", value: route},
	}
	return m.askForLines("Ask a person", lines, steps, func(v []string) tea.Cmd {
		env := m.env
		in := ops.QuestionAskInput{ID: q.ID, To: v[0], Via: model.Medium(strings.ToLower(v[1])), Route: v[2]}
		return func() tea.Msg {
			o, err := ops.QuestionAsk.Run(context.Background(), env.Deps, in)
			if err != nil {
				return doneMsg{err: err}
			}
			return doneMsg{text: fmt.Sprintf("Made ask %s for %q. Para sends nothing: send it yourself.", o.RFX, titleOr(q.Title, "the question")), reload: true}
		}
	})
}

// findPrefixes are the id words of what an agent can be sent to find context
// for: tasks, meetings, questions, Asks, goals, and agent work.
var findPrefixes = []string{"task-", "meeting-", "question-", "rfx-", "project-", "request-"}

// findableID reports whether an id is of a type investigate accepts.
func findableID(id string) bool {
	for _, p := range findPrefixes {
		if strings.HasPrefix(id, p) {
			return true
		}
	}
	return false
}

// findTarget is the item Investigate applies to: the opened document, or the
// item row under the cursor.
func (m *Model) findTarget() (id, title string, ok bool) {
	if p := m.top(); p != nil && p.kind == pageDoc {
		if findableID(p.id) {
			return p.id, p.doc.Title, true
		}
		return "", "", false
	}
	r, found := m.current()
	if !found || r.kind == "event" || !strings.HasPrefix(r.target, "doc:") {
		return "", "", false
	}
	id = strings.TrimPrefix(r.target, "doc:")
	if !findableID(id) {
		return "", "", false
	}
	return id, titleOr(m.snap.name(id), id), true
}

// looking reports whether open agent work looks for context for the item.
func (s Snapshot) looking(id string) bool { return s.Looking[id] }

// findContext queues agent work that finds context for the item, or adds the
// hint to the work that is open. The hint may be left empty.
func (m *Model) findContext() tea.Cmd {
	id, title, ok := m.findTarget()
	if !ok {
		m.setHint("Move to a task, meeting, question, ask, goal, or agent work to investigate it.")
		return nil
	}
	what := "The agent files a proposal that adds context and sources to it. You check it."
	switch {
	case strings.HasPrefix(id, "question-"):
		what = "The agent proposes an answer with sources. You check it; the question stays open."
	case m.snap.looking(id):
		what = "An agent is looking already. The hint goes into its work."
	}
	lines := []string{"  " + titleOr(title, id), "  " + what}
	steps := []promptStep{{label: "what should the agent look into? (optional; for example: check the wiki, search #platform): ", optional: true}}
	return m.askForLines("Investigate", lines, steps, func(v []string) tea.Cmd {
		env := m.env
		return func() tea.Msg {
			o, err := ops.Investigate.Run(context.Background(), env.Deps, ops.InvestigateInput{ID: id, Hint: v[0]})
			if err != nil {
				return doneMsg{err: err}
			}
			verb := "Queued agent work " + o.Request + " for"
			if o.Appended {
				verb = "Added your hint to " + o.Request + " for"
			}
			return doneMsg{text: fmt.Sprintf("%s %q. It is listed under Agent work.", verb, titleOr(title, id)), reload: true}
		}
	})
}

// later puts the question or task off, or brings it back.
func (m *Model) later() tea.Cmd {
	tg, ok := m.cursorTarget()
	if !ok {
		m.setHint("Move to a question or a task to put it off.")
		return nil
	}
	title, deferred := tg.q.Title, tg.q.Deferred
	if tg.kind == "task" {
		title, deferred = tg.t.Title, tg.t.Deferred
	}
	env := m.env
	if deferred {
		return func() tea.Msg {
			if _, err := ops.Undefer.Run(context.Background(), env.Deps, ops.UndeferInput{IDs: []string{tg.id}}); err != nil {
				return doneMsg{err: err}
			}
			return doneMsg{text: fmt.Sprintf("%q is back.", titleOr(title, "It")), reload: true}
		}
	}
	steps := []promptStep{
		{short: true, label: "until: YYYY-MM-DD, someday, after <item id or words>, or when <condition>: ", value: "someday"},
		{label: "why it can wait (optional): ", optional: true},
		{short: true, label: "place for a new when task: goal or area id (optional): ", optional: true},
	}
	lines := []string{
		"  It leaves the Agenda and next. It still blocks what depends on it.",
		"  after <item>: waits until a task, question, ask, goal, or agent work is done.",
		"  when <condition>: makes a task with that done-when, in this goal or area, and waits on it.",
	}
	return m.askForLines("Later: "+titleOr(title, tg.id), lines, steps, func(v []string) tea.Cmd {
		in := ops.DeferInput{IDs: []string{tg.id}, Reason: v[1], Place: v[2]}
		word, rest, _ := strings.Cut(strings.TrimSpace(v[0]), " ")
		switch strings.ToLower(word) {
		case "after":
			id, err := m.resolveItem(rest)
			if err != nil {
				return func() tea.Msg { return doneMsg{err: err} }
			}
			in.After = id
		case "when":
			in.When = rest
		case strings.ToLower(model.SomedayWord):
		default:
			in.Until = v[0]
		}
		return func() tea.Msg {
			o, err := ops.Defer.Run(context.Background(), env.Deps, in)
			if err != nil {
				return doneMsg{err: err}
			}
			if len(o.Similar) > 0 {
				var sim []string
				for _, x := range o.Similar {
					sim = append(sim, x.ID+" "+x.Title)
				}
				return doneMsg{err: fmt.Errorf("a similar open task exists, nothing changed. Use: after <id> with one of: %s", strings.Join(sim, "; "))}
			}
			text := fmt.Sprintf("%q is back %s.", titleOr(title, "It"), laterWhen(o))
			if o.After != "" {
				text = fmt.Sprintf("%q waits for %q.", titleOr(title, "It"), titleOr(m.snap.name(o.After), o.After))
				if o.Created != "" {
					text = fmt.Sprintf("%q waits for new task %s.", titleOr(title, "It"), o.Created)
				}
			}
			return doneMsg{text: text, reload: true}
		}
	})
}

func laterWhen(o ops.DeferOutput) string {
	if o.Until != model.SomedayWord {
		return "on " + o.Until
	}
	return "someday"
}

// resolveItem finds the item named by an id or by words of its title, among
// tasks, questions, Asks, goals, and agent work. More than one match is an
// error that lists them.
func (m *Model) resolveItem(q string) (string, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return "", fmt.Errorf("after: give an item id or words of its title")
	}
	if _, ok := m.snap.Titles[q]; ok && findableID(q) {
		return q, nil
	}
	var hits []string
	for id, title := range m.snap.Titles {
		if findableID(id) && strings.Contains(strings.ToLower(title), strings.ToLower(q)) {
			hits = append(hits, id)
		}
	}
	sort.Strings(hits)
	switch len(hits) {
	case 0:
		return "", fmt.Errorf("after: no task, question, ask, goal, or agent work matches %q", q)
	case 1:
		return hits[0], nil
	}
	var list []string
	for _, id := range hits[:min(len(hits), 5)] {
		list = append(list, id+" "+m.snap.Titles[id])
	}
	return "", fmt.Errorf("after: %d items match %q, give an id: %s", len(hits), q, strings.Join(list, "; "))
}

// cutPair is one dependency that can be cut: waiter stops waiting on dep.
type cutPair struct{ waiter, dep string }

// cutPairs lists the dependencies a Cut can remove for the target. For a
// question: each task or question that waits on it, and what it waits on. For a
// task: what it waits on.
func (m *Model) cutPairs(tg actTarget) []cutPair {
	var out []cutPair
	if tg.kind == "question" {
		for _, b := range tg.q.Blocks {
			if _, isTask := m.findTask(b); isTask || strings.HasPrefix(b, "question-") {
				out = append(out, cutPair{b, tg.id})
			}
		}
		for _, w := range tg.q.WaitingOn {
			out = append(out, cutPair{tg.id, w})
		}
		return out
	}
	for _, w := range m.taskWaits(tg.t) {
		out = append(out, cutPair{tg.id, w})
	}
	return out
}

// cut removes one dependency, with the reason recorded as an assumption.
func (m *Model) cut() tea.Cmd {
	tg, ok := m.cursorTarget()
	if !ok {
		m.setHint("Move to a question or a task to cut what it waits on.")
		return nil
	}
	pairs := m.cutPairs(tg)
	if len(pairs) == 0 {
		m.setHint("Nothing waits on this or is waited on here.")
		return nil
	}
	var lines []string
	for i, p := range pairs {
		lines = append(lines, fmt.Sprintf("  %d  %s stops waiting on %s", i+1, m.name(p.waiter), m.name(p.dep)))
	}
	steps := []promptStep{{short: true, label: "cut which (number): "}}
	if len(pairs) == 1 {
		steps = []promptStep{{short: true, label: "cut it? (number 1): ", value: "1"}}
	}
	steps = append(steps, promptStep{label: "why it can go ahead without it: "})
	return m.askForLines("Cut a dependency", lines, steps, func(v []string) tea.Cmd {
		env := m.env
		return func() tea.Msg {
			n := 0
			if _, err := fmt.Sscanf(strings.TrimSpace(v[0]), "%d", &n); err != nil || n < 1 || n > len(pairs) {
				return doneMsg{err: fmt.Errorf("choose a number from 1 to %d", len(pairs))}
			}
			p := pairs[n-1]
			if _, err := ops.DependsCut.Run(context.Background(), env.Deps, ops.DependsCutInput{ID: p.waiter, Dep: p.dep, Reason: v[1]}); err != nil {
				return doneMsg{err: err}
			}
			return doneMsg{text: fmt.Sprintf("%q goes ahead without %q. The reason is on it as an assumption.", titleOr(m.snap.name(p.waiter), "It"), titleOr(m.snap.name(p.dep), "it")), reload: true}
		}
	})
}
