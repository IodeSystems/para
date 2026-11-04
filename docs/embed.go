// Package docs embeds the para-md templates that `para init` copies into a
// new vault, and the example source documents. docs/templates and
// docs/sources are the only copies.
package docs

import "embed"

// Templates holds templates/<type>.md, one per document type.
//
//go:embed templates/*.md
var Templates embed.FS

// Sources holds sources/<system>.md, one example source document per system
// para ships: `para source example <system>` prints one.
//
//go:embed sources/*.md
var Sources embed.FS
