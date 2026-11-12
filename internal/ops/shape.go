package ops

import (
	"encoding"
	"encoding/json"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
)

// An unknown input field is refused with the input's shape on one line, so an
// agent that guessed a field name sees the right ones in the same answer:
//
//	retry_decide: input: unknown field "decision" (did you mean action?); fields: request*, action*, input, notes, approve
//
// A field marked * is required. An enum lists its values with |. A hint names
// the nearest field; para never accepts the guessed name in its place.

// Enums lists the allowed values of each named string type in package model
// that does not list its own with Enum. The MCP schema uses the same lists.
var Enums = map[reflect.Type][]string{
	reflect.TypeFor[model.Type]():             enumStrs(model.Types),
	reflect.TypeFor[model.Folder]():           enumStrs(model.Folders),
	reflect.TypeFor[model.TaskStatus]():       enumStrs(model.TaskStatuses),
	reflect.TypeFor[model.RFXDirection]():     enumStrs(model.RFXDirections),
	reflect.TypeFor[model.RFXStatus]():        enumStrs(model.RFXStatuses),
	reflect.TypeFor[model.Fidelity]():         enumStrs(model.Fidelities),
	reflect.TypeFor[model.ExceptionKind]():    enumStrs(model.ExceptionKinds),
	reflect.TypeFor[model.ExceptionStatus]():  enumStrs(model.ExceptionStatuses),
	reflect.TypeFor[model.ConventionStatus](): enumStrs(model.ConventionStatuses),
	reflect.TypeFor[model.JournalScope]():     enumStrs(model.JournalScopes),
}

// Enumer is implemented by a string type that lists its own allowed values.
// The method is called on the zero value.
type Enumer interface {
	Enum() []string
}

func enumStrs[T ~string](in []T) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = string(v)
	}
	return out
}

// EnumOf returns the allowed values of t, or nil.
func EnumOf(t reflect.Type) []string {
	if e, ok := Enums[t]; ok {
		return e
	}
	if t.Kind() == reflect.String {
		if e, ok := reflect.Zero(t).Interface().(Enumer); ok {
			return e.Enum()
		}
	}
	return nil
}

// writeShape lists the fields an agent's write takes beside the op's input.
const writeShape = "write: why*, sources[], verify, confidence low|medium|high, request, input, not_duplicate"

// Shape limits: nesting shown, and the length of the whole line.
const (
	shapeDepth = 2
	shapeMax   = 700
)

var (
	shapeTime   = reflect.TypeFor[time.Time]()
	shapeRaw    = reflect.TypeFor[json.RawMessage]()
	shapeTextMa = reflect.TypeFor[encoding.TextMarshaler]()
)

// shapeField is one JSON field of an input type.
type shapeField struct {
	name     string
	required bool
	t        reflect.Type
}

// shapeFields lists the JSON fields of a struct, embedded structs inlined.
func shapeFields(t reflect.Type) []shapeField {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	var out []shapeField
	for i := range t.NumField() {
		f := t.Field(i)
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, opts, _ := strings.Cut(tag, ",")
		if f.Anonymous && name == "" {
			out = append(out, shapeFields(f.Type)...)
			continue
		}
		if !f.IsExported() {
			continue
		}
		if name == "" {
			name = f.Name
		}
		out = append(out, shapeField{name: name, t: f.Type,
			required: !strings.Contains(opts, "omitempty") && !strings.Contains(opts, "omitzero")})
	}
	return out
}

// structOf returns the struct a field holds, through pointers and slices, and
// whether it is a list.
func structOf(t reflect.Type) (reflect.Type, bool) {
	list := false
	for {
		switch t.Kind() {
		case reflect.Pointer:
			t = t.Elem()
		case reflect.Slice, reflect.Array:
			if t.Elem().Kind() == reflect.Uint8 {
				return nil, false
			}
			list, t = true, t.Elem()
		default:
			if t.Kind() == reflect.Struct && t != shapeTime && !t.Implements(shapeTextMa) && !reflect.PointerTo(t).Implements(shapeTextMa) {
				return t, list
			}
			return nil, list
		}
	}
}

func shapeOf(t reflect.Type, depth int) string {
	parts := []string{}
	for _, f := range shapeFields(t) {
		parts = append(parts, fieldShape(f, depth))
	}
	return strings.Join(parts, ", ")
}

func fieldShape(f shapeField, depth int) string {
	var b strings.Builder
	b.WriteString(f.name)
	st, list := structOf(f.t)
	if list {
		b.WriteString("[]")
	}
	if f.required {
		b.WriteString("*")
	}
	ft := f.t
	for ft.Kind() == reflect.Pointer || ft.Kind() == reflect.Slice {
		ft = ft.Elem()
	}
	switch {
	case st != nil && depth+1 < shapeDepth:
		b.WriteString("{" + shapeOf(st, depth+1) + "}")
	case st != nil, ft.Kind() == reflect.Map && ft != shapeRaw:
		b.WriteString("{}")
	default:
		if e := EnumOf(ft); len(e) > 0 {
			b.WriteString(" " + strings.Join(e, "|"))
		}
	}
	return b.String()
}

