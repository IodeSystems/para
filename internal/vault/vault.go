// Package vault is the only write path into a vault. Each mutation runs under
// the file lock in .para/, lints the new file, replaces it atomically, logs
// one event, and updates the manifest.
package vault

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	ashid "github.com/IodeSystems/ashid/go"

	"github.com/iodesystems/paradize/internal/atomicfile"
	"github.com/iodesystems/paradize/internal/events"
	"github.com/iodesystems/paradize/internal/model"
	"github.com/iodesystems/paradize/internal/paramd"
	"github.com/iodesystems/paradize/internal/props"
)

// lockWait bounds the wait for the lock when ctx has no deadline.
const lockWait = 30 * time.Second

const fileMode fs.FileMode = 0o600

// Options configures Open.
type Options struct {
	ID      string // when set, the vault's own id must match
	Actor   string
	Surface string
	Now     func() time.Time // nil means time.Now
}

// Vault is an open vault.
type Vault struct {
	root string
	id   string
	log  *events.Log
	now  func() time.Time
}

// Open opens the vault at root and restores any missing folder.
func Open(root string, o Options) (*Vault, error) {
	cfg, err := props.Load(filepath.Join(root, dirState, "vault.properties"))
	if err != nil {
		return nil, fmt.Errorf("vault: open: %s is not a vault: %w", root, err)
	}
	id := cfg["vault.id"]
	if id == "" || (o.ID != "" && o.ID != id) {
		return nil, fmt.Errorf("vault: open: %s has id %q, want %q", root, id, o.ID)
	}
	// git keeps no empty folder and .para/tmp is ignored, so a clone lacks them.
	for _, d := range append(trackedDirs(), dirEvents, filepath.Join(dirState, "tmp")) {
		if err := os.MkdirAll(filepath.Join(root, d), 0o700); err != nil {
			return nil, fmt.Errorf("vault: open: %w", err)
		}
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	log := events.NewLog(filepath.Join(root, dirEvents), events.Source{
		Vault: id, Actor: o.Actor, Surface: o.Surface, Now: o.Now,
	})
	return &Vault{root: root, id: id, log: log, now: o.Now}, nil
}

// ID returns the vault id.
func (v *Vault) ID() string { return v.id }

// Log returns the vault's event log.
func (v *Vault) Log() *events.Log { return v.log }

// NewID returns a generated document id: <type>-<ashid>. It holds no document
// text, because the id goes into events.
func NewID(t model.Type) string {
	return strings.ReplaceAll(string(t), "_", "-") + "-" + strings.ToLower(ashid.New())
}

// Create writes a new document. An empty ID is generated. Created is set when
// empty.
func (v *Vault) Create(ctx context.Context, d model.Document) (model.Document, error) {
	if !d.Folder.Valid() {
		return d, fmt.Errorf("vault: create: unknown folder %q", d.Folder)
	}
	if d.Meta == nil {
		return d, errors.New("vault: create: document has no meta")
	}
	if d.ID == "" {
		d.ID = NewID(d.Meta.Header().Type)
	}
	if !paramd.ValidStem(d.ID) {
		return d, fmt.Errorf("vault: create: invalid id %q", d.ID)
	}
	if d.Meta.Header().Created == "" {
		d.Meta.Header().Created = v.today()
	}
	rel := path.Join(string(d.Folder), d.ID+".md")
	data, err := renderChecked(rel, d)
	if err != nil {
		return d, fmt.Errorf("vault: create: %w", err)
	}
	err = v.withLock(ctx, func(m *events.Manifest) error {
		existing, err := v.docFiles()
		if err != nil {
			return err
		}
		for _, f := range existing {
			if strings.EqualFold(f.id, d.ID) {
				return fmt.Errorf("%w: %s", model.ErrExists, f.rel)
			}
		}
		if err := v.write(rel, data, fileMode); err != nil {
			return err
		}
		undo := func() error { return os.Remove(v.abs(rel)) }
		if err := v.logOrUndo(undo, events.OpCreate, d.ID, "", string(d.Folder)); err != nil {
			return err
		}
		m.Files[rel] = events.Hash(data)
		return v.saveManifest(*m)
	})
	if err != nil {
		return d, fmt.Errorf("vault: create: %w", err)
	}
	d.Rev = events.Hash(data)
	return d, nil
}

// Update replaces a document in its current folder. d.Rev must equal the
// current content hash, or the call returns model.ErrConflict. Updated is set
// to today. A status change is logged as from and to.
func (v *Vault) Update(ctx context.Context, d model.Document) (model.Document, error) {
	if d.Meta == nil {
		return d, errors.New("vault: update: document has no meta")
	}
	d.Meta.Header().Updated = v.today()
	err := v.withLock(ctx, func(m *events.Manifest) error {
		f, err := v.find(d.ID)
		if err != nil {
			return err
		}
		old, err := os.ReadFile(v.abs(f.rel))
		if err != nil {
			return err
		}
		oldHash := events.Hash(old)
		if d.Rev != oldHash {
			return fmt.Errorf("%w: %s", model.ErrConflict, f.rel)
		}
		d.Folder = f.folder
		data, err := renderChecked(f.rel, d)
		if err != nil {
			return err
		}
		if err := v.noteExternalEdit(m, f.rel, oldHash); err != nil {
			return err
		}
		st, err := os.Stat(v.abs(f.rel))
		if err != nil {
			return err
		}
		mode := st.Mode().Perm()
		if err := v.write(f.rel, data, mode); err != nil {
			return err
		}
		undo := func() error { return v.write(f.rel, old, mode) }
		from, to := statusChange(f.rel, old, data)
		if err := v.logOrUndo(undo, events.OpUpdate, d.ID, from, to); err != nil {
			return err
		}
		d.Rev = events.Hash(data)
		m.Files[f.rel] = d.Rev
		return v.saveManifest(*m)
	})
	if err != nil {
		return d, fmt.Errorf("vault: update: %w", err)
	}
	return d, nil
}

// Move moves a document to another folder. The file name does not change, so
// no link breaks. A move to the current folder does nothing.
func (v *Vault) Move(ctx context.Context, id string, to model.Folder) (model.Document, error) {
	if !to.Valid() {
		return model.Document{}, fmt.Errorf("vault: move: unknown folder %q", to)
	}
	err := v.withLock(ctx, func(m *events.Manifest) error {
		f, err := v.find(id)
		if err != nil {
			return err
		}
		if f.folder == to {
			return nil
		}
		data, err := os.ReadFile(v.abs(f.rel))
		if err != nil {
			return err
		}
		hash := events.Hash(data)
		if err := v.noteExternalEdit(m, f.rel, hash); err != nil {
			return err
		}
		rel := path.Join(string(to), path.Base(f.rel))
		if err := os.MkdirAll(filepath.Dir(v.abs(rel)), 0o700); err != nil {
			return err
		}
		if err := os.Rename(v.abs(f.rel), v.abs(rel)); err != nil {
			return err
		}
		if err := atomicfile.SyncDir(filepath.Dir(v.abs(rel))); err != nil {
			return err
		}
		if err := atomicfile.SyncDir(filepath.Dir(v.abs(f.rel))); err != nil {
			return err
		}
		undo := func() error { return os.Rename(v.abs(rel), v.abs(f.rel)) }
		if err := v.logOrUndo(undo, events.OpMove, id, string(f.folder), string(to)); err != nil {
			return err
		}
		delete(m.Files, f.rel)
		m.Files[rel] = hash
		return v.saveManifest(*m)
	})
	if err != nil {
		return model.Document{}, fmt.Errorf("vault: move: %w", err)
	}
	return v.Get(ctx, id)
}

// Get reads a document.
func (v *Vault) Get(_ context.Context, id string) (model.Document, error) {
	f, err := v.find(id)
	if err != nil {
		return model.Document{}, fmt.Errorf("vault: get: %w", err)
	}
	data, err := os.ReadFile(v.abs(f.rel))
	if err != nil {
		return model.Document{}, fmt.Errorf("vault: get: %w", err)
	}
	pf := paramd.Parse(f.rel, data)
	d, err := model.Decode(f.id, f.folder, pf.FrontmatterYAML(), pf.Title, bodyAfterTitle(pf))
	if err != nil {
		return d, fmt.Errorf("vault: get: %w", err)
	}
	d.Rev = events.Hash(data)
	return d, nil
}

// List returns one Ref per document, sorted by id. folder and typ filter when
// not empty. A file that does not parse is listed with an empty title or type.
func (v *Vault) List(_ context.Context, folder model.Folder, typ model.Type) ([]model.Ref, error) {
	files, err := v.docFiles()
	if err != nil {
		return nil, fmt.Errorf("vault: list: %w", err)
	}
	out := []model.Ref{}
	for _, f := range files {
		if folder != "" && f.folder != folder {
			continue
		}
		data, err := os.ReadFile(v.abs(f.rel))
		if err != nil {
			return nil, fmt.Errorf("vault: list: %w", err)
		}
		pf := paramd.Parse(f.rel, data)
		if typ != "" && model.Type(pf.Type()) != typ {
			continue
		}
		out = append(out, model.Ref{ID: f.id, Folder: f.folder, Type: model.Type(pf.Type()), Title: pf.Title, Rev: events.Hash(data)})
	}
	return out, nil
}

// Check lints every document and runs the vault-wide checks. templates/ and
// events/ are not documents.
func (v *Vault) Check(_ context.Context) ([]model.Finding, error) {
	files, err := v.docFiles()
	if err != nil {
		return nil, fmt.Errorf("vault: check: %w", err)
	}
	rels := make([]string, 0, len(files))
	for _, f := range files {
		rels = append(rels, f.rel)
	}
	out, err := lintFiles(v.root, rels)
	if err != nil {
		return nil, fmt.Errorf("vault: check: %w", err)
	}
	return out, nil
}

// Reconcile logs an external_edit for each file changed outside this package
// and accepts it. With no manifest, the current files become the baseline.
func (v *Vault) Reconcile(ctx context.Context) (model.ReconcileResult, error) {
	res := model.ReconcileResult{Changes: []model.Change{}}
	err := v.lock(ctx, func() error {
		m, found, err := events.LoadManifest(v.manifestPath())
		if err != nil {
			return err
		}
		if !found {
			res.Baseline = true
			if m.Files, err = events.Scan(v.root, trackedDirs()); err != nil {
				return err
			}
			return v.saveManifest(m)
		}
		changes, err := events.Reconcile(v.log, v.root, trackedDirs(), &m)
		if err != nil {
			return err
		}
		if len(changes) == 0 {
			return nil
		}
		res.Changes = changes
		return v.saveManifest(m)
	})
	if err != nil {
		return res, fmt.Errorf("vault: reconcile: %w", err)
	}
	return res, nil
}

func (v *Vault) today() model.Date { return model.Date(v.now().Format("2006-01-02")) }

func (v *Vault) abs(rel string) string { return filepath.Join(v.root, filepath.FromSlash(rel)) }

func (v *Vault) tmpDir() string { return filepath.Join(v.root, dirState, "tmp") }

func (v *Vault) manifestPath() string { return filepath.Join(v.root, dirState, "manifest.json") }

func (v *Vault) saveManifest(m events.Manifest) error {
	return m.Save(v.manifestPath(), v.tmpDir())
}

func (v *Vault) write(rel string, data []byte, mode fs.FileMode) error {
	return atomicfile.Write(v.abs(rel), v.tmpDir(), data, mode)
}

func (v *Vault) lock(ctx context.Context, fn func() error) error {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, lockWait)
		defer cancel()
	}
	release, err := acquire(ctx, filepath.Join(v.root, dirState, "lock"))
	if err != nil {
		return err
	}
	defer release()
	return fn()
}

