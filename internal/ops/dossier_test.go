package ops

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
)

// dossierMD is an agent's dossier: a question, two changes in a bundle, a
// message, and two alternatives.
func dossierMD(f *agentFix) string {
	ev := "evidence: {quote: \"Ship it.\", locator: \"note line 1\"}\n"
	return "Context for the owner.\n\n```yaml para-dossier\ntldr: Pick a vendor and add the tasks\n```\n\n## Findings\n\nVendor B is cheaper.\n\n" +
		"```yaml para-question id=q1\ntitle: Which vendor?\nmulti: true\noptions: [A, B, C]\nrecommended: B\n" + ev + "```\n\n" +
		"## Changes\n\n" +
		"```yaml para-change id=c1\nop: task_add\ninput: {project: " + f.proj + ", title: Order nodes, done_when: Ordered.}\nhow: add a task\nimpact: one more task\nbundle: b1\ngroup: tasks\n" + ev + "```\n\n" +
		"```yaml para-change id=c2\nop: task_add\ninput: {project: " + f.proj + ", title: Pay vendor, done_when: Paid.}\nhow: add a task\nimpact: one more task\nbundle: b1\ngroup: tasks\n" + ev + "```\n\n" +
		"```yaml para-message id=m1\npeople: [ada@acme.example]\nbody: Hi Ada.\n" + ev + "```\n\n" +
		"```yaml para-action id=a1\nwhat: Book room A\nhow: calendar\nchoice: room\n" + ev + "```\n\n" +
		"```yaml para-action id=a2\nwhat: Book room B\nhow: calendar\nchoice: room\n" + ev + "```\n"
}

func fileDossierMD(t *testing.T, f *agentFix, md string) string {
	t.Helper()
	out, err := ProposalAdd.Run(writeCtx(t), f.agent, ProposalAddInput{Title: "Vendor pick", Markdown: md})
	if err != nil {
		t.Fatal(err)
	}
	return out.ID
}

func draft(t *testing.T, d Deps, id string, rev int, items ...DraftAnswer) (ProposalDraftOutput, error) {
	t.Helper()
	return ProposalDraft.Run(t.Context(), d, ProposalDraftInput{ID: id, Revision: rev, Items: items})
}

func TestDossierAddRefusesOwnerFields(t *testing.T) {
	f := newAgentFix(t)
	md := strings.Replace(dossierMD(f), "recommended: B\n", "recommended: B\nanswer: {choices: [B]}\n", 1)
	if _, err := ProposalAdd.Run(writeCtx(t), f.agent, ProposalAddInput{Title: "X", Markdown: md}); err == nil || !strings.Contains(err.Error(), "answer") {
		t.Fatalf("answer from the agent: %v", err)
	}
	id := fileDossierMD(t, f, dossierMD(f))
	show := mustRun(t, ProposalShow, f.owner, ProposalIDInput{ID: id})
	if !show.Dossier || len(show.Items) != 6 || show.Items[0].Kind != model.ItemQuestion || show.Items[0].Section != "findings" {
		t.Fatalf("show = %+v", show.Items)
	}
	for _, it := range show.Items {
		if it.State != model.ItemPending || it.Rev != 1 || it.Stack != StackProposed {
			t.Fatalf("%s: %s rev %d %s", it.ID, it.State, it.Rev, it.Stack)
		}
	}
	if !strings.Contains(show.Markdown, "Vendor B is cheaper.") || len(show.Sections) != 2 {
		t.Fatalf("markdown or sections: %+v", show.Sections)
	}
}

