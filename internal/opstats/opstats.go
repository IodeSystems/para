// Package opstats counts op calls per machine and vault, so an op nobody
// calls can be pruned with data. A count is op, surface, actor kind, number,
// and last time: no arguments, no content, no ids. Counts sit in memory and
// reach $PARA_HOME/vaults/<id>/opstats.json at Flush; the file is per machine
// state and never sits in the vault.
package opstats

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/iodesystems/para/internal/atomicfile"
)

// File is the name of the counts file in a vault's state directory.
const File = "opstats.json"

// Surfaces a call can come from.
const (
	MCPStdio = "mcp-stdio"
	MCPHTTP  = "mcp-http"
	Web      = "web" // REST and GraphQL
	CLI      = "cli"
)

// Owner is the actor kind of the owner; an agent's kind is its name.
const Owner = "owner"

// Entry is the count of one op from one surface by one actor kind.
type Entry struct {
	Op      string    `json:"op"`
	Surface string    `json:"surface"`
	Actor   string    `json:"actor"`
	Count   int       `json:"count"`
	Last    time.Time `json:"last"`
}

type key struct{ op, surface, actor string }

type file struct {
	Entries []Entry `json:"entries"`
}

// FlushEvery is how often a process that runs for long writes its counts.
var FlushEvery = 60 * time.Second

var (
	mu    sync.Mutex
	pend  = map[string]map[key]Entry{} // state dir -> counts not yet written
	start sync.Once
	now   = time.Now
)

// Record counts one call of op. An empty dir (no state dir) counts nothing.
func Record(dir, op, surface, actor string) {
	if dir == "" || op == "" {
		return
	}
	t := now().UTC().Truncate(time.Second)
	mu.Lock()
	defer mu.Unlock()
	m := pend[dir]
	if m == nil {
		m = map[key]Entry{}
		pend[dir] = m
	}
	k := key{op, surface, actor}
	e := m[k]
	e.Op, e.Surface, e.Actor, e.Last = op, surface, actor, t
	e.Count++
	m[k] = e
	start.Do(func() { go flusher() })
}

func flusher() {
	for {
		time.Sleep(FlushEvery)
		_ = FlushAll()
	}
}

// FlushAll writes the counts of every vault. Call it at exit.
func FlushAll() error {
	mu.Lock()
	dirs := make([]string, 0, len(pend))
	for d := range pend {
		dirs = append(dirs, d)
	}
	mu.Unlock()
	var errs []error
	for _, d := range dirs {
		if err := Flush(d); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// Flush adds the counts of dir held in memory to its file. Another process
// may have written the file since: it is read again and added to. The
// counts stay in memory when the write fails.
func Flush(dir string) error {
	mu.Lock()
	defer mu.Unlock()
	m := pend[dir]
	if len(m) == 0 {
		return nil
	}
	have, err := load(dir)
	if err != nil {
		return err
	}
	all := map[key]Entry{}
	for _, e := range have {
		all[key{e.Op, e.Surface, e.Actor}] = e
	}
	for k, e := range m {
		a := all[k]
		a.Op, a.Surface, a.Actor = k.op, k.surface, k.actor
		a.Count += e.Count
		if e.Last.After(a.Last) {
			a.Last = e.Last
		}
		all[k] = a
	}
	out := file{Entries: sorted(all)}
	b, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return fmt.Errorf("opstats: flush: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("opstats: flush: %w", err)
	}
	if err := atomicfile.Write(filepath.Join(dir, File), b, 0o600); err != nil {
		return fmt.Errorf("opstats: flush: %w", err)
	}
	delete(pend, dir)
	return nil
}

// Read flushes dir and returns all its counts, sorted by op, surface, actor.
func Read(dir string) ([]Entry, error) {
	if dir == "" {
		return nil, nil
	}
	if err := Flush(dir); err != nil {
		return nil, err
	}
	mu.Lock()
	defer mu.Unlock()
	return load(dir)
}

func load(dir string) ([]Entry, error) {
	b, err := os.ReadFile(filepath.Join(dir, File))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("opstats: read: %w", err)
	}
	var f file
	if err := json.Unmarshal(b, &f); err != nil {
		// A damaged cache is not worth failing a call for: start again.
		return nil, nil
	}
	return f.Entries, nil
}

func sorted(m map[key]Entry) []Entry {
	out := make([]Entry, 0, len(m))
	for _, e := range m {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Op != b.Op {
			return a.Op < b.Op
		}
		if a.Surface != b.Surface {
			return a.Surface < b.Surface
		}
		return a.Actor < b.Actor
	})
	return out
}
