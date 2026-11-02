package mcpserver

import (
	"context"

	"github.com/iodesystems/paradize/internal/model"
	"github.com/iodesystems/paradize/internal/store"
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
