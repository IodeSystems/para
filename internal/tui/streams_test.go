package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/ops"
)

func rowText(rows []row) string {
	var b strings.Builder
	for _, r := range rows {
		if r.draw != nil {
			b.WriteString(r.draw(func(_, label string) string { return label }))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func TestWorkShowsAreasAndAreaTasks(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	ctx := context.Background()
	a, err := ops.AreaAdd.Run(ctx, v.env.Deps, ops.AreaAddInput{Title: "Home"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ops.ProjectAdd.Run(ctx, v.env.Deps, ops.ProjectAddInput{Title: "Paint the hall", Area: a.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := ops.AreaLinkAdd.Run(ctx, v.env.Deps, ops.LinkAddInput{ID: a.ID, Label: "House runbook", Kind: model.PointerRunbook, Target: "~/doc/house.md"}); err != nil {
		t.Fatal(err)
	}
	due := time.Now().AddDate(0, 0, 2).Format("2006-01-02")
	if _, err := ops.TaskAdd.Run(ctx, v.env.Deps, ops.TaskAddInput{Area: a.ID, Title: "Water plants", Due: due}); err != nil {
		t.Fatal(err)
	}
	d := newDrv(t, v, 120, 50)
	text := rowText(d.m.rowsWork(120))
	for _, want := range []string{"Areas (1)", "Home", "Paint the hall", "Water plants", "runbook: House runbook  ~/doc/house.md", "due " + due, "Projects with no area (1)", "Portal launch"} {
		if !strings.Contains(text, want) {
			t.Errorf("Work lacks %q:\n%s", want, text)
		}
	}
	today := rowText(d.m.rowsToday(120))
	if !strings.Contains(today, "Due soon (1)") || !strings.Contains(today, "Water plants") {
		t.Errorf("Today lacks the due task:\n%s", today)
	}
}

func TestTodayShowsIncidentsFirstAndWaiting(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	ctx := context.Background()
	deps := v.env.Deps
	a, err := ops.AreaAdd.Run(ctx, deps, ops.AreaAddInput{Title: "Atlas service"})
	if err != nil {
		t.Fatal(err)
	}
	inc, err := ops.IncidentAdd.Run(ctx, deps, ops.IncidentAddInput{Title: "Atlas down", Severity: model.Sev1, Area: a.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ops.TaskAdd.Run(ctx, deps, ops.TaskAddInput{Area: a.ID, Title: "Ask Ada for logs", Assignee: "person-ada-example"}); err != nil {
		t.Fatal(err)
	}
	d := newDrv(t, v, 120, 50)
	text := rowText(d.m.rowsToday(120))
	i, o := strings.Index(text, "Incidents (1)"), strings.Index(text, "Overdue")
	if i < 0 || o < 0 || i > o || !strings.Contains(text, "sev1 open") || !strings.Contains(text, "Atlas down") {
		t.Errorf("Today lacks incidents before the rest:\n%s", text)
	}
	if !strings.Contains(text, "Waiting on others (2)") { // the seeded outbound RFX and the task
		t.Errorf("Today lacks the waiting count:\n%s", text)
	}
	if _, err := ops.IncidentStatus.Run(ctx, deps, ops.IncidentStatusInput{ID: inc.ID, Status: model.IncidentResolved}); err != nil {
		t.Fatal(err)
	}
	d = newDrv(t, v, 120, 50)
	if text := rowText(d.m.rowsToday(120)); strings.Contains(text, "Incidents (") {
		t.Errorf("a resolved incident is still on Today:\n%s", text)
	}
}
