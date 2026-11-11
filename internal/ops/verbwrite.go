package ops

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/iodesystems/para/internal/model"
)

// The generic write verbs: add, edit, link, and tag. Each routes to the op of
// the item's type (task_add, task_edit, task_depends_add, tag_add, ...) and
// runs it as it runs alone: the gate, the person check, the scopes, and the
// receipt are that op's own, and the receipt names that op. The per-type ops
// stay registered for the CLI, the server, and GraphQL.

// --- add ---

// AddType is the type of item add makes. goal is project; ask is rfx.
type AddType string

// AddTypes lists every AddType.
var AddTypes = []AddType{"task", "question", "project", "goal", "area", "person", "group", "obligation", "note", "rfx", "ask", "io"}

func (AddType) Enum() []string { return enumStrs(AddTypes) }

// AddInput is the input of add. Fields are the other inputs of the type's add
// op (ops_describe that op for them).
type AddInput struct {
	Type   AddType        `json:"type"`
	Title  string         `json:"title"`
	Parent string         `json:"parent,omitempty"`
	Body   string         `json:"body,omitempty"`
	Tags   []string       `json:"tags,omitempty"`
	Fields map[string]any `json:"fields,omitempty"`
}

// AddOutput is what add made: the op that ran, the new item's type and id,
// that op's output, and the tag_add that tagged it (a type whose add op
// takes no tags).
type AddOutput struct {
	Op     string         `json:"op"`
	Type   model.Type     `json:"type"`
	ID     string         `json:"id"`
	Result map[string]any `json:"result"`
	Tagged *VerbOutput    `json:"tagged,omitempty"`
	raw    any
}

// addRoute is how add fills the add op of one type.
type addRoute struct {
	op    Entry
	typ   model.Type
	title string // the input field of the title
	// body is the input field of the body; "" refuses a body, and join puts
	// it under the title in the title's field (a question's text, an ask).
	body string
	tags bool // the op takes tags itself
	// parents is the input field a parent of each type fills.
	parents map[model.Type]string
}

const addJoinBody = "join"

func addRoutes() map[AddType]addRoute {
	task := addRoute{op: TaskAdd, typ: model.TypeTask, title: "title", body: "body", parents: map[model.Type]string{
		model.TypeProject: "project", model.TypeArea: "area", model.TypeTask: "parent", model.TypeObligation: "parent", model.TypeIncident: "incident"}}
	project := addRoute{op: ProjectAdd, typ: model.TypeProject, title: "title", body: "body", parents: map[model.Type]string{model.TypeArea: "area"}}
	rfx := addRoute{op: RFXAdd, typ: model.TypeRFX, title: "ask", body: addJoinBody, parents: map[model.Type]string{
		model.TypeProject: "project", model.TypeTask: "parent", model.TypeObligation: "parent"}}
	return map[AddType]addRoute{
		"task": task,
		"question": {op: QuestionAdd, typ: model.TypeQuestion, title: "text", body: addJoinBody, parents: map[model.Type]string{
			model.TypeProject: "project", model.TypeArea: "area", model.TypeTask: "task"}},
		"project": project, "goal": project,
		"area":   {op: AreaAdd, typ: model.TypeArea, title: "title", body: "body"},
		"person": {op: OrgPersonAdd, typ: model.TypePerson, title: "name"},
		"group":  {op: OrgGroupAdd, typ: model.TypeGroup, title: "name"},
		"obligation": {op: ObligationAdd, typ: model.TypeObligation, title: "title", body: "body", parents: map[model.Type]string{
			model.TypeProject: "project", model.TypeArea: "area", model.TypeTask: "parent", model.TypeObligation: "parent"}},
		"note": {op: NoteAdd, typ: model.TypeNote, title: "title", body: "body", tags: true, parents: map[model.Type]string{
			model.TypeProject: "project", model.TypeArea: "area"}},
		"rfx": rfx, "ask": rfx,
		// An io item's title is the first line of its text; parent is the
		// item it answers or acts on (thread).
		"io": {op: IOAdd, typ: model.TypeIO, title: "text", body: addJoinBody, tags: true, parents: map[model.Type]string{
			model.TypeIO: "thread", model.TypeInteraction: "thread", model.TypeRFX: "thread", model.TypeQuestion: "thread"}},
	}
}