// Submit: an edit approves the edited text, a multi answer is recorded, a
// bundle runs as one, an approved message waits to be claimed, and a choice
// closes its alternative.
func TestDossierDraftAndSubmit(t *testing.T) {
	f := newAgentFix(t)
	id := fileDossierMD(t, f, dossierMD(f))
	edited := map[string]any{"project": f.proj, "title": "Order more nodes", "done_when": "Ordered."}
	out, err := draft(t, f.owner, id, 1,
		DraftAnswer{Item: "q1", Answer: &model.ItemAnswer{Choices: []string{"B", "D"}, AddedOptions: []string{"D"}, Comment: "D is new."}},
		DraftAnswer{Item: "c1", Answer: &model.ItemAnswer{Decision: model.AnswerEdit, Input: edited}},
		DraftAnswer{Item: "m1", Answer: &model.ItemAnswer{Decision: model.AnswerApprove}},
		DraftAnswer{Item: "a2", Answer: &model.ItemAnswer{Decision: model.AnswerApprove}})
	if err != nil || out.Drafted != 4 || out.Total != 6 {
		t.Fatalf("draft = %+v %v", out, err)
	}
	// A draft decides nothing.
	if tasks := mustRun(t, TaskList, f.owner, TaskListInput{}); len(tasks.Tasks) != 0 {
		t.Fatal("a draft ran an item")
	}
	resp := mustRun(t, ProposalResponse, f.agent, ProposalIDInput{ID: id})
	if resp.Response != model.ResponseDraft || resp.Items[0].Submitted || resp.Items[0].Answer.Via != "cli" {
		t.Fatalf("response = %+v", resp)
	}
	// A bad answer is refused.
	if _, err := draft(t, f.owner, id, 1, DraftAnswer{Item: "q1", Answer: &model.ItemAnswer{Choices: []string{"Z"}}}); err == nil {
		t.Fatal("a choice that is not an option was taken")
	}
	// An old revision is refused.
	if _, err := draft(t, f.owner, id, 7); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("stale revision: %v", err)
	}
	sub, err := ProposalSubmit.Run(t.Context(), f.owner, ProposalSubmitInput{ID: id, Revision: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(sub.Failed) != 0 {
		t.Fatalf("failed: %v", sub.Failed)
	}
	show := mustRun(t, ProposalShow, f.owner, ProposalIDInput{ID: id})
	state := map[string]model.ItemState{}
	for _, it := range show.Items {
		state[it.ID] = it.State
	}
	want := map[string]model.ItemState{"q1": model.ItemAnswered, "c1": model.ItemApproved, "c2": model.ItemApproved, "m1": model.ItemApproved,
		"a1": model.ItemDenied, "a2": model.ItemApproved}
	for k, v := range want {
		if state[k] != v {
			t.Errorf("%s = %s, want %s", k, state[k], v)
		}
	}
	if !show.Items[1].Edited || show.Items[1].Input["title"] != "Order more nodes" {
		t.Fatalf("c1 = %+v", show.Items[1])
	}
	tasks := mustRun(t, TaskList, f.owner, TaskListInput{})
	titles := map[string]bool{}
	for _, tk := range tasks.Tasks {
		titles[tk.Title] = true
	}
	if !titles["Order more nodes"] || !titles["Pay vendor"] || len(tasks.Tasks) != 2 {
		t.Fatalf("tasks = %v", titles)
	}
	resp = mustRun(t, ProposalResponse, f.agent, ProposalIDInput{ID: id})
	if resp.Response != model.ResponseSubmitted || !resp.Items[0].Submitted || resp.Items[0].Answer.Choices[1] != "D" {
		t.Fatalf("response = %+v", resp.Items[0])
	}
	if strings.Join(resp.Todo.Claim, ",") != "m1,a2" || strings.Join(resp.Todo.Answered, ",") != "q1" || strings.Join(resp.Todo.Reply, ",") != "q1" {
		t.Fatalf("todo = %+v", resp.Todo)
	}
	if len(resp.Items[1].Receipts) != 1 {
		t.Fatalf("c1 receipts = %v", resp.Items[1].Receipts)
	}
	// Reply, claim, execute.
	mustRun(t, ProposalReply, f.agent, ProposalReplyInput{ID: id, Item: "q1", Text: "Added D to the list."})
	cl, err := ProposalClaim.Run(t.Context(), f.agent, ProposalClaimInput{ID: id, Items: []string{"m1"}})
	if err != nil || len(cl.Claimed) != 1 {
		t.Fatalf("claim = %+v %v", cl, err)
	}
	if _, err := ProposalExecuted.Run(t.Context(), f.agent, ProposalExecutedInput{ID: id, Item: "m1", Result: ExecDone, URL: "https://mail.acme.example/1"}); err != nil {
		t.Fatal(err)
	}
	resp = mustRun(t, ProposalResponse, f.agent, ProposalIDInput{ID: id})
	if strings.Join(resp.Todo.Claim, ",") != "a2" || len(resp.Todo.Reply) != 0 {
		t.Fatalf("todo after = %+v", resp.Todo)
	}
	// Events hold ids and hashes, never answer text.
	raw := eventsText(t, f.root)
	if strings.Contains(raw, "D is new.") || strings.Contains(raw, "Order more nodes") || !strings.Contains(raw, "proposal_submit") {
		t.Fatal("events hold content, or no submit event")
	}
}

func eventsText(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	files, _ := filepath.Glob(filepath.Join(root, "events", "*.jsonl"))
	for _, p := range files {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(data)
	}
	return b.String()
}

// A question with for records its answer on that vault question.
func TestDossierAnswerFor(t *testing.T) {
	f := newAgentFix(t)
	q := mustRun(t, QuestionAdd, f.owner, QuestionAddInput{Text: "Which vendor?", Project: f.proj, Options: []string{"A", "B"}})
	md := "```yaml para-dossier\ntldr: t\n```\n\n```yaml para-question id=q1\ntitle: Which vendor?\noptions: [A, B]\nfor: " + q.ID + "\nevidence: {quote: q, locator: l}\n```\n"
	id := fileDossierMD(t, f, md)
	if _, err := draft(t, f.owner, id, 1, DraftAnswer{Item: "q1", Answer: &model.ItemAnswer{Choices: []string{"B"}}}); err != nil {
		t.Fatal(err)
	}
	mustRun(t, ProposalSubmit, f.owner, ProposalSubmitInput{ID: id, Revision: 1})
	got := mustRun(t, QuestionShow, f.owner, QuestionShowInput{ID: q.ID})
	if got.Question.Status != model.QuestionAnswered || got.Question.Choice != "B" {
		t.Fatalf("question = %+v", got.Question)
	}
	show := mustRun(t, ProposalShow, f.owner, ProposalIDInput{ID: id})
	if show.Items[0].State != model.ItemAnswered || show.Items[0].Result != q.ID || show.Items[0].Stack != StackDone {
		t.Fatalf("item = %+v", show.Items[0])
	}
}

// Revise in place: an unchanged item keeps its answer, a changed one keeps it
// marked stale and blocks submit, a dropped one is withdrawn.
func TestDossierReviseKeepsOrStalesAnswers(t *testing.T) {
	f := newAgentFix(t)
	md := dossierMD(f)
	id := fileDossierMD(t, f, md)
	if _, err := draft(t, f.owner, id, 1,
		DraftAnswer{Item: "q1", Answer: &model.ItemAnswer{Choices: []string{"B"}}},
		DraftAnswer{Item: "m1", Answer: &model.ItemAnswer{Decision: model.AnswerApprove}}); err != nil {
		t.Fatal(err)
	}
	next := strings.Replace(md, "body: Hi Ada.", "body: Hello Ada.", 1)
	next = next[:strings.Index(next, "```yaml para-action id=a1")]
	rv, err := ProposalRevise.Run(writeCtx(t), f.agent, ProposalReviseInput{ID: id, Markdown: next})
	if err != nil {
		t.Fatal(err)
	}
	if rv.ID != id || rv.Revision != 2 {
		t.Fatalf("revise = %+v", rv)
	}
	show := mustRun(t, ProposalShow, f.owner, ProposalIDInput{ID: id})
	by := map[string]ItemView{}
	for _, it := range show.Items {
		by[it.ID] = it
	}
	if a := by["q1"].Answer; a == nil || a.Stale || by["q1"].Rev != 1 {
		t.Fatalf("q1 = %+v", by["q1"])
	}
	if a := by["m1"].Answer; a == nil || !a.Stale || by["m1"].Rev != 2 || by["m1"].Body != "Hello Ada." {
		t.Fatalf("m1 = %+v", by["m1"])
	}
	if by["a1"].State != model.ItemWithdrawn || by["a2"].State != model.ItemWithdrawn {
		t.Fatalf("a1, a2 = %s %s", by["a1"].State, by["a2"].State)
	}
	if _, err := ProposalSubmit.Run(t.Context(), f.owner, ProposalSubmitInput{ID: id, Revision: 2}); err == nil || !strings.Contains(err.Error(), "m1") {
		t.Fatalf("submit with a stale answer: %v", err)
	}
	diff := mustRun(t, ProposalDiff, f.owner, ProposalIDInput{ID: id})
	changes := map[string]string{}
	for _, it := range diff.Items {
		changes[it.Item] = it.Change
	}
	if changes["m1"] != "changed" || changes["q1"] != "same" || !strings.HasPrefix(diff.Previous, "receipt-") {
		t.Fatalf("diff = %+v", diff)
	}
	// A decided item cannot change.
	if _, err := draft(t, f.owner, id, 2, DraftAnswer{Item: "m1", Answer: &model.ItemAnswer{Decision: model.AnswerApprove}}); err != nil {
		t.Fatal(err)
	}
	mustRun(t, ProposalSubmit, f.owner, ProposalSubmitInput{ID: id, Revision: 2})
	again := strings.Replace(next, "body: Hello Ada.", "body: Yo Ada.", 1)
	if _, err := ProposalRevise.Run(writeCtx(t), f.agent, ProposalReviseInput{ID: id, Markdown: again}); err == nil || !strings.Contains(err.Error(), "decided") {
		t.Fatalf("revise of a decided item: %v", err)
	}
}

// A claim holds until its lease ends; then another agent may take the item.
func TestDossierClaimLease(t *testing.T) {
	f := newAgentFix(t)
	id := fileDossierMD(t, f, dossierMD(f))
	if _, err := draft(t, f.owner, id, 1, DraftAnswer{Item: "m1", Answer: &model.ItemAnswer{Decision: model.AnswerApprove}}); err != nil {
		t.Fatal(err)
	}
	mustRun(t, ProposalSubmit, f.owner, ProposalSubmitInput{ID: id, Revision: 1})
	if _, err := ProposalClaim.Run(t.Context(), f.agent, ProposalClaimInput{ID: id, Items: []string{"m1"}, LeaseSeconds: 60}); err != nil {
		t.Fatal(err)
	}
	rv := &reqVault{root: f.root, t: t}
	rv.now.Store(time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC).UnixNano())
	other := rv.as("codex", "mcp", "install-a")
	if _, err := ProposalClaim.Run(t.Context(), other, ProposalClaimInput{ID: id, Items: []string{"m1"}}); err == nil {
		t.Fatal("a held claim was taken")
	}
	if _, err := ProposalExecuted.Run(t.Context(), other, ProposalExecutedInput{ID: id, Item: "m1", Result: ExecDone, URL: "https://x.example"}); err == nil {
		t.Fatal("another agent reported a held item")
	}
	rv.advance(2 * time.Minute)
	if out, err := ProposalClaim.Run(t.Context(), other, ProposalClaimInput{ID: id, Items: []string{"m1"}}); err != nil || out.Claimed[0] != "m1" {
		t.Fatalf("claim after the lease: %+v %v", out, err)
	}
	if out, err := ProposalClaim.Run(t.Context(), other, ProposalClaimInput{ID: id, Items: []string{"c1"}}); err == nil {
		t.Fatalf("claimed a vault change: %+v", out)
	}
}

