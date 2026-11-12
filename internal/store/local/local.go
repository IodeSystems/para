// Package local implements store.Store over vault and events.
package local

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/egress"
	"github.com/iodesystems/para/internal/events"
	"github.com/iodesystems/para/internal/index"
	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
	"github.com/iodesystems/para/internal/vault"
)

// Store is a vault on this machine.
type Store struct {
	v    *vault.Vault
	log  *events.Log
	root string
}

var _ store.Store = (*Store)(nil)

// Open opens the vault at root.
func Open(root string, o vault.Options) (*Store, error) {
	v, err := vault.Open(root, o)
	if err != nil {
		return nil, fmt.Errorf("local: open: %w", err)
	}
	return &Store{v: v, log: v.Log(), root: root}, nil
}

// As returns the Store acting for another actor and surface, without opening
// the vault again.
func (s *Store) As(actor, surface string) *Store {
	v := s.v.As(actor, surface)
	return &Store{v: v, log: v.Log(), root: s.root}
}

// Native returns the Store without the reading of a vault below
// model.GoalsFormat as format 11 (vault.Native).
func (s *Store) Native() store.Store {
	v := s.v.Native()
	return &Store{v: v, log: v.Log(), root: s.root}
}

// ActAs is As behind the store.Store interface.
func (s *Store) ActAs(actor, surface string) store.Store { return s.As(actor, surface) }

func (s *Store) VaultID() string { return s.v.ID() }

// DocAt maps a path to a document id by its text alone: the cleaned path
// must lie under the root, and its folder and file name must be those of a
// document. A path outside the root is refused before any file is touched.
func (s *Store) DocAt(ctx context.Context, path string) (string, error) {
	p := filepath.Clean(filepath.FromSlash(path))
	if filepath.IsAbs(p) {
		rel := ""
		for _, root := range s.roots() {
			if r, err := filepath.Rel(root, p); err == nil && r != ".." && !strings.HasPrefix(r, ".."+string(filepath.Separator)) {
				rel = r
				break
			}
		}
		if rel == "" {
			return "", store.ErrOutsideVault
		}
		p = rel
	}
	if p == ".." || strings.HasPrefix(p, ".."+string(filepath.Separator)) {
		return "", store.ErrOutsideVault
	}
	dir, file := filepath.Split(p)
	id := strings.TrimSuffix(file, ".md")
	if id == "" || strings.HasPrefix(id, ".") {
		return "", fmt.Errorf("%s: %w", path, model.ErrNotFound)
	}
	doc, err := s.v.Get(ctx, id)
	if err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	if filepath.Clean(dir) != filepath.Clean(filepath.FromSlash(string(doc.Folder))) {
		return "", fmt.Errorf("%s: %w", path, model.ErrNotFound)
	}
	return doc.ID, nil
}

// roots is the vault root as opened and with links resolved.
func (s *Store) roots() []string {
	out := []string{filepath.Clean(s.root)}
	if abs, err := filepath.Abs(s.root); err == nil {
		out = append(out, abs)
	}
	if real, err := filepath.EvalSymlinks(s.root); err == nil {
		out = append(out, real)
	}
	return out
}

func (s *Store) Info() store.Info {
	return store.Info{Actor: s.v.Actor(), Surface: s.v.Surface(), Install: s.v.Install(), StateDir: s.v.StateDir()}
}

func (s *Store) Now() time.Time { return s.v.Now() }

func (s *Store) Config(key string) (string, error) { return s.v.Config(key) }

func (s *Store) ConfigAll() (map[string]string, error) { return s.v.ConfigAll() }

func (s *Store) UnsetConfig(ctx context.Context, key string) error {
	return s.v.UnsetConfig(ctx, key)
}

func (s *Store) ConfigFile(name string) ([]byte, error) { return s.v.ConfigFile(name) }

func (s *Store) Create(ctx context.Context, d model.Document) (model.Document, error) {
	return s.v.Create(ctx, d)
}

func (s *Store) Get(ctx context.Context, id string) (model.Document, error) {
	return s.v.Get(ctx, id)
}

func (s *Store) List(ctx context.Context, f store.Filter) ([]model.Ref, error) {
	refs, err := s.v.ListTypes(ctx, f.Folder, f.Type, f.Types...)
	if err != nil || f.All || f.Folder == model.FolderArchive {
		return refs, err
	}
	return slices.DeleteFunc(refs, func(r model.Ref) bool { return r.Hidden }), nil
}

func (s *Store) Update(ctx context.Context, d model.Document) (model.Document, error) {
	return s.v.Update(ctx, d)
}

func (s *Store) Move(ctx context.Context, id string, to model.Folder) (model.Document, error) {
	return s.v.Move(ctx, id, to)
}

