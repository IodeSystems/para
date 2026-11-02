package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/store"
)

// Areas and projects point at material that lives elsewhere: a repo, a plan,
// a runbook. Para stores the pointer and never reads or copies the target.

func nonNilPointers(in []model.Pointer) []model.Pointer {
	if in == nil {
		return []model.Pointer{}
	}
	return in
}

// LinkAddInput is the input of the *_link_add ops.
type LinkAddInput struct {
	ID     string            `json:"id"`
	Label  string            `json:"label"`
	Kind   model.PointerKind `json:"kind"`
	Target string            `json:"target"` // URL or path; ~/ is stored as written
}

// LinkRemoveInput is the input of area_link_remove and project_link_remove.
type LinkRemoveInput struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// LinkOutput is the output of the link ops: the links after the change.
type LinkOutput struct {
	ID    string          `json:"id"`
	Links []model.Pointer `json:"links"`
	Rev   string          `json:"rev"`
}

// checkTarget requires a URL with a scheme, or a path starting with /, ~/,
// ./ or ../. Nothing is fetched or opened.
func checkTarget(target string) (string, error) {
	t := strings.TrimSpace(target)
	if t == "" || strings.ContainsAny(t, " \t\r\n") {
		return "", fmt.Errorf("target %q: want a URL or a path with no spaces", target)
	}
	if i := strings.Index(t, "://"); i > 0 {
		return t, nil
	}
	for _, p := range []string{"/", "~/", "./", "../"} {
		if strings.HasPrefix(t, p) {
			return t, nil
		}
	}
	return "", fmt.Errorf("target %q: want a URL (scheme://...) or a path starting with /, ~/, ./ or ../", target)
}

// pointers returns the link list of an area or a project.
func pointers(doc model.Document) (*[]model.Pointer, error) {
	switch m := doc.Meta.(type) {
	case *model.Area:
		return &m.Links, nil
	case *model.Project:
		return &m.Links, nil
	case *model.Incident:
		return &m.Links, nil
	}
	return nil, fmt.Errorf("%s is a %s: links belong to an area, a project, or an incident", doc.ID, doc.Meta.Header().Type)
}

func linkAdd(kind model.Type) Func[LinkAddInput, LinkOutput] {
	return func(ctx context.Context, d Deps, in LinkAddInput) (LinkOutput, error) {
		label := strings.Join(strings.Fields(in.Label), " ")
		if label == "" {
			return LinkOutput{}, errors.New("label is empty")
		}
		if !slices.Contains(model.PointerKinds, in.Kind) {
			return LinkOutput{}, fmt.Errorf("kind %q must be one of %v", in.Kind, model.PointerKinds)
		}
		target, err := checkTarget(in.Target)
		if err != nil {
			return LinkOutput{}, err
		}
		return editLinks(ctx, d, kind, in.ID, func(links *[]model.Pointer) error {
			for _, l := range *links {
				if strings.EqualFold(l.Label, label) {
					return fmt.Errorf("link %q already exists: remove it first", l.Label)
				}
			}
			*links = append(*links, model.Pointer{Label: label, Kind: in.Kind, Target: target})
			return nil
		})
	}
}

func linkRemove(kind model.Type) Func[LinkRemoveInput, LinkOutput] {
	return func(ctx context.Context, d Deps, in LinkRemoveInput) (LinkOutput, error) {
		label := strings.Join(strings.Fields(in.Label), " ")
		return editLinks(ctx, d, kind, in.ID, func(links *[]model.Pointer) error {
			n := len(*links)
			*links = slices.DeleteFunc(*links, func(l model.Pointer) bool { return strings.EqualFold(l.Label, label) })
			if len(*links) == n {
				return fmt.Errorf("no link labelled %q", label)
			}
			return nil
		})
	}
}

