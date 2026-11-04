package events

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/atomicfile"
	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/syncname"
)

// Manifest maps a vault-relative slash path to the content hash of the last
// write the vault made or accepted. It is per machine, in the state dir,
// never in the vault tree.
type Manifest struct {
	Files map[string]string `json:"files"`
}

// Hash returns the content hash used for manifests and Document.Rev.
func Hash(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// short cuts a hash to a length that fits an event field.
func short(h string) string {
	if len(h) > 19 {
		return h[:19]
	}
	return h
}

// LoadManifest reads the manifest. found is false when the file is absent.
func LoadManifest(file string) (m Manifest, found bool, err error) {
	data, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return Manifest{Files: map[string]string{}}, false, nil
	}
	if err != nil {
		return m, false, fmt.Errorf("events: load manifest: %w", err)
	}
	if err := json.Unmarshal(data, &m); err != nil {
		return m, false, fmt.Errorf("events: load manifest: %s: %w", file, err)
	}
	if m.Files == nil {
		m.Files = map[string]string{}
	}
	return m, true, nil
}

// Save writes the manifest atomically.
func (m Manifest) Save(file string) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("events: save manifest: %w", err)
	}
	if err := atomicfile.Write(file, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("events: save manifest: %w", err)
	}
	return nil
}

// Scan hashes every regular file under dirs, which are relative to root.
// Names that start with '.' and conflict copies (package syncname) are skipped.
func Scan(root string, dirs []string) (map[string]string, error) {
	out := map[string]string{}
	for _, dir := range dirs {
		err := filepath.WalkDir(filepath.Join(root, dir), func(p string, d fs.DirEntry, err error) error {
			switch {
			case errors.Is(err, fs.ErrNotExist):
				return nil
			case err != nil:
				return err
			case strings.HasPrefix(d.Name(), "."):
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			case !d.Type().IsRegular() || syncname.IsConflict(d.Name()):
				return nil
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			out[filepath.ToSlash(rel)] = Hash(data)
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("events: scan: %w", err)
		}
	}
	return out, nil
}

// PendingGrace is how long a change with no matching event is held before it
// is logged as external_edit. A sync tool may deliver a file before the event
// that explains it.
const PendingGrace = 10 * time.Minute

// PendingEntry is a change first seen at Since with hash Hash (empty for a
// removed file).
type PendingEntry struct {
	Hash  string    `json:"hash"`
	Since time.Time `json:"since"`
}

// Pending maps a vault-relative path to its held change. It is per machine,
// in the state dir.
type Pending map[string]PendingEntry

// LoadPending reads the pending file. An absent file is empty.
func LoadPending(file string) (Pending, error) {
	data, err := os.ReadFile(file)
	if errors.Is(err, fs.ErrNotExist) {
		return Pending{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("events: load pending: %w", err)
	}
	p := Pending{}
	if err := json.Unmarshal(data, &p); err != nil {
		return nil, fmt.Errorf("events: load pending: %s: %w", file, err)
	}
	return p, nil
}

// Save writes the pending file; an empty set removes it.
func (p Pending) Save(file string) error {
	if len(p) == 0 {
		if err := os.Remove(file); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("events: save pending: %w", err)
		}
		return nil
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("events: save pending: %w", err)
	}
	if err := atomicfile.Write(file, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("events: save pending: %w", err)
	}
	return nil
}

// Known answers whether a logged event, from any install, explains a file.
type Known struct{ byHash map[string][]model.Event }

// LoadKnown indexes every logged event that carries a hash.
func (l *Log) LoadKnown() (Known, error) {
	all, err := l.All()
	if err != nil {
		return Known{}, err
	}
	k := Known{byHash: map[string][]model.Event{}}
	for _, e := range all {
		if e.Hash != "" {
			k.byHash[e.Hash] = append(k.byHash[e.Hash], e)
		}
	}
	return k, nil
}

// Has reports whether any event carries hash.
func (k Known) Has(hash string) bool { return len(k.byHash[hash]) > 0 }

// hasMove reports a move event for doc to folder with hash.
func (k Known) hasMove(hash, doc, folder string) bool {
	for _, e := range k.byHash[hash] {
		if e.Op == OpMove && e.Doc == doc && e.To == folder {
			return true
		}
	}
	return false
}

// Reconcile compares the files under dirs with m and accepts the current
// files into m. For each difference:
//
//   - an event from any install carries the file's hash: another machine
//     wrote it. Accepted, nothing logged (OutcomeSynced). A file that
//     appeared where the same doc id vanished (a move) also needs a move event
//     for that doc and folder.
//   - otherwise it is held in p (OutcomePending), not accepted. The rule: a
//     held change whose hash is unchanged after grace has passed since it was
//     first seen is logged as one external_edit and accepted (OutcomeLogged).
//     A change that goes away, or changes hash, restarts or clears the hold.
//
// The caller holds the vault lock and saves m and p. Each From and To is a
// short content hash, empty where the file does not exist.
func Reconcile(l *Log, root string, dirs []string, m *Manifest, p Pending, now time.Time, grace time.Duration) ([]model.Change, error) {
	cur, err := Scan(root, dirs)
	if err != nil {
		return nil, err
	}
	var changes []model.Change
	for path, h := range cur {
		switch old, ok := m.Files[path]; {
		case !ok:
			changes = append(changes, model.Change{Path: path, Kind: model.ChangeAdded})
		case old != h:
			changes = append(changes, model.Change{Path: path, Kind: model.ChangeChanged})
		}
	}
	curIDs := map[string][]string{}
	for path := range cur {
		curIDs[DocID(path)] = append(curIDs[DocID(path)], path)
	}
	for path := range m.Files {
		if _, ok := cur[path]; !ok {
			changes = append(changes, model.Change{Path: path, Kind: model.ChangeRemoved})
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	for path := range p {
		if !differs(changes, path) {
			delete(p, path)
		}
	}
	if len(changes) == 0 {
		return nil, nil
	}
	known, err := l.LoadKnown()
	if err != nil {
		return nil, err
	}
	next := make(map[string]string, len(m.Files))
	for path, h := range m.Files {
		next[path] = h
	}
	moved := map[string]model.Outcome{} // doc id -> outcome of the arrival
	vanished := map[string]bool{}
	for _, c := range changes {
		if c.Kind == model.ChangeRemoved {
			vanished[DocID(c.Path)] = true
		}
	}
	// Arrivals and edits first, so a removal can follow its move.
	for i, c := range changes {
		if c.Kind == model.ChangeRemoved {
			continue
		}
		h := cur[c.Path]
		var ok bool
		if c.Kind == model.ChangeAdded && vanished[DocID(c.Path)] {
			ok = known.hasMove(h, DocID(c.Path), strings.SplitN(c.Path, "/", 2)[0])
		} else {
			ok = known.Has(h)
		}
		changes[i].Outcome = decide(p, c, h, ok, now, grace)
		if err := finish(l, &changes[i], h, m.Files[c.Path], next, cur, p); err != nil {
			return nil, err
		}
		if c.Kind == model.ChangeAdded {
			moved[DocID(c.Path)] = changes[i].Outcome
		}
	}
	out := changes[:0]
	for i, c := range changes {
		if c.Kind == model.ChangeRemoved {
			// The departure half of a move follows the arrival's outcome.
			oc, isMove := moved[DocID(c.Path)]
			if isMove && oc != model.OutcomeLogged {
				if oc == model.OutcomeSynced {
					delete(next, c.Path)
				}
				continue
			}
			g := grace
			if isMove {
				g = 0 // the arrival was logged; log the departure with it
			}
			changes[i].Outcome = decide(p, c, "", false, now, g)
			if err := finish(l, &changes[i], "", m.Files[c.Path], next, cur, p); err != nil {
				return nil, err
			}
		}
		out = append(out, changes[i])
	}
	m.Files = next
	return out, nil
}

func differs(changes []model.Change, path string) bool {
	for _, c := range changes {
		if c.Path == path {
			return true
		}
	}
	return false
}

// decide picks the outcome for one change.
func decide(p Pending, c model.Change, h string, explained bool, now time.Time, grace time.Duration) model.Outcome {
	if explained {
		return model.OutcomeSynced
	}
	held, ok := p[c.Path]
	if ok && held.Hash == h && now.Sub(held.Since) >= grace {
		return model.OutcomeLogged
	}
	if grace <= 0 {
		return model.OutcomeLogged
	}
	if !ok || held.Hash != h {
		p[c.Path] = PendingEntry{Hash: h, Since: now}
	}
	return model.OutcomePending
}

// finish applies an outcome: log, accept, or hold.
func finish(l *Log, c *model.Change, h, oldH string, next, cur map[string]string, p Pending) error {
	switch c.Outcome {
	case model.OutcomePending:
		return nil
	case model.OutcomeLogged:
		if err := l.AppendHash(OpExternalEdit, DocID(c.Path), short(oldH), short(h), h); err != nil {
			return err
		}
	}
	delete(p, c.Path)
	if _, ok := cur[c.Path]; ok {
		next[c.Path] = h
	} else {
		delete(next, c.Path)
	}
	return nil
}

// DocID is the event doc value of a vault-relative path.
func DocID(rel string) string {
	return strings.TrimSuffix(path.Base(rel), ".md")
}

// LogExternalEdit records one edit that a mutation found before it overwrote
// the file. oldHash or newHash may be empty.
func LogExternalEdit(l *Log, rel, oldHash, newHash string) error {
	return l.AppendHash(OpExternalEdit, DocID(rel), short(oldHash), short(newHash), newHash)
}
