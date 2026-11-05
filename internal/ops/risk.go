package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// Risk is an op's class, stated in its definition.
//
//	read:   changes no document.
//	safe:   additive and reversible: capture, note, draft.
//	review: changes the owner's decisions or state: close, drop, status on
//	        goals, policy, people, delete, move. It is a label on the receipt;
//	        an agent may run it.
type Risk string

const (
	RiskRead   Risk = "read"
	RiskSafe   Risk = "safe"
	RiskReview Risk = "review"
)

// WriteMeta is what an agent says about a write, beside the op's own input. A
// surface sets it on the context; Op.Run reads it.
type WriteMeta struct {
	Why        string
	Sources    []string
	Verify     string // a link, or a check the owner can do
	Confidence model.Confidence
	Request    string // the request this write serves; default the one the actor holds
	// NotDuplicate is why this create is not one of the likely matches.
	NotDuplicate string
	// OwnerSaid is the owner's words from a chat that approved this write, as
	// an agent relayed them. A receipt with them is self-reported.
	OwnerSaid string
	// Minor and Obvious are the agent's ratings of the write, for the gate:
	// minor is a small change, obvious is one the owner would make without
	// asking. Dossier is the dossier a write the gate holds back goes to.
	Minor   bool
	Obvious bool
	Dossier string
}

type (
	writeMetaKey struct{}
	recordingKey struct{}
)

// WithWrite sets the WriteMeta of the write made under ctx.
func WithWrite(ctx context.Context, m WriteMeta) context.Context {
	return context.WithValue(ctx, writeMetaKey{}, m)
}

func writeMeta(ctx context.Context) WriteMeta {
	m, _ := ctx.Value(writeMetaKey{}).(WriteMeta)
	return m
}

// FlowOps run the work itself: claiming, reporting, filing. They hold their own
// record, so they take no why and leave no receipt.
var FlowOps = []string{"request_next", "request_note", "request_done", "request_fail", "request_add", "align_record", "action_record",
	"proposal_add", "proposal_revise", "proposal_decide", "proposal_executed", "proposal_claim", "proposal_reply",
	"agent_sources", "sweep_record"}

// NeedsWhy reports whether an MCP call of the op must carry why.
func NeedsWhy(e Entry) bool {
	return e.Risk() != RiskRead && !e.PersonOnly() && !slices.Contains(FlowOps, e.Name())
}

// needsRecord reports whether a write here is an outermost agent write. An op
// another op calls is covered by the outer one.
func needsRecord(ctx context.Context, d Deps) bool {
	if ctx.Value(recordingKey{}) != nil {
		return false
	}
	return agentSurface(d)
}

// runRecorded runs a write with a store that keeps what it touched, and files a
// receipt for each document. A failed op may have written: its receipts are
// still filed.
func runRecorded[I, O any](ctx context.Context, d Deps, o *Op[I, O], in I) (O, error) {
	if agentSurface(d) && !o.person && !slices.Contains(FlowOps, o.name) {
		if err := checkCreate(ctx, d.Store, o.name, inputMap(in), writeMeta(ctx).NotDuplicate); err != nil {
			var zero O
			return zero, fmt.Errorf("%s: %w", o.name, err)
		}
	}
	if gated(d, o) {
		verdict, lacks := gateDefault, ""
		if o.gateRule != nil {
			verdict, lacks = o.gateRule(in, writeMeta(ctx))
		}
		// The owner asked for it: an undoable safe write applies, and its
		// receipt carries their words, self-reported.
		if verdict == gateDefault && o.risk == RiskSafe && o.Undoable() && strings.TrimSpace(writeMeta(ctx).OwnerSaid) != "" {
			verdict = gatePass
		}
		var missing []string
		var err error
		if verdict != gatePass {
			missing, err = gateMissing(d, o, writeMeta(ctx))
		}
		if verdict == gateStage && !slices.Contains(missing, lacks) {
			missing = append(missing, lacks)
		}
		if err != nil || len(missing) > 0 {
			var zero O
			if err != nil {
				return zero, fmt.Errorf("%s: %w", o.name, err)
			}
			staged, err := stageWrite(ctx, d, o.name, inputMap(in), writeMeta(ctx), missing, o.Undoable())
			if err != nil {
				return zero, fmt.Errorf("%s: the gate held this write back, and staging it failed: %w", o.name, err)
			}
			return zero, staged
		}
	}
	rec := newRecorder(d.Store)
	inner := d
	inner.Store = rec
	out, err := o.run(context.WithValue(ctx, recordingKey{}, o.name), inner, in)
	if slices.Contains(FlowOps, o.name) {
		return out, err
	}
	rec.proposals = o.proposalReceipts
	file := rec.file
	if o.oneReceipt {
		file = rec.fileOne
	}
	if ferr := file(ctx, d, o.name, writeMeta(ctx)); ferr != nil {
		return out, errors.Join(err, fmt.Errorf("%s: the write happened, but its receipt failed: %w", o.name, ferr))
	}
	return out, err
}

