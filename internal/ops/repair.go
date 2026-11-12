package ops

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/paramd"
	"github.com/iodesystems/para/internal/store"
)

// Repair ops. They exist so an agent never repairs a vault with a script: each
// leaves a receipt with the before and after. All writes go through the store.

// checkDoc renders d and refuses it when lint reports an error. It is the same
// check the vault makes at write time, run here so a preview and a refusal read
// alike.
func checkDoc(d model.Document) error {
	switch m := d.Meta.(type) {
	case *model.Goal:
		if err := m.CheckBrief(); err != nil {
			return fmt.Errorf("%s: %w", d.ID, err)
		}
		if err := model.CheckAssumptions(m.Assumptions); err != nil {
			return fmt.Errorf("%s: %w", d.ID, err)
		}
	case *model.Task:
		if err := model.CheckAssumptions(m.Assumptions); err != nil {
			return fmt.Errorf("%s: %w", d.ID, err)
		}
	}
	b, err := model.Render(d)
	if err != nil {
		return err
	}
	var bad []string
	for _, f := range paramd.Lint(paramd.Parse(string(d.Folder)+"/"+d.ID+".md", b)) {
		if f.Severity == paramd.Error {
			bad = append(bad, f.Msg)
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("%s would fail lint: %s", d.ID, strings.Join(bad, "; "))
	}
	return nil
}

var camelRe = regexp.MustCompile(`[A-Z]`)

// snakeFields returns fields with camelCase keys written as snake_case. The
// GraphQL surface camel-cases the keys of a JSON object; frontmatter keys are
// snake_case, and none has an upper-case letter.
func snakeFields(in map[string]any) map[string]any {
	if len(in) == 0 {
		return in
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[strings.ToLower(camelRe.ReplaceAllStringFunc(k, func(c string) string { return "_" + c }))] = v
	}
	return out
}

// DocRenameInput is the input of doc_rename.
type DocRenameInput struct {
	ID    string `json:"id"`
	NewID string `json:"new_id"`
}

// DocRenameOutput is the output of doc_rename.
type DocRenameOutput struct {
	ID        string   `json:"id"`
	Rev       string   `json:"rev"`
	Rewritten []string `json:"rewritten"` // documents whose links changed
}

// DocRename renames a document's file and rewrites every wikilink to it.
// receipt_undo cannot reverse it: the undo restores text, not the old id, and
// dossiers keep no receipt of their rewritten links.
var DocRename = withNoUndo(define("doc_rename", "Rename a document (its id and file name) and rewrite every link to it; refuses an id that is taken. It cannot be undone, so the owner makes it", func(ctx context.Context, d Deps, in DocRenameInput) (DocRenameOutput, error) {
	id, newID := normID(in.ID), normID(in.NewID)
	if id == "" || newID == "" {
		return DocRenameOutput{}, errors.New("doc_rename: id and new_id are required")
	}
	if !paramd.ValidStem(newID) {
		return DocRenameOutput{}, fmt.Errorf("doc_rename: new_id %q is not a valid file name", newID)
	}
	if _, err := d.Store.Get(ctx, id); err != nil {
		return DocRenameOutput{}, fmt.Errorf("doc_rename: %w", err)
	}
	if id == newID {
		return DocRenameOutput{}, errors.New("doc_rename: new_id is the same as id")
	}
	renamed, err := d.Store.Rename(ctx, id, newID)
	if err != nil {
		return DocRenameOutput{}, fmt.Errorf("doc_rename: %w", err)
	}
	refs, err := d.Store.List(ctx, store.Filter{All: true})
	if err != nil {
		return DocRenameOutput{}, fmt.Errorf("doc_rename: %w", err)
	}
	out := DocRenameOutput{ID: renamed.ID, Rev: renamed.Rev, Rewritten: []string{}}
	for _, r := range refs {
		doc, err := d.Store.Get(ctx, r.ID)
		if err != nil {
			return out, fmt.Errorf("doc_rename: %w", err)
		}
		nd, changed, err := doc.RewriteLinks(id, newID)
		if err != nil {
			return out, fmt.Errorf("doc_rename: %s: %w", r.ID, err)
		}
		if !changed {
			continue
		}
		saved, err := d.Store.Update(ctx, nd)
		if err != nil {
			return out, fmt.Errorf("doc_rename: %s: %w", r.ID, err)
		}
		out.Rewritten = append(out.Rewritten, r.ID)
		if r.ID == newID {
			out.Rev = saved.Rev
		}
	}
	return out, nil
}))

// DocRetypeInput is the input of doc_retype. Fields are frontmatter keys to set;
// give each field the new type requires and the document lacks.
type DocRetypeInput struct {
	ID     string         `json:"id"`
	Type   model.Type     `json:"type"`
	Fields map[string]any `json:"fields,omitempty"`
}

// DocRetypeOutput is the output of doc_retype.
type DocRetypeOutput struct {
	ID     string       `json:"id"`
	Type   model.Type   `json:"type"`
	Folder model.Folder `json:"folder"`
	Rev    string       `json:"rev"`
	// Kept lists frontmatter keys the new type does not declare. They stay as
	// extra frontmatter.
	Kept []string `json:"kept"`
}

// retypeRefused lists types a document may not become or leave: they hold
// their own state.
var retypeRefused = []model.Type{model.TypeProposal, model.TypeReceipt, model.TypeRequest, model.TypeQuestion}

// DocRetype changes a document's type and moves it to that type's folder. It
// drops nothing: keys that do not apply stay as extra frontmatter. It refuses
// when the new type's required fields are missing.
var DocRetype = define("doc_retype", "Change a document's type (note to policy_exception, person to team or role), move it to that type's folder, and set the fields the new type requires; keys that do not apply stay", func(ctx context.Context, d Deps, in DocRetypeInput) (DocRetypeOutput, error) {
	doc, err := d.Store.Get(ctx, normID(in.ID))
	if err != nil {
		return DocRetypeOutput{}, fmt.Errorf("doc_retype: %w", err)
	}
	from := doc.Meta.Header().Type
	if slices.Contains(retypeRefused, from) || slices.Contains(retypeRefused, in.Type) {
		return DocRetypeOutput{}, fmt.Errorf("doc_retype: %s and %s hold workflow state and are not retyped", from, in.Type)
	}
	if from == in.Type {
		return DocRetypeOutput{}, fmt.Errorf("doc_retype: %s is already a %s", doc.ID, from)
	}
	if in.Type == "" || model.FolderOf(in.Type) == "" {
		return DocRetypeOutput{}, fmt.Errorf("doc_retype: unknown type %q", in.Type)
	}
	if in.Type == model.TypeProject {
		if err := projectsOn(d.Store); err != nil {
			return DocRetypeOutput{}, fmt.Errorf("doc_retype: %w", err)
		}
	}
	nd, err := model.Retype(doc, in.Type, snakeFields(in.Fields))
	if err != nil {
		return DocRetypeOutput{}, fmt.Errorf("doc_retype: %w", err)
	}
	if err := checkDoc(nd); err != nil {
		return DocRetypeOutput{}, fmt.Errorf("doc_retype: %w (give the missing fields in fields)", err)
	}
	saved, err := d.Store.Update(ctx, nd)
	if err != nil {
		return DocRetypeOutput{}, fmt.Errorf("doc_retype: %w", err)
	}
	if saved, err = d.Store.Move(ctx, saved.ID, nd.Folder); err != nil {
		return DocRetypeOutput{}, fmt.Errorf("doc_retype: %w", err)
	}
	declared := map[string]bool{}
	for _, f := range paramd.TypeFields(string(in.Type)) {
		declared[f.Name] = true
	}
	out := DocRetypeOutput{ID: saved.ID, Type: in.Type, Folder: saved.Folder, Rev: saved.Rev, Kept: []string{}}
	for _, f := range paramd.TypeFields(string(from)) {
		if !declared[f.Name] {
			out.Kept = append(out.Kept, f.Name)
		}
	}
	return out, nil
})

// TaskMoveInput is the input of task_move. Give goal or area; ids moves
// several tasks.
type TaskMoveInput struct {
	ID   string   `json:"id,omitempty"`
	IDs  []string `json:"ids,omitempty"`
	Goal string   `json:"goal,omitempty"`
	Area string   `json:"area,omitempty"`
}

// TaskMoveOutput is the output of task_move.
type TaskMoveOutput struct {
	Moved []string `json:"moved"`
}

// TaskMove points tasks at another goal or area. A task keeps its other links.
var TaskMove = define("task_move", "Move tasks to another goal or area; give goal or area, and id or ids", func(ctx context.Context, d Deps, in TaskMoveInput) (TaskMoveOutput, error) {
	ids := slices.Clone(in.IDs)
	if in.ID != "" {
		ids = append(ids, in.ID)
	}
	if len(ids) == 0 {
		return TaskMoveOutput{}, errors.New("task_move: give id or ids")
	}
	if (in.Goal == "") == (in.Area == "") {
		return TaskMoveOutput{}, errors.New("task_move: give exactly one of goal or area")
	}
	project, err := goalLink(ctx, d.Store, in.Goal)
	if err != nil {
		return TaskMoveOutput{}, fmt.Errorf("task_move: %w", err)
	}
	area, err := areaLink(ctx, d.Store, in.Area)
	if err != nil {
		return TaskMoveOutput{}, fmt.Errorf("task_move: %w", err)
	}
	// Check every task before the first write, so a bad id moves nothing.
	var docs []model.Document
	for _, raw := range ids {
		doc, _, err := getTask(ctx, d.Store, normID(raw))
		if err != nil {
			return TaskMoveOutput{}, fmt.Errorf("task_move: %w", err)
		}
		docs = append(docs, doc)
	}
	out := TaskMoveOutput{Moved: []string{}}
	for _, doc := range docs {
		t := doc.Meta.(*model.Task)
		if project != "" {
			t.Goal, t.Area = project, ""
		} else {
			t.Goal, t.Area = "", area
		}
		if _, err := d.Store.Update(ctx, doc); err != nil {
			return out, fmt.Errorf("task_move: %s: %w", doc.ID, err)
		}
		out.Moved = append(out.Moved, doc.ID)
	}
	return out, nil
})

// RFXNudgeInput is the input of rfx_nudge.
type RFXNudgeInput struct {
	ID   string `json:"id"`
	Date string `json:"date"` // YYYY-MM-DD; empty clears it
}

// RFXNudgeOutput is the output of rfx_nudge.
type RFXNudgeOutput struct {
	ID        string `json:"id"`
	NextNudge string `json:"next_nudge"`
	Rev       string `json:"rev"`
}

// RFXNudge sets the follow-up date of an Ask (an rfx).
var RFXNudge = defineSafe("rfx_nudge", "Set the follow-up date (next_nudge) of an Ask, a request; an empty date clears it", func(ctx context.Context, d Deps, in RFXNudgeInput) (RFXNudgeOutput, error) {
	doc, err := d.Store.Get(ctx, normID(in.ID))
	if err != nil {
		return RFXNudgeOutput{}, fmt.Errorf("rfx_nudge: %w", err)
	}
	r, ok := doc.Meta.(*model.RFX)
	if !ok {
		return RFXNudgeOutput{}, fmt.Errorf("rfx_nudge: %s is a %s, not a request", doc.ID, doc.Meta.Header().Type)
	}
	if in.Date != "" {
		if err := checkDate("date", in.Date); err != nil {
			return RFXNudgeOutput{}, err
		}
	}
	r.NextNudge = model.Date(in.Date)
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return RFXNudgeOutput{}, fmt.Errorf("rfx_nudge: %w", err)
	}
	return RFXNudgeOutput{ID: doc.ID, NextNudge: in.Date, Rev: saved.Rev}, nil
})

