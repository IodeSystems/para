// Package local implements store.Store over vault and events.
package local

import (
	"context"
	"fmt"

	"github.com/iodesystems/paradize/internal/events"
	"github.com/iodesystems/paradize/internal/model"
	"github.com/iodesystems/paradize/internal/store"
	"github.com/iodesystems/paradize/internal/vault"
)

// Store is a vault on this machine.
type Store struct {
	v   *vault.Vault
	log *events.Log
}

var _ store.Store = (*Store)(nil)

// Open opens the vault at root.
func Open(root string, o vault.Options) (*Store, error) {
	v, err := vault.Open(root, o)
	if err != nil {
		return nil, fmt.Errorf("local: open: %w", err)
	}
	return &Store{v: v, log: v.Log()}, nil
}

func (s *Store) VaultID() string { return s.v.ID() }

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
