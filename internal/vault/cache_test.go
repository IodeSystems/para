package vault

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
)

// settleAtOnce turns the racy guard off, so an entry is trusted as soon as it
// is read: the tests below then see the cache, not a re-read.
func settleAtOnce(t *testing.T) {
	t.Helper()
	old := racyWindow
	racyWindow = -time.Hour
	t.Cleanup(func() { racyWindow = old })
}

func handDoc(title string) string {
	return "---\ntype: inbox\ncreated: 2026-11-01\ntags: [a, b]\nkept:\n  - \"[[inbox-other]]\"\n---\n\n# " + title + "\n\nText.\n"
}

func cached(v *Vault, rel string) *entry {
	v.paths.mu.Lock()
	defer v.paths.mu.Unlock()
	return v.paths.refs[rel]
}

func mustTitle(t *testing.T, v *Vault, id, want string) {
	t.Helper()
	d, err := v.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if d.Title != want {
		t.Fatalf("title = %q, want %q", d.Title, want)
	}
}

func TestCacheSeesOutsideRewrites(t *testing.T) {
	settleAtOnce(t)
	v, root := newVault(t)
	p := filepath.Join(root, "inbox", "hand.md")
	if err := os.WriteFile(p, []byte(handDoc("One")), 0o600); err != nil {
		t.Fatal(err)
	}
	mustTitle(t, v, "hand", "One")
	e := cached(v, "inbox/hand.md")
	mustTitle(t, v, "hand", "One")
	if cached(v, "inbox/hand.md") != e {
		t.Fatal("second Get read the file again")
	}

	// In place, another size.
	if err := os.WriteFile(p, []byte(handDoc("Longer")), 0o600); err != nil {
		t.Fatal(err)
	}
	mustTitle(t, v, "hand", "Longer")

	// In place, same size, modification time set back to the cached one:
	// the change time still differs.
	st, _ := os.Stat(p)
	if err := os.WriteFile(p, []byte(handDoc("Second")), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, st.ModTime(), st.ModTime()); err != nil {
		t.Fatal(err)
	}
	mustTitle(t, v, "hand", "Second")

	// Gone: List drops it, and its entry.
	if err := os.Remove(p); err != nil {
		t.Fatal(err)
	}
	refs, err := v.List(ctx, "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range refs {
		if r.ID == "hand" {
			t.Fatal("deleted file still listed")
		}
	}
	if cached(v, "inbox/hand.md") != nil {
		t.Fatal("entry of a deleted file kept")
	}
}

// The racy guard: a file changed within racyWindow of its read is read again
// even when every stamp field matches. The stamp here is copied onto the
// entry by hand, as a file system with a coarse clock would leave it.
func TestCacheRacyGuard(t *testing.T) {
	v, root := newVault(t)
	p := filepath.Join(root, "inbox", "hand.md")
	if err := os.WriteFile(p, []byte(handDoc("One")), 0o600); err != nil {
		t.Fatal(err)
	}
	mustTitle(t, v, "hand", "One")
	st, _ := os.Stat(p)
	if err := os.WriteFile(p, []byte(handDoc("Two")), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, st.ModTime(), st.ModTime()); err != nil {
		t.Fatal(err)
	}
	now, _ := os.Stat(p)
	v.paths.mu.Lock()
	v.paths.refs["inbox/hand.md"].st = stampOf(now)
	v.paths.mu.Unlock()
	mustTitle(t, v, "hand", "Two")
}

