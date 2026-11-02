package ops

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/store"
)

// AreaView is an area without its body.
type AreaView struct {
	ID     string           `json:"id"`
	Folder model.Folder     `json:"folder"`
	Title  string           `json:"title"`
	Status model.AreaStatus `json:"status"`
	Links  []model.Pointer  `json:"links"`
	Rev    string           `json:"rev"`
}

func areaView(d model.Document) (AreaView, error) {
	a, ok := d.Meta.(*model.Area)
	if !ok {
		return AreaView{}, fmt.Errorf("%s is a %s, not an area", d.ID, d.Meta.Header().Type)
	}
	return AreaView{ID: d.ID, Folder: d.Folder, Title: d.Title, Status: a.Status, Links: nonNilPointers(a.Links), Rev: d.Rev}, nil
}

// getArea reads a document that must be an area.
func getArea(ctx context.Context, s store.Store, id string) (model.Document, *model.Area, error) {
	d, err := s.Get(ctx, normID(id))
	if err != nil {
		return d, nil, err
	}
	a, ok := d.Meta.(*model.Area)
	if !ok {
		return d, nil, fmt.Errorf("%s is a %s, not an area", d.ID, d.Meta.Header().Type)
	}
	return d, a, nil
}

// areaLink returns the link to an existing area; an empty id gives no link.
func areaLink(ctx context.Context, s store.Store, raw string) (model.Link, error) {
	if normID(raw) == "" {
		return "", nil
	}
	d, _, err := getArea(ctx, s, raw)
	if err != nil {
		return "", fmt.Errorf("area %s: %w", normID(raw), err)
	}
	return model.LinkTo(d.ID), nil
}

// AreaAddInput is the input of area_add.
type AreaAddInput struct {
	Title  string           `json:"title"`
	ID     string           `json:"id,omitempty"`     // default area-<slug of title>
	Status model.AreaStatus `json:"status,omitempty"` // default active
	Body   string           `json:"body,omitempty"`
}

// AreaAddOutput is the output of area_add.
type AreaAddOutput struct {
	ID     string       `json:"id"`
	Folder model.Folder `json:"folder"`
	Rev    string       `json:"rev"`
}

// AreaAdd creates an area. An area is an ongoing responsibility with no end.
var AreaAdd = defineSafe("area_add", "Create an area (an ongoing responsibility, such as Health or Money)", func(ctx context.Context, d Deps, in AreaAddInput) (AreaAddOutput, error) {
	title, err := oneLine("title", in.Title)
	if err != nil {
		return AreaAddOutput{}, err
	}
	status := in.Status
	if status == "" {
		status = model.AreaActive
	}
	if !slices.Contains(model.AreaStatuses, status) {
		return AreaAddOutput{}, fmt.Errorf("status %q must be one of %v", in.Status, model.AreaStatuses)
	}
	id := normID(in.ID)
	if id == "" {
		s := slug(title)
		if s == "" {
			return AreaAddOutput{}, fmt.Errorf("title %q has no usable characters: pass id", title)
		}
		id = "area-" + s
	}
	doc, err := d.Store.Create(ctx, model.Document{
		ID: id, Folder: model.FolderAreas, Title: title, Body: strings.TrimSpace(in.Body),
		Meta: &model.Area{Common: model.Common{Type: model.TypeArea}, Status: status},
	})
	if err != nil {
		return AreaAddOutput{}, err
	}
	return AreaAddOutput{ID: doc.ID, Folder: doc.Folder, Rev: doc.Rev}, nil
})

// AreaListInput is the input of area_list.
type AreaListInput struct{}

// AreaListOutput is the output of area_list.
type AreaListOutput struct {
	Areas []AreaView `json:"areas"`
}

