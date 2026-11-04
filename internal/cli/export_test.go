package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/ops"
)

const exportDoc = "# Logging policy\n\nServices **must** log failures. See [the wiki](https://wiki.acme.example/x).\n"

// exportParityCases are the export ops. Ids come from seedExportParity.
var exportParityCases = map[string]parityCase{
	"export":      {`{"id":"policy-export","as":"jira"}`, []string{"export", "policy-export", "--as", "jira"}, true},
	"export_list": {`{}`, []string{"export", "--list"}, true},
}

func seedExportParity(t *testing.T, deps ops.Deps) {
	t.Helper()
	_, err := ops.Ingest.Run(context.Background(), deps, ops.IngestInput{ID: "policy-export", URL: govURL, Text: exportDoc})
	if err != nil {
		t.Fatal(err)
	}
}

// exportFakeTools puts fake tools on a sealed PATH. Each script gets FAKE_DIR.
func exportFakeTools(t *testing.T, scripts map[string]string) string {
	t.Helper()
	dir, util := t.TempDir(), t.TempDir()
	for name, body := range scripts {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"cat", "cp", "stat", "sh", "git"} {
		for _, d := range []string{"/usr/bin", "/bin"} {
			if _, err := os.Stat(filepath.Join(d, name)); err == nil {
				if err := os.Symlink(filepath.Join(d, name), filepath.Join(util, name)); err != nil {
					t.Fatal(err)
				}
				break
			}
		}
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+util)
	t.Setenv("FAKE_DIR", dir)
	return dir
}

// egressEvents returns "doc dest" for each egress event.
func egressEvents(e *env) []string {
	e.t.Helper()
	var tail ops.EventsTailOutput
	e.json(&tail, "events", "tail", "-n", "200")
	var out []string
	for _, ev := range tail.Events {
		if ev.Op == "egress" {
			out = append(out, ev.Doc+" "+ev.To+" "+ev.From)
		}
	}
	return out
}

func TestExportFormatsToStdout(t *testing.T) {
	exportFakeTools(t, map[string]string{"pandoc": `for a; do last="$a"; done; printf 'HTML-FROM-PANDOC' > "$last"`})
	e := newEnv(t)
	e.init("acme")
	pol := e.ingest(exportDoc)
	for as, want := range map[string]string{
		"plain": "Summary not written yet",
		"md":    "# ",
		"email": "Subject: [UNMARKED] ",
		"slack": "*Rules*",
		"jira":  "h2. Rules",
		"html":  "HTML-FROM-PANDOC",
	} {
		out := e.ok("export", pol.ID, "--as", as)
		if !strings.Contains(out, want) {
			t.Errorf("%s lacks %q:\n%s", as, want, out)
		}
		if strings.Contains(out, "type: policy") {
			t.Errorf("%s: frontmatter leaked", as)
		}
	}
	if _, _, err := e.run("export", pol.ID, "--as", "fax"); err == nil || !strings.Contains(err.Error(), "must be one of") {
		t.Errorf("bad format err = %v", err)
	}
	if _, _, err := e.run("export", pol.ID, "--to", "fax"); err == nil || !strings.Contains(err.Error(), "must be one of") {
		t.Errorf("bad transport err = %v", err)
	}
	if _, _, err := e.run("export"); err == nil {
		t.Error("no id accepted")
	}
	if _, _, err := e.run("export", pol.ID, "--list"); err == nil {
		t.Error("--list with an id accepted")
	}
}

func TestExportToFileUnderDeny(t *testing.T) {
	exportFakeTools(t, nil)
	e := newEnv(t)
	vault := e.init("acme")
	if raw := readFile(t, filepath.Join(vault, ".para", "vault.properties")); !strings.Contains(raw, "egress.default=deny") {
		t.Fatalf("props = %q", raw)
	}
	pol := e.ingest(exportDoc)
	dest := filepath.Join(t.TempDir(), "out.txt")
	_, stderr, err := e.run("export", pol.ID, "--as", "slack", "--to", "file:"+dest)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stderr, "wrote") || !strings.Contains(readFile(t, dest), "*Logging policy*") {
		t.Fatalf("stderr %q file %q", stderr, readFile(t, dest))
	}
	got := egressEvents(e)
	if len(got) != 1 || got[0] != pol.ID+" export:file deny" {
		t.Fatalf("egress events = %v (doc id and transport only, no path)", got)
	}
	if raw := e.ok("events", "tail", "-n", "100", "--json"); strings.Contains(raw, dest) || strings.Contains(raw, "Logging policy") {
		t.Fatalf("path or content in events:\n%s", raw)
	}
}

