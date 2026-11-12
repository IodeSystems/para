package ops

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// bulk applies one owner decision to many items: it runs one generic verb
// (close, later, reopen) or depends_set (optional) on each item by itself, so
// each item keeps that op's rules, gate, and receipt. A refused item does not
// stop the others. Every receipt of the run carries one batch id, and undo
// with that batch undoes them all.

// The verbs bulk takes.
const (
	BulkClose    = "close"
	BulkLater    = "later"
	BulkReopen   = "reopen"
	BulkOptional = "optional"
	BulkUndo     = "undo"
)

// BulkVerbs lists the verbs of bulk.
var BulkVerbs = []string{BulkClose, BulkLater, BulkReopen, BulkOptional, BulkUndo}

// BulkInput is the input of bulk. Give ids, a tag, a scope, or more than one
// of them; undo takes batch only.
type BulkInput struct {
	Verb  string   `json:"verb"`
	IDs   []string `json:"ids,omitempty"`
	Tag   string   `json:"tag,omitempty"`
	Scope string   `json:"scope,omitempty"` // a goal or an area: the items in it
	// DryRun runs each item against an overlay that keeps writes in memory:
	// nothing is written, and each item says whether it would apply.
	DryRun bool   `json:"dry_run,omitempty"`
	Reason string `json:"reason,omitempty"`
	// Close.
	Outcome string         `json:"outcome,omitempty"`
	Fields  map[string]any `json:"fields,omitempty"`
	// Later.
	Until string `json:"until,omitempty"`
	After string `json:"after,omitempty"`
	When  string `json:"when,omitempty"`
	Place string `json:"place,omitempty"`
	// On is the item the edges of optional point at.
	On string `json:"on,omitempty"`
	// Batch is the run that undo takes back.
	Batch string `json:"batch,omitempty"`
}

// Bulk item states.
const (
	BulkApplied    = "applied"
	BulkWouldApply = "would_apply"
	BulkRefused    = "refused"
)

// BulkItem is what bulk did to one item.
type BulkItem struct {
	ID      string `json:"id"`
	Title   string `json:"title,omitempty"`
	State   string `json:"state"`        // applied, would_apply, or refused
	Op      string `json:"op,omitempty"` // the op that ran (or would run)
	Receipt string `json:"receipt,omitempty"`
	Error   string `json:"error,omitempty"` // why it was refused
}

// BulkOutput is the output of bulk. Batch is the id undo takes; it is empty
// on a dry run and when nothing applied.
type BulkOutput struct {
	Verb    string     `json:"verb"`
	Batch   string     `json:"batch,omitempty"`
	DryRun  bool       `json:"dry_run,omitempty"`
	Applied int        `json:"applied"`
	Refused int        `json:"refused"`
	Items   []BulkItem `json:"items"`
}

// BulkDescription is bulk's description.
const BulkDescription = "Apply one decision to many items in one call: verb close (outcome, reason, fields as close), later (until, after, when, place, reason as later; no until is someday), reopen (reason), or optional (on: each item's dependency on that item becomes optional; the item must wait on it). " +
	"Targets: ids, a tag (every tagged open item; for reopen, closed or put off), and a scope (a goal or area: the items in it). " +
	"Each item runs the verb by itself, with that op's own rules, gate, and receipt; a refused item does not stop the others, and the output says per item applied or refused and why. " +
	"dry_run runs each item against a copy in memory and writes nothing (state would_apply or refused). " +
	"The output's batch names the run; verb undo with batch undoes every receipt of it, newest first (an item changed since is refused). An agent may do only what the verb allows it"

// Bulk applies one verb to many items.
var Bulk = func() *Op[BulkInput, BulkOutput] {
	op := define("bulk", BulkDescription, runBulk)
	// Each item's op gates and files its receipt; bulk does neither.
	op.verb = true
	return op
}()

// bulkTypes are the types a tag or a scope brings in.
var bulkTypes = []model.Type{model.TypeTask, model.TypeQuestion, model.TypeIO, model.TypeObligation, model.TypeRFX, model.TypeIncident}

func runBulk(ctx context.Context, d Deps, in BulkInput) (BulkOutput, error) {
	verb := strings.TrimSpace(in.Verb)
	out := BulkOutput{Verb: verb, DryRun: in.DryRun, Items: []BulkItem{}}
	if !slices.Contains(BulkVerbs, verb) {
		return out, fmt.Errorf("verb %q: want one of %v", verb, BulkVerbs)
	}
	if err := bulkCheck(verb, in); err != nil {
		return out, err
	}
	if verb == BulkUndo {
		return bulkUndo(ctx, d, in, out)
	}
	ids, err := bulkTargets(ctx, d, verb, in)
	if err != nil {
		return out, err
	}
	if len(ids) == 0 {
		return out, errors.New("nothing to change: give ids, a tag, or a scope with items")
	}
	run := d
	if in.DryRun {
		run.Store = newOverlay(d.Store)
	} else {
		out.Batch = newBatchID(d)
		m := writeMeta(ctx)
		m.batch = out.Batch
		ctx = WithWrite(ctx, m)
	}
	for _, id := range ids {
		it := bulkOne(ctx, run, verb, id, in)
		out.Items = append(out.Items, it)
	}
	return bulkCount(out), nil
}