// withLock runs fn under the lock with the manifest loaded. A missing
// manifest (fresh clone) is rebuilt from the files.
func (v *Vault) withLock(ctx context.Context, fn func(*events.Manifest) error) error {
	return v.lock(ctx, func() error {
		m, found, err := events.LoadManifest(v.manifestPath())
		if err != nil {
			return err
		}
		if !found {
			if m.Files, err = events.Scan(v.root, trackedDirs()); err != nil {
				return err
			}
		}
		return fn(&m)
	})
}

// logOrUndo logs the mutation. If the log fails, it reverses the file change,
// so a change without an event does not stay on disk.
func (v *Vault) logOrUndo(undo func() error, op, doc, from, to string) error {
	err := v.log.Append(op, doc, from, to)
	if err == nil {
		return nil
	}
	if uerr := undo(); uerr != nil {
		return errors.Join(err, fmt.Errorf("undo failed: %w", uerr))
	}
	return err
}

// noteExternalEdit logs a hand edit that a mutation is about to absorb, so
// reconcile does not lose it.
func (v *Vault) noteExternalEdit(m *events.Manifest, rel, hash string) error {
	if old, ok := m.Files[rel]; !ok || old != hash {
		return events.LogExternalEdit(v.log, rel, old, hash)
	}
	return nil
}

