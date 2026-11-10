package ops

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
)

// An outgoing draft keeps every text it had before an edit as a version file
// io/<id>.r<n>.md (the whole document as it was). Revision is the current
// number n, starting at 1; Revisions index the saved ones. Version files are
// not documents: list, lint, and the index skip them.

// currentRevision is the number of the text now in the draft.
func currentRevision(m *model.IO) int { return max(m.Revision, 1) }

// saveRevision writes the text the draft had before this edit as a version
// file, then moves the draft to the next number. prior is that text. It
// changes m only; the caller saves the document.
func saveRevision(ctx context.Context, d Deps, id string, m *model.IO, prior []byte) error {
	n := currentRevision(m)
	hash, err := d.Store.RevisionWrite(ctx, id, n, prior)
	if err != nil {
		return err
	}
	m.Revisions = append(slices.DeleteFunc(m.Revisions, func(r model.IORevision) bool { return r.N == n }),
		model.IORevision{N: n, Hash: hash, By: d.Store.Info().Actor, At: d.Store.Now().UTC().Format(time.RFC3339)})
	m.Revision = n + 1
	return nil
}

// IORevisionsInput is the input of io_revisions. With n it returns the text
// of that revision too.
type IORevisionsInput struct {
	ID string `json:"id" query:"id" required:"true"`
	N  int    `json:"n,omitempty" query:"n"`
}

// IORevisionsOutput is the output of io_revisions. Current is the number of
// the text now in the draft; Revisions are the saved earlier ones.
type IORevisionsOutput struct {
	ID        string             `json:"id"`
	Current   int                `json:"current"`
	Revisions []model.IORevision `json:"revisions"`
	N         int                `json:"n,omitempty"`
	Text      string             `json:"text,omitempty"`
}

// IORevisions lists the saved revisions of an outgoing draft.
var IORevisions = defineRead("io_revisions", "List the saved earlier texts of an outgoing draft (revision n, hash, who, when), or with n show the text of revision n. Each edit of the draft keeps the text before it", func(ctx context.Context, d Deps, in IORevisionsInput) (IORevisionsOutput, error) {
	doc, m, err := getIO(ctx, d, in.ID)
	if err != nil {
		return IORevisionsOutput{}, err
	}
	if m.Direction != model.IOOut {
		return IORevisionsOutput{}, fmt.Errorf("%s is an incoming message: only outgoing drafts keep revisions", doc.ID)
	}
	out := IORevisionsOutput{ID: doc.ID, Current: currentRevision(m), Revisions: m.Revisions}
	if out.Revisions == nil {
		out.Revisions = []model.IORevision{}
	}
	if in.N != 0 {
		if out.N, out.Text, err = revisionText(ctx, d, doc, m, in.N); err != nil {
			return IORevisionsOutput{}, err
		}
	}
	return out, nil
})

// revisionText returns the text of revision n: a saved one, or the current.
func revisionText(ctx context.Context, d Deps, doc model.Document, m *model.IO, n int) (int, string, error) {
	if n == currentRevision(m) {
		return n, renderText(doc), nil
	}
	if n < 1 || !slices.ContainsFunc(m.Revisions, func(r model.IORevision) bool { return r.N == n }) {
		return 0, "", fmt.Errorf("revision %d: %s has revisions 1 to %d", n, doc.ID, currentRevision(m))
	}
	data, err := d.Store.RevisionRead(ctx, doc.ID, n)
	if err != nil {
		return 0, "", fmt.Errorf("revision %d of %s: %w", n, doc.ID, err)
	}
	return n, strings.TrimRight(string(data), "\n"), nil
}

// IODiffInput is the input of io_diff. To 0 is the current text; From 0 is
// the revision before To.
type IODiffInput struct {
	ID      string `json:"id" query:"id" required:"true"`
	From    int    `json:"from,omitempty" query:"from"`
	To      int    `json:"to,omitempty" query:"to"`
	Unit    string `json:"unit,omitempty" query:"unit"`       // line (default) or paragraph
	Context int    `json:"context,omitempty" query:"context"` // unchanged units around a change; 0 is 2
}

// IODiffOutput is the output of io_diff.
type IODiffOutput struct {
	ID      string     `json:"id"`
	From    int        `json:"from"`
	To      int        `json:"to"`
	Unit    string     `json:"unit"`
	Same    bool       `json:"same"`
	Added   int        `json:"added"`
	Removed int        `json:"removed"`
	Parts   []DiffPart `json:"parts"`
	Text    string     `json:"text"` // the parts as a unified text
}

// IODiff compares two revisions of an outgoing draft.
var IODiff = defineRead("io_diff", "Show what changed in an outgoing draft between two revisions (from, to; to 0 is the current text, from 0 is the revision before to), by line or paragraph, with a few unchanged units around each change", func(ctx context.Context, d Deps, in IODiffInput) (IODiffOutput, error) {
	doc, m, err := getIO(ctx, d, in.ID)
	if err != nil {
		return IODiffOutput{}, err
	}
	if m.Direction != model.IOOut {
		return IODiffOutput{}, fmt.Errorf("%s is an incoming message: only outgoing drafts keep revisions", doc.ID)
	}
	unit := firstOf(in.Unit, UnitLine)
	if !slices.Contains(DiffUnits, unit) {
		return IODiffOutput{}, fmt.Errorf("unit %q: want line or paragraph", unit)
	}
	cur := currentRevision(m)
	to, from := in.To, in.From
	if to == 0 {
		to = cur
	}
	if from == 0 {
		from = max(to-1, 1) // revision 1 has none before it: it equals itself
	}
	if from > to {
		return IODiffOutput{}, fmt.Errorf("from %d to %d: from comes first", from, to)
	}
	_, a, err := revisionText(ctx, d, doc, m, from)
	if err != nil {
		return IODiffOutput{}, err
	}
	_, b, err := revisionText(ctx, d, doc, m, to)
	if err != nil {
		return IODiffOutput{}, err
	}
	n := in.Context
	if n <= 0 {
		n = 2
	}
	all := diffUnits(splitUnits(a, unit), splitUnits(b, unit))
	out := IODiffOutput{ID: doc.ID, From: from, To: to, Unit: unit, Parts: compactDiff(all, n)}
	for _, p := range all {
		switch p.Op {
		case DiffAdd:
			out.Added++
		case DiffDel:
			out.Removed++
		}
	}
	out.Same = out.Added == 0 && out.Removed == 0
	out.Text = renderDiff(out.Parts)
	return out, nil
})
