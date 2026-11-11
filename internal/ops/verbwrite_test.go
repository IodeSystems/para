package ops

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

// TestAddParity: per type, add makes the document the type's add op makes,
// with the same receipts (named for the type's op).
func TestAddParity(t *testing.T) {
	cases := []struct {
		name, op, opIn, verbIn string
		extra                  []string // receipt ops after the add
	}{
		{"task", "task_add", `{"project":"%p","title":"Ship it","body":"Notes.","done_when":"Shipped."}`,
			`{"type":"task","title":"Ship it","parent":"%p","body":"Notes.","fields":{"done_when":"Shipped."}}`, nil},
		{"question", "question_add", `{"text":"Which vendor?\n\nCost matters.","project":"%p"}`,
			`{"type":"question","title":"Which vendor?","body":"Cost matters.","parent":"%p"}`, nil},
		{"goal", "project_add", `{"title":"Side goal","done_when":"Done.","owner":"Jordan Example"}`,
			`{"type":"goal","title":"Side goal","fields":{"done_when":"Done.","owner":"Jordan Example"}}`, nil},
		{"area", "area_add", `{"title":"Health","body":"Body."}`, `{"type":"area","title":"Health","body":"Body."}`, nil},
		{"person", "org_person_add", `{"name":"Riley Example","org":"acme.example"}`,
			`{"type":"person","title":"Riley Example","fields":{"org":"acme.example"}}`, nil},
		{"group", "org_group_add", `{"name":"Atlas","org":"acme.example","kind":"team"}`,
			`{"type":"group","title":"Atlas","fields":{"org":"acme.example","kind":"team"}}`, nil},
		{"obligation", "obligation_add", `{"title":"Send the report","owed_to":["%s"],"due":"2026-12-01","project":"%p"}`,
			`{"type":"obligation","title":"Send the report","parent":"%p","fields":{"owed_to":["%s"],"due":"2026-12-01"}}`, nil},
		{"note", "note_add", `{"title":"Recipe","body":"Flour.","project":"%p","tags":["food"]}`,
			`{"type":"note","title":"Recipe","body":"Flour.","parent":"%p","tags":["food"]}`, nil},
		{"ask", "rfx_add", `{"direction":"out","ask":"Review the plan\n\nBy Friday.","route":"domain-logging","project":"%p"}`,
			`{"type":"ask","title":"Review the plan","body":"By Friday.","parent":"%p","fields":{"direction":"out","route":"domain-logging"}}`, nil},
		{"task tags", "task_add", `{"project":"%p","title":"Ship it","done_when":"Shipped."}`,
			`{"type":"task","title":"Ship it","parent":"%p","tags":["socials"],"fields":{"done_when":"Shipped."}}`, []string{"tag_add"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			va, vb := newVerbVault(t), newVerbVault(t)
			sub := func(s string, v *verbVault) json.RawMessage {
				return json.RawMessage(strings.NewReplacer("%p", v.proj, "%s", v.person).Replace(s))
			}
			ctx := writeCtx(t)
			res, err := registry[c.op].Invoke(ctx, va.agent, sub(c.opIn, va))
			if err != nil {
				t.Fatalf("%s: %v", c.op, err)
			}
			ia, _ := asMap(res)["id"].(string)
			if c.extra != nil {
				if _, err := TagAdd.Run(ctx, va.agent, TagInput{IDs: []string{ia}, Tags: []string{"socials"}}); err != nil {
					t.Fatal(err)
				}
			}
			got, err := Add.Invoke(ctx, vb.agent, sub(c.verbIn, vb))
			if err != nil {
				t.Fatalf("add: %v", err)
			}
			out := got.(AddOutput)
			if out.Op != c.op || out.ID == "" {
				t.Fatalf("add ran %s made %q; want %s", out.Op, out.ID, c.op)
			}
			if a, b := sameIDs(res, ia, ia), sameIDs(out.raw, out.ID, out.ID); a != b {
				t.Fatalf("output\nop   %s\nverb %s", a, b)
			}
			da, err := va.agent.Store.Get(t.Context(), ia)
			if err != nil {
				t.Fatal(err)
			}
			db, err := vb.agent.Store.Get(t.Context(), out.ID)
			if err != nil {
				t.Fatal(err)
			}
			if out.Type != da.Meta.Header().Type {
				t.Fatalf("type %s, doc %s", out.Type, da.Meta.Header().Type)
			}
			if a, b := madeID.ReplaceAllString(renderText(da), "ID"), madeID.ReplaceAllString(renderText(db), "ID"); a != b {
				t.Fatalf("doc\nop   %s\nverb %s", a, b)
			}
			ra, rb := receiptOps(t, va.agent, ia), receiptOps(t, vb.agent, out.ID)
			if !reflect.DeepEqual(ra, rb) || len(rb) != 1+len(c.extra) || !strings.HasPrefix(rb[0], c.op+" ") {
				t.Fatalf("receipts\nop   %v\nverb %v", ra, rb)
			}
		})
	}
}