func inputMap(in any) map[string]any {
	b, err := json.Marshal(in)
	if err != nil {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	return m
}

// touched is one document a write changed.
type touched struct {
	id     string
	before string
	folder model.Folder
	forced bool // the id changed: a receipt is filed though the text is the same
}

// recorder is a store that keeps the before text of each document it writes.
type recorder struct {
	store.Store
	list *[]touched
	// proposals: dossiers get receipts too (withProposalReceipts).
	proposals bool
}

func newRecorder(s store.Store) *recorder { return &recorder{Store: s, list: &[]touched{}} }

func (r *recorder) ActAs(actor, surface string) store.Store {
	return &recorder{Store: r.Store.ActAs(actor, surface), list: r.list}
}

func (r *recorder) note(ctx context.Context, id string) {
	if id == "" || slices.ContainsFunc(*r.list, func(t touched) bool { return t.id == id }) {
		return
	}
	t := touched{id: id}
	if doc, err := r.Get(ctx, id); err == nil {
		t.before, t.folder = renderText(doc), doc.Folder
	}
	*r.list = append(*r.list, t)
}

func (r *recorder) Create(ctx context.Context, d model.Document) (model.Document, error) {
	out, err := r.Store.Create(ctx, d)
	if err == nil {
		*r.list = append(*r.list, touched{id: out.ID})
	}
	return out, err
}

func (r *recorder) Update(ctx context.Context, d model.Document) (model.Document, error) {
	r.note(ctx, d.ID)
	return r.Store.Update(ctx, d)
}

func (r *recorder) Rename(ctx context.Context, id, newID string) (model.Document, error) {
	r.note(ctx, id)
	out, err := r.Store.Rename(ctx, id, newID)
	if err == nil && id != newID {
		for i := range *r.list {
			if t := &(*r.list)[i]; t.id == id {
				t.id, t.forced = newID, true
			}
		}
	}
	return out, err
}

func (r *recorder) Move(ctx context.Context, id string, to model.Folder) (model.Document, error) {
	r.note(ctx, id)
	return r.Store.Move(ctx, id, to)
}

// noReceipt lists the types that are their own record.
var noReceipt = []model.Type{model.TypeReceipt, model.TypeProposal}

// skip is true for a document that files no receipt.
func (r *recorder) skip(doc model.Document) bool {
	t := doc.Meta.Header().Type
	return slices.Contains(noReceipt, t) && (!r.proposals || t != model.TypeProposal)
}

// file writes one receipt per document that changed.
func (r *recorder) file(ctx context.Context, d Deps, op string, m WriteMeta) error {
	info := d.Store.Info()
	if m.Request == "" {
		m.Request = heldRequest(ctx, d.Store, info.Actor)
	}
	for _, t := range *r.list {
		doc, err := d.Store.Get(ctx, t.id)
		if err != nil {
			continue // a document the op made and removed left nothing to show
		}
		if r.skip(doc) {
			continue
		}
		after := renderText(doc)
		if after == t.before && !t.forced && (t.folder == "" || t.folder == doc.Folder) {
			continue
		}
		if err := fileReceipt(ctx, d, receiptInput{op: op, doc: t.id, title: doc.Title, meta: m, content: model.ReceiptContent{Folder: t.folder, Before: t.before, After: after},
			selfReported: m.OwnerSaid != "", ownerSaid: m.OwnerSaid}); err != nil {
			return err
		}
	}
	return nil
}

// fileOne writes one receipt for every document that changed: the first is
// the receipt's doc, the others go in also.
func (r *recorder) fileOne(ctx context.Context, d Deps, op string, m WriteMeta) error {
	info := d.Store.Info()
	if m.Request == "" {
		m.Request = heldRequest(ctx, d.Store, info.Actor)
	}
	var c model.ReceiptContent
	main, title := "", ""
	for _, t := range *r.list {
		doc, err := d.Store.Get(ctx, t.id)
		if err != nil || r.skip(doc) {
			continue
		}
		after := renderText(doc)
		if after == t.before && !t.forced && (t.folder == "" || t.folder == doc.Folder) {
			continue
		}
		if main == "" {
			main, title = t.id, doc.Title
			c.Folder, c.Before, c.After = t.folder, t.before, after
			continue
		}
		c.Also = append(c.Also, model.ReceiptPart{Doc: t.id, Folder: t.folder, Before: t.before, After: after})
	}
	if main == "" {
		return nil
	}
	return fileReceipt(ctx, d, receiptInput{op: op, doc: main, title: title, meta: m, content: c,
		selfReported: m.OwnerSaid != "", ownerSaid: m.OwnerSaid})
}

// receiptInput is one receipt to file.
type receiptInput struct {
	op, doc, title string
	meta           WriteMeta
	content        model.ReceiptContent
	selfReported   bool
	ownerSaid      string
	align          *model.AlignResult
	// by and onBehalfOf are set on the record of an outward act.
	by, onBehalfOf string
}

// fileReceipt writes a receipt and an event that points at it. The event holds
// ids: the document, the request in from, the receipt in to.
func fileReceipt(ctx context.Context, d Deps, in receiptInput) error {
	_, err := createReceipt(ctx, d, in)
	return err
}

func createReceipt(ctx context.Context, d Deps, in receiptInput) (string, error) {
	body, err := model.RenderReceiptBody(in.content)
	if err != nil {
		return "", err
	}
	info := d.Store.Info()
	conf := in.meta.Confidence
	saved, err := d.Store.Create(ctx, model.Document{
		Folder: model.FolderReceipts, Title: receiptTitle(in.op, in.title), Body: body,
		Meta: &model.Receipt{
			Common: model.Common{Type: model.TypeReceipt}, Actor: info.Actor, Op: in.op, Doc: in.doc, Request: in.meta.Request,
			Why: strings.TrimSpace(in.meta.Why), Sources: in.meta.Sources, Verify: strings.TrimSpace(in.meta.Verify), Confidence: conf,
			Check: model.ReceiptUnchecked, SelfReported: in.selfReported, OwnerSaid: strings.TrimSpace(in.ownerSaid),
			At: d.Store.Now().UTC().Format(time.RFC3339Nano), Surface: info.Surface, By: in.by, OnBehalfOf: in.onBehalfOf, Background: info.Surface == TickSurface || in.meta.Request != "", NotDuplicate: strings.TrimSpace(in.meta.NotDuplicate), Align: in.align,
		},
	})
	if err != nil {
		return "", err
	}
	return saved.ID, d.Store.Record(ctx, "receipt", in.doc, in.meta.Request, saved.ID, "")
}

func receiptTitle(op, title string) string {
	return strings.TrimSpace(opWords(op) + ": " + strings.Join(strings.Fields(title), " "))
}

// heldRequest is the request the actor holds, when it holds exactly one.
func heldRequest(ctx context.Context, s store.Store, actor string) string {
	docs, err := loadRequests(ctx, s)
	if err != nil {
		return ""
	}
	id := ""
	for _, doc := range docs {
		r := doc.Meta.(*model.Request)
		if r.Status == model.RequestClaimed && r.Claimed == actor {
			if id != "" {
				return ""
			}
			id = doc.ID
		}
	}
	return id
}
