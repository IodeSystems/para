package ops

import (
	"slices"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

// gatedVault is laterVault with the default gate: undoable, minor, obvious.
func gatedVault(t *testing.T) (Deps, string) {
	t.Helper()
	d, proj := laterVault(t)
	if err := d.Store.SetConfig(ctx, gateConfig, "undoable,minor,obvious"); err != nil {
		t.Fatal(err)
	}
	return d, proj
}

func TestOwnerSaidPassesTheGateForSafeWrites(t *testing.T) {
	d, _ := gatedVault(t)
	ag := agentDeps(d, "scribe")

	// Neither ratings nor the owner's words: held, and the message says how.
	_, err := ProjectAdd.Run(WithWrite(ctx, WriteMeta{Why: "A new goal."}), ag, ProjectAddInput{Title: "Launch the blog", DoneWhen: "The blog is live."})
	st, ok := AsStaged(err)
	if !ok {
		t.Fatalf("project_add without ratings = %v, want staged", err)
	}
	if want := "or with owner_said when the owner asked for it"; !strings.Contains(st.Message, want) {
		t.Fatalf("staged message = %q, want %q", st.Message, want)
	}

	// The owner asked: it applies, and the receipt is self-reported.
	out, err := ProjectAdd.Run(WithWrite(ctx, WriteMeta{Why: "Owner asked.", OwnerSaid: "add a goal for the newsletter"}), ag,
		ProjectAddInput{Title: "Start the newsletter", DoneWhen: "The first issue is out."})
	if err != nil {
		t.Fatal(err)
	}
	rs := must(t, ReceiptList, d, ReceiptListInput{Doc: out.ID}).Items
	if len(rs) != 1 || !rs[0].SelfReported || rs[0].OwnerSaid != "add a goal for the newsletter" {
		t.Fatalf("receipts = %+v", rs)
	}

	// A review op still needs the review: owner_said is not enough.
	if _, err := ProjectEdit.Run(WithWrite(ctx, WriteMeta{Why: "Owner asked.", OwnerSaid: "push it a week", Minor: true, Obvious: true}), ag,
		ProjectEditInput{ID: out.ID, Due: "2026-12-01"}); err == nil {
		t.Fatal("a review op applied")
	} else if _, ok := AsStaged(err); !ok {
		t.Fatalf("project_edit = %v, want staged", err)
	}
}

// An act the agent saw closes the item it completes: the close ops are safe
// and undoable, so minor and obvious apply them under the default gate.
func TestCloseOpsApplyWithRatings(t *testing.T) {
	d, proj := gatedVault(t)
	ag := agentDeps(d, "scribe")
	id := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Approve the API change"}).ID
	w := WithWrite(ctx, WriteMeta{Why: "Approved on the PR at 17:43 UTC, after the last commit.", Sources: []string{"https://example.test/pr/1"}, Minor: true, Obvious: true})
	if _, err := TaskDone.Run(w, ag, TaskDoneInput{ID: id}); err != nil {
		t.Fatalf("task_done with minor and obvious = %v", err)
	}
	for _, e := range []Entry{TaskDone, MeetingHold, RFXReply, ProposalExecuted, IOSend} {
		if e.Risk() != RiskSafe || !e.Undoable() {
			t.Errorf("%s: risk %s, undoable %v: an observed act would be held", e.Name(), e.Risk(), e.Undoable())
		}
	}
}

func TestFeedbackAddFilesTaggedIO(t *testing.T) {
	d, _ := gatedVault(t)
	ag := agentDeps(d, "scribe")
	out, err := FeedbackAdd.Run(WithWrite(ctx, WriteMeta{Why: "The error lacked the field.", Minor: true, Obvious: true}), ag,
		FeedbackAddInput{Text: "proposal_decide refused decision with no hint.", Kind: FeedbackBug, Tool: "proposal_decide"})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := d.Store.Get(ctx, out.ID)
	if err != nil {
		t.Fatal(err)
	}
	m := doc.Meta.(*model.IO)
	if m.Direction != model.IOIn || !slices.Contains(m.Tags, FeedbackTag) || m.Subject != "para bug: proposal_decide" {
		t.Fatalf("feedback io = %+v", m)
	}
	if rs := must(t, ReceiptList, d, ReceiptListInput{Doc: out.ID}).Items; len(rs) != 1 {
		t.Fatalf("receipts = %d, want one", len(rs))
	}
	if _, err := FeedbackAdd.Run(ctx, d, FeedbackAddInput{Text: "x", Tool: "no_such_op"}); err == nil {
		t.Fatal("an unknown tool was accepted")
	}
}
