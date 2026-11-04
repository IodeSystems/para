package model

import (
	"slices"

	"gopkg.in/yaml.v3"
)

// mergeSkip lists list keys a merge does not carry from the alias: they are
// para's own state of the alias, not facts about the work.
var mergeSkip = []string{"deferred_after", "deferred_added", "review_refs", "health_dismissed"}

// MergeLists adds to keep every entry of alias's frontmatter lists that keep
// lacks: lists of text and links (with, rfx, depends_on, blocked_by, tags,
// aliases, watchers), and calendar_uids by uid. Links to keep or alias are
// left out, so keep never waits on itself. It returns the keys it changed.
func MergeLists(keep, alias Document) (Document, []string, error) {
	kn, err := frontmatterNode(keep)
	if err != nil {
		return keep, nil, err
	}
	an, err := frontmatterNode(alias)
	if err != nil {
		return keep, nil, err
	}
	self := map[string]bool{string(LinkTo(keep.ID)): true, string(LinkTo(alias.ID)): true}
	var changed []string
	for i := 0; i+1 < len(an.Content); i += 2 {
		key, av := an.Content[i].Value, an.Content[i+1]
		if av.Kind != yaml.SequenceNode || slices.Contains(mergeSkip, key) || len(av.Content) == 0 {
			continue
		}
		kv := mapValue(kn, key)
		if kv == nil {
			kv = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			kn.Content = append(kn.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, kv)
		}
		if kv.Kind != yaml.SequenceNode {
			continue
		}
		before := len(kv.Content)
		for _, e := range av.Content {
			switch {
			case e.Kind == yaml.ScalarNode:
				if self[e.Value] || slices.ContainsFunc(kv.Content, func(x *yaml.Node) bool { return x.Kind == yaml.ScalarNode && x.Value == e.Value }) {
					continue
				}
			case e.Kind == yaml.MappingNode && key == "calendar_uids":
				uid := mapValue(e, "uid")
				if uid == nil || slices.ContainsFunc(kv.Content, func(x *yaml.Node) bool {
					u := mapValue(x, "uid")
					return u != nil && u.Value == uid.Value
				}) {
					continue
				}
			default:
				continue
			}
			kv.Content = append(kv.Content, e)
		}
		if len(kv.Content) == 0 {
			removeKey(kn, key)
			continue
		}
		if len(kv.Content) != before {
			changed = append(changed, key)
		}
	}
	if len(changed) == 0 {
		return keep, nil, nil
	}
	out, err := decodeNode(keep, kn)
	return out, changed, err
}

// DedupeLinkLists removes repeated entries from every frontmatter list of
// text, and links to d itself, as a link rewrite can leave them. It reports
// whether anything changed.
func DedupeLinkLists(d Document) (Document, bool, error) {
	n, err := frontmatterNode(d)
	if err != nil {
		return d, false, err
	}
	self := string(LinkTo(d.ID))
	changed := false
	for i := 0; i+1 < len(n.Content); i += 2 {
		v := n.Content[i+1]
		if v.Kind != yaml.SequenceNode {
			continue
		}
		seen := map[string]bool{}
		kept := v.Content[:0:0]
		for _, e := range v.Content {
			if e.Kind == yaml.ScalarNode {
				if seen[e.Value] || e.Value == self {
					changed = true
					continue
				}
				seen[e.Value] = true
			}
			kept = append(kept, e)
		}
		v.Content = kept
	}
	if !changed {
		return d, false, nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if v := n.Content[i+1]; v.Kind == yaml.SequenceNode && len(v.Content) == 0 {
			removeKey(n, n.Content[i].Value)
			i -= 2
		}
	}
	out, err := decodeNode(d, n)
	return out, true, err
}

// mapValue returns the value of key in mapping n, or nil.
func mapValue(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

// removeKey drops key from mapping n.
func removeKey(n *yaml.Node, key string) {
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			n.Content = append(n.Content[:i], n.Content[i+2:]...)
			return
		}
	}
}
