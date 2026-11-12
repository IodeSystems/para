package model

import (
	"reflect"

	"gopkg.in/yaml.v3"
)

// Clone returns a deep copy of d: a change to the copy's Meta, its lists, or
// its extra frontmatter does not reach d. The vault's parse cache hands out
// clones of the documents it keeps. Meta types hold only exported fields, so
// the copy reaches every value.
func (d Document) Clone() Document {
	c := d
	if d.Meta != nil {
		c.Meta = deepCopy(reflect.ValueOf(d.Meta)).Interface().(Meta)
	}
	if d.extra != nil {
		c.extra = make(map[string]yaml.Node, len(d.extra))
		for k, n := range d.extra {
			c.extra[k] = *cloneNode(&n, map[*yaml.Node]*yaml.Node{})
		}
	}
	return c
}

// deepCopy copies v and everything it points to. A struct's unexported
// fields (time.Time's) are copied by value.
func deepCopy(v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return v
		}
		c := reflect.New(v.Type().Elem())
		c.Elem().Set(deepCopy(v.Elem()))
		return c
	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		c := reflect.New(v.Type()).Elem()
		c.Set(deepCopy(v.Elem()))
		return c
	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		c := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := range v.Len() {
			c.Index(i).Set(deepCopy(v.Index(i)))
		}
		return c
	case reflect.Map:
		if v.IsNil() {
			return v
		}
		c := reflect.MakeMapWithSize(v.Type(), v.Len())
		for it := v.MapRange(); it.Next(); {
			c.SetMapIndex(deepCopy(it.Key()), deepCopy(it.Value()))
		}
		return c
	case reflect.Array:
		c := reflect.New(v.Type()).Elem()
		for i := range v.Len() {
			c.Index(i).Set(deepCopy(v.Index(i)))
		}
		return c
	case reflect.Struct:
		c := reflect.New(v.Type()).Elem()
		c.Set(v)
		for i := range v.NumField() {
			if v.Type().Field(i).IsExported() {
				c.Field(i).Set(deepCopy(v.Field(i)))
			}
		}
		return c
	}
	return v
}

// cloneNode copies the tree under n. seen keeps an alias pointing into the
// copy.
func cloneNode(n *yaml.Node, seen map[*yaml.Node]*yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	if c, ok := seen[n]; ok {
		return c
	}
	c := new(yaml.Node)
	*c = *n
	seen[n] = c
	if n.Content != nil {
		c.Content = make([]*yaml.Node, len(n.Content))
		for i, x := range n.Content {
			c.Content[i] = cloneNode(x, seen)
		}
	}
	c.Alias = cloneNode(n.Alias, seen)
	return c
}
