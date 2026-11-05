package mcpserver

import (
	"context"
	"encoding"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
)

// enums lists the allowed values of each named string type (ops.Enums). A
// type that implements Enumer is found without an entry. TestInputEnums fails
// when an op Input uses a string type from package model that has neither.
var enums = ops.Enums

// plainStrings are named string types in package model that are free text.
var plainStrings = map[reflect.Type]bool{
	reflect.TypeFor[model.Date](): true,
	reflect.TypeFor[model.Link](): true,
}

// Enumer is implemented by a string type that lists its own allowed values.
type Enumer = ops.Enumer

func strs[T ~string](in []T) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = string(v)
	}
	return out
}

type schema map[string]any

// enumOf returns the allowed values of t, or nil.
func enumOf(t reflect.Type) []string { return ops.EnumOf(t) }

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

// metaProps are the fields a write tool takes beside the op's own input.
var metaProps = map[string]schema{
	"why":           {"type": "string", "description": "Why you make this write. Required. Stored in the receipt"},
	"sources":       {"type": "array", "items": schema{"type": "string"}, "description": "URLs or document ids this write rests on"},
	"verify":        {"type": "string", "description": "How the owner can check it: a link, or a check they can do"},
	"confidence":    {"type": "string", "enum": strs(model.Confidences), "description": "low, medium (default), or high. Say low when you doubt the write; it is stored in the receipt"},
	"request":       {"type": "string", "description": "The agent work this write serves, if you hold more than one"},
	"not_duplicate": {"type": "string", "description": "On a create refused for a likely match: why this one is different. Stored in the receipt"},
	"minor":         {"type": "boolean", "description": "Rate the write minor: a small change. With obvious (and undoable, which para knows), it applies directly; otherwise it goes to the owner's review stack and does not run yet"},
	"obvious":       {"type": "boolean", "description": "Rate the write obvious: the owner would make it without asking. Never with confidence low"},
	"dossier":       {"type": "string", "description": "The id of your dossier a held-back write goes to; default your Staged writes dossier"},
	"owner_said":    {"type": "string", "description": "The owner's words in this session that asked for this write. An undoable safe write with them applies without minor and obvious; some writes (defer, task_resolve failed or rerouted) are held for review without them. Stored in the receipt as self-reported"},
}

// toolSchema is the input schema of a tool: the op's Input and, on a write, the
// fields of a receipt. A field the op has itself keeps the op's meaning, and its
// value also serves the receipt. why is required.
func toolSchema(e ops.Entry) (json.RawMessage, error) {
	raw, err := inputSchema(e.InputType())
	if err != nil || !ops.NeedsWhy(e) {
		return raw, err
	}
	var s schema
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	props, _ := s["properties"].(map[string]any)
	if props == nil {
		props = map[string]any{}
		s["properties"] = props
	}
	for name, p := range metaProps {
		if _, own := props[name]; !own {
			props[name] = p
		}
	}
	req, _ := s["required"].([]any)
	if !slices.Contains(req, any("why")) {
		req = append(req, "why")
	}
	s["required"] = req
	return json.Marshal(s)
}

// takeWriteMeta moves the receipt fields out of a tool call and onto the
// context. It refuses a write with no why.
func takeWriteMeta(ctx context.Context, e ops.Entry, input json.RawMessage) (context.Context, json.RawMessage, error) {
	args := map[string]any{}
	if len(input) > 0 {
		if err := json.Unmarshal(input, &args); err != nil {
			return ctx, input, errors.New("input: not an object")
		}
	}
	own := fieldNames(e.InputType())
	take := func(name string) any {
		v := args[name]
		if !own[name] {
			delete(args, name)
		}
		return v
	}
	var m ops.WriteMeta
	m.Why, _ = take("why").(string)
	if strings.TrimSpace(m.Why) == "" {
		return ctx, input, errors.New("why is required: say why you make this write")
	}
	if list, ok := take("sources").([]any); ok {
		for _, v := range list {
			if s, ok := v.(string); ok {
				m.Sources = append(m.Sources, s)
			}
		}
	}
	m.Verify, _ = take("verify").(string)
	c, _ := take("confidence").(string)
	m.Confidence = model.Confidence(c)
	if c != "" && !slices.Contains(model.Confidences, m.Confidence) {
		return ctx, input, fmt.Errorf("confidence %q: want low, medium, or high", c)
	}
	m.Request, _ = take("request").(string)
	m.NotDuplicate, _ = take("not_duplicate").(string)
	m.Minor, _ = take("minor").(bool)
	m.Obvious, _ = take("obvious").(bool)
	m.Dossier, _ = take("dossier").(string)
	m.OwnerSaid, _ = take("owner_said").(string)
	b, err := json.Marshal(args)
	if err != nil {
		return ctx, input, errors.New("input: not JSON")
	}
	return ops.WithWrite(ctx, m), b, nil
}

// fieldNames lists the JSON names of a struct's fields.
func fieldNames(t reflect.Type) map[string]bool {
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
			for k := range fieldNames(f.Type) {
				out[k] = true
			}
			continue
		}
		if name != "" && name != "-" {
			out[name] = true
		}
	}
	return out
}
