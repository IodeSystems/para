package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/iodesystems/parasolve/internal/egress"
	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/store"
)

// Para computes the before and after of a para item itself: it runs the op
// against an overlay that keeps writes in memory, so the owner reads what will
// change, not what the agent says will change.

// DocChange is one document a para item would touch.
type DocChange struct {
	Doc   string   `json:"doc"`
	Title string   `json:"title"`
	Type  string   `json:"type"`
	New   bool     `json:"new"`
	Lines []string `json:"lines"` // "- old", "+ new", and "  same" lines
}

// Downstream is one open document that waits on a document the item touches.
type Downstream struct {
	Doc    string `json:"doc"`
	Title  string `json:"title"`
	Effect string `json:"effect"`
}

// ItemPreview is what approving a para item would do. Error says why the op
// would fail now.
type ItemPreview struct {
	Changes    []DocChange  `json:"changes"`
	Downstream []Downstream `json:"downstream"`
	Error      string       `json:"error,omitempty"`
}

// overlay is a store whose writes stay in memory.
type overlay struct {
	store.Store
	docs  map[string]model.Document
	order *[]string
}

func newOverlay(s store.Store) *overlay {
	return &overlay{Store: s, docs: map[string]model.Document{}, order: &[]string{}}
}

func (o *overlay) put(d model.Document) model.Document {
	if _, seen := o.docs[d.ID]; !seen {
		*o.order = append(*o.order, d.ID)
	}
	d.Rev = "preview"
	o.docs[d.ID] = d
	return d
}

func (o *overlay) ActAs(actor, surface string) store.Store {
	return &overlay{Store: o.Store.ActAs(actor, surface), docs: o.docs, order: o.order}
}

func (o *overlay) Create(ctx context.Context, d model.Document) (model.Document, error) {
	if d.ID == "" {
		d.ID = fmt.Sprintf("new-%s-%d", d.Meta.Header().Type, len(o.docs)+1)
	} else if _, err := o.Get(ctx, d.ID); err == nil {
		return d, model.ErrExists
	}
	if h := d.Meta.Header(); h.Created == "" {
		h.Created = model.Date(o.Now().Format("2006-01-02"))
	}
	return o.put(d), nil
}

func (o *overlay) Get(ctx context.Context, id string) (model.Document, error) {
	if d, ok := o.docs[id]; ok {
		return d, nil
	}
	return o.Store.Get(ctx, id)
}

func (o *overlay) Update(_ context.Context, d model.Document) (model.Document, error) {
	return o.put(d), nil
}

func (o *overlay) Move(ctx context.Context, id string, to model.Folder) (model.Document, error) {
	d, err := o.Get(ctx, id)
	if err != nil {
		return d, err
	}
	d.Folder = to
	return o.put(d), nil
}

func (o *overlay) List(ctx context.Context, f store.Filter) ([]model.Ref, error) {
	refs, err := o.Store.List(ctx, f)
	if err != nil {
		return nil, err
	}
	for _, id := range *o.order {
		d := o.docs[id]
		if (f.Folder != "" && d.Folder != f.Folder) || (f.Type != "" && d.Meta.Header().Type != f.Type) {
			continue
		}
		if !slices.ContainsFunc(refs, func(r model.Ref) bool { return r.ID == id }) {
			refs = append(refs, model.Ref{ID: id, Folder: d.Folder, Type: d.Meta.Header().Type, Title: d.Title, Rev: d.Rev})
		}
	}
	return refs, nil
}

func (o *overlay) Egress(context.Context, string, string) (egress.Decision, error) {
	return egress.Decision{}, fmt.Errorf("a preview sends nothing")
}

func (o *overlay) Record(context.Context, string, string, string, string, string) error { return nil }

// previewItem runs the item's op against an overlay as the proposer. A failed op
// is reported, never raised: a preview cannot stop a review.
func previewItem(ctx context.Context, d Deps, actor string, it model.ProposalItem) (pv *ItemPreview) {
	pv = &ItemPreview{Changes: []DocChange{}, Downstream: []Downstream{}}
	defer func() {
		if r := recover(); r != nil {
			pv.Error = "the preview failed"
		}
	}()
	e, ok := registry[it.Op]
	if !ok {
		pv.Error = "op " + it.Op + " is not an op"
		return pv
	}
	raw, err := json.Marshal(it.Input)
	if err != nil {
		pv.Error = err.Error()
		return pv
	}
	ov := newOverlay(d.Store)
	out, err := e.Invoke(context.WithValue(ctx, recordingKey{}, "preview"), Deps{Store: ov.ActAs(actor, surfaceProposal)}, raw)
	if err == nil {
		if f, isFailing := out.(Failing); isFailing {
			err = f.Failed()
		}
	}
	if err != nil {
		pv.Error = PublicError(err, nil)
		return pv
	}
	dp, err := loadDependents(ctx, d.Store)
	if err != nil {
		pv.Error = PublicError(err, nil)
		return pv
	}
	seen := map[string]bool{}
	for _, id := range *ov.order {
		after := ov.docs[id]
		change := DocChange{Doc: id, Title: after.Title, Type: string(after.Meta.Header().Type)}
		var before string
		if doc, err := d.Store.Get(ctx, id); err == nil {
			before = renderText(doc)
		} else {
			change.New = true
		}
		change.Lines = trimContext(diffLines(before, renderText(after)), 2)
		for i, l := range change.Lines {
			change.Lines[i] = plainTitle(ctx, ov, l)
		}
		pv.Changes = append(pv.Changes, change)
		closes := closesDoc(after)
		for _, w := range dp.chain(id) {
			if seen[w] {
				continue
			}
			seen[w] = true
			effect := "waits on this"
			if closes {
				effect = "unblocked when this closes"
			}
			title := w
			if doc, err := d.Store.Get(ctx, w); err == nil {
				title = doc.Title
			}
			pv.Downstream = append(pv.Downstream, Downstream{Doc: w, Title: title, Effect: effect})
		}
	}
	return pv
}

func renderText(d model.Document) string {
	b, err := model.Render(d)
	if err != nil {
		return ""
	}
	return strings.TrimRight(string(b), "\n")
}

// closesDoc reports whether a document is in a final state, so what waits on it
// is free.
func closesDoc(d model.Document) bool {
	switch m := d.Meta.(type) {
	case *model.Task:
		return taskTerminal(m.Status)
	case *model.RFX:
		return m.Status != model.RFXOpen
	case *model.Question:
		return m.Status != model.QuestionOpen
	case *model.Request:
		return slices.Contains([]model.RequestStatus{model.RequestDone, model.RequestFailed, model.RequestCancelled}, m.Status)
	}
	return false
}

// trimContext keeps changed lines and n lines around them.
func trimContext(lines []string, n int) []string {
	keep := map[int]bool{}
	for i, l := range lines {
		if strings.HasPrefix(l, "  ") {
			continue
		}
		for j := max(0, i-n); j <= min(len(lines)-1, i+n); j++ {
			keep[j] = true
		}
	}
	out := make([]string, 0, len(keep))
	for _, i := range slices.Sorted(maps.Keys(keep)) {
		out = append(out, lines[i])
	}
	return out
}
