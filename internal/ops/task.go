package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/store"
)

// TaskView is a task without its body. Project, Area, and DependsOn hold ids.
type TaskView struct {
	ID       string           `json:"id"`
	Folder   model.Folder     `json:"folder"`
	Title    string           `json:"title"`
	Status   model.TaskStatus `json:"status"`
	Project  string           `json:"project"`
	Area     string           `json:"area,omitempty"`
	Created  model.Date       `json:"created"`
	Due      model.Date       `json:"due,omitempty"`
	Recur    string           `json:"recur,omitempty"`
	Assignee string           `json:"assignee,omitempty"`  // empty means self
	Previous string           `json:"previous,omitempty"`  // the task this one recurred from
	Incident string           `json:"incident,omitempty"`  // the incident this task follows up
	DoneWhen string           `json:"done_when,omitempty"` // how we know it is done
	// Outcome is how a closed task ended: done, partial, dropped, duplicate, or
	// superseded. Empty on an open task and on one closed before outcomes.
	Outcome   model.TaskOutcome `json:"outcome,omitempty"`
	DependsOn []string          `json:"depends_on"`
	BlockedBy []string          `json:"blocked_by"`
	Rev       string            `json:"rev"`
	// Deferred is true while a person has put the task off (itself, or its
	// goal). DeferUntil is the date or "someday". It still blocks its dependents.
	Deferred       bool   `json:"deferred"`
	DeferUntil     string `json:"defer_until,omitempty"`
	DeferredReason string `json:"deferred_reason,omitempty"`
	// WaitsOn is the part of DeferredAfter still open: the item is under Later
	// until they resolve. ReadyWhy is set for 7 days once they have, "ready: X done".
	WaitsOn      []string `json:"waits_on"`
	ReadyWhy     string   `json:"ready_why,omitempty"`
	WaitsOnNames []string `json:"waits_on_names,omitempty" nameof:"WaitsOn"`
	// Names are for people: the title of what the id fields point at, "" when
	// unknown. DependsOnNames has one entry per DependsOn.
	ProjectName    string   `json:"project_name,omitempty" nameof:"Project"`
	AreaName       string   `json:"area_name,omitempty" nameof:"Area"`
	AssigneeName   string   `json:"assignee_name,omitempty" nameof:"Assignee"`
	IncidentName   string   `json:"incident_name,omitempty" nameof:"Incident"`
	DependsOnNames []string `json:"depends_on_names,omitempty" nameof:"DependsOn"`
}

func taskView(d model.Document) (TaskView, error) {
	t, ok := d.Meta.(*model.Task)
	if !ok {
		return TaskView{}, fmt.Errorf("%s is a %s, not a task", d.ID, d.Meta.Header().Type)
	}
	v := TaskView{
		ID: d.ID, Folder: d.Folder, Title: d.Title, Status: t.Status,
		Project: t.Project.Target(), Area: t.Area.Target(), Created: t.Created, Due: t.Due, Recur: t.Recur, Assignee: t.Assignee, Previous: t.Previous.Target(), Incident: t.Incident.Target(), DoneWhen: t.DoneWhen, Outcome: t.Outcome, DependsOn: []string{}, BlockedBy: []string{}, Rev: d.Rev,
		DeferUntil: t.DeferUntil, DeferredReason: t.DeferredReason, WaitsOn: []string{},
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
	case *model.Question:
		return m.Status != model.QuestionOpen
	case *model.Project:
		return m.Status == model.ProjectDone || m.Status == model.ProjectDropped
	case *model.Request:
		return m.Status != model.RequestQueued && m.Status != model.RequestClaimed
	}
	return false
}

