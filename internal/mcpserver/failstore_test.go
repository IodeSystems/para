package mcpserver

import (
	"context"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/store"
)

// failStore fails or panics on Reconcile.
type failStore struct {
	store.Store
	err    error
	panics bool
}

func (f failStore) Reconcile(context.Context) (model.ReconcileResult, error) {
	if f.panics {
		panic("boom")
	}
	return model.ReconcileResult{}, f.err
}
