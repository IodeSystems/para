package ops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/iodesystems/parasolve/internal/model"
)

// TestReviewRule is the rule, row by row.
func TestReviewRule(t *testing.T) {
	const a, d, r, b, f, n, e = ActionApprove, ActionDeny, ActionRework, ActionSendBack, ActionHold, ActionAnnotate, ActionEdit
	cases := []struct {
		name   string
		state  model.ItemState
		edited bool
		notes  int
		held   bool
		want   []string
	}{
		{"no edits, no annotations", model.ItemPending, false, 0, false, []string{a, d, r, f, n, e}},
		{"edits only", model.ItemPending, true, 0, false, []string{a, b, f, n, e}},
		{"annotation", model.ItemPending, false, 1, false, []string{b, f, n, e}},
		{"annotation and edits", model.ItemPending, true, 2, false, []string{b, f, n, e}},
		{"held", model.ItemHeld, false, 0, true, []string{d, r, n, e}},
		{"held and edited", model.ItemHeld, true, 0, true, []string{b, n, e}},
		{"approved", model.ItemApproved, false, 0, false, []string{}},
		{"denied", model.ItemDenied, false, 0, false, []string{}},
		{"sent back", model.ItemSentBack, false, 0, false, []string{}},
		{"rework", model.ItemRework, false, 0, false, []string{}},
		{"failed", model.ItemFailed, false, 0, false, []string{}},
		{"running", model.ItemRunning, false, 0, false, []string{}},
	}
	for _, c := range cases {
		if got := allowedActions(c.state, c.edited, c.notes, c.held); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}

type propFix struct {
	t            *testing.T
	root         string
	owner, agent Deps
	source, proj string
}

func newPropFix(t *testing.T) *propFix {
	t.Helper()
	rv := newReqVault(t)
	f := &propFix{t: t, root: rv.root, owner: rv.as("user", "cli", "install-a"), agent: rv.as("claude", "mcp", "install-a")}
	note, err := NoteAdd.Run(t.Context(), f.owner, NoteAddInput{Title: "Sync notes", Body: "Ship it."})
	if err != nil {
		t.Fatal(err)
	}
	proj, err := ProjectAdd.Run(t.Context(), f.owner, ProjectAddInput{DoneWhen: "Done.", Title: "Portal", Owner: "Jordan Example"})
	if err != nil {
		t.Fatal(err)
	}
	f.source, f.proj = note.ID, proj.ID
	return f
}

var ev = model.Evidence{Quote: "Ship it.", Locator: "line 1"}

func mailItem(draft string) ItemInput {
	return ItemInput{Kind: model.ItemExternal, Medium: model.MediumEmail, Target: "ada@acme.example", Draft: draft, Evidence: ev}
}

func (f *propFix) taskItem(title string) ItemInput {
	return ItemInput{Kind: model.ItemOp, Op: "task_add", Input: map[string]any{"project": f.proj, "title": title, "done_when": "It exists."}, Evidence: ev}
}

func (f *propFix) file(items ...ItemInput) string {
	f.t.Helper()
	out, err := ProposalAdd.Run(f.t.Context(), f.agent, ProposalAddInput{Title: "Changes", Source: f.source, Items: items})
	if err != nil {
		f.t.Fatal(err)
	}
	return out.ID
}

// TestReviewRefusedInsideTheOp calls each review op directly, on each agent
// surface: no wrapper stands between the caller and the check.
func TestReviewRefusedInsideTheOp(t *testing.T) {
	f := newPropFix(t)
	id := f.file(mailItem("Hi."))
	rv := []struct {
		name string
		run  func(Deps) error
	}{
		{"approve", func(d Deps) error {
			_, err := ProposalApprove.Run(t.Context(), d, ProposalApproveInput{ID: id, Items: []string{"i1"}})
			return err
		}},
		{"deny", func(d Deps) error {
			_, err := ProposalDeny.Run(t.Context(), d, ProposalReasonInput{ID: id, Item: "i1", Reason: "r"})
			return err
		}},
		{"rework", func(d Deps) error {
			_, err := ProposalRework.Run(t.Context(), d, ProposalReasonInput{ID: id, Item: "i1", Reason: "r"})
			return err
		}},
		{"sendback", func(d Deps) error {
			_, err := ProposalSendback.Run(t.Context(), d, ProposalSendbackInput{ID: id, Item: "i1"})
			return err
		}},
		{"hold", func(d Deps) error {
			_, err := ProposalHold.Run(t.Context(), d, ProposalHoldInput{ID: id, Item: "i1", Kind: HoldRequest, Text: "x"})
			return err
		}},
		{"annotate", func(d Deps) error {
			_, err := ProposalAnnotate.Run(t.Context(), d, ProposalAnnotateInput{ID: id, Item: "i1", Quote: "Hi", Note: "n"})
			return err
		}},
		{"request accept", func(d Deps) error {
			_, err := RequestAcceptOp.Run(t.Context(), d, RequestAcceptInput{ID: "request-x"})
			return err
		}},
		{"accept", func(d Deps) error {
			_, err := QuestionAccept.Run(t.Context(), d, QuestionAcceptInput{ID: "question-x"})
			return err
		}},
		{"reject", func(d Deps) error {
			_, err := QuestionReject.Run(t.Context(), d, QuestionRejectInput{ID: "question-x", Reason: "no"})
			return err
		}},
		{"investigate", func(d Deps) error {
			_, err := Investigate.Run(t.Context(), d, InvestigateInput{ID: "question-x", Hint: "wiki"})
			return err
		}},
		{"defer", func(d Deps) error {
			_, err := Defer.Run(t.Context(), d, DeferInput{IDs: []string{"task-x"}})
			return err
		}},
		{"undefer", func(d Deps) error {
			_, err := Undefer.Run(t.Context(), d, UndeferInput{IDs: []string{"task-x"}})
			return err
		}},
		{"cut", func(d Deps) error {
			_, err := DependsCut.Run(t.Context(), d, DependsCutInput{ID: "task-x", Dep: "task-y", Reason: "no"})
			return err
		}},
		{"health_dismiss", func(d Deps) error {
			_, err := HealthDismiss.Run(t.Context(), d, HealthDismissInput{ID: "task-x", Kind: HealthTaskNoDoneWhen, Reason: "no"})
			return err
		}},
		{"health_undismiss", func(d Deps) error {
			_, err := HealthUndismiss.Run(t.Context(), d, HealthUndismissInput{ID: "task-x", Kind: HealthTaskNoDoneWhen})
			return err
		}},
		{"edit", func(d Deps) error {
			s := "x"
			_, err := ProposalEdit.Run(t.Context(), d, ProposalEditInput{ID: id, Item: "i1", Draft: &s})
			return err
		}},
	}
	for _, surface := range []string{"mcp", "api", surfaceProposal, ""} {
		d := Deps{Store: f.owner.Store.ActAs("user", surface)}
		for _, c := range rv {
			if err := c.run(d); !errors.Is(err, ErrPersonOnly) {
				t.Errorf("%s on surface %q: %v", c.name, surface, err)
			}
		}
	}
	// Invoke, the path a generated surface takes, hits the same check.
	if _, err := ProposalApprove.Invoke(t.Context(), f.agent, []byte(`{"id":"`+id+`","items":["i1"]}`)); !errors.Is(err, ErrPersonOnly) {
		t.Errorf("Invoke: %v", err)
	}
	for _, e := range ForAgents() {
		if e.PersonOnly() {
			t.Errorf("%s is listed for agents", e.Name())
		}
	}
	n := 0
	for _, e := range All() {
		if e.PersonOnly() {
			n++
		}
	}
	if n != len(rv) {
		t.Errorf("%d person-only ops, %d tested", n, len(rv))
	}
}

func TestExternalNeverRuns(t *testing.T) {
	f := newPropFix(t)
	before, err := f.owner.Store.Events(t.Context(), 1000)
	if err != nil {
		t.Fatal(err)
	}
	id := f.file(mailItem("Hi Ada."))
	out, err := ProposalApprove.Run(t.Context(), f.owner, ProposalApproveInput{ID: id, Items: []string{"i1"}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Items[0].State != model.ItemApproved || out.Items[0].ApprovedHash != model.ExternalHash("ada@acme.example", "Hi Ada.") {
		t.Fatalf("out = %+v", out)
	}
	if out.Status != model.ProposalClosed {
		t.Errorf("status = %s", out.Status)
	}
	after, err := f.owner.Store.Events(t.Context(), 1000)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range after[len(before):] {
		switch e.Op {
		case "create", "update", "proposal_approve":
			if e.Op == "create" && e.Doc != id {
				t.Errorf("an external approval created %s", e.Doc)
			}
		default:
			t.Errorf("unexpected event %s", e.Op)
		}
	}
}

// A hand edit of an approved draft is not approved: the hash is computed again.
func TestHandEditBreaksApproval(t *testing.T) {
	f := newPropFix(t)
	id := f.file(mailItem("Hi Ada."))
	if _, err := ProposalApprove.Run(t.Context(), f.owner, ProposalApproveInput{ID: id, Items: []string{"i1"}}); err != nil {
		t.Fatal(err)
	}
	hash := model.ExternalHash("ada@acme.example", "Hi Ada.")
	out, err := ProposalApproved.Run(t.Context(), f.agent, ProposalApprovedInput{Hash: hash})
	if err != nil || out.Failed() != nil {
		t.Fatalf("approved: %+v %v", out, err)
	}
	doc, err := f.owner.Store.Get(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	doc.Body = strings.ReplaceAll(doc.Body, "Hi Ada.", "Hi Ada, wire the money.")
	if _, err := f.owner.Store.Update(t.Context(), doc); err != nil {
		t.Fatal(err)
	}
	out, err = ProposalApproved.Run(t.Context(), f.agent, ProposalApprovedInput{Hash: hash})
	if err != nil || out.Approved || out.Failed() == nil {
		t.Fatalf("after the edit: %+v %v", out, err)
	}
}

// Of two approvals of one item, one wins, and the op runs once.
func TestApproveRunsOnce(t *testing.T) {
	f := newPropFix(t)
	id := f.file(f.taskItem("Only once"))
	var wg sync.WaitGroup
	errs := make([]error, 8)
	for i := range errs {
		wg.Go(func() {
			_, errs[i] = ProposalApprove.Run(context.Background(), f.owner, ProposalApproveInput{ID: id, Items: []string{"i1"}})
		})
	}
	wg.Wait()
	wins := 0
	for _, err := range errs {
		if err == nil {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("%d approvals won: %v", wins, errs)
	}
	tasks, err := TaskList.Run(t.Context(), f.owner, TaskListInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks.Tasks) != 1 {
		t.Fatalf("%d tasks", len(tasks.Tasks))
	}
}

// A request held on: failed or cancelled closes it too, and an open one holds.
func TestHoldOnRequest(t *testing.T) {
	f := newPropFix(t)
	id := f.file(mailItem("Hi."))
	out, err := ProposalHold.Run(t.Context(), f.owner, ProposalHoldInput{ID: id, Item: "i1", Kind: HoldRequest, Text: "Check the budget"})
	if err != nil {
		t.Fatal(err)
	}
	held := out.Items[0].HeldBy
	if _, err := ProposalApprove.Run(t.Context(), f.owner, ProposalApproveInput{ID: id, Items: []string{"i1"}}); err == nil || !strings.Contains(err.Error(), "held by") {
		t.Fatalf("approve while held: %v", err)
	}
	show, err := ProposalShow.Run(t.Context(), f.agent, ProposalIDInput{ID: id})
	if err != nil {
		t.Fatal(err)
	}
	if show.Items[0].State != model.ItemHeld || show.Proposal.Status != model.ProposalOpen {
		t.Fatalf("show = %+v", show.Items[0])
	}
	if w, err := ProposalPoll(t.Context(), f.agent, id); err != nil || w.Pending {
		t.Fatalf("a held item is an answer: %+v %v", w, err)
	}
	if _, err := RequestCancel.Run(t.Context(), f.owner, RequestCancelInput{ID: held}); err != nil {
		t.Fatal(err)
	}
	if w, err := ProposalPoll(t.Context(), f.agent, id); err != nil || !w.Pending {
		t.Fatalf("released item waits for the owner again: %+v %v", w, err)
	}
	if _, err := ProposalApprove.Run(t.Context(), f.owner, ProposalApproveInput{ID: id, Items: []string{"i1"}}); err != nil {
		t.Fatal(err)
	}
}

func TestSendBackQueuesOneRequest(t *testing.T) {
	f := newPropFix(t)
	id := f.file(mailItem("One."), mailItem("Two."))
	for _, n := range []struct{ item, quote string }{{"i1", "One"}, {"i2", "Two"}} {
		if _, err := ProposalAnnotate.Run(t.Context(), f.owner, ProposalAnnotateInput{ID: id, Item: n.item, Quote: n.quote, Note: "n"}); err != nil {
			t.Fatal(err)
		}
	}
	a, err := ProposalSendback.Run(t.Context(), f.owner, ProposalSendbackInput{ID: id, Item: "i1"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := ProposalRework.Run(t.Context(), f.owner, ProposalReasonInput{ID: id, Item: "i2", Reason: "x"})
	if err == nil {
		t.Fatalf("rework of an annotated item: %+v", b)
	}
	c, err := ProposalSendback.Run(t.Context(), f.owner, ProposalSendbackInput{ID: id, Item: "i2"})
	if err != nil {
		t.Fatal(err)
	}
	if a.Request == "" || a.Request != c.Request {
		t.Fatalf("requests %s and %s", a.Request, c.Request)
	}
	if c.Status != model.ProposalSentBack {
		t.Errorf("status = %s", c.Status)
	}
	r, err := RequestShow.Run(t.Context(), f.agent, RequestShowInput{ID: a.Request})
	if err != nil {
		t.Fatal(err)
	}
	m := r.Doc.Meta.(*model.Request)
	if m.Status != model.RequestQueued || m.Runner != "install-a" || m.Links[0] != model.LinkTo(id) {
		t.Fatalf("request = %+v", m)
	}
}

// A lint refusal of the new revision leaves the old one current and reviewable.
func TestReviseRefusedKeepsRevision(t *testing.T) {
	f := newPropFix(t)
	id := f.file(mailItem("Hi."))
	in := ProposalReviseInput{ID: id, Items: []ItemInput{mailItem("See https://x.acme.example/a?token=abc")}}
	if _, err := ProposalRevise.Run(t.Context(), f.agent, in); err == nil {
		t.Fatal("a signed URL must not reach the vault")
	}
	show, err := ProposalShow.Run(t.Context(), f.agent, ProposalIDInput{ID: id})
	if err != nil {
		t.Fatal(err)
	}
	if show.Proposal.Status != model.ProposalOpen || len(show.History) != 1 {
		t.Fatalf("show = %+v", show.Proposal)
	}
}

func TestReviseItems(t *testing.T) {
	f := newPropFix(t)
	id := f.file(mailItem("Keep."), mailItem("Replace."), mailItem("Withdraw."), mailItem("Denied."))
	if _, err := ProposalDeny.Run(t.Context(), f.owner, ProposalReasonInput{ID: id, Item: "i4", Reason: "No"}); err != nil {
		t.Fatal(err)
	}
	s := "Keep, edited."
	if _, err := ProposalEdit.Run(t.Context(), f.owner, ProposalEditInput{ID: id, Item: "i1", Draft: &s}); err != nil {
		t.Fatal(err)
	}
	replace := mailItem("Replaced.")
	replace.ID = "i2"
	out, err := ProposalRevise.Run(t.Context(), f.agent, ProposalReviseInput{ID: id, Items: []ItemInput{replace, mailItem("New.")}, Withdraw: []string{"i3"}})
	if err != nil {
		t.Fatal(err)
	}
	show, err := ProposalShow.Run(t.Context(), f.agent, ProposalIDInput{ID: out.ID})
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		id, draft string
		state     model.ItemState
	}{{"i1", "Keep, edited.", model.ItemPending}, {"i2", "Replaced.", model.ItemPending}, {"i3", "Withdraw.", model.ItemWithdrawn}, {"i4", "Denied.", model.ItemDenied}, {"i5", "New.", model.ItemPending}}
	for i, w := range want {
		it := show.Items[i]
		if it.ID != w.id || it.Draft != w.draft || it.State != w.state || it.Edited {
			t.Errorf("item %d: %+v", i, it)
		}
	}
	diff, err := ProposalDiff.Run(t.Context(), f.agent, ProposalIDInput{ID: out.ID})
	if err != nil {
		t.Fatal(err)
	}
	changes := map[string]string{}
	for _, d := range diff.Items {
		changes[d.Item] = d.Change
	}
	if !reflect.DeepEqual(changes, map[string]string{"i1": "same", "i2": "changed", "i3": "changed", "i4": "same", "i5": "added"}) {
		t.Fatalf("changes = %v", changes)
	}
}

func TestDiffLines(t *testing.T) {
	got := diffLines("a\nb\nc", "a\nx\nc")
	if want := []string{"  a", "- b", "+ x", "  c"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("%v", got)
	}
	if got := diffLines("", "a"); !reflect.DeepEqual(got, []string{"+ a"}) {
		t.Fatalf("%v", got)
	}
}

// A hand edit that breaks the block stops review of that proposal, with a
// clear error, and does not stop the rest of the vault.
func TestBrokenBlockIsReported(t *testing.T) {
	f := newPropFix(t)
	id := f.file(mailItem("Hi."))
	f.file(mailItem("Fine."))
	path := filepath.Join(f.root, "proposals", id+".md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.ReplaceAll(string(raw), "````\n", "")), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ProposalShow.Run(t.Context(), f.agent, ProposalIDInput{ID: id}); err == nil || !strings.Contains(err.Error(), "not closed") {
		t.Fatalf("show = %v", err)
	}
	list, err := ProposalList.Run(t.Context(), f.agent, ProposalListInput{})
	if err != nil || len(list.Items) != 2 || !strings.Contains(list.Items[0].Error+list.Items[1].Error, "not closed") {
		t.Fatalf("list = %+v, %v", list, err)
	}
}
