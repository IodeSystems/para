package ops

import (
	"context"
	"fmt"
	"slices"

	"github.com/iodesystems/paradize/internal/egress"
	"github.com/iodesystems/paradize/internal/export"
	"github.com/iodesystems/paradize/internal/model"
)

// CopyFormat is a paste target for copy.
type CopyFormat string

const (
	CopyPlain CopyFormat = "plain"
	CopyEmail CopyFormat = "email"
	CopySlack CopyFormat = "slack"
	CopyJira  CopyFormat = "jira"
)

// CopyFormats lists every CopyFormat.
var CopyFormats = []CopyFormat{CopyPlain, CopyEmail, CopySlack, CopyJira}

// Enum implements the MCP enum hook.
func (CopyFormat) Enum() []string {
	out := make([]string, len(CopyFormats))
	for i, f := range CopyFormats {
		out[i] = string(f)
	}
	return out
}

// CopyInput is the input of copy.
type CopyInput struct {
	ID        string     `json:"id"`
	As        CopyFormat `json:"as,omitempty"`        // empty means plain
	Clipboard bool       `json:"clipboard,omitempty"` // log the destination as the clipboard; the CLI does the copy
}

// CopyOutput is the output of copy.
type CopyOutput struct {
	ID            string     `json:"id"`
	As            CopyFormat `json:"as"`
	Destination   string     `json:"destination"`
	Marking       string     `json:"marking"`
	EgressDefault string     `json:"egress_default"` // egress.default of the vault
	EgressAllowed bool       `json:"egress_allowed"` // always true for copy: the swivel path stays open
	Text          string     `json:"text"`
}

// Copy renders a document for pasting by hand: no frontmatter, with the
// classification marking. It logs an egress event with the doc id and
// destination before it returns the text. The vault's egress.default never
// blocks it; it is recorded.
var Copy = define("copy", "Render a document for paste (plain, email, slack, jira) and log the egress", func(ctx context.Context, d Deps, in CopyInput) (CopyOutput, error) {
	format := in.As
	if format == "" {
		format = CopyPlain
	}
	if !slices.Contains(CopyFormats, format) {
		return CopyOutput{}, fmt.Errorf("format %q must be one of %v", format, CopyFormats)
	}
	doc, err := d.Store.Get(ctx, in.ID)
	if err != nil {
		return CopyOutput{}, err
	}
	dest := egress.DestCopy + string(format)
	if in.Clipboard {
		dest = egress.DestCopy + "clipboard"
	}
	dec, err := d.Store.Egress(ctx, doc.ID, dest)
	if err != nil {
		return CopyOutput{}, err
	}
	return CopyOutput{
		ID: doc.ID, As: format, Destination: dest, Marking: dec.Marking,
		EgressDefault: dec.Default, EgressAllowed: dec.Allowed,
		Text: renderCopy(format, doc, dec.Marking),
	}, nil
})

// renderCopy renders doc for format.
func renderCopy(f CopyFormat, doc model.Document, marking string) string {
	return export.Text(export.Format(f), doc, marking)
}
