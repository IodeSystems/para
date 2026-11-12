package ops

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/recur"
	"github.com/iodesystems/para/internal/store"
)

// Crons (plan/paralog.md § 10). A cron is recurring work on a project: an
// obligation series (schedule series, recur) with project:. It reuses the
// series: each occurrence is a file of its own, the next made when one
// closes, and only the install named by request.runner ticks. What each run
// makes is makes:
//
//   - obligation (the default): the occurrence itself, as any series. The
//     owner's lands in Next; an agent's is produce work (produce.go).
//   - task: on the occurrence's day tick makes a task with cron: [[the
//     occurrence]], under goal: when the cron names a goal of the project,
//     else with project: (the one task that links a project directly), and
//     closes the occurrence, which makes the next. The owner's task lands in
//     Next; an agent's is assigned to it with agent work.
//
// The schedule is the cron file in the vault. What fired is the task that
// links the occurrence and the closed occurrence, both vault content, so a
// second pass, on this machine or after a sync, makes no second task.

// isCron reports whether the obligation is a cron: a series on a project.
func isCron(o *model.Obligation) bool {
	return o.Schedule == model.ScheduleSeries && o.Project != ""
}

// cronSeries is the id of the cron an occurrence belongs to: its first.
func cronSeries(id string, o *model.Obligation) string { return cmp.Or(o.OccurrenceOf.Target(), id) }

// checkCron checks makes on obligation_add: a cron is a commitment series on
// a project; one that makes a task has no produce block, and an agent that
// owes it is named (agent:<name>), since the task is assigned to it.
func checkCron(in ObligationAddInput, kind model.TaskKind, sched model.Schedule, owedBy []model.Link) error {
	switch in.Makes {
	case "":
		return nil
	case model.MakesObligation, model.MakesTask:
	default:
		return fmt.Errorf("makes %q: want obligation or task", in.Makes)
	}
	switch {
	case sched != model.ScheduleSeries || normID(in.Project) == "":
		return errors.New("makes is a cron's: a series (recur) on a project (project)")
	case in.Makes == model.MakesObligation:
		return nil
	case kind != model.KindCommitment:
		return errors.New("makes task: a meeting is attended; a cron that makes a task is a commitment")
	case in.Produce != nil:
		return errors.New("makes task: the task is the work, so give no produce (produce is for makes obligation)")
	case slices.Contains(owedBy, model.OwedByAgent):
		return errors.New("makes task: the task is assigned to the agent that owes it: give owed_by agent:<name>")
	case len(owedBy) > 1:
		return errors.New("makes task: a task has one assignee: give one owed_by")
	}
	return nil
}

// goalOfProject refuses a goal that is not part of the project.
func goalOfProject(ctx context.Context, s store.Store, goal, project string) error {
	_, g, err := getGoal(ctx, s, goal)
	if err != nil {
		return err
	}
	if g.Project.Target() != project {
		pd, err := s.Get(ctx, g.Project.Target())
		if err != nil || pd.ID != project {
			return fmt.Errorf("goal %s is not part of project %s: a cron makes its tasks under a goal of its project", goal, project)
		}
	}
	return nil
}

// tickCrons fires the crons that make a task and whose occurrence is due.
func tickCrons(ctx context.Context, d Deps, out *TickOutput) error {
	if !obligationsOn(d) {
		return nil
	}
	refs, err := d.Store.List(ctx, store.Filter{Folder: model.FolderObligations, Type: model.TypeObligation})
	if err != nil {
		return err
	}
	now := d.Store.Now().Local()
	today := now.Format(recur.Layout)
	var made map[string]string // occurrence -> the task that links it
	for _, ref := range refs {
		doc, err := d.Store.Get(ctx, ref.ID)
		if err != nil {
			return err
		}
		o, ok := doc.Meta.(*model.Obligation)
		if !ok || !o.Open() || !isCron(o) || o.Makes != model.MakesTask || o.IsDeferred(today) {
			continue
		}
		if due, _ := produceDue(o, now); !due {
			continue
		}
		if made == nil {
			if made, err = cronTasks(ctx, d.Store); err != nil {
				return err
			}
		}
		task := made[doc.ID]
		if task == "" {
			var warn string
			if task, warn, err = cronTask(ctx, d, doc, o); err != nil {
				return fmt.Errorf("%s: %w", doc.ID, err)
			}
			if warn != "" {
				out.Warnings = append(out.Warnings, warn)
			}
			if task == "" {
				continue
			}
			made[doc.ID] = task
			if req := cronRequest(ctx, d, task, doc.Title, o, out); req != "" {
				out.noteRequest(ctx, d, req)
			}
		}
		// Closing the occurrence makes the next one: the series moves on.
		wctx := WithWrite(ctx, WriteMeta{Why: "Tick: cron ran", Confidence: model.ConfidenceHigh})
		if _, err := obligationClose.Run(wctx, d, obligationCloseInput{ID: doc.ID, Outcome: string(model.OutcomeDone), Reason: "cron ran: made " + task}); err != nil {
			out.Warnings = append(out.Warnings, doc.ID+": made "+task+" but could not close the occurrence: "+err.Error())
			continue
		}
		out.Crons = append(out.Crons, doc.ID+": "+task)
	}
	return nil
}

