package ops

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/iodesystems/para/internal/model"
)

// An incoming io item is filed once: into a task, question, Ask, note,
// interaction, or project that para makes from it, or into a document that
// already holds it. became links what it was filed into, and the new document
// links back. The item stays in io/ with state filed, so links keep resolving.

// TriageTarget is what an incoming io item is filed into.
type TriageTarget string

const (
	TriageTask        TriageTarget = "task"
	TriageRFX         TriageTarget = "rfx"
	TriageProject     TriageTarget = "project"
	TriageNote        TriageTarget = "note"        // a note document; with Person, a dated line in a person's notes
	TriageQuestion    TriageTarget = "question"    // an open question; it needs a scope
	TriageInteraction TriageTarget = "interaction" // a logged contact; with names the people
)

// TriageTargets lists every TriageTarget.
var TriageTargets = []TriageTarget{TriageTask, TriageRFX, TriageProject, TriageNote, TriageQuestion, TriageInteraction}

// Enum implements the MCP enum hook.
func (TriageTarget) Enum() []string {
	out := make([]string, len(TriageTargets))
	for i, t := range TriageTargets {
		out[i] = string(t)
	}
	return out
}

// triageTypes are the document types each target names, for existing.
var triageTypes = map[TriageTarget][]model.Type{
	TriageTask: {model.TypeTask}, TriageRFX: {model.TypeRFX}, TriageProject: {model.TypeProject},
	TriageNote: {model.TypeNote, model.TypePerson}, TriageQuestion: {model.TypeQuestion}, TriageInteraction: {model.TypeInteraction},
}

// IOFileInput is the input of io_file. Each target needs its own fields;
// fields of another target are rejected, so a mistake is not silent. With
// existing, no target field applies.
type IOFileInput struct {
	ID string       `json:"id"`
	To TriageTarget `json:"to,omitempty"` // required unless existing is given
	// Existing is the id of a document that already holds the message; para
	// links it and makes nothing.
	Existing string `json:"existing,omitempty"`
	// task
	Project string `json:"project,omitempty"` // task: project id (or Area); rfx: optional project id
	// project
	Owner string `json:"owner,omitempty"`
	// rfx
	Direction model.RFXDirection `json:"direction,omitempty"` // default in: someone asks the owner
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
	// interaction: person or group ids; default the sender when it is one
	With       []string         `json:"with,omitempty"`
	Visibility model.Visibility `json:"visibility,omitempty"`
}

// IOFileOutput is the output of io_file.
type IOFileOutput struct {
	ID       string        `json:"id"` // the io item
	To       TriageTarget  `json:"to"`
	Became   string        `json:"became"`  // id of the document it was filed into; the person for a person note
	Created  bool          `json:"created"` // para made Became; false with existing
	State    model.IOState `json:"state"`
	Rev      string        `json:"rev"`      // of the io item
	Warnings []string      `json:"warnings"` // from the op that made the document
}

// IOFile files an incoming message. The new document is made first: if the io
// item then cannot be marked filed, the error names the new document.
var IOFile = defineSafe("io_file", "File an incoming io item into a new task, Ask (rfx), question (it needs a scope), note (a note document, or a person note with person), interaction, or project, or link an existing document with existing; the item is marked filed and leaves the inbox", func(ctx context.Context, d Deps, in IOFileInput) (IOFileOutput, error) {
	doc, m, err := getOpenIO(ctx, d, in.ID)
	if err != nil {
		return IOFileOutput{}, err
	}
	if m.Direction != model.IOIn {
		return IOFileOutput{}, fmt.Errorf("%s is outgoing: send it with io_send or drop it", doc.ID)
	}
	out := IOFileOutput{ID: doc.ID, To: in.To, Warnings: []string{}}
	if in.Existing != "" {
		if out.To, out.Became, err = fileExisting(ctx, d, in); err != nil {
			return out, err
		}
	} else {
		if err := checkTriageFields(in); err != nil {
			return out, err
		}
		if err := fileNew(ctx, d, doc, m, in, &out); err != nil {
			return out, err
		}
		out.Created = true
	}
	// Re-read: the new document may link here, and the Rev must be current.
	doc, m, err = getIO(ctx, d, doc.ID)
	if err != nil {
		return out, triageHalf(out, err)
	}
	m.State, m.Became = model.IOFiled, model.LinkTo(out.Became)
	if doc, err = d.Store.Update(ctx, doc); err != nil {
		return out, triageHalf(out, err)
	}
	out.State, out.Rev = m.State, doc.Rev
	return out, nil
})

// fileExisting checks the document named by existing, and its type against to.
func fileExisting(ctx context.Context, d Deps, in IOFileInput) (TriageTarget, string, error) {
	other := in
	other.ID, other.To, other.Existing = "", "", ""
	if !reflect.ValueOf(other).IsZero() {
		return "", "", errors.New("existing links a document as it is: leave out the fields that make one")
	}
	id := normID(in.Existing)
	doc, err := d.Store.Get(ctx, id)
	if err != nil {
		return "", "", fmt.Errorf("existing %s: %w", id, err)
	}
	t := doc.Meta.Header().Type
	if in.To != "" {
		if !slices.Contains(triageTypes[in.To], t) {
			return "", "", fmt.Errorf("existing %s is a %s, not a %s", id, t, in.To)
		}
		return in.To, doc.ID, nil
	}
	for _, to := range TriageTargets {
		if slices.Contains(triageTypes[to], t) {
			return to, doc.ID, nil
		}
	}
	return "", "", fmt.Errorf("existing %s is a %s: file into a task, Ask, question, note, person, interaction, or project", id, t)
}

