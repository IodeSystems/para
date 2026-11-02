package model

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Errors every store returns. Test with errors.Is.
var (
	ErrNotFound = errors.New("document not found")
	ErrExists   = errors.New("document id already in use")
	ErrConflict = errors.New("document changed since it was read")
)

// Document is one para-md file. ID is the file name without .md: the link
// target, unique in the vault.
type Document struct {
	ID     string `json:"id"`
	Folder Folder `json:"folder"`
	Title  string `json:"title"`
	Body   string `json:"body"` // text after the title line
	Meta   Meta   `json:"meta"`
	Rev    string `json:"rev,omitempty"` // content hash; Update requires the current value

	// extra holds frontmatter keys that Meta does not declare. format.md
	// allows them, and an update must not drop them.
	extra map[string]yaml.Node
}

// Ref identifies a document without its content.
type Ref struct {
	ID     string `json:"id"`
	Folder Folder `json:"folder"`
	Type   Type   `json:"type"`
	Title  string `json:"title"`
	Rev    string `json:"rev"`
}

// Finding is one lint result. Path is relative to the vault root.
type Finding struct {
	Path     string `json:"path"`
	Line     int    `json:"line,omitempty"`
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// SeverityError is the Finding.Severity that refuses a write.
const SeverityError = "error"

func (f Finding) String() string {
	loc := f.Path
	if f.Line > 0 {
		loc = fmt.Sprintf("%s:%d", f.Path, f.Line)
	}
	return fmt.Sprintf("%s: %s [%s] %s", loc, f.Severity, f.Code, f.Message)
}

// LintError is a write refused because the document fails lint.
type LintError struct {
	Findings []Finding
}

func (e *LintError) Error() string {
	var b strings.Builder
	b.WriteString("lint refused the write")
	for _, f := range e.Findings {
		b.WriteString("\n  ")
		b.WriteString(f.String())
	}
	return b.String()
}

// Event is one line of events/YYYY-MM.<install>.jsonl: ids and metadata,
// never content. Install is the machine that wrote the line. Hash is the
// sha256 of the file the event produced (create, update, move, external_edit);
// it identifies content and holds none, and lets another machine tell a synced
// write from a hand edit.
type Event struct {
	TS      time.Time `json:"ts"`
	Install string    `json:"install"`
	Actor   string    `json:"actor"`
	Surface string    `json:"surface"`
	Vault   string    `json:"vault"`
	Op      string    `json:"op"`
	Doc     string    `json:"doc"`
	From    string    `json:"from"`
	To      string    `json:"to"`
	Hash    string    `json:"hash,omitempty"`
	// Prev is the hash the update replaced. Two updates from one Prev mean a
	// sync tool dropped one of them.
	Prev string `json:"prev,omitempty"`
}

// ChangeKind says how a file differs from the manifest.
type ChangeKind string

const (
	ChangeAdded   ChangeKind = "added"
	ChangeChanged ChangeKind = "changed"
	ChangeRemoved ChangeKind = "removed"
)

// Outcome says what reconcile did with a change.
type Outcome string

const (
	// OutcomeLogged: no event explained the change; an external_edit was logged.
	OutcomeLogged Outcome = "logged"
	// OutcomeSynced: a logged event from any install carries the file's hash,
	// so another machine wrote it. Accepted without an external_edit.
	OutcomeSynced Outcome = "synced"
	// OutcomePending: no matching event yet. Held for the grace period in case
	// a sync tool delivers the event after the file.
	OutcomePending Outcome = "pending"
)

// Change is one file changed outside the vault write path.
type Change struct {
	Path    string     `json:"path"`
	Kind    ChangeKind `json:"kind"`
	Outcome Outcome    `json:"outcome"`
}

// ReconcileResult is the outcome of a reconcile. Baseline is true when no
// manifest existed: the current files became the baseline and nothing was
// logged.
type ReconcileResult struct {
	Baseline bool     `json:"baseline"`
	Changes  []Change `json:"changes"`
}

// Render produces para-md: frontmatter, blank line, title, body.
func Render(d Document) ([]byte, error) { return RenderOnto(d, nil) }

// RenderOnto is Render for an update: oldFrontmatter is the YAML of the file
// being replaced. Its comments, key order, and the form of every unchanged
// value are kept, and only keys whose value changed are rewritten. Keys the
// new document no longer has are dropped, and new keys are appended. An
// oldFrontmatter that is empty or not a mapping is ignored.
func RenderOnto(d Document, oldFrontmatter []byte) ([]byte, error) {
	if d.Meta == nil {
		return nil, errors.New("model: render: document has no meta")
	}
	var fresh yaml.Node
	if err := fresh.Encode(d.Meta); err != nil {
		return nil, fmt.Errorf("model: render: %w", err)
	}
	keys := make([]string, 0, len(d.extra))
	for k := range d.extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := d.extra[k]
		fresh.Content = append(fresh.Content, &yaml.Node{Kind: yaml.ScalarNode, Value: k}, &v)
	}
	out := &fresh
	if len(bytes.TrimSpace(oldFrontmatter)) > 0 {
		var old yaml.Node
		if err := yaml.Unmarshal(oldFrontmatter, &old); err == nil &&
			old.Kind == yaml.DocumentNode && len(old.Content) == 1 && old.Content[0].Kind == yaml.MappingNode {
			mergeMapping(old.Content[0], &fresh)
			out = &old
		}
	}
	var b bytes.Buffer
	b.WriteString("---\n")
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(out); err != nil {
		return nil, fmt.Errorf("model: render: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("model: render: %w", err)
	}
	b.WriteString("---\n\n# ")
	b.WriteString(d.Title)
	b.WriteString("\n")
	if body := strings.Trim(d.Body, "\n"); body != "" {
		b.WriteString("\n")
		b.WriteString(body)
		b.WriteString("\n")
	}
	return b.Bytes(), nil
}

// mergeMapping makes old hold the keys of fresh. A key whose value is the same
// keeps its old nodes, with comments and style. A changed value is replaced,
// and the old comments move to the new value. old is edited in place.
func mergeMapping(old, fresh *yaml.Node) {
	have := map[string]bool{}
	for i := 0; i+1 < len(old.Content); i += 2 {
		have[old.Content[i].Value] = true
	}
	want := map[string]*yaml.Node{}
	for i := 0; i+1 < len(fresh.Content); i += 2 {
		want[fresh.Content[i].Value] = fresh.Content[i+1]
	}
	kept := make([]*yaml.Node, 0, len(old.Content))
	for i := 0; i+1 < len(old.Content); i += 2 {
		k, v := old.Content[i], old.Content[i+1]
		nv, ok := want[k.Value]
		if !ok {
			continue
		}
		if !sameValue(v, nv) {
			nv.HeadComment, nv.LineComment, nv.FootComment = v.HeadComment, v.LineComment, v.FootComment
			v = nv
		}
		kept = append(kept, k, v)
	}
	for i := 0; i+1 < len(fresh.Content); i += 2 {
		if !have[fresh.Content[i].Value] {
			kept = append(kept, fresh.Content[i], fresh.Content[i+1])
		}
	}
	old.Content = kept
}

// sameValue reports whether two nodes hold the same data, ignoring comments
// and scalar style (plain, quoted, folded).
func sameValue(a, b *yaml.Node) bool {
	if a.Kind == yaml.AliasNode || b.Kind == yaml.AliasNode {
		return false
	}
	if a.Kind != b.Kind || len(a.Content) != len(b.Content) {
		return false
	}
	if a.Kind == yaml.ScalarNode {
		// A Date encodes as a plain timestamp; a quoted date is the same date.
		return a.Value == b.Value && (a.ShortTag() == b.ShortTag() || b.ShortTag() == "!!timestamp")
	}
	for i := range a.Content {
		if !sameValue(a.Content[i], b.Content[i]) {
			return false
		}
	}
	return true
}

// Decode builds a Document from raw frontmatter YAML and parsed text.
func Decode(id string, folder Folder, frontmatter []byte, title, body string) (Document, error) {
	var head struct {
		Type Type `yaml:"type"`
	}
	if err := yaml.Unmarshal(frontmatter, &head); err != nil {
		return Document{}, fmt.Errorf("model: decode %s: %w", id, err)
	}
	meta, ok := NewMeta(head.Type)
	if !ok {
		return Document{}, fmt.Errorf("model: decode %s: unknown type %q", id, head.Type)
	}
	if err := yaml.Unmarshal(frontmatter, meta); err != nil {
		return Document{}, fmt.Errorf("model: decode %s: %w", id, err)
	}
	extra, err := unknownKeys(frontmatter, meta)
	if err != nil {
		return Document{}, fmt.Errorf("model: decode %s: %w", id, err)
	}
	return Document{ID: id, Folder: folder, Title: title, Body: body, Meta: meta, extra: extra}, nil
}

// unknownKeys returns the frontmatter keys that meta does not write back.
func unknownKeys(frontmatter []byte, meta Meta) (map[string]yaml.Node, error) {
	var all map[string]yaml.Node
	if err := yaml.Unmarshal(frontmatter, &all); err != nil {
		return nil, err
	}
	var known yaml.Node
	if err := known.Encode(meta); err != nil {
		return nil, err
	}
	for i := 0; i+1 < len(known.Content); i += 2 {
		delete(all, known.Content[i].Value)
	}
	if len(all) == 0 {
		return nil, nil
	}
	return all, nil
}
