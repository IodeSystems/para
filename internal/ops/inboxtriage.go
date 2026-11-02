package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/store"
)

// An inbox item is sorted once: it becomes a task, request, project, or note,
// or it is dropped. A note is a note document; with a person it is a dated line
// in that person's notes. Either way the item is archived, never
// deleted, so a link to it keeps resolving and the history stays.

// TriageTarget is where an inbox item goes.
type TriageTarget string

const (
	TriageTask     TriageTarget = "task"
	TriageRFX      TriageTarget = "rfx"
	TriageProject  TriageTarget = "project"
	TriageNote     TriageTarget = "note"     // a note document; with Person, a dated line in a person's notes
	TriageQuestion TriageTarget = "question" // an open question; it needs a scope
	TriageDrop     TriageTarget = "drop"     // archive with no new document
)

// TriageTargets lists every TriageTarget.
var TriageTargets = []TriageTarget{TriageTask, TriageRFX, TriageProject, TriageNote, TriageQuestion, TriageDrop}

// Enum implements the MCP enum hook.
func (TriageTarget) Enum() []string {
	out := make([]string, len(TriageTargets))
	for i, t := range TriageTargets {
		out[i] = string(t)
	}
	return out
}

// InboxItem is one inbox capture in a list.
type InboxItem struct {
	ID      string       `json:"id"`
	Folder  model.Folder `json:"folder"`
	Title   string       `json:"title"`
	Created model.Date   `json:"created"`
}

// InboxListInput is the input of inbox_list.
type InboxListInput struct {
	All bool `json:"all,omitempty" query:"all"` // include triaged (archived) items
}

// InboxListOutput is the output of inbox_list.
type InboxListOutput struct {
	Items []InboxItem `json:"items"`
}

// InboxList lists captures not yet triaged, oldest id first.
var InboxList = define("inbox_list", "List inbox captures not yet triaged", func(ctx context.Context, d Deps, in InboxListInput) (InboxListOutput, error) {
	refs, err := d.Store.List(ctx, store.Filter{Type: model.TypeInbox})
	if err != nil {
		return InboxListOutput{}, err
	}
	out := InboxListOutput{Items: []InboxItem{}}
	for _, r := range refs {
		if r.Folder != model.FolderInbox && !in.All {
			continue
		}
		doc, err := d.Store.Get(ctx, r.ID)
		if err != nil {
			return InboxListOutput{}, err
		}
		out.Items = append(out.Items, InboxItem{ID: doc.ID, Folder: doc.Folder, Title: doc.Title, Created: doc.Meta.Header().Created})
	}
	return out, nil
})

// InboxShowInput is the input of inbox_show.
type InboxShowInput struct {
	ID string `json:"id" query:"id" required:"true"`
}

// InboxShowOutput is the output of inbox_show.
type InboxShowOutput struct {
	Doc model.Document `json:"doc"`
}

// InboxShow shows one capture.
var InboxShow = define("inbox_show", "Show one inbox capture", func(ctx context.Context, d Deps, in InboxShowInput) (InboxShowOutput, error) {
	doc, _, err := getInbox(ctx, d, in.ID)
	if err != nil {
		return InboxShowOutput{}, err
	}
	return InboxShowOutput{Doc: doc}, nil
})

func getInbox(ctx context.Context, d Deps, id string) (model.Document, *model.Inbox, error) {
	id = normID(id)
	if id == "" {
		return model.Document{}, nil, errors.New("id is empty")
	}
	doc, err := d.Store.Get(ctx, id)
	if err != nil {
		return doc, nil, err
	}
	m, ok := doc.Meta.(*model.Inbox)
	if !ok {
		return doc, nil, fmt.Errorf("%s is %s, not inbox", id, doc.Meta.Header().Type)
	}
	return doc, m, nil
}

