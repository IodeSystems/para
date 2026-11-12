package upgrade

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/iodesystems/para/docs"
	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
	"github.com/iodesystems/para/internal/vault"
)

// goalsFixture is a synthetic vault at format 10: finite projects (one
// archived, two deferred to someday, one with a hand-made id), their tasks,
// links to them from a note, an ask, and an obligation, a receipt that names
// one, and a project area.
func goalsFixture(t *testing.T) (*fixture, store.Store) {
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
	if err := s.SetConfig(ctx, ConfigKey, strconv.Itoa(GoalsFormat-1)); err != nil {
		t.Fatal(err)
	}
	proj := func(id string, st model.ProjectStatus, defer_, reason, done string) model.Document {
		return model.Document{ID: id, Folder: model.FolderProjects, Title: "Goal " + id,
			Meta: &model.Project{Common: model.Common{Type: model.TypeProject}, Status: st, Owner: "me", DoneWhen: done,
				Deferral: model.Deferral{DeferUntil: defer_, DeferredReason: reason}}}
	}
	for _, d := range []model.Document{
		proj("project-ship", model.ProjectActive, "", "", "It ships."),
		proj("project-later", model.ProjectActive, model.SomedayWord, "The vendor ships 2.0.", "It runs."),
		proj("project-idle", model.ProjectPaused, model.SomedayWord, "", "It runs."),
		proj("acme-portal", model.ProjectActive, "", "", ""),
		{ID: "task-a", Folder: model.FolderProjects, Title: "Write it", Body: "For [[project-ship]].",
			Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskTodo, Project: model.LinkTo("project-ship")}},
		{ID: "task-b", Folder: model.FolderProjects, Title: "Portal page",
			Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskTodo, Project: model.LinkTo("acme-portal")}},
		{ID: "note-a", Folder: model.FolderResources, Title: "Background",
			Meta: &model.Note{Common: model.Common{Type: model.TypeNote}, Project: model.LinkTo("project-ship")}},
		{ID: "ask-a", Folder: model.FolderRFX, Title: "Ask X", Meta: &model.RFX{Common: model.Common{Type: model.TypeRFX}, Direction: model.RFXOut,
			Status: model.RFXOpen, Route: "x", SelfServeCheck: "searched", Project: model.LinkTo("project-later")}},
		{ID: "owed-a", Folder: model.FolderObligations, Title: "Send the report", Meta: &model.Obligation{Common: model.Common{Type: model.TypeObligation},
			Kind: model.KindCommitment, State: model.StateOpen, Schedule: model.ScheduleOnce, Project: model.LinkTo("project-ship")}},
		{ID: "receipt-a", Folder: model.FolderReceipts, Title: "Receipt",
			Meta: &model.Receipt{Common: model.Common{Type: model.TypeReceipt}, Actor: "claude", Op: "task_add", Doc: "project-ship", Check: model.ReceiptUnchecked}},
		{ID: "area-ops", Folder: model.FolderAreas, Title: "Ops", Meta: &model.Area{Common: model.Common{Type: model.TypeArea}, Status: model.AreaActive}},
	} {
		if _, err := s.Create(ctx, d); err != nil {
			t.Fatalf("create %s: %v", d.ID, err)
		}
	}
	old := proj("project-old", model.ProjectDone, "", "", "Done.")
	old.Folder = model.FolderArchive
	if _, err := s.Create(ctx, old); err != nil {
		t.Fatal(err)
	}
	return f, s
}

