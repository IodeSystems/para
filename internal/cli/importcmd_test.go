package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/iodesystems/parasolve/internal/guide"
	"github.com/iodesystems/parasolve/internal/importer"
	"github.com/iodesystems/parasolve/internal/ops"
	"github.com/iodesystems/parasolve/internal/vault"
)

func writeSrc(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// legacySrc is a small invented legacy vault.
func legacySrc(t *testing.T) string {
	dir := filepath.Join(t.TempDir(), "legacy")
	writeSrc(t, dir, "01-Projects/Atlas/Atlas.md", "---\ntype: project\n---\n\n# Atlas\n\nMove the server.\n")
	writeSrc(t, dir, "02-Areas/Home Lab.md", "---\ntype: area\n---\n\n# Home Lab\n")
	writeSrc(t, dir, "events.jsonl", strings.Join([]string{
		`{"ts":"2025-03-01T09:00:00Z","actor":"owner","action":"add","id":"T-250301-01","type":"task","text":"Order the disk","project":"Atlas"}`,
		`{"ts":"2025-03-01T09:01:00Z","actor":"owner","action":"contact","id":"T-250301-01","who":"Ada Example","via":"email","state":"sent"}`,
		`{"ts":"2025-03-01T09:02:00Z","actor":"owner","action":"add","id":"Q-250301-01","type":"question","text":"Which path?"}`,
	}, "\n")+"\n")
	return dir
}

func TestImportScanRunUndo(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	src := legacySrc(t)
	planPath := filepath.Join(e.dir, "plan.yaml")

	var scan struct {
		importer.Report
		PlanFile string `json:"plan_file"`
	}
	e.json(&scan, "import", "scan", src, "--plan", planPath)
	if scan.Adapter != "legacy" || scan.PlanFile != planPath || scan.Fold == nil ||
		scan.Fold.TasksByStatus["waiting"] != 1 || scan.Fold.OpenQuestions != 1 || scan.Fold.Items != 2 {
		t.Fatalf("scan = %+v fold %+v", scan.Report, scan.Fold)
	}
	if _, _, err := e.run("import", "scan", src, "--plan", planPath); err == nil {
		t.Fatal("scan must not overwrite a plan")
	}
	// The scan wrote nothing to the vault.
	var tail ops.EventsTailOutput
	e.json(&tail, "events", "tail")
	if len(tail.Events) != 0 {
		t.Fatalf("scan logged events: %+v", tail.Events)
	}

	var dry importer.RunResult
	e.json(&dry, "import", "run", planPath, "--dry-run")
	if !dry.DryRun || dry.Created == 0 || dry.Refused != 0 {
		t.Fatalf("dry run = %+v", dry)
	}
	e.json(&tail, "events", "tail")
	if len(tail.Events) != 0 {
		t.Fatalf("dry run logged events: %+v", tail.Events)
	}

	var run importer.RunResult
	e.json(&run, "import", "run", planPath)
	if run.DryRun || run.Created != dry.Created || run.Batch == "" {
		t.Fatalf("run = %+v", run)
	}
	var lint ops.LintOutput
	e.json(&lint, "lint")
	if lint.Errors != 0 {
		t.Fatalf("lint = %+v", lint.Findings)
	}
	var again importer.RunResult
	e.json(&again, "import", "run", planPath)
	if again.Created != 0 || again.Already != run.Created {
		t.Fatalf("rerun = %+v", again)
	}

	var undo vault.RemoveResult
	e.json(&undo, "import", "undo", run.Batch)
	if len(undo.Removed) != run.Created {
		t.Fatalf("undo = %+v", undo)
	}
	if out := e.ok("import", "undo", run.Batch); !strings.Contains(out, "removed\t0") {
		t.Errorf("second undo:\n%s", out)
	}
	if _, _, err := e.run("import", "undo", "imp-nosuchbatch"); err == nil {
		t.Error("an unknown batch must fail")
	}
}

// Import takes a path, so it is a CLI command only: no op, no MCP tool.
func TestImportIsNotAnOp(t *testing.T) {
	for _, e := range ops.All() {
		if strings.Contains(e.Name(), "import") {
			t.Errorf("op %s exposes import", e.Name())
		}
	}
	found := false
	walk(newRoot(&loader{}), func(c *cobra.Command) {
		if c.Name() == "import" {
			found = true
		}
		if strings.HasPrefix(c.CommandPath(), "para import") && c.Annotations[opAnnotation] != "" {
			t.Errorf("%s is bound to an op", c.CommandPath())
		}
	})
	if !found {
		t.Error("no import command")
	}
}

func TestImportHelpIsTheGuide(t *testing.T) {
	e := newEnv(t)
	out := e.ok("import", "--help")
	for _, want := range []string{"Run nothing yourself unless the owner says so", "para import scan", "Never read credentials", "Write no assessment of a person"} {
		if !strings.Contains(out, want) {
			t.Errorf("import --help lacks %q", want)
		}
	}
	if !strings.Contains(out, guide.ImportCorpus()[:80]) {
		t.Error("import --help does not open with the import_corpus text")
	}
}

// A hostile file name must not reach the terminal as an escape sequence.
func TestImportScanPrintsNoControlCharacters(t *testing.T) {
	e := newEnv(t)
	dir := filepath.Join(t.TempDir(), "notes")
	writeSrc(t, dir, "ok.md", "# Ok\n")
	writeSrc(t, dir, "evil\x1b[2J\x07name.txt", "x")
	writeSrc(t, dir, "bad\x1b]0;pwned\x07.md", "<b>x</b>\n")
	out := e.ok("import", "scan", dir, "--plan", filepath.Join(e.dir, "p.yaml"))
	for _, r := range out {
		if r < 0x20 && r != '\n' && r != '\t' {
			t.Fatalf("control character %q in output:\n%q", r, out)
		}
	}
	if !strings.Contains(out, "cannot map") || !strings.Contains(out, "refused") {
		t.Errorf("output:\n%s", out)
	}
	plan := string(mustReadFile(t, filepath.Join(e.dir, "p.yaml")))
	if strings.Contains(plan, "\x1b") || strings.Contains(plan, "\x07") {
		t.Error("plan file holds a raw control character")
	}
}

func mustReadFile(t *testing.T, p string) []byte {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
