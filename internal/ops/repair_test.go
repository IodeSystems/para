package ops

import (
	"errors"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/paramd"
)

func TestRepairOpsRunFromAnAgent(t *testing.T) {
	f := newAgentFix(t)
	ctx := writeCtx(t)
	s := "Renamed goal"
	if _, err := DocEdit.Run(ctx, f.agent, DocEditInput{ID: f.proj, Title: &s}); err != nil {
		t.Fatalf("doc_edit from an agent: %v", err)
	}
	if rl := must(t, ReceiptList, f.owner, ReceiptListInput{Doc: f.proj}); len(rl.Items) != 1 || rl.Items[0].Op != "doc_edit" {
		t.Fatalf("receipts = %+v", rl.Items)
	}
}

func TestDocRenameRewritesLinks(t *testing.T) {
	f := newAgentFix(t)
	d := f.owner
	other, err := NoteAdd.Run(ctx, d, NoteAddInput{Title: "Other", Body: "See [[" + f.source + "]] and [[" + f.source + "#Heading|the sync]] and [[" + f.proj + "]]."})
	if err != nil {
		t.Fatal(err)
	}
	tsk, err := TaskAdd.Run(ctx, d, TaskAddInput{Goal: f.proj, Title: "Uses it", DependsOn: nil})
	if err != nil {
		t.Fatal(err)
	}
	_ = tsk
	out, err := DocRename.Run(ctx, d, DocRenameInput{ID: f.source, NewID: "note-renamed"})
	if err != nil {
		t.Fatal(err)
	}
	if out.ID != "note-renamed" || len(out.Rewritten) != 1 || out.Rewritten[0] != other.ID {
		t.Fatalf("out = %+v", out)
	}
	got, _ := d.Store.Get(ctx, other.ID)
	if !strings.Contains(got.Body, "[[note-renamed]]") || !strings.Contains(got.Body, "[[note-renamed#Heading|the sync]]") ||
		strings.Contains(got.Body, f.source) || !strings.Contains(got.Body, "[["+f.proj+"]]") {
		t.Fatalf("body = %q", got.Body)
	}
	if _, err := d.Store.Get(ctx, f.source); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("old id still there: %v", err)
	}
	// A collision is refused, and nothing changes.
	if _, err := DocRename.Run(ctx, d, DocRenameInput{ID: other.ID, NewID: "note-renamed"}); !errors.Is(err, model.ErrExists) {
		t.Fatalf("collision: %v", err)
	}
	if _, err := DocRename.Run(ctx, d, DocRenameInput{ID: other.ID, NewID: "NOTE-Renamed"}); err == nil {
		t.Fatalf("an upper-case id was accepted")
	}
	mustLintClean(t, d)
}