// fileNew makes the document the item is filed into.
func fileNew(ctx context.Context, d Deps, doc model.Document, m *model.IO, in IOFileInput, out *IOFileOutput) error {
	text := ioText(doc, m)
	back := "From [[" + doc.ID + "]]."
	switch in.To {
	case TriageTask:
		r, err := TaskAdd.Run(ctx, d, TaskAddInput{Project: in.Project, Area: in.Area, Title: doc.Title, Body: joinBody(doc.Body, back), DoneWhen: in.DoneWhen})
		if err != nil {
			return err
		}
		out.Became = r.ID
	case TriageProject:
		r, err := ProjectAdd.Run(ctx, d, ProjectAddInput{Title: doc.Title, Owner: in.Owner, Area: in.Area, Body: joinBody(doc.Body, back), DoneWhen: in.DoneWhen})
		if err != nil {
			return err
		}
		out.Became = r.ID
	case TriageRFX:
		dir := in.Direction
		if dir == "" {
			dir = model.RFXIn
		}
		r, err := RFXAdd.Run(ctx, d, RFXAddInput{
			Direction: dir, Ask: joinBody(text, back), Route: in.Route, Targets: in.Targets,
			SelfServe: in.SelfServe, Project: in.Project, NextNudge: in.NextNudge,
		})
		if err != nil {
			return err
		}
		out.Became, out.Warnings = r.ID, r.Warnings
	case TriageQuestion:
		r, err := QuestionAdd.Run(ctx, d, QuestionAddInput{
			Text: joinBody(text, back), Project: in.Project, Task: in.Task, Area: in.Area, Root: in.Root,
			Kind: in.Kind, Options: in.Options, Recommended: in.Recommended,
		})
		if err != nil {
			return err
		}
		out.Became = r.ID
	case TriageNote:
		if in.Person == "" {
			r, err := NoteAdd.Run(ctx, d, NoteAddInput{Title: doc.Title, Body: joinBody(doc.Body, back), Area: in.Area, Project: in.Project})
			if err != nil {
				return err
			}
			out.Became = r.ID
			break
		}
		if _, err := OrgPersonNote.Run(ctx, d, PersonNoteInput{ID: in.Person, Text: ioLine(text) + " (" + strings.TrimSuffix(back, ".") + ")", Date: in.Date}); err != nil {
			return err
		}
		out.Became = normID(in.Person)
	case TriageInteraction:
		with := in.With
		if len(with) == 0 {
			with = ioLinks([]string{m.From})
		}
		if len(with) == 0 {
			return errors.New("to interaction needs with: the sender is not a person or group in the vault")
		}
		date := ""
		if len(m.At) >= 10 {
			date = m.At[:10]
		}
		link := ""
		if strings.HasPrefix(m.Ref, "http") {
			link = m.Ref
		}
		r, err := InteractionAdd.Run(ctx, d, InteractionAddInput{Summary: joinBody(doc.Title, back), With: with, Medium: m.Medium,
			Direction: model.InteractionIn, Visibility: in.Visibility, URL: link, Date: date})
		if err != nil {
			return err
		}
		out.Became, out.Warnings = r.ID, r.Warnings
	}
	return nil
}

func triageHalf(out IOFileOutput, err error) error {
	if out.Became == "" {
		return err
	}
	return fmt.Errorf("%s was filed into %s, but the io item was not marked filed: %w", out.ID, out.Became, err)
}

// splitCapture makes the first line the title and the rest the body. A long
// first line is cut for the title and kept whole in the body.
func splitCapture(text string) (title, body string, err error) {
	text = cleanText(text)
	if text == "" {
		return "", "", errors.New("text is empty")
	}
	first, rest, _ := strings.Cut(text, "\n")
	first = ioLine(first)
	if utf8.RuneCountInString(first) <= ioTitleMax {
		return first, strings.TrimSpace(rest), nil
	}
	return string([]rune(first)[:ioTitleMax]) + "...", text, nil
}

// checkTriageFields requires what the target needs and rejects the rest.
func checkTriageFields(in IOFileInput) error {
	if !slices.Contains(TriageTargets, in.To) {
		return fmt.Errorf("to %q: want one of %v, or give existing", in.To, TriageTargets)
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
		"with":        len(in.With) > 0,
		"visibility":  in.Visibility != "",
	}
	allowed := map[TriageTarget][]string{
		TriageTask:        {"project", "area"},
		TriageProject:     {"owner", "area"},
		TriageRFX:         {"direction", "route", "targets", "self_serve", "project", "next_nudge"},
		TriageNote:        {"person", "date", "area", "project"},
		TriageQuestion:    {"project", "area", "task", "root", "kind", "options", "recommended"},
		TriageInteraction: {"with", "visibility", "date"},
	}[in.To]
	required := map[TriageTarget][]string{
		TriageRFX: {"route"},
	}[in.To]
	if in.To == TriageTask && in.Project == "" && in.Area == "" {
		return errors.New("to task needs project or area")
	}
	if in.To == TriageInteraction && in.Date != "" {
		return errors.New("date applies to a person note; an interaction takes the date of at")
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

func joinBody(parts ...string) string {
	var keep []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			keep = append(keep, p)
		}
	}
	return strings.Join(keep, "\n\n")
}
