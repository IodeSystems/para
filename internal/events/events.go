// Package events is the append-only log of a vault: events/YYYY-MM.jsonl.
// A record holds ids and metadata, never content. The Append guard rejects a
// value that is not an id, an enum, or a short hash.
package events

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/iodesystems/paradize/internal/model"
)

// Op values the vault writes.
const (
	OpCreate       = "create"
	OpUpdate       = "update"
	OpMove         = "move"
	OpExternalEdit = "external_edit"
)

// Source says who is acting. Each process or session sets it once.
type Source struct {
	Vault   string
	Actor   string
	Surface string
	Now     func() time.Time // nil means time.Now
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
	return &Log{dir: dir, src: src}
}

var (
	opRe    = regexp.MustCompile(`^[a-z][a-z_]{0,63}$`)
	tokenRe = regexp.MustCompile(`^[A-Za-z0-9_.:-]{0,64}$`)
	docRe   = regexp.MustCompile(`^[^\x00-\x1f]{0,255}$`)
)

// Append writes one record.
func (l *Log) Append(op, doc, from, to string) error {
	if !opRe.MatchString(op) || !docRe.MatchString(doc) || !tokenRe.MatchString(from) || !tokenRe.MatchString(to) {
		return fmt.Errorf("events: append: op %q: values must be ids, enums, or hashes", op)
	}
	rec := model.Event{
		TS:      l.src.Now().UTC(),
		Actor:   l.src.Actor,
		Surface: l.src.Surface,
		Vault:   l.src.Vault,
		Op:      op,
		Doc:     doc,
		From:    from,
		To:      to,
	}
	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("events: append: %w", err)
	}
	name := filepath.Join(l.dir, rec.TS.Format("2006-01")+".jsonl")
	f, err := os.OpenFile(name, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("events: append: %w", err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("events: append: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("events: append: %w", err)
	}
	return nil
}

// Tail returns the last n records, oldest first, across month files.
func (l *Log) Tail(n int) ([]model.Event, error) {
	names, err := filepath.Glob(filepath.Join(l.dir, "*.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("events: tail: %w", err)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	var out []model.Event
	for _, name := range names {
		if len(out) >= n {
			break
		}
		recs, err := readFile(name)
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

func readFile(name string) ([]model.Event, error) {
	data, err := os.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("events: read: %w", err)
	}
	var out []model.Event
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(nil, 1<<20)
	for n := 1; sc.Scan(); n++ {
		if strings.TrimSpace(sc.Text()) == "" {
			continue
		}
		var rec model.Event
		if err := json.Unmarshal(sc.Bytes(), &rec); err != nil {
			return nil, fmt.Errorf("events: read: %s:%d: %w", name, n, err)
		}
		out = append(out, rec)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("events: read: %s: %w", name, err)
	}
	return out, nil
}
