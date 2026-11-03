package ops

import (
	"strings"
	"testing"

	"github.com/iodesystems/parasolve/changes"
	"github.com/iodesystems/parasolve/internal/model"
)

func TestVaultUpgradeFilesJudgementWorkOnceAndRefusesAgents(t *testing.T) {
	tv := newTickVault(t, "2026-11-03T12:00:00Z")
	d := tv.open("one", "cli")
	ctx := t.Context()
	if _, err := d.Store.Create(ctx, model.Document{ID: "project-open", Folder: model.FolderProjects, Title: "Open goal",
		Meta: &model.Project{Common: model.Common{Type: model.TypeProject}, Status: model.ProjectActive, Owner: "me"}}); err != nil {
		t.Fatal(err)
	}
	out, err := VaultUpgrade.Run(ctx, d, VaultUpgradeInput{File: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Filed) != 1 {
		t.Fatalf("filed = %v", out.Filed)
	}
	req, err := d.Store.Get(ctx, out.Filed[0])
	if err != nil {
		t.Fatal(err)
	}
	r := req.Meta.(*model.Request)
	if r.Kind != upgradeKind || !strings.HasPrefix(req.Title, upgradePrefix) || !strings.Contains(req.Body, "project-open") || !strings.Contains(req.Body, "Done when") {
		t.Fatalf("request = %+v %q", r, req.Body)
	}
	// Filing changes no document.
	if got, _ := d.Store.Get(ctx, "project-open"); got.Meta.(*model.Project).DoneWhen != "" {
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
	if err != nil || len(st.Pending) != 1 || st.Pending[0].ID != "done-when" {
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
