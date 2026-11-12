package ops

import (
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

// task_split closes the done parts with evidence, leaves the remaining parts
// open, and makes the task wait on them, in one receipt that one undo takes
// back. An agent must give evidence; no remaining part is refused.
func TestTaskSplit(t *testing.T) {
	f := newThreadFix(t)
	_, err := TaskSplit.Run(writeCtx(t), f.agent, TaskSplitInput{ID: f.task, Done: []SplitDone{{Title: "Draft the post"}}, Remaining: []SplitRemaining{{Title: "Publish the post"}}, Reason: "The draft is up."})
	if err == nil || !strings.Contains(err.Error(), "evidence is required") {
		t.Fatalf("agent split without evidence: %v", err)
	}
	if _, err := TaskSplit.Run(writeCtx(t), f.agent, TaskSplitInput{ID: f.task, Reason: "All done."}); err == nil || !strings.Contains(err.Error(), "no remaining part") {
		t.Fatalf("no remaining: %v", err)
	}
	sp, err := TaskSplit.Run(writeCtx(t), f.agent, TaskSplitInput{ID: f.task, Title: "Launch post",
		Done:      []SplitDone{{Title: "Draft the post", Evidence: &model.Evidence{Locator: "https://wiki.acme.example/post-draft"}}},
		Remaining: []SplitRemaining{{Title: "Publish the post", Due: "2026-11-20"}}, Reason: "The draft is up; publishing remains."})
	if err != nil || len(sp.Done) != 1 || len(sp.Remaining) != 1 {
		t.Fatalf("split = %+v %v", sp, err)
	}
	doc, task, err := getTask(t.Context(), f.owner.Store, f.task)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Title != "Launch post" || taskTerminal(task.Status) || len(task.DependsOn) != 1 || task.DependsOn[0].Target() != sp.Remaining[0] ||
		!strings.Contains(doc.Body, "## Split") || !strings.Contains(doc.Body, "[["+sp.Done[0]+"]]") {
		t.Fatalf("task = %q %+v\n%s", doc.Title, task, doc.Body)
	}
	pdoc, part, err := getTask(t.Context(), f.owner.Store, sp.Done[0])
	if err != nil {
		t.Fatal(err)
	}
	if part.Status != model.TaskDone || part.Parent.Target() != f.task || part.Goal.Target() != f.proj || !strings.Contains(pdoc.Body, "https://wiki.acme.example/post-draft") {
		t.Fatalf("done part = %+v\n%s", part, pdoc.Body)
	}
	_, rest, err := getTask(t.Context(), f.owner.Store, sp.Remaining[0])
	if err != nil {
		t.Fatal(err)
	}
	if rest.Status != model.TaskTodo || rest.Due != "2026-11-20" || rest.Parent.Target() != f.task {
		t.Fatalf("remaining part = %+v", rest)
	}
	var split []ReceiptView
	all, err := listReceipts(t.Context(), f.owner.Store)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range all {
		if r.Op == "task_split" {
			split = append(split, r)
		}
	}
	if len(split) != 1 {
		t.Fatalf("split receipts = %+v", split)
	}
	must(t, ReceiptUndo, f.owner, ReceiptUndoInput{ID: split[0].ID})
	if doc, task, _ := getTask(t.Context(), f.owner.Store, f.task); len(task.DependsOn) != 0 || doc.Title != "Write the launch post" {
		t.Fatalf("after undo: %q depends_on %v", doc.Title, task.DependsOn)
	}
}
