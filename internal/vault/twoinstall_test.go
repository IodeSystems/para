package vault

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/iodesystems/parasolve/internal/model"
)

// Two installs, two copies of one vault, two state dirs. The test plays the
// file-sync tool: copy moves files between the copies in a chosen order.
type pair struct {
	t        *testing.T
	a, b     *Vault
	rootA    string
	rootB    string
	now      atomic.Int64
	warnings []string
}

func (p *pair) clock() time.Time { return time.Unix(0, p.now.Load()).UTC() }

func (p *pair) advance(d time.Duration) { p.now.Add(int64(d)) }

func newPair(t *testing.T) *pair {
	t.Helper()
	p := &pair{t: t}
	p.now.Store(time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC).UnixNano())
	base := t.TempDir()
	p.rootA = filepath.Join(base, "a", "vault")
	if err := Init(p.rootA, "acme", templates(t), InitOptions{NoGit: true}); err != nil {
		t.Fatal(err)
	}
	// B starts as a sync copy of A.
	p.rootB = filepath.Join(base, "b", "vault")
	copyTree(t, p.rootA, p.rootB)
	p.a = p.open(p.rootA, "ia")
	p.b = p.open(p.rootB, "ib")
	for _, v := range []*Vault{p.a, p.b} {
		if res, err := v.Reconcile(ctx); err != nil || !res.Baseline {
			t.Fatalf("baseline = %+v, %v", res, err)
		}
	}
	return p
}

func (p *pair) open(root, install string) *Vault {
	p.t.Helper()
	v, err := Open(root, Options{ID: "acme", Actor: "user", Surface: "test", Install: install,
		StateDir: filepath.Join(filepath.Dir(root), "state"), Now: p.clock,
		Warn: func(m string) { p.warnings = append(p.warnings, m) }})
	if err != nil {
		p.t.Fatal(err)
	}
	return v
}

func copyTree(t *testing.T, from, to string) {
	t.Helper()
	err := filepath.WalkDir(from, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, path)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(to, rel), 0o700)
		}
		return copyFile(path, filepath.Join(to, rel))
	})
	if err != nil {
		t.Fatal(err)
	}
}

func copyFile(from, to string) error {
	data, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
		return err
	}
	return os.WriteFile(to, data, 0o600)
}

// send copies rel (a vault-relative path) from one root to the other. A
// missing source removes the target, like a delivered delete.
func send(t *testing.T, from, to, rel string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(from, rel)); err != nil {
		_ = os.Remove(filepath.Join(to, rel))
		return
	}
	if err := copyFile(filepath.Join(from, rel), filepath.Join(to, rel)); err != nil {
		t.Fatal(err)
	}
}

// sendEvents copies every event file of install from one root to the other.
func sendEvents(t *testing.T, from, to, install string) {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(from, "events", "*."+install+".jsonl"))
	for _, f := range files {
		send(t, from, to, filepath.Join("events", filepath.Base(f)))
	}
}

func countOp(t *testing.T, v *Vault, op string) int {
	t.Helper()
	n := 0
	for _, e := range eventLog(t, v) {
		if e.Op == op {
			n++
		}
	}
	return n
}

func rel(d model.Document) string { return string(d.Folder) + "/" + d.ID + ".md" }

// event before file: the sync tool delivers A's log first.
func TestSyncEventBeforeFile(t *testing.T) {
	p := newPair(t)
	d, err := p.a.Create(ctx, inbox("From A", "x"))
	if err != nil {
		t.Fatal(err)
	}
	sendEvents(t, p.rootA, p.rootB, "ia")
	if res, err := p.b.Reconcile(ctx); err != nil || len(res.Changes) != 0 {
		t.Fatalf("log only: %+v, %v", res, err)
	}
	send(t, p.rootA, p.rootB, rel(d))
	res, err := p.b.Reconcile(ctx)
	if err != nil || len(res.Changes) != 1 || res.Changes[0].Outcome != model.OutcomeSynced {
		t.Fatalf("file arrives: %+v, %v", res, err)
	}
	if n := countOp(t, p.b, "external_edit"); n != 0 {
		t.Fatalf("%d external_edit", n)
	}
	if got, err := p.b.Get(ctx, d.ID); err != nil || got.Rev != d.Rev {
		t.Fatalf("B reads %+v, %v", got, err)
	}
}

