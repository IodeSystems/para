package ops

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/upgrade"
)

// ConventionView is a convention as list and show report it.
type ConventionView struct {
	ID        string                 `json:"id"`
	Title     string                 `json:"title"`
	Status    model.ConventionStatus `json:"status"`
	AppliesIn []string               `json:"applies_in"`
	Refines   string                 `json:"refines,omitempty"` // the convention this one specializes
}

func conventionView(doc model.Document, c *model.Convention) ConventionView {
	return ConventionView{ID: doc.ID, Title: doc.Title, Status: c.Status, AppliesIn: nonNil(c.AppliesIn), Refines: c.Refines.Target()}
}

// getConvention reads a convention file, or a convention annotation by its id
// or the id of the file it was folded from.
func getConvention(ctx context.Context, d Deps, id string) (model.Document, *model.Convention, error) {
	x, err := newRuleIndex(ctx, d.Store)
	if err != nil {
		return model.Document{}, nil, err
	}
	doc, err := x.get(id)
	if err != nil {
		return doc, nil, err
	}
	c, ok := doc.Meta.(*model.Convention)
	if !ok {
		return doc, nil, fmt.Errorf("%s is a %s, not a convention", id, doc.Meta.Header().Type)
	}
	return doc, c, nil
}

// ConventionAddInput is the input of convention_add.
type ConventionAddInput struct {
	Title     string                 `json:"title"`
	Text      string                 `json:"text,omitempty"`       // body
	Status    model.ConventionStatus `json:"status,omitempty"`     // empty means draft
	AppliesIn []string               `json:"applies_in,omitempty"` // scopes: words, or ids of areas, goals, and tasks
	Refines   string                 `json:"refines,omitempty"`    // id of the more general convention this one specializes
	// ImportedFrom is the file the new document comes from (import_verify).
	ImportedFrom *model.ImportedFrom `json:"imported_from,omitempty"`
}

// ConventionAddOutput is the output of convention_add.
type ConventionAddOutput struct {
	ID  string `json:"id"`
	Rev string `json:"rev"`
}

// ConventionAdd records a convention.
var ConventionAdd = define("convention_add", "Record a convention", func(ctx context.Context, d Deps, in ConventionAddInput) (ConventionAddOutput, error) {
	title, err := oneLine("title", in.Title)
	if err != nil {
		return ConventionAddOutput{}, err
	}
	status := in.Status
	if in.Status == "" {
		status = model.ConventionDraft
	}
	if !slices.Contains(model.ConventionStatuses, status) {
		return ConventionAddOutput{}, fmt.Errorf("status %q must be one of %v", in.Status, model.ConventionStatuses)
	}
	refines, err := refinesLink(ctx, d.Store, in.Refines, model.TypeConvention)
	if err != nil {
		return ConventionAddOutput{}, err
	}
	nd := model.Document{
		Folder: model.FolderConventions, Title: title, Body: strings.TrimSpace(in.Text),
		Meta: &model.Convention{Common: model.Common{Type: model.TypeConvention}, Status: status, AppliesIn: splitList(in.AppliesIn), Refines: refines},
	}
	if policyDocsOn(d) {
		// An alias of annotate: the convention is an annotation in the first area applies_in names.
		f, _ := upgrade.AnnotationOf(nd, storeAreas{ctx, d.Store})
		out, err := insertFold(ctx, d, f)
		return ConventionAddOutput{ID: out.ID, Rev: out.Rev}, err
	}
	doc, err := d.Store.Create(ctx, nd)
	if err != nil {
		return ConventionAddOutput{}, err
	}
	return ConventionAddOutput{ID: doc.ID, Rev: doc.Rev}, nil
})

// ConventionListInput is the input of convention_list.
type ConventionListInput struct{}

// ConventionListOutput is the output of convention_list.
type ConventionListOutput struct {
	Conventions []ConventionView `json:"conventions"`
}

// ConventionList lists conventions.
var ConventionList = defineRead("convention_list", "List conventions", func(ctx context.Context, d Deps, _ ConventionListInput) (ConventionListOutput, error) {
	x, err := newRuleIndex(ctx, d.Store)
	if err != nil {
		return ConventionListOutput{}, err
	}
	docs, err := x.list(model.TypeConvention)
	if err != nil {
		return ConventionListOutput{}, err
	}
	out := ConventionListOutput{Conventions: []ConventionView{}}
	for _, doc := range docs {
		if c, ok := doc.Meta.(*model.Convention); ok {
			out.Conventions = append(out.Conventions, conventionView(doc, c))
		}
	}
	return out, nil
})

// ConventionShowInput is the input of convention_show.
type ConventionShowInput struct {
	ID string `json:"id" query:"id" required:"true"`
}

// ConventionShowOutput is the output of convention_show.
type ConventionShowOutput struct {
	ConventionView `json:",inline"`
	Body           string `json:"body"`
	Rev            string `json:"rev"`
}

// ConventionShow shows one convention.
var ConventionShow = defineRead("convention_show", "Show one convention", func(ctx context.Context, d Deps, in ConventionShowInput) (ConventionShowOutput, error) {
	doc, c, err := getConvention(ctx, d, in.ID)
	if err != nil {
		return ConventionShowOutput{}, err
	}
	return ConventionShowOutput{ConventionView: conventionView(doc, c), Body: doc.Body, Rev: doc.Rev}, nil
})
