// Package local implements store.Store over vault and events.
package local

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/iodesystems/parasolve/internal/egress"
	"github.com/iodesystems/parasolve/internal/events"
	"github.com/iodesystems/parasolve/internal/index"
	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/store"
	"github.com/iodesystems/parasolve/internal/vault"
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

// ActAs is As behind the store.Store interface.
func (s *Store) ActAs(actor, surface string) store.Store { return s.As(actor, surface) }

func (s *Store) VaultID() string { return s.v.ID() }

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
	return s.v.List(ctx, f.Folder, f.Type)
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

func (s *Store) SetConfig(ctx context.Context, key, value string) error {
	return s.v.SetConfig(ctx, key, value)
}

func (s *Store) Rewrite(ctx context.Context, batch string, dry bool, fn model.RewriteFunc) (model.BatchResult, error) {
	return s.v.Rewrite(ctx, batch, dry, fn)
}

func (s *Store) UndoBatch(ctx context.Context, batch string) (model.BatchResult, error) {
	return s.v.UndoBatch(ctx, batch)
}

func (s *Store) Lint(ctx context.Context) ([]model.Finding, error) { return s.v.Check(ctx) }

func (s *Store) Reconcile(ctx context.Context) (model.ReconcileResult, error) {
	return s.v.Reconcile(ctx)
}

func (s *Store) Events(_ context.Context, n int) ([]model.Event, error) { return s.log.Tail(n) }

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
	return slices.DeleteFunc(refs, func(r model.Ref) bool { return r.Folder == model.FolderReceipts }), nil
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