// AttemptAddInput is the input of attempt_add.
type AttemptAddInput struct {
	Target  string   `json:"target"` // a task or an Ask (rfx)
	What    string   `json:"what"`   // what was tried
	Result  string   `json:"result"` // what came of it
	Sources []string `json:"sources,omitempty"`
}

// AttemptAddOutput is the output of attempt_add.
type AttemptAddOutput struct {
	ID  string `json:"id"`
	Rev string `json:"rev"`
}

// AttemptAdd appends an attempt to the Attempts section of a task or an Ask.
var AttemptAdd = defineSafe("attempt_add", "Record an attempt, something tried and what came of it, on a task or an Ask", func(ctx context.Context, d Deps, in AttemptAddInput) (AttemptAddOutput, error) {
	what, err := oneLine("what", strings.Join(strings.Fields(in.What), " "))
	if err != nil {
		return AttemptAddOutput{}, fmt.Errorf("attempt_add: %w", err)
	}
	result, err := oneLine("result", strings.Join(strings.Fields(in.Result), " "))
	if err != nil {
		return AttemptAddOutput{}, fmt.Errorf("attempt_add: %w", err)
	}
	doc, err := d.Store.Get(ctx, normID(in.Target))
	if err != nil {
		return AttemptAddOutput{}, fmt.Errorf("attempt_add: %w", err)
	}
	if t := doc.Meta.Header().Type; t != model.TypeTask && t != model.TypeRFX {
		return AttemptAddOutput{}, fmt.Errorf("attempt_add: %s is a %s: an attempt goes on a task or an Ask", doc.ID, t)
	}
	addAttemptLine(&doc, attemptLine(d, what, result, in.Sources))
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return AttemptAddOutput{}, fmt.Errorf("attempt_add: %w", err)
	}
	return AttemptAddOutput{ID: doc.ID, Rev: saved.Rev}, nil
})

