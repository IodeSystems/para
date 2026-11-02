// Package local implements store.Store over vault and events.
package local

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/iodesystems/paradize/internal/egress"
	"github.com/iodesystems/paradize/internal/events"
	"github.com/iodesystems/paradize/internal/index"
	"github.com/iodesystems/paradize/internal/model"
	"github.com/iodesystems/paradize/internal/store"
	"github.com/iodesystems/paradize/internal/vault"
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

func (s *Store) VaultID() string { return s.v.ID() }

func (s *Store) Info() store.Info {
	return store.Info{Actor: s.v.Actor(), Surface: s.v.Surface(), Install: s.v.Install()}
}

func (s *Store) Now() time.Time { return s.v.Now() }

func (s *Store) Config(key string) (string, error) { return s.v.Config(key) }

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

func (s *Store) Lint(ctx context.Context) ([]model.Finding, error) { return s.v.Check(ctx) }

func (s *Store) Reconcile(ctx context.Context) (model.ReconcileResult, error) {
	return s.v.Reconcile(ctx)
}

func (s *Store) Events(_ context.Context, n int) ([]model.Event, error) { return s.log.Tail(n) }

func (s *Store) Egress(ctx context.Context, doc, dest string) (egress.Decision, error) {
	return s.v.Egress(ctx, doc, dest)
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

func (a source) Refs(ctx context.Context) ([]model.Ref, error) { return a.s.v.List(ctx, "", "") }

func (a source) Get(ctx context.Context, id string) (model.Document, error) {
	return a.s.v.Get(ctx, id)
}

func (s *Store) Search(ctx context.Context, query string, limit int) ([]index.Hit, error) {
	p, err := s.indexPath()
	if err != nil {
		return nil, err
	}
	ix, err := index.Open(p)
	if err != nil {
		return nil, fmt.Errorf("local: search: %w", err)
	}
	defer func() { _ = ix.Close() }()
	return ix.Search(ctx, source{s}, query, limit)
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
