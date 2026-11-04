package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/store"
)

// ProjectView is a project without its body.
type ProjectView struct {
	ID     string              `json:"id"`
	Folder model.Folder        `json:"folder"`
	Title  string              `json:"title"`
	Status model.ProjectStatus `json:"status"`
	Owner  string              `json:"owner"`
	Area   string              `json:"area,omitempty"`
	// Concepts are the ids of the concept areas the goal draws on.
	Concepts []string `json:"concepts"`
	// DoneWhen is how we know the goal is reached; empty on an older goal.
	DoneWhen string          `json:"done_when,omitempty"`
	Due      model.Date      `json:"due,omitempty"`
	Links    []model.Pointer `json:"links,omitempty"`
	// Brief is the goal brief, when the goal has one.
	Brief *model.GoalBrief `json:"brief,omitempty"`
	Rev   string           `json:"rev"`
	// AreaName is the title of Area, for people.
	AreaName string `json:"area_name,omitempty" nameof:"Area"`
}

func projectView(d model.Document) (ProjectView, error) {
	p, ok := d.Meta.(*model.Project)
	if !ok {
		return ProjectView{}, fmt.Errorf("%s is a %s, not a project", d.ID, d.Meta.Header().Type)
	}
	v := ProjectView{ID: d.ID, Folder: d.Folder, Title: d.Title, Status: p.Status, Owner: p.Owner, Area: p.Area.Target(), Concepts: targets(p.Concepts), DoneWhen: p.DoneWhen, Due: p.Due, Links: p.Links, Rev: d.Rev}
	if p.HasBrief() {
		v.Brief = &p.GoalBrief
	}
	return v, nil
}

// getProject reads a document that must be a project.
func getProject(ctx context.Context, s store.Store, id string) (model.Document, *model.Project, error) {
	d, err := s.Get(ctx, id)
	if err != nil {
		return d, nil, err
	}
	p, ok := d.Meta.(*model.Project)
	if !ok {
		return d, nil, fmt.Errorf("%s is a %s, not a project", id, d.Meta.Header().Type)
	}
	return d, p, nil
}

// ProjectAddInput is the input of project_add.
type ProjectAddInput struct {
	Title string `json:"title"`
	Owner string `json:"owner,omitempty"` // default: the vault's owner property, else "self"
	Area  string `json:"area,omitempty"`  // id of an area of kind project: the goal's home
	Body  string `json:"body,omitempty"`
	// Concepts are ids of concept areas the goal draws on.
	Concepts []string `json:"concepts,omitempty"`
	// DoneWhen is required: how we know the goal is reached, in one line. A
	// goal that never finishes is an area.
	DoneWhen string `json:"done_when"`
	Due      string `json:"due,omitempty"` // YYYY-MM-DD the goal is due; work leading to it inherits the date
}

// ProjectAddOutput is the output of project_add.
type ProjectAddOutput struct {
	ID     string       `json:"id"`
	Folder model.Folder `json:"folder"`
	Rev    string       `json:"rev"`
	// Warnings: the vault has more active goals than goals.active_limit allows.
	Warnings []string `json:"warnings"`
}

// ProjectAdd creates an active project.
var ProjectAdd = defineSafe("project_add", "Create a project", func(ctx context.Context, d Deps, in ProjectAddInput) (ProjectAddOutput, error) {
	title := strings.Join(strings.Fields(in.Title), " ")
	if title == "" {
		return ProjectAddOutput{}, errors.New("title is empty")
	}
	doneWhen, err := checkDoneWhen(in.DoneWhen, true)
	if err != nil {
		return ProjectAddOutput{}, fmt.Errorf("%w: a goal needs a done condition; if it never finishes, it is an area (area_add)", err)
	}
	owner := strings.TrimSpace(in.Owner)
	if owner == "" {
		var err error
		if owner, err = d.Store.Config("owner"); err != nil {
			return ProjectAddOutput{}, err
		}
		if owner = strings.TrimSpace(owner); owner == "" {
			owner = "self"
		}
	}
	area, err := areaLink(ctx, d.Store, in.Area)
	if err != nil {
		return ProjectAddOutput{}, err
	}
	if in.Due != "" {
		if err := checkDate("due", in.Due); err != nil {
			return ProjectAddOutput{}, err
		}
	}
	concepts, err := conceptLinks(ctx, d.Store, in.Concepts)
	if err != nil {
		return ProjectAddOutput{}, err
	}
	nd := model.Document{
		Folder: model.FolderProjects,
		Title:  title,
		Body:   demoteH1(in.Body),
		Meta: &model.Project{
			Common:   model.Common{Type: model.TypeProject},
			Status:   model.ProjectActive,
			Owner:    owner,
			Area:     area,
			Concepts: concepts,
			DoneWhen: doneWhen,
			Due:      model.Date(in.Due),
		},
	}
	if err := checkKindLinks(ctx, d.Store, nd); err != nil {
		return ProjectAddOutput{}, err
	}
	doc, err := d.Store.Create(ctx, nd)
	if err != nil {
		return ProjectAddOutput{}, err
	}
	out := ProjectAddOutput{ID: doc.ID, Folder: doc.Folder, Rev: doc.Rev, Warnings: []string{}}
	w, err := activeLimitWarning(ctx, d.Store)
	if err != nil {
		return ProjectAddOutput{}, fmt.Errorf("project_add: %w", err)
	}
	if w != "" {
		out.Warnings = append(out.Warnings, w)
	}
	return out, nil
})

