// Package vault is the only write path into a vault. Each mutation runs under
// the machine's file lock (in the state dir), lints the new file, replaces it atomically, logs
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
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	ashid "github.com/IodeSystems/ashid/go"

	"github.com/iodesystems/para/internal/atomicfile"
	"github.com/iodesystems/para/internal/events"
	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/paramd"
	"github.com/iodesystems/para/internal/props"
	"github.com/iodesystems/para/internal/syncname"
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
	// StateDir holds this machine's lock, manifest, pending-change list, and
	// search index ($PARA_HOME/vaults/<id>/). It must be outside the vault
	// tree: a vault may sit in a file-synced folder, and a lock, a manifest
	// of this machine's view, or SQLite would corrupt or mislead there. It is
	// required unless ReadOnly.
	StateDir string
	// Install is the install id of this machine. It names this machine's event
	// files, so it is required unless ReadOnly. Request ops need it too.
	Install string
	// ReadOnly opens a vault for reading only: no StateDir or Install needed,
	// and every write returns an error.
	ReadOnly bool
	// Warn receives non-fatal notes from reading the event log (a partial last
	// line from a sync in flight). Nil means stderr.
	Warn func(string)
}

// Vault is an open vault.
type Vault struct {
	root string
	id   string
	log  *events.Log
	now  func() time.Time

	stateDir string
	readOnly bool
	actor    string
	surface  string
	install  string

	paths *pathCache // shared by copies made with As
	// format caches format_version (legacy.go); native turns off the
	// reading of a vault below model.GoalsFormat as format 11.
	format *formatCache
	native bool
}

// pathCache maps a document id to its file, so Get does not walk the vault.
// Every walk refreshes it. A hit is used only while the file is still there:
// a sync tool or a hand edit can move or delete files at any time.
//
// refs holds each file's parse, keyed by path (cache.go).
type pathCache struct {
	mu   sync.Mutex
	m    map[string]docFile
	refs map[string]*entry
}

type listed struct {
	hash  string
	typ   model.Type
	title string
	dup   string
	// pinned is the frontmatter pinned value as text.
	pinned string
	// former are the frontmatter former_ids (ids.go).
	former []string
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
	if !o.ReadOnly {
		if o.StateDir == "" {
			return nil, errors.New("vault: open: StateDir is required to write: the lock and manifest are per machine and live outside the vault")
		}
		if o.Install == "" {
			return nil, errors.New("vault: open: Install is required to write: it names this machine's event files")
		}
		if err := os.MkdirAll(o.StateDir, 0o700); err != nil {
			return nil, fmt.Errorf("vault: open: %w", err)
		}
		// git and sync tools keep no empty folder, so a copy may lack them.
		for _, d := range append(trackedDirs(), dirEvents) {
			if err := os.MkdirAll(filepath.Join(root, d), 0o700); err != nil {
				return nil, fmt.Errorf("vault: open: %w", err)
			}
		}
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	log := events.NewLog(filepath.Join(root, dirEvents), events.Source{
		Vault: id, Install: o.Install, Actor: o.Actor, Surface: o.Surface, Now: o.Now, Warn: o.Warn,
	})
	return &Vault{root: root, id: id, log: log, now: o.Now, stateDir: o.StateDir, readOnly: o.ReadOnly, actor: o.Actor, surface: o.Surface, install: o.Install, paths: &pathCache{}, format: &formatCache{}}, nil
}

// Actor, Surface, and Install return the Options they were opened with.
func (v *Vault) Actor() string   { return v.actor }
func (v *Vault) Surface() string { return v.surface }
func (v *Vault) Install() string { return v.install }

// As returns the same open vault acting for another actor and surface. It
// shares the root and the lock file, and costs one struct and one Log, so a
// server can bind the caller per request. Mutual exclusion is the file lock,
// taken per write on its own descriptor, so copies exclude each other as
// processes do.
func (v *Vault) As(actor, surface string) *Vault {
	c := *v
	c.actor, c.surface = actor, surface
	c.log = v.log.As(actor, surface)
	return &c
}

// Now returns the vault clock.
func (v *Vault) Now() time.Time { return v.now() }

// Config returns a vault.properties value, or "" when the key is unset.
func (v *Vault) Config(key string) (string, error) {
	cfg, err := props.Load(filepath.Join(v.root, dirState, "vault.properties"))
	if err != nil {
		return "", fmt.Errorf("vault: config: %w", err)
	}
	return cfg[key], nil
}

// ConfigAll returns every key of vault.properties.
func (v *Vault) ConfigAll() (map[string]string, error) {
	cfg, err := props.Load(filepath.Join(v.root, dirState, "vault.properties"))
	if err != nil {
		return nil, fmt.Errorf("vault: config: %w", err)
	}
	return cfg, nil
}

// ConfigFile returns a file of the vault's .para directory, or nil when there is
// none. name is a bare file name.
func (v *Vault) ConfigFile(name string) ([]byte, error) {
	if name != filepath.Base(name) || strings.HasPrefix(name, ".") {
		return nil, fmt.Errorf("vault: config file %q: want a bare file name", name)
	}
	b, err := os.ReadFile(filepath.Join(v.root, dirState, name))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("vault: config file: %w", err)
	}
	return b, nil
}

