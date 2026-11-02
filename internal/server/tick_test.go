package server

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iodesystems/parasolve/internal/ops"
)

// vaultPath is the root of a fixture vault.
func (f *fixture) vaultPath(id string) string {
	f.t.Helper()
	tn, ok := f.hub.Lookup(id)
	if !ok {
		f.t.Fatalf("no vault %s", id)
	}
	return tn.(*Tenant).hide[0]
}

func TestTickAllFollowsTheRunnerProperty(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	byVault := func() map[string]bool {
		out := map[string]bool{}
		for _, r := range f.hub.TickAll(ctx) {
			if r.Error != "" {
				t.Fatalf("%s: %s", r.Vault, r.Error)
			}
			out[r.Vault] = r.Ran
		}
		return out
	}
	// Neither vault names a runner: nothing ticks.
	if got := byVault(); got["acme"] || got["beta"] || len(got) != 2 {
		t.Fatalf("no runner: %v", got)
	}
	install, err := f.hub.install.ID()
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(f.vaultPath("acme"), ".para", "vault.properties")
	cur, _ := os.ReadFile(p)
	if err := os.WriteFile(p, append(cur, []byte("request.runner="+install+"\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := byVault(); !got["acme"] || got["beta"] {
		t.Fatalf("acme names this install: %v", got)
	}
	// A vault whose runner is another machine is skipped, not an error.
	if err := os.WriteFile(p, append(cur, []byte("request.runner=someone-else\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := byVault(); got["acme"] {
		t.Fatalf("another runner: %v", got)
	}
}

// The ticker says a skipped vault once, not on every pass.
func TestTickerLogsASkipOnce(t *testing.T) {
	f := newFixture(t)
	var buf bytes.Buffer
	f.hub.log = slog.New(slog.NewTextHandler(&buf, nil))
	tk := NewTicker(f.hub, MinTickInterval)
	passes := 0
	tk.After = func(context.Context, []ops.TickOutput) { passes++ }
	for range 3 {
		tk.once(context.Background())
	}
	if passes != 3 {
		t.Errorf("After ran %d times", passes)
	}
	if n := strings.Count(buf.String(), "tick skipped"); n != 2 { // acme and beta, once each
		t.Errorf("skip logged %d times:\n%s", n, buf.String())
	}
}
