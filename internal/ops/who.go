package ops

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/iodesystems/paradize/internal/model"
)

// WhoInput is the input of who.
type WhoInput struct {
	Query  string `json:"query"`            // a name, alias, or contact value, any case
	Recent int    `json:"recent,omitempty"` // interactions to list; default 5
}

// WhoPerson is what para knows about one person.
type WhoPerson struct {
	ID           string               `json:"id"`
	Name         string               `json:"name"`
	Org          string               `json:"org"`
	Aliases      []string             `json:"aliases"`
	Role         *model.Ref           `json:"role"`
	Team         *model.Ref           `json:"team"`
	Groups       []model.Ref          `json:"groups"`
	Contacts     []model.Contact      `json:"contacts"` // preferred ones carry preferred: true
	Notes        []string             `json:"notes"`    // lines under "## Notes"
	Interactions []InteractionSummary `json:"interactions"`
	OpenRFX      []WhoRFX             `json:"open_rfx"`
}

// WhoRFX is an open RFX that targets the person.
type WhoRFX struct {
	ID        string             `json:"id"`
	Title     string             `json:"title"`
	Direction model.RFXDirection `json:"direction"`
	Targets   []model.Target     `json:"targets"` // only the targets for this person
}

// WhoOutput is the output of who. Person is set when the text names one
// person. When it names several, Candidates lists them and Person is nil.
type WhoOutput struct {
	Query      string      `json:"query"`
	Person     *WhoPerson  `json:"person"`
	Candidates []model.Ref `json:"candidates"`
}

const recentDefault = 5

// Who finds a person by name, alias, or contact value and shows what is known.
var Who = define("who", "Find a person by name, alias, or contact value", who)

func who(ctx context.Context, d Deps, in WhoInput) (WhoOutput, error) {
	q := strings.Join(strings.Fields(in.Query), " ")
	if q == "" {
		return WhoOutput{}, errors.New("query is empty")
	}
	if in.Recent < 0 {
		return WhoOutput{}, errors.New("recent is negative")
	}
	out := WhoOutput{Query: q, Candidates: []model.Ref{}}
	lq := strings.ToLower(q)
	hits, err := DefaultFinder.Find(ctx, d.Store, Query{Types: []model.Type{model.TypePerson}, Terms: []string{lq}})
	if err != nil {
		return WhoOutput{}, err
	}
	// The Finder also matches note text. Keep a hit only when the text is a
	// name, alias, or contact value: an exact one wins over a partial one.
	var exact, partial []model.Ref
	for _, h := range hits {
		doc, p, err := getPerson(ctx, d, h.ID)
		if err != nil {
			return WhoOutput{}, err
		}
		names := append([]string{doc.Title}, p.Aliases...)
		for _, c := range p.Contacts {
			names = append(names, c.Value)
		}
		switch {
		case strings.EqualFold(doc.ID, q) || slices.ContainsFunc(names, func(n string) bool { return strings.EqualFold(n, q) }):
			exact = append(exact, h)
		case slices.ContainsFunc(names, func(n string) bool { return strings.Contains(strings.ToLower(n), lq) }):
			partial = append(partial, h)
		}
	}
	found := exact
	if len(found) == 0 {
		found = partial
	}
	switch len(found) {
	case 0:
		return out, nil
	case 1:
		wp, err := describe(ctx, d, found[0].ID, in.Recent)
		if err != nil {
			return WhoOutput{}, err
		}
		out.Person = wp
	default:
		out.Candidates = found
	}
	return out, nil
}

func describe(ctx context.Context, d Deps, id string, recent int) (*WhoPerson, error) {
	if recent == 0 {
		recent = recentDefault
	}
	doc, p, err := getPerson(ctx, d, id)
	if err != nil {
		return nil, err
	}
	wp := &WhoPerson{
		ID: doc.ID, Name: doc.Title, Org: p.Org,
		Aliases:  append([]string{}, p.Aliases...),
		Groups:   []model.Ref{},
		Contacts: append([]model.Contact{}, p.Contacts...),
		Notes:    sectionLines(doc.Body, notesHeading),
	}
	if wp.Role, err = refOrNil(ctx, d, p.Role); err != nil {
		return nil, err
	}
	if wp.Team, err = refOrNil(ctx, d, p.Team); err != nil {
		return nil, err
	}
	for _, g := range p.Groups {
		r, err := refOrNil(ctx, d, g)
		if err != nil {
			return nil, err
		}
		if r != nil {
			wp.Groups = append(wp.Groups, *r)
		}
	}
	all, err := loadInteractions(ctx, d)
	if err != nil {
		return nil, err
	}
	wp.Interactions = []InteractionSummary{}
	for _, s := range all {
		if len(wp.Interactions) < recent && slices.Contains(s.With, doc.ID) {
			wp.Interactions = append(wp.Interactions, s)
		}
	}
	rfx, err := loadRFX(ctx, d)
	if err != nil {
		return nil, err
	}
	wp.OpenRFX = []WhoRFX{}
	for _, s := range rfx {
		if s.Status != model.RFXOpen {
			continue
		}
		var mine []model.Target
		for _, t := range s.Targets {
			if t.To.Target() == doc.ID {
				mine = append(mine, t)
			}
		}
		if len(mine) > 0 {
			wp.OpenRFX = append(wp.OpenRFX, WhoRFX{ID: s.ID, Title: s.Title, Direction: s.Direction, Targets: mine})
		}
	}
	return wp, nil
}

// refOrNil returns the ref a link names, nil for an empty link, and a ref with
// only an id for a link to a missing document.
func refOrNil(ctx context.Context, d Deps, l model.Link) (*model.Ref, error) {
	if l == "" {
		return nil, nil
	}
	doc, err := d.Store.Get(ctx, l.Target())
	if errors.Is(err, model.ErrNotFound) {
		return &model.Ref{ID: l.Target()}, nil
	}
	if err != nil {
		return nil, err
	}
	return &model.Ref{ID: doc.ID, Folder: doc.Folder, Type: doc.Meta.Header().Type, Title: doc.Title, Rev: doc.Rev}, nil
}