// cronTasks maps each cron occurrence to the task that links it (cron:),
// archived tasks too.
func cronTasks(ctx context.Context, s store.Store) (map[string]string, error) {
	refs, err := s.List(ctx, store.Filter{Type: model.TypeTask, All: true})
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, r := range refs {
		doc, err := s.Get(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		if t, ok := doc.Meta.(*model.Task); ok && t.Cron != "" {
			out[t.Cron.Target()] = doc.ID
		}
	}
	return out, nil
}

// cronTask makes the task of one run. A goal that is gone or closed sends
// the task to the project, with a warning.
func cronTask(ctx context.Context, d Deps, doc model.Document, o *model.Obligation) (id, warn string, err error) {
	t := &model.Task{Common: model.Common{Type: model.TypeTask, Tags: slices.Clone(o.Tags)}, Status: model.TaskTodo,
		Cron: model.LinkTo(doc.ID), Due: o.Due, DoneWhen: o.DoneWhen, Effort: o.Effort}
	if g := o.Goal.Target(); g != "" {
		gd, gm, gerr := getGoal(ctx, d.Store, g)
		switch {
		case gerr != nil:
			warn = fmt.Sprintf("cron %s: goal %s: %v; the task links the project", doc.ID, g, gerr)
		case lifeOf(gm) == LifeClosed && gm.Status != model.GoalParked:
			warn = fmt.Sprintf("cron %s: goal %s is %s; the task links the project", doc.ID, gd.ID, gm.Status)
		default:
			t.Goal = model.LinkTo(gd.ID)
		}
	}
	if t.Goal == "" {
		t.Project = o.Project
	}
	if len(o.OwedBy) > 0 {
		t.Assignee = o.OwedBy[0].Target()
	}
	title := doc.Title
	if o.Due != "" {
		title += " (" + string(o.Due) + ")"
	}
	saved, err := d.Store.Create(ctx, model.Document{Folder: model.FolderGoals, Title: title, Body: doc.Body, Meta: t})
	if err != nil {
		return "", warn, err
	}
	return saved.ID, warn, nil
}

// cronRequest queues agent work for a cron task assigned to an agent: queued
// or proposed as request.surfaces says for tick.
func cronRequest(ctx context.Context, d Deps, task, title string, o *model.Obligation, out *TickOutput) string {
	agent := o.Agent()
	if agent == "" {
		return ""
	}
	ask := fmt.Sprintf("Cron task %s of project %s, assigned to %s, due %s.", task, o.Project.Target(), agent, dashOr(string(o.Due)))
	if o.DoneWhen != "" {
		ask += " Done when: " + o.DoneWhen
	}
	req, err := addRequest(ctx, d, RequestAddInput{Title: "Cron: " + shorten(title), Ask: ask, Links: []string{task}, Kind: RecurringKind}, "")
	if err != nil {
		out.Warnings = append(out.Warnings, "cron task "+task+" is assigned to "+agent+" but no request was made: "+err.Error())
		return ""
	}
	return req.ID
}

// Bundle § crons: one line per cron of the project, at most cronLinesMax,
// within cronBytesMax.
const (
	cronLinesMax = 5
	cronBytesMax = 800
)

// cronState is one cron of a project as the bundle reads it.
type cronState struct {
	id, recur, makes, owed string
	next, last, outcome    string
}

// line is the bundle line: "<id> <recur>: makes <what>; <who>; next <date>; last <date> <outcome>".
func (c cronState) line() string {
	l := c.id + " " + c.recur + ": makes " + c.makes + "; " + cmp.Or(c.owed, "owner")
	l += "; next " + cmp.Or(c.next, "none")
	if c.last != "" {
		l += "; last " + c.last + " " + c.outcome
	}
	return clip(l, cronBytesMax/cronLinesMax)
}

// projectCrons returns the bundle lines of the crons of a project and how
// many were left out. mine says whether a project link is this project.
func projectCrons(ctx context.Context, s store.Store, mine func(string) bool) ([]string, int, error) {
	refs, err := s.List(ctx, store.Filter{Type: model.TypeObligation, All: true})
	if err != nil {
		return nil, 0, err
	}
	crons := map[string]*cronState{}
	for _, r := range refs {
		doc, err := s.Get(ctx, r.ID)
		if err != nil {
			continue
		}
		o, ok := doc.Meta.(*model.Obligation)
		if !ok || !isCron(o) || !mine(o.Project.Target()) {
			continue
		}
		sid := cronSeries(doc.ID, o)
		c := crons[sid]
		if c == nil {
			c = &cronState{id: sid}
			crons[sid] = c
		}
		if o.Open() {
			// The open occurrence holds the schedule as it is now.
			c.recur, c.next = o.Recur, string(cmp.Or(o.Due, model.Date(o.Scheduled)))
			c.makes = string(cmp.Or(o.Makes, model.MakesObligation))
			if o.Makes == model.MakesTask {
				c.makes = "task"
				if g := o.Goal.Target(); g != "" {
					c.makes += " under " + g
				}
			}
			c.owed = strings.Join(targets(o.OwedBy), ",")
			continue
		}
		if at := string(cmp.Or(o.Closed, o.Due)); at > c.last {
			c.last, c.outcome = at, string(o.Outcome)
		}
	}
	ids := make([]string, 0, len(crons))
	for id, c := range crons {
		if c.recur != "" { // a series with no open occurrence has ended
			ids = append(ids, id)
		}
	}
	slices.SortFunc(ids, func(a, b string) int {
		return cmp.Or(cmpDue(model.Date(crons[a].next), model.Date(crons[b].next)), cmp.Compare(a, b))
	})
	var lines []string
	size := 0
	for _, id := range ids {
		l := crons[id].line()
		if len(lines) == cronLinesMax || size+len(l) > cronBytesMax {
			break
		}
		lines, size = append(lines, l), size+len(l)
	}
	return lines, len(ids) - len(lines), nil
}
