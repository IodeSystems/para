package upgrade

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/docs"
	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
	"github.com/iodesystems/para/internal/store/local"
	"github.com/iodesystems/para/internal/vault"
)

type fixture struct {
	t    *testing.T
	root string
	home string
}

// newFixture makes a synthetic vault with two older meetings, a goal and a
// task without done_when, and a goal and a task with one.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	f := &fixture{t: t, root: filepath.Join(dir, "v"), home: filepath.Join(dir, "state")}
	tpl, err := fs.Sub(docs.Templates, "templates")
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Init(f.root, "acme", tpl, vault.InitOptions{NoGit: true}); err != nil {
		t.Fatal(err)
	}
	s := f.open("one")
	ctx := context.Background()
	for _, d := range []model.Document{
		{ID: "meeting-a", Folder: model.FolderMeetings, Title: "Launch sync", Body: "## Questions\n\n- [[rfx-x]] Open\n\n## Notes\n\nSome notes.\n",
			Meta: &model.Meeting{Common: model.Common{Type: model.TypeMeeting}, Status: model.MeetingScheduled,
				Start: "2026-11-04T10:00:00-05:00", End: "2026-11-04T10:30:00-05:00", With: []model.Link{model.LinkTo("person-x")}, RFX: []model.Link{model.LinkTo("rfx-x")}}},
		{ID: "meeting-b", Folder: model.FolderMeetings, Title: "Old review", Body: "## Notes\n\nHeld.\n",
			Meta: &model.Meeting{Common: model.Common{Type: model.TypeMeeting}, Status: model.MeetingHeld, Start: "2026-10-01T09:00:00Z", End: "2026-10-01T09:30:00Z", With: []model.Link{model.LinkTo("person-x")}}},
		{ID: "project-open", Folder: model.FolderProjects, Title: "Open goal",
			Meta: &model.Project{Common: model.Common{Type: model.TypeProject}, Status: model.ProjectActive, Owner: "me"}},
		{ID: "project-set", Folder: model.FolderProjects, Title: "Goal with a condition",
			Meta: &model.Project{Common: model.Common{Type: model.TypeProject}, Status: model.ProjectActive, Owner: "me", DoneWhen: "Shipped."}},
		{ID: "task-open", Folder: model.FolderProjects, Title: "Task without a condition",
			Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskTodo, Project: model.LinkTo("project-open")}},
	} {
		if _, err := s.Create(ctx, d); err != nil {
			t.Fatalf("create %s: %v", d.ID, err)
		}
	}
	return f
}

func (f *fixture) open(install string) store.Store {
	f.t.Helper()
	s, err := local.Open(f.root, vault.Options{ID: "acme", Actor: "user", Surface: "cli", Install: install,
		StateDir: filepath.Join(f.home, install), Now: func() time.Time { return time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC) }})
	if err != nil {
		f.t.Fatal(err)
	}
	return s
}