// stack_decide keeps the bundle and choice rules, across dossiers.
func TestStackDecide(t *testing.T) {
	f := newAgentFix(t)
	one := fileDossierMD(t, f, dossierMD(f))
	two := fileDossierMD(t, f, strings.NewReplacer("Order nodes", "Order disks", "Pay vendor", "Pay the shop").Replace(dossierMD(f)))
	if _, err := StackDecide.Run(t.Context(), f.owner, StackDecideInput{Decision: "approve",
		Items: []StackItemRef{{Proposal: one, Item: "a1"}, {Proposal: one, Item: "a2"}}}); err == nil || !strings.Contains(err.Error(), "alternatives") {
		t.Fatalf("two alternatives: %v", err)
	}
	if _, err := StackDecide.Run(t.Context(), f.owner, StackDecideInput{Decision: "approve", Items: []StackItemRef{{Proposal: one, Item: "q1"}}}); err == nil {
		t.Fatal("approved a question")
	}
	// A group approves across dossiers; a bundle comes with it.
	out, err := StackDecide.Run(t.Context(), f.owner, StackDecideInput{Decision: "approve", Group: "tasks"})
	if err != nil || len(out.Proposals) != 2 {
		t.Fatalf("group = %+v %v", out, err)
	}
	if tasks := mustRun(t, TaskList, f.owner, TaskListInput{}); len(tasks.Tasks) != 4 {
		t.Fatalf("tasks = %d", len(tasks.Tasks))
	}
	// Rejecting needs a comment; one item of a bundle rejects the bundle.
	if _, err := StackDecide.Run(t.Context(), f.owner, StackDecideInput{Decision: "reject", Items: []StackItemRef{{Proposal: two, Item: "a1"}}}); err == nil || !strings.Contains(err.Error(), "comment") {
		t.Fatalf("reject with no comment: %v", err)
	}
	if _, err := StackDecide.Run(t.Context(), f.owner, StackDecideInput{Decision: "reject", Items: []StackItemRef{{Proposal: two, Item: "a1"}}, Comment: "No."}); err != nil {
		t.Fatal(err)
	}
	st := mustRun(t, ProposalStack, f.owner, ProposalStackInput{})
	counts := map[string]int{}
	for _, c := range st.Counts {
		counts[c.State] = c.Count
	}
	// Per dossier: q1, m1, a1, a2 proposed (minus two's a1), c1, c2 done.
	// The denied item stays listed, with its reason.
	if counts[StackProposed] != 7 || counts[StackDone] != 4 || counts[StackClosed] != 1 || st.Total != 12 {
		t.Fatalf("counts = %v total %d", counts, st.Total)
	}
	if len(st.Groups) != 2 || st.Groups[0].Done != 2 || st.Groups[0].Total != 2 {
		t.Fatalf("groups = %+v", st.Groups)
	}
	page := mustRun(t, ProposalStack, f.owner, ProposalStackInput{Limit: 5})
	if len(page.Rows) != 5 || page.NextCursor != "5" {
		t.Fatalf("page = %d %q", len(page.Rows), page.NextCursor)
	}
	closed := mustRun(t, ProposalStack, f.owner, ProposalStackInput{State: StackClosed})
	if len(closed.Rows) != 1 || closed.Rows[0].Item != "a1" || closed.Rows[0].Reason != "No." || !slices.ContainsFunc(st.Rows, func(r StackRow) bool { return r.Stack == StackClosed && r.Reason == "No." }) {
		t.Fatalf("closed = %+v", closed.Rows)
	}
}

