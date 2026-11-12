package vault

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/paramd"
)

// A goal re-prefixed from project-x to goal-x: links move, the old id
// resolves, a receipt keeps its text, and the old id is reserved.
func TestChangeIDs(t *testing.T) {
	ctx := context.Background()
	v, root := newVault(t)
	mk := func(d model.Document) {
		t.Helper()
		if _, err := v.Create(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	mk(model.Document{ID: "project-x", Folder: model.FolderGoals, Title: "Ship X",
		Meta: &model.Goal{Common: model.Common{Type: model.TypeGoal}, Status: model.GoalActive, Owner: "self"}})
	mk(model.Document{ID: "task-a", Folder: model.FolderProjects, Title: "Write it", Body: "Part of [[project-x|X]].",
		Meta: &model.Task{Common: model.Common{Type: model.TypeTask, Threads: []string{"project-x"}}, Status: model.TaskTodo, Project: model.LinkTo("project-x")}})
	mk(model.Document{ID: "receipt-a", Folder: model.FolderReceipts, Title: "Receipt",
		Meta: &model.Receipt{Common: model.Common{Type: model.TypeReceipt}, Actor: "claude", Op: "task_add", Doc: "project-x", Check: model.ReceiptUnchecked}})

	res, err := v.ChangeIDs(ctx, map[string]string{"project-x": "goal-x"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Renamed["project-x"] != "goal-x" || !slices.Equal(res.Rewritten, []string{"task-a"}) {
		t.Fatalf("result %+v", res)
	}
	g, err := v.Get(ctx, "project-x")
	if err != nil || g.ID != "goal-x" || !g.Meta.Header().WasID("project-x") {
		t.Fatalf("old id resolves to %s %v %v", g.ID, g.Meta, err)
	}
	if id, err := v.Resolve(ctx, "project-x"); err != nil || id != "goal-x" {
		t.Fatalf("resolve %q %v", id, err)
	}
	task, err := v.Get(ctx, "task-a")
	if err != nil {
		t.Fatal(err)
	}
	tm := task.Meta.(*model.Task)
	// project: named a goal, so it moved to goal:.
	if tm.Project != "" || tm.Goal.Target() != "goal-x" || task.Body != "Part of [[goal-x|X]]." || !slices.Equal(tm.Threads, []string{"goal-x"}) {
		t.Fatalf("task %+v body %q", tm, task.Body)
	}
	rc, err := v.Get(ctx, "receipt-a")
	if err != nil || rc.Meta.(*model.Receipt).Doc != "project-x" {
		t.Fatalf("receipt changed: %+v %v", rc.Meta, err)
	}
	// The old id is reserved.
	_, err = v.Create(ctx, model.Document{ID: "project-x", Folder: model.FolderResources, Title: "Again", Meta: &model.Note{Common: model.Common{Type: model.TypeNote}}})
	if !errors.Is(err, model.ErrExists) {
		t.Fatalf("create on a former id: %v", err)
	}
	// A second run does nothing new.
	if res, err := v.ChangeIDs(ctx, map[string]string{"project-x": "goal-x"}); err != nil || len(res.Rewritten) != 0 {
		t.Fatalf("again: %+v %v", res, err)
	}
	// A file that syncs in late with the old link is fixed on reconcile.
	late := filepath.Join(root, "projects", "task-late.md")
	if err := os.WriteFile(late, []byte("---\ntype: task\ncreated: 2026-11-11\nstatus: todo\nproject: \"[[project-x]]\"\n---\n\n# Late\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	lint := paramd.CheckVault(parseAll(t, root))
	if !hasCode(lint, paramd.CodeLinkFormer) {
		t.Fatalf("lint has no %s: %v", paramd.CodeLinkFormer, lint)
	}
	noGrace(t, root)
	v = open(t, root)
	// Reconcile relinks the file it finds changed; RelinkFormer then finds
	// nothing left.
	if rec, err := v.Reconcile(ctx); err != nil || !slices.Equal(rec.Relinked, []string{"task-late"}) {
		t.Fatalf("reconcile: %+v %v", rec, err)
	}
	if res, err := v.RelinkFormer(ctx); err != nil || len(res.Rewritten) != 0 {
		t.Fatalf("relink former: %+v %v", res, err)
	}
	data, err := os.ReadFile(late)
	if err != nil || !strings.Contains(string(data), "goal: '[[goal-x]]'") && !strings.Contains(string(data), `goal: "[[goal-x]]"`) {
		t.Fatalf("late file:\n%s %v", data, err)
	}
	if lint := paramd.CheckVault(parseAll(t, root)); hasCode(lint, paramd.CodeLinkFormer) {
		t.Fatalf("lint after relink: %v", lint)
	}
	// An old copy of the goal file that comes back is a lint error.
	if err := os.WriteFile(filepath.Join(root, "projects", "project-x.md"), []byte("---\ntype: project\ncreated: 2026-11-11\nstatus: active\nowner: self\n---\n\n# Old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if lint := paramd.CheckVault(parseAll(t, root)); !hasCode(lint, paramd.CodeFormerTaken) {
		t.Fatalf("lint has no %s: %v", paramd.CodeFormerTaken, lint)
	}
}

func parseAll(t *testing.T, root string) []*paramd.File {
	t.Helper()
	var out []*paramd.File
	for _, f := range model.Folders {
		rels, err := mdFiles(root, string(f))
		if err != nil {
			t.Fatal(err)
		}
		for _, rel := range rels {
			data, err := os.ReadFile(filepath.Join(root, rel))
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, paramd.Parse(rel, data))
		}
	}
	return out
}

func hasCode(fs []paramd.Finding, code string) bool {
	return slices.ContainsFunc(fs, func(f paramd.Finding) bool { return f.Code == code })
}
