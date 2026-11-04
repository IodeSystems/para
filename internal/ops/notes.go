package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/iodesystems/para/internal/model"
)

// Notes on any item: the text under "## Notes" in its body. notes_edit
// changes that section only, and only against the hash the writer read, so
// two writers never lose each other's text.

// NotesReadInput names an item.
type NotesReadInput struct {
	ID string `json:"id" query:"id" required:"true"`
}

// NotesReadOutput is the notes of an item. Hash is empty when it has none.
type NotesReadOutput struct {
	ID   string `json:"id"`
	MD   string `json:"md"`
	Hash string `json:"hash"`
}

// NotesEditInput is the input of notes_edit. Hash is what notes_read
// returned; leave it out only when the item has no notes.
type NotesEditInput struct {
	ID   string `json:"id"`
	Hash string `json:"hash,omitempty"`
	MD   string `json:"md"`
}

// NotesEditOutput is the hash of the notes after the edit.
type NotesEditOutput struct {
	ID   string `json:"id"`
	Hash string `json:"hash"`
	Rev  string `json:"rev"`
}

// NotesStale is a notes_edit against notes that changed since they were
// read. Hash is the current one: read again, merge, and retry.
type NotesStale struct {
	ID   string
	Hash string
}

func (e *NotesStale) Error() string {
	if e.Hash == "" {
		return fmt.Sprintf("%s has no notes now: retry without hash", e.ID)
	}
	return fmt.Sprintf("the notes of %s changed since you read them (current hash %s): read them again with notes_read, merge, and retry with that hash", e.ID, e.Hash)
}

// notesHash is the hash of notes text, or "" for none.
func notesHash(md string) string {
	if md == "" {
		return ""
	}
	return hashText(md)
}

func notesDoc(ctx context.Context, d Deps, id string) (model.Document, error) {
	id = normID(id)
	doc, err := d.Store.Get(ctx, id)
	if err != nil {
		return doc, linkErr("id", id, err)
	}
	if t := doc.Meta.Header().Type; slices.Contains(noRewrite, t) {
		return doc, fmt.Errorf("%s is a %s, which is a record: it has no notes to edit", id, t)
	}
	return doc, nil
}

// NotesRead returns an item's notes and their hash.
var NotesRead = defineRead("notes_read", "Read the notes of any item (the ## Notes section of its body) and their hash; pass the hash to notes_edit", func(ctx context.Context, d Deps, in NotesReadInput) (NotesReadOutput, error) {
	doc, err := notesDoc(ctx, d, in.ID)
	if err != nil {
		return NotesReadOutput{}, err
	}
	md := headingText(doc.Body, notesHeading)
	return NotesReadOutput{ID: doc.ID, MD: md, Hash: notesHash(md)}, nil
})

// NotesEditDescription is notes_edit's tool text.
const NotesEditDescription = "Write the notes of any item (the ## Notes section; the rest of the body is untouched). " +
	"Without hash: only when the item has no notes. With hash (from notes_read): replaces the notes when the hash matches the current notes, else it is refused with the current hash: read again, merge, retry. " +
	"Empty md removes the notes. Returns the new hash. Lines may not start with # or ## (use ###)"

func notesEdit(ctx context.Context, d Deps, in NotesEditInput) (NotesEditOutput, error) {
	md := strings.TrimSpace(strings.ReplaceAll(in.MD, "\r\n", "\n"))
	for _, l := range strings.Split(md, "\n") {
		if strings.HasPrefix(l, "# ") || strings.HasPrefix(l, "## ") {
			return NotesEditOutput{}, errors.New("notes lines must not start with # or ##: they would end the Notes section; use ###")
		}
	}
	doc, err := notesDoc(ctx, d, in.ID)
	if err != nil {
		return NotesEditOutput{}, err
	}
	cur := notesHash(headingText(doc.Body, notesHeading))
	if strings.TrimSpace(in.Hash) != cur {
		return NotesEditOutput{}, &NotesStale{ID: doc.ID, Hash: cur}
	}
	if md == "" {
		doc.Body = dropSection(doc.Body, notesHeading)
	} else {
		doc.Body = setSection(doc.Body, notesHeading, md)
	}
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return NotesEditOutput{}, err
	}
	return NotesEditOutput{ID: saved.ID, Hash: notesHash(md), Rev: saved.Rev}, nil
}

// NotesEdit writes an item's notes against the hash the caller read.
var NotesEdit = defineSafe("notes_edit", NotesEditDescription, notesEdit)

// NotesSave is notes_edit for the owner in the web app (scope review:decide).
var NotesSave = definePerson("notes_save", "Write the notes of any item from the web app, against the hash read; refused with the current hash when they changed. People only", notesEdit)

// dropSection removes the section under heading and its text.
func dropSection(body, heading string) string {
	lines := strings.Split(body, "\n")
	at := slices.IndexFunc(lines, func(l string) bool { return strings.TrimRight(l, " \t") == heading })
	if at < 0 {
		return body
	}
	end := sectionEnd(lines, at)
	out := append(append([]string(nil), lines[:at]...), lines[end:]...)
	return strings.Trim(strings.Join(out, "\n"), "\n")
}