// InputShape is the one-line shape of an op's input: its fields, and for an
// agent's write the write fields.
func InputShape(e Entry) string {
	s := shapeOf(e.InputType(), 0)
	if NeedsWhy(e) {
		s += "; " + writeShape
	}
	return s
}

// fieldPaths lists every field path of t to the shape depth: id, items[].item.
func fieldPaths(t reflect.Type, prefix string, depth int) []string {
	var out []string
	for _, f := range shapeFields(t) {
		p := prefix + f.name
		out = append(out, p)
		if st, list := structOf(f.t); st != nil && depth+1 < shapeDepth {
			if list {
				p += "[]"
			}
			out = append(out, fieldPaths(st, p+".", depth+1)...)
		}
	}
	return out
}

// fieldSynonyms are names agents guess for a field of another name. A hint
// names the field only when the input has it.
var fieldSynonyms = map[string][]string{
	"kind":     {"type"},
	"type":     {"kind"},
	"decision": {"items[].decision"},
	"query":    {"q", "text"},
	"q":        {"query", "text"},
	"text":     {"body", "query"},
	"body":     {"text"},
	"note":     {"text", "reason"},
	"comment":  {"text", "note"},
	"tag":      {"tags"},
	"tags":     {"tag"},
	"date":     {"until", "due"},
	"until":    {"defer_until"},
	"name":     {"title"},
	"title":    {"name"},
	"item":     {"items"},
	"proposal": {"id"},
	"id":       {"ref"},
	"ref":      {"locator"},
	"url":      {"locator", "ref"},
}

// fieldHint names the field an agent likely meant by name, or "".
func fieldHint(t reflect.Type, name string) string {
	paths := fieldPaths(t, "", 0)
	for _, s := range fieldSynonyms[name] {
		if slices.Contains(paths, s) {
			return s
		}
	}
	// The same name one level down: decision for items[].decision.
	for _, p := range paths {
		if strings.HasSuffix(p, "."+name) {
			return p
		}
	}
	best, bestD := "", 3
	for _, p := range paths {
		leaf := p[strings.LastIndex(p, ".")+1:]
		if d := editDistance(strings.ToLower(name), leaf); d < bestD {
			best, bestD = p, d
		}
	}
	if bestD > len(name)/2 {
		return ""
	}
	return best
}

func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

var unknownFieldRe = regexp.MustCompile(`^json: unknown field "([^"]*)"$`)

// inputError is the error of an input that does not decode. An unknown field
// gets the hint and the shape, on one line.
func inputError(e Entry, input json.RawMessage, err error) error {
	m := unknownFieldRe.FindStringSubmatch(err.Error())
	if m == nil {
		return err
	}
	msg := "unknown field \"" + m[1] + "\""
	// The decoder does not say where the field was: find the object that has
	// it, so a field the input has elsewhere (id in sources[]) is not hinted
	// back to the caller as itself.
	if at, st, ok := unknownAt(e.InputType(), input, m[1], ""); ok && at != "" {
		msg += " in " + strings.TrimSuffix(at, ".")
		if h := fieldHint(st, m[1]); h != "" {
			msg += " (did you mean " + at + h + "?)"
		} else if h := fieldHint(e.InputType(), m[1]); h != "" && h != m[1] {
			msg += " (did you mean " + h + "?)"
		}
	} else if h := fieldHint(e.InputType(), m[1]); h != "" {
		msg += " (did you mean " + h + "?)"
	}
	shape := InputShape(e)
	if len(shape) > shapeMax {
		shape = shape[:shapeMax] + "..."
	}
	return &shapeError{msg: msg + "; fields: " + shape, err: err}
}

// unknownAt finds the object in raw, of struct type t, that has the field
// name t lacks: its path (sources[]. or "" at the top) and its type.
func unknownAt(t reflect.Type, raw json.RawMessage, name, prefix string) (string, reflect.Type, bool) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	var obj map[string]json.RawMessage
	if t.Kind() != reflect.Struct || json.Unmarshal(raw, &obj) != nil {
		return "", nil, false
	}
	fields := shapeFields(t)
	if _, has := obj[name]; has && !slices.ContainsFunc(fields, func(f shapeField) bool { return f.name == name }) {
		return prefix, t, true
	}
	for _, f := range fields {
		v, ok := obj[f.name]
		st, list := structOf(f.t)
		if !ok || st == nil {
			continue
		}
		if !list {
			if at, ft, ok := unknownAt(st, v, name, prefix+f.name+"."); ok {
				return at, ft, true
			}
			continue
		}
		var items []json.RawMessage
		if json.Unmarshal(v, &items) != nil {
			continue
		}
		for _, it := range items {
			if at, ft, ok := unknownAt(st, it, name, prefix+f.name+"[]."); ok {
				return at, ft, true
			}
		}
	}
	return "", nil, false
}

// shapeError keeps the decode error for errors.Is and As.
type shapeError struct {
	msg string
	err error
}

func (e *shapeError) Error() string { return e.msg }
func (e *shapeError) Unwrap() error { return e.err }
