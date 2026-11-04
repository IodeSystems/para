package server

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
)

func externalEdits(t *testing.T, f *fixture, doc string) int {
	t.Helper()
	v, _ := f.hub.Lookup("acme")
	evs, err := v.(*Tenant).store.Events(context.Background(), 1000)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range evs {
		if e.Op == "external_edit" && strings.Contains(e.Doc, doc) {
			n++
		}
	}
	return n
}

func setGrace(t *testing.T, root, val string) {
	t.Helper()
	p := filepath.Join(root, ".para", "vault.properties")
	raw, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, append(raw, []byte("\nreconcile.grace="+val+"\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
}

func waitFor(t *testing.T, within time.Duration, what string, ok func() bool) time.Duration {
	t.Helper()
	start := time.Now()
	for time.Since(start) < within {
		if ok() {
			return time.Since(start)
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("not within %v: %s", within, what)
	return 0
}

// A hand edit made while the server runs is logged and pushed with no command.
func TestServeReconcilesHandEdits(t *testing.T) {
	f := newFixture(t)
	root := vaultRoot(t, f, "acme")
	setGrace(t, root, "0s")
	p := subscribe(t, f.srv.live, "acme")
	time.Sleep(300 * time.Millisecond) // the first reconcile runs at start

	file := filepath.Join(root, "io", "inbox-by-hand.md")
	body := "---\ntype: inbox\n---\n# By hand\n"
	start := time.Now()
	if err := os.WriteFile(file, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	d := waitFor(t, 3*time.Second, "external_edit logged", func() bool { return externalEdits(t, f, "inbox-by-hand") == 1 })
	t.Logf("hand edit: external_edit logged %v after the write", d)
	p.wait(t, "inbox-by-hand", start, 3*time.Second)
	t.Logf("hand edit: pushed %v after the write", time.Since(start))

	// No loop: the reconcile's own event does not run another that logs more.
	time.Sleep(1 * time.Second)
	if n := externalEdits(t, f, "inbox-by-hand"); n != 1 {
		t.Fatalf("external_edit logged %d times", n)
	}
	// A change through the vault is not an external edit.
	v, _ := f.hub.Lookup("acme")
	if _, err := v.(*Tenant).store.Create(context.Background(), inboxDoc("inbox-by-op")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(700 * time.Millisecond)
	if n := externalEdits(t, f, "inbox-by-op"); n != 0 {
		t.Fatalf("a vault write logged %d external_edit", n)
	}
}

// reconcile.grace holds a change that no event explains.
func TestServeReconcileRespectsGrace(t *testing.T) {
	f := newFixture(t)
	root := vaultRoot(t, f, "acme")
	setGrace(t, root, "1500ms")
	time.Sleep(300 * time.Millisecond)
	start := time.Now()
	if err := os.WriteFile(filepath.Join(root, "io", "inbox-held.md"), []byte("---\ntype: inbox\n---\n# Held\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(900 * time.Millisecond)
	if n := externalEdits(t, f, "inbox-held"); n != 0 {
		t.Fatalf("logged inside the grace period")
	}
	waitFor(t, 6*time.Second, "external_edit after the grace", func() bool { return externalEdits(t, f, "inbox-held") == 1 })
	if since := time.Since(start); since < 1500*time.Millisecond {
		t.Errorf("logged after %v, inside the 1.5s grace", since)
	}
	t.Logf("external_edit logged %v after the write, grace 1.5s", time.Since(start))
}

// Temp and conflict files and other folders schedule no reconcile.
func TestDocWatcherIgnoresTempAndConflictFiles(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "inbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "events"), 0o755); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	w := &docWatcher{
		root: root, dirs: []string{"inbox"},
		reconcile: func(context.Context) (model.ReconcileResult, error) {
			calls.Add(1)
			return model.ReconcileResult{}, nil
		},
		grace:    func() time.Duration { return time.Minute },
		publish:  func([]model.Change) {},
		warn:     func(m string) { t.Log(m) },
		debounce: 40 * time.Millisecond, maxWait: time.Second, fallback: time.Hour,
	}
	stop := w.start(context.Background())
	defer stop()
	time.Sleep(200 * time.Millisecond)
	base := calls.Load() // the run at start
	for _, name := range []string{
		"inbox/.inbox-a.md.tmp-123", "inbox/a.md~", "inbox/.a.md.swp", "inbox/#a.md#",
		"inbox/a.sync-conflict-20261101-101500-ABC.md", "inbox/a (conflicted copy).md", "inbox/a.txt", "events/2026-11.jsonl",
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(400 * time.Millisecond)
	if n := calls.Load(); n != base {
		t.Fatalf("%d reconciles for ignored files", n-base)
	}
	if err := os.WriteFile(filepath.Join(root, "inbox", "real.md"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitFor(t, 2*time.Second, "reconcile for a document", func() bool { return calls.Load() > base })
	// A burst is one run.
	base = calls.Load()
	for i := range 20 {
		_ = os.WriteFile(filepath.Join(root, "inbox", "burst.md"), []byte{byte('a' + i)}, 0o600)
	}
	time.Sleep(400 * time.Millisecond)
	if n := calls.Load() - base; n != 1 {
		t.Fatalf("a burst ran %d reconciles, want 1", n)
	}
}

func TestDocWatcherStopLeavesNoGoroutine(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "inbox"), 0o755); err != nil {
		t.Fatal(err)
	}
	before := runtime.NumGoroutine()
	w := &docWatcher{root: root, dirs: []string{"inbox"},
		reconcile: func(context.Context) (model.ReconcileResult, error) { return model.ReconcileResult{}, nil },
		grace:     func() time.Duration { return time.Minute }, publish: func([]model.Change) {}, warn: func(string) {},
		debounce: 20 * time.Millisecond, maxWait: time.Second, fallback: time.Hour}
	stop := w.start(context.Background())
	_ = os.WriteFile(filepath.Join(root, "inbox", "a.md"), []byte("x"), 0o600)
	time.Sleep(100 * time.Millisecond)
	stop()
	stop()
	for i := 0; i < 100 && runtime.NumGoroutine() > before; i++ {
		time.Sleep(20 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > before {
		t.Errorf("goroutines: %d before, %d after stop", before, n)
	}
}
