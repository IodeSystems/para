package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
	"github.com/iodesystems/para/internal/upgrade"
)

// A project (vault format 11) is an ongoing tracked body of work: a state
// line, the goals in flight, and an icebox of parked goals. It has no done
// condition; a finite outcome is a goal (goalitem.go). Below format 11 a file
// of type project is a goal, so these ops refuse there.

// goalSince is the cause an op of a project gives for a goal.
const goalSince = "a goal since 0.6.0"

// projectsOn refuses below model.GoalsFormat, until upgrade step goals ran
// (model.GoalsStepKey).
func projectsOn(s store.Store) error {
	v, err := upgrade.Current(s)
	if err != nil {
		return err
	}
	ran, err := s.Config(model.GoalsStepKey)
	if err != nil {
		return err
	}
	if v < model.GoalsFormat && strings.TrimSpace(ran) == "" {
		return fmt.Errorf("this vault is below format %d, where every project is a goal (goal_add): the owner runs `para vault upgrade` (step goals) for ongoing projects", model.GoalsFormat)
	}
	return nil
}

// getOngoing reads a document that must be a project. A goal says it is one.
func getOngoing(ctx context.Context, s store.Store, id string) (model.Document, *model.Project, error) {
	d, err := s.Get(ctx, id)
	if err != nil {
		return d, nil, missing("project", id, err)
	}
	switch m := d.Meta.(type) {
	case *model.Project:
		return d, m, nil
	case *model.Goal:
		return d, nil, fmt.Errorf("%s is %s: use show {id} (goal_show), close {id, outcome} (goal_close), or edit (goal_edit)", id, goalSince)
	}
	return d, nil, fmt.Errorf("%s is a %s, not a project", id, d.Meta.Header().Type)
}

// projectLink is the link to the project raw names, or "" for none.
func projectLink(ctx context.Context, s store.Store, raw string) (model.Link, error) {
	id := normID(raw)
	if id == "" {
		return "", nil
	}
	if err := projectsOn(s); err != nil {
		return "", err
	}
	doc, _, err := getOngoing(ctx, s, id)
	if err != nil {
		return "", linkErr("project", id, err)
	}
	return model.LinkTo(doc.ID), nil
}

// ProjectView is a project without its body.
type ProjectView struct {
	ID        string              `json:"id"`
	Folder    model.Folder        `json:"folder"`
	Title     string              `json:"title"`
	Status    model.ProjectStatus `json:"status"`
	Owner     string              `json:"owner,omitempty"`
	Area      string              `json:"area,omitempty"`
	Concepts  []string            `json:"concepts"`
	StateLine string              `json:"state_line,omitempty"`
	StateDate model.Date          `json:"state_date,omitempty"`
	Links     []model.Pointer     `json:"links,omitempty"`
	Rev       string              `json:"rev"`
	// AreaName is the title of Area, for people.
	AreaName string `json:"area_name,omitempty" nameof:"Area"`
}

func projectView(d model.Document, p *model.Project) ProjectView {
	return ProjectView{ID: d.ID, Folder: d.Folder, Title: d.Title, Status: p.Status, Owner: p.Owner, Area: p.Area.Target(),
		Concepts: targets(p.Concepts), StateLine: p.StateLine, StateDate: p.StateDate, Links: p.Links, Rev: d.Rev}
}

// ProjectAddInput is the input of project_add.
type ProjectAddInput struct {
	Title string `json:"title"`
	Owner string `json:"owner,omitempty"` // default: the vault's owner property, else "self"
	Area  string `json:"area,omitempty"`  // id of an area: a facet, optional
	Body  string `json:"body,omitempty"`
	// StateLine is one line on where the project stands.
	StateLine string `json:"state_line,omitempty"`
	// DoneWhen and Due are refused: a finite outcome is a goal (goal_add).
	DoneWhen string `json:"done_when,omitempty"`
	Due      string `json:"due,omitempty"`
}

// ProjectAddOutput is the output of project_add.
type ProjectAddOutput struct {
	ID     string       `json:"id"`
	Folder model.Folder `json:"folder"`
	Rev    string       `json:"rev"`
}

