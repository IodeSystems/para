package cli

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// streamAXParityCases are the parity rows of stream AX: upgrades.
func streamAXParityCases() map[string]parityCase {
	return map[string]parityCase{
		"changes":              {`{}`, []string{"changes"}, true},
		"vault_upgrade_status": {`{}`, []string{"vault", "upgrade", "--status"}, true},
		"vault_upgrade":        {`{"dry_run":true}`, []string{"vault", "upgrade", "--dry-run"}, true},
	}
}

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", append([]string{"-c", "user.name=Test", "-c", "user.email=test@example.com", "-c", "commit.gpgsign=false", "-c", "init.defaultBranch=main"}, args...)...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// upgradeRepos makes an origin, a checkout cloned from it, and a second clone
// that pushes a new commit to the origin. The checkout is one commit behind.
func upgradeRepos(t *testing.T) (checkout string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir) // no real git config, no real ~/.local/bin
	origin := filepath.Join(dir, "origin.git")
	gitIn(t, dir, "init", "-q", "--bare", origin)
	seed := filepath.Join(dir, "seed")
	gitIn(t, dir, "clone", "-q", origin, seed)
	if err := os.WriteFile(filepath.Join(seed, "a.txt"), []byte("one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, seed, "add", "a.txt")
	gitIn(t, seed, "commit", "-q", "-m", "first")
	gitIn(t, seed, "push", "-q", "origin", "HEAD:main")
	checkout = filepath.Join(dir, "checkout")
	gitIn(t, dir, "clone", "-q", "-b", "main", origin, checkout)
	if err := os.WriteFile(filepath.Join(seed, "a.txt"), []byte("two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, seed, "commit", "-q", "-am", "second")
	gitIn(t, seed, "push", "-q", "origin", "HEAD:main")
	return checkout
}

// fakeProgram writes an executable shell script that logs its arguments and
// prints out.
func fakeProgram(t *testing.T, dir, name, out string) (path, log string) {
	t.Helper()
	path, log = filepath.Join(dir, name), filepath.Join(dir, name+".log")
	script := "#!/bin/sh\necho \"$PWD: $*\" >> '" + log + "'\ncat <<'EOF'\n" + out + "\nEOF\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path, log
}

func TestUpgradeRefusesADirtyTree(t *testing.T) {
	src := upgradeRepos(t)
	tools := t.TempDir()
	mk, mkLog := fakeProgram(t, tools, "make", "installed")
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("local edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	head := gitIn(t, src, "rev-parse", "HEAD")
	var out bytes.Buffer
	err := runUpgrade(context.Background(), &out, upgradeOptions{Src: src, Make: mk, Version: "old"})
	if err == nil || !strings.Contains(err.Error(), "uncommitted") {
		t.Fatalf("err = %v, want a refusal for uncommitted changes", err)
	}
	if _, serr := os.Stat(mkLog); serr == nil {
		t.Fatal("make ran on a dirty tree")
	}
	if gitIn(t, src, "rev-parse", "HEAD") != head {
		t.Fatal("the dirty tree was pulled")
	}
}

func TestUpgradePullsInstallsAndShowsTheChanges(t *testing.T) {
	src := upgradeRepos(t)
	tools := t.TempDir()
	mk, mkLog := fakeProgram(t, tools, "make", "installed")
	bin, binLog := fakeProgram(t, tools, "para-new", "0.9.0\n  People\n    - something new")
	before := gitIn(t, src, "rev-parse", "HEAD")
	var out bytes.Buffer
	err := runUpgrade(context.Background(), &out, upgradeOptions{
		Src: src, Make: mk, NewBin: bin, Version: "old", OldRelease: "0.1.0", Home: "/nonexistent/home", VaultIDs: []string{"acme"},
	})
	if err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	if gitIn(t, src, "rev-parse", "HEAD") == before {
		t.Fatal("the checkout was not pulled")
	}
	if b, _ := os.ReadFile(mkLog); !strings.Contains(string(b), src+": install") {
		t.Fatalf("make log = %q, want make install in the checkout", b)
	}
	for _, want := range []string{"upgraded para: old -> ", "changes since 0.1.0", "something new", "vault acme"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, out.String())
		}
	}
	b, _ := os.ReadFile(binLog)
	for _, want := range []string{"changes --since 0.1.0", "--vault acme vault upgrade --file"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("new binary log lacks %q:\n%s", want, b)
		}
	}
	// Nothing was committed or pushed: HEAD is the fetched commit, and the tree is clean.
	if gitIn(t, src, "status", "--porcelain") != "" {
		t.Fatal("the checkout is dirty after the upgrade")
	}
}

func TestUpgradeWithoutACheckoutPrintsTheGoInstallLine(t *testing.T) {
	for _, src := range []string{"", t.TempDir()} {
		tools := t.TempDir()
		mk, mkLog := fakeProgram(t, tools, "make", "x")
		var out bytes.Buffer
		if err := runUpgrade(context.Background(), &out, upgradeOptions{Src: src, Make: mk}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), goInstallLine) {
			t.Errorf("src %q: output = %q", src, out.String())
		}
		if _, err := os.Stat(mkLog); err == nil {
			t.Error("make ran without a checkout")
		}
	}
}

func TestUpgradeStopsWhenAlreadyCurrent(t *testing.T) {
	src := upgradeRepos(t)
	gitIn(t, src, "pull", "-q", "--ff-only")
	tools := t.TempDir()
	mk, mkLog := fakeProgram(t, tools, "make", "x")
	var out bytes.Buffer
	cur := gitIn(t, src, "describe", "--always", "--dirty", "--tags")
	if err := runUpgrade(context.Background(), &out, upgradeOptions{Src: src, Make: mk, Version: cur}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "para is current") {
		t.Fatalf("output = %q", out.String())
	}
	if _, err := os.Stat(mkLog); err == nil {
		t.Fatal("make ran for a current version")
	}
}

func TestStaleServerNoteNeedsTwoStampedVersions(t *testing.T) {
	if staleServerNote("abc", "abc") != "" || staleServerNote("dev", "abc") != "" || staleServerNote("", "abc") != "" {
		t.Error("a note without a difference it can tell")
	}
	if n := staleServerNote("abc", "def"); !strings.Contains(n, "abc") || !strings.Contains(n, "def") || !strings.Contains(n, "service restart") {
		t.Errorf("note = %q", n)
	}
}

func TestStaleMCPPidsReadsAFakeProc(t *testing.T) {
	proc := t.TempDir()
	mk := func(pid, exe, cmdline string) {
		d := filepath.Join(proc, pid)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(exe, filepath.Join(d, "exe")); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "cmdline"), []byte(cmdline), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("101", "/home/x/.local/bin/para (deleted)", "para\x00--vault\x00acme\x00mcp\x00")
	mk("102", "/home/x/.local/bin/para", "para\x00mcp\x00")
	mk("103", "/home/x/.local/bin/para (deleted)", "para\x00serve\x00")
	mk("104", "/usr/bin/other (deleted)", "other\x00mcp\x00")
	if got := staleMCPPids(proc); len(got) != 1 || got[0] != 101 {
		t.Fatalf("stale pids = %v, want [101]", got)
	}
}
