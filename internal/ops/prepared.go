package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/iodesystems/para/internal/model"
)

// Prepared items. An agent prepares an item that needs the owner so the owner
// only approves, edits, or rejects. The prepared result is linked from the
// item, reusing a field where one exists:
//
//   - a question with an agent's proposed answer (question_answer from an
//     agent): kind answer;
//   - an outgoing io draft with text: the draft itself, kind draft;
//   - an incoming io item, or an ask made to the owner, that an open outgoing
//     draft answers (the draft's thread links it): kind draft, ref the draft;
//   - anything else (filled fields, a meeting brief and agenda, a short plan):
//     the item's prepared key, set by item_prepared, with the item's hash
//     (model.ItemHash) at that time.
//
// Ready is prepared and not stale: for the prepared key, the item still
// hashes as it did when it was recorded (an edit after it makes it raw) and
// its ref, if any, is open. Value order lists ready items before raw ones
// within each group.
//
// On each tick, for the first prep.ahead (default 5) needs-you rows an agent
// can prepare (prep), one that is not ready, has no open agent work, and was
// not asked for at this version gets a request of kind prepare. Like
// fill-gap, it does not take the item from the owner, and it is made only when
// tick can queue. A done prepare has nothing to check: the item shows Ready.

// PrepareKind is the kind of a request to prepare one item.
const PrepareKind = "prepare"

// PrepAheadKey is the vault setting of how many prep rows the tick keeps
// prepared, from the top of the value order.
const PrepAheadKey = "prep.ahead"

// DefaultPrepAhead is prep.ahead when unset.
const DefaultPrepAhead = 5

// Readiness is what was prepared for an item, and the document that holds it
// ("" when it is on the item).
type Readiness struct {
	Kind model.PreparedKind `json:"kind"`
	Ref  string             `json:"ref,omitempty"`
}

// Ready says whether the open item is prepared and not stale.
func (sn *Snapshot) Ready(id string) (Readiness, bool) {
	doc, err := sn.Get(id)
	if err != nil || lifeOf(doc.Meta) != LifeOpen {
		return Readiness{}, false
	}
	switch m := doc.Meta.(type) {
	case *model.Question:
		if m.HasProposal() {
			return Readiness{Kind: model.PreparedAnswer}, true
		}
	case *model.IO:
		if m.Direction == model.IOOut && m.State == model.IODraft && strings.TrimSpace(doc.Body) != "" {
			return Readiness{Kind: model.PreparedDraft, Ref: doc.ID}, true
		}
		if m.Direction == model.IOIn {
			if r := sn.replyDraft(doc.ID); r != "" {
				return Readiness{Kind: model.PreparedDraft, Ref: r}, true
			}
		}
	case *model.RFX:
		if m.Direction == model.RFXIn {
			if r := sn.replyDraft(doc.ID); r != "" {
				return Readiness{Kind: model.PreparedDraft, Ref: r}, true
			}
		}
	}
	p := doc.Meta.Header().Prepared
	if p == nil || p.Hash != model.ItemHash(doc.Meta, doc.Body) {
		return Readiness{}, false
	}
	if ref := p.Ref.Target(); ref != "" {
		if rd, err := sn.Get(ref); err != nil || lifeOf(rd.Meta) != LifeOpen {
			return Readiness{}, false
		}
	}
	return Readiness{Kind: p.Kind, Ref: p.Ref.Target()}, true
}

// replyDraft is the open outgoing draft (draft or approved, not sent) whose
// thread links the item, or "". The first in list order wins.
func (sn *Snapshot) replyDraft(id string) string {
	if sn.replies == nil {
		sn.replies = map[string]string{}
		for _, doc := range sn.Docs() {
			m, ok := doc.Meta.(*model.IO)
			if !ok || m.Direction != model.IOOut || (m.State != model.IODraft && m.State != model.IOReady) || m.Thread == "" {
				continue
			}
			if _, seen := sn.replies[m.Thread.Target()]; !seen {
				sn.replies[m.Thread.Target()] = doc.ID
			}
		}
	}
	return sn.replies[id]
}

