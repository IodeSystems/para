package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/iodesystems/paradize/internal/model"
	"github.com/iodesystems/paradize/internal/store"
)

// ProjectView is a project without its body.
type ProjectView struct {
	ID     string              `json:"id"`
	Folder model.Folder        `json:"folder"`
	Title  string              `json:"title"`
	Status model.ProjectStatus `json:"status"`
	Owner  string              `json:"owner"`
	Rev    string              `json:"rev"`
}

func projectView(d model.Document) (ProjectView, error) {
	p, ok := d.Meta.(*model.Project)
	if !ok {
		return ProjectView{}, fmt.Errorf("%s is a %s, not a project", d.ID, d.Meta.Header().Type)
	}
	return ProjectView{ID: d.ID, Folder: d.Folder, Title: d.Title, Status: p.Status, Owner: p.Owner, Rev: d.Rev}, nil
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
	Owner string `json:"owner"`
	Body  string `json:"body,omitempty"`
}

// ProjectAddOutput is the output of project_add.
type ProjectAddOutput struct {
	ID     string       `json:"id"`
	Folder model.Folder `json:"folder"`
	Rev    string       `json:"rev"`
}

// ProjectAdd creates an active project.
var ProjectAdd = define("project_add", "Create a project", func(ctx context.Context, d Deps, in ProjectAddInput) (ProjectAddOutput, error) {
	title := strings.Join(strings.Fields(in.Title), " ")
	if title == "" {
		return ProjectAddOutput{}, errors.New("title is empty")
	}
	if strings.TrimSpace(in.Owner) == "" {
		return ProjectAddOutput{}, errors.New("owner is empty")
	}
	doc, err := d.Store.Create(ctx, model.Document{
		Folder: model.FolderProjects,
		Title:  title,
		Body:   strings.TrimSpace(in.Body),
		Meta: &model.Project{
			Common: model.Common{Type: model.TypeProject},
			Status: model.ProjectActive,
			Owner:  strings.TrimSpace(in.Owner),
		},
	})
	if err != nil {
		return ProjectAddOutput{}, err
	}
	return ProjectAddOutput{ID: doc.ID, Folder: doc.Folder, Rev: doc.Rev}, nil
})

// ProjectListInput is the input of project_list.
type ProjectListInput struct {
	All bool `json:"all,omitempty"` // include archived projects
}

// ProjectListOutput is the output of project_list.
type ProjectListOutput struct {
	Projects []ProjectView `json:"projects"`
}

// ProjectList lists projects, active folder only unless All.
var ProjectList = define("project_list", "List projects", func(ctx context.Context, d Deps, in ProjectListInput) (ProjectListOutput, error) {
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
	ID string `json:"id"`
}

// ProjectShowOutput is the output of project_show.
type ProjectShowOutput struct {
	Project ProjectView `json:"project"`
	Body    string      `json:"body"`
	Tasks   []TaskView  `json:"tasks"`
}

// ProjectShow returns a project and its tasks.
var ProjectShow = define("project_show", "Show a project and its tasks", func(ctx context.Context, d Deps, in ProjectShowInput) (ProjectShowOutput, error) {
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
	return ProjectShowOutput{Project: v, Body: doc.Body, Tasks: tasks}, nil
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
