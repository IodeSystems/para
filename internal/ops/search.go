package ops

import (
	"context"

	"github.com/iodesystems/paradize/internal/index"
)

// SearchInput is the input of search.
type SearchInput struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"` // 0 means 10
}

// SearchOutput is the output of search.
type SearchOutput struct {
	Hits []index.Hit `json:"hits"`
}

// Search finds documents by text. A hit says whether it is a transcript or a
// summary, and carries the source URL, so a reader can cite the original.
var Search = define("search", "Search documents, transcripts and summaries by text", func(ctx context.Context, d Deps, in SearchInput) (SearchOutput, error) {
	hits, err := d.Store.Search(ctx, in.Query, in.Limit)
	if err != nil {
		return SearchOutput{}, err
	}
	if hits == nil {
		hits = []index.Hit{}
	}
	return SearchOutput{Hits: hits}, nil
})

// IndexRebuildInput is the input of index_rebuild.
type IndexRebuildInput struct{}

// IndexRebuild drops the search index and builds it again from the markdown.
var IndexRebuild = define("index_rebuild", "Rebuild the search index from the documents", func(ctx context.Context, d Deps, _ IndexRebuildInput) (index.Stats, error) {
	return d.Store.IndexRebuild(ctx)
})
