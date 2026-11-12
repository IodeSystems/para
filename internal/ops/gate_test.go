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
	dep := mustRun(t, TaskAdd, f.owner, TaskAddInput{Goal: f.proj, Title: "Order parts", DoneWhen: "Ordered."}).ID
	mustRun(t, TaskAdd, f.owner, TaskAddInput{Goal: f.proj, Title: "Build it", DoneWhen: "Built.", DependsOn: []string{dep}})
	_, err := DocArchive.Run(w, f.agent, DocArchiveInput{ID: dep, Reason: "Not needed."})
	if !errors.Is(err, ErrPersonOnly) || !strings.Contains(err.Error(), "1 items wait on "+dep) {
		t.Fatalf("agent archive with dependents = %v", err)
	}
	mustRun(t, DocArchive, f.owner, DocArchiveInput{ID: dep, Reason: "Not needed."})
	free := mustRun(t, TaskAdd, f.owner, TaskAddInput{Goal: f.proj, Title: "Tidy up", DoneWhen: "Tidy."}).ID
	if _, err := DocArchive.Run(w, f.agent, DocArchiveInput{ID: free, Reason: "Done elsewhere."}); err != nil {
		t.Fatalf("agent archive with no dependents = %v", err)
	}
}

// Writes that were held before 0.3.0 apply now: review ops, defer, and
// task_resolve failed, with no rating and no owner_said.
func TestFormerlyHeldWritesApply(t *testing.T) {
	f := newAgentFix(t)
	w := writeCtx(t)
	task := mustRun(t, TaskAdd, f.owner, TaskAddInput{Goal: f.proj, Title: "Renew the cert", DoneWhen: "Renewed."}).ID
	if _, err := TaskAssign.Run(w, f.agent, TaskAssignInput{ID: task, Assignee: "self"}); err != nil {
		t.Fatalf("task_assign (review) = %v", err)
	}
	if _, err := Defer.Run(w, f.agent, DeferInput{IDs: []string{task}, Until: "2099-01-01"}); err != nil {
		t.Fatalf("defer = %v", err)
	}
	other := mustRun(t, TaskAdd, f.owner, TaskAddInput{Goal: f.proj, Title: "Rotate keys", DoneWhen: "Rotated."}).ID
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

// D3: an outgoing draft is approved by the owner. An agent records it only
// with the owner's words, and the receipt is self-reported; it cannot mark a
// draft ready any other way. An edit after approval makes it a draft again.
func TestIOApprove(t *testing.T) {
	f := ioFix(t)
	w := writeCtx(t)
	id := outDraft(t, f, model.ExpectsNone, "")
	if _, err := IOEdit.Run(w, f.agent, IOEditInput{ID: id, State: model.IOReady}); err == nil {
		t.Fatal("an agent marked a draft ready with io_edit")
	}
	if _, err := IOApprove.Run(w, f.agent, IOApproveInput{ID: id}); err == nil || !strings.Contains(err.Error(), "owner_said is empty") {
		t.Fatalf("agent io_approve without owner_said = %v", err)
	}
	if _, m, _ := getIO(t.Context(), f.owner, id); m.State != model.IODraft {
		t.Fatalf("state = %s", m.State)
	}
	out, err := IOApprove.Run(w, f.agent, IOApproveInput{ID: id, OwnerSaid: "Yes, send the quote email."})
	if err != nil {
		t.Fatal(err)
	}
	doc, m, _ := getIO(t.Context(), f.owner, id)
	if out.State != model.IOReady || !out.SelfReported || m.ApprovedHash != m.DraftHash(doc.Body) || !m.Approved(doc.Body) {
		t.Fatalf("approved = %+v, io %+v", out, m)
	}
	rs := mustRun(t, ReceiptList, f.owner, ReceiptListInput{Doc: id, Op: "io_approve"}).Items
	if len(rs) != 1 || !rs[0].SelfReported || rs[0].OwnerSaid != "Yes, send the quote email." {
		t.Fatalf("receipts = %+v", rs)
	}
	// An edit of the text undoes the approval.
	if o := mustRun(t, IOEdit, f.agent, IOEditInput{ID: id, Text: "Hi Ada, the quote please."}); o.State != model.IODraft {
		t.Fatalf("edited approved draft = %s", o.State)
	}
	if _, m, _ := getIO(t.Context(), f.owner, id); m.ApprovedHash != "" {
		t.Fatalf("hash kept after edit: %q", m.ApprovedHash)
	}
	// An agent's send of an unapproved draft is refused; one the owner made
	// and the agent only saw is recorded.
	if _, err := IOSend.Run(t.Context(), f.agent, IOSendInput{ID: id, URL: "https://mail.example/m/1"}); err == nil || !strings.Contains(err.Error(), "not approved") {
		t.Fatalf("unapproved send: %v", err)
	}
	mustRun(t, IOSend, f.agent, IOSendInput{ID: id, URL: "https://mail.example/m/1", Observed: true})

	// The owner approves without words, at the terminal or with io_edit ready.
	a := outDraft(t, f, model.ExpectsNone, "")
	if o := mustRun(t, IOApprove, f.owner, IOApproveInput{ID: a}); o.SelfReported || o.State != model.IOReady {
		t.Fatalf("owner approve = %+v", o)
	}
	b := outDraft(t, f, model.ExpectsNone, "")
	mustRun(t, IOEdit, f.owner, IOEditInput{ID: b, State: model.IOReady})
	if doc, m, _ := getIO(t.Context(), f.owner, b); m.ApprovedHash != m.DraftHash(doc.Body) {
		t.Fatalf("owner ready = %+v", m)
	}
	mustRun(t, IOSendCheck, f.agent, IOSendCheckInput{ID: b})
	if sent := mustRun(t, IOSend, f.agent, IOSendInput{ID: b, URL: "https://mail.example/m/2"}); len(sent.Warnings) != 0 {
		t.Fatalf("approved send warned: %v", sent.Warnings)
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
	if _, err := TaskAdd.Run(writeCtx(t), f.agent, TaskAddInput{Goal: f.proj, Title: "Applies anyway", DoneWhen: "Done."}); err != nil {
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
