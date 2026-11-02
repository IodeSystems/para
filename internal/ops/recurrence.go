package ops

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/recur"
)

// A recurring task has a `recur` value (daily, weekly, monthly, every <n>
// days). Marking it done creates the next task, with the due date advanced and
// `previous` linking back. There is no daemon: nothing happens until a person
// or an agent calls task_done (or task_done with skip).

// recurWarning explains an unknown recur value; empty when it is empty or valid.
func recurWarning(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if _, err := recur.Parse(s); err != nil {
		return err.Error() + ": stored as written, and para will not create the next task"
	}
	return ""
}

const outcomeHeading = "## Outcome"

// TaskDoneInput is the input of task_done.
type TaskDoneInput struct {
	ID   string `json:"id"`
	Skip bool   `json:"skip,omitempty"` // a recurring task: advance without doing it (this one is dropped)
	Note string `json:"note,omitempty"` // how it ended; stored under ## Outcome
}

// TaskDoneOutput is the output of task_done. Next is the id of the next
// occurrence of a recurring task; Request is the request made for it when it
// is assigned to an agent.
type TaskDoneOutput struct {
	ID        string           `json:"id"`
	Status    model.TaskStatus `json:"status"`
	BlockedBy []string         `json:"blocked_by"`
	Rev       string           `json:"rev"`
	Next      string           `json:"next,omitempty"`
	Request   string           `json:"request,omitempty"`
	// DoneWhen is the condition the task was finished against, if it had one.
	DoneWhen string   `json:"done_when,omitempty"`
	Warnings []string `json:"warnings"`
}

// TaskDone marks a task done and clears its blockers. A recurring task also
// gets its next occurrence. It is safe to repeat on a recurring task: the next
// occurrence is made once. With Skip the task is dropped, not done, and the
// next occurrence is made all the same.
var TaskDone = define("task_done", "Mark a task done; a recurring task also gets its next occurrence (skip advances without doing)", func(ctx context.Context, d Deps, in TaskDoneInput) (TaskDoneOutput, error) {
	id := normID(in.ID)
	doc, t, err := getTask(ctx, d.Store, id)
	if err != nil {
		return TaskDoneOutput{}, err
	}
	rule, recurring := recurRule(t)
	if in.Skip && !recurring {
		return TaskDoneOutput{}, fmt.Errorf("%s does not recur: skip is for a recurring task (recur: %s)", id, recur.Forms)
	}
	out := TaskDoneOutput{ID: id, Warnings: []string{}}
	if w := recurWarning(t.Recur); w != "" {
		out.Warnings = append(out.Warnings, w)
	}
	if taskTerminal(t.Status) {
		// A repeat is a success only for a recurring task that has a next one.
		next, err := successor(ctx, d, id)
		if !recurring || next == "" || err != nil {
			if err != nil {
				return TaskDoneOutput{}, err
			}
			return TaskDoneOutput{}, fmt.Errorf("%s is already %s", id, t.Status)
		}
		out.Status, out.BlockedBy, out.Rev, out.Next = t.Status, append([]string{}, t.BlockedBy...), doc.Rev, next
		return out, nil
	}
	if recurring {
		// Make the next task first: if the next step fails, this task is still
		// open, and a retry finds the successor instead of making another.
		next, req, warns, err := makeNext(ctx, d, doc, t, rule)
		if err != nil {
			return TaskDoneOutput{}, err
		}
		out.Next, out.Request = next, req
		out.Warnings = append(out.Warnings, warns...)
	}
	t.Status = model.TaskDone
	if in.Skip {
		t.Status = model.TaskDropped
	}
	t.BlockedBy = nil
	if note := strings.TrimSpace(in.Note); note != "" {
		doc.Body = appendToSection(doc.Body, outcomeHeading, note)
	}
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return TaskDoneOutput{}, err
	}
	out.Status, out.BlockedBy, out.Rev, out.DoneWhen = t.Status, []string{}, saved.Rev, t.DoneWhen
	return out, nil
})

func recurRule(t *model.Task) (recur.Rule, bool) {
	if strings.TrimSpace(t.Recur) == "" {
		return recur.Rule{}, false
	}
	r, err := recur.Parse(t.Recur)
	return r, err == nil
}

// successor returns the id of the task that recurred from id, or "".
func successor(ctx context.Context, d Deps, id string) (string, error) {
	all, err := listTasks(ctx, d.Store, "", "")
	if err != nil {
		return "", err
	}
	for _, t := range all {
		if t.Previous == id {
			return t.ID, nil
		}
	}
	return "", nil
}

// makeNext creates the next occurrence of a recurring task, or returns the one
// that exists. The due date moves one step from the old due date (from today
// when there is none), then on to the first step that is not in the past. A
// project that was closed gets no next task: a warning says so.
func makeNext(ctx context.Context, d Deps, doc model.Document, t *model.Task, rule recur.Rule) (id, request string, warns []string, err error) {
	if existing, err := successor(ctx, d, doc.ID); err != nil || existing != "" {
		return existing, "", nil, err
	}
	if p := t.Project.Target(); p != "" {
		proj, _, err := getProject(ctx, d.Store, p)
		if err != nil {
			return "", "", nil, fmt.Errorf("next task: %w", err)
		}
		if proj.Folder == model.FolderArchive {
			return "", "", []string{"project " + p + " is closed: no next task was made"}, nil
		}
	}
	today := d.Store.Now().Local()
	from := today
	if t.Due != "" {
		if from, err = parseDay(string(t.Due)); err != nil {
			return "", "", nil, fmt.Errorf("next task: %w", err)
		}
	}
	due := rule.Next(from, today).Format(recur.Layout)
	next, err := d.Store.Create(ctx, model.Document{
		Folder: model.FolderProjects,
		Title:  doc.Title,
		Body:   doc.Body,
		Meta: &model.Task{
			Common:   model.Common{Type: model.TypeTask},
			Status:   model.TaskTodo,
			Project:  t.Project,
			Area:     t.Area,
			Due:      model.Date(due),
			Recur:    t.Recur,
			Assignee: t.Assignee,
			DoneWhen: t.DoneWhen,
			Previous: model.LinkTo(doc.ID),
		},
	})
	if err != nil {
		return "", "", nil, err
	}
	request, warns = agentRequest(ctx, d, next, nil)
	return next.ID, request, warns, nil
}

// agentRequest queues a request for a recurring task assigned to an agent,
// through request_add. A failure is a warning, not an error: the task exists.
func agentRequest(ctx context.Context, d Deps, doc model.Document, warns []string) (string, []string) {
	t, ok := doc.Meta.(*model.Task)
	if !ok || !strings.HasPrefix(t.Assignee, AgentPrefix) {
		return "", warns
	}
	if _, ok := recurRule(t); !ok {
		return "", warns
	}
	ask := fmt.Sprintf("%s\n\nRecurring task %s, assigned to %s, due %s.", doc.Title, doc.ID, t.Assignee, dashOr(string(t.Due)))
	out, err := RequestAdd.Run(ctx, d, RequestAddInput{Title: doc.Title, Ask: ask, Links: []string{doc.ID}})
	if err != nil {
		return "", append(warns, "task "+doc.ID+" is assigned to "+t.Assignee+" but no request was made: "+err.Error())
	}
	return out.ID, warns
}

func dashOr(s string) string {
	if s == "" {
		return "no date"
	}
	return s
}

func parseDay(s string) (time.Time, error) { return time.ParseInLocation(recur.Layout, s, time.Local) }