type docFile struct {
	rel    string // slash path relative to the vault root
	id     string
	folder model.Folder
}

// docFiles lists every .md file under the document folders.
func (v *Vault) docFiles() ([]docFile, error) {
	var out []docFile
	for _, folder := range model.Folders {
		rels, err := mdFiles(v.root, string(folder))
		if err != nil {
			return nil, err
		}
		for _, rel := range rels {
			out = append(out, docFile{rel: rel, id: strings.TrimSuffix(path.Base(rel), ".md"), folder: folder})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	return out, nil
}

func (v *Vault) find(id string) (docFile, error) {
	files, err := v.docFiles()
	if err != nil {
		return docFile{}, err
	}
	for _, f := range files {
		if f.id == id {
			return f, nil
		}
	}
	return docFile{}, fmt.Errorf("%w: %s", model.ErrNotFound, id)
}

// mdFiles returns the regular .md files under dir, relative to root.
func mdFiles(root, dir string) ([]string, error) {
	var out []string
	err := filepath.WalkDir(filepath.Join(root, dir), func(p string, d fs.DirEntry, err error) error {
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return nil
		case err != nil:
			return err
		case d.IsDir() && strings.HasPrefix(d.Name(), "."):
			return filepath.SkipDir
		case d.IsDir() || !d.Type().IsRegular() || !strings.HasSuffix(d.Name(), ".md"):
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	return out, err
}

func bodyAfterTitle(f *paramd.File) string {
	if f.TitleLine == 0 {
		return f.Body
	}
	var b strings.Builder
	for _, l := range f.Lines[f.TitleLine:] {
		b.WriteString(l.Text)
		b.WriteByte('\n')
	}
	return strings.Trim(b.String(), "\n")
}

func statusChange(rel string, before, after []byte) (from, to string) {
	a, _ := paramd.Parse(rel, before).Frontmatter["status"].(string)
	b, _ := paramd.Parse(rel, after).Frontmatter["status"].(string)
	if a == b {
		return "", ""
	}
	return a, b
}