// attemptLine is one line of an Attempts section: when, who, what, and the result.
func attemptLine(d Deps, what, result string, sources []string) string {
	line := fmt.Sprintf("- %s %s: %s: %s", d.Store.Now().UTC().Format(time.RFC3339), d.Store.Info().Actor, what, result)
	if src := cleanList(sources); len(src) > 0 {
		line += " (sources: " + strings.Join(src, "; ") + ")"
	}
	return line
}

// addAttemptLine appends a line to the Attempts section of a document body.
func addAttemptLine(doc *model.Document, line string) {
	if strings.Contains(doc.Body, "\n## Attempts\n") || strings.HasPrefix(doc.Body, "## Attempts\n") {
		doc.Body += "\n" + line
		return
	}
	doc.Body = appendSection(doc.Body, "Attempts", line)
}

// typedLinkFields checks the goal and project fields of a doc_edit on a
// document of type typ and writes each as a link: goal names a goal, project
// an ongoing project, and a task reaches a project only through its goal,
// unless a cron made it (cronMade). A null value removes the link.
func typedLinkFields(ctx context.Context, s store.Store, typ model.Type, cronMade bool, fields map[string]any) error {
	for _, key := range []string{"goal", "project"} {
		v, ok := fields[key]
		if !ok || v == nil {
			continue
		}
		raw, ok := v.(string)
		if !ok {
			return fmt.Errorf("%s: want an id", key)
		}
		if strings.TrimSpace(raw) == "" {
			fields[key] = nil
			continue
		}
		var link model.Link
		var err error
		switch {
		case key == "project" && typ == model.TypeTask && !cronMade:
			return errors.New("project: a task reaches a project through a goal: set goal (only a task a cron made links a project)")
		case key == "project":
			link, err = projectLink(ctx, s, raw)
		default:
			link, err = goalLink(ctx, s, raw)
		}
		if err != nil {
			return err
		}
		fields[key] = string(link)
	}
	return nil
}

