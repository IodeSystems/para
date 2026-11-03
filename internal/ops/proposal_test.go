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

// propFix is the proposal tests' name for agentFix.
type propFix = agentFix

func newPropFix(t *testing.T) *propFix { return newAgentFix(t) }

// TestReviewRule: a pending item can be approved, denied, or changed; nothing
// else can be decided.
func TestReviewRule(t *testing.T) {
	f := newPropFix(t)
	id := f.file(mailItem("Hi."), mailItem("Two."))
	if _, err := ProposalDeny.Run(t.Context(), f.owner, ProposalReasonInput{ID: id, Item: "i2", Reason: "No"}); err != nil {
		t.Fatal(err)
	}
	show, err := ProposalShow.Run(t.Context(), f.owner, ProposalIDInput{ID: id})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{ActionApprove, ActionDeny, ActionChange}; !reflect.DeepEqual(show.Items[0].Actions, want) {
		t.Errorf("pending: %v, want %v", show.Items[0].Actions, want)
	}
	if len(show.Items[1].Actions) != 0 {
		t.Errorf("denied: %v", show.Items[1].Actions)
	}
	if _, err := ProposalApprove.Run(t.Context(), f.owner, ProposalApproveInput{ID: id, Items: []string{"i2"}}); err == nil {
		t.Error("a denied item was approved")
	}
}

var ev = model.Evidence{Quote: "Ship it.", Locator: "line 1"}