func TestCacheOwnWritesShowAtOnce(t *testing.T) {
	settleAtOnce(t)
	v, _ := newVault(t)
	d, err := v.Create(ctx, inbox("Before", "Text."))
	if err != nil {
		t.Fatal(err)
	}
	got, err := v.Get(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	got.Title = "After"
	if _, err := v.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	mustTitle(t, v, d.ID, "After")
	if _, err := v.Move(ctx, d.ID, model.FolderArchive); err != nil {
		t.Fatal(err)
	}
	refs, err := v.List(ctx, model.FolderArchive, "")
	if err != nil || len(refs) != 1 || refs[0].Title != "After" {
		t.Fatalf("archive = %+v, %v", refs, err)
	}
}

// A Document from Get shares nothing that a caller can change with the
// cache: not Meta, and not the frontmatter its type does not declare, which
// Relink rewrites in place.
func TestCacheReturnsCopies(t *testing.T) {
	settleAtOnce(t)
	v, root := newVault(t)
	if err := os.WriteFile(filepath.Join(root, "inbox", "hand.md"), []byte(handDoc("One")), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := v.Get(ctx, "hand")
	if err != nil {
		t.Fatal(err)
	}
	d.Meta.Header().Tags[0] = "changed"
	d.Title = "changed"
	r, changed, err := d.Relink(map[string]string{"inbox-other": "inbox-new"}, nil)
	if err != nil || !changed {
		t.Fatalf("relink = %v, %v", changed, err)
	}
	var kept []string
	if !r.Kept("kept", &kept) || kept[0] != "[[inbox-new]]" {
		t.Fatalf("relinked kept = %v", kept)
	}
	again, err := v.Get(ctx, "hand")
	if err != nil {
		t.Fatal(err)
	}
	kept = nil
	if again.Title != "One" || again.Meta.Header().Tags[0] != "a" || !again.Kept("kept", &kept) || kept[0] != "[[inbox-other]]" {
		t.Fatalf("cache changed: title %q tags %v kept %v", again.Title, again.Meta.Header().Tags, kept)
	}
}

// A hand edit made while the cache is warm is still logged.
func TestCacheWarmHandEditIsLogged(t *testing.T) {
	settleAtOnce(t)
	v, root := newVault(t)
	d, err := v.Create(ctx, inbox("Title", "Text."))
	if err != nil {
		t.Fatal(err)
	}
	noGrace(t, root)
	if _, err := v.List(ctx, "", ""); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, "inbox", d.ID+".md")
	raw, _ := os.ReadFile(p)
	if err := os.WriteFile(p, []byte(strings.Replace(string(raw), "# Title", "# Tilte", 1)), 0o600); err != nil {
		t.Fatal(err)
	}
	refs, err := v.List(ctx, "", "")
	if err != nil || len(refs) != 1 || refs[0].Title != "Tilte" {
		t.Fatalf("list = %+v, %v", refs, err)
	}
	if res, err := v.Reconcile(ctx); err != nil || len(res.Changes) != 1 {
		t.Fatalf("reconcile = %+v, %v", res, err)
	}
	recs := eventLog(t, v)
	if last := recs[len(recs)-1]; last.Op != "external_edit" || last.Doc != d.ID {
		t.Fatalf("event = %+v", last)
	}
}

// A list of a type other than receipt does not read receipts/.
func TestListSkipsReceipts(t *testing.T) {
	v, root := newVault(t)
	if _, err := v.Create(ctx, inbox("One", "")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "receipts", "receipt-bad.md"), []byte("---\ntype: [\n---\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	refs, err := v.List(ctx, "", model.TypeInbox)
	if err != nil || len(refs) != 1 {
		t.Fatalf("list = %+v, %v", refs, err)
	}
	if cached(v, "receipts/receipt-bad.md") != nil {
		t.Fatal("List(inbox) read receipts/")
	}
	if _, err := v.List(ctx, "", model.TypeReceipt); err != nil {
		t.Fatal(err)
	}
	if cached(v, "receipts/receipt-bad.md") == nil {
		t.Fatal("List(receipt) did not read receipts/")
	}
}

func TestCacheConcurrent(t *testing.T) {
	settleAtOnce(t)
	v, root := newVault(t)
	var ids []string
	for i := range 8 {
		d, err := v.Create(ctx, inbox(fmt.Sprintf("Doc %d", i), "Text."))
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, d.ID)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for w := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 20 {
				id := ids[(w+i)%len(ids)]
				d, err := v.Get(ctx, id)
				if err != nil {
					errs <- err
					return
				}
				d.Meta.Header().Tags = append(d.Meta.Header().Tags, "x")
				if w%2 == 0 {
					d.Body = fmt.Sprintf("edit %d %d", w, i)
					if _, err := v.Update(ctx, d); err != nil && !errors.Is(err, model.ErrConflict) {
						errs <- err
						return
					}
				}
				if _, err := v.List(ctx, "", model.TypeInbox); err != nil {
					errs <- err
					return
				}
				if i%5 == 0 {
					_ = os.WriteFile(filepath.Join(root, "inbox", fmt.Sprintf("hand-%d.md", w)), []byte(handDoc(fmt.Sprint(i))), 0o600)
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}
