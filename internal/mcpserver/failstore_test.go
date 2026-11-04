package mcpserver

import (
	"context"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// failStore fails or panics on Reconcile.
type failStore struct {
	store.Store
	err    error
	panics bool
}

func (f failStore) Info() store.Info { return store.Info{Surface: "cli"} }

func (f failStore) Reconcile(context.Context) (model.ReconcileResult, error) {
	if f.panics {
		panic("boom")
	}
	return model.ReconcileResult{}, f.err
}
