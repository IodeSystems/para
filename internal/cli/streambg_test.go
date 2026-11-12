package cli

import (
	"context"
	"testing"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
)

func streamBGParityCases() map[string]parityCase {
	return map[string]parityCase{
		"doc_rename": {`{"id":"note-bg-r-a","new_id":"note-bg-r-a2"}`, []string{"doc", "rename", "note-bg-r-b", "note-bg-r-b2"}, false},
		"doc_retype": {`{"id":"note-bg-t-a","type":"policy_exception","fields":{"kind":"wide_enough_for","status":"open","by":"unknown","policy_text":"Logging standard"}}`,
			[]string{"doc", "retype", "note-bg-t-b", "--to", "policy_exception", "--field", "kind=wide_enough_for", "--field", "status=open", "--field", "by=unknown", "--field", "policy_text=Logging standard"}, false},
		"doc_edit":    {`{"id":"task-bg-e-a","fields":{"assignee":"self"}}`, []string{"doc", "edit", "task-bg-e-b", "--field", "assignee=self"}, false},
		"task_move":   {`{"id":"task-bg-m-a","goal":"project-bg2"}`, []string{"task", "move", "task-bg-m-b", "--project", "project-bg2"}, false},
		"rfx_nudge":   {`{"id":"rfx-bg-a","date":"2026-12-01"}`, []string{"rfx", "nudge", "rfx-bg-b", "2026-12-01"}, false},
		"attempt_add": {`{"target":"task-bg-m-a","what":"Asked","result":"No reply"}`, []string{"attempt", "add", "task-bg-m-b", "--what", "Asked", "--result", "No reply"}, false},
	}
}

func seedStreamBGParity(t *testing.T, deps ops.Deps) {
	t.Helper()
	ctx := context.Background()
	var docs []model.Document
	for _, p := range []string{"project-bg", "project-bg2"} {
		docs = append(docs, model.Document{ID: p, Folder: model.FolderProjects, Title: p,
			Meta: &model.Goal{Common: model.Common{Type: model.TypeGoal}, Status: model.GoalActive, Owner: "Jordan Example"}})
	}
	for _, s := range []string{"a", "b"} {
		for _, k := range []string{"r", "t"} {
			docs = append(docs, model.Document{ID: "note-bg-" + k + "-" + s, Folder: model.FolderResources, Title: "BG note " + k + s,
				Meta: &model.Note{Common: model.Common{Type: model.TypeNote}}})
		}
		for _, k := range []string{"e", "m"} {
			docs = append(docs, model.Document{ID: "task-bg-" + k + "-" + s, Folder: model.FolderProjects, Title: "BG task " + k + s,
				Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskTodo, Goal: model.LinkTo("project-bg")}})
		}
		docs = append(docs, model.Document{ID: "rfx-bg-" + s, Folder: model.FolderRFX, Title: "BG ask " + s,
			Meta: &model.RFX{Common: model.Common{Type: model.TypeRFX}, Direction: model.RFXOut, Status: model.RFXOpen, Route: "domain-x", SelfServeCheck: "checked"}})
	}
	for _, d := range docs {
		if _, err := deps.Store.Create(ctx, d); err != nil {
			t.Fatalf("seed %s: %v", d.ID, err)
		}
	}
}
