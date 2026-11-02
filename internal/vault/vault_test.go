package vault

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iodesystems/paradize/docs"
	"github.com/iodesystems/paradize/internal/events"
	"github.com/iodesystems/paradize/internal/model"
	"github.com/iodesystems/paradize/internal/paramd"
	"github.com/iodesystems/paradize/internal/props"
)

var ctx = context.Background()

func templates(t testing.TB) fs.FS {
	t.Helper()
	sub, err := fs.Sub(docs.Templates, "templates")
	if err != nil {
		t.Fatal(err)
	}
	return sub
}

// stateFor is the machine-local state dir of a test vault: beside the vault,
// outside its tree.
func stateFor(root string) string { return filepath.Join(filepath.Dir(root), "state") }

func open(t testing.TB, root string) *Vault {
	t.Helper()
	v, err := Open(root, Options{ID: "acme", Actor: "user", Surface: "test", Install: "m1", StateDir: stateFor(root)})
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// noGrace makes reconcile log a hand edit at once, as on a one-machine vault.
func noGrace(t testing.TB, root string) {
	t.Helper()
	p := filepath.Join(root, ".para", "vault.properties")
	cfg, err := props.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	cfg["reconcile.grace"] = "0s"
	if err := os.WriteFile(p, []byte(props.Format(cfg)), 0o600); err != nil {
		t.Fatal(err)
	}
}

func newVault(t testing.TB) (*Vault, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "vault")
	if err := Init(root, "acme", templates(t), InitOptions{}); err != nil {
		t.Fatal(err)
	}
	return open(t, root), root
}

func inbox(title, body string) model.Document {
	return model.Document{
		Folder: model.FolderInbox, Title: title, Body: body,
		Meta: &model.Inbox{Common: model.Common{Type: model.TypeInbox}},
	}
}

func eventLog(t testing.TB, v *Vault) []model.Event {
	t.Helper()
	recs, err := v.Log().Tail(1 << 20)
	if err != nil {
		t.Fatal(err)
	}
	return recs
}

