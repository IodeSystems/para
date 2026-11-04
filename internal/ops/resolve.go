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
	Outcome model.TaskOutcome `json:"outcome,omitempty"` // done (default), partial, dropped, duplicate, or superseded
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
	Warnings      []string `json:"warnings"`
}

// TaskResolve closes a task with an outcome. done is task_done, recurrence
// included. The others close the task without claiming it was finished, and a
// dependent never waits on a closed item that did not finish.
var TaskResolve = defineSafe("task_resolve", "Close a task with an outcome: done (default), partial, dropped, duplicate, or superseded. partial needs a follow-up or a note, dropped a note, duplicate and superseded a link (an id). follow_ups are titles of new tasks in the same goal or area; dependents move to the first one.", func(ctx context.Context, d Deps, in TaskResolveInput) (TaskResolveOutput, error) {
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
	if _, recurring := recurRule(t); recurring && outcome != model.OutcomeDone && outcome != model.OutcomeDropped {
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
	out := TaskResolveOutput{ID: id, Outcome: outcome, FollowUps: []string{}, Moved: []string{}, Released: []string{}, Warnings: []string{}, OpenItems: []string{}, OpenQuestions: []string{}}
	if items, qs, err := openChildren(ctx, d.Store, id); err != nil {
		return TaskResolveOutput{}, err
	} else {
		out.OpenItems, out.OpenQuestions = append(out.OpenItems, items...), append(out.OpenQuestions, qs...)
		if len(items)+len(qs) > 0 {
			out.Warnings = append(out.Warnings, fmt.Sprintf("%s has %d open action items and %d open questions; they stay open: finish, drop, or move each", id, len(items), len(qs)))
		}
	}
	if _, recurring := recurRule(t); outcome == model.OutcomeDone || (recurring && outcome == model.OutcomeDropped) {
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
	target := ""
	switch {
	case outcome == model.OutcomeDropped:
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
	default:
		if target == link {
			for _, dep := range dp.by[id] {
				if path, err := dependencyPath(ctx, d.Store, link, dep); err != nil || path != nil {
					return out, fmt.Errorf("task_resolve: %s waits on %s; moving %s to it would make a cycle", link, dep, dep)
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
	var lines []string
	switch outcome {
	case model.OutcomeDuplicate:
		lines = append(lines, "Duplicate of [["+link+"]].")
	case model.OutcomeSuperseded:
		lines = append(lines, "Superseded by [["+link+"]].")
	}
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
	return out, nil
})

func wikilinks(ids []string) string {
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = "[[" + id + "]]"
	}
	return strings.Join(parts, ", ")
}
