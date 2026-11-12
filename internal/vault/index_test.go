package vault

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
)

func shortIndexAge(t *testing.T) {
	t.Helper()
	old := idxMaxAge
	idxMaxAge = time.Hour
	t.Cleanup(func() { idxMaxAge = old })
}

func handFormer(title, former string) string {
	return "---\ntype: inbox\ncreated: 2026-11-01\nformer_ids: [" + former + "]\n---\n\n# " + title + "\n\nText.\n"
}

func mustMissing(t *testing.T, v *Vault, id string) {
	t.Helper()
	if _, err := v.Get(ctx, id); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("Get %s: %v, want not found", id, err)
	}
}

// A former id resolves, also after a ChangeIDs in this process.
func TestIndexFormerAfterRename(t *testing.T) {
	v, _ := newVault(t)
	d, err := v.Create(ctx, inbox("One", "x"))
	if err != nil {
		t.Fatal(err)
	}
	mustMissing(t, v, "gone-id") // builds the index; the miss is cached
	if _, err := v.ChangeIDs(ctx, map[string]string{d.ID: "inbox-renamed"}); err != nil {
		t.Fatal(err)
	}
	g, err := v.Get(ctx, d.ID)
	if err != nil || g.ID != "inbox-renamed" {
		t.Fatalf("old id: %s %v", g.ID, err)
	}
	if _, err := v.Get(ctx, "inbox-renamed"); err != nil {
		t.Fatal(err)
	}
	mustMissing(t, v, "gone-id")
}

// A cached miss ends when a document with that id or former id is written
// here.
func TestIndexMissEndsOnInternalWrite(t *testing.T) {
	shortIndexAge(t)
	v, _ := newVault(t)
	mustMissing(t, v, "inbox-late")
	mustMissing(t, v, "inbox-late")
	d := inbox("Late", "x")
	d.ID = "inbox-late"
	if _, err := v.Create(ctx, d); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Get(ctx, "inbox-late"); err != nil {
		t.Fatal(err)
	}
	mustMissing(t, v, "inbox-old")
	d2, err := v.Create(ctx, inbox("Two", "y"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.ChangeIDs(ctx, map[string]string{d2.ID: "inbox-new"}); err != nil {
		t.Fatal(err)
	}
	if g, err := v.Get(ctx, d2.ID); err != nil || g.ID != "inbox-new" {
		t.Fatalf("former after write: %s %v", g.ID, err)
	}
}

// A cached miss ends when a hand-written file appears (the directory stamp
// changes), and a former id added to a file in place ends it once a scan reads
// the file (no waiting for the index age).
func TestIndexMissEndsOnExternalWrite(t *testing.T) {
	shortIndexAge(t)
	old := racyWindow
	racyWindow = 0
	oldDir := dirRacy
	dirRacy = -time.Hour
	t.Cleanup(func() { racyWindow, dirRacy = old, oldDir })
	v, root := newVault(t)
	mustMissing(t, v, "hand-new")
	mustMissing(t, v, "formerly")
	p := filepath.Join(root, "inbox", "hand-new.md")
	if err := os.WriteFile(p, []byte(handFormer("Hand", "formerly")), 0o600); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Minute)
	if err := os.Chtimes(p, past, past); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Get(ctx, "hand-new"); err != nil {
		t.Fatalf("after scan: %v", err)
	}
	if g, err := v.Get(ctx, "formerly"); err != nil || g.ID != "hand-new" {
		t.Fatalf("former of hand file: %s %v", g.ID, err)
	}

	// Edit adds a former id to an existing file.
	if err := os.WriteFile(p, []byte(handFormer("Hand", "formerly, later")), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, past.Add(time.Second), past.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	mustMissing(t, v, "other")
	mustMissing(t, v, "later") // in-place edit: no directory changed, the miss stands
	if _, err := v.List(ctx, "", ""); err != nil {
		t.Fatal(err)
	}
	if g, err := v.Get(ctx, "later"); err != nil || g.ID != "hand-new" {
		t.Fatalf("added former id: %s %v", g.ID, err)
	}
}

// With the index old, a hand-written file shows without any scan.
func TestIndexAgeBoundsMiss(t *testing.T) {
	old := idxMaxAge
	idxMaxAge = 50 * time.Millisecond
	t.Cleanup(func() { idxMaxAge = old })
	v, root := newVault(t)
	mustMissing(t, v, "hand-aged")
	if err := os.WriteFile(filepath.Join(root, "inbox", "hand-aged.md"), []byte(handDoc("Aged")), 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if _, err := v.Get(ctx, "hand-aged"); err != nil {
		t.Fatal(err)
	}
}
