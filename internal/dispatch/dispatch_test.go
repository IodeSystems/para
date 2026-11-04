package dispatch

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/iodesystems/para/docs"
	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
	"github.com/iodesystems/para/internal/store/local"
	"github.com/iodesystems/para/internal/vault"
)

// No real agent starts in these tests. Every command is a script written here
// that records what it was given and waits for a file to say it may end.

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

type rig struct {
	t      *testing.T
	root   string
	state  string
	out    string // scripts write here; it is $OUT for them
	clk    *clock
	user   ops.Deps // the owner's surface, to queue requests
	cancel context.CancelFunc
	ctx    context.Context
}

func newRig(t *testing.T) *rig {
	t.Helper()
	base := t.TempDir()
	r := &rig{t: t, root: filepath.Join(base, "v"), state: filepath.Join(base, "state"), out: filepath.Join(base, "out"),
		clk: &clock{t: time.Date(2026, 11, 2, 9, 0, 0, 0, time.Local)}}
	if err := os.MkdirAll(r.out, 0o700); err != nil {
		t.Fatal(err)
	}
	tpl, err := fs.Sub(docs.Templates, "templates")
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Init(r.root, "acme", tpl, vault.InitOptions{NoGit: true}); err != nil {
		t.Fatal(err)
	}
	s, err := local.Open(r.root, vault.Options{ID: "acme", Actor: "user", Surface: "cli", Install: "m1", StateDir: r.state, Now: r.clk.now})
	if err != nil {
		t.Fatal(err)
	}
	r.user = ops.Deps{Store: s}
	r.ctx, r.cancel = context.WithCancel(context.Background())
	t.Cleanup(func() {
		r.release("*") // let any script end; then stop what is left
		r.cancel()
	})
	t.Setenv("OUT", r.out)
	return r
}

// dispatcher builds a Dispatcher on the rig, as the server would.
func (r *rig) dispatcher() *Dispatcher {
	r.t.Helper()
	st := r.user.Store.(*local.Store).As("dispatch", "dispatch")
	d := New(Options{Vault: "acme", Home: "/tmp/para-home-test", StateDir: r.state, Store: st, Now: r.clk.now,
		Context: r.ctx, Cooldown: time.Millisecond, MaxAttempts: 3})
	r.t.Cleanup(func() { r.release("*"); d.Wait() })
	return d
}

// script writes an executable shell script and returns its path.
func (r *rig) script(body string) string {
	r.t.Helper()
	p := filepath.Join(r.t.TempDir(), "agent.sh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
		r.t.Fatal(err)
	}
	return p
}

// config writes agent.json, mode 0600.
func (r *rig) config(json string) {
	r.t.Helper()
	if err := os.MkdirAll(r.state, 0o700); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(ConfigPath(r.state), []byte(json), 0o600); err != nil {
		r.t.Fatal(err)
	}
}

func (r *rig) queue(ask string) string {
	r.t.Helper()
	out, err := ops.RequestAdd.Run(context.Background(), r.user, ops.RequestAddInput{Ask: ask})
	if err != nil {
		r.t.Fatal(err)
	}
	if out.Status != model.RequestQueued {
		r.t.Fatalf("status = %s", out.Status)
	}
	return out.ID
}

// release lets the blocked scripts of a request (or every one, with "*") end.
func (r *rig) release(id string) {
	if id == "*" {
		id = "all"
	}
	_ = os.WriteFile(filepath.Join(r.out, "release-"+id), nil, 0o600)
}

// blockScript records its start, then waits for a release file.
const blockBody = `echo "$PARA_REQUEST" >> "$OUT/started"
while [ ! -f "$OUT/release-$PARA_REQUEST" ] && [ ! -f "$OUT/release-all" ]; do sleep 0.02; done`

func (r *rig) started() []string {
	data, _ := os.ReadFile(filepath.Join(r.out, "started"))
	return strings.Fields(string(data))
}

