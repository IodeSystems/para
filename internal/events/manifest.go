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

	"github.com/iodesystems/paradize/internal/atomicfile"
	"github.com/iodesystems/paradize/internal/model"
)

// Manifest maps a vault-relative slash path to the content hash of the last
// write the vault made or accepted. It lives in .para/ and is gitignored.
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

// Save writes the manifest atomically. tmpDir is as in atomicfile.Write.
func (m Manifest) Save(file, tmpDir string) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("events: save manifest: %w", err)
	}
	if err := atomicfile.Write(file, tmpDir, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("events: save manifest: %w", err)
	}
	return nil
}

// Scan hashes every regular file under dirs, which are relative to root.
// Names that start with '.' are skipped.
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
			case !d.Type().IsRegular():
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

// Reconcile compares the files under dirs with m, logs one external_edit per
// difference, and accepts the current files into m. The caller holds the
// vault lock and saves m. Each From and To is a short content hash, empty on
// the side where the file does not exist.
func Reconcile(l *Log, root string, dirs []string, m *Manifest) ([]model.Change, error) {
	now, err := Scan(root, dirs)
	if err != nil {
		return nil, err
	}
	var changes []model.Change
	for p, h := range now {
		switch old, ok := m.Files[p]; {
		case !ok:
			changes = append(changes, model.Change{Path: p, Kind: model.ChangeAdded})
		case old != h:
			changes = append(changes, model.Change{Path: p, Kind: model.ChangeChanged})
		}
	}
	for p := range m.Files {
		if _, ok := now[p]; !ok {
			changes = append(changes, model.Change{Path: p, Kind: model.ChangeRemoved})
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	for _, c := range changes {
		if err := l.Append(OpExternalEdit, DocID(c.Path), short(m.Files[c.Path]), short(now[c.Path])); err != nil {
			return nil, err
		}
	}
	m.Files = now
	return changes, nil
}

// DocID is the event doc value of a vault-relative path.
func DocID(rel string) string {
	return strings.TrimSuffix(path.Base(rel), ".md")
}

// LogExternalEdit records one edit that a mutation found before it overwrote
// the file. oldHash or newHash may be empty.
func LogExternalEdit(l *Log, rel, oldHash, newHash string) error {
	return l.Append(OpExternalEdit, DocID(rel), short(oldHash), short(newHash))
}