func (s *Store) Rename(ctx context.Context, id, newID string) (model.Document, error) {
	return s.v.Rename(ctx, id, newID)
}

func (s *Store) ChangeIDs(ctx context.Context, ids map[string]string) (model.IDChangeResult, error) {
	return s.v.ChangeIDs(ctx, ids)
}

func (s *Store) RelinkFormer(ctx context.Context) (model.IDChangeResult, error) {
	return s.v.RelinkFormer(ctx)
}

func (s *Store) Snapshot(ctx context.Context, name string) (string, error) {
	return s.v.Snapshot(ctx, name)
}

func (s *Store) SetConfig(ctx context.Context, key, value string) error {
	return s.v.SetConfig(ctx, key, value)
}

func (s *Store) Rewrite(ctx context.Context, batch string, dry bool, fn model.RewriteFunc) (model.BatchResult, error) {
	return s.v.Rewrite(ctx, batch, dry, fn)
}

func (s *Store) RewriteExpand(ctx context.Context, batch string, dry bool, fn model.ExpandFunc) (model.BatchResult, error) {
	return s.v.RewriteExpand(ctx, batch, dry, fn)
}

func (s *Store) UndoBatch(ctx context.Context, batch string) (model.BatchResult, error) {
	return s.v.UndoBatch(ctx, batch)
}

func (s *Store) Lint(ctx context.Context) ([]model.Finding, error) { return s.v.Check(ctx) }

func (s *Store) Reconcile(ctx context.Context) (model.ReconcileResult, error) {
	return s.v.Reconcile(ctx)
}

func (s *Store) Events(_ context.Context, n int) ([]model.Event, error) { return s.log.Tail(n) }

// RevisionWrite implements store.Store.
func (s *Store) RevisionWrite(ctx context.Context, id string, n int, data []byte) (string, error) {
	return s.v.RevisionWrite(ctx, id, n, data)
}

// RevisionRead implements store.Store.
func (s *Store) RevisionRead(ctx context.Context, id string, n int) ([]byte, error) {
	return s.v.RevisionRead(ctx, id, n)
}

// AttachmentAdd implements store.Store.
func (s *Store) AttachmentAdd(ctx context.Context, doc, file string, data []byte) (model.Attachment, error) {
	return s.v.AttachmentAdd(ctx, doc, file, data)
}

// AttachmentStat implements store.Store.
func (s *Store) AttachmentStat(ctx context.Context, path string) (model.Attachment, error) {
	return s.v.AttachmentStat(ctx, path)
}

// AttachmentRead implements store.Store.
func (s *Store) AttachmentRead(ctx context.Context, path string) (model.Attachment, []byte, error) {
	return s.v.AttachmentRead(ctx, path)
}

func (s *Store) Egress(ctx context.Context, doc, dest string) (egress.Decision, error) {
	return s.v.Egress(ctx, doc, dest)
}

func (s *Store) Record(ctx context.Context, op, doc, from, to, hash string) error {
	return s.v.Record(ctx, op, doc, from, to, hash)
}

// indexPath is the search cache, in the state dir and never in the vault
// tree (a synced folder corrupts SQLite).
func (s *Store) indexPath() (string, error) {
	dir := s.v.StateDir()
	if dir == "" {
		return "", errors.New("local: no state dir configured for this vault")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("local: state dir: %w", err)
	}
	return filepath.Join(dir, "index.sqlite"), nil
}

// source adapts the vault to index.Source.
type source struct{ s *Store }

// Refs leaves receipts out: each holds the text of a document twice, and a search
// must find the document.
func (a source) Refs(ctx context.Context) ([]model.Ref, error) {
	refs, err := a.s.v.List(ctx, "", "")
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(refs, func(r model.Ref) bool { return r.Folder == model.FolderReceipts || r.Hidden }), nil
}

func (a source) Get(ctx context.Context, id string) (model.Document, error) {
	return a.s.v.Get(ctx, id)
}

func (s *Store) Search(ctx context.Context, q index.Query) ([]index.Hit, error) {
	p, err := s.indexPath()
	if err != nil {
		return nil, err
	}
	ix, err := index.Open(p)
	if err != nil {
		return nil, fmt.Errorf("local: search: %w", err)
	}
	defer func() { _ = ix.Close() }()
	return ix.Find(ctx, source{s}, q)
}

func (s *Store) IndexRebuild(ctx context.Context) (index.Stats, error) {
	p, err := s.indexPath()
	if err != nil {
		return index.Stats{}, err
	}
	ix, err := index.Open(p)
	if err != nil {
		return index.Stats{}, fmt.Errorf("local: index rebuild: %w", err)
	}
	defer func() { _ = ix.Close() }()
	return ix.Rebuild(ctx, source{s})
}
