package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/parasolve/internal/ops"
)

// The Agenda is a page reached from Today, not a tab.
func TestAgendaPageFromToday(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	ctx := context.Background()
	d := v.env.Deps
	soon := time.Now().Add(26 * time.Hour).Truncate(time.Hour)
	start, end := soon.Format(time.RFC3339), soon.Add(30*time.Minute).Format(time.RFC3339)
	m, err := ops.MeetingAdd.Run(ctx, d, ops.MeetingAddInput{Title: "Launch window sync", With: []string{"person-ada-example"}, Start: start, End: end})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ops.TaskDue.Run(ctx, d, ops.TaskDueInput{ID: v.ids["t1"], Date: time.Now().Format("2006-01-02")}); err != nil {
		t.Fatal(err)
	}
	dr := newDrv(t, v, 120, 40)
	if len(tabNames) != 6 {
		t.Fatalf("Agenda must not be a tab: %v", tabNames)
	}
	if dr.has("Agenda:") || !dr.has("Agenda") {
		t.Fatalf("Today has an Agenda sentence, or no Agenda key:\n%s", dr.plainFrame())
	}
	dr.key(dr.keyFor("act:agenda"))
	f := dr.plainFrame()
	for _, want := range []string{"Agenda ", soon.Format("15:04") + "-" + soon.Add(30*time.Minute).Format("15:04"), "Launch window sync", "with Ada Example", "Write the parser", "today"} {
		if !strings.Contains(f, want) {
			t.Errorf("agenda page lacks %q:\n%s", want, f)
		}
	}
	// A row opens its document, and esc comes back to the agenda.
	dr.key(dr.keyFor("doc:" + m.ID))
	if !dr.has("Questions") {
		t.Errorf("the meeting did not open:\n%s", dr.plainFrame())
	}
	dr.key("esc")
	if !dr.has("Launch window sync") {
		t.Errorf("esc did not return to the agenda:\n%s", dr.plainFrame())
	}
	dr.key("esc")
	if !dr.has("Today") {
		t.Errorf("esc did not return to Today:\n%s", dr.plainFrame())
	}
}

func TestAgendaPageEmpty(t *testing.T) {
	v := newTestVault(t)
	dr := newDrv(t, v, 100, 30)
	if dr.has("Agenda:") {
		t.Errorf("Today shows an Agenda summary with nothing dated:\n%s", dr.plainFrame())
	}
	dr.key(dr.keyFor("act:agenda"))
	if !dr.has("Nothing is dated") {
		t.Errorf("an empty agenda says nothing:\n%s", dr.plainFrame())
	}
}
