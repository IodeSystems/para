package ops

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
)

// clockFix is an agentFix whose clock moves (rv.advance). It starts on
// 2026-11-01 09:00 UTC.
func clockFix(t *testing.T) (*agentFix, *reqVault) {
	t.Helper()
	rv := newReqVault(t)
	f := &agentFix{t: t, root: rv.root, owner: rv.as("user", "cli", "install-a"), agent: rv.as("claude", "mcp", "install-a")}
	f.proj = must(t, ProjectAdd, f.owner, ProjectAddInput{DoneWhen: "Done.", Title: "Portal", Owner: "Jordan Example"}).ID
	f.source = must(t, NoteAdd, f.owner, NoteAddInput{Title: "Sync notes", Body: "Ship it."}).ID
	return f, rv
}

func stackItems(t *testing.T, d Deps, in ProposalStackInput) []string {
	t.Helper()
	return stackRefs(must(t, ProposalStack, d, in))
}

func pendingOf(t *testing.T, d Deps, id string) int {
	t.Helper()
	for _, v := range must(t, ProposalList, d, ProposalListInput{}).Items {
		if v.ID == id {
			return v.Pending
		}
	}
	t.Fatalf("no proposal %s", id)
	return 0
}

func TestDeferDossierHidesItsOpenItemsUntilTheDate(t *testing.T) {
	f, rv := clockFix(t)
	p := fileDossierMD(t, f, dossierMD(f))
	must(t, ProposalDecide, f.agent, ProposalDecideInput{ID: p, Items: []DecideItem{{Item: "m1", Decision: model.DecideApprove}}, OwnerSaid: "send it"})
	before := pendingOf(t, f.owner, p)
	if before == 0 {
		t.Fatal("nothing pending to start with")
	}

	out := must(t, Defer, f.owner, DeferInput{IDs: []string{p}, Until: "2026-11-03", Reason: "After the launch."})
	if !slices.ContainsFunc(out.Skipped, func(s string) bool { return strings.HasPrefix(s, p+"#m1: already approved") }) {
		t.Fatalf("skipped = %v: the approved item should say why it stays", out.Skipped)
	}
	if slices.Contains(out.Changed, p+"#m1") || len(out.Changed) == 0 {
		t.Fatalf("changed = %v", out.Changed)
	}
	for _, r := range must(t, ProposalStack, f.owner, ProposalStackInput{}).Rows {
		if r.Proposal == p && r.Stack == StackProposed {
			t.Fatalf("item %s is still proposed", r.Item)
		}
	}
	later := must(t, ProposalStack, f.owner, ProposalStackInput{State: StackLater})
	if len(later.Rows) != len(out.Changed) || later.Rows[0].DeferUntil != "2026-11-03" {
		t.Fatalf("later rows = %+v", later.Rows)
	}
	if n := pendingOf(t, f.owner, p); n != 0 {
		t.Fatalf("pending = %d while put off", n)
	}
	if st := must(t, ProposalShow, f.owner, ProposalIDInput{ID: p}).Proposal.Status; st != model.ProposalOpen {
		t.Fatalf("dossier status = %s: a put-off item still waits", st)
	}

	// The day after until, they are back.
	rv.advance(72 * time.Hour)
	if n := pendingOf(t, f.owner, p); n != before {
		t.Fatalf("pending after the date = %d, want %d", n, before)
	}
	if got := stackItems(t, f.owner, ProposalStackInput{State: StackLater}); len(got) != 0 {
		t.Fatalf("later after the date = %v", got)
	}
}

// An agent's defer of dossier items is undoable: it applies at once, and its
// receipt undoes it.
func TestAgentDeferOfItemsAppliesAndUndoes(t *testing.T) {
	f, _ := clockFix(t)
	p := fileDossierMD(t, f, dossierMD(f))
	in := DeferInput{Items: []ItemRef{{Proposal: p, Item: "q1"}}, Until: "2026-11-20"}
	if _, err := Defer.Run(WithWrite(t.Context(), WriteMeta{Why: "Not now."}), f.agent, in); err != nil {
		t.Fatal(err)
	}
	if got := stackItems(t, f.owner, ProposalStackInput{State: StackLater}); !slices.Equal(got, []string{p + "#q1"}) {
		t.Fatalf("later = %v", got)
	}
	rs := must(t, ReceiptList, f.owner, ReceiptListInput{Op: "defer", Actor: "claude"}).Items
	if len(rs) != 1 || rs[0].SelfReported {
		t.Fatalf("receipts = %+v", rs)
	}
	must(t, ReceiptUndo, f.owner, ReceiptUndoInput{ID: rs[0].ID})
	if got := stackItems(t, f.owner, ProposalStackInput{State: StackLater}); len(got) != 0 {
		t.Fatalf("after undo, later = %v", got)
	}

	// The owner puts it off and brings it back.
	must(t, Defer, f.owner, in)
	out := must(t, Undefer, f.owner, UndeferInput{IDs: []string{p}})
	if !slices.Equal(out.Changed, []string{p + "#q1"}) {
		t.Fatalf("undefer = %+v", out)
	}
	if got := stackItems(t, f.owner, ProposalStackInput{State: StackLater}); len(got) != 0 {
		t.Fatalf("after undefer, later = %v", got)
	}
}

func TestDeferTopicHidesIOAndItems(t *testing.T) {
	f, rv := clockFix(t)
	a := fileDossierMD(t, f, socialMD("Posts A"))
	io := must(t, IOAdd, f.owner, IOAddInput{Direction: model.IOIn, Text: "Can you post the launch?", Expects: model.ExpectsResponse, Tags: []string{"socials"}}).ID
	must(t, TagAdd, f.owner, TagInput{IDs: []string{a}, Tags: []string{"socials"}})
	inOrder := func() bool {
		return slices.ContainsFunc(must(t, OrderOp, f.owner, OrderInput{}).Rows, func(r ValueRow) bool { return r.ID == io })
	}
	if !inOrder() {
		t.Fatal("the io item is not in the order to start with")
	}
	out := must(t, Defer, f.owner, DeferInput{Tag: "socials", Until: "2026-11-09", Reason: "Socials wait until Monday."})
	want := []string{io, a + "#q1", a + "#q2"}
	if !slices.Equal(out.Changed, want) {
		t.Fatalf("changed = %v, want %v", out.Changed, want)
	}
	if inOrder() {
		t.Fatal("a put-off io item is in the order")
	}
	if got := stackItems(t, f.owner, ProposalStackInput{}); len(got) != 0 {
		t.Fatalf("stack = %v while put off", got)
	}
	// A view scoped by the topic still lists them, as later.
	scoped := must(t, ProposalStack, f.owner, ProposalStackInput{Tag: "socials"})
	if len(scoped.Rows) != 2 || scoped.Rows[0].Stack != StackLater || scoped.NeedsYou != 0 {
		t.Fatalf("stack by tag = %+v while put off", scoped.Rows)
	}
	list := must(t, TagList, f.owner, TagListInput{}).Tags
	if len(list) != 1 || list[0].Later != 3 || list[0].Stack != 0 {
		t.Fatalf("tag_list = %+v", list)
	}
	rv.advance(9 * 24 * time.Hour)
	if !inOrder() {
		t.Fatal("the io item did not come back on its date")
	}
	if got := stackItems(t, f.owner, ProposalStackInput{Tag: "socials"}); len(got) != 2 {
		t.Fatalf("stack by tag after the date = %v", got)
	}
}
