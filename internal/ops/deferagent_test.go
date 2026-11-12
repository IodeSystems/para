package ops

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
)

func TestBlockedTaskReturnsOnItsFollowUpDate(t *testing.T) {
	d, proj := laterVault(t)
	nextIDs := func() []string {
		out := []string{}
		for _, x := range must(t, Next, d, NextInput{}).Tasks {
			out = append(out, x.ID)
		}
		return out
	}
	listed := func(id string) (TaskView, bool) {
		for _, x := range must(t, TaskList, d, TaskListInput{}).Tasks {
			if x.ID == id {
				return x, true
			}
		}
		return TaskView{}, false
	}
	// The due date is when the work must be done: it never hides a blocked task.
	due := must(t, TaskAdd, d, TaskAddInput{Goal: proj, Title: "Pay the vendor", Due: "2026-11-20"}).ID
	must(t, TaskBlock, d, TaskBlockInput{ID: due, Reason: "Finance said the invoice is not in yet."})
	if v, ok := listed(due); !ok || v.Returns != "" || v.Deferred {
		t.Fatalf("blocked task with only a due date = %+v, listed %v", v, ok)
	}
	if _, err := TaskBlock.Run(ctx, d, TaskBlockInput{ID: due, Reason: "x", Until: "soon"}); err == nil {
		t.Fatal("a bad until was accepted")
	}

	id := must(t, TaskAdd, d, TaskAddInput{Goal: proj, Title: "Renew the cert", Due: "2026-11-30"}).ID
	must(t, TaskBlock, d, TaskBlockInput{ID: id, Reason: "Ops said the CA is down until Thursday.", Until: "2026-11-05"})
	if slices.Contains(nextIDs(), id) {
		t.Fatal("next lists a blocked task before its follow-up date")
	}
	if v, ok := listed(id); !ok || v.Status != model.TaskBlocked || v.Returns != "2026-11-05" {
		t.Fatalf("task_list = %+v, listed %v: a blocked task stays listed with returns", v, ok)
	}
	azClock = time.Date(2026, 11, 5, 9, 0, 0, 0, time.Local)
	t.Cleanup(func() { azClock = azNow })
	got := must(t, Next, d, NextInput{}).Tasks
	if !slices.ContainsFunc(got, func(v TaskView) bool { return v.ID == id && v.Status == model.TaskBlocked }) {
		t.Fatalf("next on the follow-up date = %+v", got)
	}
	if slices.Contains(nextIDs(), due) {
		t.Fatal("next lists a blocked task with no follow-up date")
	}
}

func TestUnblockClearsOnlyTheBlockFollowUp(t *testing.T) {
	d, proj := laterVault(t)
	id := must(t, TaskAdd, d, TaskAddInput{Goal: proj, Title: "Renew the cert"}).ID
	must(t, TaskBlock, d, TaskBlockInput{ID: id, Reason: "Ops said the CA is down.", Until: "2026-11-10"})
	if v, _ := taskOf(t, d, id); v.DeferUntil != "2026-11-10" || !strings.HasPrefix(v.DeferredReason, blockMarker) {
		t.Fatalf("blocked = %+v", v)
	}
	must(t, TaskStart, d, TaskStartInput{ID: id})
	if v, _ := taskOf(t, d, id); v.DeferUntil != "" || v.DeferredReason != "" || v.Deferred {
		t.Fatalf("unblocked task kept the block's follow-up date: %+v", v)
	}

	// An owner's deferral is not the block's: unblocking leaves it, and a block
	// with until does not overwrite it.
	other := must(t, TaskAdd, d, TaskAddInput{Goal: proj, Title: "Tidy the wiki"}).ID
	must(t, Defer, d, DeferInput{IDs: []string{other}, Until: "2026-12-01", Reason: "After the audit."})
	if _, err := TaskBlock.Run(ctx, d, TaskBlockInput{ID: other, Reason: "Wiki is read-only.", Until: "2026-11-10"}); err == nil {
		t.Fatal("a block until overwrote the owner's deferral")
	}
	must(t, TaskBlock, d, TaskBlockInput{ID: other, Reason: "Wiki is read-only."})
	must(t, TaskStart, d, TaskStartInput{ID: other})
	if v, _ := taskOf(t, d, other); v.DeferUntil != "2026-12-01" || v.DeferredReason != "After the audit." {
		t.Fatalf("unblocking cleared the owner's deferral: %+v", v)
	}
}
