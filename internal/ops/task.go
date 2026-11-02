package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/store"
)

// TaskView is a task without its body. Project, Area, and DependsOn hold ids.
type TaskView struct {
	ID        string           `json:"id"`
	Folder    model.Folder     `json:"folder"`
	Title     string           `json:"title"`
	Status    model.TaskStatus `json:"status"`
	Project   string           `json:"project"`
	Area      string           `json:"area,omitempty"`
	DependsOn []string         `json:"depends_on"`
	BlockedBy []string         `json:"blocked_by"`
	Rev       string           `json:"rev"`
}

func taskView(d model.Document) (TaskView, error) {
	t, ok := d.Meta.(*model.Task)
	if !ok {
		return TaskView{}, fmt.Errorf("%s is a %s, not a task", d.ID, d.Meta.Header().Type)
	}
	v := TaskView{
		ID: d.ID, Folder: d.Folder, Title: d.Title, Status: t.Status,
		Project: t.Project.Target(), Area: t.Area.Target(), DependsOn: []string{}, BlockedBy: []string{}, Rev: d.Rev,
	}
	for _, l := range t.DependsOn {
		v.DependsOn = append(v.DependsOn, l.Target())
	}
	v.BlockedBy = append(v.BlockedBy, t.BlockedBy...)
	return v, nil
}

func taskTerminal(s model.TaskStatus) bool { return s == model.TaskDone || s == model.TaskDropped }

// getTask reads a document that must be a task.
func getTask(ctx context.Context, s store.Store, id string) (model.Document, *model.Task, error) {
	d, err := s.Get(ctx, id)
	if err != nil {
		return d, nil, err
	}
	t, ok := d.Meta.(*model.Task)
	if !ok {
		return d, nil, fmt.Errorf("%s is a %s, not a task", id, d.Meta.Header().Type)
	}
	return d, t, nil
}

// listTasks returns tasks sorted by id, filtered by project id and status
// when not empty.
func listTasks(ctx context.Context, s store.Store, project string, status model.TaskStatus) ([]TaskView, error) {
	refs, err := s.List(ctx, store.Filter{Type: model.TypeTask})
	if err != nil {
		return nil, err
	}
	out := []TaskView{}
	for _, r := range refs {
		doc, err := s.Get(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		v, err := taskView(doc)
		if err != nil {
			return nil, err
		}
		if (project == "" || v.Project == project) && (status == "" || v.Status == status) {
			out = append(out, v)
		}
	}
	return out, nil
}

// resolved reports whether a document no longer holds back its dependents.
func resolved(d model.Document) bool {
	switch m := d.Meta.(type) {
	case *model.Task:
		return taskTerminal(m.Status)
	case *model.RFX:
		return m.Status == model.RFXAnswered || m.Status == model.RFXClosed
	}
	return false
}

// TaskAddInput is the input of task_add.
type TaskAddInput struct {
	Project   string   `json:"project,omitempty"` // project id; a task needs a project or an area
	Area      string   `json:"area,omitempty"`    // area id
	Title     string   `json:"title"`
	Body      string   `json:"body,omitempty"`
	DependsOn []string `json:"depends_on,omitempty"` // task or rfx ids
}

// TaskAddOutput is the output of task_add.
type TaskAddOutput struct {
	ID     string       `json:"id"`
	Folder model.Folder `json:"folder"`
	Rev    string       `json:"rev"`
}

// TaskAdd creates a todo task in a project, an area, or both. Tasks live in
// projects/. A dependency may be any task or rfx, in any project or area.
var TaskAdd = define("task_add", "Create a task in a project or an area (at least one)", func(ctx context.Context, d Deps, in TaskAddInput) (TaskAddOutput, error) {
	title := strings.Join(strings.Fields(in.Title), " ")
	if title == "" {
		return TaskAddOutput{}, errors.New("title is empty")
	}
	in.Project = normID(in.Project)
	if in.Project == "" && normID(in.Area) == "" {
		return TaskAddOutput{}, errors.New("a task needs a project or an area")
	}
	var project model.Link
	if in.Project != "" {
		proj, _, err := getProject(ctx, d.Store, in.Project)
		if err != nil {
			return TaskAddOutput{}, err
		}
		if proj.Folder == model.FolderArchive {
			return TaskAddOutput{}, fmt.Errorf("project %s is archived", in.Project)
		}
		project = model.LinkTo(in.Project)
	}
	area, err := areaLink(ctx, d.Store, in.Area)
	if err != nil {
		return TaskAddOutput{}, err
	}
	var deps []model.Link
	for _, id := range in.DependsOn {
		if slices.Contains(deps, model.LinkTo(id)) {
			continue
		}
		dep, err := d.Store.Get(ctx, id)
		if err != nil {
			return TaskAddOutput{}, fmt.Errorf("depends_on %s: %w", id, err)
		}
		if typ := dep.Meta.Header().Type; typ != model.TypeTask && typ != model.TypeRFX {
			return TaskAddOutput{}, fmt.Errorf("depends_on %s is a %s: want a task or rfx", id, typ)
		}
		deps = append(deps, model.LinkTo(id))
	}
	doc, err := d.Store.Create(ctx, model.Document{
		Folder: model.FolderProjects,
		Title:  title,
		Body:   strings.TrimSpace(in.Body),
		Meta: &model.Task{
			Common:    model.Common{Type: model.TypeTask},
			Status:    model.TaskTodo,
			Project:   project,
			Area:      area,
			DependsOn: deps,
		},
	})
	if err != nil {
		return TaskAddOutput{}, err
	}
	return TaskAddOutput{ID: doc.ID, Folder: doc.Folder, Rev: doc.Rev}, nil
})

// TaskListInput is the input of task_list.
type TaskListInput struct {
	Project string           `json:"project,omitempty" query:"project"`
	Area    string           `json:"area,omitempty" query:"area"` // tasks with this area of their own
	Status  model.TaskStatus `json:"status,omitempty" query:"status"`
}

// TaskListOutput is the output of task_list.
type TaskListOutput struct {
	Tasks []TaskView `json:"tasks"`
}

// TaskList lists tasks.
var TaskList = define("task_list", "List tasks", func(ctx context.Context, d Deps, in TaskListInput) (TaskListOutput, error) {
	if in.Status != "" && !slices.Contains(model.TaskStatuses, in.Status) {
		return TaskListOutput{}, fmt.Errorf("unknown status %q", in.Status)
	}
	tasks, err := listTasks(ctx, d.Store, in.Project, in.Status)
	if err != nil {
		return TaskListOutput{}, err
	}
	if area := normID(in.Area); area != "" {
		tasks = slices.DeleteFunc(tasks, func(t TaskView) bool { return t.Area != area })
	}
	return TaskListOutput{Tasks: tasks}, nil
})

// TaskShowInput is the input of task_show.
type TaskShowInput struct {
	ID string `json:"id" query:"id" required:"true"`
}

// TaskShowOutput is the output of task_show.
type TaskShowOutput struct {
	Task TaskView `json:"task"`
	Body string   `json:"body"`
}

// TaskShow returns one task.
var TaskShow = define("task_show", "Show a task", func(ctx context.Context, d Deps, in TaskShowInput) (TaskShowOutput, error) {
	doc, _, err := getTask(ctx, d.Store, in.ID)
	if err != nil {
		return TaskShowOutput{}, err
	}
	v, err := taskView(doc)
	return TaskShowOutput{Task: v, Body: doc.Body}, err
})

// TaskChangeOutput is the output of task_start, task_done, and task_block.
type TaskChangeOutput struct {
	ID        string           `json:"id"`
	Status    model.TaskStatus `json:"status"`
	BlockedBy []string         `json:"blocked_by"`
	Rev       string           `json:"rev"`
}

// transition applies fn to a task, then saves it. fn may refuse.
func transition(ctx context.Context, d Deps, id string, fn func(doc model.Document, t *model.Task) error) (TaskChangeOutput, error) {
	doc, t, err := getTask(ctx, d.Store, id)
	if err != nil {
		return TaskChangeOutput{}, err
	}
	if taskTerminal(t.Status) {
		return TaskChangeOutput{}, fmt.Errorf("%s is already %s", id, t.Status)
	}
	if err := fn(doc, t); err != nil {
		return TaskChangeOutput{}, err
	}
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return TaskChangeOutput{}, err
	}
	return TaskChangeOutput{ID: id, Status: t.Status, BlockedBy: append([]string{}, t.BlockedBy...), Rev: saved.Rev}, nil
}