// DocEditInput is the input of doc_edit. Fields left out stay as they are. Fields
// are frontmatter keys the type declares and a person may change; a null value
// removes a key. Rev, when set, is the revision the edit was made from; a
// document that changed since is refused.
type DocEditInput struct {
	ID     string         `json:"id"`
	Title  *string        `json:"title,omitempty"`
	Body   *string        `json:"body,omitempty"`
	Fields map[string]any `json:"fields,omitempty"`
	Rev    string         `json:"rev,omitempty"`
	// Append adds text at the end of the body, or at the end of Section
	// (made when missing). Dated makes it a line "- YYYY-MM-DD: text".
	Append string `json:"append,omitempty"`
	Dated  bool   `json:"dated,omitempty"`
	// Section names a "## " section of the body. With SectionBody its text is
	// replaced (the section is added when missing); with Append the text goes
	// at its end.
	Section     string  `json:"section,omitempty"`
	SectionBody *string `json:"section_body,omitempty"`
}

// bodyChange applies body, append, and section_body to body. ok is false
// when the input changes no body text.
func (in DocEditInput) bodyChange(body, today string) (_ string, ok bool, _ error) {
	section := strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(in.Section), "#"))
	app := strings.TrimSpace(in.Append)
	switch {
	case in.Body != nil && (app != "" || in.SectionBody != nil):
		return "", false, errors.New("give body, or append or section_body, not both")
	case app != "" && in.SectionBody != nil:
		return "", false, errors.New("give append or section_body, not both")
	case in.SectionBody != nil && section == "":
		return "", false, errors.New("section_body needs section")
	case in.Dated && app == "":
		return "", false, errors.New("dated needs append")
	case section != "" && app == "" && in.SectionBody == nil:
		return "", false, errors.New("section needs append or section_body")
	}
	switch {
	case in.Body != nil:
		return strings.TrimSpace(*in.Body), true, nil
	case app != "":
		if in.Dated {
			app = "- " + today + ": " + app
		}
		if section != "" {
			return appendToSection(body, "## "+section, app), true, nil
		}
		if body = strings.TrimSpace(body); body != "" {
			return body + "\n\n" + app, true, nil
		}
		return app, true, nil
	case in.SectionBody != nil:
		return setSection(body, "## "+section, strings.TrimSpace(*in.SectionBody)), true, nil
	}
	return body, false, nil
}

