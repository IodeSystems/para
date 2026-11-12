package ops

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/paramd"
	"github.com/iodesystems/para/internal/store"
)

// cronVault is a tick vault at format 11 whose runner is ia, tick queueing.
func cronVault(t *testing.T, now string) (*tickVault, Deps) {
	t.Helper()
	old := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = old })
	tv, d := journalTick(t, now, "request.surfaces=cli,tick")
	format11(t, d)
	return tv, d
}

// cronTaskOf is the task that links the cron occurrence occ, or nil.
func cronTaskOf(t *testing.T, d Deps, occ string) (model.Document, *model.Task) {
	t.Helper()
	refs, err := d.Store.List(ctx, store.Filter{Type: model.TypeTask, All: true})
	if err != nil {
		t.Fatal(err)
	}
	var found []model.Document
	for _, r := range refs {
		doc, _ := d.Store.Get(ctx, r.ID)
		if tk, ok := doc.Meta.(*model.Task); ok && tk.Cron.Target() == occ {
			found = append(found, doc)
		}
	}
	switch len(found) {
	case 0:
		return model.Document{}, nil
	case 1:
		return found[0], found[0].Meta.(*model.Task)
	}
	t.Fatalf("%d tasks for %s", len(found), occ)
	return model.Document{}, nil
}

// lintErrs are the lint errors of the vault file of id.
func lintErrs(t *testing.T, root, id string, edit func(fm map[string]any)) []paramd.Finding {
	t.Helper()
	var path string
	_ = filepath.WalkDir(root, func(p string, _ fs.DirEntry, _ error) error {
		if filepath.Base(p) == id+".md" {
			path = p
		}
		return nil
	})
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rel, _ := filepath.Rel(root, path)
	f := paramd.Parse(rel, data)
	if edit != nil {
		edit(f.Frontmatter)
	}
	var out []paramd.Finding
	for _, x := range paramd.Lint(f) {
		if x.Severity == paramd.Error {
			out = append(out, x)
		}
	}
	return out
}

func addOut(t *testing.T, d Deps, in string) string {
	t.Helper()
	return invoke(t, Add, d, in).(AddOutput).ID
}