// ItemPreparedInput is the input of item_prepared.
type ItemPreparedInput struct {
	ID string `json:"id"` // the item prepared
	// Kind is what was prepared: draft, answer, fields, brief, plan, or
	// artifact (what an agent produced for an obligation it owes).
	Kind model.PreparedKind `json:"kind"`
	// Ref is the document that holds it, such as an io draft; empty when it
	// is on the item (its fields, steps, or notes).
	Ref string `json:"ref,omitempty"`
}

// ItemPreparedOutput is the output of item_prepared.
type ItemPreparedOutput struct {
	ID       string         `json:"id"`
	Prepared model.Prepared `json:"prepared"`
	Rev      string         `json:"rev"`
	// Closed is set when the artifact closed its occurrence: an in-vault
	// artifact (journal, report, doc, other) can be undone, so it needs no
	// approval; Next is the next occurrence of its series.
	Closed bool   `json:"closed,omitempty"`
	Next   string `json:"next,omitempty"`
}

// preparedTypes are the types item_prepared marks: what needs the owner.
var preparedTypes = []model.Type{model.TypeTask, model.TypeObligation, model.TypeMeeting, model.TypeQuestion, model.TypeRFX, model.TypeIO, model.TypeProject, model.TypeIncident}

// ItemPrepared records that an agent prepared an item.
var ItemPrepared = defineSafe("item_prepared", "Record that you prepared an open item so the owner only approves: kind draft, answer, fields, brief, plan, or artifact (the journal summary, document, or io draft you produced for an obligation you owe, with ref; any artifact but a message closes the occurrence done at once, undoable), and ref, the document that holds it (an io draft) or empty when it is on the item (its fields, steps, or notes). Write the item first: the record binds the item as it is now, and an edit after it makes the item raw again. Not needed for a proposed answer on a question, or for a draft whose thread links the item: those read as ready by themselves. Never send or act outward to prepare: drafts only", func(ctx context.Context, d Deps, in ItemPreparedInput) (ItemPreparedOutput, error) {
	if !slices.Contains(model.PreparedKinds, in.Kind) {
		return ItemPreparedOutput{}, fmt.Errorf("kind %q: want draft, answer, fields, brief, plan, or artifact", in.Kind)
	}
	doc, err := d.Store.Get(ctx, strings.TrimSpace(in.ID))
	if err != nil {
		return ItemPreparedOutput{}, err
	}
	if !slices.Contains(preparedTypes, doc.Meta.Header().Type) {
		return ItemPreparedOutput{}, fmt.Errorf("%s is a %s: only a task, meeting, question, ask, io item, goal, or incident is prepared", doc.ID, doc.Meta.Header().Type)
	}
	if lifeOf(doc.Meta) != LifeOpen {
		return ItemPreparedOutput{}, fmt.Errorf("%s is closed: only an open item is prepared", doc.ID)
	}
	if o, ok := doc.Meta.(*model.Obligation); in.Kind == model.PreparedArtifact && (!ok || o.Produce == nil) {
		return ItemPreparedOutput{}, fmt.Errorf("kind artifact is for an obligation an agent owes (produce): %s is not one", doc.ID)
	}
	var ref model.Link
	if r := strings.TrimSpace(in.Ref); r != "" {
		rd, err := d.Store.Get(ctx, r)
		if errors.Is(err, model.ErrNotFound) {
			return ItemPreparedOutput{}, fmt.Errorf("ref %s: not found", r)
		}
		if err != nil {
			return ItemPreparedOutput{}, err
		}
		if rd.ID == doc.ID {
			return ItemPreparedOutput{}, errors.New("ref is the item itself: leave ref empty")
		}
		ref = model.LinkTo(rd.ID)
	}
	p := &model.Prepared{By: d.Store.Info().Actor, At: nowString(d.Store), Kind: in.Kind, Ref: ref}
	p.Hash = model.ItemHash(doc.Meta, doc.Body)
	doc.Meta.Header().Prepared = p
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return ItemPreparedOutput{}, fmt.Errorf("item_prepared: %w", err)
	}
	out := ItemPreparedOutput{ID: saved.ID, Prepared: *p, Rev: saved.Rev}
	if o, ok := doc.Meta.(*model.Obligation); ok && in.Kind == model.PreparedArtifact && o.Produce.Kind != model.ProduceMessage {
		// An in-vault artifact is undoable, so it needs no approval: the
		// occurrence closes done (its receipt undoes it; reopen brings it back).
		cl, err := obligationClose.Run(ctx, d, obligationCloseInput{ID: saved.ID, Reason: "produced " + string(o.Produce.Kind) + artifactRef(ref)})
		if err != nil {
			return out, fmt.Errorf("item_prepared: close: %w", err)
		}
		out.Closed, out.Next, out.Rev = true, cl.Next, cl.Rev
	}
	return out, nil
})

