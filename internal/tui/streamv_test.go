package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/parasolve/internal/ops"
)

// The Agenda tab is the home screen; By date groups it by day.
func TestAgendaByDateToggle(t *testing.T) {
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
		t.Fatalf("Agenda has no tab of its own: %v", tabNames)
	}
	if tabNames[tabToday] != "Agenda" || dr.has("Today") {
		t.Fatalf("home tab is not Agenda:\n%s", dr.plainFrame())
	}
	dr.key(dr.keyFor("act:bydate"))
	f := dr.plainFrame()
	for _, want := range []string{"Agenda by date", soon.Format("15:04") + "-" + soon.Add(30*time.Minute).Format("15:04"), "Launch window sync", "with Ada Example", "Write the parser", "today"} {
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
	dr.key(dr.keyFor("act:bydate"))
	if dr.has("Agenda by date") {
		t.Errorf("By date did not toggle off:\n%s", dr.plainFrame())
	}
}

func TestAgendaPageEmpty(t *testing.T) {
	v := newTestVault(t)
	dr := newDrv(t, v, 100, 30)
	dr.key(dr.keyFor("act:bydate"))
	if !dr.has("Nothing is dated") {
		t.Errorf("an empty agenda says nothing:\n%s", dr.plainFrame())
	}
}

// Switching vault is an Agenda-tab action; elsewhere the vault is read only.
func TestVaultSwitchOnlyOnAgenda(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	d := newDrv(t, v, 120, 36)
	if d.keyFor("act:vault") == "" {
		t.Fatalf("Agenda has no vault switch:\n%s", d.plainFrame())
	}
	for _, tab := range []string{"2", "3", "4", "5", "6"} {
		d.key(tab)
		if id, ok := d.m.keymap["v"]; ok && id == "act:vault" {
			t.Errorf("tab %s binds v to the vault switch", tab)
		}
		for _, id := range d.m.keymap {
			if id == "act:vault" {
				t.Errorf("tab %s offers the vault switch", tab)
			}
		}
		d.key("v")
		if d.m.ov.kind != ovNone {
			t.Errorf("tab %s: v opened an overlay", tab)
			d.key("esc")
		}
		if !d.has(v.env.Vault.ID) {
			t.Errorf("tab %s does not name the vault", tab)
		}
	}
	d.key("6")
	d.key(d.keyFor("doc:policy-logging"))
	for _, id := range d.m.keymap {
		if id == "act:vault" {
			t.Error("an opened document offers the vault switch")
		}
	}
}
