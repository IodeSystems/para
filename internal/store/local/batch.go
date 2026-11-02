package local

import (
	"context"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/vault"
)

// CreateBatch writes many documents under one lock (vault.CreateBatch). It is
// not part of store.Store: only an importer uses it.
func (s *Store) CreateBatch(ctx context.Context, batch string, docs []model.Document) ([]vault.BatchResult, error) {
	return s.v.CreateBatch(ctx, batch, docs)
}

// ImportedDocs lists documents with import provenance and every id.
func (s *Store) ImportedDocs(ctx context.Context) ([]vault.ImportedDoc, map[string]bool, error) {
	return s.v.ImportedDocs(ctx)
}

// RemoveBatch removes the documents of an import batch unless one changed.
func (s *Store) RemoveBatch(ctx context.Context, batch string) (vault.RemoveResult, error) {
	return s.v.RemoveBatch(ctx, batch)
}
