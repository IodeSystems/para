package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/store"
)

// Risk is an op's class, stated in its definition.
//
//	read:   changes no document.
//	safe:   additive and reversible: capture, note, draft.
//	review: changes the owner's decisions or state: close, drop, status on
//	        goals, policy, people, delete, move.
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
var FlowOps = []string{"request_next", "request_note", "request_done", "request_fail", "request_add", "proposal_add", "proposal_revise"}

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
	return agentSurface(d) || d.Store.Info().Surface == surfaceProposal
}

// runRecorded runs a write with a store that keeps what it touched, and files a
// receipt for each document. A failed op may have written: its receipts are
// still filed.
func runRecorded[I, O any](ctx context.Context, d Deps, o *Op[I, O], in I) (O, error) {
	if agentSurface(d) && !o.person && !slices.Contains(FlowOps, o.name) {
		if err := checkGate(d, o.name, o.risk, writeMeta(ctx)); err != nil {
			var zero O
			return zero, fmt.Errorf("%s: %w", o.name, err)
		}
		if err := checkCreate(ctx, d.Store, o.name, inputMap(in), writeMeta(ctx).NotDuplicate); err != nil {
			var zero O
			return zero, fmt.Errorf("%s: %w", o.name, err)
		}
	}
	rec := newRecorder(d.Store)
	inner := d
	inner.Store = rec
	out, err := o.run(context.WithValue(ctx, recordingKey{}, o.name), inner, in)
	if slices.Contains(FlowOps, o.name) {
		return out, err
	}
	if ferr := rec.file(ctx, d, o.name, writeMeta(ctx)); ferr != nil {
		return out, errors.Join(err, fmt.Errorf("%s: the write happened, but its receipt failed: %w", o.name, ferr))
	}
	return out, err
}

// agentReview is the vault.properties key that tightens the gate: all sends every
// write to a proposal, and op names are let through. Example: all,task_done.
const agentReview = "agent.review"

// ErrNeedsProposal is an agent write the gate refused.
var ErrNeedsProposal = errors.New("this needs the owner: file a proposal with proposal_add, kind para")

// checkGate is the review gate, in para and not in the prompt. From an agent, a
// review op or a low-confidence write is refused with a way forward. External
// actions are always proposals: para has no op that sends.
func checkGate(d Deps, op string, risk Risk, m WriteMeta) error {
	conf, err := d.Store.Config(agentReview)
	if err != nil {
		return err
	}
	all, allow := false, false
	for _, w := range strings.Split(conf, ",") {
		switch w = strings.TrimSpace(w); w {
		case "all":
			all = true
		case op:
			allow = true
		}
	}
	switch {
	case m.Confidence == model.ConfidenceLow:
		return fmt.Errorf("a low-confidence write: %w", ErrNeedsProposal)
	case allow:
		return nil
	case all:
		return fmt.Errorf("%s=all: %w", agentReview, ErrNeedsProposal)
	case risk == RiskReview:
		return fmt.Errorf("%s changes what the owner decides: %w", op, ErrNeedsProposal)
	}
	return nil
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
}

// recorder is a store that keeps the before text of each document it writes.
type recorder struct {
	store.Store
	list *[]touched
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

func (r *recorder) Move(ctx context.Context, id string, to model.Folder) (model.Document, error) {
	r.note(ctx, id)
	return r.Store.Move(ctx, id, to)
}

// noReceipt lists the types that are their own record.
var noReceipt = []model.Type{model.TypeReceipt, model.TypeProposal}

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
		if slices.Contains(noReceipt, doc.Meta.Header().Type) {
			continue
		}
		after := renderText(doc)
		if after == t.before && (t.folder == "" || t.folder == doc.Folder) {
			continue
		}
		if err := fileReceipt(ctx, d, receiptInput{op: op, doc: t.id, title: doc.Title, meta: m, content: model.ReceiptContent{Folder: t.folder, Before: t.before, After: after}}); err != nil {
			return err
		}
	}
	return nil
}

// receiptInput is one receipt to file.
type receiptInput struct {
	op, doc, title string
	meta           WriteMeta
	content        model.ReceiptContent
	selfReported   bool
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
			Check: model.ReceiptUnchecked, SelfReported: in.selfReported, NotDuplicate: strings.TrimSpace(in.meta.NotDuplicate),
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
