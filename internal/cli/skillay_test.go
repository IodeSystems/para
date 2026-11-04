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
	e.ok("skill", "install", "--dir", dir)
	for name, want := range map[string]string{guide.SkillIngestFile: guide.SkillIngest(), guide.SkillAlignFile: guide.SkillAlign(), guide.SkillUpdateFile: guide.SkillUpdate(), guide.SkillValidateFile: guide.SkillValidate(), guide.SkillGrillFile: guide.SkillGrill(), guide.SkillTrackFile: guide.SkillTrack(), guide.SkillSweepFile: guide.SkillSweep(), guide.SkillDelegateFile: guide.SkillDelegate()} {
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
}