// bulkCheck refuses inputs that belong to another verb.
func bulkCheck(verb string, in BulkInput) error {
	has := map[string]bool{"outcome": in.Outcome != "", "fields": len(in.Fields) > 0, "until": in.Until != "", "after": in.After != "",
		"when": in.When != "", "place": in.Place != "", "on": in.On != "", "batch": in.Batch != "", "reason": strings.TrimSpace(in.Reason) != "",
		"ids": len(in.IDs) > 0, "tag": in.Tag != "", "scope": in.Scope != ""}
	takes := map[string][]string{
		BulkClose:    {"outcome", "fields", "reason", "ids", "tag", "scope"},
		BulkLater:    {"until", "after", "when", "place", "reason", "ids", "tag", "scope"},
		BulkReopen:   {"reason", "ids", "tag", "scope"},
		BulkOptional: {"on", "ids", "tag", "scope"},
		BulkUndo:     {"batch"},
	}[verb]
	for _, k := range []string{"outcome", "fields", "until", "after", "when", "place", "on", "batch", "reason", "ids", "tag", "scope"} {
		if has[k] && !slices.Contains(takes, k) {
			return fmt.Errorf("verb %s takes no %s", verb, k)
		}
	}
	switch {
	case verb == BulkOptional && strings.TrimSpace(in.On) == "":
		return errors.New("verb optional needs on: the item the dependencies point at")
	case verb == BulkUndo && strings.TrimSpace(in.Batch) == "":
		return errors.New("verb undo needs batch: the id a bulk run returned")
	}
	return nil
}

// newBatchID names one bulk run.
func newBatchID(d Deps) string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return "bulk-" + d.Store.Now().UTC().Format("20060102-150405") + "-" + hex.EncodeToString(b)
}

func bulkCount(out BulkOutput) BulkOutput {
	for _, it := range out.Items {
		if it.State == BulkRefused {
			out.Refused++
		} else {
			out.Applied++
		}
	}
	if out.Applied == 0 {
		out.Batch = ""
	}
	return out
}

