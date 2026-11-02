package dispatch

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/iodesystems/parasolve/internal/ops"
	"github.com/iodesystems/parasolve/internal/store"
)

// Defaults of Options.
const (
	// DefaultCooldown is how long after a run ends before the same request is
	// started for again.
	DefaultCooldown = time.Minute
	// DefaultMaxAttempts is how many times a day one request is started for. A
	// command that never claims, or crashes, would otherwise be started again for
	// the same request until the day's limit.
	DefaultMaxAttempts = 3
	// Fallback is how often Run looks at the queue without an event.
	Fallback = 30 * time.Second
	// keepLogs is how many run logs stay in the state directory.
	keepLogs = 100
	// stopWait is how long a command gets to end after SIGTERM when the server
	// stops, before it is killed.
	stopWait = 10 * time.Second
)

var requestIDRe = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// ValidRequestID reports whether id may be passed to a command. Only a request
// id that matches this reaches the environment of a process: a file name that a
// sync tool or an agent chose does not.
func ValidRequestID(id string) bool { return len(id) <= 100 && requestIDRe.MatchString(id) }

// Options configures a Dispatcher.
type Options struct {
	Vault    string
	Home     string      // $PARA_HOME of the server, passed to the command
	StateDir string      // the vault's per-machine directory
	Store    store.Store // acts as the dispatcher: names this install, records events
	Log      *slog.Logger
	Now      func() time.Time // nil means time.Now
	// Context bounds the commands: when it ends they get SIGTERM, then a kill
	// after ten seconds. Nil means they are never stopped by the dispatcher.
	Context     context.Context
	Cooldown    time.Duration // 0 means DefaultCooldown
	MaxAttempts int           // 0 means DefaultMaxAttempts
}

// Dispatcher starts the command of one vault.
type Dispatcher struct {
	o     Options
	watch *ops.RequestWatch
	kick  chan struct{}
	wg    sync.WaitGroup

	mu       sync.Mutex
	state    *State
	reported string // last problem logged, so a standing one is logged once
}

// New returns a Dispatcher. It reads and starts nothing until Check or Run.
func New(o Options) *Dispatcher {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	if o.Context == nil {
		o.Context = context.Background()
	}
	if o.Cooldown == 0 {
		o.Cooldown = DefaultCooldown
	}
	if o.MaxAttempts == 0 {
		o.MaxAttempts = DefaultMaxAttempts
	}
	return &Dispatcher{
		o: o, watch: ops.NewRequestWatch(ops.Deps{Store: o.Store}),
		kick: make(chan struct{}, 1),
	}
}

// Kick asks Run to look at the queue now.
func (d *Dispatcher) Kick() {
	select {
	case d.kick <- struct{}{}:
	default:
	}
}

// Wait blocks until every command this dispatcher started has ended.
func (d *Dispatcher) Wait() { d.wg.Wait() }

// Run looks at the queue on every kick and every Fallback until ctx ends, then
// waits for the commands it started. Stop them by ending Options.Context.
func (d *Dispatcher) Run(ctx context.Context) {
	defer d.Wait()
	t := time.NewTicker(Fallback)
	defer t.Stop()
	for {
		if _, err := d.Check(ctx); err != nil {
			d.problem(err)
		}
		select {
		case <-ctx.Done():
			return
		case <-d.kick:
		case <-t.C:
		}
	}
}

// problem logs err unless it is the same one as last time.
func (d *Dispatcher) problem(err error) {
	d.mu.Lock()
	same := d.reported == err.Error()
	d.reported = err.Error()
	d.mu.Unlock()
	if !same {
		d.o.Log.Warn("dispatch", "vault", d.o.Vault, "error", err)
	}
}

// Check looks at the queue once and starts what the limits allow. It returns
// the ids it started a command for. With no agent.json it does nothing.
func (d *Dispatcher) Check(ctx context.Context) ([]string, error) {
	cfg, err := LoadConfig(d.o.StateDir)
	if err != nil || cfg == nil {
		return nil, err
	}
	prog, err := cfg.Program()
	if err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.state == nil {
		s, err := LoadState(d.o.StateDir)
		if err != nil {
			return nil, err
		}
		d.state = &s
	}
	now := d.o.Now()
	d.state.rollover(now.Local().Format("2006-01-02"))
	live := d.state.live(now)
	if len(live) != len(d.state.Running) {
		d.state.Running = live
		if err := d.state.save(d.o.StateDir); err != nil {
			return nil, err
		}
	}
	ids, err := d.watch.ClaimableIDs(ctx)
	if err != nil {
		return nil, fmt.Errorf("dispatch: queue: %w", err)
	}
	sort.Strings(ids)
	var started []string
	for _, id := range ids {
		switch {
		case !ValidRequestID(id):
			d.o.Log.Warn("dispatch: request id is not a plain id; skipped", "vault", d.o.Vault)
			continue
		case slices.ContainsFunc(live, func(r Run) bool { return r.ID == id }):
			continue
		case d.state.Attempts[id] >= d.o.MaxAttempts:
			continue
		case now.Sub(d.state.lastEnded(id)) < d.o.Cooldown:
			continue
		}
		if len(live) >= cfg.MaxConcurrent || d.state.Started >= cfg.DailyLimit {
			break
		}
		run, err := d.start(cfg, prog, id, now)
		if err != nil {
			return started, err
		}
		if run != nil {
			started = append(started, id)
			live = append(live, *run)
		}
	}
	return started, nil
}

