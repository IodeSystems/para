package mcpserver

import (
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/guide"
	"github.com/iodesystems/para/internal/ops"
)

// The ME rules reach agents: capture first, waiting is an Ask, merge and
// notes, defer, and the gate: a write that cannot be undone is the owner's,
// and owner_said counts only on io_approve.
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
		"Waiting on a person is an Ask", "it is refused, so ask the owner", "owner_said counts only on io_approve"} {
		if !strings.Contains(Instructions, want) {
			t.Errorf("instructions lack %q", want)
		}
	}
	if s := guide.Skill(); !strings.Contains(s, "Capture first") {
		t.Error("the skill lacks the capture-first rule")
	}
}