// InboxTriageInput is the input of inbox_triage. Each target needs its own
// fields; fields of another target are rejected, so a mistake is not silent.
type InboxTriageInput struct {
	ID string       `json:"id"`
	To TriageTarget `json:"to"`
	// task
	Project string `json:"project,omitempty"` // task: project id (or Area); rfx: optional project id
	// project
	Owner string `json:"owner,omitempty"`
	// rfx
	Direction model.RFXDirection `json:"direction,omitempty"` // default out
	Route     string             `json:"route,omitempty"`     // domain id
	Targets   []TargetInput      `json:"targets,omitempty"`
	SelfServe string             `json:"self_serve,omitempty"`
	NextNudge string             `json:"next_nudge,omitempty"`
	// area: task (instead of or beside Project), project, or note document
	Area string `json:"area,omitempty"`
	// note: a note document (Area, Project), or with Person a person note (Date)
	Person string `json:"person,omitempty"` // person id
	Date   string `json:"date,omitempty"`   // YYYY-MM-DD; default the local date
	// question: Project, Area, Task, or Root, exactly one
	DoneWhen    string             `json:"done_when,omitempty"` // task: how we know it is done; project: required
	Task        string             `json:"task,omitempty"`
	Root        bool               `json:"root,omitempty"`
	Kind        model.QuestionKind `json:"kind,omitempty"`
	Options     []string           `json:"options,omitempty"`
	Recommended string             `json:"recommended,omitempty"`
}

// InboxTriageOutput is the output of inbox_triage.
type InboxTriageOutput struct {
	ID       string       `json:"id"` // the inbox item
	To       TriageTarget `json:"to"`
	Created  string       `json:"created"`  // id of the new document, or of the person for a note; empty for drop
	Folder   model.Folder `json:"folder"`   // where the inbox item is now: archive
	Rev      string       `json:"rev"`      // of the inbox item
	Warnings []string     `json:"warnings"` // from the op that made the document
}

// InboxTriage turns a capture into a task, request, project, or person note,
// or drops it. The new document links back to the item in its body; the item
// gets a line that links forward, then moves to archive/. The new document is
// made first: if the archive step fails, the item stays in the inbox and the
// new document exists, so the error names it.
var InboxTriage = define("inbox_triage", "Turn an inbox capture into a task, request, project, question (it needs a scope), or note (a note document, or a person note with person), or drop it; the capture is archived", func(ctx context.Context, d Deps, in InboxTriageInput) (InboxTriageOutput, error) {
	doc, _, err := getInbox(ctx, d, in.ID)
	if err != nil {
		return InboxTriageOutput{}, err
	}
	if doc.Folder != model.FolderInbox {
		return InboxTriageOutput{}, fmt.Errorf("%s is already triaged (in %s)", doc.ID, doc.Folder)
	}
	if err := checkTriageFields(in); err != nil {
		return InboxTriageOutput{}, err
	}
	text := captureText(doc)
	back := "From [[" + doc.ID + "]]."
	out := InboxTriageOutput{ID: doc.ID, To: in.To, Warnings: []string{}}
	switch in.To {
	case TriageTask:
		r, err := TaskAdd.Run(ctx, d, TaskAddInput{Project: in.Project, Area: in.Area, Title: doc.Title, Body: joinBody(doc.Body, back), DoneWhen: in.DoneWhen})
		if err != nil {
			return out, err
		}
		out.Created = r.ID
	case TriageProject:
		r, err := ProjectAdd.Run(ctx, d, ProjectAddInput{Title: doc.Title, Owner: in.Owner, Area: in.Area, Body: joinBody(doc.Body, back), DoneWhen: in.DoneWhen})
		if err != nil {
			return out, err
		}
		out.Created = r.ID
	case TriageRFX:
		dir := in.Direction
		if dir == "" {
			dir = model.RFXOut
		}
		r, err := RFXAdd.Run(ctx, d, RFXAddInput{
			Direction: dir, Ask: joinBody(text, back), Route: in.Route, Targets: in.Targets,
			SelfServe: in.SelfServe, Project: in.Project, NextNudge: in.NextNudge,
		})
		if err != nil {
			return out, err
		}
		out.Created, out.Warnings = r.ID, r.Warnings
	case TriageQuestion:
		r, err := QuestionAdd.Run(ctx, d, QuestionAddInput{
			Text: joinBody(text, back), Project: in.Project, Task: in.Task, Area: in.Area, Root: in.Root,
			Kind: in.Kind, Options: in.Options, Recommended: in.Recommended,
		})
		if err != nil {
			return out, err
		}
		out.Created = r.ID
	case TriageNote:
		if in.Person == "" {
			r, err := NoteAdd.Run(ctx, d, NoteAddInput{Title: doc.Title, Body: joinBody(doc.Body, back), Area: in.Area, Project: in.Project})
			if err != nil {
				return out, err
			}
			out.Created = r.ID
			break
		}
		if _, err := OrgPersonNote.Run(ctx, d, PersonNoteInput{ID: in.Person, Text: text + " (" + strings.TrimSuffix(back, ".") + ")", Date: in.Date}); err != nil {
			return out, err
		}
		out.Created = normID(in.Person)
	case TriageDrop:
	}
	// Re-read: a note changes another document, not this one, but the Rev
	// must be current for Update.
	doc, _, err = getInbox(ctx, d, doc.ID)
	if err != nil {
		return out, triageHalf(out, err)
	}
	line := "Triaged: " + string(in.To)
	if out.Created != "" {
		line += " [[" + out.Created + "]]"
	}
	doc.Body = joinBody(doc.Body, line+".")
	if doc, err = d.Store.Update(ctx, doc); err != nil {
		return out, triageHalf(out, err)
	}
	moved, err := d.Store.Move(ctx, doc.ID, model.FolderArchive)
	if err != nil {
		return out, triageHalf(out, err)
	}
	out.Folder, out.Rev = moved.Folder, moved.Rev
	return out, nil
})

