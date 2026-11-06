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

// obligationFixture is a synthetic vault at format 6 with what step
// obligations folds and what it leaves.
func obligationFixture(t *testing.T) (*fixture, store.Store) {
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
	if err := s.SetConfig(ctx, ConfigKey, "6"); err != nil {
		t.Fatal(err)
	}
	task := func(st model.TaskStatus, k model.TaskKind) *model.Task {
		return &model.Task{Common: model.Common{Type: model.TypeTask}, Status: st, Kind: k, Project: model.LinkTo("project-x")}
	}
	sync := task(model.TaskTodo, model.KindMeeting)
	sync.Occasion = model.Occasion{Scheduled: "2026-11-04T10:00:00-05:00", End: "2026-11-04T10:30:00-05:00", With: []model.Link{model.LinkTo("person-x")},
		Optional: true, Attend: model.AttendYes, CalendarUIDs: []model.CalendarEvent{{UID: "sync@acme.example", Role: model.RolePrimary}}}
	owed := task(model.TaskDone, "")
	owed.Tags, owed.Due, owed.With = []string{model.CommitmentTag}, "2026-11-01", []model.Link{model.LinkTo("person-x")}
	first := task(model.TaskDone, model.KindMeeting)
	first.Recur, first.Scheduled, first.With = "weekly", "2026-10-28T09:00:00Z", []model.Link{model.LinkTo("person-x")}
	second := task(model.TaskTodo, model.KindMeeting)
	second.Recur, second.Scheduled, second.With, second.Previous = "weekly", "2026-11-04T09:00:00Z", []model.Link{model.LinkTo("person-x")}, model.LinkTo("weekly-a")
	third := task(model.TaskTodo, model.KindMeeting)
	third.Recur, third.Scheduled, third.With, third.Previous = "weekly", "2026-11-11T09:00:00Z", []model.Link{model.LinkTo("person-x")}, model.LinkTo("weekly-b")
	plain := task(model.TaskTodo, "")
	plain.DependsOn = []model.Link{model.LinkTo("asked-in")}
	for _, d := range []model.Document{
		{ID: "project-x", Folder: model.FolderProjects, Title: "Goal", Meta: &model.Project{Common: model.Common{Type: model.TypeProject}, Status: model.ProjectActive, Owner: "me", DoneWhen: "Done."}},
		{ID: "person-x", Folder: model.FolderOrg, Title: "Person X", Meta: &model.Person{Common: model.Common{Type: model.TypePerson}}},
		{ID: "sync", Folder: model.FolderProjects, Title: "Sync", Body: "## Notes\n\nSee [[plain]].", Meta: sync},
		{ID: "owed", Folder: model.FolderProjects, Title: "Send the report", Meta: owed},
		{ID: "weekly-a", Folder: model.FolderProjects, Title: "Weekly", Meta: first},
		{ID: "weekly-b", Folder: model.FolderProjects, Title: "Weekly", Meta: second},
		{ID: "weekly-c", Folder: model.FolderProjects, Title: "Weekly", Meta: third},
		{ID: "plain", Folder: model.FolderProjects, Title: "Plain task, waits on the ask", Body: "After [[sync]].", Meta: plain},
		{ID: "asked-in", Folder: model.FolderRFX, Title: "Review my doc", Meta: &model.RFX{Common: model.Common{Type: model.TypeRFX}, Direction: model.RFXIn,
			Status: model.RFXOpen, Route: "self", SelfServeCheck: "n/a", Targets: []model.Target{{To: model.LinkTo("person-x"), Medium: model.MediumEmail, State: model.TargetSent}}}},
		{ID: "asked-out", Folder: model.FolderRFX, Title: "Ask X", Meta: &model.RFX{Common: model.Common{Type: model.TypeRFX}, Direction: model.RFXOut,
			Status: model.RFXOpen, Route: "x", SelfServeCheck: "n/a"}},
	} {
		if _, err := s.Create(ctx, d); err != nil {
			t.Fatalf("create %s: %v", d.ID, err)
		}
	}
	// An unknown key a person added by hand stays through the fold.
	d, _ := s.Get(ctx, "sync")
	d, err = d.WithFields(map[string]any{"x_room": "4B"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(ctx, d); err != nil {
		t.Fatal(err)
	}
	return f, s
}

func TestObligationsStep(t *testing.T) {
	f, s := obligationFixture(t)
	ctx := context.Background()
	folded := []string{"asked-in", "owed", "sync", "weekly-a", "weekly-b", "weekly-c"}
	before := map[string]string{}
	for _, id := range folded {
		before[id] = rawAt(t, f, id)
	}

	dry, err := Run(ctx, s, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(dry.Steps) != 2 || dry.Steps[1].Count != 0 || dry.Steps[0].ID != "obligations" || strings.Join(dry.Steps[0].Docs, ",") != strings.Join(folded, ",") || dry.Steps[0].Batch != "" {
		t.Fatalf("dry run = %+v", dry)
	}
	for _, id := range folded {
		if rawAt(t, f, id) != before[id] {
			t.Fatalf("a dry run changed %s", id)
		}
	}

	res, err := Run(ctx, s, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.FormatVersion != Latest() || res.Steps[0].Count != len(folded) {
		t.Fatalf("run = %+v", res)
	}
	get := func(id string) (model.Document, *model.Obligation) {
		t.Helper()
		d, err := s.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		o, ok := d.Meta.(*model.Obligation)
		if !ok || d.Folder != model.FolderObligations {
			t.Fatalf("%s = %s %T", id, d.Folder, d.Meta)
		}
		return d, o
	}
	d, o := get("sync")
	if o.Kind != model.KindMeeting || o.State != model.StateOpen || o.Schedule != model.ScheduleAt || o.FoldedFrom != model.TypeTask ||
		o.Attend != model.AttendYes || !o.Optional || len(o.UIDs()) != 1 || o.Project != model.LinkTo("project-x") || !strings.Contains(d.Body, "[[plain]]") {
		t.Fatalf("sync = %+v", o)
	}
	text := raw(t, f, "obligations/sync.md")
	for _, k := range []string{"status: todo", "x_room: 4B", "folded_from: task"} {
		if !strings.Contains(text, k) {
			t.Fatalf("sync lost %q:\n%s", k, text)
		}
	}
	if _, o := get("owed"); o.Kind != model.KindCommitment || o.State != model.StateClosed || o.Outcome != model.OutcomeDone || o.Schedule != model.ScheduleOnce ||
		len(o.OwedTo) != 1 || o.Due != "2026-11-01" {
		t.Fatalf("owed = %+v", o)
	}
	if _, o := get("weekly-a"); o.Schedule != model.ScheduleSeries || o.OccurrenceOf != "" || o.State != model.StateClosed {
		t.Fatalf("weekly-a = %+v", o)
	}
	for _, id := range []string{"weekly-b", "weekly-c"} {
		if _, o := get(id); o.Schedule != model.ScheduleSeries || o.OccurrenceOf != model.LinkTo("weekly-a") || o.State != model.StateOpen {
			t.Fatalf("%s = %+v", id, o)
		}
	}
	if _, o := get("asked-in"); o.Kind != model.KindCommitment || o.FoldedFrom != model.TypeRFX || len(o.OwedTo) != 1 || o.OwedTo[0] != model.LinkTo("person-x") {
		t.Fatalf("asked-in = %+v", o)
	}
	if !strings.Contains(raw(t, f, "obligations/asked-in.md"), "direction: in") {
		t.Fatal("asked-in lost its rfx keys")
	}
	for _, id := range []string{"plain", "asked-out"} {
		if d, _ := s.Get(ctx, id); d.Meta.Header().Type == model.TypeObligation {
			t.Fatalf("%s was folded", id)
		}
	}
	// Links still resolve: lint finds no broken link, and the dependency reads.
	findings, err := s.Lint(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, fd := range findings {
		if fd.Severity == model.SeverityError {
			t.Fatalf("lint after the fold: %s", fd)
		}
	}

	// A re-run, and a second machine at the latest format, do nothing.
	again, err := Run(ctx, s, false)
	if err != nil || len(again.Steps) != 0 {
		t.Fatalf("re-run = %+v, %v", again, err)
	}
	two := f.open("two")
	if st, err := StatusOf(ctx, two); err != nil || st.FormatVersion != Latest() || len(st.Pending) != 0 {
		t.Fatalf("second machine = %+v, %v", st, err)
	}
	if r, err := Run(ctx, two, false); err != nil || len(r.Steps) != 0 {
		t.Fatalf("second machine run = %+v, %v", r, err)
	}

	// Undo puts every file back where it was, byte for byte.
	un, err := Undo(ctx, s, res.Steps[0].Batch)
	if err != nil {
		t.Fatal(err)
	}
	if len(un.Docs) != len(folded) || len(un.Skipped) != 0 {
		t.Fatalf("undo = %+v", un)
	}
	for _, id := range folded {
		if got := rawAt(t, f, id); got != before[id] {
			t.Fatalf("undo of %s:\n%s\nwant\n%s", id, got, before[id])
		}
	}
	if v, _ := s.Config(ConfigKey); v != "6" {
		t.Fatalf("format after undo = %q", v)
	}
}

// rawAt reads a fixture document from the folder it is in now.
func rawAt(t *testing.T, f *fixture, id string) string {
	t.Helper()
	for _, dir := range []string{"projects", "rfx", "obligations"} {
		if text, err := readIf(filepath.Join(f.root, dir, id+".md")); err == nil {
			return dir + "\n" + text
		}
	}
	t.Fatalf("%s not found", id)
	return ""
}

func TestObligationsStepRefusesAnOlderPara(t *testing.T) {
	f, s := obligationFixture(t)
	ctx := context.Background()
	// Install old writes to the vault with a para that logs no session.
	old, err := local.Open(f.root, vault.Options{ID: "acme", Actor: "user", Surface: "cli", Install: "old",
		StateDir: filepath.Join(f.home, "old"), Now: func() time.Time { return time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	if err := old.Record(ctx, "receipt", "plain", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, s, false); err == nil || !strings.Contains(err.Error(), "install old") || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("run with an older para seen = %v", err)
	}
	if d, _ := s.Get(ctx, "sync"); d.Meta.Header().Type != model.TypeTask {
		t.Fatal("a refused run changed a file")
	}
	// A dry run lists it and goes on.
	dry, err := Run(ctx, s, true)
	if err != nil || len(dry.Stale) != 1 || dry.Stale[0].Install != "old" || dry.Stale[0].Format != 0 {
		t.Fatalf("dry = %+v, %v", dry, err)
	}
	// A session of format 6 is still older.
	if err := old.Record(ctx, OpSession, "", "v0.2.0", "6", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, s, false); err == nil || !strings.Contains(err.Error(), "format 6") {
		t.Fatalf("run with format 6 seen = %v", err)
	}
	// Once that machine runs this para, the run goes ahead.
	if err := RecordSession(ctx, old, "", "v0.3.0"); err != nil {
		t.Fatal(err)
	}
	if res, err := Run(ctx, s, false); err != nil || res.FormatVersion != Latest() {
		t.Fatalf("run after the upgrade = %+v, %v", res, err)
	}
}

func TestObligationsStepForced(t *testing.T) {
	f, s := obligationFixture(t)
	ctx := context.Background()
	old, err := local.Open(f.root, vault.Options{ID: "acme", Actor: "user", Surface: "cli", Install: "old", StateDir: filepath.Join(f.home, "old"),
		Now: func() time.Time { return time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	if err := old.Record(ctx, "receipt", "plain", "", "", ""); err != nil {
		t.Fatal(err)
	}
	// An install last seen before StaleDays does not hold a run back.
	ancient, err := local.Open(f.root, vault.Options{ID: "acme", Actor: "user", Surface: "cli", Install: "ancient", StateDir: filepath.Join(f.home, "ancient"),
		Now: func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	if err := ancient.Record(ctx, "receipt", "plain", "", "", ""); err != nil {
		t.Fatal(err)
	}
	res, err := RunForced(ctx, s, false)
	if err != nil || res.FormatVersion != Latest() || len(res.Stale) != 1 || res.Stale[0].Install != "old" {
		t.Fatalf("forced = %+v, %v", res, err)
	}
}

func readIf(p string) (string, error) {
	b, err := os.ReadFile(p)
	return string(b), err
}
