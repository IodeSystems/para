package vault

import (
	"errors"
	"io/fs"
	"maps"
	"os"
	"strings"
	"time"
)

// Id index. Get of an id that no file has (a former id, or an id that is gone)
// must not walk the vault, and a tick makes hundreds of such lookups. One
// scan builds byID (file stem to file) and former (lower-case former id to
// the file that lists it); both answer hits and misses until the index is
// dropped. It is dropped by:
//   - every write here (withLock, forget);
//   - a full scan whose file set differs from the last one;
//   - a file read again with a new path or other former_ids (entryOf), which
//     is how a hand edit shows once a stamp check sees it;
//   - a directory the build walked whose stamp changed (a file added,
//     removed, or renamed outside this process), checked on each use with one
//     stat per directory. A directory stamped within dirRacy of the build is
//     not trusted, as with the file stamps (cache.go);
//   - age, idxMaxAge, because a hand edit of former_ids in place changes no
//     directory.
//
// gen counts drops: a build that overlapped one is not stored.

// idxMaxAge bounds how long a miss can outlive a file added outside this
// process. A var so tests can shorten it.
var idxMaxAge = 5 * time.Second

// dirRacy is the clock granularity a directory stamp is trusted down to.
var dirRacy = 20 * time.Millisecond

type idIndex struct {
	dirs    map[string]stamp
	trusted bool
	byID    map[string]docFile
	former  map[string]docFile
	built   time.Time
}

// sameFiles reports whether a and b hold the same files (id and path).
func sameFiles(a, b map[string]docFile) bool {
	return maps.Equal(a, b)
}

func (v *Vault) dropIndex() {
	v.paths.mu.Lock()
	v.dropIndexLocked()
	v.paths.mu.Unlock()
}

func (v *Vault) dropIndexLocked() {
	v.paths.idx = nil
	v.paths.gen++
}

// index returns the current id index, building it when dropped or old.
func (v *Vault) index() (*idIndex, error) {
	var ix *idIndex
	// A build can find its own parse reads drop the index (first reads of new
	// files); the second pass reads from the cache and settles.
	for range 3 {
		v.paths.mu.Lock()
		cur, gen := v.paths.idx, v.paths.gen
		v.paths.mu.Unlock()
		if cur != nil && v.fresh(cur) {
			return cur, nil
		}
		var err error
		if ix, err = v.buildIndex(); err != nil {
			return nil, err
		}
		v.paths.mu.Lock()
		stored := v.paths.gen == gen
		if stored {
			v.paths.idx = ix
		}
		v.paths.mu.Unlock()
		if stored {
			return ix, nil
		}
	}
	return ix, nil // dropped again and again: usable once, not stored
}

// fresh reports whether ix still describes the vault.
func (v *Vault) fresh(ix *idIndex) bool {
	if !ix.trusted || time.Since(ix.built) >= idxMaxAge {
		return false
	}
	for p, was := range ix.dirs {
		st, err := os.Lstat(p)
		switch {
		case err != nil:
			if was != (stamp{}) {
				return false
			}
		case stampOf(st) != was:
			return false
		}
	}
	return true
}

func (v *Vault) buildIndex() (*idIndex, error) {
	built := time.Now()
	dirs := map[string]stamp{}
	files, err := v.scanFilesDirs("", dirs)
	if err != nil {
		return nil, err
	}
	trusted := true
	for _, st := range dirs {
		if st.mod > built.Add(-dirRacy).UnixNano() || st.chg > built.Add(-dirRacy).UnixNano() {
			trusted = false
		}
	}
	leg := v.legacy()
	ix := &idIndex{dirs: dirs, trusted: trusted, byID: make(map[string]docFile, len(files)), former: map[string]docFile{}, built: built}
	for _, f := range files {
		if _, dup := ix.byID[f.id]; !dup {
			ix.byID[f.id] = f
		}
		e, err := v.entryOf(f.rel, leg)
		if errors.Is(err, fs.ErrNotExist) {
			continue // removed since the scan
		}
		if err != nil {
			return nil, err
		}
		for _, x := range e.l.former {
			k := strings.ToLower(x)
			if _, dup := ix.former[k]; !dup {
				ix.former[k] = f
			}
		}
	}
	return ix, nil
}
