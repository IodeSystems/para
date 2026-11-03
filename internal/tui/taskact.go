package tui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/ops"
)

// Row actions on a task. They work from a list row and from an opened task.
// Letters: not j k g q (fixed keys), not the question letters, not the document
// page letters.
var (
	actResolve  = Target{ID: "act:resolve", Label: "Resolve", Pref: 'r'}
	actStart    = Target{ID: "act:start", Label: "Start", Pref: 't'}
	actBlock    = Target{ID: "act:block", Label: "Block", Pref: 'o'}
	actAssign   = Target{ID: "act:assign", Label: "Assign", Pref: 'n'}
	actDelegate = Target{ID: "act:delegate", Label: "Give to agent", Pref: 'i'}
	actDue      = Target{ID: "act:due", Label: "Due", Pref: 'd'}
	// Update has no free letter in its label: u is Cut. m is free on Today and
	// Work, so the key shows after the label.
	actUpdate = Target{ID: "act:update", Label: "Update", Pref: 'm', Fixed: true}
)

// taskActLetters are the preferred letters of the task actions. A document page
// keeps its link keys off them.
const taskActLetters = "rtonidum"

// taskActs are the task-only actions, in footer order. Later, Cut, and Find
// context are shared with other rows.
var taskActs = []Target{actResolve, actStart, actBlock, actAssign, actDelegate, actDue, actUpdate}

// keepTaskAct reports whether a task action applies to the task.
func keepTaskAct(t Target, task ops.TaskView) bool {
	if taskDone(task) {
		return false
	}
	return t.ID != actStart.ID || task.Status != model.TaskDoing
}

func taskDone(t ops.TaskView) bool {
	return t.Status == model.TaskDone || t.Status == model.TaskDropped
}

// taskTarget is the open task an action applies to.
func (m *Model) taskTarget(verb string) (ops.TaskView, bool) {
	t, ok := m.cursorTarget()
	if !ok || t.kind != "task" || taskDone(t.t) {
		m.setHint("Move to an open task to " + verb + " it.")
		return ops.TaskView{}, false
	}
	return t.t, true
}

// taskAct runs a task action by name. ok is false for any other name.
func (m *Model) taskAct(name string) (tea.Cmd, bool) {
	switch name {
	case "resolve":
		return m.resolveTask(), true
	case "start":
		return m.startTask(), true
	case "block":
		return m.blockTask(), true
	case "assign":
		return m.assignTask(), true
	case "delegate":
		return m.delegateTask(), true
	case "due":
		return m.dueTask(), true
	case "update":
		return m.updateTask(), true
	}
	return nil, false
}

// runOp runs a write off the UI loop and reports its text.
func (m *Model) runOp(f func(ctx context.Context) (string, error)) tea.Cmd {
	return func() tea.Msg {
		text, err := f(context.Background())
		if err != nil {
			return doneMsg{err: err}
		}
		return doneMsg{text: text, reload: true}
	}
}

func (m *Model) startTask() tea.Cmd {
	t, ok := m.taskTarget("start")
	if !ok {
		return nil
	}
	env := m.env
	return m.runOp(func(ctx context.Context) (string, error) {
		if _, err := ops.TaskStart.Run(ctx, env.Deps, ops.TaskStartInput{ID: t.ID}); err != nil {
			return "", err
		}
		return fmt.Sprintf("Started %q.", titleOr(t.Title, t.ID)), nil
	})
}

func (m *Model) blockTask() tea.Cmd {
	t, ok := m.taskTarget("block")
	if !ok {
		return nil
	}
	env := m.env
	lines := []string{"  " + titleOr(t.Title, t.ID)}
	steps := []promptStep{{label: "blocked because (who said it, and where it applies): "}}
	return m.askForLines("Block", lines, steps, func(v []string) tea.Cmd {
		return m.runOp(func(ctx context.Context) (string, error) {
			if _, err := ops.TaskBlock.Run(ctx, env.Deps, ops.TaskBlockInput{ID: t.ID, Reason: v[0]}); err != nil {
				return "", err
			}
			return fmt.Sprintf("Blocked %q.", titleOr(t.Title, t.ID)), nil
		})
	})
}

