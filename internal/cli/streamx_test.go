package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/parasolve/internal/dispatch"
	"github.com/iodesystems/parasolve/internal/ops"
	"github.com/iodesystems/parasolve/internal/registry"
)

// Stream X: the per-machine dispatch config. No agent starts in these tests.

func writeAgentJSON(t *testing.T, e *env, id, content string, mode os.FileMode) string {
	t.Helper()
	dir := filepath.Join(e.home, "vaults", id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	p := dispatch.ConfigPath(dir)
	if err := os.WriteFile(p, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil { // the umask may have cleared bits
		t.Fatal(err)
	}
	return p
}

func TestAgentStatus(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	out := e.ok("agent", "status")
	if !strings.Contains(out, "not configured: para starts nothing") || !strings.Contains(out, "agent.json") {
		t.Fatalf("status = %q", out)
	}
	script := filepath.Join(t.TempDir(), "agent.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeAgentJSON(t, e, "acme", `{"command":["`+script+`","--key","SECRET-ARG"],"max_concurrent":2,"daily_limit":5}`, 0o600)
	out = e.ok("agent", "status")
	for _, want := range []string{"configured", script + " (2 arguments, not shown)", "2 at a time, 5 starts a day", "0 of 5 started", "serve\tnot running", "spends tokens"} {
		if !strings.Contains(out, want) {
			t.Errorf("status lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "SECRET-ARG") {
		t.Errorf("status shows an argument:\n%s", out)
	}
	var js struct {
		Vault      string `json:"vault"`
		Configured bool   `json:"configured"`
		Args       int    `json:"args"`
		DailyLimit int    `json:"daily_limit"`
		Running    []any  `json:"running"`
		Last       []any  `json:"last"`
	}
	e.json(&js, "agent", "status")
	if js.Vault != "acme" || !js.Configured || js.Args != 2 || js.DailyLimit != 5 || js.Running == nil || js.Last == nil {
		t.Errorf("json = %+v", js)
	}
	b, _ := json.Marshal(js)
	if strings.Contains(string(b), "SECRET-ARG") {
		t.Error("json shows an argument")
	}
	// A bad file is reported, not hidden.
	writeAgentJSON(t, e, "acme", `{"command":"claude -p"}`, 0o600)
	if out := e.ok("agent", "status"); !strings.Contains(out, "list of arguments") {
		t.Errorf("status = %q", out)
	}
}

// The real server, with a fake agent: it ticks on start and starts the command
// for a request that can be claimed here, with the request id in PARA_REQUEST.
func TestServeRunsTickAndDispatch(t *testing.T) {
	e := newEnv(t)
	vaultPath := e.init("acme")
	id, err := registry.Install{Home: e.home}.ID()
	if err != nil {
		t.Fatal(err)
	}
	props := filepath.Join(vaultPath, ".para", "vault.properties")
	cur, _ := os.ReadFile(props)
	if err := os.WriteFile(props, append(cur, []byte("request.runner="+id+"\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	var project ops.ProjectAddOutput
	e.json(&project, "project", "add", "--done-when", "Done.", "Portal", "--owner", "Jordan Example")
	var task ops.TaskAddOutput
	e.json(&task, "task", "add", "Rotate disks", "-p", project.ID, "--due", "2026-01-01", "--recur", "monthly")
	file := filepath.Join(vaultPath, "projects", task.ID+".md")
	raw, _ := os.ReadFile(file)
	if err := os.WriteFile(file, []byte(strings.Replace(string(raw), "status: todo", "status: done", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	var req ops.RequestAddOutput
	e.json(&req, "request", "add", "Summarize $(touch pwned) the portal project")

	out := t.TempDir()
	t.Setenv("OUT", out)
	script := filepath.Join(t.TempDir(), "agent.sh")
	body := "#!/bin/sh\necho \"$PARA_REQUEST\" > \"$OUT/id\"\nenv > \"$OUT/env\"\ntrap 'echo term > \"$OUT/term\"; exit 0' TERM\nwhile :; do sleep 0.05; done\n"
	if err := os.WriteFile(script, []byte(body), 0o700); err != nil {
		t.Fatal(err)
	}
	writeAgentJSON(t, e, "acme", `{"command":["`+script+`","-p"],"cwd":"`+out+`"}`, 0o600)

	_, stop := runningServer(t, e)
	waitFor := func(what string, ok func() bool) {
		t.Helper()
		for range 400 {
			if ok() {
				return
			}
			time.Sleep(25 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %s", what)
	}
	waitFor("the command", func() bool {
		b, _ := os.ReadFile(filepath.Join(out, "id"))
		return strings.TrimSpace(string(b)) == req.ID
	})
	if b, _ := os.ReadFile(filepath.Join(out, "env")); strings.Contains(string(b), "pwned") || strings.Contains(string(b), "Summarize") {
		t.Errorf("request text reached the command:\n%s", b)
	}
	if _, err := os.Stat(filepath.Join(out, "pwned")); err == nil {
		t.Error("vault text ran in a shell")
	}
	// The tick ran at start: the done recurring task has a next one.
	// The tick and the dispatch run apart, so the command can start first.
	var tasks ops.TaskListOutput
	waitFor("the next task", func() bool {
		tasks = ops.TaskListOutput{}
		e.json(&tasks, "task", "list", "-p", project.ID)
		return len(tasks.Tasks) == 2
	})
	if len(tasks.Tasks) != 2 {
		t.Errorf("tasks after the start tick = %d, want 2", len(tasks.Tasks))
	}
	var st struct {
		Started int `json:"started"`
		Running []struct {
			ID string `json:"id"`
		} `json:"running"`
		Serving bool `json:"serving"`
	}
	e.json(&st, "agent", "status")
	if st.Started != 1 || len(st.Running) != 1 || st.Running[0].ID != req.ID || !st.Serving {
		t.Errorf("status = %+v", st)
	}
	// Stopping the server sends the command SIGTERM and the server exits cleanly.
	if err := stop(); err != nil {
		t.Fatalf("server exit: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(out, "term")); !strings.Contains(string(b), "term") {
		t.Error("the command was not told to stop")
	}
	var tail ops.EventsTailOutput
	e.json(&tail, "events", "tail", "-n", "200")
	var ops_ []string
	for _, ev := range tail.Events {
		if strings.HasPrefix(ev.Op, "dispatch_") {
			ops_ = append(ops_, ev.Op+":"+ev.Doc+":"+ev.To)
		}
	}
	if len(ops_) != 2 || ops_[0] != "dispatch_start:"+req.ID+":" || !strings.HasPrefix(ops_[1], "dispatch_exit:"+req.ID+":") {
		t.Errorf("dispatch events = %v", ops_)
	}
}
