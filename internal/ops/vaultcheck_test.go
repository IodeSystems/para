package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/upgrade"
)

func TestVaultCheckFilesOneRequestThenUpdatesIt(t *testing.T) {
	tv := newTickVault(t, "2026-11-03T12:00:00Z")
	d := tv.open("one", "cli")
	ctx := t.Context()
	clean, err := VaultCheck.Run(ctx, d, VaultCheckInput{})
	if err != nil || clean.Request != "" || clean.Failed() != nil {
		t.Fatalf("clean vault = %+v, %v", clean, err)
	}
	// A synthetic file para cannot read; its text must not reach the request.
	const content = "zebrafish synthetic content line"
	if err := os.WriteFile(filepath.Join(tv.root, "io", "hand-made.md"), []byte(content+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := VaultCheck.Run(ctx, d, VaultCheckInput{})
	if err != nil || first.Request == "" || first.Updated || first.Errors == 0 || first.Failed() == nil {
		t.Fatalf("first = %+v, %v", first, err)
	}
	second, err := VaultCheck.Run(ctx, d, VaultCheckInput{})
	if err != nil || second.Request != first.Request || !second.Updated {
		t.Fatalf("second = %+v, %v", second, err)
	}
	docs, err := loadRequests(ctx, d.Store)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, doc := range docs {
		if doc.Meta.(*model.Request).Kind == VaultCheckKind {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d vault-check requests, want 1", n)
	}
	req, err := d.Store.Get(ctx, first.Request)
	if err != nil {
		t.Fatal(err)
	}
	if req.Title != "Vault check: 2 errors, 0 warnings" {
		t.Errorf("title = %q", req.Title)
	}
	for _, want := range []string{"io/hand-made.md:1: error [frontmatter]", "[title]", "request_done", "bug report", "never copy document content"} {
		if !strings.Contains(req.Body, want) {
			t.Errorf("body lacks %q:\n%s", want, req.Body)
		}
	}
	if strings.Contains(req.Body, "zebrafish") {
		t.Errorf("document content in the request:\n%s", req.Body)
	}
	if strings.Count(req.Body, "## Findings") != 1 {
		t.Errorf("findings section twice:\n%s", req.Body)
	}
	// An agent cannot call it.
	if _, err := VaultCheck.Run(ctx, tv.open("one", "mcp"), VaultCheckInput{}); err == nil {
		t.Error("an agent ran vault_check")
	}
}

func TestVaultUpgradeFilesLintFindingsAfterARun(t *testing.T) {
	tv := newTickVault(t, "2026-11-03T12:00:00Z")
	d := tv.open("one", "cli")
	ctx := t.Context()
	if err := os.WriteFile(filepath.Join(tv.root, "io", "hand-made.md"), []byte("no frontmatter\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := VaultUpgrade.Run(ctx, d, VaultUpgradeInput{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Lint == nil || out.Lint.Errors != 2 || out.Check == "" || out.Failed() == nil {
		t.Fatalf("out = %+v", out)
	}
	// A dry run lints nothing and files nothing.
	dry, err := VaultUpgrade.Run(ctx, d, VaultUpgradeInput{DryRun: true})
	if err != nil || dry.Lint != nil || dry.Check != "" || dry.Failed() != nil {
		t.Fatalf("dry = %+v, %v", dry, err)
	}
	again, err := VaultUpgrade.Run(ctx, d, VaultUpgradeInput{})
	if err != nil || again.Check != out.Check {
		t.Fatalf("again = %+v, %v", again, err)
	}
}

func TestVaultUpgradeRefusedRunReturnsTheOutput(t *testing.T) {
	tv := newTickVault(t, "2026-11-03T12:00:00Z")
	d := tv.open("one", "cli")
	ctx := t.Context()
	if err := d.Store.SetConfig(ctx, upgrade.ConfigKey, "6"); err != nil {
		t.Fatal(err)
	}
	// Install old writes with a para that logs no session.
	if err := tv.open("old", "cli").Store.Record(ctx, "receipt", "plain", "", "", ""); err != nil {
		t.Fatal(err)
	}
	out, err := VaultUpgrade.Run(ctx, d, VaultUpgradeInput{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Stopped == nil || out.Stopped.Step != "" || !strings.Contains(out.Stopped.Error, "install old") || out.Failed() == nil {
		t.Fatalf("stopped = %+v", out.Stopped)
	}
	if out.From != 6 || out.Status.FormatVersion != 6 || out.Run == nil || len(out.Run.Stale) != 1 {
		t.Fatalf("out = %+v", out)
	}
}

// A finding only the owner can clear is left out of the vault-check request:
// alone it files nothing, and an open request holding only such findings is
// cancelled.
func TestVaultCheckLeavesOwnerFindingsOut(t *testing.T) {
	tv := newTickVault(t, "2026-11-03T12:00:00Z")
	d := tv.open("one", "cli")
	ctx := t.Context()
	owner := model.Finding{Path: "proposals/p.md", Line: 9, Code: model.CodeDossierEvidence, Severity: "warning", Message: "evidence"}
	agent := model.Finding{Path: "tasks/t.md", Line: 1, Code: "field", Severity: model.SeverityError, Message: "bad field"}
	id, _, err := fileVaultCheck(ctx, d, NewLintOutput([]model.Finding{owner}), "")
	if err != nil || id != "" {
		t.Fatalf("owner only: id %q, %v", id, err)
	}
	id, _, err = fileVaultCheck(ctx, d, NewLintOutput([]model.Finding{owner, agent}), "")
	if err != nil || id == "" {
		t.Fatalf("mixed: id %q, %v", id, err)
	}
	req, err := d.Store.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if req.Title != "Vault check: 1 errors, 0 warnings" || !strings.Contains(req.Body, "tasks/t.md") || strings.Contains(req.Body, "proposals/p.md") || !strings.Contains(req.Body, "1 more findings only the owner") {
		t.Fatalf("request %q:\n%s", req.Title, req.Body)
	}
	if again, _, err := fileVaultCheck(ctx, d, NewLintOutput([]model.Finding{owner}), ""); err != nil || again != "" {
		t.Fatalf("owner only again: id %q, %v", again, err)
	}
	req, err = d.Store.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if st := req.Meta.(*model.Request).Status; st != model.RequestCancelled {
		t.Fatalf("status = %s, want cancelled", st)
	}
}