func triageHalf(out InboxTriageOutput, err error) error {
	if out.Created == "" {
		return err
	}
	return fmt.Errorf("%s was made but the inbox item was not archived: %w", out.Created, err)
}

// checkTriageFields requires what the target needs and rejects the rest.
func checkTriageFields(in InboxTriageInput) error {
	if !slices.Contains(TriageTargets, in.To) {
		return fmt.Errorf("to %q: want one of %v", in.To, TriageTargets)
	}
	have := map[string]bool{
		"project":     in.Project != "",
		"owner":       in.Owner != "",
		"direction":   in.Direction != "",
		"route":       in.Route != "",
		"targets":     len(in.Targets) > 0,
		"self_serve":  in.SelfServe != "",
		"next_nudge":  in.NextNudge != "",
		"area":        in.Area != "",
		"person":      in.Person != "",
		"date":        in.Date != "",
		"task":        in.Task != "",
		"root":        in.Root,
		"kind":        in.Kind != "",
		"options":     len(in.Options) > 0,
		"recommended": in.Recommended != "",
	}
	allowed := map[TriageTarget][]string{
		TriageTask:     {"project", "area"},
		TriageProject:  {"owner", "area"},
		TriageRFX:      {"direction", "route", "targets", "self_serve", "project", "next_nudge"},
		TriageNote:     {"person", "date", "area", "project"},
		TriageQuestion: {"project", "area", "task", "root", "kind", "options", "recommended"},
		TriageDrop:     {},
	}[in.To]
	required := map[TriageTarget][]string{
		TriageRFX: {"route"},
	}[in.To]
	if in.To == TriageTask && in.Project == "" && in.Area == "" {
		return errors.New("to task needs project or area")
	}
	if in.To == TriageNote {
		if in.Person != "" && (in.Area != "" || in.Project != "") {
			return errors.New("area and project apply to a note document: drop person, or drop area and project")
		}
		if in.Person == "" && in.Date != "" {
			return errors.New("date applies to a person note: give person")
		}
	}
	for _, f := range required {
		if !have[f] {
			return fmt.Errorf("to %s needs %s", in.To, f)
		}
	}
	for f, set := range have {
		if set && !slices.Contains(allowed, f) {
			return fmt.Errorf("%s does not apply to %s", f, in.To)
		}
	}
	return nil
}

// captureText is the text of the capture as written.
func captureText(doc model.Document) string {
	if cut, ok := strings.CutSuffix(doc.Title, "..."); ok && strings.HasPrefix(doc.Body, cut) {
		return doc.Body // inbox_add kept a long first line whole in the body
	}
	return joinBody(doc.Title, doc.Body)
}

func joinBody(parts ...string) string {
	var keep []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			keep = append(keep, p)
		}
	}
	return strings.Join(keep, "\n\n")
}
