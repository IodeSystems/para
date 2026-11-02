// Package docs embeds the para-md templates that `para init` copies into a
// new vault. docs/templates is the only copy.
package docs

import "embed"

// Templates holds templates/<type>.md, one per document type.
//
//go:embed templates/*.md
var Templates embed.FS