func (m *Model) assignTask() tea.Cmd {
	t, ok := m.taskTarget("assign")
	if !ok {
		return nil
	}
	env := m.env
	lines := []string{"  " + titleOr(t.Title, t.ID), "  Para stores the name and sends nothing. Use Give to agent to queue agent work."}
	steps := []promptStep{{short: true, label: "assign to (self, a person id, or agent:<name>): ", value: t.Assignee}}
	return m.askForLines("Assign", lines, steps, func(v []string) tea.Cmd {
		return m.runOp(func(ctx context.Context) (string, error) {
			o, err := ops.TaskAssign.Run(ctx, env.Deps, ops.TaskAssignInput{ID: t.ID, Assignee: v[0]})
			if err != nil {
				return "", err
			}
			who := o.Assignee
			if who == "" {
				who = ops.SelfAssignee
			}
			return fmt.Sprintf("%q is assigned to %s.", titleOr(t.Title, t.ID), who), nil
		})
	})
}

func (m *Model) dueTask() tea.Cmd {
	t, ok := m.taskTarget("set the due date of")
	if !ok {
		return nil
	}
	env := m.env
	lines := []string{"  " + titleOr(t.Title, t.ID)}
	steps := []promptStep{{short: true, label: "due YYYY-MM-DD (empty removes the date): ", value: string(t.Due), optional: true}}
	return m.askForLines("Due", lines, steps, func(v []string) tea.Cmd {
		return m.runOp(func(ctx context.Context) (string, error) {
			in := ops.TaskDueInput{ID: t.ID, Date: v[0], Clear: v[0] == ""}
			if _, err := ops.TaskDue.Run(ctx, env.Deps, in); err != nil {
				return "", err
			}
			if in.Clear {
				return fmt.Sprintf("Removed the due date of %q.", titleOr(t.Title, t.ID)), nil
			}
			return fmt.Sprintf("%q is due %s.", titleOr(t.Title, t.ID), v[0]), nil
		})
	})
}

func (m *Model) delegateTask() tea.Cmd {
	t, ok := m.taskTarget("give to an agent")
	if !ok {
		return nil
	}
	env := m.env
	what := "Queues agent work linked to the task, with its done condition. The agent reports with sources; you check it."
	if m.snap.Working[t.ID] {
		what = "An agent is working on it already. The hint goes into its work."
	}
	lines := []string{"  " + titleOr(t.Title, t.ID), "  " + what}
	steps := []promptStep{
		{short: true, label: "agent name (optional; default request.runner): ", optional: true},
		{label: "hint: where to start (optional): ", optional: true},
	}
	return m.askForLines("Give to agent", lines, steps, func(v []string) tea.Cmd {
		return m.runOp(func(ctx context.Context) (string, error) {
			o, err := ops.TaskDelegate.Run(ctx, env.Deps, ops.TaskDelegateInput{ID: t.ID, Agent: v[0], Hint: v[1]})
			if err != nil {
				return "", err
			}
			verb := "Queued agent work " + o.Request + " for"
			if o.Appended {
				verb = "Added your hint to " + o.Request + " for"
			}
			text := fmt.Sprintf("%s %q. It is listed under Agent work.", verb, titleOr(t.Title, t.ID))
			if o.Note != "" {
				text += " " + o.Note
			}
			return text, nil
		})
	})
}

// updateTask adds free text to a task. The agent sorts it into a proposal.
func (m *Model) updateTask() tea.Cmd {
	t, ok := m.taskTarget("add an update to")
	if !ok {
		return nil
	}
	env := m.env
	lines := []string{"  " + titleOr(t.Title, t.ID), "  Kept whole under Updates. An agent sorts it into action items, questions, commitments, policy, and follow-ups for you to approve."}
	steps := []promptStep{{label: "update: "}}
	return m.askForLines("Update", lines, steps, func(v []string) tea.Cmd {
		return m.runOp(func(ctx context.Context) (string, error) {
			o, err := ops.TaskUpdate.Run(ctx, env.Deps, ops.TaskUpdateInput{ID: t.ID, Text: v[0]})
			if err != nil {
				return "", err
			}
			text := fmt.Sprintf("Saved your update on %q.", titleOr(t.Title, t.ID))
			switch {
			case o.Appended:
				text += " Added to the open sort work " + o.Request + "."
			case o.Request != "":
				text += " Queued agent work " + o.Request + " to sort it."
			}
			if o.Note != "" {
				text += " " + o.Note
			}
			return text, nil
		})
	})
}

