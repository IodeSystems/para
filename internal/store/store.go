// Package store is the interface ops use to reach a vault. It carries no
// filesystem types, so a remote implementation can satisfy it. Actor and
// surface are bound when the Store is opened, not passed per call.
package store

import (
	"context"
	"time"

	"github.com/iodesystems/parasolve/internal/egress"
	"github.com/iodesystems/parasolve/internal/index"
	"github.com/iodesystems/parasolve/internal/model"
)

// Filter narrows List. Empty fields match everything.
type Filter struct {
	Folder model.Folder
	Type   model.Type
}

// Info is who the open vault acts as and where. Actor and Surface are what
// events record; Install is this machine's install id.
type Info struct {
	Actor   string
	Surface string
	Install string
}

// Store is one open vault.
type Store interface {
	// Info returns the identity the Store was opened with.
	Info() Info
	// Now returns the Store's clock, so lease times follow one clock.
	Now() time.Time
	// Config returns a vault.properties value, or "" when the key is unset.
	Config(key string) (string, error)

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
	// Egress checks a path out of the vault against its configuration and logs
	// an egress event with the doc id and destination. Callers release content
	// only after it returns.
	Egress(ctx context.Context, doc, dest string) (egress.Decision, error)
	// Search returns up to limit hits for query, best first. It first brings
	// the search index up to date with the documents.
	Search(ctx context.Context, query string, limit int) ([]index.Hit, error)
	// IndexRebuild drops the search index and reads every document again.
	IndexRebuild(ctx context.Context) (index.Stats, error)

	// Events returns the last n events, oldest first.
	Events(ctx context.Context, n int) ([]model.Event, error)
}