func mailItem(draft string) ItemInput {
	return ItemInput{Kind: model.ItemExternal, Medium: "email", Target: "ada@acme.example", Draft: draft, Evidence: ev}
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
		{"request accept", func(d Deps) error {
			_, err := RequestAcceptOp.Run(t.Context(), d, RequestAcceptInput{ID: "request-x"})
			return err
		}},
		{"investigate", func(d Deps) error {
			_, err := Investigate.Run(t.Context(), d, InvestigateInput{ID: "question-x", Hint: "wiki"})
			return err
		}},
		{"delegate", func(d Deps) error {
			_, err := TaskDelegate.Run(t.Context(), d, TaskDelegateInput{ID: "task-x", Agent: "scribe"})
			return err
		}},
		{"update", func(d Deps) error {
			_, err := TaskUpdate.Run(t.Context(), d, TaskUpdateInput{ID: "task-x", Text: "note"})
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
		{"goal_rank", func(d Deps) error {
			_, err := GoalRank.Run(t.Context(), d, GoalRankInput{ID: "project-x", Rank: 1})
			return err
		}},
		{"vault_upgrade", func(d Deps) error {
			_, err := VaultUpgrade.Run(t.Context(), d, VaultUpgradeInput{DryRun: true})
			return err
		}},
		{"respond", func(d Deps) error {
			_, err := RequestRespond.Run(t.Context(), d, RequestRespondInput{ID: "x", Kind: RespondClarify, Text: "t"})
			return err
		}},
		{"receipt check", func(d Deps) error {
			_, err := ReceiptCheck.Run(t.Context(), d, ReceiptCheckInput{ID: "x", Verdict: model.ReceiptVerified})
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
	changed, err := ProposalDecide.Run(t.Context(), f.agent, ProposalDecideInput{ID: id, OwnerSaid: "Say keep, edited.",
		Items: []DecideItem{{Item: "i1", Decision: model.DecideChange, Text: "Keep, edited."}}})
	if err != nil {
		t.Fatal(err)
	}
	id = changed.Revision
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
		if it.ID != w.id || it.Body != w.draft || it.State != w.state || it.Edited {
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

// reviewItem is a message with every required field.
func reviewItem(body string) ItemInput {
	return ItemInput{Kind: model.ItemMessage, Subtype: "email", People: []string{"ada@acme.example"}, Body: body,
		Reversible: model.ReversibleNo, Evidence: model.Evidence{Quote: "Ship it.", Locator: "line 1"}}
}

func (f *propFix) add(items ...ItemInput) string {
	f.t.Helper()
	out, err := ProposalAdd.Run(f.t.Context(), f.agent, ProposalAddInput{Title: "Plan", TLDR: "Do it.", Context: "We agreed.", Source: f.source, Items: items})
	if err != nil {
		f.t.Fatal(err)
	}
	return out.ID
}

func paraTask(f *propFix, title string) ItemInput {
	return ItemInput{Kind: model.ItemPara, Op: "task_add", How: "Adds a task.", Impact: "One more task.", Reversible: model.ReversibleYes,
		Input:    map[string]any{"project": f.proj, "title": title, "done_when": "It exists."},
		Evidence: model.Evidence{Quote: "Ship it.", Locator: "line 1"}}
}

func TestOldProposalFileReads(t *testing.T) {
	f := newPropFix(t)
	body := "Written by para.\n\n````yaml para-proposal\nitems:\n  - id: i1\n    kind: external\n    medium: email\n    target: a@acme.example\n    draft: Hi\n    evidence:\n      quote: q\n      locator: l\n    state: rework\n    reason: again\n  - id: i2\n    kind: op\n    op: task_start\n    input:\n      id: x\n    evidence:\n      quote: q\n      locator: l\n    state: pending\nannotations:\n  - id: a1\n    item: i2\n    quote: task\n    note: Why?\n    by: user\n    revision: 1\n    state: open\n````"
	doc, err := f.owner.Store.Create(t.Context(), model.Document{ID: "proposal-old", Folder: model.FolderProposals, Title: "Old", Body: body,
		Meta: &model.Proposal{Common: model.Common{Type: model.TypeProposal}, Source: model.LinkTo(f.source), Status: model.ProposalOpen, Revision: 1, ProposedBy: "claude"}})
	if err != nil {
		t.Fatal(err)
	}
	show, err := ProposalShow.Run(t.Context(), f.owner, ProposalIDInput{ID: doc.ID})
	if err != nil {
		t.Fatal(err)
	}
	a, b := show.Items[0], show.Items[1]
	if a.Kind != model.ItemAction || a.Body != "Hi" || a.Subtype != "email" || a.State != model.ItemSentBack {
		t.Fatalf("external = %+v", a)
	}
	if b.Kind != model.ItemPara || len(b.Thread) != 1 || b.Thread[0].State != model.TurnOpen || b.State != model.ItemPending {
		t.Fatalf("op = %+v", b)
	}
	if a.Hash != model.ExternalHash("a@acme.example", "Hi") {
		t.Fatalf("old hash changed: %s", a.Hash)
	}
}

func TestNewKindsRequireFields(t *testing.T) {
	f := newPropFix(t)
	bad := []struct {
		name string
		in   ItemInput
		want string
	}{
		{"message with nobody", ItemInput{Kind: model.ItemMessage, Body: "x", Reversible: "no", Evidence: model.Evidence{Quote: "q", Locator: "l"}}, "people or a channel"},
		{"action with no what", ItemInput{Kind: model.ItemAction, Body: "x", Reversible: "no", Evidence: model.Evidence{Quote: "q", Locator: "l"}}, "what it is"},
		{"no reversible", ItemInput{Kind: model.ItemMessage, Channel: "#x", Body: "x", Evidence: model.Evidence{Quote: "q", Locator: "l"}}, "reversible"},
	}
	for _, c := range bad {
		_, err := ProposalAdd.Run(t.Context(), f.agent, ProposalAddInput{Title: "T", TLDR: "t", Context: "c", Source: f.source, Items: []ItemInput{c.in}})
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	if _, err := ProposalAdd.Run(t.Context(), f.agent, ProposalAddInput{Title: "T", Source: f.source, Items: []ItemInput{reviewItem("x")}}); err == nil || !strings.Contains(err.Error(), "tldr") {
		t.Errorf("no tldr: %v", err)
	}
	unsourced := reviewItem("x")
	unsourced.Pros = []model.Claim{{Text: "Good.", Source: model.NoSource}}
	unsourced.Confidence = 90
	if _, err := ProposalAdd.Run(t.Context(), f.agent, ProposalAddInput{Title: "T", TLDR: "t", Context: "c", Source: f.source, Items: []ItemInput{unsourced}}); err == nil || !strings.Contains(err.Error(), "must be low") {
		t.Errorf("no source at high confidence: %v", err)
	}
}

func TestChoiceClosesOthers(t *testing.T) {
	f := newPropFix(t)
	a, b := reviewItem("Plan A."), reviewItem("Plan B.")
	a.Choice, b.Choice = "c1", "c1"
	id := f.add(a, b)
	if _, err := ProposalApprove.Run(t.Context(), f.owner, ProposalApproveInput{ID: id, Items: []string{"i2"}, Reason: "Cheaper."}); err != nil {
		t.Fatal(err)
	}
	show, _ := ProposalShow.Run(t.Context(), f.owner, ProposalIDInput{ID: id})
	if show.Items[1].State != model.ItemApproved || show.Items[0].State != model.ItemDenied || !strings.Contains(show.Items[0].Reason, "Cheaper") {
		t.Fatalf("items = %+v", show.Items)
	}
}

func TestBundleApprovesInOrderAndHalts(t *testing.T) {
	g := newPropFix(t)
	a, b, c := paraTask(g, "First"), paraTask(g, "Second"), paraTask(g, "Third")
	b.Op, b.Input = "task_start", map[string]any{"id": "task-missing"}
	a.Bundle, b.Bundle, c.Bundle = "b", "b", "b"
	b.After, c.After = "i1", "i2"
	pid := g.add(a, b, c)
	out, err := ProposalApprove.Run(t.Context(), g.owner, ProposalApproveInput{ID: pid, Items: []string{"i3"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 3 || out.Items[0].State != model.ItemApproved || out.Items[1].State != model.ItemFailed || out.Items[2].State != model.ItemHalted {
		t.Fatalf("items = %+v", out.Items)
	}
}

func TestPlaceholderMustFollowItem(t *testing.T) {
	f := newPropFix(t)
	a, b := reviewItem("Open the PR."), reviewItem("See {{i1.url}}.")
	if _, err := ProposalAdd.Run(t.Context(), f.agent, ProposalAddInput{Title: "T", TLDR: "t", Context: "c", Source: f.source, Items: []ItemInput{a, b}}); err == nil || !strings.Contains(err.Error(), "does not run after") {
		t.Fatalf("placeholder without after: %v", err)
	}
	b.After = "i1"
	id := f.add(a, b)
	show, _ := ProposalShow.Run(t.Context(), f.owner, ProposalIDInput{ID: id})
	if !strings.Contains(show.Items[1].Body, "{{i1.url}}") {
		t.Fatalf("placeholder changed: %+v", show.Items[1])
	}
}

func TestParaItemPreview(t *testing.T) {
	f := newPropFix(t)
	waits, err := TaskAdd.Run(t.Context(), f.owner, TaskAddInput{Project: f.proj, Title: "Waits"})
	if err != nil {
		t.Fatal(err)
	}
	dep, err := TaskAdd.Run(t.Context(), f.owner, TaskAddInput{Project: f.proj, Title: "Base"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := TaskDependsAdd.Run(t.Context(), f.owner, TaskDependsInput{ID: waits.ID, DependsOn: []string{dep.ID}}); err != nil {
		t.Fatal(err)
	}
	it := paraTask(f, "x")
	it.Op, it.Input = "task_done", map[string]any{"id": dep.ID}
	id := f.add(it)
	show, err := ProposalShow.Run(t.Context(), f.owner, ProposalIDInput{ID: id})
	if err != nil {
		t.Fatal(err)
	}
	pv := show.Items[0].Preview
	if pv == nil || pv.Error != "" || len(pv.Changes) != 1 || pv.Changes[0].Doc != dep.ID {
		t.Fatalf("preview = %+v", pv)
	}
	if !strings.Contains(strings.Join(pv.Changes[0].Lines, "\n"), "+ status: done") {
		t.Fatalf("lines = %v", pv.Changes[0].Lines)
	}
	if len(pv.Downstream) != 1 || pv.Downstream[0].Doc != waits.ID {
		t.Fatalf("downstream = %+v", pv.Downstream)
	}
	after, _ := TaskShow.Run(t.Context(), f.owner, TaskShowInput{ID: dep.ID})
	if after.Task.Status == model.TaskDone {
		t.Fatal("the preview wrote to the vault")
	}
}

func TestSubtypeRules(t *testing.T) {
	f := newPropFix(t)
	if err := os.WriteFile(filepath.Join(f.root, ".para", "proposals.yaml"), []byte("subtypes:\n  deploy:\n    required: [rollback]\n    default_risk: high\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	it := ItemInput{Kind: model.ItemAction, Subtype: "deploy", What: "Ship v2", How: "Rolls out v2.", Reversible: model.ReversiblePartly, ReversibleHow: "Roll back.", Evidence: model.Evidence{Quote: "q", Locator: "l"}}
	_, err := ProposalAdd.Run(t.Context(), f.agent, ProposalAddInput{Title: "T", TLDR: "t", Context: "c", Source: f.source, Items: []ItemInput{it}})
	if err == nil || !strings.Contains(err.Error(), "needs rollback") {
		t.Fatalf("err = %v", err)
	}
	it.Fields = map[string]any{"rollback": "helm rollback"}
	id := f.add(it)
	show, _ := ProposalShow.Run(t.Context(), f.owner, ProposalIDInput{ID: id})
	if show.Items[0].Risk != "high" {
		t.Fatalf("risk = %q", show.Items[0].Risk)
	}
}

func TestApproveByGroup(t *testing.T) {
	f := newPropFix(t)
	a, b, c := paraTask(f, "One"), paraTask(f, "Two"), reviewItem("Hi.")
	a.Group, b.Group = "tasks", "tasks"
	id := f.add(a, b, c)
	out, err := ProposalApprove.Run(t.Context(), f.owner, ProposalApproveInput{ID: id, Group: "tasks"})
	if err != nil || len(out.Items) != 2 {
		t.Fatalf("out = %+v, %v", out, err)
	}
	if out.Status != model.ProposalOpen {
		t.Fatalf("status = %s", out.Status)
	}
}

func TestExecutionFlow(t *testing.T) {
	f := newPropFix(t)
	pr := ItemInput{Kind: model.ItemAction, Subtype: "pr", What: "Open the PR", How: "Adds the fix.", Target: "acme/api", Title: "Fix", Body: "The body.",
		Reversible: model.ReversibleYes, Evidence: model.Evidence{Quote: "q", Locator: "l"}}
	note := ItemInput{Kind: model.ItemMessage, Channel: "#ops", Body: "See {{i1.url}}.", Subtype: "slack", After: "i1",
		Reversible: model.ReversibleNo, Evidence: model.Evidence{Quote: "q", Locator: "l"}}
	task := paraTask(f, "Watch the rollout of {{i1.url}}")
	task.After = "i1"
	id := f.add(pr, note, task)
	if _, err := ProposalApprove.Run(t.Context(), f.owner, ProposalApproveInput{ID: id, Items: []string{"i1", "i2", "i3"}}); err != nil {
		t.Fatal(err)
	}
	un, _ := ProposalUndone.Run(t.Context(), f.owner, ProposalUndoneInput{})
	if len(un.Items) != 2 {
		t.Fatalf("undone = %+v", un.Items)
	}
	// i2 runs after i1: its gate is shut, and it cannot be reported done.
	if _, err := ProposalExecuted.Run(t.Context(), f.agent, ProposalExecutedInput{ID: id, Item: "i2", Result: ExecDone, URL: "https://x.example/1"}); err == nil {
		t.Fatal("reported before its after")
	}
	done, err := ProposalExecuted.Run(t.Context(), f.agent, ProposalExecutedInput{ID: id, Item: "i1", Result: ExecDone, URL: "https://github.example/acme/api/pull/7"})
	if err != nil {
		t.Fatal(err)
	}
	if !done.SelfReported || done.Receipt == "" {
		t.Fatalf("done = %+v", done)
	}
	show, _ := ProposalShow.Run(t.Context(), f.owner, ProposalIDInput{ID: id})
	if show.Items[1].Hash != show.Items[1].ApprovedHash || !strings.Contains(show.Items[1].Filled, "pull/7") {
		t.Fatalf("i2 = %+v", show.Items[1])
	}
	// The para item that waited on i1 ran, with the value filled.
	if show.Items[2].State != model.ItemApproved || show.Items[2].Waits || show.Items[2].Result == "" {
		t.Fatalf("i3 = %+v", show.Items[2])
	}
	made, _ := TaskShow.Run(t.Context(), f.owner, TaskShowInput{ID: show.Items[2].Result})
	if !strings.Contains(made.Task.Title, "pull/7") {
		t.Fatalf("title = %q", made.Task.Title)
	}
	// action_record with the proposal and item reports it too.
	second, err := ActionRecord.Run(t.Context(), f.agent, ActionRecordInput{What: "Posted the note", Where: "#ops 12:00", Proposal: id, Item: "i2"})
	if err != nil || second.State != model.ItemDone || second.Receipt == "" {
		t.Fatalf("action_record = %+v %v", second, err)
	}
	act, err := AgentActivity.Run(t.Context(), f.owner, AgentActivityInput{Actor: "claude"})
	if err != nil || len(act.Items) == 0 {
		t.Fatalf("activity = %+v %v", act, err)
	}
	if u, _ := ProposalUndone.Run(t.Context(), f.owner, ProposalUndoneInput{}); len(u.Items) != 0 {
		t.Fatalf("undone = %+v", u.Items)
	}
}

func TestBundleHaltsOnFailedExecution(t *testing.T) {
	f := newPropFix(t)
	a, b := reviewItem("First."), reviewItem("Second.")
	a.Bundle, b.Bundle, b.After = "b", "b", "i1"
	id := f.add(a, b)
	if _, err := ProposalApprove.Run(t.Context(), f.owner, ProposalApproveInput{ID: id, Items: []string{"i2"}}); err != nil {
		t.Fatal(err)
	}
	out, err := ProposalExecuted.Run(t.Context(), f.agent, ProposalExecutedInput{ID: id, Item: "i1", Result: ExecFailed, Reason: "The API refused."})
	if err != nil || out.State != model.ItemFailed || len(out.Halted) != 1 || out.Halted[0] != "i2" {
		t.Fatalf("out = %+v %v", out, err)
	}
}

func TestNoteEdit(t *testing.T) {
	f := newPropFix(t)
	n, err := NoteAdd.Run(t.Context(), f.owner, NoteAddInput{Title: "Old title", Body: "Old."})
	if err != nil {
		t.Fatal(err)
	}
	title := "New title"
	body := "New."
	if _, err := NoteEdit.Run(t.Context(), f.owner, NoteEditInput{ID: n.ID, Title: &title, Body: &body}); err != nil {
		t.Fatal(err)
	}
	got, _ := NoteShow.Run(t.Context(), f.owner, NoteShowInput{ID: n.ID})
	if got.Note.Title != title || got.Body != body {
		t.Fatalf("got %+v", got)
	}
	id := f.add(ItemInput{Kind: model.ItemPara, Op: "note_edit", Input: map[string]any{"id": n.ID, "body": "Via proposal."},
		How: "Edits it.", Impact: "One note.", Reversible: model.ReversibleYes, Evidence: ev})
	if _, err := ProposalApprove.Run(t.Context(), f.owner, ProposalApproveInput{ID: id, Items: []string{"i1"}}); err != nil {
		t.Fatal(err)
	}
	got, _ = NoteShow.Run(t.Context(), f.owner, NoteShowInput{ID: n.ID})
	if got.Body != "Via proposal." {
		t.Fatalf("body = %q", got.Body)
	}
}
