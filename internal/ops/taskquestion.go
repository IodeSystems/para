package ops

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/store"
)

// TaskToQuestionInput is the input of task_to_question.
type TaskToQuestionInput struct {
	ID          string             `json:"id"`
	Title       string             `json:"title,omitempty"`       // retitle the question; default the task title
	Kind        model.QuestionKind `json:"kind,omitempty"`        // question (default), decision, or input
	Options     []string           `json:"options,omitempty"`     // the choices, for a decision
	Recommended string             `json:"recommended,omitempty"` // one of the options
}

// TaskToQuestionOutput is the output of task_to_question.
type TaskToQuestionOutput struct {
	Question string `json:"question"`
	Task     string `json:"task"` // the old task, now dropped
	// Relinked lists the tasks and requests whose dependency moved from the old
	// task to the question.
	Relinked []string `json:"relinked"`
	Rev      string   `json:"rev"`
}

// TaskToQuestion converts a task that is really a question: something to
// answer, not work to do. The question takes the task's title and body and its
// project (or its area). Every open task and request that depended on the task
// now depends on the question. The old task is dropped and links to the
// question. File names are unique, so the question gets a new id.
var TaskToQuestion = define("task_to_question", "Convert a task that is really a question or a decision: dependents move to the question, the task is dropped", func(ctx context.Context, d Deps, in TaskToQuestionInput) (TaskToQuestionOutput, error) {
	doc, t, err := getTask(ctx, d.Store, normID(in.ID))
	if err != nil {
		return TaskToQuestionOutput{}, err
	}
	if taskTerminal(t.Status) {
		return TaskToQuestionOutput{}, fmt.Errorf("%s is already %s", doc.ID, t.Status)
	}
	opts, rec, err := checkOptions(in.Kind, in.Options, in.Recommended)
	if err == nil {
		err = checkDecision(d, in.Kind, opts, rec, nil)
	}
	if err != nil {
		return TaskToQuestionOutput{}, err
	}
	// A question is scoped; the task's project, else its area.
	var scope model.Link
	switch {
	case t.Project != "":
		scope = t.Project
	case t.Area != "":
		scope = t.Area
	default:
		return TaskToQuestionOutput{}, fmt.Errorf("%s has no project or area to scope the question to", doc.ID)
	}
	kind := in.Kind
	if kind == model.KindQuestion {
		kind = ""
	}
	body := joinBody(doc.Body, "Converted from [["+doc.ID+"]].")
	title := doc.Title
	if strings.TrimSpace(in.Title) != "" {
		if title, err = oneLine("title", in.Title); err != nil {
			return TaskToQuestionOutput{}, err
		}
	}
	q, err := d.Store.Create(ctx, model.Document{
		Folder: model.FolderQuestions, Title: title, Body: body,
		Meta: &model.Question{
			Common: model.Common{Type: model.TypeQuestion}, Status: model.QuestionOpen, Kind: kind,
			Scope: scope, Options: opts, Recommended: rec, DependsOn: slices.Clone(t.DependsOn),
		},
	})
	if err != nil {
		return TaskToQuestionOutput{}, err
	}
	out := TaskToQuestionOutput{Question: q.ID, Task: doc.ID, Relinked: []string{}}
	half := func(err error) error {
		return fmt.Errorf("question %s was made but %s was not fully converted: %w", q.ID, doc.ID, err)
	}
	relinked, err := relinkDependents(ctx, d.Store, doc.ID, q.ID)
	out.Relinked = append(out.Relinked, relinked...)
	if err != nil {
		return out, half(err)
	}
	// Re-read: relinking can change documents the task depends on, not the task, but Rev must be current.
	doc, t, err = getTask(ctx, d.Store, doc.ID)
	if err != nil {
		return out, half(err)
	}
	t.Status, t.BlockedBy = model.TaskDropped, nil
	doc.Body = joinBody(doc.Body, "Converted to [["+q.ID+"]].")
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return out, half(err)
	}
	out.Rev = saved.Rev
	return out, nil
})

// relinkDependents replaces a dependency on from with one on to, in every open
// task and open request. It returns the ids it changed.
func relinkDependents(ctx context.Context, s store.Store, from, to string) ([]string, error) {
	dp, err := loadDependents(ctx, s)
	if err != nil {
		return nil, err
	}
	swap := func(links []model.Link) []model.Link {
		var out []model.Link
		for _, l := range links {
			if l.Target() == from {
				l = model.LinkTo(to)
			}
			if !slices.Contains(out, l) {
				out = append(out, l)
			}
		}
		return out
	}
	var done []string
	for _, id := range dp.by[from] {
		doc, err := s.Get(ctx, id)
		if err != nil {
			return done, err
		}
		switch m := doc.Meta.(type) {
		case *model.Task:
			m.DependsOn = swap(m.DependsOn)
		case *model.RFX:
			m.DependsOn = swap(m.DependsOn)
		case *model.Question:
			m.DependsOn = swap(m.DependsOn)
		default:
			continue
		}
		if _, err := s.Update(ctx, doc); err != nil {
			return done, fmt.Errorf("relink %s: %w", id, err)
		}
		done = append(done, id)
	}
	slices.Sort(done)
	return done, nil
}