// A cron that makes a task under a goal: tick makes it on the day, once,
// closes the occurrence, and the next occurrence waits for its day. The
// owner's task is in Next; the occurrence never is.
func TestCronMakesTaskUnderGoal(t *testing.T) {
	tv, d := cronVault(t, "2026-11-06T09:00:00Z")
	p := must(t, ProjectAdd, d, ProjectAddInput{Title: "Garden"})
	g := must(t, GoalAdd, d, GoalAddInput{Title: "Beds", DoneWhen: "Beds planted.", Project: p.ID})
	other := must(t, ProjectAdd, d, ProjectAddInput{Title: "Car"})
	if _, err := Add.Invoke(writeCtx(t), d, json.RawMessage(`{"type":"obligation","title":"Water","fields":{"project":"`+other.ID+`","recur":"weekly","makes":"task","goal":"`+g.ID+`"}}`)); err == nil || !strings.Contains(err.Error(), "not part of project") {
		t.Fatalf("goal of another project: %v", err)
	}
	if _, err := Add.Invoke(writeCtx(t), d, json.RawMessage(`{"type":"obligation","title":"Water","fields":{"makes":"task","recur":"weekly"}}`)); err == nil || !strings.Contains(err.Error(), "on a project") {
		t.Fatalf("makes with no project: %v", err)
	}
	cron := addOut(t, d, `{"type":"obligation","title":"Water the beds","fields":{"project":"`+p.ID+`","recur":"weekly","due":"2026-11-06","makes":"task","goal":"`+g.ID+`","done_when":"Beds are wet."}}`)
	_, o := getOb(t, d, cron)
	if o.Makes != model.MakesTask || o.Project.Target() != p.ID || o.Goal.Target() != g.ID || o.Schedule != model.ScheduleSeries {
		t.Fatalf("stored = %+v", o)
	}
	sn, err := NewSnapshot(ctx, d.Store)
	if err != nil {
		t.Fatal(err)
	}
	if n := sn.Need(cron); n.Yes {
		t.Fatalf("occurrence needs you: %+v", n)
	}

	out := tick(t, d)
	if len(out.Crons) != 1 || !strings.HasPrefix(out.Crons[0], cron+": ") {
		t.Fatalf("tick crons = %v warnings %v", out.Crons, out.Warnings)
	}
	td, tk := cronTaskOf(t, d, cron)
	if tk == nil || tk.Goal.Target() != g.ID || tk.Project != "" || tk.Due != "2026-11-06" || tk.DoneWhen != "Beds are wet." || tk.Assignee != "" {
		t.Fatalf("task = %+v", tk)
	}
	if res := lintErrs(t, tv.root, td.ID, nil); len(res) > 0 {
		t.Fatalf("lint task: %v", res)
	}
	_, o = getOb(t, d, cron)
	if o.Open() || o.Outcome != model.OutcomeDone {
		t.Fatalf("occurrence = %s %s", o.State, o.Outcome)
	}
	next, err := successor(ctx, d, cron)
	if err != nil || next == "" {
		t.Fatalf("next occurrence: %q %v", next, err)
	}
	_, no := getOb(t, d, next)
	if no.Due != "2026-11-13" || no.Makes != model.MakesTask || no.Project.Target() != p.ID || no.OccurrenceOf.Target() != cron {
		t.Fatalf("next = %+v", no)
	}
	rows := must(t, OrderOp, d, OrderInput{}).Rows
	if !slices.ContainsFunc(rows, func(r ValueRow) bool { return r.ID == td.ID }) || slices.ContainsFunc(rows, func(r ValueRow) bool { return r.ID == next }) {
		t.Fatalf("next view = %+v", rows)
	}

	// A second pass the same day makes nothing; on the next day, nothing.
	if out := tick(t, d); len(out.Crons) != 0 {
		t.Fatalf("second pass = %v", out.Crons)
	}
	tv.at("2026-11-07T09:00:00Z")
	if out := tick(t, d); len(out.Crons) != 0 {
		t.Fatalf("next day = %v", out.Crons)
	}
	// The next run comes due; a pass whose close failed after the task was
	// made (crash window) does not make a second task.
	tv.at("2026-11-13T09:00:00Z")
	nd, nob := getOb(t, d, next)
	if _, _, err := cronTask(ctx, Deps{Store: d.Store.ActAs(TickSurface, TickSurface)}, nd, nob); err != nil {
		t.Fatal(err)
	}
	if out := tick(t, d); len(out.Crons) != 1 {
		t.Fatalf("next run = %v %v", out.Crons, out.Warnings)
	}
	if _, nt := cronTaskOf(t, d, next); nt == nil || nt.Due != "2026-11-13" {
		t.Fatalf("second task = %+v", nt)
	}

	sh := must(t, ProjectShow, d, ProjectShowInput{ID: p.ID})
	if len(sh.Crons) != 1 || !strings.Contains(sh.Crons[0], "makes task under \"") || !strings.Contains(sh.Crons[0], "("+g.ID+")") || !strings.Contains(sh.Crons[0], "next 2026-11-20") || !strings.Contains(sh.Crons[0], "last 2026-11-13 done") {
		t.Fatalf("bundle crons = %v", sh.Crons)
	}
	if slices.ContainsFunc(sh.Direct, func(it BundleItem) bool { return it.Type == model.TypeObligation }) {
		t.Fatalf("cron in direct = %+v", sh.Direct)
	}
}

// A cron that makes a task linked to the project, owed by an agent: the task
// carries cron: and project:, is the agent's, and has agent work. Edit sets
// project: on it; on any other task it is refused, and lint flags it.
func TestCronMakesDirectAgentTask(t *testing.T) {
	tv, d := cronVault(t, "2026-11-06T09:00:00Z")
	p := must(t, ProjectAdd, d, ProjectAddInput{Title: "Garden"})
	p2 := must(t, ProjectAdd, d, ProjectAddInput{Title: "Shed"})
	if _, err := Add.Invoke(writeCtx(t), d, json.RawMessage(`{"type":"obligation","title":"Check","fields":{"project":"`+p.ID+`","recur":"daily","makes":"task","owed_by":["agent"]}}`)); err == nil || !strings.Contains(err.Error(), "agent:<name>") {
		t.Fatalf("bare agent: %v", err)
	}
	cron := addOut(t, d, `{"type":"obligation","title":"Check the rain gauge","fields":{"project":"`+p.ID+`","recur":"daily","due":"2026-11-06","makes":"task","owed_by":["agent:gardener"]}}`)

	out := tick(t, d)
	td, tk := cronTaskOf(t, d, cron)
	if tk == nil || tk.Project.Target() != p.ID || tk.Goal != "" || tk.Assignee != "agent:gardener" {
		t.Fatalf("task = %+v (tick %+v)", tk, out)
	}
	if res := lintErrs(t, tv.root, td.ID, nil); len(res) > 0 {
		t.Fatalf("lint cron task: %v", res)
	}
	if len(out.Requests) != 1 {
		t.Fatalf("requests = %v warnings %v", out.Requests, out.Warnings)
	}
	_, req, err := getRequest(ctx, d.Store, out.Requests[0])
	if err != nil || req.Status != model.RequestQueued || !slices.ContainsFunc(req.Links, func(l model.Link) bool { return l.Target() == td.ID }) {
		t.Fatalf("request = %+v %v", req, err)
	}
	if so := invoke(t, Show, d, `{"id":"`+td.ID+`"}`).(ShowOutput); so.NeedsYou || so.Turn != TurnAgent {
		t.Fatalf("agent task = %+v", so)
	}
	sh := must(t, ProjectShow, d, ProjectShowInput{ID: p.ID})
	if !slices.ContainsFunc(sh.Direct, func(it BundleItem) bool { return it.ID == td.ID }) || len(sh.Crons) != 1 || !strings.Contains(sh.Crons[0], "agent:gardener") {
		t.Fatalf("bundle = direct %+v crons %v", sh.Direct, sh.Crons)
	}

	// project: on the cron task may change; on another task it is refused.
	invoke(t, Edit, d, `{"id":"`+td.ID+`","fields":{"project":"`+p2.ID+`"}}`)
	g := must(t, GoalAdd, d, GoalAddInput{Title: "Beds", DoneWhen: "Beds planted.", Project: p.ID})
	plain := must(t, TaskAdd, d, TaskAddInput{Goal: g.ID, Title: "Rake", DoneWhen: "Raked."})
	if _, err := Edit.Invoke(writeCtx(t), d, json.RawMessage(`{"id":"`+plain.ID+`","fields":{"project":"`+p.ID+`"}}`)); err == nil || !strings.Contains(err.Error(), "through a goal") {
		t.Fatalf("plain task project: %v", err)
	}
	if _, err := Edit.Invoke(writeCtx(t), d, json.RawMessage(`{"id":"`+plain.ID+`","fields":{"cron":"`+cron+`"}}`)); err == nil {
		t.Fatal("cron set by edit")
	}
	if res := lintErrs(t, tv.root, plain.ID, func(fm map[string]any) { fm["project"] = "[[" + p.ID + "]]" }); len(res) == 0 {
		t.Fatal("lint: project on a task with no cron")
	}
}

