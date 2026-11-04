package mcpserver

import (
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/guide"
	"github.com/iodesystems/para/internal/ops"
)

// The ME rules reach agents: capture first, waiting is an Ask, merge and
// notes, defer with owner_said, and a held write is relayed, not a failure.
func TestInstructionsCarryStreamME(t *testing.T) {
	names := map[string]bool{}
	for _, e := range ops.All() {
		names[e.Name()] = true
	}
	for _, tool := range []string{"io_add", "task_to_asks", "doc_merge", "doc_archive", "notes_read", "notes_edit", "defer", "calendar_compare"} {
		if !names[tool] || !strings.Contains(Instructions, tool) {
			t.Errorf("instructions do not name tool %s (exists: %v)", tool, names[tool])
		}
	}
	for _, want := range []string{"Capture first", "word for word as an io item", "point the owner to the io item",
		"Waiting on a person is an Ask", "do not report it as a failure", "owner_said"} {
		if !strings.Contains(Instructions, want) {
			t.Errorf("instructions lack %q", want)
		}
	}
	if s := guide.Skill(); !strings.Contains(s, "Capture first") {
		t.Error("the skill lacks the capture-first rule")
	}
}