// DocEditOutput is the output of doc_edit.
type DocEditOutput struct {
	ID  string `json:"id"`
	Rev string `json:"rev"`
}

var journalBlockRe = regexp.MustCompile(`(?s)%% para:begin .*? %%.*?%% para:end .*? %%`)

// ErrStale is an edit made from an old revision.
var ErrStale = errors.New("the document changed since you opened it; open it again and redo the edit")

// DocEdit changes the title, body, or declared fields of a document.
var DocEdit = define("doc_edit", "Change the title, body, or declared fields of a document (task, goal, area, rfx, meeting, journal, incident, request, person, group, team, role, domain, convention, constraint, policy, exception, interaction, inbox, transcript, tracked_doc); not id, type, status, or generated text. append adds text at the end of the body (dated: as \"- YYYY-MM-DD: text\"), or at the end of section; section with section_body replaces that one \"## \" section; neither needs the whole body. Text in fields is linted like a body before the write (raw HTML such as <date> is refused; put it in backticks). A note uses note_edit", func(ctx context.Context, d Deps, in DocEditInput) (DocEditOutput, error) {
	if in.Title == nil && in.Body == nil && len(in.Fields) == 0 && strings.TrimSpace(in.Append) == "" && in.SectionBody == nil {
		return DocEditOutput{}, errors.New("doc_edit: nothing to edit: give title, body, append, section_body, or fields")
	}
	doc, err := d.Store.Get(ctx, normID(in.ID))
	if err != nil {
		return DocEditOutput{}, fmt.Errorf("doc_edit: %w", err)
	}
	if in.Rev != "" && in.Rev != doc.Rev {
		return DocEditOutput{}, fmt.Errorf("doc_edit: %s: %w", doc.ID, ErrStale)
	}
	typ := doc.Meta.Header().Type
	if typ == model.TypeNote {
		return DocEditOutput{}, errors.New("doc_edit: a note is edited with edit")
	}
	allowed := paramd.EditableFields(string(typ))
	if allowed == nil {
		return DocEditOutput{}, fmt.Errorf("doc_edit: a %s is not edited here; it has its own ops", typ)
	}
	if r, ok := doc.Meta.(*model.Request); ok && agentSurface(d) && d.Store.Info().Surface != surfaceProposal {
		// Text an agent read must not change what another agent runs: an
		// agent edits only agent work it holds itself (plan N).
		if r.Status != model.RequestClaimed || r.Claimed != d.Store.Info().Actor {
			return DocEditOutput{}, fmt.Errorf("doc_edit: %s is agent work you do not hold (%s): an agent edits only its own claimed work; note on it with request_note, or ask the owner", doc.ID, r.Status)
		}
	}
	in.Fields = snakeFields(in.Fields)
	if _, ok := in.Fields["publish"]; ok {
		return DocEditOutput{}, errors.New("doc_edit: publish is not edited here: the owner sets it with publish_set (para publish set)")
	}
	for k := range in.Fields {
		if !slices.ContainsFunc(allowed, func(f paramd.FieldInfo) bool { return f.Name == k }) {
			names := make([]string, len(allowed))
			for i, f := range allowed {
				names[i] = f.Name
			}
			return DocEditOutput{}, fmt.Errorf("doc_edit: %q is not an editable field of a %s; editable: %s", k, typ, strings.Join(names, ", "))
		}
	}
	cronMade := false
	if t, ok := doc.Meta.(*model.Task); ok {
		cronMade = t.Cron != ""
	}
	if err := typedLinkFields(ctx, d.Store, typ, cronMade, in.Fields); err != nil {
		return DocEditOutput{}, fmt.Errorf("doc_edit: %w", err)
	}
	// Field text gets the check task_add and task_edit give it: lint of the
	// file reads only the body, so text in a field is checked here, before
	// the write.
	for _, k := range slices.Sorted(maps.Keys(in.Fields)) {
		if err := lintValue(k, in.Fields[k]); err != nil {
			return DocEditOutput{}, fmt.Errorf("doc_edit: %w", err)
		}
	}
	oldBody := doc.Body
	if in.Title != nil {
		title := strings.Join(strings.Fields(*in.Title), " ")
		if title == "" {
			return DocEditOutput{}, errors.New("doc_edit: title is empty")
		}
		doc.Title = title
	}
	body, changed, err := in.bodyChange(doc.Body, d.Store.Now().Format(dateLayout))
	if err != nil {
		return DocEditOutput{}, fmt.Errorf("doc_edit: %w", err)
	}
	if changed {
		doc.Body = body
		if typ == model.TypeJournal && !slices.Equal(journalBlockRe.FindAllString(oldBody, -1), journalBlockRe.FindAllString(doc.Body, -1)) {
			return DocEditOutput{}, errors.New("doc_edit: a journal's generated sections (between %% para:begin and %% para:end) are not edited; change the text outside them")
		}
	}
	if len(in.Fields) > 0 {
		var before string
		if p, ok := doc.Meta.(*model.Project); ok {
			before = p.StateLine
		}
		if doc, err = doc.WithFields(in.Fields); err != nil {
			return DocEditOutput{}, fmt.Errorf("doc_edit: %w", err)
		}
		// A new state line is dated today (state_date is para's).
		if p, ok := doc.Meta.(*model.Project); ok && p.StateLine != before {
			p.StateLine = strings.Join(strings.Fields(p.StateLine), " ")
			p.StateDate = model.Date(d.Store.Now().Format(dateLayout))
		}
		_, sys := in.Fields["system"]
		_, ven := in.Fields["vendor"]
		if src, ok := doc.Meta.(*model.SourceDoc); ok && (sys || ven) {
			// An edited source is written as class and vendor.
			if sys && !ven {
				src.Vendor = ""
			}
			newForm(src)
		}
	}
	if err := checkDoc(doc); err != nil {
		return DocEditOutput{}, fmt.Errorf("doc_edit: %w", err)
	}
	if slices.ContainsFunc([]string{"area", "concepts", "kind", "cadence", "refines"}, func(k string) bool { _, ok := in.Fields[k]; return ok }) {
		if err := checkKindLinks(ctx, d.Store, doc); err != nil {
			return DocEditOutput{}, fmt.Errorf("doc_edit: %w", err)
		}
	}
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return DocEditOutput{}, fmt.Errorf("doc_edit: %w", err)
	}
	return DocEditOutput{ID: saved.ID, Rev: saved.Rev}, nil
})