func TestAddRefusals(t *testing.T) {
	v := newVerbVault(t)
	area, err := AreaAdd.Run(t.Context(), v.cli, AreaAddInput{Title: "Health"})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ in, want string }{
		{`{"type":"widget","title":"X"}`, `type "widget": want one of`},
		{`{"type":"task","title":"X","parent":"` + v.proj + `","fields":{"colour":"red"}}`, "fields.colour: task_add takes no colour; its fields: area,"},
		{`{"type":"task","title":"X","fields":{"title":"Y"}}`, "fields.title: add sets title itself"},
		{`{"type":"task","title":"X","parent":"` + v.proj + `","fields":{"project":"` + v.proj + `"}}`, "fields.project: add sets project itself"},
		{`{"type":"person","title":"Riley","body":"Hi."}`, "a person takes no body on add"},
		{`{"type":"area","title":"Sub","parent":"` + area.ID + `"}`, "type area takes no parent"},
		{`{"type":"note","title":"N","parent":"` + v.person + `"}`, "type note takes a parent of type area, project"},
		{`{"type":"task","title":" "}`, "title is empty"},
		{`{"type":"task","title":"X","colour":"red"}`, `unknown field "colour"`},
	}
	for _, c := range cases {
		_, err := Add.Invoke(t.Context(), v.cli, json.RawMessage(c.in))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err %v, want %q", c.in, err, c.want)
		}
	}
}

// TestEditRoutes: edit runs the type's edit op when it takes every field,
// doc_edit for title, body, and declared fields, and refuses a mix.
func TestEditRoutes(t *testing.T) {
	v := newVerbVault(t)
	task := v.task(t, "Send the plan")
	q := v.question(t, "Which vendor?")
	ask := v.rfx(t, "Review the plan")
	note, err := NoteAdd.Run(t.Context(), v.cli, NoteAddInput{Title: "Recipe", Body: "Flour."})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ id, in, op string }{
		{task, `"fields":{"effort":"s"}`, "task_edit"},
		{task, `"title":"Send the final plan"`, "doc_edit"},
		{task, `"fields":{"append":"Sent a draft.","dated":true}`, "doc_edit"},
		{v.proj, `"fields":{"due":"2027-01-15"}`, "project_edit"},
		{q, `"title":"Which vendor now?"`, "question_edit"},
		{note.ID, `"title":"Bread","body":"Flour and water."`, "note_edit"},
		{v.person, `"fields":{"org":"acme.example"}`, "org_person_edit"},
		{ask, `"title":"Review the final plan"`, "doc_edit"},
	}
	for _, c := range cases {
		got, err := Edit.Invoke(t.Context(), v.cli, json.RawMessage(`{"id":"`+c.id+`",`+c.in+`}`))
		if err != nil {
			t.Errorf("%s %s: %v", c.id, c.in, err)
			continue
		}
		if op := got.(VerbOutput).Op; op != c.op {
			t.Errorf("%s %s: ran %s, want %s", c.id, c.in, op, c.op)
		}
	}
	doc, _ := v.cli.Store.Get(t.Context(), task)
	if doc.Title != "Send the final plan" || doc.Meta.(*model.Task).Effort != "s" || !strings.Contains(doc.Body, "Sent a draft.") {
		t.Fatalf("task after edits: %q effort %q body %q", doc.Title, doc.Meta.(*model.Task).Effort, doc.Body)
	}
	for _, c := range []struct{ in, want string }{
		{`{"id":"` + task + `","title":"T","fields":{"effort":"s"}}`, "give the two in separate calls"},
		{`{"id":"` + note.ID + `","fields":{"append":"x"}}`, "note_edit takes body, tags, title"},
		{`{"id":"` + task + `"}`, "nothing to edit"},
		{`{"id":"` + task + `","fields":{"title":"T"}}`, "give title beside fields"},
	} {
		if _, err := Edit.Invoke(t.Context(), v.cli, json.RawMessage(c.in)); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err %v, want %q", c.in, err, c.want)
		}
	}
}

