package server

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/syncname"
)

const (
	// docDebounce is how long the document folders must be quiet before a
	// reconcile runs. A save, a sync batch, or a git checkout is many events.
	docDebounce = 750 * time.Millisecond
	// docMaxWait bounds the debounce under a steady stream of changes.
	docMaxWait = 10 * time.Second
	// docFallback re-runs reconcile with no event: a network mount or FUSE
	// folder may raise none. A reconcile hashes every document, so this is slow.
	docFallback = 5 * time.Minute
)

// docWatcher runs reconcile after the document folders of a vault change, so a
// hand edit or a file a sync tool delivered is logged (external_edit) and
// pushed without a person running `para reconcile`.
//
// It cannot loop on itself. It watches only the document folders and the vault
// root. Reconcile writes to events/ (an event record) and to the per-machine
// state directory (manifest and pending), none of which it watches, and it
// never writes a document. A write through the vault is a change to a document
// folder too, so it runs one reconcile that finds the manifest already current
// and logs nothing.
type docWatcher struct {
	root      string
	dirs      []string // tracked folders, relative to root
	reconcile func(context.Context) (model.ReconcileResult, error)
	grace     func() time.Duration
	publish   func([]model.Change)
	warn      func(string)
	debounce  time.Duration
	maxWait   time.Duration
	fallback  time.Duration
}

// start runs the watcher. The returned func stops it and waits.
func (w *docWatcher) start(ctx context.Context) (stop func()) {
	ctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		w.run(ctx)
	}()
	var once sync.Once
	return func() { once.Do(func() { cancel(); wg.Wait() }) }
}

func (w *docWatcher) run(ctx context.Context) {
	var events <-chan fsnotify.Event
	if fw, err := fsnotify.NewWatcher(); err == nil {
		defer func() { _ = fw.Close() }()
		w.addAll(fw)
		events = fw.Events
		// A tracked folder made later is watched when the root reports it.
		_ = fw.Add(w.root)
		w.loop(ctx, fw, events)
		return
	}
	w.loop(ctx, nil, nil)
}

func (w *docWatcher) loop(ctx context.Context, fw *fsnotify.Watcher, events <-chan fsnotify.Event) {
	var fire <-chan time.Time
	var timer *time.Timer
	var first time.Time // the first event of the burst now waiting
	armAt := func(at time.Time) {
		if timer == nil {
			timer = time.NewTimer(time.Until(at))
		} else {
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(time.Until(at))
		}
		fire = timer.C
	}
	// arm schedules a run once the folders were quiet for the debounce, or the
	// burst has lasted maxWait.
	arm := func(now time.Time) {
		if first.IsZero() {
			first = now
		}
		at, limit := now.Add(w.debounce), first.Add(w.maxWait)
		if limit.Before(at) {
			at = limit
		}
		armAt(at)
	}
	defer func() {
		if timer != nil {
			timer.Stop()
		}
	}()
	run := func() {
		fire, first = nil, time.Time{}
		pending, err := w.once(ctx)
		if err != nil {
			w.warn("reconcile: " + err.Error())
		}
		if pending {
			// Held changes become external_edit only once the grace passes.
			armAt(time.Now().Add(w.grace() + 100*time.Millisecond))
		}
	}
	run() // catches what changed while the server was down
	tick := time.NewTicker(w.fallback)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			run()
		case <-fire:
			run()
		case ev, ok := <-events:
			if !ok {
				return
			}
			if w.relevant(fw, ev) {
				arm(time.Now())
			}
		}
	}
}

// once runs one reconcile and publishes what it accepted without an event.
// It reports whether a change is still held for the grace period.
func (w *docWatcher) once(ctx context.Context) (pending bool, err error) {
	res, err := w.reconcile(ctx)
	if err != nil {
		return false, err
	}
	var quiet []model.Change
	for _, c := range res.Changes {
		switch c.Outcome {
		case model.OutcomePending:
			pending = true
			quiet = append(quiet, c)
		case model.OutcomeSynced:
			quiet = append(quiet, c)
		}
	}
	// A logged change reaches the browser as its external_edit record, through
	// the events watcher. Synced and held ones wrote no record.
	if len(quiet) > 0 {
		w.publish(quiet)
	}
	return pending, nil
}

// addAll watches every tracked folder and its subfolders.
func (w *docWatcher) addAll(fw *fsnotify.Watcher) {
	for _, d := range w.dirs {
		w.addTree(fw, filepath.Join(w.root, d))
	}
}

func (w *docWatcher) addTree(fw *fsnotify.Watcher, dir string) {
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return nil
		case !d.IsDir():
			return nil
		case p != dir && strings.HasPrefix(d.Name(), "."):
			return filepath.SkipDir
		}
		_ = fw.Add(p)
		return nil
	})
}

// relevant reports whether an event should schedule a reconcile, and watches a
// new folder. Hidden, editor temp, and sync-conflict files are ignored: they
// are not documents (see events.Scan), so a reconcile would change nothing.
func (w *docWatcher) relevant(fw *fsnotify.Watcher, ev fsnotify.Event) bool {
	rel, err := filepath.Rel(w.root, ev.Name)
	if err != nil {
		return false
	}
	top, _, _ := strings.Cut(filepath.ToSlash(rel), "/")
	tracked := false
	for _, d := range w.dirs {
		tracked = tracked || d == top
	}
	if !tracked {
		return false // events/, .para/, and anything else at the root
	}
	if ev.Has(fsnotify.Create) {
		if st, err := os.Stat(ev.Name); err == nil && st.IsDir() {
			if !strings.HasPrefix(filepath.Base(ev.Name), ".") {
				w.addTree(fw, ev.Name)
				return true // files may have landed before the watch
			}
			return false
		}
	}
	return !ignoredName(filepath.Base(ev.Name)) && strings.HasSuffix(ev.Name, ".md")
}

// ignoredName reports whether a file name is a temp or conflict copy.
func ignoredName(base string) bool {
	switch {
	case strings.HasPrefix(base, "."), strings.HasPrefix(base, "#"), strings.HasSuffix(base, "~"):
		return true // dot files include atomicfile temps and vim swap files
	case strings.HasSuffix(base, ".tmp"), strings.HasSuffix(base, ".swp"), strings.HasSuffix(base, ".swx"):
		return true
	}
	return syncname.IsConflict(base)
}
