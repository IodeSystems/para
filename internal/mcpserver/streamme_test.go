package mcpserver

import (
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/guide"
	"github.com/iodesystems/para/internal/ops"
)

// The ME rules reach agents: capture first, waiting is an Ask, merge and
// notes, later, and the gate: a write that cannot be undone is the owner's,
// and owner_said counts only on io_approve.
func TestInstructionsCarryStreamME(t *testing.T) {
	names := map[string]bool{}
	for _, e := range ops.ForAgents() {
		names[e.Name()] = true
	}
	// A core tool by its name; the rest as op_run's op.
	for _, tool := range []string{"add", "{op: task_to_asks}", "{op: doc_merge}", "{op: doc_archive}", "notes_hash", "edit", "later", "{op: calendar_compare}"} {
		name := strings.TrimSuffix(strings.TrimPrefix(tool, "{op: "), "}")
		if name != "notes_hash" && !names[name] {
			t.Errorf("instructions name %s, which does not exist", name)
		}
		if !strings.Contains(Instructions, tool) {
			t.Errorf("instructions do not name %s", tool)
		}
	}
	for _, want := range []string{"Capture first", "word for word as an io item", "point the owner to the io item",
		"Waiting on a person is an Ask", "it is refused, so ask the owner", "owner_said counts only on {op: io_approve}"} {
		if !strings.Contains(Instructions, want) {
			t.Errorf("instructions lack %q", want)
		}
	}
	if s := guide.Skill(); !strings.Contains(s, "Capture first") {
		t.Error("the skill lacks the capture-first rule")
	}
}
