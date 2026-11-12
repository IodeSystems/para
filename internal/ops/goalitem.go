package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// GoalView is a goal without its body.
type GoalView struct {
	ID     string           `json:"id"`
	Folder model.Folder     `json:"folder"`
	Title  string           `json:"title"`
	Status model.GoalStatus `json:"status"`
	Owner  string           `json:"owner"`
	Area   string           `json:"area,omitempty"`
	// Project is the id of the ongoing project the goal belongs to.
	Project string `json:"project,omitempty"`
	// ResumeWhen is what a parked goal waits on.
	ResumeWhen string `json:"resume_when,omitempty"`
	// Concepts are the ids of the concept areas the goal draws on.
	Concepts []string `json:"concepts"`
	// DoneWhen is how we know the goal is reached; empty on an older goal.
	DoneWhen string          `json:"done_when,omitempty"`
	Due      model.Date      `json:"due,omitempty"`
	Links    []model.Pointer `json:"links,omitempty"`
	// Brief is the goal brief, when the goal has one.
	Brief *model.GoalBrief `json:"brief,omitempty"`
	Rev   string           `json:"rev"`
	// AreaName is the title of Area, ProjectName of Project, for people.
	AreaName    string `json:"area_name,omitempty" nameof:"Area"`
	ProjectName string `json:"project_name,omitempty" nameof:"Project"`
}

func goalView(d model.Document) (GoalView, error) {
	p, ok := d.Meta.(*model.Goal)
	if !ok {
		return GoalView{}, fmt.Errorf("%s is a %s, not a goal", d.ID, d.Meta.Header().Type)
	}
	v := GoalView{ID: d.ID, Folder: d.Folder, Title: d.Title, Status: p.Status, Owner: p.Owner, Area: p.Area.Target(), Project: p.Project.Target(), ResumeWhen: p.ResumeWhen, Concepts: targets(p.Concepts), DoneWhen: p.DoneWhen, Due: p.Due, Links: p.Links, Rev: d.Rev}
	if p.HasBrief() {
		v.Brief = &p.GoalBrief
	}
	return v, nil
}

// getGoal reads a document that must be a goal.
func getGoal(ctx context.Context, s store.Store, id string) (model.Document, *model.Goal, error) {
	d, err := s.Get(ctx, id)
	if err != nil {
		return d, nil, missing("goal", id, err)
	}
	p, ok := d.Meta.(*model.Goal)
	if !ok {
		return d, nil, fmt.Errorf("%s is a %s, not a goal", id, d.Meta.Header().Type)
	}
	return d, p, nil
}

// GoalAddInput is the input of goal_add.
type GoalAddInput struct {
	Title string `json:"title"`
	// ID is optional, a slug; default goal-<generated>. Give it when a
	// later item of the same proposal names this goal.
	ID    string `json:"id,omitempty"`
	Owner string `json:"owner,omitempty"` // default: the vault's owner property, else "self"
	Area  string `json:"area,omitempty"`  // id of an area of kind project: the goal's home
	// Project is the id of the ongoing project the goal belongs to (vault
	// format 11).
	Project string `json:"project,omitempty"`
	Body    string `json:"body,omitempty"`
	// Concepts are ids of concept areas the goal draws on.
	Concepts []string `json:"concepts,omitempty"`
	// DoneWhen is required: how we know the goal is reached, in one line. A
	// goal that never finishes is an area.
	DoneWhen string `json:"done_when"`
	Due      string `json:"due,omitempty"` // YYYY-MM-DD the goal is due; work leading to it inherits the date
	// ImportedFrom is the file the new document comes from (import_verify).
	ImportedFrom *model.ImportedFrom `json:"imported_from,omitempty"`
}

// GoalAddOutput is the output of goal_add.
type GoalAddOutput struct {
	ID     string       `json:"id"`
	Folder model.Folder `json:"folder"`
	Rev    string       `json:"rev"`
	// Warnings: the vault has more active goals than goals.active_limit allows.
	Warnings []string `json:"warnings"`
}

