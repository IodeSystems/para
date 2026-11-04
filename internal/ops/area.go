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
	// Kind is project or concept; a file with no kind reads as project.
	Kind model.AreaKind `json:"kind"`
	// Cadence is how often the owner reviews the area; LastReviewed the date of
	// the last review, NextReview one cadence after it.
	Cadence      string     `json:"cadence,omitempty"`
	LastReviewed model.Date `json:"last_reviewed,omitempty"`
	NextReview   string     `json:"next_review,omitempty"`
	// Concepts are the ids of the concept areas a project area draws on.
	Concepts []string        `json:"concepts"`
	Links    []model.Pointer `json:"links"`
	Rev      string          `json:"rev"`
}

func areaView(d model.Document) (AreaView, error) {
	a, ok := d.Meta.(*model.Area)
	if !ok {
		return AreaView{}, fmt.Errorf("%s is a %s, not an area", d.ID, d.Meta.Header().Type)
	}
	return AreaView{ID: d.ID, Folder: d.Folder, Title: d.Title, Status: a.Status, Kind: a.Kind.Or(), Cadence: a.Cadence,
		LastReviewed: a.LastReviewed, NextReview: nextReview(a), Concepts: targets(a.Concepts), Links: nonNilPointers(a.Links), Rev: d.Rev}, nil
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
	// Kind is project (default: an ongoing body of work goals call home) or
	// concept (a discipline that spans work).
	Kind model.AreaKind `json:"kind,omitempty"`
	// Cadence is how often the owner reviews the area: daily, weekly, monthly,
	// quarterly, yearly, or every <n> days.
	Cadence string `json:"cadence,omitempty"`
	// Concepts are ids of concept areas a project area draws on.
	Concepts []string `json:"concepts,omitempty"`
	// LastReviewed is the date of the owner's last review, YYYY-MM-DD.
	LastReviewed string `json:"last_reviewed,omitempty"`
}

// AreaAddOutput is the output of area_add.
type AreaAddOutput struct {
	ID     string       `json:"id"`
	Folder model.Folder `json:"folder"`
	Rev    string       `json:"rev"`
}

// AreaAdd creates an area. An area is an ongoing responsibility with no end.
var AreaAdd = defineSafe("area_add", "Create an area: kind project, an ongoing body of work goals call home (such as Health or an LLC), or kind concept, a discipline that spans work (such as legal writing); optional review cadence and concepts", func(ctx context.Context, d Deps, in AreaAddInput) (AreaAddOutput, error) {
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
	if in.Kind != "" && !slices.Contains(model.AreaKinds, in.Kind) {
		return AreaAddOutput{}, fmt.Errorf("kind %q must be one of %v", in.Kind, model.AreaKinds)
	}
	reviewed, err := parseDate("last_reviewed", in.LastReviewed)
	if err != nil {
		return AreaAddOutput{}, err
	}
	concepts, err := conceptLinks(ctx, d.Store, in.Concepts)
	if err != nil {
		return AreaAddOutput{}, err
	}
	id := normID(in.ID)
	if id == "" {
		s := slug(title)
		if s == "" {
			return AreaAddOutput{}, fmt.Errorf("title %q has no usable characters: pass id", title)
		}
		id = "area-" + s
	}
	nd := model.Document{
		ID: id, Folder: model.FolderAreas, Title: title, Body: demoteH1(in.Body),
		Meta: &model.Area{Common: model.Common{Type: model.TypeArea}, Status: status, Kind: in.Kind,
			Cadence: strings.Join(strings.Fields(in.Cadence), " "), Concepts: concepts, LastReviewed: reviewed},
	}
	if err := checkKindLinks(ctx, d.Store, nd); err != nil {
		return AreaAddOutput{}, err
	}
	doc, err := d.Store.Create(ctx, nd)
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
	// LastAligned is the date an agent last compared this area with reality, or empty.
	LastAligned string `json:"last_aligned,omitempty"`
	// DrawnOnBy are the goals and project areas whose concepts name this area.
	DrawnOnBy []AreaUser `json:"drawn_on_by"`
}

// AreaUser is a goal or a project area that draws on a concept area.
type AreaUser struct {
	ID    string     `json:"id"`
	Type  model.Type `json:"type"`
	Title string     `json:"title"`
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
	out := AreaShowOutput{Area: v, Body: doc.Body, Projects: []ProjectView{}, Tasks: []TaskView{}, Notes: []NoteView{}, Incidents: []IncidentView{}, Recurring: []TaskView{}, DrawnOnBy: []AreaUser{}}
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
		if slices.Contains(pv.Concepts, doc.ID) {
			out.DrawnOnBy = append(out.DrawnOnBy, AreaUser{ID: pv.ID, Type: model.TypeProject, Title: pv.Title})
		}
	}
	arefs, err := d.Store.List(ctx, store.Filter{Type: model.TypeArea})
	if err != nil {
		return AreaShowOutput{}, err
	}
	for _, r := range arefs {
		ad, a, err := getArea(ctx, d.Store, r.ID)
		if err != nil {
			return AreaShowOutput{}, err
		}
		if slices.Contains(targets(a.Concepts), doc.ID) {
			out.DrawnOnBy = append(out.DrawnOnBy, AreaUser{ID: ad.ID, Type: model.TypeArea, Title: ad.Title})
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
	if out.LastAligned, err = LastAligned(ctx, d.Store, doc.ID); err != nil {
		return AreaShowOutput{}, err
	}
	return out, nil
})
