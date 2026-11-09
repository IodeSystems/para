package cli

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
	"github.com/iodesystems/para/internal/upgrade"
)

// lastLine is the last non-empty line of text output.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return lines[len(lines)-1]
}

func TestColourOnlyOnATerminalWithoutNoColor(t *testing.T) {
	f := model.Finding{Path: "io/x.md", Line: 3, Code: "title", Severity: "error", Message: "first non-blank line"}
	var buf bytes.Buffer
	if got := findingLine(&buf, f); got != f.String() || strings.Contains(got, "\x1b") {
		t.Fatalf("not a terminal: %q", got)
	}
	t.Cleanup(func() { terminal = isTerminal })
	terminal = func(any) bool { return true }
	t.Setenv("NO_COLOR", "")
	if got := findingLine(&buf, f); !strings.Contains(got, "\x1b[31merror\x1b[0m") {
		t.Fatalf("terminal: %q", got)
	}
	if got := paint(&buf, toneWarn, "warn"); got != "\x1b[33mwarn\x1b[0m" {
		t.Fatalf("warn = %q", got)
	}
	if got := paint(&buf, toneOK, "ok"); got != "\x1b[32mok\x1b[0m" {
		t.Fatalf("ok = %q", got)
	}
	t.Setenv("NO_COLOR", "1")
	if got := findingLine(&buf, f); got != f.String() {
		t.Fatalf("NO_COLOR: %q", got)
	}
}

func TestLintVerdictAndExit(t *testing.T) {
	e := newEnv(t)
	path := e.init("acme")
	if out := e.ok("lint"); lastLine(out) != "✓ lint: 0 errors, 0 warnings" {
		t.Fatalf("clean lint = %q", out)
	}
	if err := os.WriteFile(filepath.Join(path, "io", "hand-made.md"), []byte("no frontmatter\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, _, err := e.run("lint")
	if err == nil || lastLine(out) != "✗ lint: 2 errors, 0 warnings" {
		t.Fatalf("lint with errors = %q, %v", out, err)
	}
}

func TestLintFileFilesOneRequestAndUpdatesIt(t *testing.T) {
	e := newEnv(t)
	path := e.init("acme")
	if err := os.WriteFile(filepath.Join(path, "io", "hand-made.md"), []byte("zebrafish synthetic content\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, _, err := e.run("lint", "--file")
	if err == nil {
		t.Fatal("lint --file with errors exited 0")
	}
	first := lastLine(out)
	if !strings.HasPrefix(first, "✗ lint: 2 errors, 0 warnings — filed as agent work request-") || !strings.HasSuffix(first, "; run /para loop") {
		t.Fatalf("first verdict = %q", first)
	}
	out, _, _ = e.run("lint", "--file")
	id := strings.TrimSuffix(strings.TrimPrefix(first, "✗ lint: 2 errors, 0 warnings — filed as agent work "), "; run /para loop")
	if got := lastLine(out); got != "✗ lint: 2 errors, 0 warnings — updated agent work "+id+"; run /para loop" {
		t.Fatalf("second verdict = %q", got)
	}
	files, _ := filepath.Glob(filepath.Join(path, "requests", "*.md"))
	if len(files) != 1 {
		t.Fatalf("requests = %v", files)
	}
	body, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "kind: vault-check") || !strings.Contains(string(body), "io/hand-made.md:1: error [frontmatter]") || strings.Contains(string(body), "zebrafish") {
		t.Fatalf("request:\n%s", body)
	}
	if _, _, err := e.run("lint", path, "--file"); err == nil {
		t.Fatal("lint --file took a path")
	}
}

func TestVaultUpgradeVerdictAndExit(t *testing.T) {
	e := newEnv(t)
	path := e.initAt("acme", 6)
	latest := upgrade.Latest()
	out := e.ok("vault", "upgrade")
	if want := fmt.Sprintf("✓ acme upgraded — format 6 -> %d of %d; no automated step ran; lint: 0 errors, 0 warnings", latest, latest); lastLine(out) != want {
		t.Fatalf("upgrade verdict = %q, want %q", lastLine(out), want)
	}
	if got := lastLine(e.ok("vault", "upgrade", "status")); got != fmt.Sprintf("✓ acme is at format %d of %d, nothing pending", latest, latest) {
		t.Fatalf("status verdict = %q", got)
	}
	if got := lastLine(e.ok("vault", "upgrade", "--dry-run")); !strings.HasPrefix(got, "✓ acme dry run: would rewrite 0 documents") {
		t.Fatalf("dry-run verdict = %q", got)
	}
	if err := os.WriteFile(filepath.Join(path, "io", "hand-made.md"), []byte("no frontmatter\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, _, err := e.run("vault", "upgrade")
	got := lastLine(out)
	if err == nil || !strings.HasPrefix(got, "✗ acme upgraded") || !strings.Contains(got, "lint: 2 errors, 0 warnings; findings filed as agent work request-") {
		t.Fatalf("upgrade with lint errors = %q, %v", got, err)
	}
}

func TestVaultUpgradeStoppedVerdict(t *testing.T) {
	o := ops.VaultUpgradeOutput{
		From:    7,
		Run:     &upgrade.RunResult{FormatVersion: 7, Steps: []upgrade.StepOutcome{{ID: "dossiers", Auto: true, Batch: "up-dossiers-20261108120000", Count: 2}}},
		Stopped: &ops.UpgradeStop{Step: "small-types", Error: "upgrade: small-types: lint refused the write", Errors: 5},
		Status:  upgrade.Status{FormatVersion: 7, Latest: 10, Pending: []upgrade.Pending{{ID: "small-types", Auto: true}, {ID: "done-when"}}},
		Lint:    &ops.LintCount{Errors: 5},
		Check:   "request-sample",
	}
	if o.Failed() == nil {
		t.Fatal("a stopped run does not fail")
	}
	var buf bytes.Buffer
	if err := upgradeVerdict(&buf, "acme", o); err != nil {
		t.Fatal(err)
	}
	want := "✗ acme upgrade stopped at step small-types: 5 errors — format 7 of 10; batches up-dossiers-20261108120000; lint: 5 errors, 0 warnings; " +
		"automated steps pending: small-types (para vault upgrade --dry-run, then para vault upgrade); judgement steps pending: done-when (queue them: para vault upgrade --file); " +
		"run para vault upgrade status; findings filed as agent work request-sample; run /para loop\n"
	if got := buf.String(); got != want {
		t.Fatalf("verdict =\n%q\nwant\n%q", got, want)
	}
}

func TestDoctorVerdict(t *testing.T) {
	var buf bytes.Buffer
	r := &report{w: io.Writer(&buf)}
	r.ok("home", "/x")
	r.warn("vaults", "none")
	r.verdict()
	if got := lastLine(buf.String()); got != "✓ doctor: 0 failed, 1 warnings" {
		t.Fatalf("verdict = %q", got)
	}
	r.fail("vault acme net.rules", "missing")
	r.verdict()
	if got := lastLine(buf.String()); got != "✗ doctor: 1 failed, 1 warnings" {
		t.Fatalf("verdict = %q", got)
	}
}
