// Package store is the interface ops use to reach a vault. It carries no
// filesystem types, so a remote implementation can satisfy it. Actor and
// surface are bound when the Store is opened, not passed per call.
package store

import (
	"context"

	"github.com/iodesystems/paradize/internal/model"
)

// Filter narrows List. Empty fields match everything.
type Filter struct {
	Folder model.Folder
	Type   model.Type
}

// Store is one open vault.
type Store interface {
	// VaultID returns the id of the open vault.
	VaultID() string

	// Create writes a new document and returns it with its ID and Rev set.
	// An empty ID is generated. Returns model.ErrExists or *model.LintError.
	Create(ctx context.Context, d model.Document) (model.Document, error)
	// Get returns model.ErrNotFound for an unknown id.
	Get(ctx context.Context, id string) (model.Document, error)
	// List returns document refs sorted by id.
	List(ctx context.Context, f Filter) ([]model.Ref, error)
	// Update replaces a document. d.Rev must be current, else model.ErrConflict.
	Update(ctx context.Context, d model.Document) (model.Document, error)
	// Move changes the folder of a document. The id does not change.
	Move(ctx context.Context, id string, to model.Folder) (model.Document, error)

	// Lint checks every document in the vault.
	Lint(ctx context.Context) ([]model.Finding, error)
	// Reconcile logs changes made outside the store.
	Reconcile(ctx context.Context) (model.ReconcileResult, error)
	// Events returns the last n events, oldest first.
	Events(ctx context.Context, n int) ([]model.Event, error)
}
