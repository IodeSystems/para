package vault

import (
	"context"
	"fmt"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/iodesystems/para/internal/events"
)

// OpRevisionAdd is the event of a stored revision of an outgoing draft. Doc is
// the item id, from the revision number; hash is the sha256 of the version
// file. The event holds no content.
const OpRevisionAdd = "revision_add"

// revisionRe matches a version file name. A document id never has this shape
// (paramd.ValidStem refuses a dot), so a version file is never a document:
// mdFiles skips it, and so docFiles, lint, and the index.
var revisionRe = regexp.MustCompile(`\.r[0-9]+\.md$`)

// RevisionWrite stores data as <folder>/<id>.r<n>.md next to the document,
// tracks it in the manifest, and logs one event. A file of that number that
// exists is replaced: a failed edit may leave one.
func (v *Vault) RevisionWrite(ctx context.Context, id string, n int, data []byte) (string, error) {
	if n < 1 {
		return "", fmt.Errorf("vault: revision write: revision %d: want 1 or more", n)
	}
	hash := events.Hash(data)
	err := v.withLock(ctx, func(m *events.Manifest) error {
		f, err := v.find(id)
		if err != nil {
			return err
		}
		rel := revisionRel(f.rel, id, n)
		old, rerr := os.ReadFile(v.abs(rel))
		if rerr == nil && events.Hash(old) == hash {
			m.Files[rel] = hash
			return v.saveManifest(*m)
		}
		if err := v.write(rel, data, fileMode); err != nil {
			return err
		}
		undo := func() error {
			if rerr != nil {
				return os.Remove(v.abs(rel))
			}
			return v.write(rel, old, fileMode)
		}
		if err := v.logOrUndo(undo, OpRevisionAdd, id, strconv.Itoa(n), "", hash); err != nil {
			return err
		}
		m.Files[rel] = hash
		return v.saveManifest(*m)
	})
	if err != nil {
		return "", fmt.Errorf("vault: revision write: %w", err)
	}
	return hash, nil
}

// RevisionRead returns the text of revision n of the document id.
func (v *Vault) RevisionRead(_ context.Context, id string, n int) ([]byte, error) {
	f, err := v.find(id)
	if err != nil {
		return nil, fmt.Errorf("vault: revision read: %w", err)
	}
	data, err := os.ReadFile(v.abs(revisionRel(f.rel, id, n)))
	if err != nil {
		return nil, fmt.Errorf("vault: revision read: %w", notFound(err, fmt.Sprintf("%s.r%d", id, n)))
	}
	return data, nil
}

func revisionRel(docRel, id string, n int) string {
	return path.Join(path.Dir(docRel), id+".r"+strconv.Itoa(n)+".md")
}

// revisionsOf lists the version files of a document, as vault paths.
func (v *Vault) revisionsOf(docRel, id string) []string {
	entries, err := os.ReadDir(v.abs(path.Dir(docRel)))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.Type().IsRegular() && strings.HasPrefix(name, id+".r") && revisionRe.MatchString(name) &&
			strings.TrimSuffix(name, revisionRe.FindString(name)) == id {
			out = append(out, path.Join(path.Dir(docRel), name))
		}
	}
	return out
}

// moveRevisions renames each version file of a document to toDir and newID,
// and returns the undo. The manifest follows.
func (v *Vault) moveRevisions(m *events.Manifest, docRel, toDir, id, newID string) (func() error, error) {
	type mv struct{ from, to string }
	var done []mv
	undo := func() error {
		for _, x := range done {
			if err := os.Rename(v.abs(x.to), v.abs(x.from)); err != nil {
				return err
			}
		}
		return nil
	}
	for _, from := range v.revisionsOf(docRel, id) {
		to := path.Join(toDir, newID+strings.TrimPrefix(path.Base(from), id))
		if err := os.MkdirAll(v.abs(toDir), 0o700); err != nil {
			return undo, err
		}
		if err := os.Rename(v.abs(from), v.abs(to)); err != nil {
			return undo, err
		}
		done = append(done, mv{from, to})
		if h, ok := m.Files[from]; ok {
			m.Files[to] = h
		}
		delete(m.Files, from)
	}
	return undo, nil
}