// StateDir returns Options.StateDir.
func (v *Vault) StateDir() string { return v.stateDir }

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
	given := d.ID != ""
	if !given {
		d.ID = NewID(d.Meta.Header().Type)
	}
	if !paramd.ValidStem(d.ID) {
		return d, fmt.Errorf("vault: create: invalid id %q", d.ID)
	}
	if given {
		if err := v.notReserved(d.ID); err != nil {
			return d, fmt.Errorf("vault: create: %w", err)
		}
	}
	if d.Meta.Header().Created == "" {
		d.Meta.Header().Created = v.today()
	}
	leg := v.legacy()
	if leg {
		d.Folder = legacyFolder(d.Folder)
	}
	rel := path.Join(string(d.Folder), d.ID+".md")
	data, err := renderChecked(rel, d, nil, leg)
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
		// A folder added after the vault was made, such as io/, may not exist yet.
		if err := os.MkdirAll(filepath.Dir(v.abs(rel)), 0o700); err != nil {
			return err
		}
		if err := v.write(rel, data, fileMode); err != nil {
			return err
		}
		undo := func() error { return os.Remove(v.abs(rel)) }
		if err := v.logOrUndo(undo, events.OpCreate, d.ID, "", string(d.Folder), events.Hash(data)); err != nil {
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
		data, err := renderChecked(f.rel, d, paramd.Parse(f.rel, old).FrontmatterYAML(), v.legacy())
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
		if err := v.undoOnError(undo, v.log.AppendUpdate(events.OpUpdate, d.ID, from, to, events.Hash(data), oldHash)); err != nil {
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
	to = v.folderFor(to)
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
		defer v.forget(f.rel, rel)
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
		undoRevs, err := v.moveRevisions(m, f.rel, string(to), id, id)
		undo := func() error { return errors.Join(undoRevs(), os.Rename(v.abs(rel), v.abs(f.rel))) }
		if err != nil {
			return v.undoOnError(undo, err)
		}
		if err := v.logOrUndo(undo, events.OpMove, id, string(f.folder), string(to), hash); err != nil {
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

// Rename changes the id of a document, which is its file name. The folder and
// content stay. It refuses a new id that another document holds (ids compare
// without case). Links to the old id are not rewritten here; the caller
// updates each document that names it.
func (v *Vault) Rename(ctx context.Context, id, newID string) (model.Document, error) {
	if !paramd.ValidStem(newID) {
		return model.Document{}, fmt.Errorf("vault: rename: invalid id %q", newID)
	}
	if g, ok, err := v.formerOf(newID); err != nil {
		return model.Document{}, fmt.Errorf("vault: rename: %w", err)
	} else if ok && g.id != id {
		return model.Document{}, fmt.Errorf("vault: rename: %w: %s is a former id of %s", model.ErrExists, newID, g.id)
	}
	err := v.withLock(ctx, func(m *events.Manifest) error {
		files, err := v.docFiles()
		if err != nil {
			return err
		}
		var f docFile
		found := false
		for _, x := range files {
			if x.id == id {
				f, found = x, true
			}
		}
		if !found {
			return fmt.Errorf("%w: %s", model.ErrNotFound, id)
		}
		for _, x := range files {
			if x.id != id && strings.EqualFold(x.id, newID) {
				return fmt.Errorf("%w: %s", model.ErrExists, x.rel)
			}
		}
		if id == newID {
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
		rel := path.Join(path.Dir(f.rel), newID+".md")
		defer v.forget(f.rel, rel)
		if err := os.Rename(v.abs(f.rel), v.abs(rel)); err != nil {
			return err
		}
		if err := atomicfile.SyncDir(filepath.Dir(v.abs(rel))); err != nil {
			return err
		}
		undoRevs, err := v.moveRevisions(m, f.rel, path.Dir(f.rel), id, newID)
		undo := func() error { return errors.Join(undoRevs(), os.Rename(v.abs(rel), v.abs(f.rel))) }
		if err != nil {
			return v.undoOnError(undo, err)
		}
		if err := v.logOrUndo(undo, events.OpRename, id, id, newID, hash); err != nil {
			return err
		}
		delete(m.Files, f.rel)
		m.Files[rel] = hash
		return v.saveManifest(*m)
	})
	if err != nil {
		return model.Document{}, fmt.Errorf("vault: rename: %w", err)
	}
	return v.Get(ctx, newID)
}

// Get reads a document.
// An id no file has resolves to the document that lists it in former_ids
// (ids.go); the Document then has its current id.
func (v *Vault) Get(_ context.Context, id string) (model.Document, error) {
	f, err := v.find(id)
	if errors.Is(err, model.ErrNotFound) {
		if g, ok, ferr := v.formerOf(id); ferr != nil {
			err = ferr
		} else if ok {
			f, err = g, nil
		}
	}
	if err != nil {
		return model.Document{}, fmt.Errorf("vault: get: %w", err)
	}
	e, err := v.entryOf(f.rel, v.legacy())
	if err != nil {
		return model.Document{}, fmt.Errorf("vault: get: %w", err)
	}
	d, err := e.doc(f)
	if err != nil {
		return d, fmt.Errorf("vault: get: %w", err)
	}
	return d, nil
}

// List returns one Ref per document, sorted by id. folder and typ filter when
// not empty. A file that does not parse is listed with an empty title or type.
// A type other than receipt skips the receipts folder: it holds only receipts,
// and most of a vault's files.
func (v *Vault) List(ctx context.Context, folder model.Folder, typ model.Type) ([]model.Ref, error) {
	return v.ListTypes(ctx, folder, typ)
}

// ListTypes is List for one pass over several types: typ and more, or every
// type when both are empty. It reads the vault once.
func (v *Vault) ListTypes(_ context.Context, folder model.Folder, typ model.Type, more ...model.Type) ([]model.Ref, error) {
	want := more
	if typ != "" {
		want = append([]model.Type{typ}, more...)
	}
	var skip model.Folder
	if len(want) > 0 && !slices.Contains(want, model.TypeReceipt) {
		skip = model.FolderOf(model.TypeReceipt)
	}
	files, err := v.scanFiles(skip)
	if err != nil {
		return nil, fmt.Errorf("vault: list: %w", err)
	}
	out := []model.Ref{}
	leg := v.legacy()
	for _, f := range files {
		if folder != "" && f.folder != folder {
			continue
		}
		e, err := v.entryOf(f.rel, leg)
		if err != nil {
			return nil, fmt.Errorf("vault: list: %w", err)
		}
		l := e.l
		lt := typeOf(l.typ, leg)
		if len(want) > 0 && !slices.Contains(want, lt) {
			continue
		}
		out = append(out, model.Ref{ID: f.id, Folder: f.folder, Type: lt, Title: l.title, Rev: l.hash, Pinned: l.pinned,
			Hidden: f.folder == model.FolderArchive || strings.TrimSpace(l.dup) != ""})
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
	out, err := lintFiles(v.root, rels, v.legacy())
	if err != nil {
		return nil, fmt.Errorf("vault: check: %w", err)
	}
	conflicts, err := v.Conflicts()
	if err != nil {
		return nil, fmt.Errorf("vault: check: %w", err)
	}
	lost, err := v.LostUpdates()
	if err != nil {
		return nil, fmt.Errorf("vault: check: %w", err)
	}
	return append(append(out, conflictFindings(conflicts)...), lostFindings(lost)...), nil
}

// DefaultGrace is how long reconcile holds a change that no logged event
// explains. vault.properties key reconcile.grace (a Go duration, "0s" for
// none) overrides it.
const DefaultGrace = events.PendingGrace

func (v *Vault) grace() (time.Duration, error) {
	val, err := v.Config("reconcile.grace")
	if err != nil || val == "" {
		return DefaultGrace, err
	}
	d, err := time.ParseDuration(val)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("vault: reconcile.grace %q is not a duration", val)
	}
	return d, nil
}

// Reconcile accepts files changed outside this package. A file whose hash a
// logged event from any install carries is another machine's write and logs
// nothing. Any other change is held for the grace period (see
// events.Reconcile), then logged as one external_edit. With no manifest, the
// current files become the baseline.
func (v *Vault) Reconcile(ctx context.Context) (model.ReconcileResult, error) {
	res := model.ReconcileResult{Changes: []model.Change{}}
	grace, err := v.grace()
	if err != nil {
		return res, err
	}
	err = v.lock(ctx, func() error {
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
		p, err := events.LoadPending(v.pendingPath())
		if err != nil {
			return err
		}
		changes, err := events.Reconcile(v.log, v.root, trackedDirs(), &m, p, v.now(), grace)
		if err != nil {
			return err
		}
		if err := p.Save(v.pendingPath()); err != nil {
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
	if res.Relinked, err = v.relinkChanged(ctx, res.Changes); err != nil {
		return res, fmt.Errorf("vault: reconcile: %w", err)
	}
	return res, nil
}

func (v *Vault) today() model.Date { return model.Date(v.now().Format("2006-01-02")) }

func (v *Vault) abs(rel string) string { return filepath.Join(v.root, filepath.FromSlash(rel)) }

func (v *Vault) manifestPath() string { return filepath.Join(v.stateDir, "manifest.json") }

func (v *Vault) pendingPath() string { return filepath.Join(v.stateDir, "pending.json") }

func (v *Vault) saveManifest(m events.Manifest) error {
	return m.Save(v.manifestPath())
}

func (v *Vault) write(rel string, data []byte, mode fs.FileMode) error {
	defer v.forget(rel)
	return atomicfile.Write(v.abs(rel), data, mode)
}

func (v *Vault) lock(ctx context.Context, fn func() error) error {
	if v.readOnly {
		return errors.New("vault: opened read-only")
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, lockWait)
		defer cancel()
	}
	release, err := acquire(ctx, filepath.Join(v.stateDir, "lock"))
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
func (v *Vault) logOrUndo(undo func() error, op, doc, from, to, hash string) error {
	return v.undoOnError(undo, v.log.AppendHash(op, doc, from, to, hash))
}

func (v *Vault) undoOnError(undo func() error, err error) error {
	if err == nil {
		return nil
	}
	if uerr := undo(); uerr != nil {
		return errors.Join(err, fmt.Errorf("undo failed: %w", uerr))
	}
	return err
}

// noteExternalEdit handles a file that differs from the manifest just before a
// mutation absorbs it. A hash that a logged event from any install carries is
// another machine's write: nothing to log. Otherwise it is logged as a hand
// edit now (the caller has read the file, so the grace period does not apply)
// and any held pending entry is cleared.
func (v *Vault) noteExternalEdit(m *events.Manifest, rel, hash string) error {
	old, ok := m.Files[rel]
	if ok && old == hash {
		return nil
	}
	known, err := v.log.LoadKnown()
	if err != nil {
		return err
	}
	if !known.Has(hash) {
		if err := events.LogExternalEdit(v.log, rel, old, hash); err != nil {
			return err
		}
	}
	p, err := events.LoadPending(v.pendingPath())
	if err != nil {
		return err
	}
	if _, held := p[rel]; held {
		delete(p, rel)
		return p.Save(v.pendingPath())
	}
	return nil
}

type docFile struct {
	rel    string // slash path relative to the vault root
	id     string
	folder model.Folder
}

// docFiles lists every .md file under the document folders.
func (v *Vault) docFiles() ([]docFile, error) { return v.scanFiles("") }

// scanFiles is docFiles without the folder skip, when set. A partial scan leaves the
// path cache as it is.
func (v *Vault) scanFiles(skip model.Folder) ([]docFile, error) {
	var out []docFile
	for _, folder := range model.Folders {
		if folder == skip {
			continue
		}
		rels, err := mdFiles(v.root, string(folder))
		if err != nil {
			return nil, err
		}
		for _, rel := range rels {
			out = append(out, docFile{rel: rel, id: strings.TrimSuffix(path.Base(rel), ".md"), folder: folder})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].id < out[j].id })
	if skip != "" {
		return out, nil
	}
	m := make(map[string]docFile, len(out))
	for _, f := range out {
		if _, dup := m[f.id]; !dup {
			m[f.id] = f
		}
	}
	v.paths.mu.Lock()
	v.paths.m = m
	for rel := range v.paths.refs {
		if f, ok := m[strings.TrimSuffix(path.Base(rel), ".md")]; !ok || f.rel != rel {
			delete(v.paths.refs, rel)
		}
	}
	v.paths.mu.Unlock()
	return out, nil
}

func (v *Vault) find(id string) (docFile, error) {
	v.paths.mu.Lock()
	f, ok := v.paths.m[id]
	v.paths.mu.Unlock()
	if ok {
		if st, err := os.Stat(v.abs(f.rel)); err == nil && st.Mode().IsRegular() {
			return f, nil
		}
	}
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
		case d.IsDir() || !d.Type().IsRegular() || !strings.HasSuffix(d.Name(), ".md") || syncname.IsConflict(d.Name()) || revisionRe.MatchString(d.Name()):
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

// decodeFile decodes the frontmatter paramd already parsed. Invalid YAML goes
// through model.Decode so the error is the YAML error. legacy reads a file
// below model.GoalsFormat as format 11 (legacy.go).
func decodeFile(f docFile, pf *paramd.File, legacy bool) (model.Document, error) {
	if legacy {
		legacyParsed(pf)
	}
	if pf.FMNode == nil {
		return model.Decode(f.id, f.folder, pf.FrontmatterYAML(), pf.Title, bodyAfterTitle(pf))
	}
	return model.DecodeNode(f.id, f.folder, pf.FMNode, pf.Title, bodyAfterTitle(pf))
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
