package ops

import (
	"os"
	"path/filepath"
	"testing"
)

// noGate sets gate.require=none in a test vault: tests written before the gate
// expect agent writes to apply. gate_test.go uses the default.
func noGate(t testing.TB, root string) {
	t.Helper()
	p := filepath.Join(root, ".para", "vault.properties")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) > 0 && b[len(b)-1] != '\n' {
		b = append(b, '\n')
	}
	if err := os.WriteFile(p, append(b, "gate.require=none\n"...), 0o600); err != nil {
		t.Fatal(err)
	}
}
