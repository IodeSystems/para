package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/ops"
)

func TestVaultConfigCLI(t *testing.T) {
	e := newEnv(t)
	path := e.init("acme")
	fail := func(args ...string) string {
		t.Helper()
		_, _, err := e.run(args...)
		if err == nil {
			t.Fatalf("para %v: want an error", args)
		}
		return err.Error()
	}

	if got := strings.TrimSpace(e.ok("vault", "config", "get", "egress.default")); got != "deny" {
		t.Fatalf("egress.default = %q", got)
	}
	e.ok("vault", "config", "set", "goals.active_limit", "5")
	if got := strings.TrimSpace(e.ok("vault", "config", "get", "goals.active_limit")); got != "5" {
		t.Fatalf("goals.active_limit = %q", got)
	}
	list := e.ok("vault", "config", "list")
	for _, want := range []string{"vault.id\tacme\t(read-only)", "goals.active_limit\t5", "align.days\t(unset)"} {
		if !strings.Contains(list, want) {
			t.Fatalf("list lacks %q:\n%s", want, list)
		}
	}

	// Checks: type, range, unknown key, read-only key.
	if out := fail("vault", "config", "set", "goals.active_limit", "many"); !strings.Contains(out, "whole number") {
		t.Fatalf("type: %s", out)
	}
	if out := fail("vault", "config", "set", "--", "align.days", "-1"); !strings.Contains(out, "0 or more") {
		t.Fatalf("range: %s", out)
	}
	if out := fail("vault", "config", "set", "egress.default", "maybe"); !strings.Contains(out, "allow or deny") {
		t.Fatalf("enum: %s", out)
	}
	if out := fail("vault", "config", "set", "align.dayz", "3"); !strings.Contains(out, "known settings are") || !strings.Contains(out, "align.days") {
		t.Fatalf("unknown: %s", out)
	}
	if out := fail("vault", "config", "set", "vault.id", "other"); !strings.Contains(out, "read-only") {
		t.Fatalf("vault.id: %s", out)
	}
	if out := fail("vault", "config", "unset", "vault.id"); !strings.Contains(out, "read-only") {
		t.Fatalf("unset vault.id: %s", out)
	}

	// Agent: why is required, and a setting cannot be undone, so every key is
	// the owner's.
	if out := fail("--actor", "claude", "vault", "config", "set", "align.days", "7"); !strings.Contains(out, "--why is required") {
		t.Fatalf("agent, no why: %s", out)
	}
	if out := fail("--actor", "claude", "--why", "The owner asked for a 7 day check.", "vault", "config", "set", "align.days", "7"); !strings.Contains(out, "cannot be undone") {
		t.Fatalf("agent tuning key: %s", out)
	}
	e.ok("vault", "config", "set", "align.days", "7")
	if out := fail("--actor", "claude", "--why", "Open it up.", "vault", "config", "set", "egress.default", "allow"); !strings.Contains(out, "only the owner") {
		t.Fatalf("agent egress: %s", out)
	}
	if out := fail("--actor", "claude", "--why", "Open it up.", "vault", "config", "unset", "egress.default"); !strings.Contains(out, "only the owner") {
		t.Fatalf("agent unset egress: %s", out)
	}
	if got := strings.TrimSpace(e.ok("vault", "config", "get", "egress.default")); got != "deny" {
		t.Fatalf("egress.default = %q after agent attempts", got)
	}

	// Unset keeps the other lines, and the file stays valid.
	e.ok("vault", "config", "unset", "goals.active_limit")
	e.ok("vault", "config", "unset", "goals.active_limit") // not set: no error
	data, err := os.ReadFile(filepath.Join(path, ".para", "vault.properties"))
	if err != nil {
		t.Fatal(err)
	}
	if s := string(data); strings.Contains(s, "goals.active_limit") || !strings.Contains(s, "vault.id=acme") || !strings.Contains(s, "align.days=7") {
		t.Fatalf("vault.properties:\n%s", s)
	}

	// Events hold the key, never the value.
	var sawSet, sawUnset bool
	var text string
	files, _ := filepath.Glob(filepath.Join(path, "events", "*.jsonl"))
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		text += string(raw)
	}
	for _, ev := range strings.Split(text, "\n") {
		if strings.Contains(ev, `"config_set"`) && strings.Contains(ev, "goals.active_limit") {
			sawSet = true
		}
		if strings.Contains(ev, `"config_unset"`) {
			sawUnset = true
		}
	}
	if !sawSet || !sawUnset || strings.Contains(text, "The owner asked") {
		t.Fatalf("events:\n%s", text)
	}

	var out ops.VaultConfigListOutput
	e.json(&out, "vault", "config", "list")
	if len(out.Settings) < len(ops.ConfigKeyNames()) {
		t.Fatalf("list json: %+v", out)
	}
}
