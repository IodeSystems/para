package ops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

func TestDiffLines(t *testing.T) {
	got := diffLines("a\nb\nc", "a\nx\nc")
	if want := []string{"  a", "- b", "+ x", "  c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("%v", got)
	}
	if got := diffLines("", "a"); !reflect.DeepEqual(got, []string{"+ a"}) {
		t.Fatalf("%v", got)
	}
}

func writeCtx(t *testing.T) context.Context {
	return WithWrite(t.Context(), WriteMeta{Why: "The owner asked.", Sources: []string{"note-x"}, Verify: "Open the task.", Confidence: model.ConfidenceHigh})
}

func TestReceiptRecordsDiffAndWhy(t *testing.T) {
	f := newAgentFix(t)
	ctx := writeCtx(t)
	task, err := TaskAdd.Run(ctx, f.agent, TaskAddInput{Project: f.proj, Title: "Receipted", DoneWhen: "It exists."})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := TaskStart.Run(ctx, f.agent, TaskStartInput{ID: task.ID}); err != nil {
		t.Fatal(err)
	}
	list, err := ReceiptList.Run(t.Context(), f.owner, ReceiptListInput{Doc: task.ID})
	if err != nil || len(list.Items) != 2 {
		t.Fatalf("receipts = %+v %v", list.Items, err)
	}
	start := list.Items[0]
	if start.Op != "task_start" {
		start = list.Items[1]
	}
	if start.Why != "The owner asked." || start.Verify == "" || start.Actor != "claude" || start.Check != model.ReceiptUnchecked {
		t.Fatalf("receipt = %+v", start)
	}
	show, err := ReceiptShow.Run(t.Context(), f.owner, ReceiptIDInput{ID: start.ID})
	if err != nil {
		t.Fatal(err)
	}
	if text := strings.Join(show.Lines, "\n"); !strings.Contains(text, "- status: todo") || !strings.Contains(text, "+ status: doing") || !show.Revertible {
		t.Fatalf("show = %+v", show)
	}
	// Events hold ids and point at the receipt.
	evs, err := f.owner.Store.Events(t.Context(), 200)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range evs {
		found = found || (e.Op == "receipt" && e.To == start.ID && e.Doc == task.ID)
	}
	if !found {
		t.Fatal("no event points at the receipt")
	}
	// Revert uses the stored text.
	if _, err := ReceiptUndo.Run(t.Context(), f.owner, ReceiptUndoInput{ID: start.ID}); err != nil {
		t.Fatal(err)
	}
	got, _ := TaskShow.Run(t.Context(), f.owner, TaskShowInput{ID: task.ID})
	if got.Task.Status != model.TaskTodo {
		t.Fatalf("status = %s", got.Task.Status)
	}
	if _, err := ReceiptUndo.Run(t.Context(), f.owner, ReceiptUndoInput{ID: start.ID}); err == nil || !strings.Contains(err.Error(), "changed since") {
		t.Fatalf("second revert: %v", err)
	}
}

func TestReceiptWrongOpensWork(t *testing.T) {
	f := newAgentFix(t)
	task, err := TaskAdd.Run(writeCtx(t), f.agent, TaskAddInput{Project: f.proj, Title: "Wrong one", DoneWhen: "It exists."})
	if err != nil {
		t.Fatal(err)
	}
	list, _ := ReceiptList.Run(t.Context(), f.owner, ReceiptListInput{Doc: task.ID})
	if _, err := ReceiptCheck.Run(t.Context(), f.owner, ReceiptCheckInput{ID: list.Items[0].ID, Verdict: model.ReceiptWrong}); err == nil {
		t.Fatal("wrong needs a note")
	}
	out, err := ReceiptCheck.Run(t.Context(), f.owner, ReceiptCheckInput{ID: list.Items[0].ID, Verdict: model.ReceiptWrong, Note: "Wrong project."})
	if err != nil || out.Request == "" {
		t.Fatalf("out = %+v %v", out, err)
	}
	if _, err := ReceiptCheck.Run(t.Context(), f.agent, ReceiptCheckInput{ID: list.Items[0].ID, Verdict: model.ReceiptVerified}); !errors.Is(err, ErrPersonOnly) {
		t.Fatalf("agent check: %v", err)
	}
}

