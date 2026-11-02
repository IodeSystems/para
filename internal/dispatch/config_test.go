package dispatch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseConfig(t *testing.T) {
	state := "/state/acme"
	ok, err := ParseConfig([]byte(`{"command":["claude","-p"]}`), state)
	if err != nil {
		t.Fatal(err)
	}
	if ok.MaxConcurrent != 1 || ok.DailyLimit != 10 || ok.Cwd != filepath.Join(state, "work") || len(ok.Command) != 2 {
		t.Errorf("defaults = %+v", ok)
	}
	full, err := ParseConfig([]byte(`{"command":["/opt/my agent/bin/run","--x"],"max_concurrent":3,"daily_limit":25,"cwd":"/srv/work"}`), state)
	if err != nil || full.MaxConcurrent != 3 || full.DailyLimit != 25 || full.Cwd != "/srv/work" {
		t.Errorf("full = %+v, %v", full, err)
	}
	for _, c := range []struct{ name, in, want string }{
		{"shell string", `{"command":"claude -p"}`, "list of arguments"},
		{"shell string in the first slot", `{"command":["claude -p"]}`, "not a shell string"},
		{"empty list", `{"command":[]}`, "command is required"},
		{"empty program", `{"command":[""]}`, "command is required"},
		{"missing", `{}`, "command is required"},
		{"nul", `{"command":["a\u0000b"]}`, "NUL"},
		{"unknown field", `{"command":["a"],"env":{"X":"1"}}`, "unknown field"},
		{"negative concurrency", `{"command":["a"],"max_concurrent":-1}`, "max_concurrent"},
		{"too much concurrency", `{"command":["a"],"max_concurrent":99}`, "max_concurrent"},
		{"negative limit", `{"command":["a"],"daily_limit":-5}`, "daily_limit"},
		{"huge limit", `{"command":["a"],"daily_limit":100000}`, "daily_limit"},
		{"relative cwd", `{"command":["a"],"cwd":"work"}`, "absolute"},
		{"two values", `{"command":["a"]} {"command":["b"]}`, "more than one"},
		{"not json", `command: claude`, "agent.json"},
	} {
		if _, err := ParseConfig([]byte(c.in), state); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
		}
	}
}

func TestLoadConfigFile(t *testing.T) {
	dir := t.TempDir()
	if c, err := LoadConfig(dir); c != nil || err != nil {
		t.Fatalf("no file: %v, %v", c, err)
	}
	path := ConfigPath(dir)
	if err := os.WriteFile(path, []byte(`{"command":["sh"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if c, err := LoadConfig(dir); c == nil || err != nil {
		t.Fatalf("0600: %v, %v", c, err)
	}
	for _, mode := range []os.FileMode{0o660, 0o666, 0o602} {
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadConfig(dir); err == nil || !strings.Contains(err.Error(), "writable by others") {
			t.Errorf("mode %o: err = %v", mode, err)
		}
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConfig(dir); err != nil {
		t.Errorf("0644 is readable by others but not writable: %v", err)
	}
}
