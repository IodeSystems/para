package cli

import (
	"context"
	"strings"
	"testing"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/ops"
)

// streamBOParityCases are the parity rows of stream BO: grill and recheck.
// Ids come from seedStreamBOParity.
func streamBOParityCases() map[string]parityCase {
	return map[string]parityCase{
		"goal_risk_set":  {`{"id":"project-bo","text":"Vendor slips","impact":"high","source":"owner in chat"}`, []string{"goal", "risk", "set", "project-bo", "Vendor folds", "--impact", "high", "--source", "owner in chat"}, false},
		"assumption_set": {`{"id":"task-bo","text":"The API is stable","source":"note"}`, []string{"assumption", "set", "task-bo", "The quota holds", "--source", "note"}, false},
		"grill_context":  {`{"id":"project-bo"}`, []string{"grill", "context", "project-bo"}, true},
		"recheck":        {`{"scope":"project-bo","dry_run":true}`, []string{"recheck", "project-bo", "--dry-run"}, true},
		"recheck_record": {`{"id":"constraint-bo-a","result":"holds","source":"https://wiki.acme.example/a"}`, []string{"recheck", "record", "constraint-bo-b", "holds", "--source", "https://wiki.acme.example/b"}, false},
	}
}

func seedStreamBOParity(t *testing.T, deps ops.Deps) {
	t.Helper()
	docs := []model.Document{
		{ID: "project-bo", Folder: model.FolderProjects, Title: "BO goal",
			Meta: &model.Project{Common: model.Common{Type: model.TypeProject}, Status: model.ProjectActive, Owner: "Jordan Example"}},
		{ID: "task-bo", Folder: model.FolderProjects, Title: "BO task", Body: "## Assumptions\n\n- Legal signs off in a week",
			Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskTodo, Project: model.LinkTo("project-bo")}},
	}
	for _, id := range []string{"constraint-bo-a", "constraint-bo-b"} {
		docs = append(docs, model.Document{ID: id, Folder: model.FolderConstraints, Title: "BO " + id,
			Meta: &model.Constraint{Common: model.Common{Type: model.TypeConstraint}, Why: "audit", By: "Jordan Example",
				Source:    model.Source{URL: "https://wiki.acme.example/a", Retrieved: "2026-11-01", Fidelity: model.FidelityNone},
				AppliesIn: []string{"project-bo"}, NotIn: []string{}, ReviewBy: "2026-01-01"}})
	}
	for _, d := range docs {
		if _, err := deps.Store.Create(context.Background(), d); err != nil {
			t.Fatalf("seed %s: %v", d.ID, err)
		}
	}
}

// TestGrillRecheckCLI: a risk, an assumption, the grill context, and a
// recheck queued and recorded through the CLI.
func TestGrillRecheckCLI(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	var goal ops.ProjectAddOutput
	e.json(&goal, "project", "add", "Launch the portal", "--done-when", "Live.")
	var task ops.TaskAddOutput
	e.json(&task, "task", "add", "Write the runbook", "-p", goal.ID, "--done-when", "Merged.")
	e.ok("goal", "risk", "set", goal.ID, "Vendor slips", "--impact", "high", "--source", "owner in chat")
	e.ok("assumption", "set", task.ID, "The API is stable", "--source", "note")
	var g ops.GrillContextOutput
	e.json(&g, "grill", "context", goal.ID)
	if !strings.Contains(e.ok("grill", "context", goal.ID), "no_owner") {
		t.Fatalf("grill context lacks the unowned risk: %+v", g.Probes)
	}
	var r ops.RecheckOutput
	e.json(&r, "recheck", goal.ID)
	if len(r.Items) != 1 || r.Request == "" || r.Status != model.RequestQueued {
		t.Fatalf("recheck = %+v", r)
	}
	var rec ops.RecheckRecordOutput
	e.json(&rec, "recheck", "record", task.ID, "holds", "--assumption", "The API is stable", "--source", "https://acme.example/status")
	if rec.Result != model.AssumptionHolds || rec.Checked == "" {
		t.Fatalf("record = %+v", rec)
	}
	var again ops.RecheckOutput
	e.json(&again, "recheck", goal.ID, "--older-than", "30", "--dry-run")
	if len(again.Items) != 0 {
		t.Fatalf("checked today, still listed: %+v", again.Items)
	}
}
