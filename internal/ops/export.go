package ops

import (
	"context"
	"encoding/base64"
	"fmt"
	"slices"

	"github.com/iodesystems/para/internal/export"
)

// ExportInput is the input of export.
type ExportInput struct {
	ID string        `json:"id"`
	As export.Format `json:"as,omitempty"` // empty means plain
	To string        `json:"to,omitempty"` // transport: stdout (default), file:<path>, clipboard, osc52, tmux, wormhole, qr, ssh:<host>:<path>
}

// ExportOutput is the output of export. Exactly one of Text and DataBase64
// holds the content.
type ExportOutput struct {
	ID            string        `json:"id"`
	As            export.Format `json:"as"`
	To            string        `json:"to"`
	Destination   string        `json:"destination"` // egress destination logged: export:<transport>
	Marking       string        `json:"marking"`
	EgressDefault string        `json:"egress_default"`
	EgressAllowed bool          `json:"egress_allowed"`
	Ext           string        `json:"ext"`
	Text          string        `json:"text,omitempty"`
	DataBase64    string        `json:"data_base64,omitempty"` // pdf and docx
}

// Rendered returns the content of the output, for a surface that runs the
// transport.
func (o ExportOutput) Rendered() (export.Rendered, error) {
	r := export.Rendered{Format: o.As, Ext: o.Ext, Binary: o.As.Binary(), Data: []byte(o.Text)}
	if o.DataBase64 != "" {
		b, err := base64.StdEncoding.DecodeString(o.DataBase64)
		if err != nil {
			return export.Rendered{}, fmt.Errorf("export: data: %w", err)
		}
		r.Data = b
	}
	return r, nil
}

// Export renders a document in a format and logs the egress for the named
// transport. It moves nothing: the op returns the content to its caller, and
// only the CLI runs the transport. An agent that calls this op over MCP
// receives content in its own context and cannot make para write a file,
// touch the clipboard, or open a connection. The transport must pass the
// vault's egress.export allowlist; a denied transport is logged and returns
// no content. egress.default never blocks an export.
var Export = defineRead("export", "Render a document (plain, md, email, slack, jira, html, pdf, docx) for a transport and log the egress; the CLI runs the transport", func(ctx context.Context, d Deps, in ExportInput) (ExportOutput, error) {
	format := in.As
	if format == "" {
		format = export.Plain
	}
	target, err := export.ParseTarget(in.To)
	if err != nil {
		return ExportOutput{}, err
	}
	if !slices.Contains(export.Formats, format) {
		return ExportOutput{}, fmt.Errorf("format %q must be one of %v", format, export.Formats)
	}
	doc, err := d.Store.Get(ctx, in.ID)
	if err != nil {
		return ExportOutput{}, err
	}
	dest := target.Dest()
	dec, err := d.Store.Egress(ctx, doc.ID, dest)
	if err != nil {
		return ExportOutput{}, err
	}
	if !dec.Allowed {
		return ExportOutput{}, fmt.Errorf("transport %s is not in this vault's egress.export", target.Kind)
	}
	r, err := export.Render(ctx, format, doc, dec.Marking)
	if err != nil {
		return ExportOutput{}, err
	}
	if err := target.Check(r); err != nil {
		return ExportOutput{}, err
	}
	out := ExportOutput{
		ID: doc.ID, As: format, To: target.String(), Destination: dest, Marking: dec.Marking,
		EgressDefault: dec.Default, EgressAllowed: dec.Allowed, Ext: r.Ext,
	}
	if r.Binary {
		out.DataBase64 = base64.StdEncoding.EncodeToString(r.Data)
	} else {
		out.Text = string(r.Data)
	}
	return out, nil
})

// ExportListInput is the input of export_list.
type ExportListInput struct{}

// ExportListOutput is the output of export_list.
type ExportListOutput struct {
	Formats    []export.Item `json:"formats"`
	Transports []export.Item `json:"transports"`
}

// ExportList reports the formats and transports that work on the machine
// that runs it, by looking for tools on PATH. It reads nothing from a vault
// and changes nothing. It does not apply a vault's egress.export.
var ExportList = defineRead("export_list", "List the export formats and transports available on this machine", func(ctx context.Context, d Deps, in ExportListInput) (ExportListOutput, error) {
	return ExportListOutput{Formats: export.AvailableFormats(), Transports: export.AvailableTransports()}, nil
})