// ProjectAdd creates an active project.
var ProjectAdd = defineSafe("project_add", "Create an ongoing project: a tracked body of work with a state line and goals in flight, and no done condition (a finite outcome is a goal: goal_add)", func(ctx context.Context, d Deps, in ProjectAddInput) (ProjectAddOutput, error) {
	if strings.TrimSpace(in.DoneWhen) != "" || strings.TrimSpace(in.Due) != "" {
		return ProjectAddOutput{}, fmt.Errorf("a project has no done_when or due: an outcome that finishes is %s: use goal_add (add {type: goal})", goalSince)
	}
	title := strings.Join(strings.Fields(in.Title), " ")
	if title == "" {
		return ProjectAddOutput{}, errors.New("title is empty")
	}
	if err := projectsOn(d.Store); err != nil {
		return ProjectAddOutput{}, err
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
	p := &model.Project{Common: model.Common{Type: model.TypeProject}, Status: model.ProjectActive, Owner: owner, Area: area}
	if line := strings.Join(strings.Fields(in.StateLine), " "); line != "" {
		if err := lintText("state_line", line); err != nil {
			return ProjectAddOutput{}, err
		}
		p.StateLine = line
		p.StateDate = model.Date(d.Store.Now().Format("2006-01-02"))
	}
	doc, err := d.Store.Create(ctx, model.Document{Folder: model.FolderProjects, Title: title, Body: demoteH1(in.Body), Meta: p})
	if err != nil {
		return ProjectAddOutput{}, err
	}
	return ProjectAddOutput{ID: doc.ID, Folder: doc.Folder, Rev: doc.Rev}, nil
})

// ProjectShowInput is the input of project_show.
type ProjectShowInput struct {
	ID string `json:"id" query:"id" required:"true"`
}

// ProjectShowOutput is the output of project_show.
type ProjectShowOutput struct {
	Project ProjectView `json:"project"`
	Body    string      `json:"body"`
	// Goals are the goals that link the project, closed ones left out.
	Goals []GoalView `json:"goals"`
}

// ProjectShow returns a project and its goals.
var ProjectShow = defineRead("project_show", "Show an ongoing project and its goals", func(ctx context.Context, d Deps, in ProjectShowInput) (ProjectShowOutput, error) {
	doc, p, err := getOngoing(ctx, d.Store, normID(in.ID))
	if err != nil {
		return ProjectShowOutput{}, err
	}
	refs, err := d.Store.List(ctx, store.Filter{Type: model.TypeGoal})
	if err != nil {
		return ProjectShowOutput{}, err
	}
	out := ProjectShowOutput{Project: projectView(doc, p), Body: doc.Body, Goals: []GoalView{}}
	for _, r := range refs {
		gd, err := d.Store.Get(ctx, r.ID)
		if err != nil {
			continue
		}
		if g, ok := gd.Meta.(*model.Goal); ok && g.Project.Target() == doc.ID {
			v, err := goalView(gd)
			if err != nil {
				return ProjectShowOutput{}, err
			}
			out.Goals = append(out.Goals, v)
		}
	}
	return out, nil
})

// ProjectRetireInput is the input of project_retire.
type ProjectRetireInput struct {
	ID string `json:"id"`
}

// ProjectRetireOutput is the output of project_retire.
type ProjectRetireOutput struct {
	ID     string              `json:"id"`
	Status model.ProjectStatus `json:"status"`
	Rev    string              `json:"rev"`
}

// ProjectRetire sets a project retired. It stays where it is, for its
// history; its goals keep their own status.
var ProjectRetire = define("project_retire", "Retire an ongoing project: it leaves the Projects view and keeps its history", func(ctx context.Context, d Deps, in ProjectRetireInput) (ProjectRetireOutput, error) {
	doc, p, err := getOngoing(ctx, d.Store, normID(in.ID))
	if err != nil {
		return ProjectRetireOutput{}, err
	}
	if p.Status == model.ProjectRetired {
		return ProjectRetireOutput{}, fmt.Errorf("%s is already retired", doc.ID)
	}
	p.Status = model.ProjectRetired
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return ProjectRetireOutput{}, err
	}
	return ProjectRetireOutput{ID: saved.ID, Status: p.Status, Rev: saved.Rev}, nil
})
