// Package events is the append-only log of a vault: one file per install and
// month, events/YYYY-MM.<install>.jsonl. An install appends only to its own
// files, so a file-sync tool never has two writers to merge. Readers merge
// every file by ts. A record holds ids and metadata, never content. The
// Append guard rejects a value that is not an id, an enum, or a hash.
package events

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/iodesystems/para/internal/model"
)

// Op values the vault writes.
const (
	OpCreate       = "create"
	OpUpdate       = "update"
	OpMove         = "move"
	OpRename       = "rename"
	OpExternalEdit = "external_edit"
)

// Source says who is acting. Each process or session sets it once.
type Source struct {
	Vault   string
	Install string // required to Append; names the file this install writes
	Actor   string
	Surface string
	Now     func() time.Time // nil means time.Now
	// Warn receives a note when a reader skips a partial last line. Nil means
	// stderr.
	Warn func(string)
}

// Log appends to and reads the event files in one directory. The caller
// holds the vault lock while it appends.
type Log struct {
	dir string
	src Source
}

// NewLog returns a Log over dir.
func NewLog(dir string, src Source) *Log {
	if src.Now == nil {
		src.Now = time.Now
	}
	if src.Warn == nil {
		src.Warn = func(m string) { fmt.Fprintln(os.Stderr, "warning: "+m) }
	}
	return &Log{dir: dir, src: src}
}

// As returns a Log over the same files that records another actor and surface.
func (l *Log) As(actor, surface string) *Log {
	c := *l
	c.src.Actor, c.src.Surface = actor, surface
	return &c
}

var (
	opRe      = regexp.MustCompile(`^[a-z][a-z_]{0,63}$`)
	tokenRe   = regexp.MustCompile(`^[A-Za-z0-9_.:-]{0,64}$`)
	docRe     = regexp.MustCompile(`^[^\x00-\x1f]{0,255}$`)
	hashRe    = regexp.MustCompile(`^(sha256:[0-9a-f]{64})?$`)
	installRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	// fileRe matches an event file: 2026-11.<install>.jsonl, or the install-less
	// 2026-11.jsonl of a log written before per-install files.
	fileRe = regexp.MustCompile(`^(\d{4}-\d{2})(?:\.([A-Za-z0-9_-]{1,64}))?\.jsonl$`)
)

// Append writes one record with no content hash.
func (l *Log) Append(op, doc, from, to string) error {
	return l.AppendHash(op, doc, from, to, "")
}

// AppendHash writes one record. hash is the full content hash of the file the
// event produced, or empty.
func (l *Log) AppendHash(op, doc, from, to, hash string) error {
	return l.AppendUpdate(op, doc, from, to, hash, "")
}

