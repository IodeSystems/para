package cli

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// setupEnv is a machine with no kind, a fake service manager that records its
// calls, and a PATH with git and, when withClaude, a claude that records its
// arguments in the returned file.
func setupEnv(t *testing.T, withClaude bool) (e *env, claudeLog string, svcCalls *[]string) {
	t.Helper()
	e = newEnvNoKind(t)
	bin := filepath.Join(e.dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tool := range []string{"git", "sh", "cat", "tail"} {
		if p, err := exec.LookPath(tool); err == nil {
			if err := os.Symlink(p, filepath.Join(bin, tool)); err != nil {
				t.Fatal(err)
			}
		}
	}
	claudeLog = filepath.Join(e.dir, "claude.log")
	if withClaude {
		script := "#!/bin/sh\necho \"$@\" >> " + claudeLog + "\n"
		if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	calls := []string{}
	zero := time.Duration(0)
	e.hooks = &setupHooks{wait: &zero, svcRun: func(_ io.Writer, name string, args ...string) error {
		calls = append(calls, name+" "+strings.Join(args, " "))
		return nil
	}}
	return e, claudeLog, &calls
}

func TestSetupFullRunAndRerun(t *testing.T) {
	e, claudeLog, calls := setupEnv(t, true)
	vpath := filepath.Join(e.dir, "life")
	out := e.ok("setup", "--kind", "work", "--vault", "life", "--path", vpath, "--service", "--agent", "claude")
	for _, want := range []string{
		"✓ doctor: 0 failed",
		"machine kind: work (recorded",
		"vault: life at " + vpath,
		"service: installed and started",
		"sign-in: no server is running. Start it: para service start, or para serve",
		"agent: Claude Code has para-life",
		"agent: para skill installed",
		"setup: done",
		"`bootstrap` prompt",
		"para mcp --read-only --vault <id> --actor <project>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("setup output lacks %q:\n%s", want, out)
		}
	}
	if got, _ := os.ReadFile(claudeLog); !strings.HasPrefix(string(got), "mcp add --scope user para-life -- /") || !strings.Contains(string(got), "--actor claude") {
		t.Errorf("claude was run with %q", got)
	}
	if len(*calls) == 0 || !strings.Contains((*calls)[len(*calls)-1], "enable --now") {
		t.Errorf("service manager calls: %v", *calls)
	}
	if _, err := os.Stat(filepath.Join(e.dir, ".claude", "skills", "para", "SKILL.md")); err != nil {
		t.Error(err)
	}
	props, _ := os.ReadFile(filepath.Join(vpath, ".para", "vault.properties"))
	if !strings.Contains(string(props), "vault.kind=work") {
		t.Errorf("vault.properties = %s", props)
	}

	// Rerun: Claude Code now lists the server (the fake does not write it).
	if err := os.WriteFile(filepath.Join(e.dir, ".claude.json"), []byte(`{"mcpServers":{"para-life":{}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(claudeLog)
	n := len(*calls)
	out = e.ok("setup", "--kind", "work", "--service", "--agent", "claude")
	for _, want := range []string{"(recorded)", "vault: life (registered)", "service: installed (already)", "Claude Code already has para-life and the para skill"} {
		if !strings.Contains(out, want) {
			t.Errorf("rerun lacks %q:\n%s", want, out)
		}
	}
	if after, _ := os.ReadFile(claudeLog); string(after) != string(before) || len(*calls) != n {
		t.Errorf("rerun ran a command: claude %q, service calls %v", after, (*calls)[n:])
	}
	if _, _, err := e.run("setup", "--kind", "personal"); err == nil || !strings.Contains(err.Error(), "never share") {
		t.Errorf("other kind on a work machine: %v", err)
	}
}

func TestSetupNoTTYListsMissingFlags(t *testing.T) {
	e, _, calls := setupEnv(t, true)
	_, errOut, err := e.run("setup")
	if err == nil {
		t.Fatal("setup with no answers and no terminal must fail")
	}
	for _, want := range []string{"--kind work|personal", "--vault <id> --path <dir>", "--service or --no-service"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr lacks %q:\n%s", want, errOut)
		}
	}
	if k, _ := e.install().Kind(); k != "" || len(*calls) != 0 {
		t.Errorf("setup changed things before failing: kind %q, calls %v", k, *calls)
	}
}

func TestSetupClaudeNotOnPATHPrintsLine(t *testing.T) {
	e, claudeLog, _ := setupEnv(t, false)
	out := e.ok("setup", "--kind", "personal", "--vault", "life", "--path", filepath.Join(e.dir, "life"), "--no-service", "--agent", "claude")
	for _, want := range []string{"claude is not on PATH", "claude mcp add --scope user para-life -- /", "para skill install", "service: skipped"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if _, err := os.Stat(claudeLog); err == nil {
		t.Error("claude ran")
	}
	if _, err := os.Stat(filepath.Join(e.dir, ".claude", "skills", "para", "SKILL.md")); err == nil {
		t.Error("the skill was installed without claude")
	}
}

func TestSetupPrompts(t *testing.T) {
	e, _, calls := setupEnv(t, true)
	e.hooks.tty = func(io.Reader) bool { return true }
	vpath := filepath.Join(e.dir, "asked")
	// kind, vault id, path, service (no), agent (default no).
	e.stdin = "personal\nasked\n" + vpath + "\nn\n\n"
	out := e.ok("setup")
	for _, want := range []string{"machine kind: personal", "vault: asked at " + vpath, "service: skipped", "agent: skipped"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if len(*calls) != 0 {
		t.Errorf("service manager ran: %v", *calls)
	}
}

func TestSetupGenericAgent(t *testing.T) {
	e, _, _ := setupEnv(t, false)
	e.ok("init", "acme", filepath.Join(e.dir, "acme"), "--kind", "personal")
	// No server: it says so and skips.
	out := e.ok("setup", "--no-service", "--agent", "generic", "--agent-name", "bot")
	if !strings.Contains(out, "agent: no server is running. Start it: para service start, or para serve") {
		t.Errorf("no server:\n%s", out)
	}
	port, _ := runningServer(t, e)
	out = e.ok("setup", "--no-service", "--agent", "generic", "--agent-name", "bot", "--print")
	for _, want := range []string{"name:  bot", "url:   http://acme.localhost:" + port + "/mcp", "token: ", "shown once"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	var ks []struct {
		Name   string   `json:"name"`
		Scopes []string `json:"scopes"`
	}
	e.json(&ks, "key", "list")
	found := false
	for _, k := range ks {
		found = found || k.Name == "bot"
	}
	if !found {
		t.Errorf("keys: %+v", ks)
	}
	if out = e.ok("setup", "--no-service", "--agent", "generic", "--agent-name", "bot"); !strings.Contains(out, "key bot exists") {
		t.Errorf("rerun:\n%s", out)
	}
}

func TestMachineKindRules(t *testing.T) {
	e := newEnvNoKind(t)
	if _, _, err := e.run("init", "a", filepath.Join(e.dir, "a")); err == nil || !strings.Contains(err.Error(), "para setup") || !strings.Contains(err.Error(), "--kind work|personal") {
		t.Fatalf("init with no kind: %v", err)
	}
	if _, _, err := e.run("init", "a", filepath.Join(e.dir, "a"), "--kind", "both"); err == nil {
		t.Error("bad kind accepted")
	}
	a := filepath.Join(e.dir, "a")
	e.ok("init", "a", a, "--kind", "work")
	if k, _ := e.install().Kind(); k != "work" {
		t.Errorf("machine kind = %q", k)
	}
	e.ok("init", "b", filepath.Join(e.dir, "b")) // the machine kind is enough now
	if b, _ := os.ReadFile(filepath.Join(e.dir, "b", ".para", "vault.properties")); !strings.Contains(string(b), "vault.kind=work") {
		t.Errorf("vault b: %s", b)
	}
	if _, _, err := e.run("init", "c", filepath.Join(e.dir, "c"), "--kind", "personal"); err == nil || !strings.Contains(err.Error(), "never share") {
		t.Errorf("other kind: %v", err)
	}

	// Doctor: kind and vault agree, then a vault of the other kind fails.
	out, _, err := e.run("doctor")
	if err != nil || !strings.Contains(out, "ok    machine kind: work") || !strings.Contains(out, "ok    vault a kind: work") {
		t.Errorf("doctor: %v\n%s", err, out)
	}
	props := filepath.Join(a, ".para", "vault.properties")
	raw, _ := os.ReadFile(props)
	if err := os.WriteFile(props, []byte(strings.Replace(string(raw), "vault.kind=work", "vault.kind=personal", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	out, _, err = e.run("doctor")
	if err == nil || !strings.Contains(out, "fail  vault a kind") {
		t.Errorf("doctor on a personal vault on a work machine: %v\n%s", err, out)
	}
	if _, _, err := e.run("--vault", "a", "agenda", "--days", "1"); err == nil || !strings.Contains(err.Error(), "never share") {
		t.Errorf("open a vault of the other kind: %v", err)
	}
	// No kind on the vault: a warning.
	if err := os.WriteFile(props, []byte(strings.Replace(string(raw), "vault.kind=work\n", "", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, _, _ = e.run("doctor"); !strings.Contains(out, "warn  vault a kind") {
		t.Errorf("doctor, vault without kind:\n%s", out)
	}
	// No machine kind: a warning.
	e2 := newEnvNoKind(t)
	if out, _, _ = e2.run("doctor"); !strings.Contains(out, "warn  machine kind") {
		t.Errorf("doctor, no machine kind:\n%s", out)
	}
}

func TestLoginWithNoServer(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	for _, args := range [][]string{{"login", "--print"}, {"serve", "login", "--print"}} {
		out, _, err := e.run(args...)
		if err == nil || !strings.Contains(err.Error(), "no server is running") || !strings.Contains(err.Error(), "Start it: para service start, or para serve") {
			t.Errorf("%v: %v", args, err)
		}
		if out != "" {
			t.Errorf("%v printed %q", args, out)
		}
	}
}

func TestSetupStampsVaultKinds(t *testing.T) {
	e := newEnvNoKind(t)
	// mk makes a vault on a scratch machine and registers it here.
	mk := func(id, kind string, strip bool) string {
		p := filepath.Join(e.dir, id)
		newEnvNoKind(t).ok("init", id, p, "--kind", kind)
		pp := filepath.Join(p, ".para", "vault.properties")
		if strip {
			raw, _ := os.ReadFile(pp)
			if err := os.WriteFile(pp, []byte(strings.Replace(string(raw), "vault.kind="+kind+"\n", "", 1)), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		if err := e.install().Add(id, p); err != nil {
			t.Fatal(err)
		}
		return pp
	}
	bare := mk("bare", "work", true)
	e.hooks = &setupHooks{tty: func(io.Reader) bool { return false }}
	setup := func() string {
		return e.ok("setup", "--kind", "work", "--no-service", "--agent", "none")
	}
	// Machine kind not yet recorded: setup records it and stamps the vault.
	if out := setup(); !strings.Contains(out, "vault kind: bare marked work") {
		t.Errorf("no stamp line:\n%s", out)
	}
	b, _ := os.ReadFile(bare)
	if !strings.Contains(string(b), "vault.kind=work") || !strings.Contains(string(b), "vault.id=bare") {
		t.Errorf("bare: %s", b)
	}
	// Rerun: nothing to stamp, file unchanged.
	if out := setup(); strings.Contains(out, "marked") {
		t.Errorf("rerun stamped:\n%s", out)
	}
	if again, _ := os.ReadFile(bare); string(again) != string(b) {
		t.Errorf("rerun changed bare:\n%s", again)
	}
	// Machine kind recorded; a vault of the other kind is reported, not changed.
	other := mk("other", "personal", false)
	otherBefore, _ := os.ReadFile(other)
	out, _, err := e.run("setup", "--kind", "work", "--no-service", "--agent", "none")
	if err == nil || !strings.Contains(out, "vault other kind") || strings.Contains(out, "marked") {
		t.Errorf("other-kind vault: %v\n%s", err, out)
	}
	if o, _ := os.ReadFile(other); string(o) != string(otherBefore) {
		t.Errorf("other changed: %s", o)
	}
}
