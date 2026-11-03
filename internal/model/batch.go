package model

// RewriteFunc returns the document to write in place of d, or false to leave
// d as it is. It must be deterministic: the same document gives the same result.
type RewriteFunc func(d Document) (Document, bool, error)

// BatchResult lists the documents a batch rewrote, or would rewrite on a dry
// run. Skipped lists documents an undo left alone because they changed since.
type BatchResult struct {
	Batch   string   `json:"batch"`
	DryRun  bool     `json:"dry_run,omitempty"`
	Docs    []string `json:"docs"`
	Skipped []string `json:"skipped,omitempty"`
}