// ProjectListInput is the input of project_list.
type ProjectListInput struct {
	All bool `json:"all,omitempty" query:"all"` // include archived projects
}

// ProjectListOutput is the output of project_list.
type ProjectListOutput struct {
	Projects []ProjectView `json:"projects"`
}

// ProjectList lists projects, active folder only unless All.
var ProjectList = defineRead("project_list", "List projects", func(ctx context.Context, d Deps, in ProjectListInput) (ProjectListOutput, error) {
	refs, err := d.Store.List(ctx, store.Filter{Type: model.TypeProject})
	if err != nil {
		return ProjectListOutput{}, err
	}
	out := ProjectListOutput{Projects: []ProjectView{}}
	for _, r := range refs {
		if r.Folder == model.FolderArchive && !in.All {
			continue
		}
		doc, err := d.Store.Get(ctx, r.ID)
		if err != nil {
			return ProjectListOutput{}, err
		}
		v, err := projectView(doc)
		if err != nil {
			return ProjectListOutput{}, err
		}
		out.Projects = append(out.Projects, v)
	}
	return out, nil
})

// ProjectShowInput is the input of project_show.
type ProjectShowInput struct {
	ID string `json:"id" query:"id" required:"true"`
}

// ProjectShowOutput is the output of project_show.
type ProjectShowOutput struct {
	Project ProjectView `json:"project"`
	Body    string      `json:"body"`
	Tasks   []TaskView  `json:"tasks"`
	// Questions are the open questions in scope: the project's own and those of its tasks.
	Questions []QuestionView `json:"questions"`
	// LastAligned is the date an agent last compared this goal with reality
	// (align_record), or empty.
	LastAligned string `json:"last_aligned,omitempty"`
	// Rules are the conventions and constraints in effect (rules_in_effect).
	Rules []RuleRef `json:"rules"`
	// Assumptions are the goal's assumptions, with ## Assumptions body lines
	// read as never checked.
	Assumptions []AssumptionView `json:"assumptions"`
}

// ProjectShow returns a project and its tasks.
var ProjectShow = defineRead("project_show", "Show a project and its tasks", func(ctx context.Context, d Deps, in ProjectShowInput) (ProjectShowOutput, error) {
	doc, _, err := getProject(ctx, d.Store, in.ID)
	if err != nil {
		return ProjectShowOutput{}, err
	}
	v, err := projectView(doc)
	if err != nil {
		return ProjectShowOutput{}, err
	}
	tasks, err := listTasks(ctx, d.Store, in.ID, "")
	if err != nil {
		return ProjectShowOutput{}, err
	}
	qs, err := openQuestions(ctx, d.Store, func(x qrow) bool { return x.project == doc.ID })
	if err != nil {
		return ProjectShowOutput{}, err
	}
	aligned, err := LastAligned(ctx, d.Store, doc.ID)
	if err != nil {
		return ProjectShowOutput{}, err
	}
	rules, err := rulesShort(ctx, d.Store, doc.ID)
	if err != nil {
		return ProjectShowOutput{}, err
	}
	cutoff, err := recheckCutoff(d.Store)
	if err != nil {
		return ProjectShowOutput{}, err
	}
	return ProjectShowOutput{Project: v, Body: doc.Body, Tasks: tasks, Questions: qs, LastAligned: aligned, Rules: rules, Assumptions: assumptionsOf(doc, cutoff)}, nil
})

// ProjectCloseInput is the input of project_close.
type ProjectCloseInput struct {
	ID      string `json:"id"`
	Dropped bool   `json:"dropped,omitempty"` // close as dropped, not done
	Force   bool   `json:"force,omitempty"`   // close with open tasks
}

// ProjectCloseOutput is the output of project_close.
type ProjectCloseOutput struct {
	ID     string              `json:"id"`
	Status model.ProjectStatus `json:"status"`
	Folder model.Folder        `json:"folder"`
	Rev    string              `json:"rev"`
}

