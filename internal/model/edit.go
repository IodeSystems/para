package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// FolderOf returns the folder a document of type t lives in.
func FolderOf(t Type) Folder {
	switch t {
	case TypeProject, TypeTask:
		return FolderProjects
	case TypeArea:
		return FolderAreas
	case TypeNote:
		return FolderResources
	case TypeRFX:
		return FolderRFX
	case TypeConstraint:
		return FolderConstraints
	case TypePolicy, TypeTranscript, TypePolicyException:
		return FolderPolicies
	case TypeConvention:
		return FolderConventions
	case TypePerson, TypeTeam, TypeRole, TypeDomain, TypeGroup:
		return FolderOrg
	case TypeJournal:
		return FolderJournal
	case TypeInbox:
		return FolderInbox
	case TypeIO:
		return FolderIO
	case TypeTrackedDoc:
		return FolderTracked
	case TypeSource:
		return FolderSources
	case TypeInteraction:
		return FolderInteractions
	case TypeRequest:
		return FolderRequests
	case TypeIncident:
		return FolderIncidents
	case TypeProposal:
		return FolderProposals
	case TypeQuestion:
		return FolderQuestions
	case TypeMeeting:
		return FolderMeetings
	case TypeReceipt:
		return FolderReceipts
	case TypeObligation:
		return FolderObligations
	}
	return ""
}

// frontmatterNode returns the document's frontmatter as one mapping: the typed
// fields and the extra keys.
func frontmatterNode(d Document) (*yaml.Node, error) {
	var n yaml.Node
	if err := n.Encode(d.Meta); err != nil {
		return nil, fmt.Errorf("model: %w", err)
	}
	keys := make([]string, 0, len(d.extra))
	for k := range d.extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := d.extra[k]
		n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: k}, &v)
	}
	return &n, nil
}

// decodeNode builds a document from n with the identity and text of d.
func decodeNode(d Document, n *yaml.Node) (Document, error) {
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	if err := enc.Encode(n); err != nil {
		return d, fmt.Errorf("model: %w", err)
	}
	if err := enc.Close(); err != nil {
		return d, fmt.Errorf("model: %w", err)
	}
	out, err := Decode(d.ID, d.Folder, b.Bytes(), d.Title, d.Body)
	if err != nil {
		return d, err
	}
	out.Rev = d.Rev
	return out, nil
}

// setKey sets key in mapping n; a nil value removes the key.
func setKey(n *yaml.Node, key string, v any) error {
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			if v == nil {
				n.Content = append(n.Content[:i], n.Content[i+2:]...)
				return nil
			}
			var nv yaml.Node
			if err := nv.Encode(v); err != nil {
				return err
			}
			n.Content[i+1] = &nv
			return nil
		}
	}
	if v == nil {
		return nil
	}
	var nv yaml.Node
	if err := nv.Encode(v); err != nil {
		return err
	}
	n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: key}, &nv)
	return nil
}

// WithFields returns d with the frontmatter keys in set applied: a nil value
// removes a key. It does not check the keys; lint does when the write is made.
func (d Document) WithFields(set map[string]any) (Document, error) {
	n, err := frontmatterNode(d)
	if err != nil {
		return d, err
	}
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if err := setKey(n, k, set[k]); err != nil {
			return d, fmt.Errorf("model: field %s: %w", k, err)
		}
	}
	return decodeNode(d, n)
}

// Retype returns d as type to, with set applied. Keys of the old type that the
// new one does not declare stay as extra frontmatter; nothing is dropped. The
// folder is the one for the new type.
func Retype(d Document, to Type, set map[string]any) (Document, error) {
	if _, ok := NewMeta(to); !ok {
		return d, fmt.Errorf("model: unknown type %q", to)
	}
	n, err := frontmatterNode(d)
	if err != nil {
		return d, err
	}
	if err := setKey(n, "type", string(to)); err != nil {
		return d, err
	}
	for k, v := range set {
		if k == "type" {
			continue
		}
		if err := setKey(n, k, v); err != nil {
			return d, fmt.Errorf("model: field %s: %w", k, err)
		}
	}
	out, err := decodeNode(d, n)
	if err != nil {
		return d, err
	}
	out.Folder = FolderOf(to)
	return out, nil
}

var wikilinkRe = regexp.MustCompile(`\[\[([^\]\|#]+)((?:#[^\]\|]*)?(?:\|[^\]]*)?)\]\]`)

// RewriteLinkText changes every wikilink to from in s so that it names to. The
// heading, block id, and alias after the name stay.
func RewriteLinkText(s, from, to string) string {
	return wikilinkRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := wikilinkRe.FindStringSubmatch(m)
		if strings.TrimSpace(sub[1]) != from {
			return m
		}
		return "[[" + to + sub[2] + "]]"
	})
}

// RewriteLinks returns d with every wikilink to from changed to name to, in the
// title, body, and frontmatter. changed is false when no text names from.
func (d Document) RewriteLinks(from, to string) (out Document, changed bool, err error) {
	n, err := frontmatterNode(d)
	if err != nil {
		return d, false, err
	}
	var walk func(*yaml.Node)
	walk = func(x *yaml.Node) {
		if x.Kind == yaml.ScalarNode && x.Tag != "!!int" {
			if v := RewriteLinkText(x.Value, from, to); v != x.Value {
				x.Value, changed = v, true
			}
		}
		for _, c := range x.Content {
			walk(c)
		}
	}
	walk(n)
	title, body := RewriteLinkText(d.Title, from, to), RewriteLinkText(d.Body, from, to)
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

// FieldText returns the frontmatter of d as text, per key: a scalar as it is
// written, anything else as JSON. A person edits these in a buffer.
func (d Document) FieldText() (map[string]string, error) {
	n, err := frontmatterNode(d)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, v := n.Content[i].Value, n.Content[i+1]
		if v.Kind == yaml.ScalarNode {
			out[k] = v.Value
			continue
		}
		var x any
		if err := v.Decode(&x); err != nil {
			return nil, err
		}
		b, err := json.Marshal(jsonable(x))
		if err != nil {
			return nil, err
		}
		out[k] = string(b)
	}
	return out, nil
}

// jsonable turns the map[any]any that YAML may give into maps JSON can write.
func jsonable(x any) any {
	switch v := x.(type) {
	case map[string]any:
		for k, e := range v {
			v[k] = jsonable(e)
		}
	case []any:
		for i, e := range v {
			v[i] = jsonable(e)
		}
	}
	return x
}