// A cron that makes obligations (the default) on a project: each occurrence
// keeps the project, and the owner's occurrence needs the owner.
func TestCronMakesObligation(t *testing.T) {
	_, d := cronVault(t, "2026-11-06T09:00:00Z")
	p := must(t, ProjectAdd, d, ProjectAddInput{Title: "Garden"})
	cron := addOut(t, d, `{"type":"obligation","title":"Pay the allotment fee","fields":{"project":"`+p.ID+`","recur":"monthly","due":"2026-11-06"}}`)
	sn, err := NewSnapshot(ctx, d.Store)
	if err != nil {
		t.Fatal(err)
	}
	if n := sn.Need(cron); !n.Yes {
		t.Fatalf("owner occurrence = %+v", n)
	}
	invoke(t, Close, d, `{"id":"`+cron+`","outcome":"done"}`)
	next, err := successor(ctx, d, cron)
	if err != nil || next == "" {
		t.Fatal("no next occurrence")
	}
	if _, no := getOb(t, d, next); no.Project.Target() != p.ID || no.Due != "2026-12-06" {
		t.Fatalf("next = %+v", no)
	}
	sh := must(t, ProjectShow, d, ProjectShowInput{ID: p.ID})
	if len(sh.Crons) != 1 || !strings.Contains(sh.Crons[0], "makes obligation") || !strings.Contains(sh.Crons[0], "next 2026-12-06") {
		t.Fatalf("crons = %v", sh.Crons)
	}
	if bundleSize(&sh) > projectBundleMax {
		t.Fatalf("bundle %d bytes", bundleSize(&sh))
	}
}

// Lint: makes is a cron's (a series on a project); a task with project:
// passes only with cron:.
func TestCronLint(t *testing.T) {
	errs := func(src string) []string {
		var out []string
		for _, f := range paramd.Lint(paramd.Parse("obligations/x.md", []byte(src))) {
			if f.Severity == paramd.Error {
				out = append(out, f.Msg)
			}
		}
		return out
	}
	if e := errs("---\ntype: obligation\nkind: commitment\nstate: open\nschedule: once\ncreated: 2026-11-01\nmakes: task\n---\n# A\n"); !slices.ContainsFunc(e, func(m string) bool { return strings.Contains(m, "a cron is a series on a project") }) {
		t.Fatalf("makes on a once obligation: %v", e)
	}
	if e := errs("---\ntype: obligation\nkind: commitment\nstate: open\nschedule: series\nrecur: weekly\nproject: \"[[project-a]]\"\ncreated: 2026-11-01\nmakes: task\n---\n# A\n"); len(e) > 0 {
		t.Fatalf("cron: %v", e)
	}
	if e := errs("---\ntype: task\nstatus: todo\ncreated: 2026-11-01\nproject: \"[[project-a]]\"\ncron: \"[[obligation-a]]\"\n---\n# A\n"); len(e) > 0 {
		t.Fatalf("cron task: %v", e)
	}
}

