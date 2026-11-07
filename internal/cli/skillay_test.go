package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/guide"
)

// The skill routes ingest and align to reference files, and install writes them.
func TestSkillInstallWritesIngestAndAlign(t *testing.T) {
	e := newEnv(t)
	dir := t.TempDir()
	// A file an earlier version installed is removed, so no stale copy is read.
	if err := os.MkdirAll(filepath.Join(dir, "para"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "para", "update.md"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.ok("skill", "install", "--dir", dir)
	for _, old := range guide.SkillRetired {
		if _, err := os.Stat(filepath.Join(dir, "para", old)); !os.IsNotExist(err) {
			t.Errorf("%s still installed: %v", old, err)
		}
	}
	for name, want := range map[string]string{guide.SkillIngestFile: guide.SkillIngest(), guide.SkillAlignFile: guide.SkillAlign(), guide.SkillGrillFile: guide.SkillGrill(), guide.SkillSweepFile: guide.SkillSweep(), guide.SkillLoopFile: guide.SkillLoop(), guide.SkillIOFile: guide.SkillIO()} {
		b, err := os.ReadFile(filepath.Join(dir, "para", name))
		if err != nil || string(b) != want {
			t.Errorf("%s differs from the guide text: %v", name, err)
		}
		if !strings.Contains(guide.Skill(), name) {
			t.Errorf("the skill does not route to %s", name)
		}
	}
	if !strings.Contains(guide.Skill(), "`ingest <url|file|text>`") || !strings.Contains(guide.Skill(), "`align <goal|area|vault>`") {
		t.Error("the skill does not name the ingest and align arguments")
	}
	for _, arg := range []string{"`/para io setup`", "`/para io stats`", "`/para io [source…]`", "`io setup`", "`io stats`"} {
		if !strings.Contains(guide.Skill(), arg) {
			t.Errorf("the skill does not list %s", arg)
		}
	}
}