func TestInit(t *testing.T) {
	v, root := newVault(t)
	for _, d := range strings.Fields("inbox projects areas resources archive rfx constraints policies conventions org journal interactions templates events .para .git") {
		if st, err := os.Stat(filepath.Join(root, d)); err != nil || !st.IsDir() {
			t.Errorf("missing dir %s: %v", d, err)
		}
	}
	cfg, err := props.Load(filepath.Join(root, ".para", "vault.properties"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg["egress.default"] != "deny" || cfg["vault.id"] != "acme" {
		t.Errorf("config = %v", cfg)
	}
	for _, typ := range model.Types {
		if _, err := os.Stat(filepath.Join(root, "templates", string(typ)+".md")); err != nil {
			t.Errorf("template %s: %v", typ, err)
		}
	}
	// A fresh vault has no manifest on this machine: the first reconcile
	// baselines, the second finds nothing.
	for i, wantBase := range []bool{true, false} {
		res, err := v.Reconcile(ctx)
		if err != nil || len(res.Changes) != 0 || res.Baseline != wantBase {
			t.Errorf("fresh vault reconcile %d = %+v, %v", i, res, err)
		}
	}
	if got := eventLog(t, v); len(got) != 0 {
		t.Errorf("init logged %v", got)
	}
	findings, err := v.Check(ctx)
	if err != nil || len(findings) != 0 {
		t.Errorf("fresh vault check = %v, %v", findings, err)
	}
}

func TestInitGitignore(t *testing.T) {
	_, root := newVault(t)
	cases := []struct {
		path    string
		ignored bool
	}{
		{".para/anything", true},
		{"inbox/.note.md.tmp-123", true},
		{".para/vault.properties", false},
		{"inbox/x.md", false},
		{"events/2026-11.jsonl", false},
	}
	for _, c := range cases {
		err := exec.Command("git", "-C", root, "check-ignore", "-q", c.path).Run()
		if (err == nil) != c.ignored {
			t.Errorf("%s: ignored=%v, want %v", c.path, err == nil, c.ignored)
		}
	}
}

func TestInitRefuses(t *testing.T) {
	dir := t.TempDir()
	full := filepath.Join(dir, "full")
	os.MkdirAll(full, 0o700)
	os.WriteFile(filepath.Join(full, "x"), nil, 0o600)
	empty := filepath.Join(dir, "empty")
	os.MkdirAll(empty, 0o700)
	cases := []struct {
		name, root, id string
		ok             bool
	}{
		{"non-empty dir", full, "acme", false},
		{"uppercase id", filepath.Join(dir, "a"), "Acme", false},
		{"dot id", filepath.Join(dir, "b"), "a.b", false},
		{"path id", filepath.Join(dir, "c"), "../x", false},
		{"long id", filepath.Join(dir, "d"), strings.Repeat("a", 64), false},
		{"empty dir", empty, "acme", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := Init(c.root, c.id, templates(t), InitOptions{})
			if (err == nil) != c.ok {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestOpenRefuses(t *testing.T) {
	_, root := newVault(t)
	cases := []struct {
		name, root, id string
	}{
		{"not a vault", t.TempDir(), ""},
		{"wrong id", root, "personal"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := Open(c.root, Options{ID: c.id, ReadOnly: true}); err == nil {
				t.Fatal("want error")
			}
		})
	}
}

func TestCreate(t *testing.T) {
	v, root := newVault(t)
	d, err := v.Create(ctx, inbox("A title", "Text."))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(d.ID, "inbox-") || !paramd.ValidStem(d.ID) {
		t.Errorf("id = %q", d.ID)
	}
	data, err := os.ReadFile(filepath.Join(root, "inbox", d.ID+".md"))
	if err != nil {
		t.Fatal(err)
	}
	if d.Rev != events.Hash(data) {
		t.Errorf("rev = %s", d.Rev)
	}
	got, err := v.Get(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "A title" || got.Body != "Text." || got.Rev != d.Rev || got.Folder != model.FolderInbox {
		t.Errorf("got %+v", got)
	}
	left, _ := os.ReadDir(filepath.Join(root, "inbox"))
	if len(left) != 1 {
		t.Errorf("inbox holds %v, want only the document", left)
	}
	if left, _ := os.ReadDir(filepath.Join(root, ".para")); len(left) != 1 {
		t.Errorf(".para holds %v, want only vault.properties", left)
	}
}

func TestCreateUniqueAcrossFolders(t *testing.T) {
	v, root := newVault(t)
	first := inbox("One", "")
	first.ID = "same-name"
	if _, err := v.Create(ctx, first); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "archive", "other-name.md"), []byte("x"), 0o600)
	cases := []struct {
		name, id string
		folder   model.Folder
	}{
		{"same folder", "same-name", model.FolderInbox},
		{"other folder", "same-name", model.FolderArchive},
		{"hand-placed file", "other-name", model.FolderInbox},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := inbox("Two", "")
			d.ID, d.Folder = c.id, c.folder
			if _, err := v.Create(ctx, d); !errors.Is(err, model.ErrExists) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestCreateRefusals(t *testing.T) {
	bad := func(mut func(*model.Document)) model.Document {
		d := inbox("Title", "Text.")
		mut(&d)
		return d
	}
	cases := []struct {
		name string
		doc  model.Document
		code string // lint code, or "" for a non-lint refusal
	}{
		{"raw html", bad(func(d *model.Document) { d.Body = "<b>x</b>" }), paramd.CodeHTML},
		{"signed url", bad(func(d *model.Document) { d.Body = "https://x.example/a?token=abc" }), paramd.CodeURL},
		{"second h1", bad(func(d *model.Document) { d.Body = "# Other" }), paramd.CodeTitle},
		{"crlf", bad(func(d *model.Document) { d.Body = "a\r\nb" }), paramd.CodeLineEnding},
		{"bad date", bad(func(d *model.Document) { d.Meta.Header().Created = "11/01/2026" }), paramd.CodeFieldValue},
		{"bad enum", bad(func(d *model.Document) {
			d.Meta = &model.Project{Common: model.Common{Type: model.TypeProject}, Status: "maybe", Owner: "x"}
		}), paramd.CodeFieldValue},
		{"path id", bad(func(d *model.Document) { d.ID = "../escape" }), ""},
		{"uppercase id", bad(func(d *model.Document) { d.ID = "Upper" }), ""},
		{"bad folder", bad(func(d *model.Document) { d.Folder = "../x" }), ""},
		{"template folder", bad(func(d *model.Document) { d.Folder = "templates" }), ""},
		{"no meta", bad(func(d *model.Document) { d.Meta = nil }), ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v, root := newVault(t)
			_, err := v.Create(ctx, c.doc)
			if err == nil {
				t.Fatal("want refusal")
			}
			if c.code != "" {
				var le *model.LintError
				if !errors.As(err, &le) {
					t.Fatalf("err = %v, want *LintError", err)
				}
				found := false
				for _, f := range le.Findings {
					found = found || f.Code == c.code
				}
				if !found {
					t.Fatalf("findings = %v, want %s", le.Findings, c.code)
				}
			}
			if got := eventLog(t, v); len(got) != 0 {
				t.Errorf("events = %v", got)
			}
			if entries, _ := os.ReadDir(filepath.Join(root, "inbox")); len(entries) != 0 {
				t.Errorf("wrote %v", entries)
			}
		})
	}
}

func TestUpdateRefusesLint(t *testing.T) {
	v, root := newVault(t)
	d, err := v.Create(ctx, inbox("Title", "Text."))
	if err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(root, "inbox", d.ID+".md"))
	d.Body = "<script>x</script>"
	if _, err := v.Update(ctx, d); err == nil {
		t.Fatal("want refusal")
	}
	after, _ := os.ReadFile(filepath.Join(root, "inbox", d.ID+".md"))
	if string(before) != string(after) {
		t.Fatal("file changed")
	}
	if got := eventLog(t, v); len(got) != 1 {
		t.Fatalf("events = %v", got)
	}
}

func TestEventPerMutation(t *testing.T) {
	v, _ := newVault(t)
	proj := model.Document{
		ID: "acme-portal", Folder: model.FolderProjects, Title: "Portal",
		Meta: &model.Project{Common: model.Common{Type: model.TypeProject}, Status: model.ProjectActive, Owner: "Jordan"},
	}
	d, err := v.Create(ctx, proj)
	if err != nil {
		t.Fatal(err)
	}
	steps := []struct {
		name         string
		run          func() error
		op, from, to string
	}{
		{"body edit", func() error {
			got, _ := v.Get(ctx, d.ID)
			got.Body = "Edited."
			_, err := v.Update(ctx, got)
			return err
		}, events.OpUpdate, "", ""},
		{"status change", func() error {
			got, _ := v.Get(ctx, d.ID)
			got.Meta.(*model.Project).Status = model.ProjectPaused
			_, err := v.Update(ctx, got)
			return err
		}, events.OpUpdate, "active", "paused"},
		{"move", func() error {
			_, err := v.Move(ctx, d.ID, model.FolderArchive)
			return err
		}, events.OpMove, "projects", "archive"},
	}
	for i, s := range steps {
		t.Run(s.name, func(t *testing.T) {
			if err := s.run(); err != nil {
				t.Fatal(err)
			}
			recs := eventLog(t, v)
			if len(recs) != i+2 {
				t.Fatalf("events = %d, want %d", len(recs), i+2)
			}
			r := recs[len(recs)-1]
			if r.Op != s.op || r.Doc != d.ID || r.From != s.from || r.To != s.to || r.Vault != "acme" || r.Actor != "user" || r.Surface != "test" {
				t.Fatalf("event = %+v", r)
			}
		})
	}
	first := eventLog(t, v)[0]
	if first.Op != events.OpCreate || first.To != "projects" {
		t.Errorf("create event = %+v", first)
	}
	res, err := v.Reconcile(ctx)
	if err != nil || len(res.Changes) != 0 {
		t.Errorf("reconcile after vault writes = %+v, %v", res, err)
	}
}

func TestNoContentInEvents(t *testing.T) {
	const marker = "zebrafish"
	v, root := newVault(t)
	d, err := v.Create(ctx, inbox(marker+" title", marker+" body"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(d.ID, marker) {
		t.Fatalf("id holds content: %s", d.ID)
	}
	d.Body = marker + " edited"
	if d, err = v.Update(ctx, d); err != nil {
		t.Fatal(err)
	}
	if _, err = v.Move(ctx, d.ID, model.FolderArchive); err != nil {
		t.Fatal(err)
	}
	noGrace(t, root)
	p := filepath.Join(root, "archive", d.ID+".md")
	raw, _ := os.ReadFile(p)
	os.WriteFile(p, append(raw, []byte(marker+" by hand\n")...), 0o600)
	if _, err := v.Reconcile(ctx); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(root, "events", "*.jsonl"))
	if len(files) != 1 {
		t.Fatalf("event files = %v", files)
	}
	data, _ := os.ReadFile(files[0])
	if len(strings.Split(strings.TrimSpace(string(data)), "\n")) != 4 {
		t.Fatalf("events:\n%s", data)
	}
	if strings.Contains(string(data), marker) {
		t.Fatalf("content in events:\n%s", data)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatal(err)
		}
		// ts install actor surface vault op doc from to hash
		if len(rec) != 10 {
			t.Fatalf("fields = %v", rec)
		}
		if h, _ := rec["hash"].(string); len(h) != len("sha256:")+64 {
			t.Fatalf("hash = %q", h)
		}
	}
}

func TestUpdateConflict(t *testing.T) {
	v, _ := newVault(t)
	d, err := v.Create(ctx, inbox("Title", "One."))
	if err != nil {
		t.Fatal(err)
	}
	stale := d
	d.Body = "Two."
	if _, err := v.Update(ctx, d); err != nil {
		t.Fatal(err)
	}
	stale.Body = "Three."
	if _, err := v.Update(ctx, stale); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("err = %v", err)
	}
	stale.Rev = ""
	if _, err := v.Update(ctx, stale); !errors.Is(err, model.ErrConflict) {
		t.Fatalf("empty rev: err = %v", err)
	}
	got, _ := v.Get(ctx, d.ID)
	if got.Body != "Two." {
		t.Fatalf("body = %q", got.Body)
	}
	missing := inbox("X", "")
	missing.ID = "no-such-doc"
	if _, err := v.Update(ctx, missing); !errors.Is(err, model.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestUpdateKeepsUnknownFrontmatterAndMode(t *testing.T) {
	v, root := newVault(t)
	p := filepath.Join(root, "inbox", "hand-made.md")
	os.WriteFile(p, []byte("---\ntype: inbox\ncreated: 2026-11-01\nnote: keep me\n---\n\n# Hand\n\nText.\n"), 0o640)
	d, err := v.Get(ctx, "hand-made")
	if err != nil {
		t.Fatal(err)
	}
	d.Body = "Edited."
	if _, err := v.Update(ctx, d); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	if !strings.Contains(string(data), "note: keep me") {
		t.Errorf("lost key:\n%s", data)
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o640 {
		t.Errorf("mode = %v", st.Mode())
	}
}

// A hand edit that a vault write absorbs still leaves an external_edit.
func TestMutationLogsPendingHandEdit(t *testing.T) {
	cases := []struct {
		name string
		run  func(v *Vault, d model.Document) error
	}{
		{"update", func(v *Vault, d model.Document) error {
			d.Body = "Edited by para."
			_, err := v.Update(ctx, d)
			return err
		}},
		{"move", func(v *Vault, d model.Document) error {
			_, err := v.Move(ctx, d.ID, model.FolderArchive)
			return err
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v, root := newVault(t)
			d, err := v.Create(ctx, inbox("Title", "Text."))
			if err != nil {
				t.Fatal(err)
			}
			p := filepath.Join(root, "inbox", d.ID+".md")
			raw, _ := os.ReadFile(p)
			os.WriteFile(p, append(raw, []byte("By hand.\n")...), 0o600)
			fresh, err := v.Get(ctx, d.ID)
			if err != nil {
				t.Fatal(err)
			}
			if err := c.run(v, fresh); err != nil {
				t.Fatal(err)
			}
			recs := eventLog(t, v)
			if len(recs) != 3 || recs[1].Op != events.OpExternalEdit || recs[1].Doc != d.ID {
				t.Fatalf("events = %+v", recs)
			}
			res, err := v.Reconcile(ctx)
			if err != nil || len(res.Changes) != 0 {
				t.Fatalf("reconcile = %+v, %v", res, err)
			}
		})
	}
}

func TestMoveKeepsFilenameAndLinks(t *testing.T) {
	v, root := newVault(t)
	proj := model.Document{
		ID: "acme-portal", Folder: model.FolderProjects, Title: "Portal",
		Meta: &model.Project{Common: model.Common{Type: model.TypeProject}, Status: model.ProjectDone, Owner: "Jordan"},
	}
	task := model.Document{
		ID: "task-one", Folder: model.FolderProjects, Title: "Task", Body: "See [[acme-portal]].",
		Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskDone, Project: model.LinkTo("acme-portal")},
	}
	for _, d := range []model.Document{proj, task} {
		if _, err := v.Create(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	moved, err := v.Move(ctx, "acme-portal", model.FolderArchive)
	if err != nil {
		t.Fatal(err)
	}
	if moved.Folder != model.FolderArchive || moved.ID != "acme-portal" {
		t.Errorf("moved = %+v", moved)
	}
	if _, err := os.Stat(filepath.Join(root, "archive", "acme-portal.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "projects", "acme-portal.md")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("old file: %v", err)
	}
	if findings, err := v.Check(ctx); err != nil || len(findings) != 0 {
		t.Fatalf("check = %v, %v", findings, err)
	}
	n := len(eventLog(t, v))
	if _, err := v.Move(ctx, "acme-portal", model.FolderArchive); err != nil {
		t.Fatal(err)
	}
	if len(eventLog(t, v)) != n {
		t.Error("a move to the same folder logged an event")
	}
	if _, err := v.Move(ctx, "acme-portal", "templates"); err == nil {
		t.Error("move to templates must fail")
	}
	if _, err := v.Move(ctx, "no-such", model.FolderArchive); !errors.Is(err, model.ErrNotFound) {
		t.Errorf("err = %v", err)
	}
}

func TestFailedLogUndoesTheChange(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory modes")
	}
	cases := []struct {
		name  string
		run   func(v *Vault, d model.Document) error
		check func(t *testing.T, root string, d model.Document)
	}{
		{"create", func(v *Vault, _ model.Document) error {
			_, err := v.Create(ctx, inbox("New", ""))
			return err
		}, func(t *testing.T, root string, _ model.Document) {
			if entries, _ := os.ReadDir(filepath.Join(root, "inbox")); len(entries) != 1 {
				t.Errorf("inbox = %v", entries)
			}
		}},
		{"update", func(v *Vault, d model.Document) error {
			d.Body = "Changed."
			_, err := v.Update(ctx, d)
			return err
		}, func(t *testing.T, root string, d model.Document) {
			data, _ := os.ReadFile(filepath.Join(root, "inbox", d.ID+".md"))
			if !strings.Contains(string(data), "Text.") {
				t.Errorf("file = %s", data)
			}
		}},
		{"move", func(v *Vault, d model.Document) error {
			_, err := v.Move(ctx, d.ID, model.FolderArchive)
			return err
		}, func(t *testing.T, root string, d model.Document) {
			if _, err := os.Stat(filepath.Join(root, "inbox", d.ID+".md")); err != nil {
				t.Error(err)
			}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v, root := newVault(t)
			d, err := v.Create(ctx, inbox("Title", "Text."))
			if err != nil {
				t.Fatal(err)
			}
			evDir := filepath.Join(root, "events")
			files, _ := filepath.Glob(filepath.Join(evDir, "*.jsonl"))
			for _, f := range files {
				os.Chmod(f, 0o400)
			}
			os.Chmod(evDir, 0o500)
			t.Cleanup(func() { os.Chmod(evDir, 0o700) })
			if err := c.run(v, d); err == nil {
				t.Fatal("want error")
			}
			c.check(t, root, d)
			res, err := v.Reconcile(ctx)
			if err != nil || len(res.Changes) != 0 {
				t.Errorf("reconcile = %+v, %v", res, err)
			}
		})
	}
}

func TestReconcileThroughVault(t *testing.T) {
	v, root := newVault(t)
	d, err := v.Create(ctx, inbox("Title", "Text."))
	if err != nil {
		t.Fatal(err)
	}
	noGrace(t, root)
	p := filepath.Join(root, "inbox", d.ID+".md")
	raw, _ := os.ReadFile(p)
	os.WriteFile(p, append(raw, []byte("By hand.\n")...), 0o600)
	res, err := v.Reconcile(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Changes) != 1 || res.Changes[0].Kind != model.ChangeChanged {
		t.Fatalf("res = %+v", res)
	}
	recs := eventLog(t, v)
	last := recs[len(recs)-1]
	if last.Op != "external_edit" || last.Doc != d.ID || last.From == "" || last.To == "" || last.From == last.To {
		t.Fatalf("event = %+v", last)
	}
	if res, _ := v.Reconcile(ctx); len(res.Changes) != 0 {
		t.Fatalf("second run = %+v", res)
	}
}

// A clone has no manifest. The files become the baseline with no events, and
// a write does not make the other files look new.
func TestMissingManifest(t *testing.T) {
	cases := []struct {
		name     string
		write    bool
		baseline bool
	}{
		{"reconcile first", false, true},
		{"write first", true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v, root := newVault(t)
			if _, err := v.Create(ctx, inbox("One", "")); err != nil {
				t.Fatal(err)
			}
			os.Remove(filepath.Join(stateFor(root), "manifest.json"))
			n := 1
			if c.write {
				if _, err := v.Create(ctx, inbox("Two", "")); err != nil {
					t.Fatal(err)
				}
				n = 2
			}
			res, err := v.Reconcile(ctx)
			if err != nil || res.Baseline != c.baseline || len(res.Changes) != 0 {
				t.Fatalf("res = %+v, %v", res, err)
			}
			if got := eventLog(t, v); len(got) != n {
				t.Fatalf("events = %v", got)
			}
		})
	}
}

// A clone holds only committed files: no empty folders, no ignored state.
func TestWriteAfterClone(t *testing.T) {
	_, root := newVault(t)
	cfg, err := os.ReadFile(filepath.Join(root, ".para", "vault.properties"))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{".para", "events", "inbox", "archive"} {
		if err := os.RemoveAll(filepath.Join(root, d)); err != nil {
			t.Fatal(err)
		}
	}
	os.MkdirAll(filepath.Join(root, ".para"), 0o700)
	os.WriteFile(filepath.Join(root, ".para", "vault.properties"), cfg, 0o600)

	v := open(t, root)
	d, err := v.Create(ctx, inbox("After clone", ""))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.Move(ctx, d.ID, model.FolderArchive); err != nil {
		t.Fatal(err)
	}
	if got := eventLog(t, v); len(got) != 2 {
		t.Fatalf("events = %v", got)
	}
}

func TestConcurrentWriters(t *testing.T) {
	const writers = 24
	v, root := newVault(t)
	ids := make([]string, writers)
	var wg sync.WaitGroup
	for i := range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d, err := open(t, root).Create(ctx, inbox(fmt.Sprintf("Item %d", i), ""))
			if err != nil {
				t.Error(err)
				return
			}
			ids[i] = d.ID
		}()
	}
	wg.Wait()
	refs, err := v.List(ctx, model.FolderInbox, "")
	if err != nil || len(refs) != writers {
		t.Fatalf("refs = %d, %v", len(refs), err)
	}
	if got := eventLog(t, v); len(got) != writers {
		t.Fatalf("events = %d", len(got))
	}
	m, _, err := events.LoadManifest(filepath.Join(stateFor(root), "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if _, ok := m.Files["inbox/"+id+".md"]; !ok {
			t.Errorf("%s not in manifest", id)
		}
	}
}

func TestConcurrentCreateSameID(t *testing.T) {
	const writers = 12
	_, root := newVault(t)
	results := make(chan error, writers)
	for range writers {
		go func() {
			d := inbox("Same", "")
			d.ID = "same-id"
			_, err := open(t, root).Create(ctx, d)
			results <- err
		}()
	}
	won := 0
	for range writers {
		switch err := <-results; {
		case err == nil:
			won++
		case !errors.Is(err, model.ErrExists):
			t.Error(err)
		}
	}
	if won != 1 {
		t.Fatalf("%d writers won", won)
	}
}

// Writers retry on conflict. The lock plus Rev loses no increment.
func TestConcurrentUpdatesLoseNothing(t *testing.T) {
	const writers = 16
	v, root := newVault(t)
	d, err := v.Create(ctx, inbox("Counter", "0"))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range writers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			w := open(t, root)
			for {
				got, err := w.Get(ctx, d.ID)
				if err != nil {
					t.Error(err)
					return
				}
				n, _ := strconv.Atoi(got.Body)
				got.Body = strconv.Itoa(n + 1)
				_, err = w.Update(ctx, got)
				if err == nil {
					return
				}
				if !errors.Is(err, model.ErrConflict) {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
	got, err := v.Get(ctx, d.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != strconv.Itoa(writers) {
		t.Fatalf("counter = %s", got.Body)
	}
	if n := len(eventLog(t, v)); n != writers+1 {
		t.Fatalf("events = %d", n)
	}
}

func TestLockTimeoutHonorsContext(t *testing.T) {
	v, root := newVault(t)
	release, err := acquire(ctx, filepath.Join(stateFor(root), "lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	short, cancel := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel()
	if _, err := v.Create(short, inbox("Title", "")); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
}

const helperEnv = "PARA_TEST_LOCK_ROOT"

// The test binary re-runs itself as a writer process.
func TestMain(m *testing.M) {
	if root := os.Getenv(helperEnv); root != "" {
		v, err := Open(root, Options{ID: "acme", Actor: "user", Surface: "test", Install: "m1", StateDir: stateFor(root)})
		if err == nil {
			for i := 0; i < 5 && err == nil; i++ {
				_, err = v.Create(ctx, inbox("From a process", ""))
			}
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestLockAcrossProcesses(t *testing.T) {
	const procs = 4
	v, root := newVault(t)
	cmds := make([]*exec.Cmd, procs)
	for i := range cmds {
		cmds[i] = exec.Command(os.Args[0])
		cmds[i].Env = append(os.Environ(), helperEnv+"="+root)
		cmds[i].Stderr = os.Stderr
		if err := cmds[i].Start(); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range cmds {
		if err := c.Wait(); err != nil {
			t.Fatal(err)
		}
	}
	if got := eventLog(t, v); len(got) != procs*5 {
		t.Fatalf("events = %d", len(got))
	}
	res, err := v.Reconcile(ctx)
	if err != nil || len(res.Changes) != 0 {
		t.Fatalf("reconcile = %+v, %v", res, err)
	}
}

func TestCheckFindsVaultWideProblems(t *testing.T) {
	v, root := newVault(t)
	if _, err := v.Create(ctx, model.Document{
		ID: "linker", Folder: model.FolderInbox, Title: "Linker", Body: "See [[missing-doc]].",
		Meta: &model.Inbox{Common: model.Common{Type: model.TypeInbox}},
	}); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(root, "archive", "linker.md"), []byte("not para-md"), 0o600)
	findings, err := v.Check(ctx)
	if err != nil {
		t.Fatal(err)
	}
	codes := map[string]bool{}
	for _, f := range findings {
		codes[f.Code] = true
		if filepath.IsAbs(f.Path) {
			t.Errorf("absolute path in finding: %s", f.Path)
		}
	}
	for _, want := range []string{paramd.CodeLinkBroken, paramd.CodeDupFilename, paramd.CodeFrontmatter} {
		if !codes[want] {
			t.Errorf("missing %s in %v", want, findings)
		}
	}
}

func TestLintPath(t *testing.T) {
	_, root := newVault(t)
	v := open(t, root)
	d, err := v.Create(ctx, inbox("Title", ""))
	if err != nil {
		t.Fatal(err)
	}
	plain := t.TempDir()
	os.WriteFile(filepath.Join(plain, "bad-file.md"), []byte("no frontmatter"), 0o600)
	cases := []struct {
		name    string
		path    string
		wantErr bool
		clean   bool
	}{
		{"vault dir skips templates", root, false, true},
		{"one file", filepath.Join(root, "inbox", d.ID+".md"), false, true},
		{"plain dir", plain, false, false},
		{"missing", filepath.Join(plain, "nope"), true, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := LintPath(c.path)
			if (err != nil) != c.wantErr {
				t.Fatalf("err = %v", err)
			}
			if !c.wantErr && (len(got) == 0) != c.clean {
				t.Fatalf("findings = %v", got)
			}
		})
	}
}

func TestList(t *testing.T) {
	v, _ := newVault(t)
	if _, err := v.Create(ctx, inbox("One", "")); err != nil {
		t.Fatal(err)
	}
	p := model.Document{
		ID: "acme-portal", Folder: model.FolderProjects, Title: "Portal",
		Meta: &model.Project{Common: model.Common{Type: model.TypeProject}, Status: model.ProjectActive, Owner: "Jordan"},
	}
	if _, err := v.Create(ctx, p); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		folder model.Folder
		typ    model.Type
		want   int
	}{
		{"all", "", "", 2},
		{"folder", model.FolderProjects, "", 1},
		{"type", "", model.TypeInbox, 1},
		{"none", model.FolderArchive, "", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := v.List(ctx, c.folder, c.typ)
			if err != nil || len(got) != c.want {
				t.Fatalf("got %v, %v", got, err)
			}
		})
	}
}

// Update touches only the lines that changed: comments and key order stay.
func TestUpdateKeepsCommentsAndKeyOrder(t *testing.T) {
	v, root := newVault(t)
	p := filepath.Join(root, "inbox", "hand-made.md")
	in := "---\n# mine\nnote: keep me # why\ncreated: 2026-11-01\ntype: inbox\n---\n\n# Hand\n\nText.\n"
	if err := os.WriteFile(p, []byte(in), 0o600); err != nil {
		t.Fatal(err)
	}
	d, err := v.Get(ctx, "hand-made")
	if err != nil {
		t.Fatal(err)
	}
	d.Body = "Edited."
	if _, err := v.Update(ctx, d); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(p)
	want := "---\n# mine\nnote: keep me # why\ncreated: 2026-11-01\ntype: inbox\nupdated: " +
		string(v.today()) + "\n---\n\n# Hand\n\nEdited.\n"
	if string(got) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}
