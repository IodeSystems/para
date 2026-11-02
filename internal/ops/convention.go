package ops

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/iodesystems/paradize/internal/model"
	"github.com/iodesystems/paradize/internal/store"
)

// ConventionView is a convention as list and show report it.
type ConventionView struct {
	ID        string                 `json:"id"`
	Title     string                 `json:"title"`
	Status    model.ConventionStatus `json:"status"`
	AppliesIn []string               `json:"applies_in"`
}

func conventionView(doc model.Document, c *model.Convention) ConventionView {
	return ConventionView{ID: doc.ID, Title: doc.Title, Status: c.Status, AppliesIn: nonNil(c.AppliesIn)}
}

func getConvention(ctx context.Context, d Deps, id string) (model.Document, *model.Convention, error) {
	doc, err := d.Store.Get(ctx, id)
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
	AppliesIn []string               `json:"applies_in,omitempty"` // scopes
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
	doc, err := d.Store.Create(ctx, model.Document{
		Folder: model.FolderConventions, Title: title, Body: strings.TrimSpace(in.Text),
		Meta: &model.Convention{Common: model.Common{Type: model.TypeConvention}, Status: status, AppliesIn: cleanList(in.AppliesIn)},
	})
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
var ConventionList = define("convention_list", "List conventions", func(ctx context.Context, d Deps, _ ConventionListInput) (ConventionListOutput, error) {
	refs, err := d.Store.List(ctx, store.Filter{Type: model.TypeConvention})
	if err != nil {
		return ConventionListOutput{}, err
	}
	out := ConventionListOutput{Conventions: []ConventionView{}}
	for _, r := range refs {
		doc, c, err := getConvention(ctx, d, r.ID)
		if err != nil {
			return ConventionListOutput{}, err
		}
		out.Conventions = append(out.Conventions, conventionView(doc, c))
	}
	return out, nil
})

// ConventionShowInput is the input of convention_show.
type ConventionShowInput struct {
	ID string `json:"id"`
}

// ConventionShowOutput is the output of convention_show.
type ConventionShowOutput struct {
	ConventionView `json:",inline"`
	Body           string `json:"body"`
	Rev            string `json:"rev"`
}

// ConventionShow shows one convention.
var ConventionShow = define("convention_show", "Show one convention", func(ctx context.Context, d Deps, in ConventionShowInput) (ConventionShowOutput, error) {
	doc, c, err := getConvention(ctx, d, in.ID)
	if err != nil {
		return ConventionShowOutput{}, err
	}
	return ConventionShowOutput{ConventionView: conventionView(doc, c), Body: doc.Body, Rev: doc.Rev}, nil
})
