package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/iodesystems/para/internal/model"
)

// SplitDone is a part of a task that is done: it becomes a task that closes
// done at once. Evidence is what shows it done; an agent must give it.
type SplitDone struct {
	Title    string          `json:"title"`
	Evidence *model.Evidence `json:"evidence,omitempty"`
	Note     string          `json:"note,omitempty"`
}

// SplitRemaining is a part of a task that is not done: it becomes an open
// task. Due is YYYY-MM-DD.
type SplitRemaining struct {
	Title string `json:"title"`
	Body  string `json:"body,omitempty"`
	Due   string `json:"due,omitempty"`
}

// TaskSplitInput is the input of task_split.
type TaskSplitInput struct {
	ID        string           `json:"id"`
	Done      []SplitDone      `json:"done,omitempty"`
	Remaining []SplitRemaining `json:"remaining"`
	// Title is a new title for the task, now the umbrella of its parts.
	Title  string `json:"title,omitempty"`
	Reason string `json:"reason"`
}

// TaskSplitOutput is the output of task_split: the task, and the ids of the
// done and the remaining parts.
type TaskSplitOutput struct {
	ID        string   `json:"id"`
	Rev       string   `json:"rev"`
	Done      []string `json:"done"`
	Remaining []string `json:"remaining"`
}

// TaskSplitDescription is task_split's tool text.
const TaskSplitDescription = "Split a task that is partly done into its parts, in one write: done (each {title, evidence, note}: a task that closes done at once; evidence the owner can open, an agent must give it) and remaining (each {title, body, due}: an open task). " +
	"Each part is in the task's goal or area, an action item of the task (parent), and links it. The task stays open, waits on every remaining part (depends_on), and takes title when given; reason goes under ## Split. " +
	"Refused with no remaining part: close the task. One receipt; receipt_undo reverses all of it"

// TaskSplit splits an open task into done and remaining parts.
var TaskSplit = withOneReceipt(defineSafe("task_split", TaskSplitDescription, func(ctx context.Context, d Deps, in TaskSplitInput) (TaskSplitOutput, error) {
	id := normID(in.ID)
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		return TaskSplitOutput{}, errors.New("reason is empty: say what is done and what remains")
	}
	if len(in.Remaining) == 0 {
		return TaskSplitOutput{}, fmt.Errorf("%s has no remaining part: when all of it is done, close it (close {id: %s, evidence})", id, id)
	}
	doc, t, err := getTask(ctx, d.Store, id)
	if err != nil {
		return TaskSplitOutput{}, err
	}
	if taskTerminal(t.Status) {
		return TaskSplitOutput{}, fmt.Errorf("%s is already %s", id, t.Status)
	}
	title := ""
	if strings.TrimSpace(in.Title) != "" {
		if title, err = oneLine("title", in.Title); err != nil {
			return TaskSplitOutput{}, err
		}
	}
	// Check every part before anything is written.
	for i, p := range in.Done {
		if strings.TrimSpace(p.Title) == "" {
			return TaskSplitOutput{}, fmt.Errorf("done[%d]: title is empty", i)
		}
		if p.Evidence == nil {
			if agentSurface(d) {
				return TaskSplitOutput{}, fmt.Errorf("done[%d] %q: evidence is required: a URL, a vault id, or a file the owner can open", i, p.Title)
			}
			continue
		}
		if err := checkEvidence(ctx, d, *p.Evidence); err != nil {
			return TaskSplitOutput{}, fmt.Errorf("done[%d]: %w", i, err)
		}
	}
	dues := make([]model.Date, len(in.Remaining))
	for i, p := range in.Remaining {
		if strings.TrimSpace(p.Title) == "" {
			return TaskSplitOutput{}, fmt.Errorf("remaining[%d]: title is empty", i)
		}
		if dues[i], err = parseDue(p.Due); err != nil {
			return TaskSplitOutput{}, fmt.Errorf("remaining[%d]: %w", i, err)
		}
	}
	out := TaskSplitOutput{ID: id, Done: []string{}, Remaining: []string{}}
	part := func(title, body string, due model.Date) (string, error) {
		body = strings.TrimSpace(demoteH1(body))
		if body != "" {
			body += "\n\n"
		}
		p, err := d.Store.Create(ctx, model.Document{
			Folder: model.FolderGoals, Title: strings.Join(strings.Fields(title), " "), Body: body + "Part of [[" + id + "]].",
			Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskTodo,
				Project: t.Project, Goal: t.Goal, Area: t.Area, Incident: t.Incident, Parent: model.LinkTo(id), Due: due},
		})
		return p.ID, err
	}
	// The parts first: if a later step fails, the task is as it was.
	for _, p := range in.Done {
		pid, err := part(p.Title, "", "")
		if err != nil {
			return out, fmt.Errorf("done part %q: %w", p.Title, err)
		}
		var lines []string
		if p.Evidence != nil {
			ev := "Evidence: " + strings.TrimSpace(p.Evidence.Locator)
			if q := strings.TrimSpace(p.Evidence.Quote); q != "" {
				ev += ` ("` + q + `")`
			}
			lines = append(lines, ev)
			ctx = withEvidence(ctx, p.Evidence)
		}
		if n := strings.TrimSpace(p.Note); n != "" {
			lines = append(lines, n)
		}
		if _, err := finishTask(ctx, d, TaskDoneInput{ID: pid, Note: strings.Join(lines, "\n\n")}, model.OutcomeDone); err != nil {
			return out, fmt.Errorf("done part %s: %w", pid, err)
		}
		out.Done = append(out.Done, pid)
	}
	for i, p := range in.Remaining {
		pid, err := part(p.Title, p.Body, dues[i])
		if err != nil {
			return out, fmt.Errorf("remaining part %q: %w", p.Title, err)
		}
		out.Remaining = append(out.Remaining, pid)
	}
	// Re-read: a part's write can have changed nothing here, but the Rev is
	// checked on update.
	doc, t, err = getTask(ctx, d.Store, id)
	if err != nil {
		return out, err
	}
	for _, pid := range out.Remaining {
		t.DependsOn = append(t.DependsOn, model.LinkTo(pid))
	}
	if title != "" {
		doc.Title = title
	}
	lines := []string{reason}
	if len(out.Done) > 0 {
		lines = append(lines, "Done: "+wikilinks(out.Done)+".")
	}
	lines = append(lines, "Remains: "+wikilinks(out.Remaining)+".")
	doc.Body = appendToSection(doc.Body, "## Split", strings.Join(lines, "\n\n"))
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return out, err
	}
	out.Rev = saved.Rev
	return out, nil
}))