// The gate: with the default, an agent write applies only when undoable,
// minor, and obvious; otherwise it is staged in a dossier and does not run.
func TestGateMatrix(t *testing.T) {
	f := newAgentFix(t)
	mustRun(t, VaultConfigUnset, f.owner, VaultConfigUnsetInput{Key: "gate.require"})
	add := func(m WriteMeta, title string) error {
		m.Why, m.NotDuplicate = "The owner asked.", "A test of the gate."
		_, err := TaskAdd.Run(WithWrite(t.Context(), m), f.agent, TaskAddInput{Project: f.proj, Title: title, DoneWhen: "Done."})
		return err
	}
	cases := []struct {
		name   string
		m      WriteMeta
		staged bool
	}{
		{"unrated", WriteMeta{}, true},
		{"minor only", WriteMeta{Minor: true}, true},
		{"obvious only", WriteMeta{Obvious: true}, true},
		{"both", WriteMeta{Minor: true, Obvious: true}, false},
		{"both but low", WriteMeta{Minor: true, Obvious: true, Confidence: model.ConfidenceLow}, true},
	}
	for i, c := range cases {
		err := add(c.m, "Gate task "+string(rune('a'+i)))
		s, staged := AsStaged(err)
		if staged != c.staged || (!staged && err != nil) {
			t.Fatalf("%s: staged %v, err %v", c.name, staged, err)
		}
		if staged && (s.Op != "task_add" || s.Item == "") {
			t.Fatalf("%s: %+v", c.name, s)
		}
	}
	// A review-risk op is never minor; a no-undo op is never undoable.
	err := func() error {
		_, err := TaskAssign.Run(WithWrite(t.Context(), WriteMeta{Why: "w", Minor: true, Obvious: true}), f.agent, TaskAssignInput{ID: "task-none", Assignee: "self"})
		return err
	}()
	if s, ok := AsStaged(err); !ok || !strings.Contains(strings.Join(s.Missing, ","), "minor") {
		t.Fatalf("review op: %v", err)
	}
	_, err = VaultConfigSet.Run(WithWrite(t.Context(), WriteMeta{Why: "w", Minor: true, Obvious: true}), f.agent, VaultConfigSetInput{Key: "align.days", Value: "30"})
	if s, ok := AsStaged(err); !ok || s.Missing[0] != GateUndoable {
		t.Fatalf("no-undo op: %v", err)
	}
	// Staged writes go to one dossier, grouped by op.
	st := mustRun(t, ProposalStack, f.owner, ProposalStackInput{Group: "task_add"})
	if len(st.Rows) != 4 || st.Rows[0].Title != stagedTitle("claude") {
		t.Fatalf("stack = %+v", st.Rows)
	}
	if tasks := mustRun(t, TaskList, f.owner, TaskListInput{}); len(tasks.Tasks) != 1 {
		t.Fatalf("tasks = %d", len(tasks.Tasks))
	}
	// Approving the group runs them, as the agent, with receipts.
	mustRun(t, StackDecide, f.owner, StackDecideInput{Decision: "approve", Group: "task_add"})
	if tasks := mustRun(t, TaskList, f.owner, TaskListInput{}); len(tasks.Tasks) != 5 {
		t.Fatalf("tasks after = %d", len(tasks.Tasks))
	}
	// An agent cannot set gate.require: staged, and approving it fails as the agent.
	_, err = VaultConfigSet.Run(WithWrite(t.Context(), WriteMeta{Why: "w"}), f.agent, VaultConfigSetInput{Key: "gate.require", Value: "none"})
	s, ok := AsStaged(err)
	if !ok {
		t.Fatalf("agent gate.require: %v", err)
	}
	r := mustRun(t, StackDecide, f.owner, StackDecideInput{Decision: "approve", Items: []StackItemRef{{Proposal: s.Proposal, Item: s.Item}}})
	if len(r.Proposals[0].Failed) != 1 {
		t.Fatalf("approved agent gate change: %+v", r)
	}
	if v := mustRun(t, VaultConfigGet, f.owner, VaultConfigGetInput{Key: "gate.require"}); v.Value != "" {
		t.Fatalf("gate.require = %q", v.Value)
	}
	mustRun(t, VaultConfigSet, f.owner, VaultConfigSetInput{Key: "gate.require", Value: "undoable"})
	if err := add(WriteMeta{}, "Gate task z"); err != nil {
		t.Fatalf("undoable only: %v", err)
	}
	// A named dossier takes the staged write; it must be the agent's own.
	mustRun(t, VaultConfigSet, f.owner, VaultConfigSetInput{Key: "gate.require", Value: "minor"})
	id := fileDossierMD(t, f, dossierMD(f))
	err = add(WriteMeta{Dossier: id}, "Into the dossier")
	if s, ok := AsStaged(err); !ok || s.Proposal != id || s.Item != "s1" {
		t.Fatalf("named dossier: %v", err)
	}
}

