package cli

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/mcpserver"
	"github.com/iodesystems/para/internal/update"
)

// streamAXParityCases are the parity rows of stream AX: upgrades.
func streamAXParityCases() map[string]parityCase {
	return map[string]parityCase{
		"changes":              {`{}`, []string{"changes"}, true},
		"vault_upgrade_status": {`{}`, []string{"vault", "upgrade", "status"}, true},
		"vault_upgrade":        {`{"dry_run":true}`, []string{"vault", "upgrade", "--dry-run"}, true},
		"vault_check":          {`{}`, []string{"vault", "check"}, false},
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

// releaseServer serves a release of para at version: a tarball whose para is
// newBin (a shell script), and a manifest signed by a test key with para's
// trusted comment. It returns an Upgrader for it with dir as the install.
func releaseServer(t *testing.T, product, version, trusted, newBin, dir string) update.Upgrader {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key := update.PublicKey{ID: [8]byte{1, 2, 3, 4, 5, 6, 7, 8}, Key: pub}
	var tb bytes.Buffer
	gz := gzip.NewWriter(&tb)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: "para", Mode: 0o755, Size: int64(len(newBin)), Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte(newBin))
	tw.Close()
	gz.Close()
	files := map[string][]byte{"/dl/para_linux_amd64.tar.gz": tb.Bytes()}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(b)
	}))
	t.Cleanup(srv.Close)
	sum := sha256.Sum256(tb.Bytes())
	body, _ := json.Marshal(update.Manifest{Product: product, Version: version, Published: "2026-11-10T00:00:00Z", Summary: "a summary",
		Platforms: map[string]update.File{"linux_amd64": {URL: srv.URL + "/dl/para_linux_amd64.tar.gz", SHA256: hex.EncodeToString(sum[:]), Size: int64(tb.Len())}}})
	files["/latest/para-manifest.json"] = body
	files["/latest/para-manifest.json.minisig"] = []byte(update.Sign(priv, key.ID, body, trusted))
	return update.Upgrader{
		ManifestURL: srv.URL + "/latest/para-manifest.json", Keys: []update.PublicKey{key},
		Exe: filepath.Join(dir, "bin", "para"), StatePath: update.StateFile(dir), Platform: "linux_amd64",
		Fetch: &update.Fetcher{AllowHTTP: true},
	}
}

// fakeReleaseBin is the new para of a release: it logs its arguments, says
// an automated step is pending, and has a running service.
func fakeReleaseBin(log string) string {
	return "#!/bin/sh\necho \"$*\" >> '" + log + "'\n" +
		"case \"$*\" in *\"upgrade status --json\"*) echo '{\"format_version\":6,\"latest\":7,\"pending\":[{\"id\":\"obligations\",\"auto\":true}]}';; esac\n"
}

