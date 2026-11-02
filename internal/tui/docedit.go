package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/ops"
	"github.com/iodesystems/parasolve/internal/paramd"
)

// editable reports whether e opens the editor on d: a note, or a type doc_edit
// changes.
func editable(d model.Document) bool {
	if _, ok := d.Meta.(*model.Note); ok {
		return true
	}
	return paramd.EditableFields(string(d.Meta.Header().Type)) != nil
}

// docBuffer is a document as the editor shows it: the title, one line per
// declared field ("name: value", JSON for a list), a split line, the body. A
// field with no value is left out; add a line to set it.
func docBuffer(d model.Document) (string, error) {
	text, err := d.FieldText()
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(editTitle + d.Title + "\n")
	for _, f := range paramd.EditableFields(string(d.Meta.Header().Type)) {
		if v, ok := text[f.Name]; ok {
			fmt.Fprintf(&b, "%s: %s\n", f.Name, v)
		}
	}
	b.WriteString(editSplit + "\n" + d.Body)
	return b.String(), nil
}

// fieldValue reads the text of one field back into the value doc_edit takes.
func fieldValue(f paramd.FieldInfo, v string) (any, error) {
	switch f.Kind {
	case "bool":
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("%s: want true or false", f.Name)
		}
		return b, nil
	case "count":
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, fmt.Errorf("%s: want a number", f.Name)
		}
		return n, nil
	case "list", "links", "contacts", "pointers", "targets", "sources", "source":
		var x any
		if err := json.Unmarshal([]byte(v), &x); err != nil {
			return nil, fmt.Errorf("%s: want JSON, such as [\"a\", \"b\"]", f.Name)
		}
		return x, nil
	}
	return v, nil
}

// parseDocBuffer reads an edited buffer back into a doc_edit. Only the fields
// that changed are sent; a field whose line was deleted is removed.
func parseDocBuffer(d model.Document, text string) (ops.DocEditInput, error) {
	head, body, ok := strings.Cut(text, "\n"+editSplit+"\n")
	lines := strings.Split(head, "\n")
	title, isTitle := strings.CutPrefix(lines[0], editTitle)
	if !ok || !isTitle {
		return ops.DocEditInput{}, fmt.Errorf("want a first line %q and a line %q before the body", editTitle+"...", editSplit)
	}
	if !strings.HasSuffix(d.Body, "\n") {
		body = strings.TrimSuffix(body, "\n")
	}
	allowed := paramd.EditableFields(string(d.Meta.Header().Type))
	old, err := d.FieldText()
	if err != nil {
		return ops.DocEditInput{}, err
	}
	seen := map[string]bool{}
	fields := map[string]any{}
	for _, l := range lines[1:] {
		if strings.TrimSpace(l) == "" {
			continue
		}
		name, v, ok := strings.Cut(l, ": ")
		if !ok {
			name, v = strings.TrimSuffix(l, ":"), ""
		}
		var fi *paramd.FieldInfo
		for i := range allowed {
			if allowed[i].Name == name {
				fi = &allowed[i]
			}
		}
		if fi == nil {
			return ops.DocEditInput{}, fmt.Errorf("%q is not a field you can edit here", name)
		}
		seen[name] = true
		if v = strings.TrimSpace(v); v == old[name] {
			continue
		}
		if v == "" {
			fields[name] = nil
			continue
		}
		val, err := fieldValue(*fi, v)
		if err != nil {
			return ops.DocEditInput{}, err
		}
		fields[name] = val
	}
	for _, f := range allowed {
		if _, had := old[f.Name]; had && !seen[f.Name] {
			fields[f.Name] = nil
		}
	}
	title = strings.TrimSpace(title)
	in := ops.DocEditInput{ID: d.ID, Title: &title, Body: &body, Rev: d.Rev}
	if len(fields) > 0 {
		in.Fields = fields
	}
	return in, nil
}

// docEditedMsg is the end of an $EDITOR run on a document other than a note.
type docEditedMsg struct {
	id, path string
	doc      model.Document
	err      error
}

// editDocBuffer runs $EDITOR on the document of the page showing. A note keeps
// its own op; every other editable type goes through doc_edit.
func (m *Model) editDocBuffer(p *page) tea.Cmd {
	buf, err := docBuffer(p.doc)
	if err != nil {
		m.setStatus(err.Error(), true)
		return nil
	}
	f, err := os.CreateTemp(m.env.StateDir, "edit-*.txt")
	if err != nil {
		m.setStatus(err.Error(), true)
		return nil
	}
	path := f.Name()
	_, werr := f.WriteString(buf)
	if err := errors.Join(werr, f.Close()); err != nil {
		_ = os.Remove(path)
		m.setStatus(err.Error(), true)
		return nil
	}
	editor := strings.Fields(m.env.getenv("EDITOR"))
	if len(editor) == 0 {
		editor = []string{"vi"}
	}
	doc := p.doc
	c := exec.Command(editor[0], append(editor[1:], path)...)
	return tea.ExecProcess(c, func(err error) tea.Msg { return docEditedMsg{id: doc.ID, path: path, doc: doc, err: err} })
}

func (m *Model) onDocEdited(msg docEditedMsg) tea.Cmd {
	defer func() { _ = os.Remove(msg.path) }()
	if msg.err != nil {
		m.setStatus("editor: "+msg.err.Error(), true)
		return nil
	}
	b, err := os.ReadFile(msg.path)
	if err != nil {
		m.setStatus(err.Error(), true)
		return nil
	}
	in, err := parseDocBuffer(msg.doc, string(b))
	if err != nil {
		m.setStatus(err.Error(), true)
		return nil
	}
	env := m.env
	load := m.loadDoc(msg.id, true)
	return func() tea.Msg {
		if _, err := ops.DocEdit.Run(context.Background(), env.Deps, in); err != nil {
			return doneMsg{err: err}
		}
		return load()
	}
}