// TaskStartInput is the input of task_start.
type TaskStartInput struct {
	ID    string `json:"id"`
	Force bool   `json:"force,omitempty"` // start with unfinished dependencies
}

// TaskStart moves a todo or blocked task to doing and clears its blockers.
// Unfinished dependencies refuse the start unless Force.
var TaskStart = define("task_start", "Start a task", func(ctx context.Context, d Deps, in TaskStartInput) (TaskChangeOutput, error) {
	return transition(ctx, d, in.ID, func(_ model.Document, t *model.Task) error {
		if t.Status == model.TaskDoing {
			return fmt.Errorf("%s is already doing", in.ID)
		}
		if !in.Force {
			var open []string
			for _, l := range t.DependsOn {
				dep, err := d.Store.Get(ctx, l.Target())
				if err != nil || !resolved(dep) {
					open = append(open, l.Target())
				}
			}
			if len(open) > 0 {
				return fmt.Errorf("%s waits on %s (use force)", in.ID, strings.Join(open, " "))
			}
		}
		t.Status = model.TaskDoing
		t.BlockedBy = nil
		return nil
	})
})

// TaskDoneInput is the input of task_done.
type TaskDoneInput struct {
	ID string `json:"id"`
}

// TaskDone marks a task done and clears its blockers.
var TaskDone = define("task_done", "Mark a task done", func(ctx context.Context, d Deps, in TaskDoneInput) (TaskChangeOutput, error) {
	return transition(ctx, d, in.ID, func(_ model.Document, t *model.Task) error {
		t.Status = model.TaskDone
		t.BlockedBy = nil
		return nil
	})
})

// TaskBlockInput is the input of task_block.
type TaskBlockInput struct {
	ID     string `json:"id"`
	Reason string `json:"reason"` // why: who said it, and where it applies
}

// TaskBlock marks a task blocked and records the reason. Blocking a blocked
// task adds a reason.
var TaskBlock = define("task_block", "Block a task with a reason", func(ctx context.Context, d Deps, in TaskBlockInput) (TaskChangeOutput, error) {
	reason := strings.Join(strings.Fields(in.Reason), " ")
	if reason == "" {
		return TaskChangeOutput{}, errors.New("reason is empty")
	}
	return transition(ctx, d, in.ID, func(_ model.Document, t *model.Task) error {
		t.Status = model.TaskBlocked
		if !slices.Contains(t.BlockedBy, reason) {
			t.BlockedBy = append(t.BlockedBy, reason)
		}
		return nil
	})
})
