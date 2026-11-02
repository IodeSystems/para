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
