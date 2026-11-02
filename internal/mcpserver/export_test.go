package mcpserver

import (
	"encoding/json"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iodesystems/paradize/internal/ops"
)

// An agent must not move data off the machine through para. The export op
// renders and logs; the CLI runs the transport. This test calls export over
// MCP with every acting transport and checks that nothing acted.
func TestExportOverMCPRendersAndMovesNothing(t *testing.T) {
	s, _ := newVault(t)
	seedSamples(t, s)
	c := connect(t, ops.Deps{Store: s})

	// Fake tools that leave a marker if para ever runs them.
	bin, marks, work := t.TempDir(), t.TempDir(), t.TempDir()
	for _, name := range []string{"pbcopy", "wl-copy", "xclip", "clip.exe", "tmux", "wormhole", "ssh"} {
		body := "#!/bin/sh\ntouch '" + filepath.Join(marks, name) + "'\n"
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)

	targets := []string{
		"file:" + filepath.Join(work, "leak.txt"),
		"clipboard", "osc52", "tmux", "wormhole", "qr",
		"ssh:desk.example:" + filepath.Join(work, "remote.txt"),
	}
	for _, to := range targets {
		in, _ := json.Marshal(map[string]string{"id": "policy-sample", "as": "slack", "to": to})
		res := call(t, c, "export", string(in))
		if res.IsError {
			t.Fatalf("%s: %s", to, textOf(t, res))
		}
		var out ops.ExportOutput
		if err := json.Unmarshal([]byte(textOf(t, res)), &out); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.Text, "*Sample policy*") || out.To != to {
			t.Errorf("%s: out = %+v", to, out)
		}
	}
	if entries, _ := os.ReadDir(work); len(entries) != 0 {
		t.Errorf("export over MCP wrote files: %v", entries)
	}
	if entries, _ := os.ReadDir(marks); len(entries) != 0 {
		t.Errorf("export over MCP ran tools: %v", entries)
	}

	// Every use is logged with the doc id and the transport.
	evs, err := s.Events(t.Context(), 100)
	if err != nil {
		t.Fatal(err)
	}
	var dests []string
	for _, e := range evs {
		if e.Op == "egress" {
			if e.Doc != "policy-sample" {
				t.Errorf("event = %+v", e)
			}
			dests = append(dests, e.To)
		}
	}
	if got := strings.Join(dests, " "); got != "export:file export:clipboard export:osc52 export:tmux export:wormhole export:qr export:ssh" {
		t.Errorf("egress destinations = %q", got)
	}

	// export_list is read-only and needs no vault document.
	res := call(t, c, "export_list", "{}")
	if res.IsError || !strings.Contains(textOf(t, res), `"formats"`) {
		t.Fatalf("export_list: %v", res)
	}
}

// The ops and mcpserver packages must not hold transport code. This keeps the
// split from eroding: a transport added to an op would be reachable by agents.
func TestOpsAndMCPHoldNoTransportCode(t *testing.T) {
	forbidden := map[string]bool{
		"os/exec": true, "net": true, "net/http": true, "net/smtp": true,
		"github.com/iodesystems/paradize/internal/atomicfile": true,
	}
	for _, dir := range []string{"../ops", "."} {
		files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			af, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatal(err)
			}
			for _, imp := range af.Imports {
				path := strings.Trim(imp.Path.Value, `"`)
				if forbidden[path] {
					t.Errorf("%s imports %s", f, path)
				}
			}
			raw, _ := os.ReadFile(f)
			for _, sym := range []string{"export.Send", "export.ClipboardCopy"} {
				if strings.Contains(string(raw), sym) {
					t.Errorf("%s uses %s", f, sym)
				}
			}
		}
	}
}