// GoalAdd creates an active goal.
var GoalAdd = defineSafe("goal_add", "Create a goal: a finite outcome with a done condition", func(ctx context.Context, d Deps, in GoalAddInput) (GoalAddOutput, error) {
	title := strings.Join(strings.Fields(in.Title), " ")
	if title == "" {
		return GoalAddOutput{}, errors.New("title is empty")
	}
	doneWhen, err := checkDoneWhen(in.DoneWhen, true)
	if err != nil {
		return GoalAddOutput{}, fmt.Errorf("%w: a goal needs a done condition; if it never finishes, it is an area (add {type: area})", err)
	}
	owner := strings.TrimSpace(in.Owner)
	if owner == "" {
		var err error
		if owner, err = d.Store.Config("owner"); err != nil {
			return GoalAddOutput{}, err
		}
		if owner = strings.TrimSpace(owner); owner == "" {
			owner = "self"
		}
	}
	area, err := areaLink(ctx, d.Store, in.Area)
	if err != nil {
		return GoalAddOutput{}, err
	}
	if in.Due != "" {
		if err := checkDate("due", in.Due); err != nil {
			return GoalAddOutput{}, err
		}
	}
	concepts, err := conceptLinks(ctx, d.Store, in.Concepts)
	if err != nil {
		return GoalAddOutput{}, err
	}
	project, err := projectLink(ctx, d.Store, in.Project)
	if err != nil {
		return GoalAddOutput{}, err
	}
	nd := model.Document{
		ID:     normID(in.ID),
		Folder: model.FolderGoals,
		Title:  title,
		Body:   demoteH1(in.Body),
		Meta: &model.Goal{
			Common:   model.Common{Type: model.TypeGoal},
			Status:   model.GoalActive,
			Owner:    owner,
			Project:  project,
			Area:     area,
			Concepts: concepts,
			DoneWhen: doneWhen,
			Due:      model.Date(in.Due),
		},
	}
	if err := checkKindLinks(ctx, d.Store, nd); err != nil {
		return GoalAddOutput{}, err
	}
	doc, err := d.Store.Create(ctx, nd)
	if err != nil {
		return GoalAddOutput{}, err
	}
	out := GoalAddOutput{ID: doc.ID, Folder: doc.Folder, Rev: doc.Rev, Warnings: []string{}}
	w, err := activeLimitWarning(ctx, d.Store)
	if err != nil {
		return GoalAddOutput{}, fmt.Errorf("goal_add: %w", err)
	}
	if w != "" {
		out.Warnings = append(out.Warnings, w)
	}
	return out, nil
})

// GoalListInput is the input of goal_list.
type GoalListInput struct {
	All bool `json:"all,omitempty" query:"all"` // include archived goals
}

// GoalListOutput is the output of goal_list.
type GoalListOutput struct {
	Goals []GoalView `json:"goals"`
}

// GoalList lists goals, active folder only unless All.
var GoalList = defineRead("goal_list", "List goals", func(ctx context.Context, d Deps, in GoalListInput) (GoalListOutput, error) {
	refs, err := d.Store.List(ctx, store.Filter{Type: model.TypeGoal, All: in.All})
	if err != nil {
		return GoalListOutput{}, err
	}
	out := GoalListOutput{Goals: []GoalView{}}
	for _, r := range refs {
		if r.Folder == model.FolderArchive && !in.All {
			continue
		}
		doc, err := d.Store.Get(ctx, r.ID)
		if err != nil {
			return GoalListOutput{}, err
		}
		v, err := goalView(doc)
		if err != nil {
			return GoalListOutput{}, err
		}
		out.Goals = append(out.Goals, v)
	}
	return out, nil
})

// GoalShowInput is the input of goal_show.
type GoalShowInput struct {
	ID string `json:"id" query:"id" required:"true"`
}