// TaskAddInput is the input of task_add.
type TaskAddInput struct {
	Project   string   `json:"project,omitempty"` // project id; a task needs a project or an area
	Area      string   `json:"area,omitempty"`    // area id
	Title     string   `json:"title"`
	Body      string   `json:"body,omitempty"`
	Due       string   `json:"due,omitempty"`        // YYYY-MM-DD
	Recur     string   `json:"recur,omitempty"`      // daily, weekly, monthly, or every <n> days
	Assignee  string   `json:"assignee,omitempty"`   // self (default), a person id, or agent:<name>
	Incident  string   `json:"incident,omitempty"`   // incident id this task follows up; with no project or area the task goes in the incident's area
	DoneWhen  string   `json:"done_when,omitempty"`  // how we know it is done; required from an agent
	DependsOn []string `json:"depends_on,omitempty"` // task or rfx ids
}

// TaskAddOutput is the output of task_add.
type TaskAddOutput struct {
	ID      string       `json:"id"`
	Folder  model.Folder `json:"folder"`
	Rev     string       `json:"rev"`
	Request string       `json:"request,omitempty"` // request made for a recurring agent task
	// Warnings: an unknown recur is stored, and lint warns.
	Warnings []string `json:"warnings"`
}

// TaskAdd creates a todo task in a project, an area, or both. Tasks live in
// projects/. A dependency may be any task or rfx, in any project or area.
var TaskAdd = defineSafe("task_add", "Create a task in a project or an area (at least one)", func(ctx context.Context, d Deps, in TaskAddInput) (TaskAddOutput, error) {
	title := strings.Join(strings.Fields(in.Title), " ")
	if title == "" {
		return TaskAddOutput{}, errors.New("title is empty")
	}
	due, err := parseDue(in.Due)
	if err != nil {
		return TaskAddOutput{}, err
	}
	doneWhen, err := checkDoneWhen(in.DoneWhen, doneWhenNeeded(d))
	if err != nil {
		return TaskAddOutput{}, fmt.Errorf("%w (every task an agent writes says how we know it is done)", err)
	}
	in.Project = normID(in.Project)
	var incident model.Link
	if id := normID(in.Incident); id != "" {
		idoc, im, err := getIncident(ctx, d.Store, id)
		if err != nil {
			return TaskAddOutput{}, fmt.Errorf("incident %s: %w", id, err)
		}
		incident = model.LinkTo(idoc.ID)
		if in.Project == "" && normID(in.Area) == "" {
			in.Area = im.Area.Target()
		}
	}
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
	assignee, err := resolveAssignee(ctx, d, in.Assignee)
	if err != nil {
		return TaskAddOutput{}, err
	}
	var deps []model.Link
	for _, id := range in.DependsOn {
		l, err := dependencyLink(ctx, d.Store, id)
		if err != nil {
			return TaskAddOutput{}, err
		}
		if !slices.Contains(deps, l) {
			deps = append(deps, l)
		}
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
			Due:       due,
			Recur:     strings.Join(strings.Fields(in.Recur), " "),
			Assignee:  assignee,
			Incident:  incident,
			DoneWhen:  doneWhen,
			DependsOn: deps,
		},
	})
	if err != nil {
		return TaskAddOutput{}, err
	}
	out := TaskAddOutput{ID: doc.ID, Folder: doc.Folder, Rev: doc.Rev, Warnings: []string{}}
	if w := recurWarning(in.Recur); w != "" {
		out.Warnings = append(out.Warnings, w)
	}
	out.Request, out.Warnings = agentRequest(ctx, d, doc, out.Warnings)
	return out, nil
})

// TaskListInput is the input of task_list.
type TaskListInput struct {
	Project string           `json:"project,omitempty" query:"project"`
	Area    string           `json:"area,omitempty" query:"area"` // tasks with this area of their own
	Status  model.TaskStatus `json:"status,omitempty" query:"status"`
	// Assignee keeps tasks assigned to this person id or agent:<name>; self
	// keeps the tasks with no assignee.
	Assignee string `json:"assignee,omitempty" query:"assignee"`
	Incident string `json:"incident,omitempty" query:"incident"` // follow-ups of this incident
	// Deferred includes the tasks a person put off (Later). They are left out
	// by default; each is marked.
	Deferred bool `json:"deferred,omitempty" query:"deferred"`
}