// Add makes an item of any type by that type's add op.
var Add = func() *Op[AddInput, AddOutput] {
	op := define("add", "Add an item of type. parent sets its project, area, or parent; do not repeat it in fields. A task needs fields.done_when", runAdd)
	// The type's add op gates and files the receipt; add does neither.
	op.verb = true
	return op
}()

func runAdd(ctx context.Context, d Deps, in AddInput) (AddOutput, error) {
	ar, rt, err := addRouteFor(ctx, d, in)
	if err != nil {
		return AddOutput{}, err
	}
	vo, err := runRoute(ctx, d, rt)
	out := AddOutput{Op: vo.Op, Type: ar.typ, Result: vo.Result, raw: vo.raw}
	out.ID, _ = vo.Result["id"].(string)
	if t, ok := vo.Result["type"].(string); ok && t != "" {
		out.Type = model.Type(t) // an obligation is a task of its kind at vault format 10
	}
	if err != nil || ar.tags || len(in.Tags) == 0 {
		return out, err
	}
	tr, err := newRoute(TagAdd, TagInput{IDs: []string{out.ID}, Tags: in.Tags}, false)
	if err != nil {
		return out, err
	}
	tagged, err := runRoute(ctx, d, tr)
	out.Tagged = &tagged
	if err != nil {
		return out, fmt.Errorf("%s made %s, but tag_add failed: %w", out.Op, out.ID, err)
	}
	return out, nil
}

// addRouteFor is the add op of in.Type and its input.
func addRouteFor(ctx context.Context, d Deps, in AddInput) (addRoute, verbRoute, error) {
	ar, ok := addRoutes()[AddType(strings.TrimSpace(string(in.Type)))]
	if !ok {
		return addRoute{}, verbRoute{}, fmt.Errorf("type %q: want one of %v", in.Type, AddTypes)
	}
	base := map[string]any{}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return ar, verbRoute{}, errors.New("title is empty")
	}
	base[ar.title] = title
	if strings.TrimSpace(in.Body) != "" {
		switch ar.body {
		case "":
			return ar, verbRoute{}, fmt.Errorf("a %s takes no body on add: add it, then edit {id, body}", ar.typ)
		case addJoinBody:
			base[ar.title] = title + "\n\n" + in.Body
		default:
			base[ar.body] = in.Body
		}
	}
	if ar.tags && len(in.Tags) > 0 {
		base["tags"] = in.Tags
	}
	if p := normID(in.Parent); p != "" {
		doc, err := d.Store.Get(ctx, p)
		if err != nil {
			return ar, verbRoute{}, linkErr("parent", p, err)
		}
		pt := incidentShape(doc).Meta.Header().Type // a task of kind incident is an incident parent
		field, ok := ar.parents[pt]
		if !ok {
			return ar, verbRoute{}, fmt.Errorf("parent %s is a %s; type %s takes %s", doc.ID, typeWord(doc), in.Type, parentWords(ar))
		}
		base[field] = doc.ID
	}
	allowed := inputFieldNames(ar.op.InputType())
	for k, v := range in.Fields {
		if _, taken := base[k]; taken || k == ar.title || k == ar.body {
			return ar, verbRoute{}, fmt.Errorf("fields.%s: add sets %s itself (from title, body, tags, or parent)", k, k)
		}
		if !allowed[k] {
			return ar, verbRoute{}, fmt.Errorf("fields.%s: %s takes no %s; its fields: %s", k, ar.op.Name(), k, strings.Join(addFieldNames(ar, allowed), ", "))
		}
		base[k] = v
	}
	rt, err := newRoute(ar.op, base, false)
	return ar, rt, err
}

// parentWords lists the parent types of a route, or none.
func parentWords(ar addRoute) string {
	if len(ar.parents) == 0 {
		return "no parent"
	}
	ts := make([]string, 0, len(ar.parents))
	for t := range ar.parents {
		ts = append(ts, string(t))
	}
	slices.Sort(ts)
	return "a parent of type " + strings.Join(ts, ", ")
}