// TestEditParity: edit and task_edit leave the same document and receipts.
func TestEditParity(t *testing.T) {
	va, vb := newVerbVault(t), newVerbVault(t)
	ia, ib := va.task(t, "Send the plan"), vb.task(t, "Send the plan")
	ctx := writeCtx(t)
	if _, err := TaskEdit.Invoke(ctx, va.agent, json.RawMessage(`{"id":"`+ia+`","effort":"s","done_when":"Sent."}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := Edit.Invoke(ctx, vb.agent, json.RawMessage(`{"id":"`+ib+`","fields":{"effort":"s","done_when":"Sent."}}`)); err != nil {
		t.Fatal(err)
	}
	da, _ := va.agent.Store.Get(t.Context(), ia)
	db, _ := vb.agent.Store.Get(t.Context(), ib)
	if a, b := madeID.ReplaceAllString(renderText(da), "ID"), madeID.ReplaceAllString(renderText(db), "ID"); a != b {
		t.Fatalf("doc\nop   %s\nverb %s", a, b)
	}
	if ra, rb := receiptOps(t, va.agent, ia), receiptOps(t, vb.agent, ib); !reflect.DeepEqual(ra, rb) || !slices.Contains(rb, "task_edit claude mcp") {
		t.Fatalf("receipts\nop   %v\nverb %v", ra, rb)
	}
}

func TestLinkRels(t *testing.T) {
	v := newVerbVault(t)
	a, b := v.task(t, "Draft"), v.task(t, "Review")
	q := v.question(t, "Which vendor?")
	ask := v.rfx(t, "Review the plan")
	steps := []struct{ in, op string }{
		{`{"from":"` + b + `","to":"` + a + `"}`, "task_depends_add"},
		{`{"from":"` + b + `","to":"` + a + `","remove":true}`, "task_depends_remove"},
		{`{"from":"` + q + `","to":"` + a + `","rel":"depends_on"}`, "question_depends_add"},
		{`{"from":"` + q + `","to":"` + a + `","remove":true}`, "question_depends_remove"},
		{`{"from":"` + ask + `","to":"` + a + `"}`, "depends_set"},
		{`{"from":"` + v.proj + `","to":"https://wiki.acme.example/plan","label":"Plan","kind":"plan"}`, "project_link_add"},
		{`{"from":"` + v.proj + `","to":"https://wiki.acme.example/plan","remove":true}`, "project_link_remove"},
		{`{"from":"` + b + `","to":"` + a + `","rel":"part_of"}`, "task_edit"},
	}
	for _, s := range steps {
		got, err := Link.Invoke(t.Context(), v.cli, json.RawMessage(s.in))
		if err != nil {
			t.Fatalf("%s: %v", s.in, err)
		}
		if op := got.(VerbOutput).Op; op != s.op {
			t.Fatalf("%s: ran %s, want %s", s.in, op, s.op)
		}
	}
	doc, _ := v.cli.Store.Get(t.Context(), b)
	if m := doc.Meta.(*model.Task); m.Parent.Target() != a || len(m.DependsOn) != 0 {
		t.Fatalf("task: parent %q depends %v", m.Parent, m.DependsOn)
	}
	doc, _ = v.cli.Store.Get(t.Context(), ask)
	if m := doc.Meta.(*model.RFX); len(m.DependsOn) != 1 {
		t.Fatalf("ask depends %v", m.DependsOn)
	}
	if _, err := Link.Invoke(t.Context(), v.cli, json.RawMessage(`{"from":"`+b+`","rel":"part_of","remove":true}`)); err != nil {
		t.Fatal(err)
	}
	if doc, _ = v.cli.Store.Get(t.Context(), b); doc.Meta.(*model.Task).Parent != "" {
		t.Fatal("parent not cleared")
	}
	for _, c := range []struct{ in, want string }{
		{`{"from":"` + v.proj + `","to":"` + a + `","rel":"depends_on"}`, "rel depends_on links from a task, question, obligation, or rfx"},
		{`{"from":"` + a + `","to":"https://x.example","rel":"link"}`, "rel link links from an area, a goal, or an incident"},
		{`{"from":"` + a + `","to":"` + b + `","rel":"next"}`, `rel "next": want one of`},
		{`{"from":"` + a + `","to":"` + b + `","label":"L"}`, "label and kind are a link's"},
		{`{"from":"` + a + `"}`, "to is empty"},
	} {
		if _, err := Link.Invoke(t.Context(), v.cli, json.RawMessage(c.in)); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err %v, want %q", c.in, err, c.want)
		}
	}
}

func TestTagVerb(t *testing.T) {
	v := newVerbVault(t)
	a := v.task(t, "Draft")
	ran := func(in string) []string {
		t.Helper()
		got, err := Tag.Invoke(t.Context(), v.cli, json.RawMessage(in))
		if err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		var ops []string
		for _, r := range got.(TagVerbOutput).Ran {
			ops = append(ops, r.Op)
		}
		return ops
	}
	tags := func() []string {
		doc, _ := v.cli.Store.Get(t.Context(), a)
		return doc.Meta.Header().Tags
	}
	if got := ran(`{"ids":["` + a + `"],"add":["socials","ops"]}`); !reflect.DeepEqual(got, []string{"tag_add"}) || len(tags()) != 2 {
		t.Fatalf("add: ran %v, tags %v", got, tags())
	}
	if got := ran(`{"ids":["` + a + `"],"add":["legal"],"remove":["ops"]}`); !reflect.DeepEqual(got, []string{"tag_add", "tag_remove"}) {
		t.Fatalf("both: ran %v", got)
	}
	if got := ran(`{"ids":["` + a + `"],"remove":["socials"]}`); !reflect.DeepEqual(got, []string{"tag_remove"}) || !reflect.DeepEqual(tags(), []string{"legal"}) {
		t.Fatalf("remove: ran %v, tags %v", got, tags())
	}
	if _, err := Tag.Invoke(t.Context(), v.cli, json.RawMessage(`{"ids":["`+a+`"]}`)); err == nil {
		t.Fatal("tag with no add or remove ran")
	}
}

// writeVerbTargets are the ops each write verb may run.
var writeVerbTargets = map[string][]Entry{
	"add":  {TaskAdd, QuestionAdd, ProjectAdd, AreaAdd, OrgPersonAdd, OrgGroupAdd, ObligationAdd, NoteAdd, RFXAdd, IOAdd, TagAdd},
	"edit": {TaskEdit, ProjectEdit, QuestionEdit, NoteEdit, OrgPersonEdit, IOEdit, DocEdit, NotesEdit},
	"link": {TaskDependsAdd, TaskDependsRemove, QuestionDependsAdd, QuestionDependsRemove, DependsSet, AreaLinkAdd, AreaLinkRemove,
		ProjectLinkAdd, ProjectLinkRemove, IncidentLinkAdd, IncidentLinkRemove, TaskEdit, IOFile},
	"tag":  {TagAdd, TagRemove},
	"work": {RequestNext, RequestNote, RequestDone, RequestFail, RequestAdd},
	"undo": {ReceiptUndo},
	// close's routes for an answer, a reply, and an agent's send (io_done
	// stays the person's route).
	"close": {QuestionAnswer, RFXReply, IOSendCheck, IOSend},
}

var riskRank = map[Risk]int{RiskRead: 0, RiskSafe: 1, RiskReview: 2}

// TestWriteVerbClass: a write verb is a verb (it files no receipt and runs no
// gate itself), its class is at least each target's, and the scopes of the
// caller (Authorize) apply to the op it routes to: a verb widens nothing.
func TestWriteVerbClass(t *testing.T) {
	for name, targets := range writeVerbTargets {
		e := registry[name]
		if !e.(interface{ isVerb() bool }).isVerb() || e.PersonOnly() {
			t.Errorf("%s: not a verb, or person only", name)
		}
		if n := len(e.Description()); name != "close" && n > 120 {
			t.Errorf("%s: description of %d chars, want at most 120", name, n)
		}
		for _, op := range targets {
			if riskRank[op.Risk()] > riskRank[e.Risk()] {
				t.Errorf("%s is %s, its target %s is %s", name, e.Risk(), op.Name(), op.Risk())
			}
			if op.PersonOnly() {
				t.Errorf("%s routes to person-only %s: add a refusal test", name, op.Name())
			}
		}
	}
	v := newVerbVault(t)
	task := v.task(t, "Draft")
	agent := v.agent
	agent.Authorize = func(op string) error {
		if op == "task_add" || op == "tag_remove" || op == "task_depends_add" || op == "doc_edit" {
			return ErrPersonOnly
		}
		return nil
	}
	ctx := writeCtx(t)
	calls := []struct {
		op Entry
		in string
	}{
		{Add, `{"type":"task","title":"Refused","parent":"` + v.proj + `","fields":{"done_when":"x"}}`},
		{Tag, `{"ids":["` + task + `"],"remove":["socials"]}`},
		{Link, `{"from":"` + task + `","to":"` + v.question(t, "Which?") + `"}`},
		{Edit, `{"id":"` + task + `","title":"Refused"}`},
	}
	for _, c := range calls {
		if _, err := c.op.Invoke(ctx, agent, json.RawMessage(c.in)); !errors.Is(err, ErrPersonOnly) {
			t.Errorf("%s %s: err %v, want the target's refusal", c.op.Name(), c.in, err)
		}
	}
	if doc, _ := v.cli.Store.Get(t.Context(), task); doc.Title != "Draft" {
		t.Fatal("a refused edit wrote")
	}
}

func (o *Op[I, O]) isVerb() bool { return o.verb }
