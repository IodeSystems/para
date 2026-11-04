package events

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
)

type sink struct {
	mu   sync.Mutex
	docs []string
}

func (s *sink) add(b []model.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range b {
		s.docs = append(s.docs, e.Doc)
	}
}

func (s *sink) get() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.docs...)
}

func (s *sink) waitLen(t *testing.T, n int) []string {
	t.Helper()
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if g := s.get(); len(g) >= n {
			return g
		}
	}
	t.Fatalf("got %v, want %d records", s.get(), n)
	return nil
}

func appender(dir, install string) *Log {
	return NewLog(dir, Source{Vault: "v", Install: install, Actor: "a", Surface: "cli"})
}

func TestWatchStartsAtEndAndSeesAppends(t *testing.T) {
	dir := t.TempDir()
	a := appender(dir, "ia")
	if err := a.Append(OpCreate, "old-1", "", "inbox"); err != nil {
		t.Fatal(err)
	}
	var s sink
	stop := Watch(t.Context(), dir, 0, s.add, nil)
	defer stop()
	if err := a.Append(OpCreate, "new-1", "", "inbox"); err != nil {
		t.Fatal(err)
	}
	s.waitLen(t, 1)
	time.Sleep(300 * time.Millisecond)
	if got := s.get(); len(got) != 1 || got[0] != "new-1" {
		t.Fatalf("got %v, want only [new-1]: history must not replay", got)
	}
}

func TestWatchHoldsPartialLine(t *testing.T) {
	dir := t.TempDir()
	var s sink
	stop := Watch(t.Context(), dir, 0, s.add, nil)
	defer stop()
	name := filepath.Join(dir, "2026-11.ib.jsonl")
	line := `{"ts":"2026-11-01T00:00:00Z","install":"ib","op":"create","doc":"half"}` + "\n"
	f, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(line[:30]); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if g := s.get(); len(g) != 0 {
		t.Fatalf("partial line published: %v", g)
	}
	if _, err := f.WriteString(line[30:]); err != nil {
		t.Fatal(err)
	}
	if g := s.waitLen(t, 1); g[0] != "half" {
		t.Fatalf("got %v", g)
	}
}

func TestWatchNewInstallFileAndConflictCopy(t *testing.T) {
	dir := t.TempDir()
	if err := appender(dir, "ia").Append(OpCreate, "x", "", "inbox"); err != nil {
		t.Fatal(err)
	}
	var s sink
	stop := Watch(t.Context(), dir, 0, s.add, nil)
	defer stop()
	conflict := filepath.Join(dir, "2026-11.ia (conflict copy).jsonl")
	if err := os.WriteFile(conflict, []byte(`{"op":"create","doc":"conflict"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := appender(dir, "ibnew").Append(OpCreate, "from-new-install", "", "inbox"); err != nil {
		t.Fatal(err)
	}
	s.waitLen(t, 1)
	time.Sleep(300 * time.Millisecond)
	if g := s.get(); len(g) != 1 || g[0] != "from-new-install" {
		t.Fatalf("got %v, want [from-new-install]", g)
	}
}

func TestWatchPollsWithoutFileEvents(t *testing.T) {
	// The directory does not exist at start, so fsnotify cannot watch it: the poll finds it.
	dir := filepath.Join(t.TempDir(), "events")
	var s sink
	stop := Watch(t.Context(), dir, 50*time.Millisecond, s.add, nil)
	defer stop()
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := appender(dir, "ia").Append(OpCreate, "polled", "", "inbox"); err != nil {
		t.Fatal(err)
	}
	if g := s.waitLen(t, 1); g[0] != "polled" {
		t.Fatalf("got %v", g)
	}
}

func TestWatchBurstIsFewBatches(t *testing.T) {
	dir := t.TempDir()
	var mu sync.Mutex
	batches, total := 0, 0
	stop := Watch(t.Context(), dir, 0, func(b []model.Event) {
		mu.Lock()
		batches++
		total += len(b)
		mu.Unlock()
	}, nil)
	defer stop()
	a := appender(dir, "ia")
	for i := 0; i < 200; i++ {
		if err := a.Append(OpUpdate, "d", "", ""); err != nil {
			t.Fatal(err)
		}
	}
	for end := time.Now().Add(3 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		mu.Lock()
		n := total
		mu.Unlock()
		if n == 200 {
			break
		}
	}
	mu.Lock()
	defer mu.Unlock()
	t.Logf("200 records in %d batches", batches)
	if total != 200 || batches > 60 {
		t.Fatalf("total %d in %d batches", total, batches)
	}
}
