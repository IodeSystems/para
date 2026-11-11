package cli

import (
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/ops"
)

// An agent's write at the terminal applies at once with no rating; --minor and
// --obvious are gone (D4): unknown flags.
func TestCLIAgentWriteAppliesAndRatingsAreGone(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	var added ops.IOAddOutput
	e.json(&added, "--actor", "claude", "--why", "Owner asked.", "inbox", "add", "Synthetic capture")
	if !strings.HasPrefix(added.ID, "io-") {
		t.Fatalf("added = %+v", added)
	}
	for _, flag := range []string{"--minor", "--obvious", "--dossier=d"} {
		if _, _, err := e.run("--actor", "claude", "--why", "Owner asked.", flag, "inbox", "add", "Another capture", "--json"); err == nil || !strings.Contains(err.Error(), "unknown flag") {
			t.Fatalf("%s: %v", flag, err)
		}
	}
}
