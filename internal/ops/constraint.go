package ops

import (
	"context"
	"fmt"
	"strings"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/upgrade"
)

// ConstraintView is a constraint as list and show report it. Says is the
// document title: the "no" itself.
type ConstraintView struct {
	ID        string `json:"id"`
	Says      string `json:"says"`
	Why       string `json:"why"`
	By        string `json:"by"`
	SourceURL string `json:"source_url"`
	// SourceVerify is the sentence that tells where a no with no link was heard.
	SourceVerify string     `json:"source_verify,omitempty"`
	AppliesIn    []string   `json:"applies_in"`
	NotIn        []string   `json:"not_in"`
	ReviewBy     model.Date `json:"review_by"`
	Refines      string     `json:"refines,omitempty"` // the constraint this one specializes
}

func constraintView(doc model.Document, c *model.Constraint) ConstraintView {
	return ConstraintView{
		ID: doc.ID, Says: doc.Title, Why: c.Why, By: c.By, SourceURL: c.Source.URL, SourceVerify: c.Source.Verify,
		AppliesIn: nonNil(c.AppliesIn), NotIn: nonNil(c.NotIn), ReviewBy: c.ReviewBy, Refines: c.Refines.Target(),
	}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// getConstraint reads a constraint file, or a constraint annotation by its id
// or the id of the file it was folded from.
func getConstraint(ctx context.Context, d Deps, id string) (model.Document, *model.Constraint, error) {
	x, err := newRuleIndex(ctx, d.Store)
	if err != nil {
		return model.Document{}, nil, err
	}
	doc, err := x.get(id)
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
	Says   string `json:"says"`   // the "no", one line
	Why    string `json:"why"`    // the reason given
	By     string `json:"by"`     // who said it
	Source string `json:"source"` // where: stable http(s) URL
	// Verify replaces source for the owner's no heard with no link: one line, who and where.
	Verify    string   `json:"verify,omitempty"`
	AppliesIn []string `json:"applies_in,omitempty"` // scopes where it holds
	NotIn     []string `json:"not_in,omitempty"`     // scopes where it does not
	ReviewBy  string   `json:"review_by"`            // YYYY-MM-DD, when to ask again
	Text      string   `json:"text,omitempty"`       // optional body
	Refines   string   `json:"refines,omitempty"`    // id of the more general constraint this one specializes
	// ImportedFrom is the file the new document comes from (import_verify).
	ImportedFrom *model.ImportedFrom `json:"imported_from,omitempty"`
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
	var u, verify string
	if strings.TrimSpace(in.Source) == "" && strings.TrimSpace(in.Verify) != "" && !agentSurface(d) {
		var err error
		if verify, err = oneLine("verify", in.Verify); err != nil {
			return ConstraintAddOutput{}, err
		}
	} else {
		var err error
		if u, err = checkURL(in.Source); err != nil {
			return ConstraintAddOutput{}, fmt.Errorf("source: %w", err)
		}
	}
	if err := checkDate("review_by", in.ReviewBy); err != nil {
		return ConstraintAddOutput{}, err
	}
	refines, err := refinesLink(ctx, d.Store, in.Refines, model.TypeConstraint)
	if err != nil {
		return ConstraintAddOutput{}, err
	}
	nd := model.Document{
		Folder: model.FolderConstraints,
		Title:  f[0],
		Body:   strings.TrimSpace(in.Text),
		Meta: &model.Constraint{
			Common: model.Common{Type: model.TypeConstraint},
			Why:    f[1], By: f[2],
			Source:    model.Source{URL: u, Verify: verify, Retrieved: timestamp(), Fidelity: model.FidelityNone, Backend: backendManual},
			AppliesIn: splitList(in.AppliesIn), NotIn: splitList(in.NotIn),
			ReviewBy: model.Date(in.ReviewBy), Refines: refines,
		},
	}
	if policyDocsOn(d) {
		// An alias of annotate: the constraint is an annotation in the first area applies_in names.
		f, _ := upgrade.AnnotationOf(nd, storeAreas{ctx, d.Store})
		out, err := insertFold(ctx, d, f)
		return ConstraintAddOutput{ID: out.ID, Rev: out.Rev}, err
	}
	doc, err := d.Store.Create(ctx, nd)
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
var ConstraintList = defineRead("constraint_list", "List constraints", func(ctx context.Context, d Deps, _ ConstraintListInput) (ConstraintListOutput, error) {
	x, err := newRuleIndex(ctx, d.Store)
	if err != nil {
		return ConstraintListOutput{}, err
	}
	docs, err := x.list(model.TypeConstraint)
	if err != nil {
		return ConstraintListOutput{}, err
	}
	out := ConstraintListOutput{Constraints: []ConstraintView{}}
	for _, doc := range docs {
		if c, ok := doc.Meta.(*model.Constraint); ok {
			out.Constraints = append(out.Constraints, constraintView(doc, c))
		}
	}
	return out, nil
})

// ConstraintShowInput is the input of constraint_show.
type ConstraintShowInput struct {
	ID string `json:"id" query:"id" required:"true"`
}

// ConstraintShowOutput is the output of constraint_show.
type ConstraintShowOutput struct {
	ConstraintView `json:",inline"`
	Body           string `json:"body"`
	Rev            string `json:"rev"`
}

// ConstraintShow shows one constraint.
var ConstraintShow = defineRead("constraint_show", "Show one constraint", func(ctx context.Context, d Deps, in ConstraintShowInput) (ConstraintShowOutput, error) {
	doc, c, err := getConstraint(ctx, d, in.ID)
	if err != nil {
		return ConstraintShowOutput{}, err
	}
	return ConstraintShowOutput{ConstraintView: constraintView(doc, c), Body: doc.Body, Rev: doc.Rev}, nil
})