// addFieldNames are the fields of a route's op that add does not set itself.
func addFieldNames(ar addRoute, allowed map[string]bool) []string {
	out := []string{}
	for _, k := range slices.Sorted(maps.Keys(allowed)) {
		if k != ar.title && k != ar.body && (!ar.tags || k != "tags") {
			out = append(out, k)
		}
	}
	return out
}

// --- edit ---

// EditInput is the input of edit. Fields are the type's edit op inputs, or
// doc_edit's (append, section, a declared field).
type EditInput struct {
	ID     string         `json:"id"`
	Title  *string        `json:"title,omitempty"`
	Body   *string        `json:"body,omitempty"`
	Fields map[string]any `json:"fields,omitempty"`
	// Notes replaces the item's ## Notes against NotesHash, the hash show
	// gave (notes_edit); empty removes them. Alone, with no other change.
	Notes     *string `json:"notes,omitempty"`
	NotesHash string  `json:"notes_hash,omitempty"`
}

// editOps is the edit op of each type that has one. Others edit by doc_edit.
func editOps() map[model.Type]Entry {
	return map[model.Type]Entry{
		model.TypeTask: TaskEdit, model.TypeProject: ProjectEdit, model.TypeQuestion: QuestionEdit,
		model.TypeNote: NoteEdit, model.TypePerson: OrgPersonEdit, model.TypeIO: IOEdit,
	}
}

// Edit changes any item by its type's edit op, or doc_edit.
var Edit = defineVerb("edit", "Edit any item: title, body, fields (type's edit op, else doc_edit; message read: state read), or notes with notes_hash",
	RiskReview, editRoute)

func editRoute(ctx context.Context, d Deps, in EditInput) (verbRoute, error) {
	id := normID(in.ID)
	if id == "" {
		return verbRoute{}, errors.New("id is empty")
	}
	doc, err := d.Store.Get(ctx, id)
	if err != nil {
		return verbRoute{}, linkErr("id", id, err)
	}
	if in.Notes != nil {
		if in.Title != nil || in.Body != nil || len(in.Fields) > 0 {
			return verbRoute{}, errors.New("notes go alone: edit title, body, or fields in another call")
		}
		return newRoute(NotesEdit, NotesEditInput{ID: doc.ID, Hash: in.NotesHash, MD: *in.Notes}, false)
	}
	if in.NotesHash != "" {
		return verbRoute{}, errors.New("notes_hash goes with notes")
	}
	keys := slices.Sorted(maps.Keys(in.Fields))
	for _, k := range keys {
		if k == "id" || k == "title" || k == "body" {
			return verbRoute{}, fmt.Errorf("fields.%s: give %s beside fields, not in them", k, k)
		}
	}
	all := slices.Clone(keys)
	if in.Title != nil {
		all = append(all, "title")
	}
	if in.Body != nil {
		all = append(all, "body")
	}
	if len(all) == 0 {
		return verbRoute{}, errors.New("nothing to edit: give title, body, or fields")
	}
	base := map[string]any{"id": doc.ID}
	if in.Title != nil {
		base["title"] = *in.Title
	}
	if in.Body != nil {
		base["body"] = *in.Body
	}
	typ := doc.Meta.Header().Type
	if op, ok := editOps()[typ]; ok {
		names := inputFieldNames(op.InputType())
		var own []string // fields only the type's op takes
		fits := true
		for _, k := range all {
			switch {
			case !names[k]:
				fits = false
			case k != "title" && k != "body":
				own = append(own, k)
			}
		}
		if fits {
			maps.Copy(base, in.Fields)
			return newRoute(op, base, false)
		}
		if typ == model.TypeNote || len(own) > 0 {
			return verbRoute{}, fmt.Errorf("%s: %s takes %s; doc_edit takes title, body, append, dated, section, section_body, or a declared field: give the two in separate calls",
				doc.ID, op.Name(), strings.Join(slices.DeleteFunc(slices.Sorted(maps.Keys(names)), func(k string) bool { return k == "id" }), ", "))
		}
	}
	docTop := inputFieldNames(DocEdit.InputType())
	declared := map[string]any{}
	for k, v := range in.Fields {
		if docTop[k] && k != "fields" {
			base[k] = v
		} else {
			declared[k] = v
		}
	}
	if len(declared) > 0 {
		base["fields"] = declared
	}
	return newRoute(DocEdit, base, false)
}

