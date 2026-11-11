package opstats

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func find(es []Entry, op, surface, actor string) (Entry, bool) {
	for _, e := range es {
		if e.Op == op && e.Surface == surface && e.Actor == actor {
			return e, true
		}
	}
	return Entry{}, false
}

// Counts are per op, surface, and actor; nothing reaches the file before a
// flush; a second process's counts are added, not replaced.
func TestCountsFlushAndMerge(t *testing.T) {
	dir := t.TempDir()
	Record(dir, "show", CLI, Owner)
	Record(dir, "show", CLI, Owner)
	Record(dir, "show", MCPStdio, "claude")
	Record(dir, "list", Web, Owner)
	if _, err := os.Stat(filepath.Join(dir, File)); !os.IsNotExist(err) {
		t.Fatalf("file written before a flush: %v", err)
	}
	if err := Flush(dir); err != nil {
		t.Fatal(err)
	}
	// Another process wrote while this one ran.
	Record(dir, "show", CLI, Owner)
	b := []byte(`{"entries":[{"op":"show","surface":"cli","actor":"owner","count":10,"last":"2020-01-01T00:00:00Z"},{"op":"old","surface":"cli","actor":"owner","count":1,"last":"2020-01-01T00:00:00Z"}]}`)
	if err := os.WriteFile(filepath.Join(dir, File), b, 0o600); err != nil {
		t.Fatal(err)
	}
	es, err := Read(dir)
	if err != nil {
		t.Fatal(err)
	}
	if e, ok := find(es, "show", CLI, Owner); !ok || e.Count != 11 {
		t.Errorf("show cli owner = %+v, want 11", e)
	}
	if e, ok := find(es, "old", CLI, Owner); !ok || e.Count != 1 {
		t.Errorf("old = %+v", e)
	}
	if e, _ := find(es, "show", CLI, Owner); time.Since(e.Last) > time.Minute {
		t.Errorf("last time not updated: %v", e.Last)
	}
	// Nothing left in memory: a second flush changes nothing.
	if err := Flush(dir); err != nil {
		t.Fatal(err)
	}
	es2, _ := Read(dir)
	if len(es2) != len(es) {
		t.Errorf("entries changed on a second flush")
	}
}

// An empty state dir counts nothing and does not fail.
func TestNoStateDir(t *testing.T) {
	Record("", "show", CLI, Owner)
	if es, err := Read(""); err != nil || es != nil {
		t.Errorf("Read(\"\") = %v, %v", es, err)
	}
}