// environ is the environment of the command: the server's own, minus any
// variable of ours, plus the three that tell it what to claim.
func environ(base []string, id, home, vault string) []string {
	out := make([]string, 0, len(base)+3)
	for _, kv := range base {
		k, _, _ := strings.Cut(kv, "=")
		if k != "PARA_REQUEST" && k != "PARA_HOME" && k != "PARA_VAULT" {
			out = append(out, kv)
		}
	}
	return append(out, "PARA_REQUEST="+id, "PARA_HOME="+home, "PARA_VAULT="+vault)
}

// start counts the run, records it, and starts the command. The count is on
// disk before the command exists, so a crash cannot make the day's limit
// undercount. The caller holds d.mu.
func (d *Dispatcher) start(cfg *Config, prog, id string, now time.Time) (*Run, error) {
	if err := cfg.CheckCwd(d.o.StateDir); err != nil {
		return nil, err
	}
	logFile, logName, err := d.openLog(id, now)
	if err != nil {
		return nil, err
	}
	d.state.Started++
	d.state.Attempts[id]++
	run := Run{ID: id, Started: now, Log: logName}
	d.state.Running = append(d.state.Running, run)
	if err := d.state.save(d.o.StateDir); err != nil {
		_ = logFile.Close()
		return nil, err
	}
	ctx := context.Background()
	if err := d.o.Store.Record(ctx, "dispatch_start", id, "", "", ""); err != nil {
		_ = logFile.Close()
		d.finish(run, "start_failed", now)
		return nil, fmt.Errorf("dispatch: record: %w", err)
	}
	cmd := exec.CommandContext(d.o.Context, prog, cfg.Command[1:]...)
	cmd.Dir = cfg.Cwd
	cmd.Env = environ(os.Environ(), id, d.o.Home, d.o.Vault)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	cmd.Cancel = func() error { return terminate(cmd.Process) }
	cmd.WaitDelay = stopWait
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		d.o.Log.Warn("dispatch: start failed", "vault", d.o.Vault, "request", id)
		d.finish(run, "start_failed", now)
		_ = d.o.Store.Record(ctx, "dispatch_exit", id, "", "start_failed", "")
		return nil, nil
	}
	run.PID = cmd.Process.Pid
	for i := range d.state.Running {
		if d.state.Running[i].ID == id && d.state.Running[i].PID == 0 {
			d.state.Running[i].PID = run.PID
		}
	}
	if err := d.state.save(d.o.StateDir); err != nil {
		d.o.Log.Warn("dispatch: state", "vault", d.o.Vault, "error", err)
	}
	d.o.Log.Info("dispatch: started", "vault", d.o.Vault, "request", id)
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		status := exitStatus(cmd.Wait())
		_ = logFile.Close()
		d.mu.Lock()
		d.finish(run, status, d.o.Now())
		d.mu.Unlock()
		if err := d.o.Store.Record(context.Background(), "dispatch_exit", id, "", status, ""); err != nil {
			d.o.Log.Warn("dispatch: record", "vault", d.o.Vault, "error", err)
		}
		d.o.Log.Info("dispatch: ended", "vault", d.o.Vault, "request", id, "status", status)
		d.Kick()
	}()
	return &run, nil
}

// finish takes a run out of the in-flight list, remembers how it ended, and
// starts the cooldown of its request. The caller holds d.mu, except from start
// before the first Check returns.
func (d *Dispatcher) finish(run Run, status string, at time.Time) {
	keep := d.state.Running[:0]
	for _, r := range d.state.Running {
		if r.ID != run.ID || !r.Started.Equal(run.Started) {
			keep = append(keep, r)
		}
	}
	d.state.Running = keep
	d.state.remember(Exit{ID: run.ID, Ended: at, Status: status, Log: run.Log})
	if err := d.state.save(d.o.StateDir); err != nil {
		d.o.Log.Warn("dispatch: state", "vault", d.o.Vault, "error", err)
	}
}

// exitStatus turns the result of Wait into a token the event log accepts.
func exitStatus(err error) string {
	if err == nil {
		return "exit:0"
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if code := ee.ExitCode(); code >= 0 {
			return fmt.Sprintf("exit:%d", code)
		}
		return token(strings.ToLower(ee.String()))
	}
	return "wait_failed"
}

// token keeps the characters an event field allows: "signal: killed" becomes
// signal:killed.
func token(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == ' ':
			return -1
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == ':', r == '_', r == '-', r == '.':
			return r
		}
		return '_'
	}, s)
}

// openLog creates the log file of a run with mode 0600. The name holds the time
// and the validated request id, nothing else.
func (d *Dispatcher) openLog(id string, now time.Time) (*os.File, string, error) {
	dir := filepath.Join(d.o.StateDir, "agent", "runs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, "", fmt.Errorf("dispatch: log: %w", err)
	}
	name := fmt.Sprintf("%s-%s.%d.log", now.Local().Format("20060102-150405"), id, d.state.Attempts[id]+1)
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, "", fmt.Errorf("dispatch: log: %w", err)
	}
	pruneLogs(dir)
	return f, name, nil
}

// pruneLogs keeps the newest keepLogs files. Names start with the time, so
// they sort oldest first.
func pruneLogs(dir string) {
	ents, err := os.ReadDir(dir)
	if err != nil || len(ents) <= keepLogs {
		return
	}
	names := make([]string, 0, len(ents))
	for _, e := range ents {
		if e.Type().IsRegular() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, n := range names[:max(len(names)-keepLogs, 0)] {
		_ = os.Remove(filepath.Join(dir, n))
	}
}