func TestExportAllowlist(t *testing.T) {
	exportFakeTools(t, nil)
	e := newEnv(t)
	vault := e.init("acme")
	pol := e.ingest(exportDoc)
	props := filepath.Join(vault, ".para", "vault.properties")
	if err := os.WriteFile(props, []byte(readFile(t, props)+"egress.export=stdout,file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	ok := filepath.Join(dir, "ok.txt")
	if _, _, err := e.run("export", pol.ID, "--to", "file:"+ok); err != nil {
		t.Fatal(err)
	}
	denied := filepath.Join(dir, "denied.txt")
	_ = denied
	for _, to := range []string{"clipboard", "ssh:desk:" + denied, "wormhole"} {
		out, _, err := e.run("export", pol.ID, "--to", to)
		if err == nil || !strings.Contains(err.Error(), "egress.export") || out != "" {
			t.Errorf("%s: out %q err %v", to, out, err)
		}
	}
	got := strings.Join(egressEvents(e), "|")
	want := pol.ID + " export:file deny|" + pol.ID + " export:clipboard deny|" + pol.ID + " export:ssh deny|" + pol.ID + " export:wormhole deny"
	if got != want {
		t.Errorf("egress events = %q, want %q (denied attempts are logged)", got, want)
	}
}

func TestExportSSHAndWormholeThroughCLI(t *testing.T) {
	work := t.TempDir()
	exportFakeTools(t, map[string]string{
		"ssh":      `[ "$1" = "--" ] || exit 9; shift; shift; cd "$FAKE_WORK" && exec sh -c "$*"`,
		"wormhole": `cat > "$FAKE_WORK/wormhole.in"; echo "Wormhole code is: 4-test-code" >&2`,
	})
	t.Setenv("FAKE_WORK", work)
	e := newEnv(t)
	e.init("acme")
	pol := e.ingest(exportDoc)
	if _, _, err := e.run("export", pol.ID, "--as", "email", "--to", "ssh:desk:in box.txt"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readFile(t, filepath.Join(work, "in box.txt")), "Subject: [UNMARKED] ") {
		t.Error("ssh content")
	}
	_, stderr, err := e.run("export", pol.ID, "--to", "wormhole")
	if err != nil || !strings.Contains(stderr, "4-test-code") {
		t.Fatalf("stderr %q err %v", stderr, err)
	}
	if !strings.Contains(readFile(t, filepath.Join(work, "wormhole.in")), "Logging policy") {
		t.Error("wormhole content")
	}
	// A hostile host stops at parsing: nothing runs, nothing is logged.
	before := len(egressEvents(e))
	for _, to := range []string{"ssh:-oProxyCommand=touch pwned:/x", "ssh:$(touch pwned):/x"} {
		if _, _, err := e.run("export", pol.ID, "--to", to); err == nil {
			t.Errorf("%q accepted", to)
		}
	}
	if len(egressEvents(e)) != before {
		t.Error("rejected target was logged")
	}
}

func TestExportJSONAndList(t *testing.T) {
	exportFakeTools(t, map[string]string{"pandoc": `for a; do last="$a"; done; printf 'DOCX-BYTES' > "$last"`, "tmux": "exit 0"})
	e := newEnv(t)
	e.init("acme")
	pol := e.ingest(exportDoc)
	if _, _, err := e.run("export", pol.ID, "--to", "file:/tmp/x", "--json"); err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Errorf("json+to err = %v", err)
	}
	var out ops.ExportOutput
	e.json(&out, "export", pol.ID, "--as", "docx")
	if out.Text != "" || out.DataBase64 != "RE9DWC1CWVRFUw==" || out.Ext != "docx" || !out.EgressAllowed || out.Destination != "export:stdout" {
		t.Fatalf("out = %+v", out)
	}
	text := e.ok("export", "--list")
	for _, want := range []string{"formats:", "docx       ok  (pandoc)", "pdf        missing", "transports:", "tmux       ok  (tmux)", "wormhole   missing  needs wormhole", "ssh        missing", "file:<path>"} {
		if !strings.Contains(text, want) {
			t.Errorf("list lacks %q:\n%s", want, text)
		}
	}
	if text != e.ok("export", "list") {
		t.Error("export list differs from export --list")
	}
	var list ops.ExportListOutput
	e.json(&list, "export", "--list")
	if len(list.Formats) != 8 || len(list.Transports) != 8 {
		t.Fatalf("list = %+v", list)
	}
}

func TestCopyStaysClipboardShortcut(t *testing.T) {
	dir := exportFakeTools(t, map[string]string{"xclip": `cat > "$FAKE_DIR/clip.in"`})
	e := newEnv(t)
	e.init("acme")
	pol := e.ingest(exportDoc)
	if out := e.ok("copy", pol.ID, "--clipboard"); !strings.Contains(out, "xclip") {
		t.Fatalf("out = %q", out)
	}
	if !strings.Contains(readFile(t, filepath.Join(dir, "clip.in")), "Logging policy") {
		t.Error("clipboard content")
	}
}
