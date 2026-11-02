package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/parasolve/internal/ops"
)

func TestJournalEndToEnd(t *testing.T) {
	e := newEnv(t)
	root := e.init("acme")
	e.ok("inbox", "add", "synthetic capture")
	day := time.Now().UTC().Format("2006-01-02")

	var j ops.JournalOutput
	e.json(&j, "journal", "daily", day)
	if j.Action != ops.JournalCreated || j.ID != day || j.Events != 1 {
		t.Fatalf("journal = %+v", j)
	}
	path := filepath.Join(root, "journal", day+".md")
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(first, []byte("\nMy own note.\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	e.ok("reconcile")
	e.json(&j, "journal", "daily", day)
	if j.Action != ops.JournalUnchanged || j.Events != 1 {
		t.Fatalf("rerun = %+v", j)
	}
	e.json(&j, "journal", "weekly", day)
	if j.Action != ops.JournalCreated {
		t.Fatalf("weekly = %+v", j)
	}
	e.json(&j, "journal", "monthly", day)
	if j.Action != ops.JournalCreated {
		t.Fatalf("monthly = %+v", j)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "My own note.") {
		t.Errorf("hand text lost:\n%s", got)
	}
	if !strings.Contains(string(got), "[[inbox-") {
		t.Errorf("no link to the capture:\n%s", got)
	}
	var lint ops.LintOutput
	e.json(&lint, "lint")
	if lint.Errors != 0 || lint.Warnings != 0 {
		t.Fatalf("lint = %+v", lint)
	}
	if _, _, err := e.run("journal", "daily", "not-a-date"); err == nil {
		t.Error("bad date accepted")
	}
}