// file before event: held, then explained when the event arrives.
func TestSyncFileBeforeEvent(t *testing.T) {
	p := newPair(t)
	d, _ := p.a.Create(ctx, inbox("From A", "x"))
	send(t, p.rootA, p.rootB, rel(d))
	for range 2 { // still held on a second run inside the grace period
		res, err := p.b.Reconcile(ctx)
		if err != nil || len(res.Changes) != 1 || res.Changes[0].Outcome != model.OutcomePending {
			t.Fatalf("held: %+v, %v", res, err)
		}
		p.advance(time.Minute)
	}
	sendEvents(t, p.rootA, p.rootB, "ia")
	res, err := p.b.Reconcile(ctx)
	if err != nil || len(res.Changes) != 1 || res.Changes[0].Outcome != model.OutcomeSynced {
		t.Fatalf("explained: %+v, %v", res, err)
	}
	if n := countOp(t, p.b, "external_edit"); n != 0 {
		t.Fatalf("%d external_edit", n)
	}
	if res, _ := p.b.Reconcile(ctx); len(res.Changes) != 0 {
		t.Fatalf("settled: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(p.b.stateDir, "pending.json")); err == nil {
		t.Error("pending file not cleared")
	}
}

// file never explained: logged once, after the grace period.
func TestSyncUnexplainedFileLogsAfterGrace(t *testing.T) {
	p := newPair(t)
	d, _ := p.a.Create(ctx, inbox("Hand", "x"))
	send(t, p.rootA, p.rootB, rel(d))
	if res, _ := p.b.Reconcile(ctx); res.Changes[0].Outcome != model.OutcomePending {
		t.Fatalf("first: %+v", res)
	}
	p.advance(DefaultGrace - time.Second)
	if res, _ := p.b.Reconcile(ctx); res.Changes[0].Outcome != model.OutcomePending {
		t.Fatalf("inside grace: %+v", res)
	}
	p.advance(2 * time.Second)
	res, err := p.b.Reconcile(ctx)
	if err != nil || len(res.Changes) != 1 || res.Changes[0].Outcome != model.OutcomeLogged {
		t.Fatalf("after grace: %+v, %v", res, err)
	}
	if n := countOp(t, p.b, "external_edit"); n != 1 {
		t.Fatalf("%d external_edit", n)
	}
	if res, _ := p.b.Reconcile(ctx); len(res.Changes) != 0 {
		t.Fatalf("settled: %+v", res)
	}
}

// A hold restarts when the file changes again.
func TestSyncHoldRestartsOnNewHash(t *testing.T) {
	p := newPair(t)
	d, _ := p.a.Create(ctx, inbox("Hand", "x"))
	send(t, p.rootA, p.rootB, rel(d))
	p.b.Reconcile(ctx)
	p.advance(DefaultGrace - time.Second)
	if err := os.WriteFile(filepath.Join(p.rootB, rel(d)), []byte("---\ntype: inbox\n---\n\n# Changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p.b.Reconcile(ctx)
	p.advance(time.Minute)
	if res, _ := p.b.Reconcile(ctx); res.Changes[0].Outcome != model.OutcomePending {
		t.Fatalf("restarted hold released early: %+v", res)
	}
}

// partial event line: skipped with a warning; full line read after it lands.
func TestSyncPartialEventLine(t *testing.T) {
	p := newPair(t)
	p.a.Create(ctx, inbox("One", ""))
	p.a.Create(ctx, inbox("Two", ""))
	files, _ := filepath.Glob(filepath.Join(p.rootA, "events", "*.ia.jsonl"))
	if len(files) != 1 {
		t.Fatalf("files = %v", files)
	}
	data, _ := os.ReadFile(files[0])
	cut := data[:len(data)-40] // second line loses its tail and its newline
	dst := filepath.Join(p.rootB, "events", filepath.Base(files[0]))
	os.WriteFile(dst, cut, 0o600)
	got := eventLog(t, p.b)
	if len(got) != 1 || len(p.warnings) != 1 || !strings.Contains(p.warnings[0], "partial") {
		t.Fatalf("events = %d, warnings = %v", len(got), p.warnings)
	}
	os.WriteFile(dst, data, 0o600)
	if got := eventLog(t, p.b); len(got) != 2 {
		t.Fatalf("after full delivery: %d", len(got))
	}
	// A bad line in the middle is corruption, not a sync in flight.
	os.WriteFile(dst, append([]byte("{bad\n"), data...), 0o600)
	if _, err := p.b.Log().Tail(10); err == nil {
		t.Fatal("middle corruption accepted")
	}
}

// A crash leaves a partial last line in the writer's own file: the next
// append must not glue onto it.
func TestOwnPartialLineIsTrimmedOnAppend(t *testing.T) {
	p := newPair(t)
	p.a.Create(ctx, inbox("One", ""))
	files, _ := filepath.Glob(filepath.Join(p.rootA, "events", "*.ia.jsonl"))
	f, _ := os.OpenFile(files[0], os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString(`{"ts":"2026-11-0`)
	f.Close()
	if _, err := p.a.Create(ctx, inbox("Two", "")); err != nil {
		t.Fatal(err)
	}
	if got := eventLog(t, p.a); len(got) != 2 {
		t.Fatalf("events = %d", len(got))
	}
}

// Readers merge both installs by ts.
func TestSyncMergeByTimestamp(t *testing.T) {
	p := newPair(t)
	var want []string
	for i := range 4 {
		v, name := p.a, "A"
		if i%2 == 1 {
			v, name = p.b, "B"
		}
		d, err := v.Create(ctx, inbox(name, ""))
		if err != nil {
			t.Fatal(err)
		}
		want = append(want, d.ID)
		p.advance(time.Second)
	}
	sendEvents(t, p.rootA, p.rootB, "ia")
	sendEvents(t, p.rootB, p.rootA, "ib")
	for _, v := range []*Vault{p.a, p.b} {
		got := eventLog(t, v)
		if len(got) != 4 {
			t.Fatalf("events = %d", len(got))
		}
		for i, e := range got {
			if e.Doc != want[i] {
				t.Errorf("event %d = %s, want %s", i, e.Doc, want[i])
			}
		}
	}
}

// A synced move is explained by the move event, with no external_edit.
func TestSyncMove(t *testing.T) {
	p := newPair(t)
	d, _ := p.a.Create(ctx, inbox("Mover", ""))
	sendEvents(t, p.rootA, p.rootB, "ia")
	send(t, p.rootA, p.rootB, rel(d))
	p.b.Reconcile(ctx)
	if _, err := p.a.Move(ctx, d.ID, model.FolderArchive); err != nil {
		t.Fatal(err)
	}
	sendEvents(t, p.rootA, p.rootB, "ia")
	send(t, p.rootA, p.rootB, "archive/"+d.ID+".md")
	send(t, p.rootA, p.rootB, "inbox/"+d.ID+".md")
	res, err := p.b.Reconcile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range res.Changes {
		if c.Outcome != model.OutcomeSynced {
			t.Errorf("change %+v", c)
		}
	}
	if n := countOp(t, p.b, "external_edit"); n != 0 || len(res.Changes) != 1 {
		t.Fatalf("external_edit = %d, changes = %+v", n, res.Changes)
	}
	if res, _ := p.b.Reconcile(ctx); len(res.Changes) != 0 {
		t.Fatalf("settled: %+v", res)
	}
}

// Two machines update one file between syncs. Each Update succeeds on its
// own copy; the Rev check cannot see across machines. When A's file reaches B
// the sync tool replaces B's file (or makes a conflict copy), and B's next
// Update with its old Rev fails with ErrConflict.
func TestSyncTwoUpdatesBetweenSyncs(t *testing.T) {
	p := newPair(t)
	d, _ := p.a.Create(ctx, inbox("Shared", "v0"))
	sendEvents(t, p.rootA, p.rootB, "ia")
	send(t, p.rootA, p.rootB, rel(d))
	p.b.Reconcile(ctx)
	da, _ := p.a.Get(ctx, d.ID)
	db, _ := p.b.Get(ctx, d.ID)
	da.Body = "from A"
	db.Body = "from B"
	ua, err := p.a.Update(ctx, da)
	if err != nil {
		t.Fatal(err)
	}
	ub, err := p.b.Update(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if ua.Rev == ub.Rev {
		t.Fatal("revs equal")
	}
	// The tool delivers A's file over B's. B's own update is gone from disk;
	// its event stays in B's log.
	sendEvents(t, p.rootA, p.rootB, "ia")
	send(t, p.rootA, p.rootB, rel(d))
	res, err := p.b.Reconcile(ctx)
	if err != nil || len(res.Changes) != 1 || res.Changes[0].Outcome != model.OutcomeSynced {
		t.Fatalf("reconcile: %+v, %v", res, err)
	}
	ub.Body = "B again"
	if _, err := p.b.Update(ctx, ub); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("stale B update: %v", err)
	}
	// B's write is reported as lost on both machines once logs have synced.
	sendEvents(t, p.rootB, p.rootA, "ib")
	for _, v := range []*Vault{p.a, p.b} {
		lost, err := v.LostUpdates()
		if err != nil || len(lost) != 1 || lost[0].Install != "ib" || lost[0].Doc != d.ID {
			t.Fatalf("lost updates = %+v, %v", lost, err)
		}
	}
	// B edits again from the surviving version, so the report clears.
	p.advance(time.Minute)
	cur, _ := p.b.Get(ctx, d.ID)
	cur.Body = "B merged by hand"
	if _, err := p.b.Update(ctx, cur); err != nil {
		t.Fatal(err)
	}
	if lost, err := p.b.LostUpdates(); err != nil || len(lost) != 0 {
		t.Fatalf("after re-edit = %+v, %v", lost, err)
	}
}

func TestPlainUpdatesAreNotLost(t *testing.T) {
	p := newPair(t)
	d, _ := p.a.Create(ctx, inbox("Shared", "v0"))
	for _, body := range []string{"one", "two", "three"} {
		p.advance(time.Second)
		cur, _ := p.a.Get(ctx, d.ID)
		cur.Body = body
		if _, err := p.a.Update(ctx, cur); err != nil {
			t.Fatal(err)
		}
	}
	if lost, err := p.a.LostUpdates(); err != nil || len(lost) != 0 {
		t.Fatalf("lost = %+v, %v", lost, err)
	}
}

func TestSyncConflictCopies(t *testing.T) {
	names := []string{
		"inbox/note (1).md", "inbox/note (Ann's conflicted copy 2026-11-01).md", "inbox/note.conflict1.md",
		"inbox/note.md.conflict2", "inbox/note.sync-conflict-20261101-101500-ABCDEFG.md", "inbox/note.md.orig",
		"events/2026-11.ia.sync-conflict-20261101-101500-ABCDEFG.jsonl",
	}
	p := newPair(t)
	d, _ := p.a.Create(ctx, inbox("Real", ""))
	for _, n := range names {
		send(t, p.rootA, p.rootA, n) // no-op guard
		if err := os.WriteFile(filepath.Join(p.rootA, n), []byte("copy"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// A copy of a real document keeps a valid-looking body but is no document.
	raw, _ := os.ReadFile(filepath.Join(p.rootA, rel(d)))
	os.WriteFile(filepath.Join(p.rootA, "inbox", d.ID+" (1).md"), raw, 0o600)
	refs, err := p.a.List(ctx, "", "")
	if err != nil || len(refs) != 1 {
		t.Fatalf("refs = %v, %v", refs, err)
	}
	findings, err := p.a.Check(ctx)
	if err != nil {
		t.Fatal(err)
	}
	conflicts := 0
	for _, f := range findings {
		if f.Code != "sync-conflict" {
			t.Errorf("finding %+v", f)
		}
		conflicts++
	}
	if conflicts != len(names)+1 {
		t.Fatalf("%d sync-conflict findings, want %d: %v", conflicts, len(names)+1, findings)
	}
	// Reconcile ignores them: no external_edit, nothing pending.
	res, err := p.a.Reconcile(ctx)
	if err != nil || len(res.Changes) != 0 {
		t.Fatalf("reconcile: %+v, %v", res, err)
	}
	// The conflict copy of an event file is not read as events.
	if got := eventLog(t, p.a); len(got) != 1 {
		t.Fatalf("events = %d", len(got))
	}
	// Creating the same id still works: the copy holds no id.
	if _, err := p.a.Create(ctx, func() model.Document { x := inbox("Other", ""); x.ID = "note"; return x }()); err != nil {
		t.Fatal(err)
	}
}
