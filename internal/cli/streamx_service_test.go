package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/registry"
)

// Stream X: para doctor and para service install see the dispatch and tick config.

// doctorLines runs the dispatch and tick checks of one vault, without the rest
// of doctor: the rest probes the daemon of this machine, and no test may touch it.
func doctorLines(t *testing.T, e *env, id, path string) (string, int) {
	t.Helper()
	l := &loader{env: func(k string) string {
		if k == "PARA_HOME" {
			return e.home
		}
		return ""
	}}
	if err := l.load(); err != nil {
		t.Fatal(err)
	}
	var buf strings.Builder
	r := &report{w: &buf}
	doctorTick(l, r, id, path)
	doctorDispatch(l, r, id)
	return buf.String(), r.fails
}

func TestDoctorReportsDispatchAndTick(t *testing.T) {
	e := newEnv(t)
	vaultPath := e.init("acme")
	id, err := registry.Install{Home: e.home}.ID()
	if err != nil {
		t.Fatal(err)
	}
	out, fails := doctorLines(t, e, "acme", vaultPath)
	if fails != 0 || !strings.Contains(out, "ok    vault acme dispatch: off: no agent.json, para starts nothing") ||
		!strings.Contains(out, "warn  vault acme tick: no runner named") || !strings.Contains(out, "request.runner="+id) {
		t.Fatalf("doctor = %s", out)
	}
	props := filepath.Join(vaultPath, ".para", "vault.properties")
	cur, _ := os.ReadFile(props)
	if err := os.WriteFile(props, append(cur, []byte("request.runner="+id+"\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(t.TempDir(), "agent.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeAgentJSON(t, e, "acme", `{"command":["`+script+`","-p"],"cwd":"/nonexistent/work"}`, 0o600)
	out, fails = doctorLines(t, e, "acme", vaultPath)
	for _, want := range []string{"ok    vault acme tick: this install is the runner", "ok    vault acme dispatch: on: " + script + " (1 arguments), 1 at a time, 10 starts a day. A run starts an agent and spends tokens", "warn  vault acme dispatch cwd:"} {
		if !strings.Contains(out, want) {
			t.Errorf("doctor lacks %q:\n%s", want, out)
		}
	}
	if fails != 0 {
		t.Errorf("fails = %d:\n%s", fails, out)
	}
	// Another install is the runner.
	if err := os.WriteFile(props, append(cur, []byte("request.runner=someone-else\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, _ := doctorLines(t, e, "acme", vaultPath); !strings.Contains(out, "the runner is someone-else, not this install") {
		t.Errorf("doctor = %s", out)
	}
	// A bad file fails the doctor.
	writeAgentJSON(t, e, "acme", `{"command":["a"]}`, 0o666)
	if out, fails := doctorLines(t, e, "acme", vaultPath); fails != 1 || !strings.Contains(out, "fail  vault acme dispatch: ") || !strings.Contains(out, "writable by others") {
		t.Errorf("fails %d, doctor:\n%s", fails, out)
	}
}

// serve --check, which the service unit runs first, stops on a bad agent.json,
// and service install runs the same check, so a bad file is caught at install.
func TestServeCheckAndServiceInstallSeeDispatchConfig(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	if _, _, err := e.run("serve", "--check"); err != nil {
		t.Fatal(err)
	}
	writeAgentJSON(t, e, "acme", `{"command":"claude -p"}`, 0o600)
	if _, _, err := e.run("serve", "--check"); err == nil || !strings.Contains(err.Error(), "list of arguments") {
		t.Fatalf("serve --check err = %v", err)
	}
	if _, _, err := e.run("service", "install", "--print", "--exec", "/opt/para"); err == nil || !strings.Contains(err.Error(), "list of arguments") {
		t.Fatalf("service install err = %v", err)
	}
	// A good file, and a command that is only missing, still install.
	writeAgentJSON(t, e, "acme", `{"command":["/nonexistent/agent","-p"]}`, 0o600)
	out, stderr, err := e.run("service", "install", "--print", "--exec", "/opt/para")
	if err != nil || !strings.Contains(out, "ExecStart=/opt/para serve") {
		t.Fatalf("install: %v\n%s\n%s", err, out, stderr)
	}
	// The unit does not carry the agent config: it stays in the state directory.
	if strings.Contains(out, "/nonexistent/agent") {
		t.Errorf("the unit names the agent command:\n%s", out)
	}
}