// A hand edit of an answer reads as a draft via file, and only a submit decides it.
func TestDossierHandEditIsADraft(t *testing.T) {
	f := newAgentFix(t)
	id := fileDossierMD(t, f, dossierMD(f))
	path := filepath.Join(f.root, "proposals", id+".md")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(raw), "body: Hi Ada.\n", "body: Hi Ada.\nanswer: {decision: approve}\n", 1)
	if err := os.WriteFile(path, []byte(edited), 0o600); err != nil {
		t.Fatal(err)
	}
	resp := mustRun(t, ProposalResponse, f.agent, ProposalIDInput{ID: id})
	m1 := resp.Items[3]
	if m1.ID != "m1" || m1.Answer == nil || m1.Answer.Via != model.ViaFile || m1.Submitted || m1.State != model.ItemPending {
		t.Fatalf("m1 = %+v", m1)
	}
	mustRun(t, ProposalSubmit, f.owner, ProposalSubmitInput{ID: id, Revision: 1})
	if resp = mustRun(t, ProposalResponse, f.agent, ProposalIDInput{ID: id}); resp.Items[3].State != model.ItemApproved {
		t.Fatalf("after submit: %s", resp.Items[3].State)
	}
	_ = context.Background
}

func TestSummaryOfQuestionAndDocEdit(t *testing.T) {
	f := newAgentFix(t)
	z := newItemSummarizer(t.Context(), f.owner)
	if got := z.of(model.ProposalItem{Kind: model.ItemQuestion, Title: "Which vendor?"}); got != "Question: Which vendor?" {
		t.Fatalf("question = %q", got)
	}
	if got := z.of(model.ProposalItem{Kind: model.ItemDocEdit, Target: "[[" + f.source + "]]"}); got != "Edit Sync notes" {
		t.Fatalf("doc edit = %q", got)
	}
}

