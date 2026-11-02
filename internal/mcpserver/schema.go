package mcpserver

import (
	"encoding"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/iodesystems/parasolve/internal/model"
)

// enums lists the allowed values of each named string type. A type that
// implements Enumer is found without an entry here. TestInputEnums fails when
// an op Input uses a string type from package model that has neither.
var enums = map[reflect.Type][]string{
	reflect.TypeFor[model.Type]():             strs(model.Types),
	reflect.TypeFor[model.Folder]():           strs(model.Folders),
	reflect.TypeFor[model.ProjectStatus]():    strs(model.ProjectStatuses),
	reflect.TypeFor[model.TaskStatus]():       strs(model.TaskStatuses),
	reflect.TypeFor[model.RFXDirection]():     strs(model.RFXDirections),
	reflect.TypeFor[model.RFXStatus]():        strs(model.RFXStatuses),
	reflect.TypeFor[model.Fidelity]():         strs(model.Fidelities),
	reflect.TypeFor[model.ExceptionKind]():    strs(model.ExceptionKinds),
	reflect.TypeFor[model.ExceptionStatus]():  strs(model.ExceptionStatuses),
	reflect.TypeFor[model.ConventionStatus](): strs(model.ConventionStatuses),
	reflect.TypeFor[model.JournalScope]():     strs(model.JournalScopes),
}

// plainStrings are named string types in package model that are free text.
var plainStrings = map[reflect.Type]bool{
	reflect.TypeFor[model.Date](): true,
	reflect.TypeFor[model.Link](): true,
}

// Enumer is implemented by a string type that lists its own allowed values.
// The method is called on the zero value.
type Enumer interface {
	Enum() []string
}

func strs[T ~string](in []T) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = string(v)
	}
	return out
}

type schema map[string]any

// enumOf returns the allowed values of t, or nil.
func enumOf(t reflect.Type) []string {
	if e, ok := enums[t]; ok {
		return e
	}
	if t.Kind() == reflect.String {
		if e, ok := reflect.Zero(t).Interface().(Enumer); ok {
			return e.Enum()
		}
	}
	return nil
}

// inputSchema builds the JSON Schema of an op Input type. Fields without
// omitempty are required. An unknown field is rejected, as Invoke does.
func inputSchema(t reflect.Type) (json.RawMessage, error) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("mcpserver: schema: input %s is not a struct", t)
	}
	s, err := typeSchema(t, 0)
	if err != nil {
		return nil, err
	}
	return json.Marshal(s)
}

const maxDepth = 12

var (
	timeType      = reflect.TypeFor[time.Time]()
	rawType       = reflect.TypeFor[json.RawMessage]()
	textMarshaler = reflect.TypeFor[encoding.TextMarshaler]()
)

func typeSchema(t reflect.Type, depth int) (schema, error) {
	if depth > maxDepth {
		return nil, fmt.Errorf("mcpserver: schema: %s nests too deep or is recursive", t)
	}
	if t == timeType {
		return schema{"type": "string", "format": "date-time"}, nil
	}
	if t == rawType {
		return schema{}, nil
	}
	if t.Kind() != reflect.Pointer && t.Implements(textMarshaler) {
		return schema{"type": "string"}, nil
	}
	switch t.Kind() {
	case reflect.Pointer:
		return typeSchema(t.Elem(), depth)
	case reflect.String:
		s := schema{"type": "string"}
		if e := enumOf(t); e != nil {
			s["enum"] = e
		}
		return s, nil
	case reflect.Bool:
		return schema{"type": "boolean"}, nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return schema{"type": "integer"}, nil
	case reflect.Float32, reflect.Float64:
		return schema{"type": "number"}, nil
	case reflect.Interface:
		return schema{}, nil
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 && t.Kind() == reflect.Slice {
			return schema{"type": "string", "contentEncoding": "base64"}, nil
		}
		items, err := typeSchema(t.Elem(), depth+1)
		if err != nil {
			return nil, err
		}
		return schema{"type": "array", "items": items}, nil
	case reflect.Map:
		if t.Key().Kind() != reflect.String {
			return nil, fmt.Errorf("mcpserver: schema: map key of %s is not a string", t)
		}
		v, err := typeSchema(t.Elem(), depth+1)
		if err != nil {
			return nil, err
		}
		return schema{"type": "object", "additionalProperties": v}, nil
	case reflect.Struct:
		return structSchema(t, depth)
	}
	return nil, fmt.Errorf("mcpserver: schema: unsupported type %s", t)
}

func structSchema(t reflect.Type, depth int) (schema, error) {
	props := map[string]any{}
	required := []string{}
	if err := addFields(t, depth, props, &required); err != nil {
		return nil, err
	}
	s := schema{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		s["required"] = required
	}
	return s, nil
}

func addFields(t reflect.Type, depth int, props map[string]any, required *[]string) error {
	for i := range t.NumField() {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if f.Anonymous && name == "" {
			ft := f.Type
			for ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				if err := addFields(ft, depth, props, required); err != nil {
					return err
				}
				continue
			}
		}
		if !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		fs, err := typeSchema(f.Type, depth+1)
		if err != nil {
			return fmt.Errorf("%s.%s: %w", t.Name(), f.Name, err)
		}
		props[name] = fs
		if !strings.Contains(opts, "omitempty") && !strings.Contains(opts, "omitzero") {
			*required = append(*required, name)
		}
	}
	return nil
}
