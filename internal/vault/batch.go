package vault

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/iodesystems/parasolve/internal/atomicfile"
	"github.com/iodesystems/parasolve/internal/events"
	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/paramd"
)

// BatchResult is the outcome for one document of CreateBatch. A document that
// fails lint or whose id is taken has Err set and is not written.
type BatchResult struct {
	ID     string
	Folder model.Folder
	Rev    string
	Err    error
}

// ErrBatchChanged is returned by RemoveBatch when a document of the batch
// changed after the import. Nothing is removed.
var ErrBatchChanged = errors.New("a document of the batch changed since the import")

// CreateBatch writes many new documents under one lock, with one manifest
// save and one event write. Each document needs an id. A document that fails
// lint or collides with an existing id gets an Err and the rest are written.
// Files are written without a per-file fsync, then synced together; one
// "import" event per document follows, carrying the batch id and the file
// hash, never content. If the events cannot be written, the files are removed.
func (v *Vault) CreateBatch(ctx context.Context, batch string, docs []model.Document) ([]BatchResult, error) {
	res := make([]BatchResult, len(docs))
	err := v.withLock(ctx, func(m *events.Manifest) error {
		existing, err := v.docFiles()
		if err != nil {
			return err
		}
		taken := make(map[string]bool, len(existing)+len(docs))
		for _, f := range existing {
			taken[strings.ToLower(f.id)] = true
		}
		type planned struct {
			i    int
			rel  string
			data []byte
		}
		var todo []planned
		for i, d := range docs {
			res[i] = BatchResult{ID: d.ID, Folder: d.Folder}
			switch {
			case !d.Folder.Valid():
				res[i].Err = fmt.Errorf("unknown folder %q", d.Folder)
				continue
			case d.Meta == nil:
				res[i].Err = errors.New("document has no meta")
				continue
			case !paramd.ValidStem(d.ID):
				res[i].Err = fmt.Errorf("invalid id %q", d.ID)
				continue
			case taken[strings.ToLower(d.ID)]:
				res[i].Err = fmt.Errorf("%w: %s", model.ErrExists, d.ID)
				continue
			}
			if d.Meta.Header().Created == "" {
				d.Meta.Header().Created = v.today()
			}
			rel := path.Join(string(d.Folder), d.ID+".md")
			data, err := renderChecked(rel, d, nil)
			if err != nil {
				res[i].Err = err
				continue
			}
			taken[strings.ToLower(d.ID)] = true
			res[i].Rev = events.Hash(data)
			todo = append(todo, planned{i, rel, data})
		}
		written := make([]string, 0, len(todo))
		cleanup := func() {
			for _, rel := range written {
				_ = os.Remove(v.abs(rel))
			}
		}
		dirs := map[string]bool{}
		for _, p := range todo {
			if err := v.writeNoSync(p.rel, p.data); err != nil {
				cleanup()
				return err
			}
			written = append(written, p.rel)
			dirs[filepath.Dir(v.abs(p.rel))] = true
		}
		var abs []string
		for _, rel := range written {
			abs = append(abs, v.abs(rel))
		}
		dirList := make([]string, 0, len(dirs))
		for d := range dirs {
			dirList = append(dirList, d)
		}
		if err := syncBatch(v.root, abs, dirList); err != nil {
			cleanup()
			return err
		}
		recs := make([]events.Rec, 0, len(todo))
		for _, p := range todo {
			recs = append(recs, events.Rec{Op: events.OpImport, Doc: docs[p.i].ID, From: batch, To: string(docs[p.i].Folder), Hash: res[p.i].Rev})
		}
		if err := v.log.AppendMany(recs); err != nil {
			cleanup()
			return err
		}
		for _, p := range todo {
			m.Files[p.rel] = res[p.i].Rev
		}
		return v.saveManifest(*m)
	})
	if err != nil {
		return nil, fmt.Errorf("vault: create batch: %w", err)
	}
	for i := range res {
		if res[i].Err == nil {
			docs[i].Rev = res[i].Rev
		}
	}
	return res, nil
}

// writeNoSync writes rel through a dotted temp file and a rename, so a reader
// sees the old file or the whole new one, without an fsync. The caller syncs.
func (v *Vault) writeNoSync(rel string, data []byte) error {
	dst := v.abs(rel)
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".tmp-*")
	if err != nil {
		return err
	}
	if _, err = tmp.Write(data); err == nil {
		err = tmp.Chmod(fileMode)
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), dst)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
	}
	return err
}

