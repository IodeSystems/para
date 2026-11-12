package ops

import (
	"context"
	"reflect"
	"strings"
	"sync"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// Display names. An output field whose tag is `nameof:"Sibling"` is filled
// after the op runs with the title of the document the sibling names: a person's
// name, a project's title. The sibling is a string or a link, or a slice of them,
// or "Slice.Field" for a field of each struct in a slice. A name field has the
// same shape as its source: a string for a string, a slice of the same length for
// a slice, with "" where nothing is known. The id fields stay as they are, so an
// agent that works in ids is not affected. Names are a convenience for people:
// a failed lookup leaves them empty and never fails the op.
//
// A field of type []PartOfRef tagged `partof:"Sibling"` is filled the same way
// with what the document the sibling names is part of (theme.go): its parent,
// obligations and questions, goal, and tags, nearest first.
//
// A field of type []Blocker tagged `blockersof:"Sibling"` is filled with the
// open items among the ids the sibling holds (blockers.go).

var namesNeeded sync.Map // reflect.Type -> bool

// needsNames reports whether a value of type t holds a nameof field.
func needsNames(t reflect.Type) bool {
	if v, ok := namesNeeded.Load(t); ok {
		return v.(bool)
	}
	r := scanNames(t, map[reflect.Type]bool{})
	namesNeeded.Store(t, r)
	return r
}

func scanNames(t reflect.Type, seen map[reflect.Type]bool) bool {
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Array:
		return scanNames(t.Elem(), seen)
	case reflect.Struct:
		if seen[t] {
			return false
		}
		seen[t] = true
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if _, ok := f.Tag.Lookup("nameof"); ok {
				return true
			}
			if _, ok := f.Tag.Lookup("partof"); ok {
				return true
			}
			if _, ok := f.Tag.Lookup("blockersof"); ok {
				return true
			}
			if f.IsExported() && scanNames(f.Type, seen) {
				return true
			}
		}
	}
	return false
}

// namer resolves ids to titles, reading the vault once and only when asked.
type namer struct {
	ctx    context.Context
	s      store.Store
	titles map[string]string
	parts  *partIndex
	docs   map[string]*model.Document
}

// partOf is what the document id names is part of.
func (n *namer) partOf(id string) []PartOfRef {
	if n.s == nil {
		return []PartOfRef{}
	}
	if n.parts == nil {
		n.parts = newPartIndex(n.ctx, n.s)
	}
	return n.parts.partOfID(id)
}

func (n *namer) load() {
	if n.titles != nil {
		return
	}
	n.titles = map[string]string{}
	if n.s == nil {
		return
	}
	refs, err := n.s.List(n.ctx, store.Filter{All: true})
	if err != nil {
		return
	}
	for _, r := range refs {
		n.titles[r.ID] = r.Title
	}
}

// get reads a document, once per id.
func (n *namer) get(id string) (model.Document, bool) {
	if n.s == nil {
		return model.Document{}, false
	}
	if n.docs == nil {
		n.docs = map[string]*model.Document{}
	}
	if d, ok := n.docs[id]; ok {
		return derefDoc(d)
	}
	d, err := n.s.Get(n.ctx, id)
	if err != nil {
		n.docs[id] = nil
		return model.Document{}, false
	}
	n.docs[id] = &d
	return d, true
}

func derefDoc(d *model.Document) (model.Document, bool) {
	if d == nil {
		return model.Document{}, false
	}
	return *d, true
}

// name is the display name of a link or id: "" when there is none to show.
func (n *namer) name(ref string) string {
	id := model.Link(strings.TrimSpace(ref)).Target()
	switch id {
	case "", "self", "root":
		return ""
	}
	if a, ok := strings.CutPrefix(id, "agent:"); ok {
		return a
	}
	n.load()
	return n.titles[id]
}

// cutID is the one id that starts (prefix) or ends with frag, the part of a
// link a snippet's edge left; "" when none or several do.
func (n *namer) cutID(frag string, prefix bool) string {
	if frag == "" {
		return ""
	}
	n.load()
	found := ""
	for id := range n.titles {
		if (prefix && strings.HasPrefix(id, frag)) || (!prefix && strings.HasSuffix(id, frag)) {
			if found != "" {
				return ""
			}
			found = id
		}
	}
	return found
}

// FillNames fills every nameof and partof field below v, a pointer, for a
// view that is not an op's output (the Today screen).
func FillNames(ctx context.Context, s store.Store, v any) { fillNames(ctx, s, v) }

// fillNames fills every nameof field below v, which is a pointer.
func fillNames(ctx context.Context, s store.Store, v any) {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return
	}
	fillValue(&namer{ctx: ctx, s: s}, rv.Elem())
}

func fillValue(n *namer, v reflect.Value) {
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			fillValue(n, v.Elem())
		}
	case reflect.Slice, reflect.Array:
		if !needsNames(v.Type().Elem()) {
			return
		}
		for i := 0; i < v.Len(); i++ {
			fillValue(n, v.Index(i))
		}
	case reflect.Struct:
		t := v.Type()
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			if spec, ok := f.Tag.Lookup("nameof"); ok {
				fillOne(n, v, v.Field(i), spec)
				continue
			}
			if spec, ok := f.Tag.Lookup("partof"); ok {
				if ids, ok := sourceIDs(v, spec); ok && len(ids) > 0 && v.Field(i).CanSet() {
					v.Field(i).Set(reflect.ValueOf(n.partOf(ids[0])))
				}
				continue
			}
			if spec, ok := f.Tag.Lookup("blockersof"); ok {
				if ids, ok := sourceIDs(v, spec); ok && len(ids) > 0 && v.Field(i).CanSet() {
					v.Field(i).Set(reflect.ValueOf(blockersOf(n.get, ids)))
				}
				continue
			}
			if needsNames(f.Type) {
				fillValue(n, v.Field(i))
			}
		}
	}
}

// sourceIDs reads the strings a nameof spec points at.
func sourceIDs(parent reflect.Value, spec string) ([]string, bool) {
	first, rest, nested := strings.Cut(spec, ".")
	src := parent.FieldByName(first)
	if !src.IsValid() {
		return nil, false
	}
	var out []string
	one := func(x reflect.Value) {
		if x.Kind() == reflect.String {
			out = append(out, x.String())
		}
	}
	switch src.Kind() {
	case reflect.String:
		one(src)
		return out, true
	case reflect.Slice:
		for i := 0; i < src.Len(); i++ {
			el := src.Index(i)
			if nested {
				el = el.FieldByName(rest)
			}
			if el.IsValid() {
				one(el)
			}
		}
		return out, true
	}
	return nil, false
}

func fillOne(n *namer, parent, dst reflect.Value, spec string) {
	ids, ok := sourceIDs(parent, spec)
	if !ok {
		return
	}
	switch dst.Kind() {
	case reflect.String:
		if len(ids) > 0 {
			dst.SetString(n.name(ids[0]))
		}
	case reflect.Slice:
		names := make([]string, len(ids))
		any := false
		for i, id := range ids {
			if names[i] = n.name(id); names[i] != "" {
				any = true
			}
		}
		if any {
			dst.Set(reflect.ValueOf(names))
		}
	}
}