func TestPersonWriteLeavesNoReceipt(t *testing.T) {
	f := newAgentFix(t)
	if _, err := TaskAdd.Run(t.Context(), f.owner, TaskAddInput{Project: f.proj, Title: "Mine"}); err != nil {
		t.Fatal(err)
	}
	list, _ := ReceiptList.Run(t.Context(), f.owner, ReceiptListInput{})
	if len(list.Items) != 0 {
		t.Fatalf("receipts = %+v", list.Items)
	}
}

func TestEveryOpHasARisk(t *testing.T) {
	want := map[string]Risk{"task_list": RiskRead, "inbox_add": RiskSafe, "project_close": RiskReview, "org_person_add": RiskReview}
	for _, e := range All() {
		if !slices.Contains([]Risk{RiskRead, RiskSafe, RiskReview}, e.Risk()) {
			t.Errorf("%s: risk %q", e.Name(), e.Risk())
		}
		if w, ok := want[e.Name()]; ok && e.Risk() != w {
			t.Errorf("%s: risk %s, want %s", e.Name(), e.Risk(), w)
		}
	}
}

func TestSearchBeforeCreate(t *testing.T) {
	f := newAgentFix(t)
	ctx := writeCtx(t)
	first, err := TaskAdd.Run(ctx, f.agent, TaskAddInput{Project: f.proj, Title: "Rotate the staging keys", DoneWhen: "Keys are new."})
	if err != nil {
		t.Fatal(err)
	}
	// Exact title.
	_, err = TaskAdd.Run(ctx, f.agent, TaskAddInput{Project: f.proj, Title: "rotate the  staging keys", DoneWhen: "x"})
	if err == nil || !strings.Contains(err.Error(), first.ID) || !strings.Contains(err.Error(), "Rotate the staging keys") {
		t.Fatalf("duplicate: %v", err)
	}
	// Near title, and a near type.
	_, err = QuestionAdd.Run(ctx, f.agent, QuestionAddInput{Text: "Rotate staging keys", Project: f.proj})
	if err == nil || !strings.Contains(err.Error(), first.ID) {
		t.Fatalf("near type: %v", err)
	}
	// An override with a reason passes and lands in the receipt.
	over := WithWrite(t.Context(), WriteMeta{Why: "w", NotDuplicate: "This one is for production."})
	dup, err := TaskAdd.Run(over, f.agent, TaskAddInput{Project: f.proj, Title: "Rotate the staging keys", DoneWhen: "x"})
	if err != nil {
		t.Fatal(err)
	}
	list, _ := ReceiptList.Run(t.Context(), f.owner, ReceiptListInput{Doc: dup.ID})
	if len(list.Items) != 1 || list.Items[0].NotDuplicate != "This one is for production." {
		t.Fatalf("receipt = %+v", list.Items)
	}
	// A person is never refused.
	if _, err := TaskAdd.Run(t.Context(), f.owner, TaskAddInput{Project: f.proj, Title: "Rotate the staging keys"}); err != nil {
		t.Fatalf("person: %v", err)
	}
	if hint := SimilarFor(t.Context(), f.owner.Store, "task_add", TaskAddInput{Title: "Rotate the staging keys"}); len(hint) == 0 {
		t.Fatal("no hint")
	}
	again := WithWrite(t.Context(), WriteMeta{Why: "Another environment.", NotDuplicate: "Another environment."})
	if _, err := TaskAdd.Run(again, f.agent, TaskAddInput{Project: f.proj, Title: "Rotate the staging keys", DoneWhen: "Done."}); err != nil {
		t.Fatalf("not_duplicate: %v", err)
	}
	// A closed match is not a match.
	if _, err := TaskDone.Run(t.Context(), f.owner, TaskDoneInput{ID: first.ID}); err != nil {
		t.Fatal(err)
	}
	near, _ := SimilarOp.Run(t.Context(), f.owner, SimilarInput{Type: model.TypeTask, Title: "Rotate the staging keys"})
	for _, s := range near.Items {
		if s.ID == first.ID {
			t.Fatalf("closed match: %+v", near.Items)
		}
	}
}

