package ops

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/iodesystems/para/internal/model"
)

// An op item's input is the input of that op's own tool. The three receipt
// fields an agent may add beside any tool input, why, sources, and
// not_duplicate, are also accepted inside it, with the same meaning, unless the
// op has a field of that name itself.

// opMetaFields are the receipt fields accepted in an op item's input.
var opMetaFields = []string{"why", "sources", "not_duplicate"}

// opItemMeta is what an op item's input says about its receipt.
type opItemMeta struct {
	Why          string
	Sources      []string
	NotDuplicate string
}

// inputFieldNames lists the JSON names of an op's input fields.
func inputFieldNames(t reflect.Type) map[string]bool {
	out := map[string]bool{}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return out
	}
	for i := range t.NumField() {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if f.Anonymous && name == "" {
			for k := range inputFieldNames(f.Type) {
				out[k] = true
			}
			continue
		}
		if name == "-" || !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		out[name] = true
	}
	return out
}

// splitOpInput separates the receipt fields from an op item's input. It returns
// the op's own input, and what the item said about the receipt.
func splitOpInput(op string, input map[string]any) (map[string]any, opItemMeta, error) {
	var m opItemMeta
	e, ok := registry[op]
	if !ok || input == nil {
		return input, m, nil
	}
	own := inputFieldNames(e.InputType())
	clean := make(map[string]any, len(input))
	for k, v := range input {
		clean[k] = v
	}
	for _, name := range opMetaFields {
		v, has := input[name]
		if !has || own[name] {
			continue
		}
		delete(clean, name)
		switch name {
		case "why":
			s, ok := v.(string)
			if !ok {
				return nil, m, errors.New("why: want a string")
			}
			m.Why = strings.TrimSpace(s)
		case "not_duplicate":
			s, ok := v.(string)
			if !ok {
				return nil, m, errors.New("not_duplicate: want a string")
			}
			m.NotDuplicate = strings.TrimSpace(s)
		case "sources":
			list, ok := v.([]any)
			if !ok {
				return nil, m, errors.New("sources: want a list of strings")
			}
			for _, x := range list {
				s, ok := x.(string)
				if !ok {
					return nil, m, errors.New("sources: want a list of strings")
				}
				m.Sources = append(m.Sources, s)
			}
		}
	}
	return clean, m, nil
}

// cleanInput is the op's own input of an item.
func cleanInput(op string, input map[string]any) map[string]any {
	clean, _, err := splitOpInput(op, input)
	if err != nil {
		return input
	}
	return clean
}

// unknownInputFields names every field of input the op does not take, sorted.
func unknownInputFields(op string, input map[string]any) []string {
	e, ok := registry[op]
	if !ok {
		return nil
	}
	own := inputFieldNames(e.InputType())
	var out []string
	for k := range input {
		if !own[k] {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// linkInputFields are the input fields that name existing documents, in the
// ops an agent proposes. id names one in an op that does not create.
var linkInputFields = map[string]bool{
	"project": true, "area": true, "parent": true, "incident": true, "depends_on": true,
	"blocks": true, "question": true, "task": true, "concepts": true, "ids": true, "team": true,
}

// checkItemLinks checks that each link-valued input of each op item names a
// document that exists, or an id an earlier item creates with its own id input.
// It lists every problem. A value with a placeholder is filled when the item runs.
func checkItemLinks(ctx context.Context, d Deps, items []model.ProposalItem) error {
	created := map[string]string{}
	var problems []string
	for i, it := range items {
		if it.Kind != model.ItemPara || it.Op == "" {
			continue
		}
		creates := strings.HasSuffix(it.Op, "_add")
		for field, v := range cleanInput(it.Op, it.Input) {
			if !linkInputFields[field] && (field != "id" || creates) {
				continue
			}
			for _, raw := range stringValues(v) {
				id := normID(raw)
				if id == "" || strings.Contains(raw, "{{") {
					continue
				}
				if from, ok := created[id]; ok && from != "" {
					continue
				}
				if _, err := d.Store.Get(ctx, id); err != nil {
					if !errors.Is(err, model.ErrNotFound) {
						return err
					}
					problems = append(problems, fmt.Sprintf("item %d: %s %q is not a document in this vault and no earlier item creates it (create it with an id on an earlier item)", i+1, field, id))
				}
			}
		}
		if id, _ := it.Input["id"].(string); creates && normID(id) != "" {
			created[normID(id)] = it.ID
		}
	}
	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return errors.New(strings.Join(problems, "; "))
}

func stringValues(v any) []string {
	switch x := v.(type) {
	case string:
		return []string{x}
	case []any:
		var out []string
		for _, e := range x {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