// A cron whose goal is parked skips its runs: tick makes nothing, the
// bundle line says paused, and the open occurrence moves to its next date.
// On unpark the runs resume from the next due date, with no backlog.
func TestCronPausedWhileGoalParked(t *testing.T) {
	tv, d := cronVault(t, "2026-11-06T09:00:00Z")
	p := must(t, ProjectAdd, d, ProjectAddInput{Title: "Garden"})
	g := must(t, GoalAdd, d, GoalAddInput{Title: "Beds", DoneWhen: "Beds planted.", Project: p.ID})
	cron := addOut(t, d, `{"type":"obligation","title":"Water the beds","fields":{"project":"`+p.ID+`","recur":"weekly","due":"2026-11-06","makes":"task","goal":"`+g.ID+`"}}`)
	invoke(t, Edit, d, `{"id":"`+g.ID+`","fields":{"status":"parked","resume_when":"after the frost"}}`)

	sh := must(t, ProjectShow, d, ProjectShowInput{ID: p.ID})
	if len(sh.Crons) != 1 || !strings.Contains(sh.Crons[0], "paused: goal parked") || strings.Contains(sh.Crons[0], "; next ") {
		t.Fatalf("bundle crons = %v", sh.Crons)
	}
	// Three weeks parked: each due run is skipped, no task, no backlog.
	for _, at := range []string{"2026-11-06", "2026-11-13", "2026-11-20", "2026-11-27"} {
		tv.at(at + "T09:00:00Z")
		out := tick(t, d)
		if len(out.Crons) != 0 || len(out.CronsPaused) != 1 || !strings.HasPrefix(out.CronsPaused[0], cron+": goal parked") {
			t.Fatalf("%s: crons %v paused %v warnings %v", at, out.Crons, out.CronsPaused, out.Warnings)
		}
		if _, tk := cronTaskOf(t, d, cron); tk != nil {
			t.Fatalf("%s: task made while parked: %+v", at, tk)
		}
	}
	_, o := getOb(t, d, cron)
	if !o.Open() || o.Due != "2026-12-04" {
		t.Fatalf("occurrence = open %v due %s", o.Open(), o.Due)
	}
	// Unparked mid-week: nothing fires until the next due date, then one run.
	tv.at("2026-11-30T09:00:00Z")
	invoke(t, Edit, d, `{"id":"`+g.ID+`","fields":{"status":"active"}}`)
	if out := tick(t, d); len(out.Crons) != 0 || len(out.CronsPaused) != 0 {
		t.Fatalf("after unpark: crons %v paused %v", out.Crons, out.CronsPaused)
	}
	sh = must(t, ProjectShow, d, ProjectShowInput{ID: p.ID})
	if len(sh.Crons) != 1 || !strings.Contains(sh.Crons[0], "next 2026-12-04") {
		t.Fatalf("bundle crons after unpark = %v", sh.Crons)
	}
	tv.at("2026-12-04T09:00:00Z")
	if out := tick(t, d); len(out.Crons) != 1 {
		t.Fatalf("resumed run = %v %v", out.Crons, out.Warnings)
	}
	if _, tk := cronTaskOf(t, d, cron); tk == nil || tk.Goal.Target() != g.ID || tk.Due != "2026-12-04" {
		t.Fatalf("resumed task = %+v", tk)
	}
}

// A bundle names the goal and task beside their ids, and list {type: goal}
// carries the project, open task count, and next task.
func TestBundleTitlesAndGoalList(t *testing.T) {
	_, d := cronVault(t, "2026-11-06T09:00:00Z")
	p := must(t, ProjectAdd, d, ProjectAddInput{Title: "Garden"})
	g := must(t, GoalAdd, d, GoalAddInput{Title: "Beds", DoneWhen: "Beds planted.", Project: p.ID})
	tk := must(t, TaskAdd, d, TaskAddInput{Goal: g.ID, Title: "Rake", DoneWhen: "Raked."})
	sh := must(t, ProjectShow, d, ProjectShowInput{ID: p.ID})
	if len(sh.Goals) != 1 || sh.Goals[0].Next != tk.ID || sh.Goals[0].NextTitle != "Rake" || sh.Goals[0].Tasks[0].ViaTitle != "Beds" {
		t.Fatalf("bundle goals = %+v", sh.Goals)
	}
	ls := must(t, List, d, ListInput{Type: model.TypeGoal, Status: "active"})
	if len(ls.Rows) != 1 || ls.Rows[0].Goal == nil {
		t.Fatalf("goal rows = %+v", ls.Rows)
	}
	if r := ls.Rows[0].Goal; r.Project != p.ID || r.ProjectTitle != "Garden" || r.OpenTasks != 1 || r.Next != tk.ID || r.NextTitle != "Rake" {
		t.Fatalf("goal row = %+v", r)
	}
}