// bulkTargets lists the ids to change: ids as given, then a tag's and a
// scope's items of bulkTypes in the life the verb changes.
func bulkTargets(ctx context.Context, d Deps, verb string, in BulkInput) ([]string, error) {
	var ids []string
	add := func(id string) {
		if id = normID(id); id != "" && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	for _, id := range in.IDs {
		add(id)
	}
	tag, scope := normTag(in.Tag), normID(in.Scope)
	if tag == "" && scope == "" {
		return ids, nil
	}
	if scope != "" {
		sd, err := d.Store.Get(ctx, scope)
		if err != nil {
			return nil, linkErr("scope", scope, err)
		}
		switch sd.Meta.(type) {
		case *model.Goal, *model.Area:
		default:
			return nil, fmt.Errorf("scope %s is a %s: give a goal or an area", sd.ID, typeWord(sd))
		}
	}
	sn, err := NewSnapshot(ctx, d.Store)
	if err != nil {
		return nil, err
	}
	refs, err := d.Store.List(ctx, store.Filter{All: verb == BulkReopen})
	if err != nil {
		return nil, err
	}
	on := normID(in.On)
	for _, r := range refs {
		if !slices.Contains(bulkTypes, r.Type) {
			continue
		}
		doc, err := sn.Get(r.ID)
		if err != nil {
			continue
		}
		life := sn.Need(r.ID).Life
		switch verb {
		case BulkLater:
			if life != LifeOpen {
				continue
			}
		case BulkReopen:
			if life == LifeOpen {
				continue
			}
		default:
			if life == LifeClosed {
				continue
			}
		}
		if verb == BulkOptional {
			if hard, _ := model.DepLists(doc.Meta); hard == nil || !slices.ContainsFunc(*hard, func(l model.Link) bool { return l.Target() == on }) {
				continue
			}
		}
		if tag != "" && !slices.Contains(normTags(doc.Meta.Header().Tags), tag) {
			continue
		}
		if scope != "" {
			chain, err := scopeChain(ctx, d.Store, doc.ID)
			if err != nil || !slices.Contains(chain[1:], scope) {
				continue
			}
		}
		add(doc.ID)
	}
	return ids, nil
}

// bulkOne runs the verb on one item. A refusal, or a panic, is the item's,
// never the run's.
func bulkOne(ctx context.Context, d Deps, verb, id string, in BulkInput) (it BulkItem) {
	it = BulkItem{ID: id, State: BulkApplied}
	if in.DryRun {
		it.State = BulkWouldApply
	}
	defer func() {
		if r := recover(); r != nil {
			it.State, it.Error = BulkRefused, "the write failed"
		}
	}()
	refuse := func(err error) BulkItem {
		it.State, it.Error = BulkRefused, PublicError(err, nil)
		return it
	}
	doc, err := d.Store.Get(ctx, id)
	if err != nil {
		return refuse(linkErr("id", id, err))
	}
	it.ID, it.Title = doc.ID, doc.Title
	rt, err := bulkRoute(ctx, d, verb, doc, in)
	if err != nil {
		return refuse(err)
	}
	it.Op = rt.op.Name()
	vo, err := runRoute(ctx, d, rt)
	if err == nil {
		if f, ok := vo.raw.(Failing); ok {
			err = f.Failed()
		}
	}
	if err == nil {
		err = deferSkipped(vo.Result)
	}
	if err != nil {
		return refuse(err)
	}
	if !in.DryRun {
		it.Receipt = vo.Receipt
	}
	return it
}

// bulkRoute is the route of one item: the verb's own router, with the item
// as its one id.
func bulkRoute(ctx context.Context, d Deps, verb string, doc model.Document, in BulkInput) (verbRoute, error) {
	var input any
	switch verb {
	case BulkClose:
		input = CloseInput{ID: doc.ID, Outcome: in.Outcome, Reason: in.Reason, Fields: in.Fields}
	case BulkLater:
		input = LaterInput{ID: doc.ID, Until: in.Until, After: in.After, When: in.When, Place: in.Place, Reason: in.Reason}
	case BulkReopen:
		input = ReopenInput{ID: doc.ID, Reason: in.Reason}
	case BulkOptional:
		on := normID(in.On)
		hard, opt := model.DepLists(doc.Meta)
		has := func(list *[]model.Link) bool {
			return list != nil && slices.ContainsFunc(*list, func(l model.Link) bool { return l.Target() == on })
		}
		switch {
		case hard == nil:
			return verbRoute{}, fmt.Errorf("%s is a %s: it waits on nothing", doc.ID, typeWord(doc))
		case has(opt):
			return verbRoute{}, fmt.Errorf("%s: its dependency on %s is optional already", doc.ID, on)
		case !has(hard):
			return verbRoute{}, fmt.Errorf("%s does not wait on %s", doc.ID, on)
		}
		// depends_set files no receipt of its own on a person's surface:
		// the route files one, so the batch undoes it.
		return newRoute(DependsSet, DependsSetInput{ID: doc.ID, On: on, Optional: true}, true)
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return verbRoute{}, err
	}
	return verbRoutes[verb](ctx, d, raw)
}

// deferSkipped is defer's or undefer's skip of the item, as an error: the
// op ran, but changed nothing.
func deferSkipped(res map[string]any) error {
	changed, ok := res["changed"].([]any)
	if !ok || len(changed) > 0 {
		return nil
	}
	if sim, _ := res["similar"].([]any); len(sim) > 0 {
		return errors.New("an open task like the when text exists: run it again with after set to that task")
	}
	if sk, _ := res["skipped"].([]any); len(sk) > 0 {
		if s, ok := sk[0].(string); ok {
			return errors.New(s)
		}
	}
	return errors.New("nothing changed")
}

// bulkUndo undoes every receipt of a batch not undone yet, newest first. A
// receipt refused does not stop the others.
func bulkUndo(ctx context.Context, d Deps, in BulkInput, out BulkOutput) (BulkOutput, error) {
	batch := strings.TrimSpace(in.Batch)
	out.Batch = batch
	all, err := listReceipts(ctx, d.Store)
	if err != nil {
		return out, err
	}
	run := d
	if in.DryRun {
		run.Store = newOverlay(d.Store)
	}
	for _, r := range all {
		if r.Batch != batch || r.Undone != "" {
			continue
		}
		it := BulkItem{ID: r.Doc, State: BulkApplied, Op: ReceiptUndo.Name(), Receipt: r.ID}
		if doc, err := d.Store.Get(ctx, r.Doc); err == nil {
			it.Title = doc.Title
		}
		if in.DryRun {
			it.State = BulkWouldApply
		}
		rt, err := newRoute(ReceiptUndo, ReceiptUndoInput{ID: r.ID}, false)
		if err == nil {
			_, err = runRoute(ctx, run, rt)
		}
		if err != nil {
			it.State, it.Error = BulkRefused, PublicError(err, nil)
		}
		out.Items = append(out.Items, it)
	}
	if len(out.Items) == 0 {
		return out, fmt.Errorf("batch %s: no receipt of it is left to undo", batch)
	}
	out = bulkCount(out)
	out.Batch = batch
	return out, nil
}