func TestDocRetypeKeepsExtras(t *testing.T) {
	f := newAgentFix(t)
	d := f.owner
	n, _ := NoteAdd.Run(ctx, d, NoteAddInput{Title: "Logging rule", Body: "Text.", Area: ""})
	// Missing required fields: refused.
	if _, err := DocRetype.Run(ctx, d, DocRetypeInput{ID: n.ID, Type: model.TypePolicyException}); err == nil || !strings.Contains(err.Error(), "lint") {
		t.Fatalf("missing fields: %v", err)
	}
	doc, _ := d.Store.Get(ctx, n.ID)
	doc.Meta.(*model.Note).Goal = model.LinkTo(f.proj)
	if _, err := d.Store.Update(ctx, doc); err != nil {
		t.Fatal(err)
	}
	out, err := DocRetype.Run(ctx, d, DocRetypeInput{ID: n.ID, Type: model.TypePolicyException, Fields: map[string]any{
		"kind": "established_example", "status": "open", "by": "unknown", "policy_text": "Logging standard"}})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := d.Store.Get(ctx, n.ID)
	e, ok := got.Meta.(*model.PolicyException)
	if !ok || got.Folder != model.FolderPolicies || e.Kind != model.ExceptionEstablishedExample || e.PolicyText != "Logging standard" || got.Body != "Text." {
		t.Fatalf("got %+v folder %s", got.Meta, got.Folder)
	}
	if !slicesHas(out.Kept, "goal") {
		t.Fatalf("kept = %v", out.Kept)
	}
	text, _ := got.FieldText()
	if text["goal"] != "[["+f.proj+"]]" {
		t.Fatalf("project dropped: %v", text)
	}
	mustLintClean(t, d)
	// Person to team.
	p, err := OrgPersonAdd.Run(ctx, d, OrgAddInput{Name: "Pat Example", Org: "acme"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DocRetype.Run(ctx, d, DocRetypeInput{ID: p.ID, Type: model.TypeTeam}); err != nil {
		t.Fatal(err)
	}
	tm, _ := d.Store.Get(ctx, p.ID)
	if _, ok := tm.Meta.(*model.Team); !ok || tm.Folder != model.FolderOrg {
		t.Fatalf("team = %+v", tm)
	}
}

func slicesHas(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func TestTaskMove(t *testing.T) {
	f := newAgentFix(t)
	d := f.owner
	p2, _ := GoalAdd.Run(ctx, d, GoalAddInput{Title: "Second", Owner: "Jordan Example", DoneWhen: "Done."})
	a, _ := TaskAdd.Run(ctx, d, TaskAddInput{Goal: f.proj, Title: "A"})
	b, _ := TaskAdd.Run(ctx, d, TaskAddInput{Goal: f.proj, Title: "B"})
	if _, err := TaskMove.Run(ctx, d, TaskMoveInput{IDs: []string{a.ID, b.ID}, Goal: p2.ID}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{a.ID, b.ID} {
		doc, _ := d.Store.Get(ctx, id)
		if doc.Meta.(*model.Task).Goal.Target() != p2.ID {
			t.Fatalf("%s not moved", id)
		}
	}
	if _, err := TaskMove.Run(ctx, d, TaskMoveInput{IDs: []string{a.ID, "task-none"}, Goal: f.proj}); err == nil {
		t.Fatal("a bad id moved")
	}
	doc, _ := d.Store.Get(ctx, a.ID)
	if doc.Meta.(*model.Task).Goal.Target() != p2.ID {
		t.Fatal("a failed move changed a task")
	}
}

func TestDocEditFieldLimitsAndConflict(t *testing.T) {
	f := newAgentFix(t)
	d := f.owner
	tk, _ := TaskAdd.Run(ctx, d, TaskAddInput{Goal: f.proj, Title: "Edit me"})
	title, body := "Edited title", "Edited body."
	if _, err := DocEdit.Run(ctx, d, DocEditInput{ID: tk.ID, Title: &title, Body: &body, Fields: map[string]any{"assignee": "agent:x", "done_when": "It reads well."}}); err != nil {
		t.Fatal(err)
	}
	doc, _ := d.Store.Get(ctx, tk.ID)
	m := doc.Meta.(*model.Task)
	if doc.Title != title || doc.Body != body || m.Assignee != "agent:x" || m.DoneWhen != "It reads well." || m.Status != model.TaskTodo {
		t.Fatalf("doc = %+v %+v", doc, m)
	}
	for _, k := range []string{"status", "type", "created", "previous", "nope"} {
		if _, err := DocEdit.Run(ctx, d, DocEditInput{ID: tk.ID, Fields: map[string]any{k: "x"}}); err == nil || !strings.Contains(err.Error(), "not an editable field") {
			t.Errorf("field %s: %v", k, err)
		}
	}
	if _, err := DocEdit.Run(ctx, d, DocEditInput{ID: tk.ID, Fields: map[string]any{"due": "not a date"}}); err == nil {
		t.Error("an invalid date was saved")
	}
	if _, err := DocEdit.Run(ctx, d, DocEditInput{ID: f.source, Title: &title}); err == nil || !strings.Contains(err.Error(), "edited with edit") {
		t.Errorf("note: %v", err)
	}
	// Conflict: an edit from an old revision is refused.
	stale := doc.Rev
	t2 := "Second"
	if _, err := DocEdit.Run(ctx, d, DocEditInput{ID: tk.ID, Title: &t2}); err != nil {
		t.Fatal(err)
	}
	t3 := "Third"
	if _, err := DocEdit.Run(ctx, d, DocEditInput{ID: tk.ID, Title: &t3, Rev: stale}); !errors.Is(err, ErrStale) {
		t.Fatalf("stale edit: %v", err)
	}
}

func TestDocEditJournalKeepsGenerated(t *testing.T) {
	f := newAgentFix(t)
	d := f.owner
	createDoc(t, d, model.Document{ID: "2026-11-01", Folder: model.FolderJournal, Title: "2026-11-01",
		Body: "Mine.\n\n%% para:begin events %%\nevents: 3\n%% para:end events %%",
		Meta: &model.Journal{Common: model.Common{Type: model.TypeJournal}, Date: "2026-11-01", Scope: model.JournalDay}})
	ok := "Mine, more.\n\n%% para:begin events %%\nevents: 3\n%% para:end events %%"
	if _, err := DocEdit.Run(ctx, d, DocEditInput{ID: "2026-11-01", Body: &ok}); err != nil {
		t.Fatal(err)
	}
	bad := "Mine.\n\n%% para:begin events %%\nevents: 99\n%% para:end events %%"
	if _, err := DocEdit.Run(ctx, d, DocEditInput{ID: "2026-11-01", Body: &bad}); err == nil {
		t.Fatal("a generated section was edited")
	}
}

func TestEditableFieldsNeverStatusOrIdentity(t *testing.T) {
	for _, typ := range paramd.EditableTypes() {
		for _, fd := range paramd.EditableFields(typ) {
			switch fd.Name {
			case "status", "type", "created", "updated":
				t.Errorf("%s: %s is editable", typ, fd.Name)
			}
		}
	}
	if paramd.EditableFields("proposal") != nil || paramd.EditableFields("receipt") != nil {
		t.Error("proposal or receipt is editable")
	}
}

func TestExceptionPolicyTextAndBy(t *testing.T) {
	f := newAgentFix(t)
	d := f.owner
	if _, err := ExceptionAdd.Run(ctx, d, ExceptionAddInput{Kind: model.ExceptionWideEnoughFor, By: "x"}); err == nil {
		t.Fatal("no policy and no policy_text")
	}
	out, err := ExceptionAdd.Run(ctx, d, ExceptionAddInput{PolicyText: "Retention standard", Kind: model.ExceptionEstablishedExample, By: "unknown"})
	if err != nil {
		t.Fatal(err)
	}
	h, err := HealthOp.Run(ctx, d, HealthInput{})
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, g := range h.Groups {
		for _, fi := range g.Findings {
			if fi.ID == out.ID {
				kinds = append(kinds, fi.Kind)
			}
		}
	}
	if !slicesHas(kinds, HealthExceptionNoPolicy) || !slicesHas(kinds, HealthByUnknown) {
		t.Fatalf("kinds = %v", kinds)
	}
}

func TestAttemptAddAndNudge(t *testing.T) {
	f := newAgentFix(t)
	d := f.owner
	tk, _ := TaskAdd.Run(ctx, d, TaskAddInput{Goal: f.proj, Title: "Chase"})
	for i := 0; i < 2; i++ {
		if _, err := AttemptAdd.Run(ctx, d, AttemptAddInput{Target: tk.ID, What: "Emailed", Result: "No reply", Sources: []string{"https://acme.example/m/1"}}); err != nil {
			t.Fatal(err)
		}
	}
	doc, _ := d.Store.Get(ctx, tk.ID)
	if strings.Count(doc.Body, "## Attempts") != 1 || strings.Count(doc.Body, "Emailed: No reply") != 2 {
		t.Fatalf("body = %q", doc.Body)
	}
	if _, err := AttemptAdd.Run(ctx, d, AttemptAddInput{Target: f.source, What: "x", Result: "y"}); err == nil {
		t.Fatal("attempt on a note")
	}
}