func editLinks(ctx context.Context, d Deps, kind model.Type, id string, fn func(*[]model.Pointer) error) (LinkOutput, error) {
	doc, err := d.Store.Get(ctx, normID(id))
	if err != nil {
		return LinkOutput{}, err
	}
	if got := doc.Meta.Header().Type; got != kind {
		return LinkOutput{}, fmt.Errorf("%s is a %s, not a %s", doc.ID, got, kind)
	}
	links, err := pointers(doc)
	if err != nil {
		return LinkOutput{}, err
	}
	if err := fn(links); err != nil {
		return LinkOutput{}, err
	}
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return LinkOutput{}, err
	}
	return LinkOutput{ID: doc.ID, Links: nonNilPointers(*links), Rev: saved.Rev}, nil
}

// The link ops. Each kind shares one implementation.
var (
	AreaLinkAdd       = define("area_link_add", "Point an area at a repo, plan, docs, or runbook that lives elsewhere", linkAdd(model.TypeArea))
	AreaLinkRemove    = define("area_link_remove", "Remove a link from an area by label", linkRemove(model.TypeArea))
	ProjectLinkAdd    = define("project_link_add", "Point a project at a repo, plan, docs, or runbook that lives elsewhere", linkAdd(model.TypeProject))
	ProjectLinkRemove = define("project_link_remove", "Remove a link from a project by label", linkRemove(model.TypeProject))
)

// WhereInput is the input of where.
type WhereInput struct {
	Query string `json:"query" query:"query" required:"true"` // words matched against id, title, and link labels
	All   bool   `json:"all,omitempty" query:"all"`           // include archived projects
}

// WhereMatch is an area or project that matches, with its links.
type WhereMatch struct {
	ID     string          `json:"id"`
	Type   model.Type      `json:"type"`
	Folder model.Folder    `json:"folder"`
	Title  string          `json:"title"`
	Links  []model.Pointer `json:"links"`
	// Incidents are the open and mitigated incidents of a matching area.
	Incidents []IncidentView `json:"incidents"`
}

// WhereOutput is the output of where.
type WhereOutput struct {
	Matches []WhereMatch `json:"matches"`
}

// Where finds areas and projects by name or link label and returns their
// links, so "where does the plan for X live?" has one answer. An area also
// lists its open incidents. Every word of
// the query must appear in the id, title, or a link label (case-insensitive).
// It reads no target.
var Where = define("where", "Find areas and projects by name or link label and show their links", func(ctx context.Context, d Deps, in WhereInput) (WhereOutput, error) {
	words := strings.Fields(strings.ToLower(in.Query))
	if len(words) == 0 {
		return WhereOutput{}, errors.New("query is empty")
	}
	out := WhereOutput{Matches: []WhereMatch{}}
	for _, typ := range []model.Type{model.TypeArea, model.TypeProject} {
		refs, err := d.Store.List(ctx, store.Filter{Type: typ})
		if err != nil {
			return WhereOutput{}, err
		}
		for _, r := range refs {
			if r.Folder == model.FolderArchive && !in.All {
				continue
			}
			doc, err := d.Store.Get(ctx, r.ID)
			if err != nil {
				return WhereOutput{}, err
			}
			links, err := pointers(doc)
			if err != nil {
				return WhereOutput{}, err
			}
			hay := strings.ToLower(doc.ID + " " + doc.Title)
			for _, l := range *links {
				hay += " " + strings.ToLower(l.Label)
			}
			if !slices.ContainsFunc(words, func(w string) bool { return !strings.Contains(hay, w) }) {
				m := WhereMatch{ID: doc.ID, Type: typ, Folder: doc.Folder, Title: doc.Title, Links: nonNilPointers(*links), Incidents: []IncidentView{}}
				if typ == model.TypeArea {
					if m.Incidents, err = areaIncidents(ctx, d.Store, doc.ID, false); err != nil {
						return WhereOutput{}, err
					}
				}
				out.Matches = append(out.Matches, m)
			}
		}
	}
	return out, nil
})
