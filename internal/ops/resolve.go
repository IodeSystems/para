package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/iodesystems/para/internal/model"
)

// TaskResolveInput is the input of task_resolve.
type TaskResolveInput struct {
	ID      string            `json:"id"`
	Outcome model.TaskOutcome `json:"outcome,omitempty"` // done (default), partial, dropped, duplicate, superseded, declined, failed, or rerouted
	// Note says how it ended. partial needs a note or a follow-up; dropped
	// needs a note. It is stored under ## Outcome.
	Note string `json:"note,omitempty"`
	// Link is the id of the item that duplicates or supersedes this task.
	Link string `json:"link,omitempty"`
	// FollowUps are titles of new open tasks in the same goal or area. Open
	// dependents move to the first one (partial, duplicate, superseded).
	FollowUps []string `json:"follow_ups,omitempty"`
	// DoneWhen changes the done condition before closing; the old text goes
	// under ## Outcome.
	DoneWhen string `json:"done_when,omitempty"`
	// Due is the due date of the follow-up tasks, YYYY-MM-DD.
	Due string `json:"due,omitempty"`
	// Reason is why it was declined or rerouted (required for both).
	Reason string `json:"reason,omitempty"`
	// DeclinedBy is owner or other (declined); DeclinedPerson is the id of
	// the person who said no, optional.
	DeclinedBy     model.DeclinedBy `json:"declined_by,omitempty"`
	DeclinedPerson string           `json:"declined_person,omitempty"`
	// Source is where the no was said: a document id or a URL. Date is the
	// day it was said, YYYY-MM-DD. declined needs one of them.
	Source string `json:"source,omitempty"`
	Date   string `json:"date,omitempty"`
	// Tried and StoppedBecause say what was tried and why it stopped (failed,
	// both required). RetryAfter is when it may be tried again, YYYY-MM-DD.
	Tried          string `json:"tried,omitempty"`
	StoppedBecause string `json:"stopped_because,omitempty"`
	RetryAfter     string `json:"retry_after,omitempty"`
	// To is where a rerouted task went: a vault document id, or a URL of an
	// outside system.
	To string `json:"to,omitempty"`
}

// AskEffect is an open Ask a close touched. Relation says how it hangs on
// the task: blocks (the task waits on it), depends_on (it waits on the task),
// or project (its project link names the task). Action is closed (declined or
// dropped closed it) or needs_decision (failed or rerouted: decide each).
type AskEffect struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Relation string `json:"relation"`
	Action   string `json:"action"`
}

// TaskResolveOutput is the output of task_resolve.
type TaskResolveOutput struct {
	ID      string            `json:"id"`
	Status  model.TaskStatus  `json:"status"`
	Outcome model.TaskOutcome `json:"outcome"`
	Rev     string            `json:"rev"`
	// FollowUps are the ids of the tasks made.
	FollowUps []string `json:"follow_ups"`
	// Moved are the dependents that now wait on the first follow-up (or the
	// link). Released are the dependents of a dropped task: nothing holds them.
	Moved    []string `json:"moved"`
	Released []string `json:"released"`
	Next     string   `json:"next,omitempty"`
	Request  string   `json:"request,omitempty"`
	// OpenItems and OpenQuestions are open action items and open questions of
	// the task. They stay open; decide each.
	OpenItems     []string `json:"open_items"`
	OpenQuestions []string `json:"open_questions"`
	// Asks are the open Asks that hang on this task only (declined, failed,
	// rerouted, dropped): closed, or listed as needing a decision.
	Asks     []AskEffect `json:"asks"`
	Warnings []string    `json:"warnings"`
	// Merge is what outcome duplicate merged into the link (doc_merge).
	Merge *DocMergeOutput `json:"merge,omitempty"`
}

