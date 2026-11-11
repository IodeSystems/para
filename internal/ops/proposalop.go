package ops

import (
	"errors"
	"reflect"
	"strings"
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
