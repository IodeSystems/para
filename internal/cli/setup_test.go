package cli

import (
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/keys"
	"github.com/iodesystems/para/internal/server"
)

// setupEnv is a machine with no domains, a fake service manager that records its
// calls, and a PATH with git and, when withClaude, a claude that records its
// arguments in the returned file.
func setupEnv(t *testing.T, withClaude bool) (e *env, claudeLog string, svcCalls *[]string) {
	t.Helper()
	e = newEnv(t)
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
	out := e.ok("setup", "--vault", "life", "--path", vpath, "--service", "--agent", "claude")
	for _, want := range []string{
		"✓ doctor: 0 failed",
		"domains: life added to machine.domains",
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
	if !strings.Contains(string(props), "vault.domain=life") {
		t.Errorf("vault.properties = %s", props)
	}
	if d, _ := e.install().Domains(); strings.Join(d, ",") != "life" {
		t.Errorf("machine domains = %v", d)
	}
	if v, ok := installedSkill(""); !ok || v != "test" {
		t.Errorf("skill version = %q, %v", v, ok)
	}

	// Rerun: Claude Code now lists the server (the fake does not write it).
	if err := os.WriteFile(filepath.Join(e.dir, ".claude.json"), []byte(`{"mcpServers":{"para-life":{}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(claudeLog)
	n := len(*calls)
	out = e.ok("setup", "--service", "--agent", "claude")
	for _, want := range []string{"domains: life (recorded)", "vault: life (registered)", "service: installed (already)", "Claude Code already has para-life and the para skill"} {
		if !strings.Contains(out, want) {
			t.Errorf("rerun lacks %q:\n%s", want, out)
		}
	}
	if after, _ := os.ReadFile(claudeLog); string(after) != string(before) || len(*calls) != n {
		t.Errorf("rerun ran a command: claude %q, service calls %v", after, (*calls)[n:])
	}
}

func TestSetupNoTTYListsMissingFlags(t *testing.T) {
	e, _, calls := setupEnv(t, true)
	_, errOut, err := e.run("setup")
	if err == nil {
		t.Fatal("setup with no answers and no terminal must fail")
	}
	for _, want := range []string{"--vault <id> --path <dir>", "--service or --no-service"} {
		if !strings.Contains(errOut, want) {
			t.Errorf("stderr lacks %q:\n%s", want, errOut)
		}
	}
	if d, _ := e.install().Domains(); len(d) != 0 || len(*calls) != 0 {
		t.Errorf("setup changed things before failing: domains %v, calls %v", d, *calls)
	}
}

func TestSetupClaudeNotOnPATHPrintsLine(t *testing.T) {
	e, claudeLog, _ := setupEnv(t, false)
	out := e.ok("setup", "--vault", "life", "--path", filepath.Join(e.dir, "life"), "--no-service", "--agent", "claude")
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
	// vault id, path, service (no), agent (default no).
	e.stdin = "asked\n" + vpath + "\nn\n\n"
	out := e.ok("setup")
	for _, want := range []string{"domains: asked added to machine.domains", "vault: asked at " + vpath, "service: skipped", "agent: skipped"} {
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
	e.ok("init", "acme", filepath.Join(e.dir, "acme"))
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

func vaultProps(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, ".para", "vault.properties"))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func rewriteProps(t *testing.T, dir, from, to string) {
	t.Helper()
	p := filepath.Join(dir, ".para", "vault.properties")
	raw, _ := os.ReadFile(p)
	if err := os.WriteFile(p, []byte(strings.Replace(string(raw), from, to, 1)), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDomainRules(t *testing.T) {
	e := newEnv(t)
	a := filepath.Join(e.dir, "a")
	out := e.ok("init", "a", a, "--domain", "iode")
	if !strings.Contains(out, "domain: iode; added to machine.domains") {
		t.Errorf("init output:\n%s", out)
	}
	if !strings.Contains(vaultProps(t, a), "vault.domain=iode") {
		t.Errorf("vault a: %s", vaultProps(t, a))
	}
	// Default domain is the id; the list gains it.
	b := filepath.Join(e.dir, "b")
	e.ok("init", "b", b)
	if !strings.Contains(vaultProps(t, b), "vault.domain=b") {
		t.Errorf("vault b: %s", vaultProps(t, b))
	}
	if d, _ := e.install().Domains(); strings.Join(d, ",") != "iode,b" {
		t.Errorf("machine domains = %v", d)
	}
	if out = e.ok("init", "c", filepath.Join(e.dir, "c"), "--domain", "iode"); strings.Contains(out, "added to machine.domains") {
		t.Errorf("a held domain was reported as added:\n%s", out)
	}
	if _, _, err := e.run("init", "d", filepath.Join(e.dir, "d"), "--domain", "Bad Word"); err == nil {
		t.Error("bad domain accepted")
	}

	// A vault of a domain the machine does not hold is refused, on every open.
	rewriteProps(t, a, "vault.domain=iode", "vault.domain=corp")
	_, _, err := e.run("--vault", "a", "agenda", "--days", "1")
	if err == nil || !strings.Contains(err.Error(), "vault a is domain corp, which this machine does not hold (machine.domains: iode, b)") ||
		!strings.Contains(err.Error(), "para setup --allow-domain corp if it belongs here") {
		t.Errorf("open a vault of another domain: %v", err)
	}
	if _, _, err := e.run("--vault", "a", "mcp", "--read-only", "--actor", "x"); err == nil || !strings.Contains(err.Error(), "does not hold") {
		t.Errorf("mcp: %v", err)
	}
	if _, _, err := e.run("vault", "use-check", "--vault", "a"); err == nil || !strings.Contains(err.Error(), "does not hold") {
		t.Errorf("use-check: %v", err)
	}
	out, _, err = e.run("doctor")
	if !strings.Contains(out, "warn  vault a domain: corp is not in machine.domains (iode, b); it will not open") {
		t.Errorf("doctor: %v\n%s", err, out)
	}
}

func TestDomainWarnings(t *testing.T) {
	e := newEnv(t)
	a := filepath.Join(e.dir, "a")
	e.ok("init", "a", a)
	out, _, err := e.run("doctor")
	if err != nil || !strings.Contains(out, "ok    machine domains: a") || !strings.Contains(out, "ok    vault a domain: a") {
		t.Errorf("doctor: %v\n%s", err, out)
	}
	// A vault with no domain opens and doctor warns.
	rewriteProps(t, a, "vault.domain=a\n", "")
	if _, _, err := e.run("--vault", "a", "agenda", "--days", "1"); err != nil {
		t.Errorf("vault with no domain: %v", err)
	}
	if out, _, _ = e.run("doctor"); !strings.Contains(out, "warn  vault a domain") {
		t.Errorf("doctor, vault without domain:\n%s", out)
	}
	// A machine with no list opens a vault of any domain and doctor warns.
	rewriteProps(t, a, "egress.default=deny", "egress.default=deny\nvault.domain=corp")
	if err := e.install().SetDomains(nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.run("--vault", "a", "agenda", "--days", "1"); err != nil {
		t.Errorf("machine with no domains: %v", err)
	}
	if out, _, _ = e.run("doctor"); !strings.Contains(out, "warn  machine domains") {
		t.Errorf("doctor, no machine domains:\n%s", out)
	}
	// Retired kind keys are ignored and doctor says so.
	rewriteProps(t, a, "vault.domain=corp", "vault.domain=corp\nvault.kind=work")
	f := filepath.Join(e.home, "install.properties")
	raw, _ := os.ReadFile(f)
	if err := os.WriteFile(f, append(raw, []byte("machine.kind=personal\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.run("--vault", "a", "agenda", "--days", "1"); err != nil {
		t.Errorf("legacy kind keys must be ignored: %v", err)
	}
	out, _, _ = e.run("doctor")
	for _, w := range []string{"warn  machine.kind: is no longer used; para setup records domains", "warn  vault a vault.kind: is no longer used"} {
		if !strings.Contains(out, w) {
			t.Errorf("doctor lacks %q:\n%s", w, out)
		}
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

func TestSetupDomainsStep(t *testing.T) {
	e := newEnv(t)
	// mk makes a vault on a scratch machine and registers it here, with no domain.
	mk := func(id string) string {
		p := filepath.Join(e.dir, id)
		newEnv(t).ok("init", id, p)
		rewriteProps(t, p, "vault.domain="+id+"\n", "")
		if err := e.install().Add(id, p); err != nil {
			t.Fatal(err)
		}
		return p
	}
	iode, life := mk("iode"), mk("life")
	t.Setenv("HOME", e.dir) // newEnv above moved it
	e.hooks = &setupHooks{tty: func(io.Reader) bool { return false }}
	// No terminal and no answer: listed, nothing changed.
	_, errOut, err := e.run("setup", "--no-service", "--agent", "none")
	if err == nil || !strings.Contains(errOut, "--domain iode=<domain>") || !strings.Contains(errOut, "--domain life=<domain>") || !strings.Contains(errOut, "--domains-default") {
		t.Errorf("missing answers: %v\n%s", err, errOut)
	}
	if strings.Contains(vaultProps(t, iode), "vault.domain") {
		t.Error("setup stamped before failing")
	}
	// An unknown vault and a bad domain are refused.
	if _, _, err := e.run("setup", "--domain", "nope=x", "--no-service"); err == nil {
		t.Error("--domain for an unknown vault accepted")
	}
	if _, _, err := e.run("setup", "--domain", "iode=Bad", "--no-service"); err == nil {
		t.Error("bad --domain accepted")
	}
	out := e.ok("setup", "--domain", "iode=iode", "--domains-default", "--allow-domain", "corp", "--no-service", "--agent", "none")
	for _, want := range []string{"domain: iode marked iode", "domain: life marked life", "This machine holds domains: iode, life, corp", "recorded in"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if !strings.Contains(vaultProps(t, iode), "vault.domain=iode") || !strings.Contains(vaultProps(t, life), "vault.id=life") {
		t.Errorf("props: %s %s", vaultProps(t, iode), vaultProps(t, life))
	}
	if d, _ := e.install().Domains(); strings.Join(d, ",") != "iode,life,corp" {
		t.Errorf("machine domains = %v", d)
	}
	// Rerun: nothing to stamp, files unchanged.
	before := vaultProps(t, life)
	if out = e.ok("setup", "--no-service", "--agent", "none"); strings.Contains(out, "marked") || !strings.Contains(out, "domains: iode, life, corp (recorded)") {
		t.Errorf("rerun:\n%s", out)
	}
	if vaultProps(t, life) != before {
		t.Error("rerun changed life")
	}
	// A different domain by flag on a marked vault is refused.
	if _, _, err := e.run("setup", "--domain", "life=corp", "--no-service", "--agent", "none"); err == nil || !strings.Contains(err.Error(), "would change it") {
		t.Errorf("changing a domain: %v", err)
	}
	// A vault of a domain the list lacks: not added without a terminal.
	rewriteProps(t, life, "vault.domain=life", "vault.domain=other")
	out = e.ok("setup", "--no-service", "--agent", "none")
	if !strings.Contains(out, "para setup --allow-domain other adds it") {
		t.Errorf("unlisted domain:\n%s", out)
	}
	out = e.ok("setup", "--allow-domain", "other", "--no-service", "--agent", "none")
	if d, _ := e.install().Domains(); !strings.Contains(strings.Join(d, ","), "other") {
		t.Errorf("--allow-domain: %v\n%s", d, out)
	}
}

func TestSetupDomainPrompts(t *testing.T) {
	e := newEnv(t)
	for _, id := range []string{"iode", "life"} {
		p := filepath.Join(e.dir, id)
		newEnv(t).ok("init", id, p)
		rewriteProps(t, p, "vault.domain="+id+"\n", "")
		if err := e.install().Add(id, p); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", e.dir)
	e.hooks = &setupHooks{tty: func(io.Reader) bool { return true }}
	// iode: default; life: "personal"; confirm; service no; agent no.
	e.stdin = "\npersonal\n\nn\n\n"
	out := e.ok("setup")
	if !strings.Contains(out, "This machine holds domains: iode, personal") {
		t.Errorf("output:\n%s", out)
	}
	if d, _ := e.install().Domains(); strings.Join(d, ",") != "iode,personal" {
		t.Errorf("domains = %v", d)
	}
}

func TestSetupSkillVersion(t *testing.T) {
	e, claudeLog, _ := setupEnv(t, true)
	e.ok("init", "life", filepath.Join(e.dir, "life"))
	e.ok("skill", "install")
	skill := filepath.Join(e.dir, ".claude", "skills", "para")
	if b, _ := os.ReadFile(filepath.Join(skill, skillVersionFile)); strings.TrimSpace(string(b)) != "test" {
		t.Fatalf("version file = %q", b)
	}
	// Current: nothing to do, even with --agent none.
	if out := e.ok("setup", "--no-service", "--agent", "none"); strings.Contains(out, "skill") {
		t.Errorf("current skill touched:\n%s", out)
	}
	if out, _, _ := e.run("doctor"); strings.Contains(out, "para skill") {
		t.Errorf("doctor warns on a current skill:\n%s", out)
	}
	// Stale: updated, and doctor warned first.
	if err := os.WriteFile(filepath.Join(skill, skillVersionFile), []byte("v0.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, _, _ := e.run("doctor"); !strings.Contains(out, "warn  para skill: is version v0.1, this para is test") {
		t.Errorf("doctor on a stale skill:\n%s", out)
	}
	if out := e.ok("setup", "--no-service", "--agent", "none"); !strings.Contains(out, "agent: para skill updated (v0.1 → test)") {
		t.Errorf("stale skill:\n%s", out)
	}
	if b, _ := os.ReadFile(filepath.Join(skill, skillVersionFile)); strings.TrimSpace(string(b)) != "test" {
		t.Errorf("version file = %q", b)
	}
	// No marker (installed before versions): unknown, then updated.
	if err := os.Remove(filepath.Join(skill, skillVersionFile)); err != nil {
		t.Fatal(err)
	}
	if out := e.ok("setup", "--no-service", "--agent", "none"); !strings.Contains(out, "(unknown → test)") {
		t.Errorf("unmarked skill:\n%s", out)
	}
	// Missing: installed only with --agent claude.
	if err := os.RemoveAll(skill); err != nil {
		t.Fatal(err)
	}
	if out := e.ok("setup", "--no-service", "--agent", "none"); strings.Contains(out, "skill installed") {
		t.Errorf("none installed the skill:\n%s", out)
	}
	if out := e.ok("setup", "--no-service", "--agent", "claude"); !strings.Contains(out, "agent: para skill installed") {
		t.Errorf("claude:\n%s", out)
	}
	_ = claudeLog
}

func TestSetupSignInSkippedWhileLoginFresh(t *testing.T) {
	e, _, _ := setupEnv(t, false)
	e.ok("init", "acme", filepath.Join(e.dir, "acme"))
	runningServer(t, e)
	store := keys.New(server.ServerDir(e.install(), "acme"))
	run := func(args ...string) string {
		return e.ok(append([]string{"setup", "--no-service", "--agent", "none", "--print"}, args...)...)
	}
	// No login key: signs in (prints a URL).
	if out := run(); !strings.Contains(out, "sign-in: done") {
		t.Errorf("first:\n%s", out)
	}
	act, _ := store.ActiveLogins()
	n := len(act)
	if n == 0 {
		t.Fatal("no login key made")
	}
	if out := run(); !strings.Contains(out, "sign-in: already (para login to sign in again)") {
		t.Errorf("second:\n%s", out)
	}
	if act, _ = store.ActiveLogins(); len(act) != n {
		t.Errorf("skip minted a key: %d -> %d", n, len(act))
	}
	if out := run("--login"); !strings.Contains(out, "sign-in: done") {
		t.Errorf("--login:\n%s", out)
	}
	if act, _ = store.ActiveLogins(); len(act) != n+1 {
		t.Errorf("--login keys: %d", len(act))
	}
	// Expired and revoked keys do not count.
	for _, k := range act {
		if _, err := store.Revoke(k.Name); err != nil {
			t.Fatal(err)
		}
	}
	if out := run(); !strings.Contains(out, "sign-in: done") {
		t.Errorf("after revoke:\n%s", out)
	}
}

// snapshot reads every file under dirs: path, mode, and bytes.
func snapshot(t *testing.T, dirs ...string) map[string]string {
	t.Helper()
	got := map[string]string{}
	for _, d := range dirs {
		err := filepath.Walk(d, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				got[p] = "dir"
				return nil
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			got[p] = info.Mode().String() + "\n" + string(b)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return got
}

func TestSetupDryRunChangesNothing(t *testing.T) {
	e, claudeLog, calls := setupEnv(t, true)
	for _, id := range []string{"iode", "life"} {
		p := filepath.Join(e.dir, id)
		newEnv(t).ok("init", id, p)
		rewriteProps(t, p, "vault.domain="+id+"\n", "")
		if err := e.install().Add(id, p); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", e.dir) // newEnv above moved it
	// An old skill that a real run would update.
	skill := filepath.Join(e.dir, ".claude", "skills", "para")
	if err := os.MkdirAll(skill, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	dirs := []string{e.home, filepath.Join(e.dir, "iode"), filepath.Join(e.dir, "life"), filepath.Join(e.dir, ".claude")}
	before := snapshot(t, dirs...)
	for _, args := range [][]string{
		{"setup", "--dry-run"},
		{"setup", "--dry-run", "--vault", "iode", "--domains-default", "--service", "--agent", "claude", "--login"},
		{"setup", "--dry-run", "--vault", "iode", "--domain", "iode=corp", "--domain", "life=life", "--allow-domain", "x", "--service", "--agent", "generic", "--agent-name", "bot"},
	} {
		out := e.ok(args...)
		if !strings.Contains(out, "dry run: nothing is written") || !strings.Contains(out, "setup: dry run, nothing changed") {
			t.Errorf("%v:\n%s", args, out)
		}
	}
	out := e.ok("setup", "--dry-run", "--vault", "iode", "--domains-default", "--service", "--agent", "claude")
	for _, want := range []string{
		"domain: iode would be marked iode", "write " + filepath.Join(e.dir, "iode", ".para", "vault.properties") + ": vault.domain=iode",
		"This machine holds domains: iode, life", "machine.domains=iode,life",
		"would run: para service install --exec", "would run: para service start",
		"sign-in: would run para login once the server is up",
		"would run claude mcp add for para-iode", "agent: would update para skill (unknown → test)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dry run lacks %q:\n%s", want, out)
		}
	}
	if after := snapshot(t, dirs...); !reflect.DeepEqual(before, after) {
		for k, v := range after {
			if before[k] != v {
				t.Errorf("changed: %s", k)
			}
		}
		for k := range before {
			if _, ok := after[k]; !ok {
				t.Errorf("removed: %s", k)
			}
		}
	}
	if len(*calls) != 0 {
		t.Errorf("service manager ran: %v", *calls)
	}
	if _, err := os.Stat(claudeLog); err == nil {
		t.Error("claude ran")
	}
}

func TestSetupDryRunFreshHomeCreatesNothing(t *testing.T) {
	e, _, _ := setupEnv(t, false)
	before := snapshot(t, e.dir)
	out := e.ok("setup", "--dry-run", "--vault", "life", "--path", filepath.Join(e.dir, "life"), "--no-service")
	if !strings.Contains(out, "vault: life at") || !strings.Contains(out, "machine.domains gains life") {
		t.Errorf("output:\n%s", out)
	}
	if after := snapshot(t, e.dir); !reflect.DeepEqual(before, after) {
		t.Errorf("dry run on a fresh home wrote files: %d -> %d entries", len(before), len(after))
	}
}

func TestIsTerminalRejectsDevNull(t *testing.T) {
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Skip(err)
	}
	defer f.Close()
	if isTerminal(f) {
		t.Error("/dev/null counted as a terminal")
	}
}

func TestSetupNoTTYDomainsDefaultRecordsWithoutPrompt(t *testing.T) {
	e := newEnv(t)
	p := filepath.Join(e.dir, "iode")
	newEnv(t).ok("init", "iode", p)
	rewriteProps(t, p, "vault.domain=iode\n", "")
	if err := e.install().Add("iode", p); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", e.dir)
	e.hooks = &setupHooks{tty: func(io.Reader) bool { return false }}
	out := e.ok("setup", "--domains-default", "--no-service", "--agent", "none")
	if strings.Contains(out, "[Y/n]") || !strings.Contains(out, "recorded in") {
		t.Errorf("prompted or not recorded:\n%s", out)
	}
	if d, _ := e.install().Domains(); strings.Join(d, ",") != "iode" {
		t.Errorf("machine domains = %v", d)
	}
}

func TestSetupNoTTYMissingDomainChangesNothing(t *testing.T) {
	e := newEnv(t)
	p := filepath.Join(e.dir, "iode")
	newEnv(t).ok("init", "iode", p)
	rewriteProps(t, p, "vault.domain=iode\n", "")
	if err := e.install().Add("iode", p); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", e.dir)
	e.hooks = &setupHooks{tty: func(io.Reader) bool { return false }}
	_, errOut, err := e.run("setup", "--no-service", "--agent", "none")
	if err == nil || !strings.Contains(errOut, "--domain iode=<domain>") {
		t.Errorf("want missing flag listed: %v\n%s", err, errOut)
	}
	if strings.Contains(vaultProps(t, p), "vault.domain") {
		t.Error("vault marked before refusal")
	}
	if d, _ := e.install().Domains(); len(d) != 0 {
		t.Errorf("machine domains changed: %v", d)
	}
}

// With the para service installed and running, setup hooks Claude Code up
// over HTTP with a new agent key, and that key opens an MCP session at /mcp
// through the gate (loopback, <vault>.localhost Host, bearer, no Origin).
func TestSetupClaudePrefersHTTPWhenTheServiceRuns(t *testing.T) {
	e, claudeLog, _ := setupEnv(t, true)
	e.ok("init", "acme", filepath.Join(e.dir, "acme"))
	e.ok("setup", "--service", "--agent", "none")
	if !serviceInstalled() {
		t.Fatal("setup --service wrote no unit file")
	}
	port, _ := runningServer(t, e)
	out := e.ok("setup", "--service", "--agent", "claude")
	got, _ := os.ReadFile(claudeLog)
	line := strings.TrimSpace(string(got))
	url := "http://acme.localhost:" + port + "/mcp"
	if !strings.HasPrefix(line, "mcp add --transport http --scope user para-acme "+url+" --header Authorization: Bearer ") {
		t.Fatalf("claude was run with %q\n%s", line, out)
	}
	if strings.Contains(out, line[strings.LastIndex(line, " ")+1:]) {
		t.Error("setup printed the token")
	}
	tok := line[strings.LastIndex(line, " ")+1:]
	var ks []struct {
		Name   string   `json:"name"`
		Scopes []string `json:"scopes"`
	}
	e.json(&ks, "key", "list")
	found := false
	for _, k := range ks {
		found = found || (k.Name == claudeKeyName && slices.Contains(k.Scopes, "vault:write"))
	}
	if !found {
		t.Fatalf("no agent key %s: %+v", claudeKeyName, ks)
	}

	post := func(token, session, body string) *http.Response {
		t.Helper()
		req, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:"+port+"/mcp", strings.NewReader(body))
		req.Host = "acme.localhost:" + port
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if session != "" {
			req.Header.Set("Mcp-Session-Id", session)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { res.Body.Close() })
		return res
	}
	init := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`
	if res := post("", "", init); res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token: %d", res.StatusCode)
	}
	res := post(tok, "", init)
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK || !strings.Contains(string(body), `"listChanged":true`) || res.Header.Get("Mcp-Session-Id") == "" {
		t.Fatalf("initialize: %d %s", res.StatusCode, body)
	}
	// A session id from before a restart is unknown: the client initializes again.
	if res := post(tok, "mcp-session-00000000-0000-0000-0000-000000000000", `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`); res.StatusCode != http.StatusNotFound {
		t.Fatalf("old session: %d", res.StatusCode)
	}
	// Rerun: hooked already, so nothing runs.
	if err := os.WriteFile(filepath.Join(e.dir, ".claude.json"), []byte(`{"mcpServers":{"para-acme":{}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	e.ok("setup", "--service", "--agent", "claude")
	if again, _ := os.ReadFile(claudeLog); string(again) != string(got) {
		t.Errorf("rerun ran claude: %q", again)
	}
}

// fakeService makes the para service look installed and running for setup:
// a unit file and a server record of this process at addr.
func fakeService(t *testing.T, e *env, addr string) {
	t.Helper()
	e.ok("setup", "--service", "--agent", "none")
	if !serviceInstalled() {
		t.Fatal("setup --service wrote no unit file")
	}
	if err := server.EnsureVarDir(e.home); err != nil {
		t.Fatal(err)
	}
	if err := server.WriteRecord(e.home, server.Record{PID: os.Getpid(), Addr: addr, Vault: "acme", Started: time.Now()}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.RemoveRecord(e.home) })
}

// A failed claude mcp add revokes its key; a run again makes the next free
// name instead of looping on the revoked one. The key works from loopback only.
func TestSetupClaudeHTTPRecoversAfterAFailedAdd(t *testing.T) {
	e, claudeLog, _ := setupEnv(t, true)
	marker := filepath.Join(e.dir, "claude.failed")
	script := "#!/bin/sh\nif [ ! -e " + marker + " ]; then : > " + marker + "; exit 1; fi\necho \"$@\" >> " + claudeLog + "\n"
	if err := os.WriteFile(filepath.Join(e.dir, "bin", "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	e.ok("init", "acme", filepath.Join(e.dir, "acme"))
	fakeService(t, e, "127.0.0.1:7799")
	if _, _, err := e.run("setup", "--service", "--agent", "claude"); err == nil {
		t.Fatal("a failed claude mcp add passed")
	}
	out := e.ok("setup", "--service", "--agent", "claude")
	if !strings.Contains(out, "key claude-2") {
		t.Fatalf("rerun did not make claude-2:\n%s", out)
	}
	got, _ := os.ReadFile(claudeLog)
	if !strings.Contains(string(got), "--transport http") || !strings.Contains(string(got), "Bearer para_claude-2_") {
		t.Fatalf("claude was run with %q", got)
	}
	var ks []struct {
		Name    string   `json:"name"`
		AllowIP []string `json:"allow_ip"`
		Revoked *string  `json:"revoked"`
	}
	e.json(&ks, "key", "list")
	for _, k := range ks {
		switch k.Name {
		case "claude":
			if k.Revoked == nil {
				t.Error("the key of the failed run works")
			}
		case "claude-2":
			if k.Revoked != nil || !slices.Equal(k.AllowIP, loopbackKey) {
				t.Errorf("claude-2: %+v", k)
			}
		}
	}
}

// A service on a LAN address is not used for Claude Code: the token would
// cross the network. Setup uses stdio instead.
func TestSetupClaudeHTTPRefusesANonLoopbackService(t *testing.T) {
	e, claudeLog, _ := setupEnv(t, true)
	e.ok("init", "acme", filepath.Join(e.dir, "acme"))
	fakeService(t, e, "192.168.77.5:7799")
	out := e.ok("setup", "--service", "--agent", "claude")
	if !strings.Contains(out, "not a loopback address") {
		t.Fatalf("no refusal:\n%s", out)
	}
	got, _ := os.ReadFile(claudeLog)
	if strings.Contains(string(got), "--transport http") || !strings.Contains(string(got), "mcp add --scope user para-acme -- ") {
		t.Fatalf("claude was run with %q", got)
	}
}

func TestLoopbackBase(t *testing.T) {
	for base, want := range map[string]bool{
		"http://acme.localhost:7700": true, "http://127.0.0.1:7700": true, "http://[::1]:7700": true,
		"http://192.168.1.76:7700": false, "http://para.example:7700": false,
	} {
		if got := loopbackBase(base); got != want {
			t.Errorf("%s: %v", base, got)
		}
	}
}
