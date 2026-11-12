package ops

import (
	"errors"
	"slices"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

// report_outcomes counts closes by outcome and actor, and receipt undos (from receipt_undo and the owner's
// receipt_revert).
func TestReportOutcomes(t *testing.T) {
	f := newAgentFix(t)
	add := func(title string) string {
		return mustRun(t, TaskAdd, f.owner, TaskAddInput{Goal: f.proj, Title: title, DoneWhen: "Done."}).ID
	}
	mustRun(t, TaskDone, f.owner, TaskDoneInput{ID: add("One")})
	mustRun(t, TaskResolve, f.owner, TaskResolveInput{ID: add("Two"), Outcome: model.OutcomeDropped, Note: "Not needed."})
	said := WithWrite(t.Context(), WriteMeta{Why: "Owner said.", OwnerSaid: "it failed", Confidence: model.ConfidenceHigh})
	if _, err := TaskResolve.Run(said, f.agent, TaskResolveInput{ID: add("Three"), Outcome: model.OutcomeFailed, Tried: "x", StoppedBecause: "y"}); err != nil {
		t.Fatal(err)
	}
	// Two agent writes, each with a receipt: one undone by the agent, one by the owner.
	n1 := mustRun(t, NoteAdd, f.agent, NoteAddInput{Title: "Agent note one", Body: "x"})
	n2 := mustRun(t, NoteAdd, f.agent, NoteAddInput{Title: "Agent note two", Body: "y"})
	_ = n1
	r1 := mustRun(t, ReceiptList, f.owner, ReceiptListInput{Doc: n1.ID}).Items[0].ID
	r2 := mustRun(t, ReceiptList, f.owner, ReceiptListInput{Doc: n2.ID}).Items[0].ID
	if _, err := ReceiptRevert.Run(t.Context(), f.agent, ReceiptIDInput{ID: r2}); !errors.Is(err, ErrPersonOnly) {
		t.Fatalf("an agent ran receipt_revert: %v", err)
	}
	mustRun(t, ReceiptUndo, f.owner, ReceiptUndoInput{ID: r1})
	mustRun(t, ReceiptRevert, f.owner, ReceiptIDInput{ID: r2})

	out := mustRun(t, ReportOutcomes, f.owner, ReportOutcomesInput{})
	counts := map[string]int{}
	for _, o := range out.Outcomes {
		counts[o.Key] = o.Count
	}
	if out.Closes != 3 || counts["done"] != 1 || counts["dropped"] != 1 || counts["failed"] != 1 || len(out.Outcomes) != len(model.TaskOutcomes) {
		t.Fatalf("closes = %d %v", out.Closes, counts)
	}
	byActor := map[string]int{}
	for _, a := range out.ByActor {
		byActor[a.Actor] = a.Total
	}
	if byActor["user"] != 2 || byActor["claude"] != 1 {
		t.Fatalf("by actor = %+v", out.ByActor)
	}
	if len(out.Undos) != 2 || !slices.ContainsFunc(out.Undos, func(u UndoRow) bool { return u.Receipt == r2 && u.Doc == n2.ID }) {
		t.Fatalf("undos = %+v", out.Undos)
	}
	// A window before today holds nothing.
	old := mustRun(t, ReportOutcomes, f.owner, ReportOutcomesInput{Since: "2020-01-01", Until: "2020-01-31"})
	if old.Closes != 0 || len(old.Undos) != 0 {
		t.Fatalf("old window = %+v", old)
	}
	if _, err := ReportOutcomes.Run(t.Context(), f.owner, ReportOutcomesInput{Since: "2026-12-01", Until: "2026-11-01"}); err == nil {
		t.Fatal("since after until was accepted")
	}
	if l := mustRun(t, ReceiptList, f.owner, ReceiptListInput{Doc: n2.ID}); l.Items[0].Undone == "" {
		t.Fatalf("the reverted receipt is not marked undone: %+v", l.Items[0])
	}
	// receipt_list filters by op and by day.
	if l := mustRun(t, ReceiptList, f.owner, ReceiptListInput{Op: "note_add", Since: "2020-01-01"}); len(l.Items) != 2 {
		t.Fatalf("receipts by op = %d", len(l.Items))
	}
	if l := mustRun(t, ReceiptList, f.owner, ReceiptListInput{Until: "2020-01-01"}); len(l.Items) != 0 {
		t.Fatalf("receipts until 2020 = %d", len(l.Items))
	}
}
