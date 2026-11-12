package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// NoteView is a note without its body.
type NoteView struct {
	ID     string       `json:"id"`
	Folder model.Folder `json:"folder"`
	Title  string       `json:"title"`
	Area   string       `json:"area,omitempty"`
	Goal   string       `json:"goal,omitempty"`
	Tags   []string     `json:"tags"`
	Rev    string       `json:"rev"`
}

func noteView(d model.Document) (NoteView, error) {
	n, ok := d.Meta.(*model.Note)
	if !ok {
		return NoteView{}, fmt.Errorf("%s is a %s, not a note", d.ID, d.Meta.Header().Type)
	}
	return NoteView{ID: d.ID, Folder: d.Folder, Title: d.Title, Area: n.Area.Target(), Goal: n.Goal.Target(),
		Tags: nonNil(n.Tags), Rev: d.Rev}, nil
}

// listNotes returns the notes that match keep, sorted by id.
func listNotes(ctx context.Context, s store.Store, keep func(NoteView) bool) ([]NoteView, error) {
	refs, err := s.List(ctx, store.Filter{Type: model.TypeNote})
	if err != nil {
		return nil, err
	}
	out := []NoteView{}
	for _, r := range refs {
		doc, err := s.Get(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		v, err := noteView(doc)
		if err != nil {
			return nil, err
		}
		if keep == nil || keep(v) {
			out = append(out, v)
		}
	}
	return out, nil
}

// goalLink returns the link to an existing goal; an empty id gives no link.
func goalLink(ctx context.Context, s store.Store, raw string) (model.Link, error) {
	id := normID(raw)
	if id == "" {
		return "", nil
	}
	d, _, err := getGoal(ctx, s, id)
	if err != nil {
		return "", linkErr("goal", id, err)
	}
	return model.LinkTo(d.ID), nil
}

// NoteAddInput is the input of note_add.
type NoteAddInput struct {
	Title string   `json:"title"`
	Body  string   `json:"body,omitempty"`
	Area  string   `json:"area,omitempty"` // area id
	Goal  string   `json:"goal,omitempty"` // goal id
	Tags  []string `json:"tags,omitempty"`
	// ImportedFrom is the file the new document comes from (import_verify).
	ImportedFrom *model.ImportedFrom `json:"imported_from,omitempty"`
}

// NoteAddOutput is the output of note_add.
type NoteAddOutput struct {
	ID     string       `json:"id"`
	Folder model.Folder `json:"folder"`
	Rev    string       `json:"rev"`
}

// NoteAdd creates a note in resources/.
var NoteAdd = defineSafe("note_add", "Create a note (reference material), optionally linked to an area or goal", func(ctx context.Context, d Deps, in NoteAddInput) (NoteAddOutput, error) {
	title := strings.Join(strings.Fields(in.Title), " ")
	if title == "" {
		return NoteAddOutput{}, errors.New("title is empty")
	}
	area, err := areaLink(ctx, d.Store, in.Area)
	if err != nil {
		return NoteAddOutput{}, err
	}
	project, err := goalLink(ctx, d.Store, in.Goal)
	if err != nil {
		return NoteAddOutput{}, err
	}
	doc, err := d.Store.Create(ctx, model.Document{
		Folder: model.FolderResources, Title: title, Body: demoteH1(in.Body),
		Meta: &model.Note{Common: model.Common{Type: model.TypeNote, Tags: cleanList(in.Tags)}, Area: area, Goal: project},
	})
	if err != nil {
		return NoteAddOutput{}, err
	}
	return NoteAddOutput{ID: doc.ID, Folder: doc.Folder, Rev: doc.Rev}, nil
})

// NoteListInput is the input of note_list.
type NoteListInput struct {
	Area string `json:"area,omitempty" query:"area"`
	Goal string `json:"goal,omitempty" query:"goal"`
	Tag  string `json:"tag,omitempty" query:"tag"`
}

// NoteListOutput is the output of note_list.
type NoteListOutput struct {
	Notes []NoteView `json:"notes"`
}

// NoteList lists notes.
var NoteList = defineRead("note_list", "List notes", func(ctx context.Context, d Deps, in NoteListInput) (NoteListOutput, error) {
	area, project, tag := normID(in.Area), normID(in.Goal), strings.TrimSpace(in.Tag)
	notes, err := listNotes(ctx, d.Store, func(n NoteView) bool {
		return (area == "" || n.Area == area) && (project == "" || n.Goal == project) &&
			(tag == "" || slices.Contains(n.Tags, tag))
	})
	return NoteListOutput{Notes: notes}, err
})

// NoteShowInput is the input of note_show.
type NoteShowInput struct {
	ID string `json:"id" query:"id" required:"true"`
}

// NoteShowOutput is the output of note_show.
type NoteShowOutput struct {
	Note NoteView `json:"note"`
	Body string   `json:"body"`
}

// NoteShow returns one note.
var NoteShow = defineRead("note_show", "Show a note", func(ctx context.Context, d Deps, in NoteShowInput) (NoteShowOutput, error) {
	doc, err := d.Store.Get(ctx, normID(in.ID))
	if err != nil {
		return NoteShowOutput{}, err
	}
	v, err := noteView(doc)
	if err != nil {
		return NoteShowOutput{}, err
	}
	return NoteShowOutput{Note: v, Body: doc.Body}, nil
})

// NoteEditInput is the input of note_edit. Fields left out stay as they are.
// Tags replaces the list; an empty list clears it.
type NoteEditInput struct {
	ID    string    `json:"id"`
	Title *string   `json:"title,omitempty"`
	Body  *string   `json:"body,omitempty"`
	Tags  *[]string `json:"tags,omitempty"`
}

// NoteEditOutput is the output of note_edit.
type NoteEditOutput struct {
	ID  string `json:"id"`
	Rev string `json:"rev"`
}

// NoteEdit changes the title, body, or tags of a note.
var NoteEdit = define("note_edit", "Change the title, body, or tags of a note (reference material)", func(ctx context.Context, d Deps, in NoteEditInput) (NoteEditOutput, error) {
	if in.Title == nil && in.Body == nil && in.Tags == nil {
		return NoteEditOutput{}, errors.New("nothing to edit: give title, body, or tags")
	}
	doc, err := d.Store.Get(ctx, normID(in.ID))
	if err != nil {
		return NoteEditOutput{}, fmt.Errorf("note_edit: %w", err)
	}
	n, ok := doc.Meta.(*model.Note)
	if !ok {
		return NoteEditOutput{}, fmt.Errorf("%s is a %s, not a note", doc.ID, doc.Meta.Header().Type)
	}
	if in.Title != nil {
		title := strings.Join(strings.Fields(*in.Title), " ")
		if title == "" {
			return NoteEditOutput{}, errors.New("title is empty")
		}
		doc.Title = title
	}
	if in.Body != nil {
		doc.Body = strings.TrimSpace(*in.Body)
	}
	if in.Tags != nil {
		n.Tags = cleanList(*in.Tags)
	}
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return NoteEditOutput{}, fmt.Errorf("note_edit: %w", err)
	}
	return NoteEditOutput{ID: saved.ID, Rev: saved.Rev}, nil
})
