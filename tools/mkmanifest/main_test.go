package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/update"
)

func TestManifestFromArchivesAndNotes(t *testing.T) {
	dir := t.TempDir()
	for _, p := range []string{"linux_amd64", "darwin_arm64"} {
		if err := os.WriteFile(filepath.Join(dir, "para_"+p+".tar.gz"), []byte("archive "+p), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	notes := filepath.Join(dir, "0.4.0.md")
	if err := os.WriteFile(notes, []byte("# 0.4.0\n\n## People\n\n- Releases install by themselves. Run para upgrade.\n- Second.\n\n## Agents\n\n- Nothing new.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "para-manifest.json")
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	if err := run("v0.4.0", dir, "https://example.invalid/dl/v0.4.0/", notes, "", out, now); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(out)
	m, err := update.ParseManifest(b)
	if err != nil {
		t.Fatal(err)
	}
	if m.Product != "para" || m.Version != "v0.4.0" || m.Published != "2026-11-10T12:00:00Z" || m.Summary != "Releases install by themselves." {
		t.Fatalf("manifest %+v", m)
	}
	f := m.Platforms["darwin_arm64"]
	if len(m.Platforms) != 2 || f.URL != "https://example.invalid/dl/v0.4.0/para_darwin_arm64.tar.gz" || f.Size != int64(len("archive darwin_arm64")) {
		t.Fatalf("platforms %+v", m.Platforms)
	}
	var raw map[string]any
	_ = json.Unmarshal(b, &raw)
	if _, ok := raw["key_next"]; ok {
		t.Fatal("key_next written")
	}
	// -summary overrides the notes.
	if err := run("v0.4.0", dir, "https://example.invalid/dl", "", "by hand", out, now); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(out); !strings.Contains(string(b), `"summary": "by hand"`) {
		t.Fatalf("override: %s", b)
	}
	for name, args := range map[string][5]string{
		"not vX.Y.Z":  {"0.4.0", dir, "https://x.invalid", notes, ""},
		"no archives": {"v0.4.0", t.TempDir(), "https://x.invalid", notes, ""},
		"no summary":  {"v0.4.0", dir, "https://x.invalid", "", ""},
	} {
		if err := run(args[0], args[1], args[2], args[3], args[4], out, now); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}
