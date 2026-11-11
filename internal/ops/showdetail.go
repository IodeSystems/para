package ops

import (
	"cmp"
	"reflect"
	"slices"
)

// ShowDetail is the detail of show: the Output of the item's type's show op
// as JSON, with "op" set to that op's name. A list the op leaves nil is [].
// GraphQL reads it as a union of the show Outputs, by op (server/schema.go).
type ShowDetail map[string]any

// showDetail is the ShowDetail of res, the Output of op.
func showDetail(op string, res any) ShowDetail {
	if res == nil {
		return nil
	}
	v := reflect.New(reflect.TypeOf(res))
	v.Elem().Set(reflect.ValueOf(res))
	emptyLists(v.Elem())
	m := asMap(v.Interface())
	if m == nil {
		return nil
	}
	m["op"] = op
	return m
}

// emptyLists sets each nil slice v reaches through structs and pointers to an
// empty one, so it is [] in JSON.
func emptyLists(v reflect.Value) {
	switch v.Kind() {
	case reflect.Pointer:
		if !v.IsNil() {
			emptyLists(v.Elem())
		}
	case reflect.Struct:
		for i := range v.NumField() {
			if f := v.Field(i); f.CanSet() {
				emptyLists(f)
			}
		}
	case reflect.Slice:
		if v.IsNil() {
			if v.CanSet() {
				v.Set(reflect.MakeSlice(v.Type(), 0, 0))
			}
			return
		}
		for i := range v.Len() {
			emptyLists(v.Index(i))
		}
	}
}

// ShowOps lists the show op of each type, by name, once each: what show's
// detail can be the Output of.
func ShowOps() []Entry {
	var out []Entry
	for _, e := range showOps() {
		if !slices.ContainsFunc(out, func(x Entry) bool { return x.Name() == e.Name() }) {
			out = append(out, e)
		}
	}
	slices.SortFunc(out, func(a, b Entry) int { return cmp.Compare(a.Name(), b.Name()) })
	return out
}
