package upgrade

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/vault"
)

// The io-folder step moves each capture in inbox/ to io/ as an unread incoming
// io item with the same id, so a link to it still resolves. A capture triaged
// before io stays in archive/. Undo puts the files back.
func TestIOFolderStep(t *testing.T) {
	f := newFixture(t)
	s := f.open("one")
	ctx := context.Background()
	for _, d := range []model.Document{
		{ID: "inbox-a", Folder: model.FolderInbox, Title: "Call the plumber", Body: "about the leak",
			Meta: &model.Inbox{Common: model.Common{Type: model.TypeInbox}}},
		{ID: "inbox-long", Folder: model.FolderInbox, Title: strings.Repeat("w", 80) + "...", Body: strings.Repeat("w", 90),
			Meta: &model.Inbox{Common: model.Common{Type: model.TypeInbox}}},
		{ID: "inbox-old", Folder: model.FolderArchive, Title: "Old", Body: "Triaged: drop.",
			Meta: &model.Inbox{Common: model.Common{Type: model.TypeInbox}}},
		{ID: "task-links", Folder: model.FolderProjects, Title: "Linked", Body: "See [[inbox-a]].",
			Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskTodo, Goal: model.LinkTo("project-open")}},
	} {
		if _, err := s.Create(ctx, d); err != nil {
			t.Fatalf("create %s: %v", d.ID, err)
		}
	}
	before := raw(t, f, "inbox/inbox-a.md")
	st, err := StatusOf(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	var pend *Pending
	for i := range st.Pending {
		if st.Pending[i].ID == "io-folder" {
			pend = &st.Pending[i]
		}
	}
	if pend == nil || pend.Count != 2 {
		t.Fatalf("status = %+v", st)
	}
	res, err := Run(ctx, s, false)
	if err != nil {
		t.Fatal(err)
	}
	var batch string
	for _, so := range res.Steps {
		if so.ID == "io-folder" {
			batch = so.Batch
			if so.Count != 2 {
				t.Fatalf("io-folder = %+v", so)
			}
		}
	}
	if _, err := os.Stat(filepath.Join(f.root, "inbox", "inbox-a.md")); !os.IsNotExist(err) {
		t.Fatalf("inbox/inbox-a.md still there: %v", err)
	}
	d, err := s.Get(ctx, "inbox-a")
	if err != nil {
		t.Fatal(err)
	}
	m, ok := d.Meta.(*model.IO)
	if !ok || d.Folder != model.FolderIO || m.Direction != model.IOIn || m.State != model.IOUnread || m.Medium != model.MediumOther {
		t.Fatalf("inbox-a = %+v %#v", d, d.Meta)
	}
	if d.Title != "Call the plumber" || d.Body != "Call the plumber\n\nabout the leak" {
		t.Fatalf("text = %q / %q", d.Title, d.Body)
	}
	if l, _ := s.Get(ctx, "inbox-long"); l.Body != strings.Repeat("w", 90) {
		t.Fatalf("long body = %q", l.Body)
	}
	if o, _ := s.Get(ctx, "inbox-old"); o.Folder != model.FolderArchive || o.Meta.Header().Type != model.TypeInbox {
		t.Fatalf("archived capture changed: %+v", o)
	}
	findings, err := s.Lint(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, fd := range findings {
		if strings.Contains(fd.Path, "inbox-") || strings.Contains(fd.Path, "task-links") {
			t.Errorf("lint: %+v", fd)
		}
	}
	ev, _ := s.Events(ctx, 100)
	n := 0
	for _, e := range ev {
		if e.Op == vault.OpUpgrade && e.From == batch && e.To == string(model.FolderIO) {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("%d upgrade events to io, want 2", n)
	}
	if st, _ := StatusOf(ctx, s); len(st.Pending) > 0 && st.Pending[len(st.Pending)-1].ID == "io-folder" {
		t.Fatalf("io-folder still pending: %+v", st)
	}

	un, err := Undo(ctx, s, batch)
	if err != nil || len(un.Docs) != 2 {
		t.Fatalf("undo = %+v %v", un, err)
	}
	if got := raw(t, f, "inbox/inbox-a.md"); got != before {
		t.Fatalf("undo did not restore the bytes:\n%s\nwant\n%s", got, before)
	}
	if _, err := os.Stat(filepath.Join(f.root, "io", "inbox-a.md")); !os.IsNotExist(err) {
		t.Fatalf("io/inbox-a.md still there after undo: %v", err)
	}
	if d, _ := s.Get(ctx, "inbox-a"); d.Folder != model.FolderInbox {
		t.Fatalf("after undo inbox-a is in %s", d.Folder)
	}
}