// Without a checkout (or with --release) para upgrade installs the signed
// release over the binary, restarts the service, shows the changes, dry-runs
// each vault's automated steps and runs them only when allowed.
func TestUpgradeFromARelease(t *testing.T) {
	for name, c := range map[string]struct {
		src      func(t *testing.T) string
		release  bool
		yes      bool
		ask      func(string) bool
		applied  bool
		noSteps  bool
		wantText string
	}{
		"no checkout, no terminal":           {src: func(*testing.T) string { return "" }, wantText: "no terminal to ask on"},
		"not a checkout, --yes":              {src: func(t *testing.T) string { return t.TempDir() }, yes: true, applied: true},
		"checkout with --release, asked yes": {src: upgradeRepos, release: true, ask: func(string) bool { return true }, applied: true},
		"asked no":                           {src: func(*testing.T) string { return "" }, ask: func(string) bool { return false }, wantText: "not run; later"},
		"--no-vault-steps":                   {src: func(*testing.T) string { return "" }, yes: true, noSteps: true},
	} {
		t.Run(name, func(t *testing.T) {
			src := c.src(t)
			dir := t.TempDir()
			log := filepath.Join(dir, "new.log")
			tools := t.TempDir()
			mk, mkLog := fakeProgram(t, tools, "make", "x")
			var out bytes.Buffer
			err := runUpgrade(context.Background(), &out, upgradeOptions{
				Src: src, Make: mk, Version: "v0.3.0", OldRelease: "0.3.0", Home: dir, VaultIDs: []string{"acme"},
				Skipped: []string{"corp: not held here"}, Release: c.release, Yes: c.yes, Ask: c.ask, NoVaultSteps: c.noSteps,
				Updater: releaseServer(t, "para", "v0.4.0", "para v0.4.0 manifest", fakeReleaseBin(log), dir),
			})
			if err != nil {
				t.Fatalf("%v\n%s", err, out.String())
			}
			if _, err := os.Stat(mkLog); err == nil {
				t.Fatal("make ran for a release upgrade")
			}
			b, _ := os.ReadFile(log)
			calls := string(b)
			for _, want := range []string{"service status", "service restart", "changes --since 0.3.0", "--vault acme vault upgrade --file"} {
				if !strings.Contains(calls, want) {
					t.Errorf("new binary calls lack %q:\n%s", want, calls)
				}
			}
			if got := strings.Contains(calls, "vault upgrade --dry-run"); got == c.noSteps {
				t.Errorf("dry run ran = %v, want %v:\n%s", got, !c.noSteps, calls)
			}
			if got := strings.Contains(calls, "--vault acme vault upgrade\n"); got != c.applied {
				t.Errorf("steps applied = %v, want %v:\n%s", got, c.applied, calls)
			}
			if strings.Contains(calls, "corp") {
				t.Error("a vault this machine does not hold was touched")
			}
			for _, want := range []string{"installed para v0.4.0", "upgraded para: v0.3.0 -> v0.4.0", "skipped corp", c.wantText} {
				if !strings.Contains(out.String(), want) {
					t.Errorf("output lacks %q:\n%s", want, out.String())
				}
			}
		})
	}
}

// A release that is not newer is not installed; a dun manifest signed with
// the same key is refused and leaves no binary.
func TestUpgradeReleaseRefusals(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "new.log")
	var out bytes.Buffer
	u := releaseServer(t, "para", "v0.3.0", "para v0.3.0 manifest", fakeReleaseBin(log), dir)
	if err := runUpgrade(context.Background(), &out, upgradeOptions{Version: "v0.4.0", Home: dir, Updater: u}); err == nil || !strings.Contains(err.Error(), "downgrade") {
		t.Fatalf("downgrade: %v", err)
	}
	if err := runUpgrade(context.Background(), &out, upgradeOptions{Version: "v0.3.0", Home: dir, Updater: u}); err != nil || !strings.Contains(out.String(), "para is current") {
		t.Fatalf("current: %v\n%s", err, out.String())
	}
	// A checkout build (git describe) counts as its newest notes' release.
	if err := runUpgrade(context.Background(), &out, upgradeOptions{Version: "14ecdf7", OldRelease: "0.3.0", Home: dir, Updater: u}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(u.Exe); err == nil {
		t.Fatal("a release not newer was installed")
	}
	dir2 := t.TempDir()
	d := releaseServer(t, "dun", "v0.9.0", "dun v0.9.0 manifest", fakeReleaseBin(log), dir2)
	if err := runUpgrade(context.Background(), &out, upgradeOptions{Version: "v0.3.0", Home: dir2, Updater: d}); err == nil || !update.IsVerify(errors.Unwrap(err)) {
		t.Fatalf("dun manifest: %v", err)
	}
	if _, err := os.Stat(d.Exe); err == nil {
		t.Fatal("a dun manifest installed a binary")
	}
	if _, err := os.Stat(d.StatePath); err == nil {
		t.Fatal("a dun manifest left state")
	}
}

// The embedded key is the one para upgrade uses by default.
func TestUpgradeEmbeddedKey(t *testing.T) {
	keys, err := upgradeKeys()
	if err != nil || len(keys) != 1 || keys[0].IDString() != "2CC27A171DE03D42" {
		t.Fatalf("%v %v", keys, err)
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

func TestMCPSurfaceIsTheServersSurface(t *testing.T) {
	e := newEnv(t)
	var got mcpserver.Surface
	if err := json.Unmarshal([]byte(e.ok("mcp", "--surface")), &got); err != nil {
		t.Fatal(err)
	}
	want, err := mcpserver.CurrentSurface(got.Version)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("para mcp --surface differs from the server's surface")
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
