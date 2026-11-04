package ops

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/docs"
	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store/local"
	"github.com/iodesystems/para/internal/vault"
)

// tickVault is a vault on disk that two installs can open.
type tickVault struct {
	t    *testing.T
	root string
	now  time.Time
}

func newTickVault(t *testing.T, now string, props ...string) *tickVault {
	t.Helper()
	tv := &tickVault{t: t, root: filepath.Join(t.TempDir(), "v"), now: utc(now)}
	tpl, err := fs.Sub(docs.Templates, "templates")
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Init(tv.root, "acme", tpl, vault.InitOptions{NoGit: true}); err != nil {
		t.Fatal(err)
	}
	noGate(t, tv.root)
	if len(props) > 0 {
		tv.addProps(props...)
	}
	return tv
}

func (tv *tickVault) addProps(lines ...string) {
	tv.t.Helper()
	p := filepath.Join(tv.root, ".para", "vault.properties")
	cur, err := os.ReadFile(p)
	if err != nil {
		tv.t.Fatal(err)
	}
	if err := os.WriteFile(p, append(cur, []byte(strings.Join(lines, "\n")+"\n")...), 0o600); err != nil {
		tv.t.Fatal(err)
	}
}

// open opens the vault as an install, on the surface given.
func (tv *tickVault) open(install, surface string) Deps {
	tv.t.Helper()
	s, err := local.Open(tv.root, vault.Options{ID: "acme", Actor: "user", Surface: surface, Install: install,
		StateDir: filepath.Join(filepath.Dir(tv.root), "state-"+install), Now: func() time.Time { return tv.now }})
	if err != nil {
		tv.t.Fatal(err)
	}
	return Deps{Store: s}
}