func TestAgentWorkTurns(t *testing.T) {
	f := newAgentFix(t)
	if err := os.WriteFile(filepath.Join(f.root, ".para", "proposals.yaml"), []byte("work:\n  release:\n    require: ["+f.source+"]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	add, err := RequestAdd.Run(t.Context(), f.owner, RequestAddInput{Ask: "Ship it", Kind: "release", Context: []string{f.proj}})
	if err != nil {
		t.Fatal(err)
	}
	next, err := RequestNext.Run(t.Context(), f.agent, RequestNextInput{})
	if err != nil || next.Request == nil || len(next.Request.Context) != 2 {
		t.Fatalf("next = %+v %v", next.Request, err)
	}
	if _, err := RequestDone.Run(t.Context(), f.agent, RequestDoneInput{ID: add.ID, Result: "x"}); err == nil || !strings.Contains(err.Error(), "resolution") {
		t.Fatalf("no resolution: %v", err)
	}
	if _, err := RequestDone.Run(t.Context(), f.agent, RequestDoneInput{ID: add.ID, Resolution: model.ResolutionDone, Result: "x"}); err == nil || !strings.Contains(err.Error(), "requires") {
		t.Fatalf("no citation: %v", err)
	}
	task, err := TaskAdd.Run(writeCtx(t), f.agent, TaskAddInput{Project: f.proj, Title: "Under the request", DoneWhen: "It exists."})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := RequestDone.Run(t.Context(), f.agent, RequestDoneInput{ID: add.ID, Resolution: model.ResolutionPartial, Result: "Half.", Followed: []string{f.source}}); err != nil {
		t.Fatal(err)
	}
	ch, err := RequestChanges.Run(t.Context(), f.owner, RequestChangesInput{ID: add.ID})
	if err != nil || len(ch.Items) != 1 || ch.Items[0].Doc != task.ID {
		t.Fatalf("changes = %+v %v", ch.Items, err)
	}
	if _, err := RequestRespond.Run(t.Context(), f.agent, RequestRespondInput{ID: add.ID, Kind: RespondChange, Text: "x"}); !errors.Is(err, ErrPersonOnly) {
		t.Fatalf("agent respond: %v", err)
	}
	if _, err := RequestRespond.Run(t.Context(), f.owner, RequestRespondInput{ID: add.ID, Kind: RespondClarify, Text: "Which half?"}); err != nil {
		t.Fatal(err)
	}
	again, err := RequestNext.Run(t.Context(), f.agent, RequestNextInput{})
	if err != nil || again.Request == nil || again.Request.ID != add.ID || again.Request.Turn != 1 {
		t.Fatalf("again = %+v %v", again.Request, err)
	}
	if !strings.Contains(again.Request.Body, "## Result (partial)") || !strings.Contains(again.Request.Body, "Which half?") {
		t.Fatalf("body = %q", again.Request.Body)
	}
	// A wrong receipt opens a response on the item it served.
	list, _ := ReceiptList.Run(t.Context(), f.owner, ReceiptListInput{Doc: task.ID})
	if _, err := RequestDone.Run(t.Context(), f.agent, RequestDoneInput{ID: add.ID, Resolution: model.ResolutionDone, Result: "All.", Followed: []string{f.source}}); err != nil {
		t.Fatal(err)
	}
	out, err := ReceiptCheck.Run(t.Context(), f.owner, ReceiptCheckInput{ID: list.Items[0].ID, Verdict: model.ReceiptWrong, Note: "Wrong goal."})
	if err != nil || out.Request != add.ID {
		t.Fatalf("wrong = %+v %v", out, err)
	}
}

func TestActionRecordKeepsAReceipt(t *testing.T) {
	f := newAgentFix(t)
	if _, err := ActionRecord.Run(t.Context(), f.agent, ActionRecordInput{What: "Opened a PR"}); err == nil {
		t.Fatal("recorded with no where")
	}
	if _, err := ActionRecord.Run(t.Context(), f.agent, ActionRecordInput{What: "Opened a PR", Where: "repo", URL: "javascript:alert(1)"}); err == nil {
		t.Fatal("recorded a bad url")
	}
	out := must(t, ActionRecord, f.agent, ActionRecordInput{What: "Opened a PR", Where: "github.example/acme/portal", URL: "https://github.example/acme/portal/pull/1", OwnerSaid: "yes, open it", Sources: []string{f.source}})
	sh := must(t, ReceiptShow, f.owner, ReceiptIDInput{ID: out.Receipt})
	r := sh.Receipt
	if r.Op != "action_record" || !r.SelfReported || r.OwnerSaid != "yes, open it" || r.Surface != "mcp" || r.Background || r.Verify != "https://github.example/acme/portal/pull/1" {
		t.Fatalf("receipt = %+v", r)
	}
	if _, err := ReceiptUndo.Run(t.Context(), f.owner, ReceiptUndoInput{ID: out.Receipt}); err == nil || !strings.Contains(err.Error(), "nothing in the vault to undo") {
		t.Fatalf("undo of an action: %v", err)
	}
}

func TestReceiptUndoManyNewestFirst(t *testing.T) {
	f := newAgentFix(t)
	ctx := writeCtx(t)
	task, err := TaskAdd.Run(ctx, f.agent, TaskAddInput{Project: f.proj, Title: "Undo me", DoneWhen: "It exists."})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := TaskStart.Run(ctx, f.agent, TaskStartInput{ID: task.ID}); err != nil {
		t.Fatal(err)
	}
	rl := must(t, ReceiptList, f.owner, ReceiptListInput{Doc: task.ID})
	if len(rl.Items) != 2 {
		t.Fatalf("receipts = %+v", rl.Items)
	}
	newest, oldest := rl.Items[0].ID, rl.Items[1].ID
	if rl.Items[0].Op != "task_start" {
		newest, oldest = oldest, newest
	}
	// Oldest first is refused: the document changed after it.
	if _, err := ReceiptUndo.Run(t.Context(), f.owner, ReceiptUndoInput{IDs: []string{oldest, newest}}); err == nil || !strings.Contains(err.Error(), "changed since") {
		t.Fatalf("oldest first: %v", err)
	}
	out := must(t, ReceiptUndo, f.agent, ReceiptUndoInput{IDs: []string{newest, oldest}})
	if len(out.Undone) != 2 {
		t.Fatalf("undone = %+v", out)
	}
	if got, err := f.owner.Store.Get(t.Context(), task.ID); err != nil || got.Folder != model.FolderArchive {
		t.Fatalf("the created task is not archived: %+v %v", got.Folder, err)
	}
}

func TestSimilarNotFalsePositive(t *testing.T) {
	d := newDeps(t)
	proj := must(t, ProjectAdd, d, ProjectAddInput{Title: "Home", DoneWhen: "Done."}).ID
	must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Collect the HOA packet", DoneWhen: "x"})
	must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Call the surveyor", DoneWhen: "x"})
	must(t, QuestionAdd, d, QuestionAddInput{Text: "Renew the EV registration", Project: proj})
	for _, c := range []struct {
		typ   model.Type
		title string
	}{
		{model.TypeTask, "Mail the EV registration form"},
		{model.TypeQuestion, "Which surveyor did the seller use?"},
		{model.TypeTask, "HOA packet"},
		{model.TypeQuestion, "Who is the surveyor?"},
	} {
		got, err := FindSimilar(ctx, d.Store, c.typ, c.title, c.title)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range got {
			if s.Title != "Collect the HOA packet" || c.title != "HOA packet" {
				t.Errorf("%q matched %q", c.title, s.Title)
			}
		}
	}
}

func TestRFXRouteWarning(t *testing.T) {
	d := newDeps(t)
	must(t, OrgDomainAdd, d, OrgAddInput{Name: "Billing", Owner: "self"})
	ok := must(t, RFXAdd, d, RFXAddInput{Direction: model.RFXOut, Ask: "Invoice copy", Route: "domain-billing", SelfServe: "checked"})
	if len(ok.Warnings) != 0 {
		t.Fatalf("warnings = %v", ok.Warnings)
	}
	bad := must(t, RFXAdd, d, RFXAddInput{Direction: model.RFXOut, Ask: "Lawn quote", Route: "landscaping", SelfServe: "checked"})
	if len(bad.Warnings) != 1 || !strings.Contains(bad.Warnings[0], "matches no domain") {
		t.Fatalf("warnings = %v", bad.Warnings)
	}
}
