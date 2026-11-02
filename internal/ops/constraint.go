package ops

import (
	"context"
	"fmt"
	"strings"

	"github.com/iodesystems/paradize/internal/model"
	"github.com/iodesystems/paradize/internal/store"
)

// ConstraintView is a constraint as list and show report it. Says is the
// document title: the "no" itself.
type ConstraintView struct {
	ID        string     `json:"id"`
	Says      string     `json:"says"`
	Why       string     `json:"why"`
	By        string     `json:"by"`
	SourceURL string     `json:"source_url"`
	AppliesIn []string   `json:"applies_in"`
	NotIn     []string   `json:"not_in"`
	ReviewBy  model.Date `json:"review_by"`
}

func constraintView(doc model.Document, c *model.Constraint) ConstraintView {
	return ConstraintView{
		ID: doc.ID, Says: doc.Title, Why: c.Why, By: c.By, SourceURL: c.Source.URL,
		AppliesIn: nonNil(c.AppliesIn), NotIn: nonNil(c.NotIn), ReviewBy: c.ReviewBy,
	}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func getConstraint(ctx context.Context, d Deps, id string) (model.Document, *model.Constraint, error) {
	doc, err := d.Store.Get(ctx, id)
	if err != nil {
		return doc, nil, err
	}
	c, ok := doc.Meta.(*model.Constraint)
	if !ok {
		return doc, nil, fmt.Errorf("%s is a %s, not a constraint", id, doc.Meta.Header().Type)
	}
	return doc, c, nil
}

// ConstraintAddInput is the input of constraint_add.
type ConstraintAddInput struct {
	Says      string   `json:"says"`                 // the "no", one line
	Why       string   `json:"why"`                  // the reason given
	By        string   `json:"by"`                   // who said it
	Source    string   `json:"source"`               // where: stable http(s) URL
	AppliesIn []string `json:"applies_in,omitempty"` // scopes where it holds
	NotIn     []string `json:"not_in,omitempty"`     // scopes where it does not
	ReviewBy  string   `json:"review_by"`            // YYYY-MM-DD, when to ask again
	Text      string   `json:"text,omitempty"`       // optional body
}

// ConstraintAddOutput is the output of constraint_add.
type ConstraintAddOutput struct {
	ID  string `json:"id"`
	Rev string `json:"rev"`
}

// ConstraintAdd records a justified "no". Its source is a URL, not fetched:
// fidelity none.
var ConstraintAdd = define("constraint_add", "Record a justified no: says, why, by, source, scopes, review date", func(ctx context.Context, d Deps, in ConstraintAddInput) (ConstraintAddOutput, error) {
	var f [3]string
	for i, p := range []struct{ name, v string }{{"says", in.Says}, {"why", in.Why}, {"by", in.By}} {
		var err error
		if f[i], err = oneLine(p.name, p.v); err != nil {
			return ConstraintAddOutput{}, err
		}
	}
	u, err := checkURL(in.Source)
	if err != nil {
		return ConstraintAddOutput{}, fmt.Errorf("source: %w", err)
	}
	if err := checkDate("review_by", in.ReviewBy); err != nil {
		return ConstraintAddOutput{}, err
	}
	doc, err := d.Store.Create(ctx, model.Document{
		Folder: model.FolderConstraints,
		Title:  f[0],
		Body:   strings.TrimSpace(in.Text),
		Meta: &model.Constraint{
			Common: model.Common{Type: model.TypeConstraint},
			Why:    f[1], By: f[2],
			Source:    model.Source{URL: u, Retrieved: timestamp(), Fidelity: model.FidelityNone, Backend: backendManual},
			AppliesIn: cleanList(in.AppliesIn), NotIn: cleanList(in.NotIn),
			ReviewBy: model.Date(in.ReviewBy),
		},
	})
	if err != nil {
		return ConstraintAddOutput{}, err
	}
	return ConstraintAddOutput{ID: doc.ID, Rev: doc.Rev}, nil
})

// ConstraintListInput is the input of constraint_list.
type ConstraintListInput struct{}

// ConstraintListOutput is the output of constraint_list.
type ConstraintListOutput struct {
	Constraints []ConstraintView `json:"constraints"`
}

// ConstraintList lists constraints.
var ConstraintList = define("constraint_list", "List constraints", func(ctx context.Context, d Deps, _ ConstraintListInput) (ConstraintListOutput, error) {
	refs, err := d.Store.List(ctx, store.Filter{Type: model.TypeConstraint})
	if err != nil {
		return ConstraintListOutput{}, err
	}
	out := ConstraintListOutput{Constraints: []ConstraintView{}}
	for _, r := range refs {
		doc, c, err := getConstraint(ctx, d, r.ID)
		if err != nil {
			return ConstraintListOutput{}, err
		}
		out.Constraints = append(out.Constraints, constraintView(doc, c))
	}
	return out, nil
})

// ConstraintShowInput is the input of constraint_show.
type ConstraintShowInput struct {
	ID string `json:"id"`
}

// ConstraintShowOutput is the output of constraint_show.
type ConstraintShowOutput struct {
	ConstraintView `json:",inline"`
	Body           string `json:"body"`
	Rev            string `json:"rev"`
}

// ConstraintShow shows one constraint.
var ConstraintShow = define("constraint_show", "Show one constraint", func(ctx context.Context, d Deps, in ConstraintShowInput) (ConstraintShowOutput, error) {
	doc, c, err := getConstraint(ctx, d, in.ID)
	if err != nil {
		return ConstraintShowOutput{}, err
	}
	return ConstraintShowOutput{ConstraintView: constraintView(doc, c), Body: doc.Body, Rev: doc.Rev}, nil
})