// --- link ---

// LinkRel is a kind of link between two items, or an item and a URL.
type LinkRel string

// The LinkRels: depends_on (from waits on to), link (an area, goal, or
// incident points at a URL or path), part_of (a task is an action item of a
// task or obligation), became (an incoming io item is filed as the existing
// item to: io_file existing), and member_of (a person is in the group to).
const (
	RelDependsOn LinkRel = "depends_on"
	RelLink      LinkRel = "link"
	RelPartOf    LinkRel = "part_of"
	RelBecame    LinkRel = "became"
	RelMemberOf  LinkRel = "member_of"
)

// LinkRels lists every LinkRel.
var LinkRels = []LinkRel{RelDependsOn, RelLink, RelPartOf, RelBecame, RelMemberOf}

func (LinkRel) Enum() []string { return enumStrs(LinkRels) }

// LinkInput is the input of link. Label and kind are a link's (rel link).
type LinkInput struct {
	From   string            `json:"from"`
	To     string            `json:"to,omitempty"`
	Rel    LinkRel           `json:"rel,omitempty"`
	Remove bool              `json:"remove,omitempty"`
	Label  string            `json:"label,omitempty"`
	Kind   model.PointerKind `json:"kind,omitempty"`
}

// Link links two items, or an item and a URL, by the op of from's type.
var Link = defineVerb("link", "Link. depends_on: waits on to; link: area/goal/incident URL; part_of: task under to; became: io filed; member_of: group",
	RiskSafe, linkRoute)

func linkRoute(ctx context.Context, d Deps, in LinkInput) (verbRoute, error) {
	from := normID(in.From)
	if from == "" {
		return verbRoute{}, errors.New("from is empty")
	}
	doc, err := d.Store.Get(ctx, from)
	if err != nil {
		return verbRoute{}, linkErr("from", from, err)
	}
	typ := incidentShape(doc).Meta.Header().Type
	rel := in.Rel
	if rel == "" {
		rel = RelDependsOn
		switch typ {
		case model.TypeArea, model.TypeProject, model.TypeIncident:
			rel = RelLink
		case model.TypeIO:
			rel = RelBecame
		case model.TypePerson:
			rel = RelMemberOf
		}
	}
	if rel != RelLink && (in.Label != "" || in.Kind != "") {
		return verbRoute{}, fmt.Errorf("label and kind are a link's (rel link), not %s", rel)
	}
	to := strings.TrimSpace(in.To)
	if rel != RelLink {
		to = normID(to)
	}
	clearing := in.Remove && (rel == RelPartOf || rel == RelLink && in.Label != "")
	if to == "" && !clearing {
		return verbRoute{}, errors.New("to is empty")
	}
	bad := func(want string) error {
		return fmt.Errorf("%s is a %s: rel %s links from %s", doc.ID, typeWord(doc), rel, want)
	}
	switch rel {
	case RelDependsOn:
		switch typ {
		case model.TypeTask:
			return newRoute(pick(in.Remove, TaskDependsRemove, TaskDependsAdd), TaskDependsInput{ID: doc.ID, DependsOn: []string{to}}, false)
		case model.TypeQuestion:
			return newRoute(pick(in.Remove, QuestionDependsRemove, QuestionDependsAdd), QuestionDependsInput{ID: doc.ID, DependsOn: []string{to}}, false)
		case model.TypeObligation, model.TypeRFX:
			return newRoute(DependsSet, DependsSetInput{ID: doc.ID, On: to, Remove: in.Remove}, false)
		}
		return verbRoute{}, bad("a task, question, obligation, or rfx")
	case RelLink:
		var add, rm Entry
		switch typ {
		case model.TypeArea:
			add, rm = AreaLinkAdd, AreaLinkRemove
		case model.TypeProject:
			add, rm = ProjectLinkAdd, ProjectLinkRemove
		case model.TypeIncident:
			add, rm = IncidentLinkAdd, IncidentLinkRemove
		default:
			return verbRoute{}, bad("an area, a goal, or an incident")
		}
		if !in.Remove {
			return newRoute(add, LinkAddInput{ID: doc.ID, Label: cmp.Or(in.Label, to), Kind: cmp.Or(in.Kind, model.PointerOther), Target: to}, false)
		}
		label := in.Label
		if label == "" {
			label, err = linkLabel(doc, to)
			if err != nil {
				return verbRoute{}, err
			}
		}
		return newRoute(rm, LinkRemoveInput{ID: doc.ID, Label: label}, false)
	case RelBecame:
		if typ != model.TypeIO {
			return verbRoute{}, bad("an incoming io item")
		}
		if in.Remove {
			return verbRoute{}, fmt.Errorf("%s: a filed io item is not unfiled by link: undo its io_file receipt", doc.ID)
		}
		return newRoute(IOFile, IOFileInput{ID: doc.ID, Existing: to}, false)
	case RelPartOf:
		if typ != model.TypeTask {
			return verbRoute{}, bad("a task")
		}
		if in.Remove {
			return newRoute(TaskEdit, TaskEditInput{ID: doc.ID, ClearParent: true}, false)
		}
		return newRoute(TaskEdit, TaskEditInput{ID: doc.ID, Parent: to}, false)
	case RelMemberOf:
		if typ != model.TypePerson {
			return verbRoute{}, bad("a person")
		}
		return newRoute(pick(in.Remove, OrgPersonGroupRemove, OrgPersonGroupAdd), PersonGroupInput{ID: doc.ID, Group: to}, false)
	}
	return verbRoute{}, fmt.Errorf("rel %q: want one of %v", rel, LinkRels)
}