func TestGoalsStep(t *testing.T) {
	f, s := goalsFixture(t)
	ctx := context.Background()

	dry, err := Run(ctx, s, true)
	if err != nil {
		t.Fatal(err)
	}
	so := stepOut(dry, "goals")
	want := map[string]int{CountGoals: 5, CountRePrefixed: 4, CountMoved: 6, CountRelinked: 5, CountKeysMoved: 5,
		CountParked: 2, CountParkedNoWhen: 1, CountRecordsLeft: 1}
	for k, n := range want {
		if so.Counts[k] != n {
			t.Errorf("dry %s = %d, want %d", k, so.Counts[k], n)
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s: would rewrite %d documents\n", so.ID, so.Count)
	for _, k := range CountOrder {
		fmt.Fprintf(&b, "  %s: %d\n", k, so.Counts[k])
	}
	for _, p := range so.Plan {
		fmt.Fprintf(&b, "  plan %s:\n", p.Doc)
		for _, ln := range p.Lines {
			fmt.Fprintf(&b, "    %s\n", ln)
		}
	}
	t.Logf("dry run:\n%s", b.String())
	if _, err := os.Stat(filepath.Join(f.root, "projects", "project-ship.md")); err != nil {
		t.Fatal("a dry run moved a file")
	}

	// Unattended (upgrade.vault_steps=auto): the owner's step does not run.
	un, err := RunWith(ctx, s, RunOptions{Unattended: true})
	if err != nil || stepOut(un, "goals").Skipped == "" || un.FormatVersion != GoalsFormat-1 {
		t.Fatalf("unattended = %+v, %v", un, err)
	}

	res, err := Run(ctx, s, false)
	if err != nil {
		t.Fatal(err)
	}
	so = stepOut(res, "goals")
	if res.FormatVersion != GoalsFormat || so.Batch == "" || so.Snapshot == "" {
		t.Fatalf("run = %+v", res)
	}
	if st, err := os.Stat(so.Snapshot); err != nil || st.Size() == 0 || strings.HasPrefix(so.Snapshot, f.root) {
		t.Fatalf("snapshot %s: %v", so.Snapshot, err)
	}
	get := func(id string) model.Document {
		t.Helper()
		d, err := s.Get(ctx, id)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		return d
	}
	g := get("project-ship") // the old id resolves
	if gm, ok := g.Meta.(*model.Goal); !ok || g.ID != "goal-ship" || g.Folder != model.FolderGoals || gm.FoldedFrom != model.TypeProject || !gm.WasID("project-ship") {
		t.Fatalf("goal-ship = %s %s %+v", g.ID, g.Folder, g.Meta)
	}
	if gm := get("goal-later").Meta.(*model.Goal); gm.Status != model.GoalParked || gm.ResumeWhen != "The vendor ships 2.0." || gm.DeferUntil != "" {
		t.Fatalf("goal-later = %+v", gm)
	}
	if gm := get("goal-idle").Meta.(*model.Goal); gm.Status != model.GoalParked || gm.ResumeWhen != "" {
		t.Fatalf("goal-idle = %+v", gm)
	}
	if d := get("acme-portal"); d.Folder != model.FolderGoals || d.Meta.Header().Type != model.TypeGoal {
		t.Fatalf("acme-portal = %s %+v", d.Folder, d.Meta)
	}
	if d := get("goal-old"); d.Folder != model.FolderArchive {
		t.Fatalf("goal-old in %s", d.Folder)
	}
	ta := get("task-a")
	if tm := ta.Meta.(*model.Task); ta.Folder != model.FolderGoals || tm.Goal != model.LinkTo("goal-ship") || tm.Project != "" || ta.Body != "For [[goal-ship]]." {
		t.Fatalf("task-a = %s %+v %q", ta.Folder, tm, ta.Body)
	}
	if tm := get("task-b").Meta.(*model.Task); tm.Goal != model.LinkTo("acme-portal") || tm.Project != "" {
		t.Fatalf("task-b = %+v", tm)
	}
	if n := get("note-a").Meta.(*model.Note); n.Goal != model.LinkTo("goal-ship") || n.Project != "" {
		t.Fatalf("note-a = %+v", n)
	}
	if r := get("ask-a").Meta.(*model.RFX); r.Goal != model.LinkTo("goal-later") || r.Project != "" {
		t.Fatalf("ask-a = %+v", r)
	}
	if o := get("owed-a").Meta.(*model.Obligation); o.Goal != model.LinkTo("goal-ship") || o.Project != "" {
		t.Fatalf("owed-a = %+v", o)
	}
	if r := get("receipt-a").Meta.(*model.Receipt); r.Doc != "project-ship" {
		t.Fatalf("a receipt changed: %+v", r)
	}
	for _, fd := range mustLint(t, s) {
		if fd.Severity == model.SeverityError {
			t.Errorf("lint after the step: %s", fd)
		}
	}
	// The judgement step lists the project area and the goal with no
	// done_when; it does not hold the format back.
	st, err := StatusOf(ctx, s)
	if err != nil || st.FormatVersion != GoalsFormat || len(st.Pending) != 1 || st.Pending[0].ID != "projects-sort" ||
		!slices.Equal(sorted(st.Pending[0].Docs), []string{"acme-portal", "area-ops"}) {
		t.Fatalf("status after = %+v, %v", st, err)
	}

	// A file another machine wrote before it saw the step syncs in late:
	// reconcile relinks it.
	late := filepath.Join(f.root, "projects", "task-late.md")
	if err := os.WriteFile(late, []byte("---\ntype: task\ncreated: 2026-11-03\nstatus: todo\nproject: \"[[project-ship]]\"\n---\n\n# Late\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.SetConfig(ctx, "reconcile.grace", "0s"); err != nil {
		t.Fatal(err)
	}
	rec, err := s.Reconcile(ctx)
	if err != nil || !slices.Equal(rec.Relinked, []string{"task-late"}) {
		t.Fatalf("reconcile = %+v, %v", rec, err)
	}
	if tm := get("task-late").Meta.(*model.Task); tm.Goal != model.LinkTo("goal-ship") || tm.Project != "" {
		t.Fatalf("task-late = %+v", tm)
	}
}

func mustLint(t *testing.T, s store.Store) []model.Finding {
	t.Helper()
	fs, err := s.Lint(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return fs
}
