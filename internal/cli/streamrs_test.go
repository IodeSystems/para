package cli

import (
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/ops"
)

// An agent's write at the terminal applies at once with no rating; --minor and
// --obvious are accepted, ignored, and named in a note on stderr.
func TestCLIAgentWriteAppliesAndRatingsAreNoted(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	var added ops.IOAddOutput
	e.json(&added, "--actor", "claude", "--why", "Owner asked.", "inbox", "add", "Synthetic capture")
	if !strings.HasPrefix(added.ID, "io-") {
		t.Fatalf("added = %+v", added)
	}
	out, stderr, err := e.run("--actor", "claude", "--why", "Owner asked.", "--minor", "--obvious", "inbox", "add", "Another capture", "--json")
	if err != nil || !strings.Contains(out, `"id": "io-`) {
		t.Fatalf("rated add: %v\n%s", err, out)
	}
	if !strings.Contains(stderr, "note: minor, obvious ignored") {
		t.Fatalf("stderr = %q, want the ratings note", stderr)
	}
}