func pick(remove bool, rm, add Entry) Entry {
	if remove {
		return rm
	}
	return add
}

// linkLabel is the label of doc's link to target.
func linkLabel(doc model.Document, target string) (string, error) {
	links, err := pointers(doc)
	if err != nil {
		return "", err
	}
	for _, l := range *links {
		if l.Target == target {
			return l.Label, nil
		}
	}
	return "", fmt.Errorf("%s has no link to %s: give its label", doc.ID, target)
}

// --- tag ---

// TagVerbInput is the input of tag: tags to add, to remove, or both.
type TagVerbInput struct {
	IDs    []string `json:"ids"`
	Add    []string `json:"add,omitempty"`
	Remove []string `json:"remove,omitempty"`
}

// TagVerbOutput is what tag ran: tag_add, tag_remove, or both, in order.
type TagVerbOutput struct {
	Ran []VerbOutput `json:"ran"`
}

// Tag tags items, or untags them, by tag_add and tag_remove.
var Tag = func() *Op[TagVerbInput, TagVerbOutput] {
	op := define("tag", "Tag items with topics (add), or untag them (remove): runs tag_add, tag_remove", runTag)
	op.risk, op.verb = RiskSafe, true
	return op
}()

func runTag(ctx context.Context, d Deps, in TagVerbInput) (TagVerbOutput, error) {
	out := TagVerbOutput{Ran: []VerbOutput{}}
	if len(in.IDs) == 0 {
		return out, errors.New("ids is empty")
	}
	if len(in.Add) == 0 && len(in.Remove) == 0 {
		return out, errors.New("nothing to tag: give add, remove, or both")
	}
	for _, step := range []struct {
		op   Entry
		tags []string
	}{{TagAdd, in.Add}, {TagRemove, in.Remove}} {
		if len(step.tags) == 0 {
			continue
		}
		rt, err := newRoute(step.op, TagInput{IDs: in.IDs, Tags: step.tags}, false)
		if err != nil {
			return out, err
		}
		vo, err := runRoute(ctx, d, rt)
		out.Ran = append(out.Ran, vo)
		if err != nil {
			return out, err
		}
	}
	return out, nil
}