// TaskListOutput is the output of task_list.
type TaskListOutput struct {
	Tasks []TaskView `json:"tasks"`
}

// TaskList lists tasks.
var TaskList = defineRead("task_list", "List tasks", func(ctx context.Context, d Deps, in TaskListInput) (TaskListOutput, error) {
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
	if inc := normID(in.Incident); inc != "" {
		tasks = slices.DeleteFunc(tasks, func(t TaskView) bool { return t.Incident != inc })
	}
	if want := strings.TrimSpace(in.Assignee); want != "" {
		if strings.EqualFold(want, SelfAssignee) {
			want = ""
		} else if want, err = resolveAssignee(ctx, d, want); err != nil {
			return TaskListOutput{}, err
		}
		tasks = slices.DeleteFunc(tasks, func(t TaskView) bool { return t.Assignee != want })
	}
	if tasks, err = markDeferred(ctx, d.Store, tasks, in.Deferred); err != nil {
		return TaskListOutput{}, err
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
	// Questions are the open questions scoped to this task.
	Questions []QuestionView `json:"questions"`
}

// TaskShow returns one task.
var TaskShow = defineRead("task_show", "Show a task", func(ctx context.Context, d Deps, in TaskShowInput) (TaskShowOutput, error) {
	doc, _, err := getTask(ctx, d.Store, in.ID)
	if err != nil {
		return TaskShowOutput{}, err
	}
	v, err := taskView(doc)
	if err != nil {
		return TaskShowOutput{}, err
	}
	qs, err := openQuestions(ctx, d.Store, func(x qrow) bool { return x.v.Scope == doc.ID })
	return TaskShowOutput{Task: v, Body: doc.Body, Questions: qs}, err
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
var TaskStart = defineSafe("task_start", "Start a task", func(ctx context.Context, d Deps, in TaskStartInput) (TaskChangeOutput, error) {
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

// TaskBlockInput is the input of task_block.
type TaskBlockInput struct {
	ID     string `json:"id"`
	Reason string `json:"reason"` // why: who said it, and where it applies
}

// TaskBlock marks a task blocked and records the reason. Blocking a blocked
// task adds a reason.
var TaskBlock = defineSafe("task_block", "Block a task with a reason", func(ctx context.Context, d Deps, in TaskBlockInput) (TaskChangeOutput, error) {
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

// parseDue checks a YYYY-MM-DD date. Empty gives no date.
func parseDue(s string) (model.Date, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	if _, err := time.Parse("2006-01-02", s); err != nil {
		return "", fmt.Errorf("due %q: want a date YYYY-MM-DD", s)
	}
	return model.Date(s), nil
}

// TaskDueInput is the input of task_due.
type TaskDueInput struct {
	ID    string `json:"id"`
	Date  string `json:"date,omitempty"`  // YYYY-MM-DD; empty with Clear removes the date
	Clear bool   `json:"clear,omitempty"` // remove the due date
	Recur string `json:"recur,omitempty"` // daily, weekly, monthly, or every <n> days
}

// TaskDueOutput is the output of task_due.
type TaskDueOutput struct {
	ID    string     `json:"id"`
	Due   model.Date `json:"due,omitempty"`
	Recur string     `json:"recur,omitempty"`
	Rev   string     `json:"rev"`
}

// TaskDue sets or clears the due date of an open task, and optionally its
// recurrence. The recurrence acts when the task is marked done.
var TaskDue = defineSafe("task_due", "Set or clear the due date of a task", func(ctx context.Context, d Deps, in TaskDueInput) (TaskDueOutput, error) {
	date, err := parseDue(in.Date)
	if err != nil {
		return TaskDueOutput{}, err
	}
	if (date == "") == !in.Clear {
		return TaskDueOutput{}, errors.New("give a date, or clear to remove it")
	}
	doc, t, err := getTask(ctx, d.Store, normID(in.ID))
	if err != nil {
		return TaskDueOutput{}, err
	}
	if taskTerminal(t.Status) {
		return TaskDueOutput{}, fmt.Errorf("%s is already %s", doc.ID, t.Status)
	}
	t.Due = date
	if recur := strings.Join(strings.Fields(in.Recur), " "); recur != "" {
		t.Recur = recur
	}
	if in.Clear {
		t.Recur = ""
	}
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return TaskDueOutput{}, err
	}
	return TaskDueOutput{ID: doc.ID, Due: t.Due, Recur: t.Recur, Rev: saved.Rev}, nil
})

// dependencyLink checks that id names a task, an rfx, or a question, in any
// project or area.
func dependencyLink(ctx context.Context, s store.Store, raw string) (model.Link, error) {
	id := normID(raw)
	dep, err := s.Get(ctx, id)
	if err != nil {
		return "", fmt.Errorf("depends_on %s: %w", id, err)
	}
	if typ := dep.Meta.Header().Type; typ != model.TypeTask && typ != model.TypeRFX && typ != model.TypeQuestion && typ != model.TypeProject && typ != model.TypeRequest {
		return "", fmt.Errorf("depends_on %s is a %s: want a task, rfx, question, goal, or agent work", id, typ)
	}
	return model.LinkTo(id), nil
}

// dependencyPath returns a chain of dependencies from "from" to "to" (both
// ends included), or nil when "from" does not depend on "to", directly or not.
// A missing document ends its branch.
func dependencyPath(ctx context.Context, s store.Store, from, to string) ([]string, error) {
	seen := map[string]bool{}
	var walk func(id string) ([]string, error)
	walk = func(id string) ([]string, error) {
		if id == to {
			return []string{id}, nil
		}
		if seen[id] {
			return nil, nil
		}
		seen[id] = true
		doc, err := s.Get(ctx, id)
		if errors.Is(err, model.ErrNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		var links []model.Link
		switch m := doc.Meta.(type) {
		case *model.Task:
			links = m.DependsOn
		case *model.RFX:
			links = m.DependsOn
		case *model.Question:
			links = m.DependsOn
		}
		for _, l := range links {
			path, err := walk(l.Target())
			if err != nil || path != nil {
				return append([]string{id}, path...), err
			}
		}
		return nil, nil
	}
	return walk(from)
}

// TaskDependsInput is the input of task_depends_add and task_depends_remove.
type TaskDependsInput struct {
	ID        string   `json:"id"`
	DependsOn []string `json:"depends_on"` // task or rfx ids, in any project or area
}

// TaskDependsOutput is the output of task_depends_add and task_depends_remove.
type TaskDependsOutput struct {
	ID        string   `json:"id"`
	DependsOn []string `json:"depends_on"` // all dependencies after the change
	Rev       string   `json:"rev"`
}

// TaskDependsAdd adds dependencies to an open task. An edge that would close a
// cycle is refused, and nothing is changed.
var TaskDependsAdd = defineSafe("task_depends_add", "Make a task wait on other tasks or rfx; a cycle is refused", func(ctx context.Context, d Deps, in TaskDependsInput) (TaskDependsOutput, error) {
	doc, t, err := getTask(ctx, d.Store, normID(in.ID))
	if err != nil {
		return TaskDependsOutput{}, err
	}
	if taskTerminal(t.Status) {
		return TaskDependsOutput{}, fmt.Errorf("%s is already %s", doc.ID, t.Status)
	}
	if len(in.DependsOn) == 0 {
		return TaskDependsOutput{}, errors.New("depends_on is empty")
	}
	for _, raw := range in.DependsOn {
		l, err := dependencyLink(ctx, d.Store, raw)
		if err != nil {
			return TaskDependsOutput{}, err
		}
		if slices.Contains(t.DependsOn, l) {
			continue
		}
		if l.Target() == doc.ID {
			return TaskDependsOutput{}, fmt.Errorf("%s cannot depend on itself", doc.ID)
		}
		// The new edge doc -> dep closes a cycle when dep already reaches doc.
		back, err := dependencyPath(ctx, d.Store, l.Target(), doc.ID)
		if err != nil {
			return TaskDependsOutput{}, err
		}
		if back != nil {
			return TaskDependsOutput{}, fmt.Errorf("%s -> %s would make a dependency cycle: %s -> %s",
				doc.ID, l.Target(), doc.ID, strings.Join(back, " -> "))
		}
		t.DependsOn = append(t.DependsOn, l)
	}
	return saveDepends(ctx, d, doc, t)
})

// TaskDependsRemove removes dependencies from a task.
var TaskDependsRemove = defineSafe("task_depends_remove", "Stop a task waiting on other tasks or rfx", func(ctx context.Context, d Deps, in TaskDependsInput) (TaskDependsOutput, error) {
	doc, t, err := getTask(ctx, d.Store, normID(in.ID))
	if err != nil {
		return TaskDependsOutput{}, err
	}
	if len(in.DependsOn) == 0 {
		return TaskDependsOutput{}, errors.New("depends_on is empty")
	}
	for _, raw := range in.DependsOn {
		l := model.LinkTo(normID(raw))
		if !slices.Contains(t.DependsOn, l) {
			return TaskDependsOutput{}, fmt.Errorf("%s does not depend on %s", doc.ID, normID(raw))
		}
		t.DependsOn = slices.DeleteFunc(t.DependsOn, func(x model.Link) bool { return x == l })
	}
	return saveDepends(ctx, d, doc, t)
})

func saveDepends(ctx context.Context, d Deps, doc model.Document, t *model.Task) (TaskDependsOutput, error) {
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return TaskDependsOutput{}, err
	}
	return TaskDependsOutput{ID: doc.ID, DependsOn: targets(t.DependsOn), Rev: saved.Rev}, nil
}

// doneWhenNeeded is true for a caller that is not a person: an agent, or an
// item an agent proposed. A person may capture a task without a condition.
func doneWhenNeeded(d Deps) bool {
	return !slices.Contains(PersonSurfaces, d.Store.Info().Surface)
}

// TaskEditInput is the input of task_edit.
type TaskEditInput struct {
	ID       string `json:"id"`
	DoneWhen string `json:"done_when"` // how we know the task is done
}

// TaskEditOutput is the output of task_edit.
type TaskEditOutput struct {
	ID       string `json:"id"`
	DoneWhen string `json:"done_when"`
	Rev      string `json:"rev"`
}

// TaskEdit sets the done condition of an open task.
var TaskEdit = defineSafe("task_edit", "Set how we know a task is done", func(ctx context.Context, d Deps, in TaskEditInput) (TaskEditOutput, error) {
	doc, t, err := getTask(ctx, d.Store, normID(in.ID))
	if err != nil {
		return TaskEditOutput{}, err
	}
	if taskTerminal(t.Status) {
		return TaskEditOutput{}, fmt.Errorf("%s is already %s", doc.ID, t.Status)
	}
	dw, err := checkDoneWhen(in.DoneWhen, true)
	if err != nil {
		return TaskEditOutput{}, err
	}
	t.DoneWhen = dw
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return TaskEditOutput{}, err
	}
	return TaskEditOutput{ID: doc.ID, DoneWhen: dw, Rev: saved.Rev}, nil
})

// markDeferred sets Deferred on open tasks, and drops them unless keep is set.
func markDeferred(ctx context.Context, s store.Store, tasks []TaskView, keep bool) ([]TaskView, error) {
	set, err := deferredSet(ctx, s, s.Now().Format(dateLayout))
	if err != nil {
		return nil, err
	}
	out := tasks[:0]
	for _, t := range tasks {
		t.Deferred = set[t.ID].any() && !taskTerminal(t.Status)
		if !taskTerminal(t.Status) {
			if t.WaitsOn = set[t.ID].Waits; t.WaitsOn == nil {
				t.WaitsOn = []string{}
			}
			t.ReadyWhy = set[t.ID].Ready
		}
		if t.Deferred && !keep {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}