// A reject submitted without a comment is refused; with one, the item is
// denied with the comment as its reason.
func TestSubmitRejectNeedsComment(t *testing.T) {
	f := newAgentFix(t)
	id := fileDossierMD(t, f, dossierMD(f))
	if _, err := draft(t, f.owner, id, 1, DraftAnswer{Item: "m1", Answer: &model.ItemAnswer{Decision: model.AnswerReject}}); err != nil {
		t.Fatal(err)
	}
	if _, err := ProposalSubmit.Run(t.Context(), f.owner, ProposalSubmitInput{ID: id, Revision: 1}); err == nil || !strings.Contains(err.Error(), "comment") {
		t.Fatalf("reject with no comment: %v", err)
	}
	if _, err := draft(t, f.owner, id, 1, DraftAnswer{Item: "m1", Answer: &model.ItemAnswer{Decision: model.AnswerReject, Comment: "Wrong person."}}); err != nil {
		t.Fatal(err)
	}
	mustRun(t, ProposalSubmit, f.owner, ProposalSubmitInput{ID: id, Revision: 1})
	st := mustRun(t, ProposalStack, f.owner, ProposalStackInput{Proposal: id})
	if !slices.ContainsFunc(st.Rows, func(r StackRow) bool {
		return r.Item == "m1" && r.State == model.ItemDenied && r.Reason == "Wrong person."
	}) {
		t.Fatalf("rows = %+v", st.Rows)
	}
}