func tick(t *testing.T, d Deps) TickOutput {
	t.Helper()
	out, err := Tick(ctx, d, TickInput{})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func eventCount(t *testing.T, d Deps) int {
	t.Helper()
	evs, err := d.Store.Events(ctx, 100000)
	if err != nil {
		t.Fatal(err)
	}
	return len(evs)
}

// markDone sets a task done the way a hand edit would: no op runs, so no next
// occurrence is made.
func markDone(t *testing.T, d Deps, id string) {
	t.Helper()
	doc, tm, err := getTask(ctx, d.Store, id)
	if err != nil {
		t.Fatal(err)
	}
	tm.Status = model.TaskDone
	if _, err := d.Store.Update(ctx, doc); err != nil {
		t.Fatal(err)
	}
}

func TestTickRunnerGate(t *testing.T) {
	tv := newTickVault(t, "2026-11-02T09:00:00Z")
	a, b := tv.open("ia", "cli"), tv.open("ib", "cli")
	proj := must(t, ProjectAdd, a, ProjectAddInput{DoneWhen: "Done.", Title: "Portal"}).ID
	task := must(t, TaskAdd, a, TaskAddInput{Project: proj, Title: "Rotate disks", Due: "2026-10-01", Recur: "monthly"}).ID
	markDone(t, a, task)

	// No runner named: refuse, say what to add, and change nothing.
	before := eventCount(t, a)
	out := tick(t, a)
	if out.Ran || !out.NoRunner || out.Failed() == nil || !strings.Contains(out.Skipped, "para vault config set request.runner ia") {
		t.Fatalf("no runner: %+v", out)
	}
	if got := eventCount(t, a); got != before {
		t.Errorf("a refused tick wrote %d events", got-before)
	}

	// A synced vault: the property is in the shared file, both machines see it.
	tv.addProps("request.runner=ia")
	other := tick(t, b)
	if other.Ran || other.NoRunner || other.Failed() != nil || !strings.Contains(other.Skipped, "not the runner (ia)") {
		t.Fatalf("other install: %+v", other)
	}
	tasks, _ := listTasks(ctx, b.Store, "", "")
	if len(tasks) != 1 {
		t.Fatalf("a non-runner tick made tasks: %d", len(tasks))
	}
	ran := tick(t, a)
	if !ran.Ran || len(ran.Revived) != 1 {
		t.Fatalf("runner: %+v", ran)
	}
	// The other machine now sees the successor, so even a wrong tick has nothing to repeat.
	if again := tick(t, b); again.Ran {
		t.Errorf("the other install ran: %+v", again)
	}
	if tasks, _ := listTasks(ctx, a.Store, "", ""); len(tasks) != 2 {
		t.Errorf("tasks = %d, want 2", len(tasks))
	}
}

func TestTickRevivesRecurringDoneByHand(t *testing.T) {
	tv := newTickVault(t, "2026-11-02T09:00:00Z", "request.runner=ia")
	d := tv.open("ia", "cli")
	proj := must(t, ProjectAdd, d, ProjectAddInput{DoneWhen: "Done.", Title: "Portal"}).ID
	byHand := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Rotate disks", Due: "2026-10-01", Recur: "monthly"}).ID
	viaOp := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Water plants", Due: "2026-10-15", Recur: "weekly"}).ID
	dropped := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Ended on purpose", Due: "2026-10-01", Recur: "daily"}).ID
	plain := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Once", Due: "2026-10-01"}).ID
	bad := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Odd recur", Due: "2026-10-01", Recur: "whenever"}).ID
	markDone(t, d, byHand)
	markDone(t, d, plain)
	markDone(t, d, bad)
	must(t, TaskDone, d, TaskDoneInput{ID: viaOp}) // makes its own next
	doc, tm, _ := getTask(ctx, d.Store, dropped)
	tm.Status = model.TaskDropped
	if _, err := d.Store.Update(ctx, doc); err != nil {
		t.Fatal(err)
	}
	n := len(must(t, TaskList, d, TaskListInput{}).Tasks)

	out := tick(t, d)
	if len(out.Revived) != 1 {
		t.Fatalf("revived = %v", out.Revived)
	}
	next, nm, err := getTask(ctx, d.Store, out.Revived[0])
	if err != nil {
		t.Fatal(err)
	}
	if nm.Previous.Target() != byHand || nm.Status != model.TaskTodo || nm.Recur != "monthly" || string(nm.Due) != "2026-12-01" || next.Title != "Rotate disks" {
		t.Errorf("next = %+v %q", nm, next.Title)
	}
	if got := len(must(t, TaskList, d, TaskListInput{}).Tasks); got != n+1 {
		t.Errorf("tasks %d -> %d", n, got)
	}
	// The pass left the vault as it found it, so a second pass has nothing to do.
	events := eventCount(t, d)
	again := tick(t, d)
	if len(again.Revived)+len(again.Requests) != 0 || again.Changes != 0 {
		t.Errorf("second pass = %+v", again)
	}
	if got := eventCount(t, d); got != events {
		t.Errorf("second pass wrote %d events", got-events)
	}
	// Actor and surface say who did it.
	evs, _ := d.Store.Events(ctx, 100000)
	found := false
	for _, e := range evs {
		if e.Doc == out.Revived[0] && e.Actor == TickSurface && e.Surface == TickSurface {
			found = true
		}
	}
	if !found {
		t.Error("the new task is not logged as tick")
	}
	// The odd recur value is a lint warning of its own; nothing is an error.
	finds, _ := d.Store.Lint(ctx)
	for _, f := range finds {
		if f.Severity == model.SeverityError {
			t.Errorf("lint: %v", f)
		}
	}
}

func TestTickClosedProjectMakesNothingAndWarnsEachPass(t *testing.T) {
	tv := newTickVault(t, "2026-11-02T09:00:00Z", "request.runner=ia")
	d := tv.open("ia", "cli")
	proj := must(t, ProjectAdd, d, ProjectAddInput{DoneWhen: "Done.", Title: "Portal"}).ID
	task := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Rotate disks", Due: "2026-10-01", Recur: "monthly"}).ID
	markDone(t, d, task)
	must(t, ProjectClose, d, ProjectCloseInput{ID: proj, Force: true})
	for i := range 2 {
		out := tick(t, d)
		if len(out.Revived) != 0 || len(out.Warnings) != 1 || !strings.Contains(out.Warnings[0], "closed") {
			t.Errorf("pass %d = %+v", i, out)
		}
	}
}

func TestTickRequestsForAgentTasks(t *testing.T) {
	tv := newTickVault(t, "2026-11-02T09:00:00Z", "request.runner=ia")
	d := tv.open("ia", "cli")
	proj := must(t, ProjectAdd, d, ProjectAddInput{DoneWhen: "Done.", Title: "Portal"}).ID
	add := func(title, due, status string, assign string) string {
		id := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: title, Due: due, Recur: "daily"}).ID
		if assign != "" {
			must(t, TaskAssign, d, TaskAssignInput{ID: id, Assignee: assign})
		}
		return id
	}
	due := add("Summarize alerts", "2026-11-02", "", "agent:scribe")
	noDate := add("No date", "", "", "agent:scribe")
	later := add("Tomorrow", "2026-11-03", "", "agent:scribe")
	mine := add("Mine", "2026-11-02", "", "")
	started := add("Started", "2026-11-02", "", "agent:scribe")
	must(t, TaskStart, d, TaskStartInput{ID: started})
	cancelled := add("Cancelled before", "2026-11-02", "", "agent:scribe")
	rq := must(t, RequestAdd, d, RequestAddInput{Ask: "earlier", Links: []string{cancelled}})
	must(t, RequestCancel, d, RequestCancelInput{ID: rq.ID})
	// The vault's default: tick is not a surface that queues, so a request is only proposed.
	out := tick(t, d)
	if len(out.Requests) != 2 || out.Proposed != 2 {
		t.Fatalf("pass 1 = %+v (later %s, mine %s, started %s, cancelled %s)", out, later, mine, started, cancelled)
	}
	for _, id := range out.Requests {
		_, r, err := getRequest(ctx, d.Store, id)
		if err != nil {
			t.Fatal(err)
		}
		if r.Status != model.RequestProposed || r.Runner != "ia" || len(r.Links) != 1 || !slices.Contains([]string{due, noDate}, r.Links[0].Target()) {
			t.Errorf("request = %+v", r)
		}
	}
	// Repeat: nothing new.
	events := eventCount(t, d)
	if again := tick(t, d); len(again.Requests) != 0 || eventCount(t, d) != events {
		t.Errorf("pass 2 = %+v", again)
	}

	// Opting tick in to the queue makes the next one queued. The first two keep their state.
	tv.addProps("request.surfaces=cli,pwa,tick")
	tv.now = tv.now.Add(24 * time.Hour) // Nov 3: "Tomorrow" is due
	out = tick(t, d)
	if len(out.Requests) != 1 || out.Proposed != 0 {
		t.Fatalf("pass 3 = %+v", out)
	}
	if _, r, _ := getRequest(ctx, d.Store, out.Requests[0]); r.Status != model.RequestQueued || r.Links[0].Target() != later {
		t.Errorf("request = %+v", r)
	}
	mustLintClean(t, d)
}

func TestTickNamesNudges(t *testing.T) {
	tv := newTickVault(t, "2026-11-02T09:00:00Z", "request.runner=ia")
	d := tv.open("ia", "cli")
	ask := func(nudge string) string {
		return must(t, RFXAdd, d, RFXAddInput{Direction: model.RFXOut, Ask: "Ask " + nudge, Route: "domain-logging", SelfServe: "x", NextNudge: nudge}).ID
	}
	late, today, future, none := ask("2026-10-30"), ask("2026-11-02"), ask("2026-11-09"), ask("")
	closed := ask("2026-10-01")
	must(t, RFXClose, d, RFXCloseInput{ID: closed})
	out := tick(t, d)
	want := []string{late, today}
	slices.Sort(want)
	if !slices.Equal(out.Nudges, want) {
		t.Errorf("nudges = %v, want %v (future %s, none %s)", out.Nudges, want, future, none)
	}
}