// --- work ---

// WorkAction is what work does with agent work.
type WorkAction string

// WorkActions lists every WorkAction.
var WorkActions = []WorkAction{"next", "note", "done", "fail", "ask"}

func (WorkAction) Enum() []string { return enumStrs(WorkActions) }

// WorkInput is the input of work. Each action takes its own fields
// (workFields).
type WorkInput struct {
	Action       WorkAction              `json:"action"`
	ID           string                  `json:"id,omitempty"`
	Text         string                  `json:"text,omitempty"`
	LeaseSeconds int                     `json:"lease_seconds,omitempty"`
	Resolution   model.RequestResolution `json:"resolution,omitempty"`
	Summary      string                  `json:"summary,omitempty"`
	Links        []string                `json:"links,omitempty"`
	Followed     []string                `json:"followed,omitempty"`
	Reply        string                  `json:"reply,omitempty"`
	Reason       string                  `json:"reason,omitempty"`
	Title        string                  `json:"title,omitempty"`
	Context      []string                `json:"context,omitempty"`
	Kind         string                  `json:"kind,omitempty"`
	OwnerSaid    string                  `json:"owner_said,omitempty"`
}

// workFields are the fields each action takes, beside action.
var workFields = map[WorkAction][]string{
	"next": {"lease_seconds"},
	"note": {"id", "text", "lease_seconds"},
	"done": {"id", "resolution", "summary", "links", "followed", "reply"},
	"fail": {"id", "reason", "reply"},
	"ask":  {"title", "text", "links", "context", "kind", "owner_said"},
}

// Work claims, notes, ends, or asks for agent work by the request ops.
var Work = defineVerb("work", "Agent work: next (claim), note (id, text), done (id, resolution, summary, reply), fail (id, reason), ask (text)",
	RiskSafe, workRoute)

func workRoute(_ context.Context, _ Deps, in WorkInput) (verbRoute, error) {
	allowed, ok := workFields[in.Action]
	if !ok {
		return verbRoute{}, fmt.Errorf("action %q: want one of %v", in.Action, WorkActions)
	}
	for _, k := range slices.Sorted(maps.Keys(asMap(in))) {
		if k != "action" && !slices.Contains(allowed, k) {
			return verbRoute{}, fmt.Errorf("action %s takes no %s; it takes %s", in.Action, k, strings.Join(allowed, ", "))
		}
	}
	switch in.Action {
	case "next":
		return newRoute(RequestNext, RequestNextInput{LeaseSeconds: in.LeaseSeconds}, false)
	case "note":
		return newRoute(RequestNote, RequestNoteInput{ID: in.ID, Text: in.Text, LeaseSeconds: in.LeaseSeconds}, false)
	case "done":
		return newRoute(RequestDone, RequestDoneInput{ID: in.ID, Resolution: in.Resolution, Result: in.Summary, Links: in.Links, Followed: in.Followed, Reply: in.Reply}, false)
	case "fail":
		return newRoute(RequestFail, RequestFailInput{ID: in.ID, Reason: in.Reason, Reply: in.Reply}, false)
	}
	return newRoute(RequestAdd, RequestAddInput{Title: in.Title, Ask: in.Text, Links: in.Links, Context: in.Context, Kind: in.Kind, OwnerSaid: in.OwnerSaid}, false)
}

// --- undo ---

// UndoInput is the input of undo: one receipt or several, newest first.
type UndoInput struct {
	Receipt  string   `json:"receipt,omitempty"`
	Receipts []string `json:"receipts,omitempty"`
}

// Undo undoes writes from their receipts: receipt_undo.
var Undo = defineVerb("undo", "Undo writes from their receipts (receipt_undo): receipt or receipts, newest first; refused if the item changed since",
	RiskSafe, func(_ context.Context, _ Deps, in UndoInput) (verbRoute, error) {
		if in.Receipt == "" && len(in.Receipts) == 0 {
			return verbRoute{}, errors.New("give receipt or receipts")
		}
		return newRoute(ReceiptUndo, ReceiptUndoInput{ID: in.Receipt, IDs: in.Receipts}, false)
	})
