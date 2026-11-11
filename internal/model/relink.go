package model

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// RecordTypes are the types whose text is a record of what happened: an id
// change leaves their references as written, and a former id resolves them.
var RecordTypes = []Type{TypeReceipt, TypeProposal, TypeTranscript}

// IsRecord reports whether t is a record type.
func IsRecord(t Type) bool {
	for _, r := range RecordTypes {
		if r == t {
			return true
		}
	}
	return false
}

// noRelinkKeys are frontmatter keys an id change never rewrites: former_ids
// holds the old ids on purpose.
var noRelinkKeys = map[string]bool{"former_ids": true, "type": true}

// LinkKeyFor returns the key a top-level link of key key belongs under when it
// names a document of type target, and whether that differs from key. A
// project link that names a goal belongs under goal: a file written before
// upgrade step goals, or one a sync brought in late.
func LinkKeyFor(key string, target Type) (string, bool) {
	if key == "project" && target == TypeGoal {
		return "goal", true
	}
	return key, false
}

// Relink returns d with every reference to an id in ids (old to new) changed
// to the new id: wikilinks in the title, body, and frontmatter, and
// frontmatter scalars that are exactly an old id. When typeOf is not nil, a
// top-level link key whose target is of another type moves to its key
// (LinkKeyFor), unless that key is already set. changed is false when nothing
// changed.
func (d Document) Relink(ids map[string]string, typeOf func(id string) Type) (out Document, changed bool, err error) {
	n, err := frontmatterNode(d)
	if err != nil {
		return d, false, err
	}
	rewrite := func(s string) string {
		if to, ok := ids[strings.TrimSpace(s)]; ok {
			return to
		}
		for from, to := range ids {
			s = RewriteLinkText(s, from, to)
		}
		return s
	}
	var walk func(*yaml.Node)
	walk = func(x *yaml.Node) {
		if x.Kind == yaml.ScalarNode && x.Tag != "!!int" {
			if v := rewrite(x.Value); v != x.Value {
				x.Value, changed = v, true
			}
		}
		for _, c := range x.Content {
			walk(c)
		}
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if !noRelinkKeys[n.Content[i].Value] {
			walk(n.Content[i+1])
		}
	}
	if typeOf != nil {
		if moveKeys(n, typeOf) {
			changed = true
		}
	}
	title, body := d.Title, d.Body
	for from, to := range ids {
		title, body = RewriteLinkText(title, from, to), RewriteLinkText(body, from, to)
	}
	if title != d.Title || body != d.Body {
		changed = true
	}
	if !changed {
		return d, false, nil
	}
	d.Title, d.Body = title, body
	out, err = decodeNode(d, n)
	return out, true, err
}

// moveKeys moves each top-level scalar link to the key its target's type
// asks for (LinkKeyFor), when that key is not set yet.
func moveKeys(n *yaml.Node, typeOf func(string) Type) bool {
	set := map[string]bool{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		set[n.Content[i].Value] = true
	}
	moved := false
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i], n.Content[i+1]
		if v.Kind != yaml.ScalarNode || v.Value == "" {
			continue
		}
		to, ok := LinkKeyFor(k.Value, typeOf(Link(v.Value).Target()))
		if !ok || set[to] {
			continue
		}
		set[to], set[k.Value] = true, false
		k.Value, moved = to, true
	}
	return moved
}