// outcomeFrom reads what was typed at the outcome step: a full word, or a
// prefix that names one outcome. Empty is done.
func outcomeFrom(s string) (model.TaskOutcome, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return model.OutcomeDone, true
	}
	var hit model.TaskOutcome
	n := 0
	for _, o := range model.TaskOutcomes {
		if string(o) == s {
			return o, true
		}
		if strings.HasPrefix(string(o), s) {
			hit, n = o, n+1
		}
	}
	return hit, n == 1
}

// resolveTask closes a task. The outcome defaults to done: r, then enter, is the
// whole job. The other outcomes ask for what they need.
func (m *Model) resolveTask() tea.Cmd {
	t, ok := m.taskTarget("resolve")
	if !ok {
		return nil
	}
	env := m.env
	out := func(v []string) model.TaskOutcome { o, _ := outcomeFrom(v[0]); return o }
	not := func(os ...model.TaskOutcome) func(v []string) bool {
		return func(v []string) bool {
			for _, o := range os {
				if out(v) == o {
					return false
				}
			}
			return true
		}
	}
	lines := []string{"  " + titleOr(t.Title, t.ID), "  Enter closes it as done. The first letters of another outcome is enough: pa, dr, du, su."}
	if t.DoneWhen != "" {
		lines = append(lines, "  Done when: "+t.DoneWhen)
	}
	steps := []promptStep{
		{short: true, label: "outcome (enter for done; or partial, dropped, duplicate, superseded): ", optional: true},
		{label: "why it ends this way (partial: or give a follow-up; dropped: required): ", skip: not(model.OutcomePartial, model.OutcomeDropped, model.OutcomeDuplicate, model.OutcomeSuperseded), optional: true},
		{short: true, label: "the id of the item it duplicates or that supersedes it: ", skip: not(model.OutcomeDuplicate, model.OutcomeSuperseded)},
		{label: "follow-up task title (optional; separate several with ;): ", skip: not(model.OutcomePartial, model.OutcomeDuplicate, model.OutcomeSuperseded), optional: true},
		{label: "change the done condition (optional): ", value: t.DoneWhen, skip: func(v []string) bool { return t.DoneWhen == "" || out(v) == model.OutcomeDone }, optional: true},
	}
	return m.askForLines("Resolve", lines, steps, func(v []string) tea.Cmd {
		o, ok := outcomeFrom(v[0])
		if !ok {
			return func() tea.Msg {
				return doneMsg{err: fmt.Errorf("unknown outcome %q: want done, partial, dropped, duplicate, or superseded", v[0])}
			}
		}
		in := ops.TaskResolveInput{ID: t.ID, Outcome: o, Note: v[1], Link: v[2], DoneWhen: v[4]}
		if in.DoneWhen == t.DoneWhen {
			in.DoneWhen = ""
		}
		for _, f := range strings.Split(v[3], ";") {
			if f = strings.TrimSpace(f); f != "" {
				in.FollowUps = append(in.FollowUps, f)
			}
		}
		return m.runOp(func(ctx context.Context) (string, error) {
			r, err := ops.TaskResolve.Run(ctx, env.Deps, in)
			if err != nil {
				return "", err
			}
			text := fmt.Sprintf("Closed %q as %s.", titleOr(t.Title, t.ID), r.Outcome)
			if len(r.FollowUps) > 0 {
				text += fmt.Sprintf(" Made %d follow-up.", len(r.FollowUps))
			}
			if len(r.Moved) > 0 {
				text += fmt.Sprintf(" %d waiting item moved to it.", len(r.Moved))
			}
			if len(r.Released) > 0 {
				text += fmt.Sprintf(" %d waiting item released.", len(r.Released))
			}
			return text, nil
		})
	})
}
