package ops

import (
	"errors"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

// The ops whose write receipt_undo cannot reverse: refused on an agent
// surface, made by the owner.
func TestNotUndoableOpsAreTheOwners(t *testing.T) {
	for _, e := range []Entry{VaultConfigSet, VaultConfigUnset, VaultUpgrade, DocRename} {
		if e.Undoable() {
			t.Errorf("%s is marked undoable", e.Name())
		}
	}
	f := newAgentFix(t)
	w := writeCtx(t)
	if _, err := VaultConfigSet.Run(w, f.agent, VaultConfigSetInput{Key: "align.days", Value: "30"}); !errors.Is(err, ErrPersonOnly) || !strings.Contains(err.Error(), "cannot be undone") {
		t.Fatalf("agent vault_config_set = %v", err)
	}
	mustRun(t, VaultConfigSet, f.owner, VaultConfigSetInput{Key: "align.days", Value: "30"})
	if v := mustRun(t, VaultConfigGet, f.owner, VaultConfigGetInput{Key: "align.days"}); v.Value != "30" {
		t.Fatalf("align.days = %q", v.Value)
	}
	if _, err := VaultConfigUnset.Run(w, f.agent, VaultConfigUnsetInput{Key: "align.days"}); !errors.Is(err, ErrPersonOnly) {
		t.Fatalf("agent vault_config_unset = %v", err)
	}
	if _, err := DocRename.Run(w, f.agent, DocRenameInput{ID: f.source, NewID: "note-renamed"}); !errors.Is(err, ErrPersonOnly) {
		t.Fatalf("agent doc_rename = %v", err)
	}
	mustRun(t, DocRename, f.owner, DocRenameInput{ID: f.source, NewID: "note-renamed"})

	// An archive of an item others wait on is the owner's; with none it applies.
	dep := mustRun(t, TaskAdd, f.owner, TaskAddInput{Project: f.proj, Title: "Order parts", DoneWhen: "Ordered."}).ID
	mustRun(t, TaskAdd, f.owner, TaskAddInput{Project: f.proj, Title: "Build it", DoneWhen: "Built.", DependsOn: []string{dep}})
	_, err := DocArchive.Run(w, f.agent, DocArchiveInput{ID: dep, Reason: "Not needed."})
	if !errors.Is(err, ErrPersonOnly) || !strings.Contains(err.Error(), "1 items wait on "+dep) {
		t.Fatalf("agent archive with dependents = %v", err)
	}
	mustRun(t, DocArchive, f.owner, DocArchiveInput{ID: dep, Reason: "Not needed."})
	free := mustRun(t, TaskAdd, f.owner, TaskAddInput{Project: f.proj, Title: "Tidy up", DoneWhen: "Tidy."}).ID
	if _, err := DocArchive.Run(w, f.agent, DocArchiveInput{ID: free, Reason: "Done elsewhere."}); err != nil {
		t.Fatalf("agent archive with no dependents = %v", err)
	}
}

// Writes that were held before 0.3.0 apply now: review ops, defer, and
// task_resolve failed, with no rating and no owner_said.
func TestFormerlyHeldWritesApply(t *testing.T) {
	f := newAgentFix(t)
	w := writeCtx(t)
	task := mustRun(t, TaskAdd, f.owner, TaskAddInput{Project: f.proj, Title: "Renew the cert", DoneWhen: "Renewed."}).ID
	if _, err := TaskAssign.Run(w, f.agent, TaskAssignInput{ID: task, Assignee: "self"}); err != nil {
		t.Fatalf("task_assign (review) = %v", err)
	}
	if _, err := Defer.Run(w, f.agent, DeferInput{IDs: []string{task}, Until: "2099-01-01"}); err != nil {
		t.Fatalf("defer = %v", err)
	}
	other := mustRun(t, TaskAdd, f.owner, TaskAddInput{Project: f.proj, Title: "Rotate keys", DoneWhen: "Rotated."}).ID
	if _, err := TaskResolve.Run(w, f.agent, TaskResolveInput{ID: other, Outcome: model.OutcomeFailed, Tried: "Twice.", StoppedBecause: "No access."}); err != nil {
		t.Fatalf("task_resolve failed = %v", err)
	}
	rs := mustRun(t, ReceiptList, f.owner, ReceiptListInput{Doc: other}).Items
	if len(rs) != 1 || rs[0].SelfReported {
		t.Fatalf("receipts = %+v", rs)
	}
	mustRun(t, ReceiptUndo, f.owner, ReceiptUndoInput{ID: rs[0].ID})
	if v, _ := taskOf(t, f.owner, other); v.Status != model.TaskTodo {
		t.Fatalf("after undo = %s", v.Status)
	}
}

// An agent edits only agent work it holds: text it read must not change what
// another agent runs.
func TestAgentDocEditOfRequests(t *testing.T) {
	f := newAgentFix(t)
	w := writeCtx(t)
	queued := mustRun(t, RequestAdd, f.owner, RequestAddInput{Title: "Ask: find the owner", Ask: "Find who owns the review."}).ID
	title := "Ask: do something else"
	if _, err := DocEdit.Run(w, f.agent, DocEditInput{ID: queued, Title: &title}); err == nil || !strings.Contains(err.Error(), "you do not hold") {
		t.Fatalf("agent edit of queued work = %v", err)
	}
	mustRun(t, DocEdit, f.owner, DocEditInput{ID: queued, Title: &title})
}

// The vault setting gate.require is read by nothing: the owner can only
// unset it, and health names it while it is set.
func TestGateSettingUnused(t *testing.T) {
	f := newAgentFix(t)
	if _, err := VaultConfigSet.Run(t.Context(), f.owner, VaultConfigSetInput{Key: GateConfig, Value: "none"}); err == nil {
		t.Fatal("gate.require was set")
	}
	if err := f.owner.Store.SetConfig(t.Context(), GateConfig, "undoable,minor,obvious"); err != nil {
		t.Fatal(err)
	}
	// Set or not, an unrated agent write applies.
	if _, err := TaskAdd.Run(writeCtx(t), f.agent, TaskAddInput{Project: f.proj, Title: "Applies anyway", DoneWhen: "Done."}); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, g := range mustRun(t, HealthOp, f.owner, HealthInput{}).Groups {
		for _, x := range g.Findings {
			found = found || (x.Kind == HealthGateUnused && strings.Contains(x.Message, "gate.require is set (undoable,minor,obvious)"))
		}
	}
	if !found {
		t.Fatal("health does not name gate.require")
	}
	mustRun(t, VaultConfigUnset, f.owner, VaultConfigUnsetInput{Key: GateConfig})
	for _, g := range mustRun(t, HealthOp, f.owner, HealthInput{}).Groups {
		for _, x := range g.Findings {
			if x.Kind == HealthGateUnused {
				t.Fatal("finding after unset")
			}
		}
	}
}
