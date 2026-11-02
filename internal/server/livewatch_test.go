package server

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/store/local"
	"github.com/iodesystems/parasolve/internal/vault"
)

type pushes struct {
	mu  sync.Mutex
	got []LiveEvent
}

func subscribe(t *testing.T, l *Live, vaultID string) *pushes {
	t.Helper()
	p := &pushes{}
	cancel, err := l.bus.Subscribe(liveChannel(vaultID), func(b []byte) {
		var e LiveEvent
		if err := json.Unmarshal(b, &e); err != nil {
			t.Error(err)
			return
		}
		p.mu.Lock()
		p.got = append(p.got, e)
		p.mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cancel)
	return p
}

func (p *pushes) docs() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for _, e := range p.got {
		out = append(out, e.Doc)
	}
	return out
}

func (p *pushes) wait(t *testing.T, doc string, start time.Time, within time.Duration) time.Duration {
	t.Helper()
	for time.Since(start) < within {
		for _, d := range p.docs() {
			if d == doc {
				return time.Since(start)
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no push for %q within %v; got %v", doc, within, p.docs())
	return 0
}

func inboxDoc(id string) model.Document {
	return model.Document{ID: id, Folder: model.FolderInbox, Title: id,
		Meta: &model.Inbox{Common: model.Common{Type: model.TypeInbox}}}
}

// A second Store on the same vault stands for `para mcp` or the CLI: its writes
// reach live clients only through the watcher.
func TestLivePushesWritesFromAnotherProcess(t *testing.T) {
	f := newFixture(t)
	p := subscribe(t, f.srv.live, "acme")
	other, err := local.Open(vaultRoot(t, f, "acme"), vault.Options{ID: "acme", Actor: "agent", Surface: "mcp",
		Install: "other-install", StateDir: filepath.Join(f.home, "vaults", "acme")})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := other.Create(t.Context(), inboxDoc("inbox-from-mcp")); err != nil {
		t.Fatal(err)
	}
	d := p.wait(t, "inbox-from-mcp", start, 2*time.Second)
	t.Logf("cross-process push latency: %v", d)
}

func TestLiveInProcessWritePushesOnce(t *testing.T) {
	f := newFixture(t)
	p := subscribe(t, f.srv.live, "acme")
	v, _ := f.hub.Lookup("acme")
	start := time.Now()
	if _, err := v.(*Tenant).store.Create(t.Context(), inboxDoc("inbox-in-proc")); err != nil {
		t.Fatal(err)
	}
	d := p.wait(t, "inbox-in-proc", start, 2*time.Second)
	t.Logf("in-process push latency: %v", d)
	time.Sleep(2500 * time.Millisecond) // past one poll tick
	n := 0
	for _, doc := range p.docs() {
		if doc == "inbox-in-proc" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("pushed %d times, want 1", n)
	}
}

func TestLiveBulkCollapses(t *testing.T) {
	f := newFixture(t)
	p := subscribe(t, f.srv.live, "acme")
	batch := make([]model.Event, 100)
	for i := range batch {
		batch[i] = model.Event{Op: "create", Doc: "d" + string(rune('a'+i%26)) + strings.Repeat("x", i)}
	}
	f.srv.live.Publish("acme", batch)
	if got := p.docs(); len(got) != 1 {
		t.Fatalf("got %d messages, want 1 bulk", len(got))
	}
}

func TestHubCloseStopsWatchers(t *testing.T) {
	f := newFixture(t)
	if !watchGoroutines() {
		t.Fatal("no watcher running before Close")
	}
	f.hub.Close()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !watchGoroutines() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("watcher goroutines remain after Hub.Close")
}

func watchGoroutines() bool {
	buf := make([]byte, 1<<20)
	buf = buf[:runtime.Stack(buf, true)]
	return strings.Contains(string(buf), "events.Watch") || strings.Contains(string(buf), "(*watcher).run")
}

func vaultRoot(t *testing.T, f *fixture, id string) string {
	t.Helper()
	vs, err := f.hub.install.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range vs {
		if v.ID == id {
			return v.Path
		}
	}
	t.Fatal("vault not registered")
	return ""
}
