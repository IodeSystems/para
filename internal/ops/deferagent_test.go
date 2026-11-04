package ops

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
)

func TestAgentDeferIsGated(t *testing.T) {
	d, proj := laterVault(t)
	id := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Tidy the wiki"}).ID
	ag := agentDeps(d, "scribe")
	_, err := Defer.Run(WithWrite(ctx, WriteMeta{Why: "Not this week.", Minor: true, Obvious: true}), ag, DeferInput{IDs: []string{id}, Until: "2026-12-01"})
	st, ok := AsStaged(err)
	if !ok || !slices.Contains(st.Missing, "owner_said") {
		t.Fatalf("agent defer without owner_said = %v", err)
	}
	// The held write says where it went and when it applies.
	if want := "held for review in Staged writes scribe item " + st.Item + ": " + st.Proposal + "; it applies when the owner approves"; !strings.HasPrefix(st.Message, want) {
		t.Fatalf("staged message = %q, want prefix %q", st.Message, want)
	}
	if v, _ := taskOf(t, d, id); v.Deferred {
		t.Fatal("a staged defer ran")
	}
	if _, err := Defer.Run(WithWrite(ctx, WriteMeta{Why: "Owner asked.", OwnerSaid: "push the wiki to December"}), ag, DeferInput{IDs: []string{id}, Until: "2026-12-01"}); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(taskIDs(must(t, TaskList, d, TaskListInput{})), id) {
		t.Fatal("the deferred task is still listed")
	}
}

func TestBlockedTaskReturnsOnItsFollowUpDate(t *testing.T) {
	d, proj := laterVault(t)
	id := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Renew the cert", Due: "2026-11-05"}).ID
	must(t, TaskBlock, d, TaskBlockInput{ID: id, Reason: "Ops said the CA is down until Thursday."})
	nextIDs := func() []string {
		out := []string{}
		for _, x := range must(t, Next, d, NextInput{}).Tasks {
			out = append(out, x.ID)
		}
		return out
	}
	if slices.Contains(nextIDs(), id) || slices.Contains(taskIDs(must(t, TaskList, d, TaskListInput{})), id) {
		t.Fatal("a blocked task is listed before its follow-up date")
	}
	if !slices.Contains(taskIDs(must(t, TaskList, d, TaskListInput{Deferred: true})), id) {
		t.Fatal("the blocked task is not under Later")
	}
	azClock = time.Date(2026, 11, 5, 9, 0, 0, 0, time.Local)
	t.Cleanup(func() { azClock = azNow })
	got := must(t, Next, d, NextInput{}).Tasks
	if !slices.ContainsFunc(got, func(v TaskView) bool { return v.ID == id && v.Status == model.TaskBlocked }) {
		t.Fatalf("next on the follow-up date = %+v", got)
	}
}

func TestDescriptionsPointTheWay(t *testing.T) {
	if !strings.Contains(TaskEdit.Description(), "doc_edit") {
		t.Errorf("task_edit = %q", TaskEdit.Description())
	}
	for _, w := range []string{"matched by id only", "appended", "same title but another id does not replace", "marked stale"} {
		if !strings.Contains(ProposalRevise.Description(), w) {
			t.Errorf("proposal_revise lacks %q", w)
		}
	}
}