// TaskResolve closes a task with an outcome. done is task_done, recurrence
// included. The others close the task without claiming it was finished, and a
// dependent never waits on a closed item that did not finish.
var TaskResolve = withOneReceipt(defineSafe("task_resolve", TaskResolveDescription, func(ctx context.Context, d Deps, in TaskResolveInput) (TaskResolveOutput, error) {
	id := normID(in.ID)
	outcome := in.Outcome
	if outcome == "" {
		outcome = model.OutcomeDone
	}
	if !slices.Contains(model.TaskOutcomes, outcome) {
		return TaskResolveOutput{}, fmt.Errorf("unknown outcome %q: want one of %v", outcome, model.TaskOutcomes)
	}
	note := strings.TrimSpace(in.Note)
	link := normID(in.Link)
	var titles []string
	for _, f := range in.FollowUps {
		if f = strings.Join(strings.Fields(f), " "); f != "" {
			titles = append(titles, f)
		}
	}
	switch outcome {
	case model.OutcomePartial:
		if len(titles) == 0 && note == "" {
			return TaskResolveOutput{}, errors.New("partial needs a follow-up task or a note that says why there is none")
		}
	case model.OutcomeDropped:
		if note == "" {
			return TaskResolveOutput{}, errors.New("dropped needs a note: why it is not needed")
		}
	case model.OutcomeDuplicate, model.OutcomeSuperseded:
		if link == "" {
			return TaskResolveOutput{}, fmt.Errorf("%s needs link: the id of the item that %s this task", outcome, map[model.TaskOutcome]string{model.OutcomeDuplicate: "duplicates", model.OutcomeSuperseded: "supersedes"}[outcome])
		}
	}
	cf, err := checkClose(ctx, d, id, outcome, in)
	if err != nil {
		return TaskResolveOutput{}, err
	}
	due, err := parseDue(in.Due)
	if err != nil {
		return TaskResolveOutput{}, err
	}
	doc, t, err := getTask(ctx, d.Store, id)
	if err != nil {
		return TaskResolveOutput{}, err
	}
	if err := closeGuard(doc, t, outcome); err != nil {
		return TaskResolveOutput{}, err
	}
	if taskTerminal(t.Status) && outcome != model.OutcomeDone {
		return TaskResolveOutput{}, fmt.Errorf("%s is already %s", id, t.Status)
	}
	if link != "" {
		if link == id {
			return TaskResolveOutput{}, fmt.Errorf("%s cannot be a duplicate of itself", id)
		}
		if _, err := dependencyLink(ctx, d.Store, link); err != nil {
			return TaskResolveOutput{}, fmt.Errorf("link: %s", strings.TrimPrefix(err.Error(), "depends_on "))
		}
	}
	merge := false
	if outcome == model.OutcomeDuplicate {
		// A duplicate of a task merges into it; check before anything is written.
		if ldoc, err := d.Store.Get(ctx, link); err == nil && sameKind(ldoc.Meta.Header().Type, model.TypeTask) {
			if _, _, err := mergeCheck(ctx, d, link, id); err != nil {
				return TaskResolveOutput{}, err
			}
			merge = true
		}
	}
	if _, recurring := recurRule(t.Recur); recurring && outcome != model.OutcomeDone && outcome != model.OutcomeDropped {
		return TaskResolveOutput{}, fmt.Errorf("%s recurs: resolve it done, or dropped to skip this one", id)
	}
	// A changed condition is recorded before closing; the old text stays.
	if dw := strings.TrimSpace(in.DoneWhen); dw != "" && dw != t.DoneWhen {
		dw, err := checkDoneWhen(dw, true)
		if err != nil {
			return TaskResolveOutput{}, err
		}
		was := t.DoneWhen
		if was == "" {
			was = "(none)"
		}
		note = strings.TrimSpace(note + "\nDone when changed. Was: " + was + "\nNow: " + dw)
		if !taskTerminal(t.Status) {
			t.DoneWhen = dw
			if _, err = d.Store.Update(ctx, doc); err != nil {
				return TaskResolveOutput{}, err
			}
		}
	}
	out := TaskResolveOutput{ID: id, Outcome: outcome, FollowUps: []string{}, Moved: []string{}, Released: []string{}, Warnings: []string{}, OpenItems: []string{}, OpenQuestions: []string{}, Asks: []AskEffect{}}
	if items, qs, err := openChildren(ctx, d.Store, id); err != nil {
		return TaskResolveOutput{}, err
	} else {
		out.OpenItems, out.OpenQuestions = append(out.OpenItems, items...), append(out.OpenQuestions, qs...)
		if len(items)+len(qs) > 0 {
			out.Warnings = append(out.Warnings, fmt.Sprintf("%s has %d open action items and %d open questions; they stay open: finish, drop, or move each", id, len(items), len(qs)))
		}
	}
	if _, recurring := recurRule(t.Recur); outcome == model.OutcomeDone || (recurring && outcome == model.OutcomeDropped) {
		r, err := finishTask(ctx, d, TaskDoneInput{ID: id, Note: note, Skip: outcome == model.OutcomeDropped}, outcome)
		if err != nil {
			return TaskResolveOutput{}, err
		}
		out.Status, out.Rev, out.Next, out.Request = r.Status, r.Rev, r.Next, r.Request
		out.Warnings = append(out.Warnings, r.Warnings...)
		return out, nil
	}
	// Follow-ups first: if a later step fails, the task is still open.
	for _, title := range titles {
		fu, err := d.Store.Create(ctx, model.Document{
			Folder: model.FolderProjects, Title: title, Body: "Follow-up of [[" + id + "]].",
			Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskTodo,
				Project: t.Project, Area: t.Area, Incident: t.Incident, Due: due},
		})
		if err != nil {
			return out, fmt.Errorf("task_resolve: follow-up %q: %w (made: %v)", title, err, out.FollowUps)
		}
		out.FollowUps = append(out.FollowUps, fu.ID)
	}
	// The Asks are found before dependents move: an Ask that waits on this
	// task only is one of them.
	asks, err := asksOf(ctx, d.Store, id, t)
	if err != nil {
		return out, err
	}
	target := ""
	switch {
	case outcome == model.OutcomeDropped, outcome == model.OutcomeDeclined, outcome == model.OutcomeFailed:
	case outcome == model.OutcomeRerouted:
		target = cf.moveTo
	case len(out.FollowUps) > 0:
		target = out.FollowUps[0]
	case outcome != model.OutcomePartial:
		target = link
	}
	dp, err := loadDependents(ctx, d.Store)
	if err != nil {
		return out, err
	}
	switch target {
	case "":
		out.Released = append(out.Released, dp.by[id]...)
		if outcome == model.OutcomeRerouted && len(dp.by[id]) > 0 {
			out.Warnings = append(out.Warnings, fmt.Sprintf("%s went to %s, which is not a task or Ask in this vault: %d dependents were released, nothing holds them now: %s", id, cf.to, len(dp.by[id]), strings.Join(dp.by[id], ", ")))
		}
	default:
		if target == link || target == cf.moveTo {
			for _, dep := range dp.by[id] {
				if path, err := dependencyPath(ctx, d.Store, target, dep); err != nil || path != nil {
					return out, fmt.Errorf("task_resolve: %s waits on %s; moving %s to it would make a cycle", target, dep, dep)
				}
			}
		}
		moved, err := relinkDependents(ctx, d.Store, id, target)
		out.Moved = append(out.Moved, moved...)
		if err != nil {
			return out, fmt.Errorf("task_resolve: follow-ups were made but %s is still open: %w", id, err)
		}
	}
	// Re-read: the relink can have changed the Rev.
	doc, t, err = getTask(ctx, d.Store, id)
	if err != nil {
		return out, err
	}
	t.Status, t.Outcome, t.BlockedBy = model.TaskDropped, outcome, nil
	if outcome == model.OutcomePartial {
		t.Status = model.TaskDone
	}
	t.DeclinedBy, t.DeclinedPerson, t.RetryAfter, t.ReroutedTo = cf.declinedBy, cf.person, cf.retryAfter, cf.toField
	var lines []string
	switch outcome {
	case model.OutcomeDuplicate:
		lines = append(lines, "Duplicate of [["+link+"]].")
	case model.OutcomeSuperseded:
		lines = append(lines, "Superseded by [["+link+"]].")
	}
	lines = append(lines, cf.lines()...)
	if note != "" {
		lines = append(lines, note)
	}
	for _, f := range out.FollowUps {
		lines = append(lines, "Follow-up: [["+f+"]].")
	}
	if len(out.Released) > 0 {
		lines = append(lines, "Released, no longer waiting: "+wikilinks(out.Released)+".")
	}
	if len(lines) == 0 {
		lines = append(lines, string(outcome))
	}
	doc.Body = appendToSection(doc.Body, outcomeHeading, strings.Join(lines, "\n\n"))
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return out, err
	}
	out.Status, out.Rev = t.Status, saved.Rev
	if err := recordClose(ctx, d, id, string(outcome)); err != nil {
		return out, err
	}
	if merge {
		m, err := mergeDocs(ctx, d, link, id)
		if err != nil {
			return out, fmt.Errorf("task_resolve: %s is closed as a duplicate, but the merge into %s failed: %w", id, link, err)
		}
		out.Merge = &m
		if doc, err := d.Store.Get(ctx, id); err == nil {
			out.Rev = doc.Rev
		}
	}
	why := firstOf(cf.reason, cf.stopped, note)
	if out.Asks, err = settleAsks(ctx, d, id, outcome, why, asks); err != nil {
		return out, err
	}
	for _, a := range out.Asks {
		if a.Action == askNeedsDecision {
			out.Warnings = append(out.Warnings, fmt.Sprintf("Ask %s hangs on %s only and is still open: close it, reroute it, or keep it", a.ID, id))
		}
	}
	return out, nil
}))

func wikilinks(ids []string) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = "[[" + id + "]]"
	}
	return strings.Join(parts, ", ")
}
