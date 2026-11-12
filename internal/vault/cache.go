package vault

import (
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/iodesystems/para/internal/events"
	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/paramd"
)

// Parse cache. List and Get read every document many times per tick, so each
// file's parse is kept per process, keyed by the file's stamp, as git's index
// does: device, inode, size, and modification and change time in ns (Linux
// and macOS; elsewhere modification time and size only). A file whose stamp is
// unchanged is not read again. Every write here replaces the file with a
// rename (new inode) and drops the entry. A sync tool may keep the remote
// modification time, but its temp-file rename changes the inode and the
// change time, which no tool can set. Racy guard (git's "racily clean"): an
// entry is trusted only when the file's times are older than its read by
// racyWindow, since an edit within one tick of the time stamp clock can keep
// every field. Writes do not depend on this cache: Update compares Rev with
// a fresh read, and reconcile and the external_edit check hash the file.

// racyWindow is how much older than its read a file must be for its entry to
// be trusted.
var racyWindow = 2 * time.Second // a var so tests can settle files at once

// stamp identifies one version of a file without reading it.
type stamp struct {
	dev, ino uint64
	size     int64
	mod, chg int64 // ns; chg is 0 where the platform has no change time
}

func stampOf(st fs.FileInfo) stamp {
	s := stamp{mod: st.ModTime().UnixNano(), size: st.Size()}
	s.dev, s.ino, s.chg = sysStamp(st)
	return s
}

// settled reports whether a file with stamp s, read at t, cannot have changed
// unseen since.
func (s stamp) settled(t time.Time) bool {
	before := t.Add(-racyWindow).UnixNano()
	return s.mod < before && s.chg < before
}

// entry is one parsed file. It is not changed after it is stored, except
// for the decode that doc makes once.
type entry struct {
	st     stamp
	readAt time.Time // when the file was read, before the read
	leg    bool      // node was read as legacy (legacy.go)
	l      listed
	// node is the frontmatter mapping after legacy reading, nil when absent
	// or invalid; fm is the raw frontmatter for the error in that case.
	node        *yaml.Node
	fm          []byte
	title, body string

	once sync.Once
	d    model.Document
	err  error
}

// entryOf returns the parse of the file at rel, reading it only when its
// stamp changed. The stamp is taken before the read: a write between the two
// leaves an entry with the old stamp, which the next call reads again.
func (v *Vault) entryOf(rel string, leg bool) (*entry, error) {
	st, err := os.Stat(v.abs(rel))
	if err != nil {
		return nil, err
	}
	s := stampOf(st)
	v.paths.mu.Lock()
	e, ok := v.paths.refs[rel]
	v.paths.mu.Unlock()
	if ok && e.st == s && e.leg == leg && s.settled(e.readAt) {
		return e, nil
	}
	readAt := time.Now()
	data, err := os.ReadFile(v.abs(rel))
	if err != nil {
		return nil, err
	}
	old := e
	e = parseEntry(rel, data, leg)
	e.st, e.readAt = s, readAt
	v.paths.mu.Lock()
	if !ok || !slices.Equal(old.l.former, e.l.former) {
		v.dropIndexLocked() // a new file, or other former ids
	}
	if v.paths.refs == nil {
		v.paths.refs = map[string]*entry{}
	}
	v.paths.refs[rel] = e
	v.paths.mu.Unlock()
	return e, nil
}

// forget drops the entry of rel, after a write here.
func (v *Vault) forget(rels ...string) {
	v.paths.mu.Lock()
	for _, rel := range rels {
		delete(v.paths.refs, rel)
	}
	v.dropIndexLocked()
	v.paths.mu.Unlock()
}

func parseEntry(rel string, data []byte, leg bool) *entry {
	pf := paramd.Parse(rel, data)
	dup, _ := pf.Frontmatter["duplicate_of"].(string)
	pinned := ""
	switch p := pf.Frontmatter["pinned"].(type) {
	case string:
		pinned = p
	case time.Time:
		pinned = p.UTC().Format(time.RFC3339)
	}
	var former []string
	if list, ok := pf.Frontmatter["former_ids"].([]any); ok {
		for _, x := range list {
			if id := strings.TrimSpace(fmt.Sprint(x)); id != "" {
				former = append(former, id)
			}
		}
	}
	e := &entry{leg: leg, l: listed{hash: events.Hash(data), typ: model.Type(pf.Type()), title: pf.Title, dup: dup, pinned: pinned, former: former}}
	if leg {
		legacyParsed(pf)
	}
	e.node = pf.FMNode
	if e.node == nil {
		e.fm = pf.FrontmatterYAML()
	}
	e.title, e.body = pf.Title, bodyAfterTitle(pf)
	return e
}

// doc returns the document of f: decoded once per entry, and cloned per
// call, so a caller that changes its Document changes nothing here.
func (e *entry) doc(f docFile) (model.Document, error) {
	e.once.Do(func() {
		if e.node == nil {
			e.d, e.err = model.Decode(f.id, f.folder, e.fm, e.title, e.body)
		} else {
			e.d, e.err = model.DecodeNode(f.id, f.folder, e.node, e.title, e.body)
		}
		if e.err == nil {
			e.d.Rev = e.l.hash
		}
	})
	return e.d.Clone(), e.err
}
