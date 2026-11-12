package ops

import (
	"strings"
	"testing"

	"github.com/iodesystems/para/changes"
	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/upgrade"
)

func TestVaultUpgradeFilesJudgementWorkOnceAndRefusesAgents(t *testing.T) {
	tv := newTickVault(t, "2026-11-03T12:00:00Z")
	d := tv.open("one", "cli")
	ctx := t.Context()
	if _, err := d.Store.Create(ctx, model.Document{ID: "project-open", Folder: model.FolderProjects, Title: "Open goal",
		Meta: &model.Goal{Common: model.Common{Type: model.TypeGoal}, Status: model.GoalActive, Owner: "me"}}); err != nil {
		t.Fatal(err)
	}
	out, err := VaultUpgrade.Run(ctx, d, VaultUpgradeInput{File: true})
	if err != nil {
		t.Fatal(err)
	}
	// The done-when work, and the item that asks the owner to run step goals;
	// projects-sort waits for that run.
	if len(out.Filed) != 2 {
		t.Fatalf("filed = %v", out.Filed)
	}
	var req model.Document
	for _, id := range out.Filed {
		doc, err := d.Store.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(doc.Title, AutoStepsTitle) {
			req = doc
		}
	}
	r := req.Meta.(*model.Request)
	if r.Kind != upgradeKind || !strings.HasPrefix(req.Title, upgradePrefix) || !strings.Contains(req.Body, "project-open") || !strings.Contains(req.Body, "Done when") {
		t.Fatalf("request = %+v %q", r, req.Body)
	}
	// Filing changes no document.
	if got, _ := d.Store.Get(ctx, "project-open"); got.Meta.(*model.Goal).DoneWhen != "" {
		t.Fatal("filing edited a document")
	}
	again, err := VaultUpgrade.Run(ctx, d, VaultUpgradeInput{File: true})
	if err != nil || len(again.Filed) != 0 {
		t.Fatalf("second filing = %v, %v", again.Filed, err)
	}
	if _, err := VaultUpgrade.Run(ctx, d, VaultUpgradeInput{File: true, DryRun: true}); err == nil {
		t.Fatal("file with dry_run was accepted")
	}

	agent := tv.open("one", "mcp")
	if _, err := VaultUpgrade.Run(ctx, agent, VaultUpgradeInput{DryRun: true}); err == nil {
		t.Fatal("an agent ran vault_upgrade")
	}
	st, err := VaultUpgradeStatus.Run(ctx, agent, VaultUpgradeStatusInput{})
	if err != nil || len(st.Pending) == 0 || st.Pending[0].ID != "done-when" {
		t.Fatalf("agent status = %+v, %v", st, err)
	}
}

func TestChangesUsesTheSessionSinceWhenGivenNone(t *testing.T) {
	tv := newTickVault(t, "2026-11-03T12:00:00Z")
	d := tv.open("one", "mcp")
	out, err := Changes.Run(t.Context(), d, ChangesInput{})
	if err != nil || len(out.Releases) != 1 || out.Current != changes.Latest() {
		t.Fatalf("no since: %+v, %v", out, err)
	}
	d.ChangesSince = changes.Latest()
	out, err = Changes.Run(t.Context(), d, ChangesInput{})
	if err != nil || len(out.Releases) != 0 || !strings.Contains(out.Text, "No changes after") {
		t.Fatalf("since latest: %+v, %v", out, err)
	}
	out, err = Changes.Run(t.Context(), d, ChangesInput{Since: "0.0.1"})
	if err != nil || len(out.Releases) == 0 {
		t.Fatalf("explicit since: %+v, %v", out, err)
	}
}

// Judgement steps (annotation-placement, notes-placement) file agent work
// that links areas and notes, so request_add must take them.
func TestRequestLinksAreasAndNotes(t *testing.T) {
	tv := newTickVault(t, "2026-11-03T12:00:00Z")
	d := tv.open("one", "cli")
	ctx := t.Context()
	area := must(t, AreaAdd, d, AreaAddInput{Title: "Ops"}).ID
	note := must(t, NoteAdd, d, NoteAddInput{Title: "X", Area: area}).ID
	if _, err := RequestAdd.Run(ctx, d, RequestAddInput{Title: "Place", Ask: "Place them.", Links: []string{area, note}, Kind: upgradeKind}); err != nil {
		t.Fatal(err)
	}
}

// Every judgement step's instructions become a request body when filed, so
// they must pass the vault's lint (no raw HTML such as <heading>).
func TestUpgradeInstructionsFileCleanly(t *testing.T) {
	tv := newTickVault(t, "2026-11-03T12:00:00Z")
	d := tv.open("one", "cli")
	for _, s := range upgrade.Steps() {
		if s.Instructions == "" {
			continue
		}
		ask := s.Instructions + "\n\n## Done when\n\n" + s.Done
		if _, err := RequestAdd.Run(t.Context(), d, RequestAddInput{Title: "Step " + s.ID, Ask: ask, Kind: upgradeKind}); err != nil {
			t.Errorf("step %s: %v", s.ID, err)
		}
	}
}

// A pending automated step is filed as one agent work item that asks the
// owner to run it: nothing runs unattended, and the item is filed once.
func TestVaultUpgradeFilesTheAutomatedStepsAsOneItem(t *testing.T) {
	tv := newTickVault(t, "2026-11-03T12:00:00Z")
	d := tv.open("one", "cli")
	ctx := t.Context()
	if _, err := d.Store.Create(ctx, model.Document{ID: "meeting-a", Folder: model.FolderMeetings, Title: "Sync", Body: "## Notes\n\nx\n",
		Meta: &model.Meeting{Common: model.Common{Type: model.TypeMeeting}, Status: model.MeetingScheduled,
			Start: "2026-11-04T10:00:00-05:00", End: "2026-11-04T10:30:00-05:00", With: []model.Link{model.LinkTo("person-x")}}}); err != nil {
		t.Fatal(err)
	}
	out, err := VaultUpgrade.Run(ctx, d, VaultUpgradeInput{File: true})
	if err != nil {
		t.Fatal(err)
	}
	var auto string
	for _, id := range out.Filed {
		if doc, _ := d.Store.Get(ctx, id); doc.Title == upgradePrefix+AutoStepsTitle {
			auto = doc.Body
		}
	}
	if !strings.Contains(auto, "vault upgrade --dry-run") || !strings.Contains(auto, "cannot run it") {
		t.Fatalf("no automated-steps item: filed %v, body %q", out.Filed, auto)
	}
	if got, _ := d.Store.Get(ctx, "meeting-a"); got.Meta.Header().Type != model.TypeMeeting {
		t.Fatal("filing ran the automated step")
	}
	again, err := VaultUpgrade.Run(ctx, d, VaultUpgradeInput{File: true})
	if err != nil || len(again.Filed) != 0 {
		t.Fatalf("second filing = %v, %v", again.Filed, err)
	}
}