// AppendUpdate is AppendHash plus prev, the hash of the content it replaced.
func (l *Log) AppendUpdate(op, doc, from, to, hash, prev string) error {
	if !opRe.MatchString(op) || !docRe.MatchString(doc) || !tokenRe.MatchString(from) || !tokenRe.MatchString(to) || !hashRe.MatchString(hash) || !hashRe.MatchString(prev) {
		return fmt.Errorf("events: append: op %q: values must be ids, enums, or hashes", op)
	}
	if !installRe.MatchString(l.src.Install) {
		return fmt.Errorf("events: append: install id %q is not set or not a file-name token", l.src.Install)
	}
	rec := model.Event{
		TS:      l.src.Now().UTC(),
		Install: l.src.Install,
		Actor:   l.src.Actor,
		Surface: l.src.Surface,
		Vault:   l.src.Vault,
		Op:      op,
		Doc:     doc,
		From:    from,
		To:      to,
		Hash:    hash,
		Prev:    prev,
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("events: append: %w", err)
	}
	name := filepath.Join(l.dir, rec.TS.Format("2006-01")+"."+l.src.Install+".jsonl")
	f, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("events: append: %w", err)
	}
	defer func() { _ = f.Close() }()
	if err := trimPartial(f); err != nil {
		return fmt.Errorf("events: append: %w", err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("events: append: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("events: append: %w", err)
	}
	return nil
}

// trimPartial drops an unterminated last line from a file this install owns
// (a crash mid-append) and leaves f positioned at its end. Only the owner
// writes the file, so no other machine's data is cut.
func trimPartial(f *os.File) error {
	st, err := f.Stat()
	if err != nil {
		return err
	}
	size := st.Size()
	if size > 0 {
		last := make([]byte, 1)
		if _, err := f.ReadAt(last, size-1); err != nil {
			return err
		}
		if last[0] != '\n' {
			data, err := io.ReadAll(io.NewSectionReader(f, 0, size))
			if err != nil {
				return err
			}
			size = int64(bytes.LastIndexByte(data, '\n') + 1)
			if err := f.Truncate(size); err != nil {
				return err
			}
		}
	}
	_, err = f.Seek(size, io.SeekStart)
	return err
}

// files returns the event files grouped by month, newest month first. A name
// that is not an event file (a conflict copy, a temp file) is skipped.
func (l *Log) files() ([][]string, error) {
	ents, err := os.ReadDir(l.dir)
	if err != nil {
		return nil, fmt.Errorf("events: read dir: %w", err)
	}
	byMonth := map[string][]string{}
	for _, e := range ents {
		m := fileRe.FindStringSubmatch(e.Name())
		if m == nil || !e.Type().IsRegular() {
			continue
		}
		byMonth[m[1]] = append(byMonth[m[1]], filepath.Join(l.dir, e.Name()))
	}
	months := make([]string, 0, len(byMonth))
	for m := range byMonth {
		months = append(months, m)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(months)))
	out := make([][]string, 0, len(months))
	for _, m := range months {
		out = append(out, byMonth[m])
	}
	return out, nil
}

// Tail returns the last n records of all installs, oldest first, ordered by
// ts (ties by install, then file order).
func (l *Log) Tail(n int) ([]model.Event, error) {
	months, err := l.files()
	if err != nil {
		return nil, err
	}
	var out []model.Event
	for _, names := range months {
		if len(out) >= n {
			break
		}
		recs, err := l.readMonth(names)
		if err != nil {
			return nil, err
		}
		out = append(recs, out...)
	}
	if len(out) > n {
		out = out[len(out)-n:]
	}
	return out, nil
}

// All returns every record of every install, merged by ts.
func (l *Log) All() ([]model.Event, error) { return l.Tail(int(^uint(0) >> 1)) }

// readMonth reads and merges the files of one month.
func (l *Log) readMonth(names []string) ([]model.Event, error) {
	sort.Strings(names)
	var out []model.Event
	for _, name := range names {
		recs, err := l.readFile(name)
		if err != nil {
			return nil, err
		}
		out = append(out, recs...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].TS.Equal(out[j].TS) {
			return out[i].TS.Before(out[j].TS)
		}
		return out[i].Install < out[j].Install
	})
	return out, nil
}

// readFile parses one file. An unterminated last line that does not parse is
// a write in flight (a sync tool delivered half a line): it is skipped with a
// warning. Any other bad line is corruption and an error.
func (l *Log) readFile(name string) ([]model.Event, error) {
	data, err := os.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("events: read: %w", err)
	}
	var out []model.Event
	for n := 1; len(data) > 0; n++ {
		line := data
		terminated := false
		if i := bytes.IndexByte(data, '\n'); i >= 0 {
			line, data, terminated = data[:i], data[i+1:], true
		} else {
			data = nil
		}
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var rec model.Event
		if err := json.Unmarshal(line, &rec); err != nil {
			if !terminated {
				l.src.Warn(fmt.Sprintf("events: %s:%d: partial last line skipped (sync in flight?)", filepath.Base(name), n))
				continue
			}
			return nil, fmt.Errorf("events: read: %s:%d: %w", name, n, err)
		}
		out = append(out, rec)
	}
	return out, nil
}
