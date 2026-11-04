// Package store is the interface ops use to reach a vault. It carries no
// filesystem types, so a remote implementation can satisfy it. Actor and
// surface are bound when the Store is opened, not passed per call.
package store

import (
	"context"
	"errors"
	"time"

	"github.com/iodesystems/para/internal/egress"
	"github.com/iodesystems/para/internal/index"
	"github.com/iodesystems/para/internal/model"
)

// Filter narrows List. Empty fields match everything.
type Filter struct {
	Folder model.Folder
	Type   model.Type
	// All lists hidden documents too: archived, or merged into another
	// (duplicate_of). By default List leaves them out; Get still finds them.
	All bool
}

// Info is who the open vault acts as and where. Actor and Surface are what
// events record; Install is this machine's install id.
type Info struct {
	Actor   string
	Surface string
	Install string
	// StateDir is this machine's directory for the vault, or empty. Per-machine
	// config such as raglit.json lives there, never in the vault.
	StateDir string
}

// ErrOutsideVault is a path that is not inside the vault. Para reads nothing
// outside the vault.
var ErrOutsideVault = errors.New("the path is outside the vault: para reads nothing outside it")

// Store is one open vault.
type Store interface {
	// DocAt returns the id of the document at path: a path relative to the
	// vault root, or an absolute path inside it. It reads no file. It returns
	// ErrOutsideVault for a path outside the vault, and model.ErrNotFound when
	// no document of the vault is at that path.
	DocAt(ctx context.Context, path string) (string, error)
	// Info returns the identity the Store was opened with.
	Info() Info
	// ActAs returns this Store acting for another actor and surface. A background
	// run uses it to act under the actor it serves.
	ActAs(actor, surface string) Store
	// Now returns the Store's clock, so lease times follow one clock.
	Now() time.Time
	// Config returns a vault.properties value, or "" when the key is unset.
	Config(key string) (string, error)
	// ConfigAll returns every key of vault.properties.
	ConfigAll() (map[string]string, error)
	// ConfigFile returns a file of the vault's .para directory, or nil when
	// there is none, such as proposals.yaml.
	ConfigFile(name string) ([]byte, error)

	// SetConfig sets one key of vault.properties and keeps the other lines.
	SetConfig(ctx context.Context, key, value string) error
	// UnsetConfig removes one key of vault.properties and keeps the other lines.
	UnsetConfig(ctx context.Context, key string) error
	// Rewrite applies fn to every document as one batch that UndoBatch can
	// reverse on this machine, with one event per changed document. A dry run
	// changes nothing and lists what would change.
	Rewrite(ctx context.Context, batch string, dry bool, fn model.RewriteFunc) (model.BatchResult, error)
	// UndoBatch restores the documents a batch changed, unless edited since.
	UndoBatch(ctx context.Context, batch string) (model.BatchResult, error)

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
	// Rename changes a document's id and file name; folder and content stay.
	// It returns model.ErrExists for an id another document holds. Links to the
	// old id are the caller's to rewrite.
	Rename(ctx context.Context, id, newID string) (model.Document, error)

	// Lint checks every document in the vault.
	Lint(ctx context.Context) ([]model.Finding, error)
	// Reconcile logs changes made outside the store.
	Reconcile(ctx context.Context) (model.ReconcileResult, error)
	// Egress checks a path out of the vault against its configuration and logs
	// an egress event with the doc id and destination. Callers release content
	// only after it returns.
	Egress(ctx context.Context, doc, dest string) (egress.Decision, error)
	// Record logs one event that no write logs by itself, such as a receipt. It takes ids, states, and hashes, never content.
	Record(ctx context.Context, op, doc, from, to, hash string) error
	// Search returns up to q.Limit hits for q.Text that pass the filters, best
	// first. It first brings
	// the search index up to date with the documents.
	Search(ctx context.Context, q index.Query) ([]index.Hit, error)
	// IndexRebuild drops the search index and reads every document again.
	IndexRebuild(ctx context.Context) (index.Stats, error)

	// Events returns the last n events, oldest first.
	Events(ctx context.Context, n int) ([]model.Event, error)
}
