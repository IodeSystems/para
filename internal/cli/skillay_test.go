package cli

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/guide"
)

// Install writes SKILL.md and the version file only, and removes the
// reference files of earlier versions, so no stale copy is read.
func TestSkillInstallRemovesReferenceFiles(t *testing.T) {
	e := newEnv(t)
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "para"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := []string{"import.md", "ingest.md", "align.md", "grill.md", "sweep.md", "loop.md", "io.md", "update.md"}
	for _, name := range old {
		if err := os.WriteFile(filepath.Join(dir, "para", name), []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	e.ok("skill", "install", "--dir", dir)
	for _, name := range old {
		if _, err := os.Stat(filepath.Join(dir, "para", name)); !os.IsNotExist(err) {
			t.Errorf("%s still installed: %v", name, err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(dir, "para"))
	if err != nil || len(entries) != 2 {
		t.Fatalf("installed files = %v, %v", entries, err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "para", "SKILL.md"))
	if err != nil || string(b) != guide.Skill() {
		t.Fatalf("SKILL.md differs: %v", err)
	}
	if regexp.MustCompile(`/para (import|ingest|align|update|validate|grill|recheck|track|io|sweep|delegate|loop|health)`).Match(b) {
		t.Error("SKILL.md names a /para verb")
	}
	if !strings.Contains(string(b), "op_run {op: guide, topic: loop}") {
		t.Error("SKILL.md does not point at the guide op")
	}
}

// para guide with a topic prints the prompt text; with none, the use cases.
func TestGuideCommandTopic(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	if out := e.ok("guide", "loop"); !strings.Contains(out, "work the agent queue") {
		t.Errorf("guide loop: %.80s", out)
	}
	if _, _, err := e.run("guide", "nope"); err == nil || !strings.Contains(err.Error(), "grill_goal") {
		t.Errorf("unknown topic error = %v", err)
	}
}