// Skip leaves an item undecided: it stays in the stack as skipped (proposed),
// the agent reads the skip and the note, and a later submit decides it.
func TestSubmitSkip(t *testing.T) {
	f := newAgentFix(t)
	id := fileDossierMD(t, f, dossierMD(f))
	if _, err := draft(t, f.owner, id, 1, DraftAnswer{Item: "m1", Answer: &model.ItemAnswer{Decision: model.AnswerSkip, Body: "x"}}); err == nil {
		t.Fatal("skip with an edit was accepted")
	}
	if _, err := draft(t, f.owner, id, 1,
		DraftAnswer{Item: "m1", Answer: &model.ItemAnswer{Decision: model.AnswerSkip, Comment: "Ask me after the launch."}},
		DraftAnswer{Item: "q1", Answer: &model.ItemAnswer{Decision: model.AnswerSkip}}); err != nil {
		t.Fatal(err)
	}
	mustRun(t, ProposalSubmit, f.owner, ProposalSubmitInput{ID: id, Revision: 1})
	st := mustRun(t, ProposalStack, f.owner, ProposalStackInput{Proposal: id})
	for _, item := range []string{"m1", "q1"} {
		if !slices.ContainsFunc(st.Rows, func(r StackRow) bool { return r.Item == item && r.State == model.ItemSkipped && r.Stack == StackProposed }) {
			t.Fatalf("%s is not skipped in the stack: %+v", item, st.Rows)
		}
	}
	resp := mustRun(t, ProposalResponse, f.agent, ProposalIDInput{ID: id})
	var m1 ResponseItem
	for _, it := range resp.Items {
		if it.ID == "m1" {
			m1 = it
		}
	}
	if m1.State != model.ItemSkipped || m1.Decision != model.AnswerSkip || m1.Answer == nil || m1.Answer.Comment != "Ask me after the launch." {
		t.Fatalf("response m1 = %+v", m1)
	}
	if resp.Status != model.ProposalOpen {
		t.Fatalf("status = %s", resp.Status)
	}
	// A later submit decides it.
	if _, err := draft(t, f.owner, id, 1, DraftAnswer{Item: "m1", Answer: &model.ItemAnswer{Decision: model.AnswerApprove}}); err != nil {
		t.Fatal(err)
	}
	mustRun(t, ProposalSubmit, f.owner, ProposalSubmitInput{ID: id, Revision: 1})
	show := mustRun(t, ProposalShow, f.owner, ProposalIDInput{ID: id})
	for _, it := range show.Items {
		if it.ID == "m1" && it.State != model.ItemApproved {
			t.Fatalf("m1 after the later submit = %s", it.State)
		}
	}
	mustLintClean(t, f.owner)
}