func (r *rig) waitStarted(n int) {
	r.t.Helper()
	for range 500 {
		if len(r.started()) >= n {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	r.t.Fatalf("only %d scripts started, want %d", len(r.started()), n)
}

func (r *rig) events() []model.Event {
	r.t.Helper()
	evs, err := r.user.Store.Events(context.Background(), 100000)
	if err != nil {
		r.t.Fatal(err)
	}
	return evs
}

func (r *rig) dispatchEvents() []model.Event {
	var out []model.Event
	for _, e := range r.events() {
		if strings.HasPrefix(e.Op, "dispatch_") {
			out = append(out, e)
		}
	}
	return out
}

func (r *rig) loadState() State {
	r.t.Helper()
	s, err := LoadState(r.state)
	if err != nil {
		r.t.Fatal(err)
	}
	return s
}

func TestNoConfigStartsNothing(t *testing.T) {
	r := newRig(t)
	r.queue("do the thing")
	d := r.dispatcher()
	got, err := d.Check(r.ctx)
	if err != nil || len(got) != 0 {
		t.Fatalf("started %v, err %v", got, err)
	}
	if _, err := os.Stat(StatePath(r.state)); err == nil {
		t.Error("a state file exists although nothing is configured")
	}
	if _, err := os.Stat(filepath.Join(r.state, "agent")); err == nil {
		t.Error("a run directory exists although nothing is configured")
	}
	if len(r.dispatchEvents()) != 0 {
		t.Error("an event was logged although nothing is configured")
	}
}

func TestStartsTheCommandWithTheRequestIDOnly(t *testing.T) {
	r := newRig(t)
	work := t.TempDir()
	hostile := "Summarize $(touch " + filepath.Join(r.out, "pwned") + "); `touch " + filepath.Join(r.out, "pwned2") + "` && rm -rf ~"
	id := r.queue(hostile + "\n\nbody text that must not reach the command: SECRET-BODY")
	sc := r.script(`echo "$PARA_REQUEST" > "$OUT/id"
echo "$PARA_HOME" > "$OUT/home"
echo "$PARA_VAULT" > "$OUT/vault"
pwd > "$OUT/cwd"
for a in "$@"; do echo "$a"; done > "$OUT/argv"
env > "$OUT/env"
echo script output`)
	r.config(`{"command": ["` + sc + `", "-p", "fixed arg"], "cwd": "` + work + `"}`)
	d := r.dispatcher()
	got, err := d.Check(r.ctx)
	if err != nil || len(got) != 1 || got[0] != id {
		t.Fatalf("started %v, err %v", got, err)
	}
	d.Wait()
	read := func(name string) string {
		b, err := os.ReadFile(filepath.Join(r.out, name))
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(string(b))
	}
	if read("id") != id || read("home") != "/tmp/para-home-test" || read("vault") != "acme" {
		t.Errorf("env: id %q home %q vault %q", read("id"), read("home"), read("vault"))
	}
	if cwd, _ := filepath.EvalSymlinks(work); read("cwd") != cwd && read("cwd") != work {
		t.Errorf("cwd = %q, want %q", read("cwd"), work)
	}
	if read("argv") != "-p\nfixed arg" {
		t.Errorf("argv = %q: only the configured arguments are passed", read("argv"))
	}
	for _, leak := range []string{"SECRET-BODY", "Summarize", "pwned", "rm -rf"} {
		if strings.Contains(read("env"), leak) || strings.Contains(read("argv"), leak) {
			t.Errorf("vault text %q reached the command", leak)
		}
	}
	for _, n := range []string{"pwned", "pwned2"} {
		if _, err := os.Stat(filepath.Join(r.out, n)); err == nil {
			t.Errorf("a shell ran vault text: %s exists", n)
		}
	}
	// The log: one file, mode 0600, with the command's output.
	runs, _ := os.ReadDir(filepath.Join(r.state, "agent", "runs"))
	if len(runs) != 1 {
		t.Fatalf("logs = %d", len(runs))
	}
	st, _ := os.Stat(filepath.Join(r.state, "agent", "runs", runs[0].Name()))
	if st.Mode().Perm() != 0o600 || !strings.HasSuffix(runs[0].Name(), "-"+id+".1.log") {
		t.Errorf("log %s mode %v", runs[0].Name(), st.Mode().Perm())
	}
	if b, _ := os.ReadFile(filepath.Join(r.state, "agent", "runs", runs[0].Name())); !strings.Contains(string(b), "script output") {
		t.Errorf("log = %q", b)
	}
	if fi, _ := os.Stat(StatePath(r.state)); fi.Mode().Perm() != 0o600 {
		t.Errorf("state mode = %v", fi.Mode().Perm())
	}
	// Events: ids and an exit status, nothing else.
	evs := r.dispatchEvents()
	if len(evs) != 2 || evs[0].Op != "dispatch_start" || evs[1].Op != "dispatch_exit" || evs[1].To != "exit:0" || evs[0].Doc != id || evs[1].Doc != id {
		t.Fatalf("events = %+v", evs)
	}
	for _, e := range evs {
		if e.Actor != "dispatch" || e.Surface != "dispatch" || strings.Contains(e.From+e.To+e.Doc, "Summarize") {
			t.Errorf("event = %+v", e)
		}
	}
}

func TestOnlyClaimableOnThisInstallStarts(t *testing.T) {
	r := newRig(t)
	sc := r.script(blockBody)
	r.config(`{"command": ["` + sc + `"], "max_concurrent": 4}`)
	mine := r.queue("mine")
	other, err := ops.RequestAdd.Run(context.Background(), r.user, ops.RequestAddInput{Ask: "someone else's", Runner: "m2"})
	if err != nil {
		t.Fatal(err)
	}
	claimed := r.queue("already claimed")
	if _, err := ops.RequestNext.Run(context.Background(), ops.Deps{Store: r.user.Store.ActAs("agent", "mcp")}, ops.RequestNextInput{}); err != nil {
		t.Fatal(err)
	}
	// request_next claims the oldest queued request: that was `mine`. Queue it again.
	_ = claimed
	d := r.dispatcher()
	got, err := d.Check(r.ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range got {
		if id == other.ID {
			t.Errorf("started a request that belongs to another install")
		}
	}
	if len(got) != 1 || got[0] == mine {
		t.Fatalf("started %v (mine %s was claimed by the agent, claimed %s)", got, mine, claimed)
	}
	r.release("*")
	d.Wait()
}

func TestConcurrencyLimit(t *testing.T) {
	r := newRig(t)
	sc := r.script(blockBody)
	r.config(`{"command": ["` + sc + `"], "max_concurrent": 1, "daily_limit": 50}`)
	a, b := r.queue("first"), r.queue("second")
	d := r.dispatcher()
	got, err := d.Check(r.ctx)
	if err != nil || len(got) != 1 {
		t.Fatalf("started %v, err %v", got, err)
	}
	r.waitStarted(1)
	// A look at the queue while one runs starts nothing.
	if again, _ := d.Check(r.ctx); len(again) != 0 {
		t.Fatalf("started %v while one was running", again)
	}
	first := got[0]
	r.release(first)
	for range 500 { // the run ends, and its slot frees
		if len(r.loadState().Running) == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	next, err := d.Check(r.ctx)
	if err != nil || len(next) != 1 || next[0] == first {
		t.Fatalf("after the first ended, started %v (first %s; a %s b %s)", next, first, a, b)
	}
	r.release("*")
	d.Wait()

	// Two slots: two at once.
	r2 := newRig(t)
	sc2 := r2.script(blockBody)
	r2.config(`{"command": ["` + sc2 + `"], "max_concurrent": 2, "daily_limit": 50}`)
	r2.queue("one")
	r2.queue("two")
	r2.queue("three")
	d2 := r2.dispatcher()
	got, err = d2.Check(r2.ctx)
	if err != nil || len(got) != 2 {
		t.Fatalf("two slots: started %v, err %v", got, err)
	}
	r2.release("*")
	d2.Wait()
}

func TestDailyLimitAndTheNextDay(t *testing.T) {
	r := newRig(t)
	sc := r.script(`echo "$PARA_REQUEST" >> "$OUT/started"`)
	r.config(`{"command": ["` + sc + `"], "max_concurrent": 4, "daily_limit": 2}`)
	for _, n := range []string{"a", "b", "c"} {
		r.queue(n)
	}
	d := r.dispatcher()
	got, err := d.Check(r.ctx)
	if err != nil || len(got) != 2 {
		t.Fatalf("started %v, err %v", got, err)
	}
	d.Wait()
	if again, _ := d.Check(r.ctx); len(again) != 0 {
		t.Fatalf("over the daily limit: started %v", again)
	}
	if s := r.loadState(); s.Started != 2 || s.Day != "2026-11-02" {
		t.Errorf("state = %+v", s)
	}
	// The next local day starts a new count.
	r.clk.add(24 * time.Hour)
	// Two of the three are still queued (the scripts claim nothing) and the third
	// was never started; the new day allows two starts again.
	next, err := d.Check(r.ctx)
	if err != nil || len(next) != 2 {
		t.Fatalf("next day: started %v, err %v", next, err)
	}
	d.Wait()
	if s := r.loadState(); s.Started != 2 || s.Day != "2026-11-03" {
		t.Errorf("state after rollover = %+v", s)
	}
}

// A restart of the server does not reset either limit: both live in the state file.
func TestLimitsHoldAcrossARestart(t *testing.T) {
	t.Run("daily", func(t *testing.T) {
		r := newRig(t)
		sc := r.script(`echo "$PARA_REQUEST" >> "$OUT/started"`)
		r.config(`{"command": ["` + sc + `"], "max_concurrent": 4, "daily_limit": 1}`)
		r.queue("a")
		r.queue("b")
		d1 := r.dispatcher()
		if got, _ := d1.Check(r.ctx); len(got) != 1 {
			t.Fatalf("started %v", got)
		}
		d1.Wait()
		d2 := r.dispatcher() // a new process: nothing in memory
		if got, err := d2.Check(r.ctx); err != nil || len(got) != 0 {
			t.Fatalf("after a restart, started %v, err %v", got, err)
		}
	})
	t.Run("concurrent", func(t *testing.T) {
		r := newRig(t)
		sc := r.script(blockBody)
		r.config(`{"command": ["` + sc + `"], "max_concurrent": 1, "daily_limit": 50}`)
		r.queue("a")
		r.queue("b")
		d1 := r.dispatcher()
		if got, _ := d1.Check(r.ctx); len(got) != 1 {
			t.Fatalf("started %v", got)
		}
		r.waitStarted(1)
		// The server restarted while the command was still running. Its pid is in
		// the state file and alive, so the slot is taken.
		d2 := r.dispatcher()
		if got, err := d2.Check(r.ctx); err != nil || len(got) != 0 {
			t.Fatalf("after a restart, started %v, err %v", got, err)
		}
		r.release("*")
		d1.Wait()
		// Once it ends, d2 learns it from the pid, not from memory.
		got, err := d2.Check(r.ctx)
		if err != nil || len(got) != 1 {
			t.Fatalf("after it ended, started %v, err %v", got, err)
		}
		d2.Wait()
	})
	t.Run("a dead pid in the file frees its slot", func(t *testing.T) {
		r := newRig(t)
		sc := r.script(`echo "$PARA_REQUEST" >> "$OUT/started"`)
		r.config(`{"command": ["` + sc + `"], "max_concurrent": 1, "daily_limit": 50}`)
		r.queue("a")
		// A previous life died with a run recorded: pid 2^22+1 is above pid_max on Linux.
		s := State{Day: "2026-11-02", Started: 1, Attempts: map[string]int{}, Running: []Run{{ID: "request-gone", PID: 4194305, Started: r.clk.now()}}}
		if err := s.save(r.state); err != nil {
			t.Fatal(err)
		}
		d := r.dispatcher()
		if got, err := d.Check(r.ctx); err != nil || len(got) != 1 {
			t.Fatalf("started %v, err %v", got, err)
		}
		d.Wait()
	})
}

func TestACrashInTheCommand(t *testing.T) {
	t.Run("killed", func(t *testing.T) {
		r := newRig(t)
		sc := r.script(`echo "$PARA_REQUEST" >> "$OUT/started"
kill -KILL $$`) // the script kills only itself
		r.config(`{"command": ["` + sc + `"], "max_concurrent": 1, "daily_limit": 50}`)
		id := r.queue("will crash")
		d := r.dispatcher()
		if got, _ := d.Check(r.ctx); len(got) != 1 {
			t.Fatalf("started %v", got)
		}
		d.Wait()
		evs := r.dispatchEvents()
		if len(evs) != 2 || evs[1].To != "signal:killed" || evs[1].Doc != id {
			t.Fatalf("events = %+v", evs)
		}
		s := r.loadState()
		if len(s.Running) != 0 || len(s.Last) != 1 || s.Last[0].Status != "signal:killed" {
			t.Errorf("state = %+v", s)
		}
	})
	t.Run("exit status, and the request is tried again only so often", func(t *testing.T) {
		r := newRig(t)
		sc := r.script(`echo "$PARA_REQUEST" >> "$OUT/started"
exit 3`)
		r.config(`{"command": ["` + sc + `"], "max_concurrent": 1, "daily_limit": 50}`)
		id := r.queue("fails, never claims")
		d := r.dispatcher()
		d.o.Cooldown = time.Hour
		if got, _ := d.Check(r.ctx); len(got) != 1 {
			t.Fatalf("started %v", got)
		}
		d.Wait()
		if evs := r.dispatchEvents(); evs[len(evs)-1].To != "exit:3" {
			t.Fatalf("events = %+v", evs)
		}
		// Still queued, but cooling down: not started again at once, and not by a
		// dispatcher that was restarted either.
		if got, _ := d.Check(r.ctx); len(got) != 0 {
			t.Fatalf("started %v inside the cooldown", got)
		}
		restarted := r.dispatcher()
		restarted.o.Cooldown = time.Hour
		if got, _ := restarted.Check(r.ctx); len(got) != 0 {
			t.Fatalf("after a restart, started %v inside the cooldown", got)
		}
		for i := 1; i < 5; i++ {
			r.clk.add(2 * time.Hour)
			got, _ := d.Check(r.ctx)
			d.Wait()
			want := 1
			if i >= 3 { // three starts a day for one request: the first, and two more
				want = 0
			}
			if len(got) != want {
				t.Fatalf("attempt %d: started %v, want %d", i+1, got, want)
			}
		}
		if n := len(r.started()); n != 3 {
			t.Errorf("the request was started %d times, want 3", n)
		}
		// A new day gives it three more.
		r.clk.add(24 * time.Hour)
		_ = id
		if got, _ := d.Check(r.ctx); len(got) != 1 {
			t.Fatalf("next day: started %v", got)
		}
		d.Wait()
	})
	t.Run("the command is not found", func(t *testing.T) {
		r := newRig(t)
		r.config(`{"command": ["/nonexistent/agent-binary"]}`)
		r.queue("x")
		d := r.dispatcher()
		if got, err := d.Check(r.ctx); err == nil || len(got) != 0 {
			t.Fatalf("started %v, err %v", got, err)
		}
		if s := r.loadState(); s.Started != 0 {
			t.Errorf("a command that cannot start was counted: %+v", s)
		}
	})
	t.Run("the working directory is missing", func(t *testing.T) {
		r := newRig(t)
		sc := r.script(`true`)
		r.config(`{"command": ["` + sc + `"], "cwd": "/nonexistent/dir"}`)
		r.queue("x")
		d := r.dispatcher()
		if got, err := d.Check(r.ctx); err == nil || len(got) != 0 {
			t.Fatalf("started %v, err %v", got, err)
		}
		if s := r.loadState(); s.Started != 0 {
			t.Errorf("counted: %+v", s)
		}
	})
}

func TestRequestIDValidation(t *testing.T) {
	for id, want := range map[string]bool{
		"request-1m6dgt3gr213hqxy6zw1yd": true,
		"request-a":                      true,
		"a":                              true,
		"":                               false,
		"Request-A":                      false,
		"request_a":                      false,
		"request a":                      false,
		"request-a;rm -rf /":             false,
		"request-a\nPARA_HOME=/x":        false,
		"$(touch x)":                     false,
		"../request-a":                   false,
		"request-a/..":                   false,
		"-request":                       false,
		"request--a":                     false,
		strings.Repeat("a", 100):         true,
		strings.Repeat("a", 101):         false,
	} {
		if got := ValidRequestID(id); got != want {
			t.Errorf("ValidRequestID(%q) = %v, want %v", id, got, want)
		}
	}
}

// A file a sync tool or an agent put in requests/ can have any name. One that is
// not a plain id is never passed to a command.
func TestARequestFileWithAHostileNameIsNeverDispatched(t *testing.T) {
	r := newRig(t)
	sc := r.script(blockBody)
	r.config(`{"command": ["` + sc + `"], "max_concurrent": 4}`)
	bad := "Request_X"
	body := "---\ntype: request\ncreated: 2026-11-02\nstatus: queued\nrunner: m1\nqueued_at: 2026-11-02T08:00:00Z\n---\n\n# Hostile name\n"
	if err := os.WriteFile(filepath.Join(r.root, "requests", bad+".md"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	good := r.queue("fine")
	d := r.dispatcher()
	got, err := d.Check(r.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != good {
		t.Fatalf("started %v, want only %s", got, good)
	}
	r.waitStarted(1)
	for _, s := range r.started() {
		if s == bad {
			t.Error("the hostile id reached the command")
		}
	}
	r.release("*")
	d.Wait()
}

func TestStopEndsTheCommandWithSIGTERM(t *testing.T) {
	r := newRig(t)
	sc := r.script(`trap 'echo term >> "$OUT/term"; exit 0' TERM
echo "$PARA_REQUEST" >> "$OUT/started"
while :; do sleep 0.02; done`)
	r.config(`{"command": ["` + sc + `"]}`)
	r.queue("long")
	d := r.dispatcher()
	if got, _ := d.Check(r.ctx); len(got) != 1 {
		t.Fatalf("started %v", got)
	}
	r.waitStarted(1)
	r.cancel() // the server stops
	done := make(chan struct{})
	go func() { d.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("the command did not end")
	}
	if b, _ := os.ReadFile(filepath.Join(r.out, "term")); !strings.Contains(string(b), "term") {
		t.Error("the command did not get SIGTERM")
	}
}

func TestACorruptStateFileStartsNothing(t *testing.T) {
	r := newRig(t)
	sc := r.script(`echo "$PARA_REQUEST" >> "$OUT/started"`)
	r.config(`{"command": ["` + sc + `"]}`)
	r.queue("x")
	if err := os.WriteFile(StatePath(r.state), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	d := r.dispatcher()
	got, err := d.Check(r.ctx)
	if err == nil || !strings.Contains(err.Error(), "nothing starts") || len(got) != 0 {
		t.Fatalf("started %v, err %v", got, err)
	}
}

// Run looks at the queue when kicked, with no timer.
func TestRunStartsOnAKick(t *testing.T) {
	r := newRig(t)
	sc := r.script(`echo "$PARA_REQUEST" >> "$OUT/started"`)
	r.config(`{"command": ["` + sc + `"], "max_concurrent": 4}`)
	d := r.dispatcher()
	ctx, cancel := context.WithCancel(r.ctx)
	done := make(chan struct{})
	go func() { d.Run(ctx); close(done) }()
	time.Sleep(50 * time.Millisecond) // first pass: nothing queued
	r.queue("late")
	d.Kick()
	r.waitStarted(1)
	cancel()
	<-done
}