func syncFile(p string) error {
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// ImportedDoc is a document that records where it came from: a source.url
// that starts with file://, or an imported_id.
type ImportedDoc struct {
	ID         string       `json:"id"`
	Folder     model.Folder `json:"folder"`
	Type       string       `json:"type"`
	SourceURL  string       `json:"source_url,omitempty"`
	SourceHash string       `json:"source_hash,omitempty"`
	ImportedID string       `json:"imported_id,omitempty"`
}

// ImportedDocs lists documents with import provenance, and every id in the
// vault (lower case) so an importer can avoid a collision.
func (v *Vault) ImportedDocs(_ context.Context) (docs []ImportedDoc, ids map[string]bool, err error) {
	files, err := v.docFiles()
	if err != nil {
		return nil, nil, fmt.Errorf("vault: imported: %w", err)
	}
	ids = make(map[string]bool, len(files))
	for _, f := range files {
		ids[strings.ToLower(f.id)] = true
		data, err := os.ReadFile(v.abs(f.rel))
		if err != nil {
			return nil, nil, fmt.Errorf("vault: imported: %w", err)
		}
		fm := paramd.Parse(f.rel, data).Frontmatter
		d := ImportedDoc{ID: f.id, Folder: f.folder}
		d.Type, _ = fm["type"].(string)
		d.ImportedID, _ = fm["imported_id"].(string)
		if src, ok := fm["source"].(map[string]any); ok {
			if u, _ := src["url"].(string); strings.HasPrefix(u, "file://") {
				d.SourceURL = u
				d.SourceHash, _ = src["hash"].(string)
			}
		}
		if d.SourceURL != "" || d.ImportedID != "" {
			docs = append(docs, d)
		}
	}
	return docs, ids, nil
}

// RemoveResult says what RemoveBatch did. Changed lists documents whose
// content differs from what the import wrote; with any, nothing is removed.
type RemoveResult struct {
	Removed []string `json:"removed"`
	Missing []string `json:"missing"`
	Changed []string `json:"changed"`
}

// RemoveBatch removes the documents an import batch created, unless one of
// them changed since. A document already gone is listed as missing. It logs
// one import_undo event per removed document.
func (v *Vault) RemoveBatch(ctx context.Context, batch string) (RemoveResult, error) {
	res := RemoveResult{Removed: []string{}, Missing: []string{}, Changed: []string{}}
	err := v.withLock(ctx, func(m *events.Manifest) error {
		all, err := v.log.All()
		if err != nil {
			return err
		}
		want := map[string]string{} // doc -> hash the import wrote
		var order []string
		undone := map[string]bool{}
		for _, e := range all {
			if e.From != batch {
				continue
			}
			switch e.Op {
			case events.OpImport:
				if _, dup := want[e.Doc]; !dup {
					order = append(order, e.Doc)
				}
				want[e.Doc] = e.Hash
			case events.OpImportUndo:
				undone[e.Doc] = true
			}
		}
		if len(order) == 0 {
			return fmt.Errorf("no import batch %q in the event log", batch)
		}
		files, err := v.docFiles()
		if err != nil {
			return err
		}
		byID := map[string]docFile{}
		for _, f := range files {
			byID[f.id] = f
		}
		var remove []docFile
		var recs []events.Rec
		for _, id := range order {
			if undone[id] {
				continue
			}
			f, ok := byID[id]
			if !ok {
				res.Missing = append(res.Missing, id)
				continue
			}
			data, err := os.ReadFile(v.abs(f.rel))
			if err != nil {
				return err
			}
			h := events.Hash(data)
			if h != want[id] {
				res.Changed = append(res.Changed, id)
				continue
			}
			remove = append(remove, f)
			recs = append(recs, events.Rec{Op: events.OpImportUndo, Doc: id, From: batch, To: string(f.folder), Hash: h})
		}
		if len(res.Changed) > 0 {
			return ErrBatchChanged
		}
		dirs := map[string]bool{}
		for _, f := range remove {
			if err := os.Remove(v.abs(f.rel)); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			dirs[filepath.Dir(v.abs(f.rel))] = true
			delete(m.Files, f.rel)
			res.Removed = append(res.Removed, f.id)
		}
		for d := range dirs {
			if err := atomicfile.SyncDir(d); err != nil {
				return err
			}
		}
		if err := v.log.AppendMany(recs); err != nil {
			return err
		}
		return v.saveManifest(*m)
	})
	if err != nil {
		return res, fmt.Errorf("vault: remove batch: %w", err)
	}
	return res, nil
}
