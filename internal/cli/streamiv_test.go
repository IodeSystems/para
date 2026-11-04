package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
)

const streamIVICS = "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\nUID:sync@acme.example\r\nSUMMARY:Portal sync\r\n" +
	"DTSTART:20261104T150000Z\r\nDTEND:20261104T153000Z\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"

func shaHex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// writeOldVault writes a small source folder: two notes and a hidden folder
// that verify skips.
func writeOldVault(t *testing.T, dir string) map[string]string {
	t.Helper()
	files := map[string]string{"notes/runbook.md": "runbook v1", "notes/plan.md": "plan v2", "goals.md": "goals", ".obsidian/app.md": "skip", "img.png": "png"}
	out := map[string]string{}
	for rel, body := range files {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		out[rel] = p
	}
	return out
}

// streamIVParityCases are the parity rows of stream IV: import verify and
// calendar compare. The CLI reads local files; the op gets path and hash.
func streamIVParityCases(t *testing.T, e *env) map[string]parityCase {
	old := filepath.Join(e.dir, "old-iv")
	writeOldVault(t, old)
	ics := filepath.Join(e.dir, "cal.ics")
	if err := os.WriteFile(ics, []byte(streamIVICS), 0o600); err != nil {
		t.Fatal(err)
	}
	h := shaHex([]byte("goals"))
	return map[string]parityCase{
		"import_verify":    {`{"files":[{"path":"/old/goals.md","hash":"` + h + `"}],"root":"/old"}`, []string{"import", "verify", old}, false},
		"import_list":      {`{}`, []string{"import", "list"}, true},
		"calendar_compare": {`{"ics":` + jsonString(streamIVICS) + `}`, []string{"calendar", "compare", "--ics", ics}, false},
	}
}

func jsonString(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\r", `\r`, "\n", `\n`)
	return `"` + r.Replace(s) + `"`
}

// TestImportVerifyCLI hashes a local folder and reports what the vault lacks.
func TestImportVerifyCLI(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	old := filepath.Join(e.dir, "old")
	paths := writeOldVault(t, old)
	abs, _ := filepath.Abs(paths["notes/runbook.md"])
	l := &loader{env: func(k string) string {
		if k == "PARA_HOME" {
			return e.home
		}
		return ""
	}}
	if err := l.load(); err != nil {
		t.Fatal(err)
	}
	deps, err := l.deps()
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []struct{ title, path, body string }{
		{"Runbook", abs, "runbook v1"},
		{"Plan", filepath.Join(filepath.Dir(abs), "plan.md"), "plan v1"},
	} {
		in := ops.NoteAddInput{Title: n.title, ImportedFrom: &model.ImportedFrom{Path: n.path, Hash: shaHex([]byte(n.body))}}
		if _, err := ops.NoteAdd.Run(context.Background(), deps, in); err != nil {
			t.Fatal(err)
		}
	}
	out := e.ok("import", "verify", old)
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if !strings.HasPrefix(lines[0], "checked 3: 1 imported, 1 missing, 1 changed") {
		t.Fatalf("verify = %q", out)
	}
	if !strings.HasPrefix(lines[1], "missing\t") || !strings.HasSuffix(lines[1], "/goals.md") || !strings.HasPrefix(lines[2], "changed\t") {
		t.Errorf("missing and changed do not come first: %q", out)
	}
	if strings.Contains(out, ".obsidian") || strings.Contains(out, "img.png") {
		t.Errorf("hidden or other files were checked: %q", out)
	}
	if out := e.ok("import", "verify", old, "--ext", "md,png"); !strings.Contains(out, "checked 4") {
		t.Errorf("--ext md,png = %q", out)
	}
	if out := e.ok("import", "list"); strings.Count(out, "\n") != 2 {
		t.Errorf("import list = %q", out)
	}
}

// TestCalendarCompareCLI reads a local .ics file.
func TestCalendarCompareCLI(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	ics := filepath.Join(e.dir, "cal.ics")
	if err := os.WriteFile(ics, []byte(streamIVICS), 0o600); err != nil {
		t.Fatal(err)
	}
	out := e.ok("calendar", "compare", "--ics", ics, "--from", "2026-11-01", "--to", "2026-11-10", "--zone", "UTC")
	if !strings.Contains(out, "calendar only\t2026-11-04T15:00:00Z\tPortal sync\tsync@acme.example") {
		t.Fatalf("compare = %q", out)
	}
	if _, _, err := e.run("calendar", "compare"); err == nil || !strings.Contains(err.Error(), "--ics") {
		t.Errorf("no --ics: %v", err)
	}
}
