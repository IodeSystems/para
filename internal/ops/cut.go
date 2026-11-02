package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/iodesystems/parasolve/internal/model"
)

const assumptionsHeading = "## Assumptions"

// DependsCutInput is the input of depends_cut.
type DependsCutInput struct {
	ID     string `json:"id"`     // the task or question that waits
	Dep    string `json:"dep"`    // what it waits on
	Reason string `json:"reason"` // why it can go ahead without the answer: required
}

// DependsCutOutput is the output of depends_cut.
type DependsCutOutput struct {
	ID        string   `json:"id"`
	DependsOn []string `json:"depends_on"`
	Rev       string   `json:"rev"`
}

// DependsCut removes one dependency so the waiting task or question can go
// ahead, and records why as an assumption in its body. Para's history shows
// the edit.
var DependsCut = definePerson("depends_cut", "Cut one dependency so a task or question can go ahead without waiting; the reason is required and is recorded as an assumption on it", func(ctx context.Context, d Deps, in DependsCutInput) (DependsCutOutput, error) {
	reason := strings.Join(strings.Fields(in.Reason), " ")
	if reason == "" {
		return DependsCutOutput{}, errors.New("reason is required: say why this can go ahead without it")
	}
	if err := lintText("reason", reason); err != nil {
		return DependsCutOutput{}, err
	}
	dep, id := normID(in.Dep), normID(in.ID)
	doc, err := d.Store.Get(ctx, id)
	if err != nil {
		return DependsCutOutput{}, err
	}
	var rest []string
	switch doc.Meta.(type) {
	case *model.Task:
		o, err := TaskDependsRemove.Run(ctx, d, TaskDependsInput{ID: id, DependsOn: []string{dep}})
		if err != nil {
			return DependsCutOutput{}, err
		}
		rest = o.DependsOn
	case *model.Question:
		o, err := QuestionDependsRemove.Run(ctx, d, QuestionDependsInput{ID: id, DependsOn: []string{dep}})
		if err != nil {
			return DependsCutOutput{}, err
		}
		rest = o.DependsOn
	default:
		return DependsCutOutput{}, fmt.Errorf("%s is a %s: cut a dependency of a task or a question", id, doc.Meta.Header().Type)
	}
	name := dep
	if dd, err := d.Store.Get(ctx, dep); err == nil && strings.TrimSpace(dd.Title) != "" {
		name = dd.Title + " [[" + dep + "]]"
	}
	doc, err = d.Store.Get(ctx, id)
	if err != nil {
		return DependsCutOutput{}, err
	}
	doc.Body = appendToSection(doc.Body, assumptionsHeading, fmt.Sprintf("- Went ahead without %s: %s (%s, via %s, on %s)",
		name, reason, d.Store.Info().Actor, d.Store.Info().Surface, d.Store.Now().Format(dateLayout)))
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return DependsCutOutput{}, fmt.Errorf("%s no longer waits on %s but the assumption was not recorded: %w", id, dep, err)
	}
	return DependsCutOutput{ID: id, DependsOn: rest, Rev: saved.Rev}, nil
})