func raw(t *testing.T, f *fixture, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(f.root, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestMeetingStepDryRunRunAndUndo(t *testing.T) {
	f := newFixture(t)
	s := f.open("one")
	ctx := context.Background()
	before := raw(t, f, "meetings/meeting-a.md")

	st, err := StatusOf(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if st.FormatVersion != 0 || len(st.Pending) != 2 || st.Pending[0].ID != "meeting-as-task" || st.Pending[0].Count != 2 {
		t.Fatalf("status = %+v", st)
	}

	dry, err := Run(ctx, s, true)
	if err != nil {
		t.Fatal(err)
	}
	// proposal-dossier and io-folder also run, and find nothing in this vault.
	if len(dry.Steps) != 3 || dry.Steps[0].Count != 2 || dry.Steps[0].Batch != "" || dry.Steps[1].Count != 0 || dry.Steps[2].Count != 0 {
		t.Fatalf("dry run = %+v", dry)
	}
	if raw(t, f, "meetings/meeting-a.md") != before {
		t.Fatal("a dry run changed a file")
	}
	if v, _ := s.Config(ConfigKey); v != "" {
		t.Fatalf("a dry run set format_version=%s", v)
	}
	if ev, _ := s.Events(ctx, 50); hasOp(ev, vault.OpUpgrade) {
		t.Fatal("a dry run logged an event")
	}

	res, err := Run(ctx, s, false)
	if err != nil {
		t.Fatal(err)
	}
	batch := res.Steps[0].Batch
	if res.Steps[0].Count != 2 || batch == "" {
		t.Fatalf("run = %+v", res)
	}
	// Meeting-as-task is version 1; the done-when judgement step (2) is still pending.
	if res.FormatVersion != 1 {
		t.Fatalf("format_version = %d, want 1", res.FormatVersion)
	}
	d, err := s.Get(ctx, "meeting-a")
	if err != nil {
		t.Fatal(err)
	}
	task, ok := d.Meta.(*model.Task)
	if !ok || task.Kind != model.KindMeeting || task.Scheduled != "2026-11-04T10:00:00-05:00" || len(task.With) != 1 || len(task.RFX) != 1 {
		t.Fatalf("meeting-a = %#v", d.Meta)
	}
	if !strings.Contains(d.Body, "Some notes.") || !strings.Contains(d.Body, "[[rfx-x]]") {
		t.Fatalf("body lost: %q", d.Body)
	}
	b, _ := s.Get(ctx, "meeting-b")
	if bt := b.Meta.(*model.Task); bt.Status != model.TaskDone || bt.Outcome != model.OutcomeDone {
		t.Fatalf("meeting-b = %#v", bt)
	}
	ev, _ := s.Events(ctx, 50)
	n := 0
	for _, e := range ev {
		if e.Op == vault.OpUpgrade && e.From == batch {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("%d upgrade events, want one per document (2)", n)
	}

	// The batch is gone from the vault, so the old bytes live only in state.
	if strings.Contains(raw(t, f, "meetings/meeting-a.md"), "type: meeting") {
		t.Fatal("file still says type: meeting")
	}
	un, err := Undo(ctx, s, batch)
	if err != nil {
		t.Fatal(err)
	}
	if len(un.Docs) != 2 || len(un.Skipped) != 0 {
		t.Fatalf("undo = %+v", un)
	}
	if got := raw(t, f, "meetings/meeting-a.md"); got != before {
		t.Fatalf("undo did not restore the bytes:\n%s\nwant\n%s", got, before)
	}
	if v, _ := s.Config(ConfigKey); v != "0" {
		t.Fatalf("format_version after undo = %q, want 0", v)
	}
	if _, err := Undo(ctx, s, batch); err == nil {
		t.Fatal("a second undo of the same batch succeeded")
	}
}

func hasOp(ev []model.Event, op string) bool {
	for _, e := range ev {
		if e.Op == op {
			return true
		}
	}
	return false
}

func TestSecondRunAndSecondInstallDoNothing(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	one := f.open("one")
	if _, err := Run(ctx, one, false); err != nil {
		t.Fatal(err)
	}
	again, err := Run(ctx, one, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Steps) != 2 || again.Steps[0].ID != "proposal-dossier" || again.Steps[0].Count != 0 || again.Steps[1].Count != 0 {
		t.Fatalf("second run = %+v", again)
	}
	// A second machine sees the same vault.properties and the rewritten files.
	two := f.open("two")
	st, err := StatusOf(ctx, two)
	if err != nil {
		t.Fatal(err)
	}
	if st.FormatVersion != 1 || len(st.Pending) != 1 || st.Pending[0].ID != "done-when" {
		t.Fatalf("second install status = %+v", st)
	}
	r, err := Run(ctx, two, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Steps) != 2 || r.Steps[0].Count != 0 || r.Steps[1].Count != 0 {
		t.Fatalf("second install run = %+v", r)
	}
	// Without format_version (the property did not sync yet) the selector
	// still finds nothing to rewrite.
	if err := two.SetConfig(ctx, ConfigKey, "0"); err != nil {
		t.Fatal(err)
	}
	r, err = Run(ctx, two, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Steps) != 3 || r.Steps[0].Count != 0 || r.Steps[1].Count != 0 || r.Steps[2].Count != 0 || r.FormatVersion != 1 {
		t.Fatalf("run with a stale version = %+v", r)
	}
	if hasOp(mustEvents(t, two), vault.OpUpgradeUndo) {
		t.Fatal("unexpected undo event")
	}
}

func mustEvents(t *testing.T, s store.Store) []model.Event {
	t.Helper()
	ev, err := s.Events(context.Background(), 100)
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

func TestJudgementStepClearsWhenDoneTestFindsNothing(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s := f.open("one")
	if _, err := Run(ctx, s, false); err != nil {
		t.Fatal(err)
	}
	st, _ := StatusOf(ctx, s)
	if len(st.Pending) != 1 || st.Pending[0].Auto || st.Pending[0].Count != 2 {
		t.Fatalf("status = %+v", st)
	}
	// The selector skips a goal that has one, a closed task, and a meeting.
	got := map[string]bool{}
	for _, id := range st.Pending[0].Docs {
		got[id] = true
	}
	if !got["project-open"] || !got["task-open"] || got["project-set"] || got["meeting-a"] {
		t.Fatalf("selected %v", st.Pending[0].Docs)
	}
	for _, id := range []string{"project-open", "task-open"} {
		d, _ := s.Get(ctx, id)
		switch m := d.Meta.(type) {
		case *model.Project:
			m.DoneWhen = "Agreed by the owner."
		case *model.Task:
			m.DoneWhen = "The draft is sent."
		}
		if _, err := s.Update(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	v, err := Settle(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if v != Latest() {
		t.Fatalf("settled at %d, want %d", v, Latest())
	}
	st, _ = StatusOf(ctx, s)
	if len(st.Pending) != 0 || st.FormatVersion != Latest() {
		t.Fatalf("status after = %+v", st)
	}
}

func TestEditedDocumentIsSkippedByUndo(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s := f.open("one")
	res, _ := Run(ctx, s, false)
	d, _ := s.Get(ctx, "meeting-a")
	d.Title = "Renamed after the upgrade"
	if _, err := s.Update(ctx, d); err != nil {
		t.Fatal(err)
	}
	un, err := Undo(ctx, s, res.Steps[0].Batch)
	if err != nil {
		t.Fatal(err)
	}
	if len(un.Docs) != 1 || len(un.Skipped) != 1 || un.Skipped[0] != "meeting-a" {
		t.Fatalf("undo = %+v", un)
	}
}

func TestStepsAreOrderedAndNamed(t *testing.T) {
	prev := 0
	for _, s := range Steps() {
		if s.Version <= prev {
			t.Errorf("step %s version %d is not above %d", s.ID, s.Version, prev)
		}
		prev = s.Version
		if s.Select == nil || len(s.Types) == 0 {
			t.Errorf("step %s has no selector", s.ID)
		}
		if s.Auto && s.Rewrite == nil {
			t.Errorf("automated step %s has no rewrite", s.ID)
		}
		if !s.Auto && (s.Instructions == "" || s.Done == "") {
			t.Errorf("judgement step %s lacks instructions or a done test", s.ID)
		}
		if got, ok := stepOfBatch(BatchID(s.ID, time.Now())); !ok || got.ID != s.ID {
			t.Errorf("batch id of %s does not read back", s.ID)
		}
	}
	if Latest() != prev {
		t.Errorf("Latest = %d, want %d", Latest(), prev)
	}
}

func TestStatusListsEveryDocument(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s := f.open("one")
	if _, err := Run(ctx, s, false); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		id := "project-bulk-" + string(rune('a'+i/26)) + string(rune('a'+i%26))
		if _, err := s.Create(ctx, model.Document{ID: id, Folder: model.FolderProjects, Title: "Bulk " + id,
			Meta: &model.Project{Common: model.Common{Type: model.TypeProject}, Status: model.ProjectActive, Owner: "me"}}); err != nil {
			t.Fatal(err)
		}
	}
	st, err := StatusOf(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Pending) != 1 || st.Pending[0].Count < 31 || len(st.Pending[0].Docs) != st.Pending[0].Count {
		t.Fatalf("count %d, listed %d", st.Pending[0].Count, len(st.Pending[0].Docs))
	}
}
