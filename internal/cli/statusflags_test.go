package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
)

// para goal edit --status parks and resumes a goal (parked needs
// --resume-when); para list --status lists by the type's own status; para
// obligation add makes a cron with --project, --recur, --makes, and --goal.
func TestGoalStatusListStatusAndCronFlags(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	var p ops.ProjectAddOutput
	e.json(&p, "project", "add", "Garden")
	var g ops.GoalAddOutput
	e.json(&g, "goal", "add", "Beds", "--done-when", "Beds planted.", "--project", p.ID)

	if _, _, err := e.run("goal", "edit", g.ID, "--status", "parked"); err == nil || !strings.Contains(err.Error(), "resume_when") {
		t.Fatalf("parked with no --resume-when: %v", err)
	}
	e.ok("goal", "edit", g.ID, "--status", "parked", "--resume-when", "after the frost")
	var parked ops.ListOutput
	e.json(&parked, "list", "goal", "--status", "parked")
	if len(parked.Rows) != 1 || parked.Rows[0].ID != g.ID || parked.Rows[0].Status != string(model.GoalParked) {
		t.Fatalf("list --status parked = %+v", parked.Rows)
	}
	e.ok("goal", "edit", g.ID, "--status", "active")
	e.json(&parked, "list", "goal", "--status", "parked")
	if len(parked.Rows) != 0 {
		t.Fatalf("after resume: %+v", parked.Rows)
	}

	var cron ops.ObligationAddOutput
	e.json(&cron, "obligation", "add", "Water", "the", "beds", "--project", p.ID, "--recur", "weekly", "--due", "2026-11-20",
		"--makes", "task", "--goal", g.ID, "--done-when", "Beds are wet.")
	var show ops.ShowOutput
	e.json(&show, "show", cron.ID)
	raw, _ := json.Marshal(show)
	if !strings.Contains(show.Reason, "cron: makes a task") || !strings.Contains(string(raw), g.ID) {
		t.Fatalf("cron show = %s", raw)
	}
	var bundle ops.ProjectShowOutput
	e.json(&bundle, "project", "show", p.ID)
	if len(bundle.Crons) != 1 || !strings.Contains(bundle.Crons[0], cron.ID) {
		t.Fatalf("project crons = %v", bundle.Crons)
	}
	if _, _, err := e.run("obligation", "add", "Water", "--makes", "task", "--recur", "weekly"); err == nil || !strings.Contains(err.Error(), "on a project") {
		t.Fatalf("--makes with no --project: %v", err)
	}
}
