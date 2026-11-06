package ops

import (
	"cmp"
	"context"
	"encoding/json"
	"slices"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
	"github.com/iodesystems/para/internal/upgrade"
)

// The small types. Upgrade step small-types (vault format 10) made a team, a
// role, and a domain groups of that kind, an interaction a closed io item (a
// logged contact), and an incident a task of kind incident. At that format
// the older add ops write the new forms; below it they write the older
// types, so a para not upgraded on another machine still reads the vault.
// Every reader of the older types reads both forms (a fold never makes data
// vanish from a read).

// smallTypesOn reports whether the vault is at the small types format.
func smallTypesOn(d Deps) bool {
	v, err := upgrade.Current(d.Store)
	return err == nil && v >= upgrade.SmallTypesFormat
}

// smallShape is a new document as the vault stores it: a team, a role, a
// domain, an interaction, or an incident in its new form at the small types
// format, and as it is below it.
func smallShape(d Deps, doc model.Document) (model.Document, error) {
	if !smallTypesOn(d) || !upgrade.FoldsToSmallType(doc) {
		return doc, nil
	}
	return upgrade.AsSmallType(doc)
}

// asOlderType reports whether doc is of the older small type t in either
// form: the file of type t, or the form step small-types made of it.
func asOlderType(doc model.Document, t model.Type) bool {
	switch t {
	case model.TypeTeam, model.TypeRole, model.TypeDomain:
		return model.OrgKind(doc) == t
	case model.TypeInteraction:
		_, ok := model.InteractionOf(doc)
		return ok
	case model.TypeIncident:
		return model.IsIncident(doc)
	}
	return doc.Meta.Header().Type == t
}

// newFormType is the type that holds the new form of an older small type, or "".
func newFormType(t model.Type) model.Type {
	switch t {
	case model.TypeTeam, model.TypeRole, model.TypeDomain:
		return model.TypeGroup
	case model.TypeInteraction:
		return model.TypeIO
	case model.TypeIncident:
		return model.TypeTask
	}
	return ""
}

// refsOf lists the documents of type t in folder (empty: any) and, for an
// older small type, those of its new form, with Type t, sorted by id.
func refsOf(ctx context.Context, s store.Store, f store.Filter) ([]model.Ref, error) {
	refs, err := s.List(ctx, f)
	if err != nil {
		return nil, err
	}
	into := newFormType(f.Type)
	if into == "" {
		return refs, nil
	}
	more, err := s.List(ctx, store.Filter{Folder: f.Folder, Type: into, All: f.All})
	if err != nil {
		return nil, err
	}
	for _, r := range more {
		doc, err := s.Get(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		if asOlderType(doc, f.Type) {
			r.Type = f.Type
			refs = append(refs, r)
		}
	}
	slices.SortStableFunc(refs, func(a, b model.Ref) int { return cmp.Compare(a.ID, b.ID) })
	return refs, nil
}

// The ops that the older add ops are aliases of route to themselves
// (opalias.go).
var smallTypesRouted = func() bool {
	for _, name := range []string{"org_group_add", "io_add", "task_add"} {
		verbRoutes[name] = func(_ context.Context, _ Deps, raw json.RawMessage) (verbRoute, error) {
			return verbRoute{op: registry[name], input: raw}, nil
		}
	}
	return true
}()