// ProjectClose sets the project done (or dropped) and moves it to archive.
// Open tasks refuse the close unless Force. Tasks stay where they are: links
// to the archived project keep resolving.
var ProjectClose = define("project_close", "Close a project and move it to archive", func(ctx context.Context, d Deps, in ProjectCloseInput) (ProjectCloseOutput, error) {
	doc, p, err := getProject(ctx, d.Store, in.ID)
	if err != nil {
		return ProjectCloseOutput{}, err
	}
	if doc.Folder == model.FolderArchive {
		return ProjectCloseOutput{}, fmt.Errorf("%s is already archived", in.ID)
	}
	if !in.Force {
		tasks, err := listTasks(ctx, d.Store, in.ID, "")
		if err != nil {
			return ProjectCloseOutput{}, err
		}
		var open []string
		for _, t := range tasks {
			if !taskTerminal(t.Status) {
				open = append(open, t.ID)
			}
		}
		if len(open) > 0 {
			return ProjectCloseOutput{}, fmt.Errorf("%s has open tasks: %s (finish them or use force)", in.ID, strings.Join(open, " "))
		}
	}
	p.Status = model.ProjectDone
	if in.Dropped {
		p.Status = model.ProjectDropped
	}
	if _, err := d.Store.Update(ctx, doc); err != nil {
		return ProjectCloseOutput{}, err
	}
	moved, err := d.Store.Move(ctx, in.ID, model.FolderArchive)
	if err != nil {
		return ProjectCloseOutput{}, err
	}
	return ProjectCloseOutput{ID: moved.ID, Status: p.Status, Folder: moved.Folder, Rev: moved.Rev}, nil
})

// checkDoneWhen returns a done condition in one line. When need is set an
// empty one is an error.
func checkDoneWhen(s string, need bool) (string, error) {
	s = strings.Join(strings.Fields(s), " ")
	if s == "" {
		if need {
			return "", errors.New("done_when is required: say how we know it is done")
		}
		return "", nil
	}
	if len([]rune(s)) > 300 {
		return "", errors.New("done_when is over 300 characters: one line that a person can check")
	}
	if err := lintText("done_when", s); err != nil {
		return "", err
	}
	return s, nil
}

// ProjectEditInput is the input of project_edit.
type ProjectEditInput struct {
	ID       string `json:"id"`
	DoneWhen string `json:"done_when,omitempty"` // how we know the goal is reached; required unless only the date changes
	Due      string `json:"due,omitempty"`       // YYYY-MM-DD the goal is due
	ClearDue bool   `json:"clear_due,omitempty"` // remove the due date
	// Area is the id of the goal's home, an area of kind project. Concepts
	// replace the concept areas the goal draws on. Clear either with doc_edit.
	Area     string   `json:"area,omitempty"`
	Concepts []string `json:"concepts,omitempty"`
}

// ProjectEditOutput is the output of project_edit.
type ProjectEditOutput struct {
	ID       string `json:"id"`
	DoneWhen string `json:"done_when"`
	Rev      string `json:"rev"`
}

// ProjectEdit sets the done condition of a goal.
var ProjectEdit = define("project_edit", "Set the done condition, the due date, the home area, or the concepts of a project (a goal)", func(ctx context.Context, d Deps, in ProjectEditInput) (ProjectEditOutput, error) {
	doc, p, err := getProject(ctx, d.Store, normID(in.ID))
	if err != nil {
		return ProjectEditOutput{}, err
	}
	dw := p.DoneWhen
	other := in.Due != "" || in.ClearDue || in.Area != "" || len(in.Concepts) > 0
	if in.DoneWhen != "" || !other {
		if dw, err = checkDoneWhen(in.DoneWhen, true); err != nil {
			return ProjectEditOutput{}, err
		}
		p.DoneWhen = dw
	}
	switch {
	case in.Due != "" && in.ClearDue:
		return ProjectEditOutput{}, errors.New("give due or clear_due, not both")
	case in.Due != "":
		if err := checkDate("due", in.Due); err != nil {
			return ProjectEditOutput{}, err
		}
		p.Due = model.Date(in.Due)
	case in.ClearDue:
		p.Due = ""
	}
	if in.Area != "" {
		if p.Area, err = areaLink(ctx, d.Store, in.Area); err != nil {
			return ProjectEditOutput{}, err
		}
	}
	if len(in.Concepts) > 0 {
		if p.Concepts, err = conceptLinks(ctx, d.Store, in.Concepts); err != nil {
			return ProjectEditOutput{}, err
		}
	}
	if err := checkKindLinks(ctx, d.Store, doc); err != nil {
		return ProjectEditOutput{}, err
	}
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return ProjectEditOutput{}, err
	}
	return ProjectEditOutput{ID: doc.ID, DoneWhen: dw, Rev: saved.Rev}, nil
})