// GoalShowOutput is the output of goal_show.
type GoalShowOutput struct {
	Goal  GoalView   `json:"goal"`
	Body  string     `json:"body"`
	Tasks []TaskView `json:"tasks"`
	// Questions are the open questions in scope: the goal's own and those of its tasks.
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

// GoalShow returns a goal and its tasks.
var GoalShow = defineRead("goal_show", "Show a goal and its tasks", func(ctx context.Context, d Deps, in GoalShowInput) (GoalShowOutput, error) {
	doc, _, err := getGoal(ctx, d.Store, in.ID)
	if err != nil {
		return GoalShowOutput{}, err
	}
	v, err := goalView(doc)
	if err != nil {
		return GoalShowOutput{}, err
	}
	tasks, err := listTasks(ctx, d.Store, in.ID, "")
	if err != nil {
		return GoalShowOutput{}, err
	}
	qs, err := openQuestions(ctx, d.Store, func(x qrow) bool { return x.project == doc.ID })
	if err != nil {
		return GoalShowOutput{}, err
	}
	aligned, err := LastAligned(ctx, d.Store, doc.ID)
	if err != nil {
		return GoalShowOutput{}, err
	}
	rules, err := rulesShort(ctx, d.Store, doc.ID)
	if err != nil {
		return GoalShowOutput{}, err
	}
	cutoff, err := recheckCutoff(d.Store)
	if err != nil {
		return GoalShowOutput{}, err
	}
	return GoalShowOutput{Goal: v, Body: doc.Body, Tasks: tasks, Questions: qs, LastAligned: aligned, Rules: rules, Assumptions: assumptionsOf(doc, cutoff)}, nil
})

// GoalCloseInput is the input of goal_close.
type GoalCloseInput struct {
	ID      string `json:"id"`
	Dropped bool   `json:"dropped,omitempty"` // close as dropped, not done
	Force   bool   `json:"force,omitempty"`   // close with open tasks
}

// GoalCloseOutput is the output of goal_close.
type GoalCloseOutput struct {
	ID     string           `json:"id"`
	Status model.GoalStatus `json:"status"`
	Folder model.Folder     `json:"folder"`
	Rev    string           `json:"rev"`
}

// GoalClose sets the goal done (or dropped) and moves it to archive.
// Open tasks refuse the close unless Force. Tasks stay where they are: links
// to the archived goal keep resolving.
var GoalClose = define("goal_close", "Close a goal and move it to archive", func(ctx context.Context, d Deps, in GoalCloseInput) (GoalCloseOutput, error) {
	doc, p, err := getGoal(ctx, d.Store, in.ID)
	if err != nil {
		return GoalCloseOutput{}, err
	}
	if doc.Folder == model.FolderArchive {
		return GoalCloseOutput{}, fmt.Errorf("%s is already archived", in.ID)
	}
	if !in.Force {
		tasks, err := listTasks(ctx, d.Store, in.ID, "")
		if err != nil {
			return GoalCloseOutput{}, err
		}
		var open []string
		for _, t := range tasks {
			if !taskTerminal(t.Status) {
				open = append(open, t.ID)
			}
		}
		if len(open) > 0 {
			return GoalCloseOutput{}, fmt.Errorf("%s has open tasks: %s (finish them or use force)", in.ID, strings.Join(open, " "))
		}
	}
	p.Status = model.GoalDone
	if in.Dropped {
		p.Status = model.GoalDropped
	}
	if _, err := d.Store.Update(ctx, doc); err != nil {
		return GoalCloseOutput{}, err
	}
	moved, err := d.Store.Move(ctx, in.ID, model.FolderArchive)
	if err != nil {
		return GoalCloseOutput{}, err
	}
	return GoalCloseOutput{ID: moved.ID, Status: p.Status, Folder: moved.Folder, Rev: moved.Rev}, nil
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

// GoalEditInput is the input of goal_edit.
type GoalEditInput struct {
	ID       string `json:"id"`
	DoneWhen string `json:"done_when,omitempty"` // how we know the goal is reached; required unless only the date changes
	Due      string `json:"due,omitempty"`       // YYYY-MM-DD the goal is due
	ClearDue bool   `json:"clear_due,omitempty"` // remove the due date
	// Area is the id of the goal's home, an area of kind project. Concepts
	// replace the concept areas the goal draws on. Clear either with doc_edit.
	Area     string   `json:"area,omitempty"`
	Concepts []string `json:"concepts,omitempty"`
	// Status moves the goal between active, paused, and parked (the icebox;
	// close ends it). Parked needs resume_when: what it waits on.
	Status     model.GoalStatus `json:"status,omitempty"`
	ResumeWhen string           `json:"resume_when,omitempty"`
}

// GoalEditOutput is the output of goal_edit.
type GoalEditOutput struct {
	ID       string `json:"id"`
	DoneWhen string `json:"done_when"`
	Rev      string `json:"rev"`
}

// goalStatusEdit sets the status and resume_when of a goal p in doc. Parked
// needs a resume_when, given or already set; leaving parked clears it.
func goalStatusEdit(doc model.Document, p *model.Goal, status model.GoalStatus, resume string) error {
	resume = strings.Join(strings.Fields(resume), " ")
	if resume != "" {
		if err := lintText("resume_when", resume); err != nil {
			return err
		}
	}
	switch status {
	case "":
		if resume != "" && p.Status != model.GoalParked {
			return errors.New("resume_when is what a parked goal waits on: give status parked with it")
		}
	case model.GoalActive, model.GoalPaused, model.GoalParked:
		if doc.Folder == model.FolderArchive {
			return fmt.Errorf("%s is closed: reopen it first", doc.ID)
		}
		if status == model.GoalParked && resume == "" && p.ResumeWhen == "" {
			return errors.New("status parked needs resume_when: what the goal waits on to resume, a condition or a date")
		}
		if status != model.GoalParked {
			if resume != "" {
				return fmt.Errorf("resume_when goes with status parked, not %s", status)
			}
			p.ResumeWhen = ""
		}
		p.Status = status
	default:
		return fmt.Errorf("status %q: want active, paused, or parked; close {id, outcome} ends a goal", status)
	}
	if resume != "" {
		p.ResumeWhen = resume
	}
	return nil
}

// GoalEdit sets the done condition of a goal.
var GoalEdit = define("goal_edit", "Set the done condition, the due date, the home area, the concepts, or the status (active, paused, parked with resume_when) of a goal", func(ctx context.Context, d Deps, in GoalEditInput) (GoalEditOutput, error) {
	doc, p, err := getGoal(ctx, d.Store, normID(in.ID))
	if err != nil {
		return GoalEditOutput{}, err
	}
	dw := p.DoneWhen
	other := in.Due != "" || in.ClearDue || in.Area != "" || len(in.Concepts) > 0 || in.Status != "" || in.ResumeWhen != ""
	if in.DoneWhen != "" || !other {
		if dw, err = checkDoneWhen(in.DoneWhen, true); err != nil {
			return GoalEditOutput{}, err
		}
		p.DoneWhen = dw
	}
	switch {
	case in.Due != "" && in.ClearDue:
		return GoalEditOutput{}, errors.New("give due or clear_due, not both")
	case in.Due != "":
		if err := checkDate("due", in.Due); err != nil {
			return GoalEditOutput{}, err
		}
		p.Due = model.Date(in.Due)
	case in.ClearDue:
		p.Due = ""
	}
	if in.Area != "" {
		if p.Area, err = areaLink(ctx, d.Store, in.Area); err != nil {
			return GoalEditOutput{}, err
		}
	}
	if len(in.Concepts) > 0 {
		if p.Concepts, err = conceptLinks(ctx, d.Store, in.Concepts); err != nil {
			return GoalEditOutput{}, err
		}
	}
	if err := goalStatusEdit(doc, p, in.Status, in.ResumeWhen); err != nil {
		return GoalEditOutput{}, err
	}
	if err := checkKindLinks(ctx, d.Store, doc); err != nil {
		return GoalEditOutput{}, err
	}
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return GoalEditOutput{}, err
	}
	return GoalEditOutput{ID: doc.ID, DoneWhen: dw, Rev: saved.Rev}, nil
})
