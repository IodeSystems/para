package ops

import (
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

func opItem(op string, in map[string]any) ItemInput {
	return ItemInput{Kind: model.ItemPara, Op: op, Input: in, How: "Adds it.", Impact: "One more.", Reversible: model.ReversibleYes, Evidence: ev}
}

func (f *propFix) tryFile(items ...ItemInput) (ProposalFiled, error) {
	return ProposalAdd.Run(f.t.Context(), f.agent, ProposalAddInput{Title: "Changes", TLDR: "Add things.", Context: "From a note.", Source: f.source, Items: items})
}

func TestOpItemReportsAllUnknownFields(t *testing.T) {
	f := newPropFix(t)
	_, err := f.tryFile(opItem("task_add", map[string]any{"project": f.proj, "title": "A", "done_when": "Done.", "zeta": 1, "alpha": 2}))
	if err == nil || !strings.Contains(err.Error(), "item 1: op task_add: input has unknown fields: alpha, zeta") || !strings.Contains(err.Error(), "tool task_add") {
		t.Fatalf("err = %v", err)
	}
}

func TestOpItemWhySourcesNotDuplicateReachTheReceipt(t *testing.T) {
	f := newPropFix(t)
	in := map[string]any{"project": f.proj, "title": "Receipt task", "done_when": "Done.", "why": "Ada asked for it.", "sources": []any{"note-x line 3"}, "not_duplicate": "No such task."}
	out, err := f.tryFile(opItem("task_add", in))
	if err != nil {
		t.Fatal(err)
	}
	dec, err := ProposalDecide.Run(t.Context(), f.agent, ProposalDecideInput{ID: out.ID, OwnerSaid: "Yes.", Items: decideOne("i1", model.DecideApprove, "")})
	if err != nil {
		t.Fatal(err)
	}
	rl, err := ReceiptList.Run(t.Context(), f.owner, ReceiptListInput{Doc: dec.Items[0].Result})
	if err != nil || len(rl.Items) != 1 {
		t.Fatalf("receipts = %+v, %v", rl.Items, err)
	}
	r := rl.Items[0]
	if r.Why != "Ada asked for it." || r.NotDuplicate != "No such task." || len(r.Sources) != 2 || r.Sources[0] != "note-x line 3" {
		t.Fatalf("receipt = %+v", r)
	}
}

func TestProposalSourceMustBeADocumentID(t *testing.T) {
	f := newPropFix(t)
	_, err := ProposalAdd.Run(t.Context(), f.agent, ProposalAddInput{Title: "T", Source: "/home/a/notes.md", Items: []ItemInput{mailItem("Hi.")}})
	if err == nil || !strings.HasSuffix(err.Error(), `source "/home/a/notes.md" is not a document id in this vault; put a file path or other provenance in source_path`) {
		t.Fatalf("err = %v", err)
	}
	out, err := ProposalAdd.Run(t.Context(), f.agent, ProposalAddInput{Title: "T", Source: f.source, SourcePath: "/home/a/notes.md", Items: []ItemInput{mailItem("Hi.")}})
	if err != nil {
		t.Fatal(err)
	}
	show, err := ProposalShow.Run(t.Context(), f.owner, ProposalIDInput{ID: out.ID})
	if err != nil || show.Proposal.SourcePath != "/home/a/notes.md" {
		t.Fatalf("show = %+v, %v", show.Proposal, err)
	}
}

func TestProposalLinksResolveToDocumentsOrEarlierItems(t *testing.T) {
	f := newPropFix(t)
	goal := opItem("project_add", map[string]any{"id": "project-later", "title": "Later goal", "done_when": "It ships."})
	task := opItem("task_add", map[string]any{"project": "project-later", "title": "A task", "done_when": "Done."})
	out, err := f.tryFile(goal, task)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ProposalDecide.Run(t.Context(), f.agent, ProposalDecideInput{ID: out.ID, OwnerSaid: "Yes.", Items: append(decideOne("i1", model.DecideApprove, ""), decideOne("i2", model.DecideApprove, "")...)}); err != nil {
		t.Fatal(err)
	}
	if _, err := ProjectShow.Run(t.Context(), f.owner, ProjectShowInput{ID: "project-later"}); err != nil {
		t.Fatal(err)
	}
	// A later item cannot be the creator, and every problem is listed.
	_, err = f.tryFile(
		opItem("task_add", map[string]any{"project": "project-b", "title": "One", "done_when": "Done."}),
		opItem("project_add", map[string]any{"id": "project-b", "title": "B", "done_when": "Done."}),
		opItem("task_add", map[string]any{"project": "project-c", "area": "area-d", "title": "Two", "done_when": "Done."}),
	)
	if err == nil || !strings.Contains(err.Error(), `item 1: project "project-b"`) || !strings.Contains(err.Error(), `item 3: area "area-d"`) || !strings.Contains(err.Error(), `item 3: project "project-c"`) {
		t.Fatalf("err = %v", err)
	}
}

func TestTaskListOpen(t *testing.T) {
	f := newPropFix(t)
	a, _ := TaskAdd.Run(t.Context(), f.owner, TaskAddInput{Project: f.proj, Title: "Open one", DoneWhen: "Done."})
	b, _ := TaskAdd.Run(t.Context(), f.owner, TaskAddInput{Project: f.proj, Title: "Closed one", DoneWhen: "Done."})
	if _, err := TaskDone.Run(t.Context(), f.owner, TaskDoneInput{ID: b.ID}); err != nil {
		t.Fatal(err)
	}
	got, err := TaskList.Run(t.Context(), f.owner, TaskListInput{Open: true})
	if err != nil || len(got.Tasks) != 1 || got.Tasks[0].ID != a.ID {
		t.Fatalf("open = %+v, %v", got.Tasks, err)
	}
	if _, err := TaskList.Run(t.Context(), f.owner, TaskListInput{Open: true, Status: model.TaskDone}); err == nil {
		t.Fatal("open with status was accepted")
	}
}
