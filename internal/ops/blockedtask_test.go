package ops

import (
	"testing"

	"github.com/iodesystems/para/internal/model"
)

// A task that waits on an unapproved draft is not in the owner's order; the
// draft is. The task's context and its list row name the draft as its
// blocker, labelled output and unapproved.
func TestTaskBlockedByDraftWaits(t *testing.T) {
	f := newThreadFix(t)
	draft := must(t, IOAdd, f.owner, IOAddInput{Direction: model.IOOut, Text: "Can you review the post?", To: []string{"sam@acme.example"}, Expects: model.ExpectsNone}).ID
	if _, err := Link.Invoke(writeCtx(t), f.agent, []byte(`{"from":"`+f.task+`","to":"`+draft+`","rel":"depends_on"}`)); err != nil {
		t.Fatal(err)
	}
	rows := map[string]bool{}
	for _, r := range must(t, OrderOp, f.owner, OrderInput{}).Rows {
		rows[r.ID] = true
	}
	if rows[f.task] || !rows[draft] {
		t.Fatalf("order: task %v (want false), draft %v (want true)", rows[f.task], rows[draft])
	}
	c := must(t, ItemContext, f.owner, ItemContextInput{ID: f.task})
	if len(c.BlockedBy) != 1 || c.BlockedBy[0].ID != draft || c.BlockedBy[0].Type != "output" || c.BlockedBy[0].State != "unapproved" || c.BlockedBy[0].Title == "" {
		t.Fatalf("blocked_by = %+v", c.BlockedBy)
	}
	found := false
	for _, tv := range must(t, TaskList, f.owner, TaskListInput{Goal: f.proj}).Tasks {
		if tv.ID == f.task {
			found = true
			if len(tv.Blockers) != 1 || tv.Blockers[0].ID != draft || tv.Blockers[0].State != "unapproved" {
				t.Fatalf("task row blockers = %+v", tv.Blockers)
			}
		}
	}
	if !found {
		t.Fatal("task not listed")
	}
}
