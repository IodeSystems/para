package events

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/iodesystems/parasolve/internal/model"
)

const (
	// PollInterval is the fallback scan period. Some sync folders (network
	// mounts, FUSE) raise no file events.
	PollInterval = 2 * time.Second
	// settle coalesces a burst of file events into one scan.
	settle = 40 * time.Millisecond
)

// Watch reports every record appended to the event files of dir by any
// process: this one, `para mcp`, the CLI, the TUI, or a sync tool. It starts at
// the end of the existing files, so history is not replayed. Each scan sends
// the new records of all files as one batch, in file order.
//
// A partial last line is held until its newline arrives. A name that is not an
// event file (a conflict copy, a temp file) is skipped. A line that does not
// parse is skipped and reported to warn.
//
// Watch runs until ctx ends. The returned stop func cancels and waits for the
// goroutine, so no goroutine outlives it.
func Watch(ctx context.Context, dir string, poll time.Duration, onBatch func([]model.Event), warn func(string)) (stop func()) {
	if poll <= 0 {
		poll = PollInterval
	}
	if warn == nil {
		warn = func(string) {}
	}
	ctx, cancel := context.WithCancel(ctx)
	w := &watcher{dir: dir, offsets: map[string]int64{}, warn: warn}
	w.prime()
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		w.run(ctx, poll, onBatch)
	}()
	var once sync.Once
	return func() { once.Do(func() { cancel(); wg.Wait() }) }
}

type watcher struct {
	dir     string
	offsets map[string]int64 // file name to the end of its last complete line
	warn    func(string)
}

// prime sets every existing file's offset to the end of its last complete line.
func (w *watcher) prime() {
	ents, err := os.ReadDir(w.dir)
	if err != nil {
		return
	}
	for _, e := range ents {
		if fileRe.MatchString(e.Name()) && e.Type().IsRegular() {
			w.offsets[e.Name()] = lineEnd(filepath.Join(w.dir, e.Name()))
		}
	}
}

// lineEnd returns the offset just past the last newline of the file.
func lineEnd(name string) int64 {
	f, err := os.Open(name)
	if err != nil {
		return 0
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return 0
	}
	const chunk = 4096
	for end := st.Size(); end > 0; {
		start := max(end-chunk, 0)
		buf := make([]byte, end-start)
		if _, err := f.ReadAt(buf, start); err != nil && err != io.EOF {
			return st.Size()
		}
		if i := bytes.LastIndexByte(buf, '\n'); i >= 0 {
			return start + int64(i) + 1
		}
		end = start
	}
	return 0
}

func (w *watcher) run(ctx context.Context, poll time.Duration, onBatch func([]model.Event)) {
	var kick <-chan fsnotify.Event
	if fw, err := fsnotify.NewWatcher(); err == nil {
		defer func() { _ = fw.Close() }()
		if err := fw.Add(w.dir); err == nil {
			kick = fw.Events
		}
	}
	tick := time.NewTicker(poll)
	defer tick.Stop()
	scan := func() {
		if batch := w.scan(); len(batch) > 0 {
			onBatch(batch)
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			scan()
		case <-kick:
			// Let the burst land, drop the queued events, scan once.
			select {
			case <-ctx.Done():
				return
			case <-time.After(settle):
			}
			for drained := false; !drained; {
				select {
				case <-kick:
				default:
					drained = true
				}
			}
			scan()
		}
	}
}

// scan reads the bytes appended since the last scan.
func (w *watcher) scan() []model.Event {
	ents, err := os.ReadDir(w.dir)
	if err != nil {
		return nil
	}
	var out []model.Event
	for _, e := range ents {
		if !fileRe.MatchString(e.Name()) || !e.Type().IsRegular() {
			continue
		}
		out = append(out, w.readNew(e.Name())...)
	}
	return out
}

func (w *watcher) readNew(name string) []model.Event {
	path := filepath.Join(w.dir, name)
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return nil
	}
	off := w.offsets[name] // zero for a file not seen before
	if st.Size() < off {
		// Shrunk: a crash trim, or a sync rewrite. Follow it, replay nothing.
		w.offsets[name] = lineEnd(path)
		return nil
	}
	if st.Size() == off {
		return nil
	}
	data, err := io.ReadAll(io.NewSectionReader(f, off, st.Size()-off))
	if err != nil {
		return nil
	}
	i := bytes.LastIndexByte(data, '\n')
	if i < 0 {
		return nil // partial line: wait for its newline
	}
	w.offsets[name] = off + int64(i) + 1
	var out []model.Event
	for _, line := range bytes.Split(data[:i], []byte{'\n'}) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var rec model.Event
		if err := json.Unmarshal(line, &rec); err != nil {
			w.warn(fmt.Sprintf("events: watch: %s: bad line skipped: %v", name, err))
			continue
		}
		out = append(out, rec)
	}
	return out
}