// AreaList lists areas.
var AreaList = defineRead("area_list", "List areas", func(ctx context.Context, d Deps, _ AreaListInput) (AreaListOutput, error) {
	refs, err := d.Store.List(ctx, store.Filter{Type: model.TypeArea})
	if err != nil {
		return AreaListOutput{}, err
	}
	out := AreaListOutput{Areas: []AreaView{}}
	for _, r := range refs {
		doc, err := d.Store.Get(ctx, r.ID)
		if err != nil {
			return AreaListOutput{}, err
		}
		v, err := areaView(doc)
		if err != nil {
			return AreaListOutput{}, err
		}
		out.Areas = append(out.Areas, v)
	}
	return out, nil
})

// areaIncidents returns the incidents of an area, open ones first. Without
// resolved, resolved incidents are left out.
func areaIncidents(ctx context.Context, s store.Store, area string, resolved bool) ([]IncidentView, error) {
	all, err := loadIncidents(ctx, s)
	if err != nil {
		return nil, err
	}
	out := []IncidentView{}
	for _, v := range all {
		if v.Area == area && (resolved || incidentOpen(v.Status)) {
			out = append(out, v)
		}
	}
	return out, nil
}

// AreaShowInput is the input of area_show.
type AreaShowInput struct {
	ID string `json:"id" query:"id" required:"true"`
}

// AreaShowOutput is the output of area_show. Tasks holds the area's own tasks
// and the tasks of its projects; Notes likewise.
type AreaShowOutput struct {
	Area     AreaView      `json:"area"`
	Body     string        `json:"body"`
	Projects []ProjectView `json:"projects"`
	Tasks    []TaskView    `json:"tasks"`
	Notes    []NoteView    `json:"notes"`
	// Incidents are the area's incidents, open ones first. Recurring are its
	// open tasks with a recurrence.
	Incidents []IncidentView `json:"incidents"`
	Recurring []TaskView     `json:"recurring"`
	// Questions are the open questions in scope: the area's own, and those of
	// its projects and tasks.
	Questions []QuestionView `json:"questions"`
}

// AreaShow returns an area with its projects and tasks.
var AreaShow = defineRead("area_show", "Show an area with its projects and tasks", func(ctx context.Context, d Deps, in AreaShowInput) (AreaShowOutput, error) {
	doc, _, err := getArea(ctx, d.Store, in.ID)
	if err != nil {
		return AreaShowOutput{}, err
	}
	v, err := areaView(doc)
	if err != nil {
		return AreaShowOutput{}, err
	}
	out := AreaShowOutput{Area: v, Body: doc.Body, Projects: []ProjectView{}, Tasks: []TaskView{}, Notes: []NoteView{}, Incidents: []IncidentView{}, Recurring: []TaskView{}}
	refs, err := d.Store.List(ctx, store.Filter{Type: model.TypeProject})
	if err != nil {
		return AreaShowOutput{}, err
	}
	inArea := map[string]bool{}
	for _, r := range refs {
		pd, err := d.Store.Get(ctx, r.ID)
		if err != nil {
			return AreaShowOutput{}, err
		}
		pv, err := projectView(pd)
		if err != nil {
			return AreaShowOutput{}, err
		}
		if pv.Area == doc.ID {
			out.Projects = append(out.Projects, pv)
			inArea[pv.ID] = true
		}
	}
	tasks, err := listTasks(ctx, d.Store, "", "")
	if err != nil {
		return AreaShowOutput{}, err
	}
	for _, t := range tasks {
		if t.Area == doc.ID || inArea[t.Project] {
			out.Tasks = append(out.Tasks, t)
			if t.Recur != "" && !taskTerminal(t.Status) {
				out.Recurring = append(out.Recurring, t)
			}
		}
	}
	if out.Incidents, err = areaIncidents(ctx, d.Store, doc.ID, true); err != nil {
		return AreaShowOutput{}, err
	}
	if out.Questions, err = openQuestions(ctx, d.Store, func(x qrow) bool { return x.area == doc.ID }); err != nil {
		return AreaShowOutput{}, err
	}
	if out.Notes, err = listNotes(ctx, d.Store, func(n NoteView) bool { return n.Area == doc.ID || inArea[n.Project] }); err != nil {
		return AreaShowOutput{}, err
	}
	return out, nil
})