// tickPrepare makes the prepare requests of this pass.
func tickPrepare(ctx context.Context, d Deps, out *TickOutput) error {
	ahead, err := configInt(d.Store, PrepAheadKey, DefaultPrepAhead)
	if err != nil || ahead == 0 {
		return err
	}
	if ok, err := canQueue(d, TickSurface); err != nil || !ok {
		if err == nil {
			out.notQueued()
		}
		return err
	}
	sn, err := NewSnapshot(ctx, d.Store)
	if err != nil {
		return err
	}
	o, err := OrderOf(sn)
	if err != nil {
		return err
	}
	open := openWorkOn(sn)
	w := workOf(sn, PrepareKind)
	n := 0
	for _, r := range o.Rows {
		if !r.Prep {
			continue
		}
		if n++; n > ahead {
			break
		}
		doc, err := sn.Get(r.ID)
		if err != nil {
			return err
		}
		v := model.ItemHash(doc.Meta, doc.Body)
		if r.Ready || open[r.ID] || w.tried[r.ID+" "+v] {
			continue
		}
		req, err := addRequest(ctx, d, RequestAddInput{Title: "Prepare: " + shorten(doc.Title), Ask: prepareAsk(doc, v), Kind: PrepareKind, Links: []string{r.ID}}, "")
		if err != nil {
			out.Warnings = append(out.Warnings, fmt.Sprintf("prepare %s: %v", r.ID, err))
			continue
		}
		w.tried[r.ID+" "+v] = true
		out.Prepares = append(out.Prepares, req.ID)
		out.noteRequest(ctx, d, req.ID)
	}
	return nil
}

// openWorkOn is the set of items that open agent work (of any kind) links.
func openWorkOn(sn *Snapshot) map[string]bool {
	out := map[string]bool{}
	for _, doc := range sn.Docs() {
		if r, ok := doc.Meta.(*model.Request); ok && requestOpen(r) {
			for _, l := range r.Links {
				out[l.Target()] = true
			}
		}
	}
	return out
}

// prepareAsk is the body of a prepare request: what to prepare for the kind
// of item.
func prepareAsk(doc model.Document, version string) string {
	var what string
	switch m := doc.Meta.(type) {
	case *model.Question:
		what = "Research the question and propose an answer with sources: question_answer from you proposes it, and the owner accepts, edits, or rejects it. Do not decide for the owner; for a decision, recommend an option and say why."
	case *model.RFX:
		what = "Draft the owner's reply to this ask: io_add with direction out, state draft, thread " + doc.ID + ", to the person who asked, and the text. The draft is ready by its thread; the owner approves it."
	case *model.IO:
		if m.Direction == model.IOOut {
			what = "Write the text of this outgoing draft (io_edit). The owner approves it."
		} else {
			what = "Draft the owner's reply to this message: io_add with direction out, state draft, medium " + string(m.Medium) + ", thread " + doc.ID + ", to the sender, and the text. The draft is ready by its thread; the owner approves it."
		}
	default:
		what = "Prepare what the owner needs to do it in one sitting: a short plan (task_edit steps), the brief and agenda of a meeting, or the missing fields; then item_prepared {id: " + doc.ID + ", kind: plan, brief, or fields}."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Prepare %s (%s) so the owner only approves it.\n\n%s\n\n", doc.Title, doc.ID, what)
	b.WriteString("Never send, post, or act outward: drafts only. Cite what you used. Pass request (this request's id) on each write. ")
	b.WriteString("End with request_done linking what you made; when you cannot prepare it, say why.\n\n")
	b.WriteString("Item version: " + version + "\n")
	return b.String()
}

// artifactRef is " [[id]]" for a produced document, or "".
func artifactRef(ref model.Link) string {
	if ref == "" {
		return ""
	}
	return " " + string(ref)
}
