package ops

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/paramd"
	"github.com/iodesystems/parasolve/internal/store"
)

// Repair ops. They exist so an agent never repairs a vault with a script: each is
// risk review, so an agent files a proposal and the owner reads the computed
// before and after. All writes go through the store.

// checkDoc renders d and refuses it when lint reports an error. It is the same
// check the vault makes at write time, run here so a preview and a refusal read
// alike.
func checkDoc(d model.Document) error {
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
var DocRename = define("doc_rename", "Rename a document (its id and file name) and rewrite every link to it; refuses an id that is taken", func(ctx context.Context, d Deps, in DocRenameInput) (DocRenameOutput, error) {
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
	refs, err := d.Store.List(ctx, store.Filter{})
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
})

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

// TaskMoveInput is the input of task_move. Give project or area; ids moves
// several tasks.
type TaskMoveInput struct {
	ID      string   `json:"id,omitempty"`
	IDs     []string `json:"ids,omitempty"`
	Project string   `json:"project,omitempty"`
	Area    string   `json:"area,omitempty"`
}

// TaskMoveOutput is the output of task_move.
type TaskMoveOutput struct {
	Moved []string `json:"moved"`
}

// TaskMove points tasks at another goal or area. A task keeps its other links.
var TaskMove = define("task_move", "Move tasks to another goal (project) or area; give project or area, and id or ids", func(ctx context.Context, d Deps, in TaskMoveInput) (TaskMoveOutput, error) {
	ids := slices.Clone(in.IDs)
	if in.ID != "" {
		ids = append(ids, in.ID)
	}
	if len(ids) == 0 {
		return TaskMoveOutput{}, errors.New("task_move: give id or ids")
	}
	if (in.Project == "") == (in.Area == "") {
		return TaskMoveOutput{}, errors.New("task_move: give exactly one of project or area")
	}
	project, err := projectLink(ctx, d.Store, in.Project)
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
			t.Project, t.Area = project, ""
		} else {
			t.Project, t.Area = "", area
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
	line := fmt.Sprintf("- %s %s: %s: %s", d.Store.Now().UTC().Format(time.RFC3339), d.Store.Info().Actor, what, result)
	if src := cleanList(in.Sources); len(src) > 0 {
		line += " (sources: " + strings.Join(src, "; ") + ")"
	}
	if strings.Contains(doc.Body, "\n## Attempts\n") || strings.HasPrefix(doc.Body, "## Attempts\n") {
		doc.Body += "\n" + line
	} else {
		doc.Body = appendSection(doc.Body, "Attempts", line)
	}
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return AttemptAddOutput{}, fmt.Errorf("attempt_add: %w", err)
	}
	return AttemptAddOutput{ID: doc.ID, Rev: saved.Rev}, nil
})

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
}

// DocEditOutput is the output of doc_edit.
type DocEditOutput struct {
	ID  string `json:"id"`
	Rev string `json:"rev"`
}

var journalBlockRe = regexp.MustCompile(`(?s)%% para:begin .*? %%.*?%% para:end .*? %%`)

// ErrStale is an edit made from an old revision.
var ErrStale = errors.New("the document changed since you opened it; open it again and redo the edit")

// DocEdit changes the title, body, or declared fields of a document. It is risk
// review: an agent files a proposal item that names it, and approval runs it.
var DocEdit = define("doc_edit", "Change the title, body, or declared fields of a document (task, goal, area, rfx, meeting, journal, incident, request, person, group, team, role, domain, convention, constraint, policy, exception, interaction, inbox, transcript); not id, type, status, or generated text. A note uses note_edit", func(ctx context.Context, d Deps, in DocEditInput) (DocEditOutput, error) {
	if in.Title == nil && in.Body == nil && len(in.Fields) == 0 {
		return DocEditOutput{}, errors.New("doc_edit: nothing to edit: give title, body, or fields")
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
		return DocEditOutput{}, errors.New("doc_edit: a note is edited with note_edit")
	}
	allowed := paramd.EditableFields(string(typ))
	if allowed == nil {
		return DocEditOutput{}, fmt.Errorf("doc_edit: a %s is not edited here; it has its own ops", typ)
	}
	in.Fields = snakeFields(in.Fields)
	for k := range in.Fields {
		if !slices.ContainsFunc(allowed, func(f paramd.FieldInfo) bool { return f.Name == k }) {
			names := make([]string, len(allowed))
			for i, f := range allowed {
				names[i] = f.Name
			}
			return DocEditOutput{}, fmt.Errorf("doc_edit: %q is not an editable field of a %s; editable: %s", k, typ, strings.Join(names, ", "))
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
	if in.Body != nil {
		doc.Body = strings.TrimSpace(*in.Body)
		if typ == model.TypeJournal && !slices.Equal(journalBlockRe.FindAllString(oldBody, -1), journalBlockRe.FindAllString(doc.Body, -1)) {
			return DocEditOutput{}, errors.New("doc_edit: a journal's generated sections (between %% para:begin and %% para:end) are not edited; change the text outside them")
		}
	}
	if len(in.Fields) > 0 {
		if doc, err = doc.WithFields(in.Fields); err != nil {
			return DocEditOutput{}, fmt.Errorf("doc_edit: %w", err)
		}
	}
	if err := checkDoc(doc); err != nil {
		return DocEditOutput{}, fmt.Errorf("doc_edit: %w", err)
	}
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return DocEditOutput{}, fmt.Errorf("doc_edit: %w", err)
	}
	return DocEditOutput{ID: saved.ID, Rev: saved.Rev}, nil
})
