package ops

import (
	"slices"
	"strings"

	"github.com/iodesystems/para/internal/model"
)

// An open incoming message needs the owner only when one of these holds, in
// this order:
//
//  1. a triage rule decided it (io.triage needs_you or digest; triage.go);
//  2. the sender waits for the owner's reply (expects response);
//  3. the sender is priority high: the person, or for a person with no
//     priority of their own, a group they are in (priority.go);
//  4. the source it came through (via.source) is priority high;
//  5. it is the owner's own capture: no sender, no via, and no link; or a
//     medium the owner reports or carries in by hand (verbal, swivel).
//
// Any other incoming message is not the owner's turn: it is listed in the
// day's inbox digest (journal, section inbox-digest) and in the Inbox's
// Digest view, and stays open until filed or dropped. Who and what is high
// is vault data; no name is in the code.

// inboundTurn is whose turn an open incoming message is.
func (sn *Snapshot) inboundTurn(m *model.IO) Need {
	yes, reason := sn.inboundVerdict(m)
	if yes {
		return Need{Turn: TurnOwner, Reason: reason}
	}
	return Need{Turn: TurnNone, Reason: reason}
}

// inboundVerdict is the rule above: whether an incoming message needs the
// owner, and why in words. It does not look at state: a closed message keeps
// its verdict, so the digest can list what came in on a day.
func (sn *Snapshot) inboundVerdict(m *model.IO) (bool, string) {
	switch m.Triage {
	case model.TriageNeedsYou:
		return true, "needs you by triage " + firstOf(m.TriageBy, "rule")
	case model.TriageDigest:
		return false, "digest by triage " + firstOf(m.TriageBy, "rule")
	}
	if m.Expects == model.ExpectsResponse {
		return true, "to answer: the sender waits for your reply"
	}
	if p, from := sn.senderPriority(m.From); p == model.PriorityHigh {
		return true, "from a high-priority sender (" + from + ")"
	}
	if src := sn.sourceOf(m.Via); src != nil && src.Meta.(*model.SourceDoc).Priority == model.PriorityHigh {
		return true, "from a high-priority source (" + src.ID + ")"
	}
	if strings.TrimSpace(m.From) == "" && m.Via == nil && m.Ref == "" || slices.Contains(model.OwnerMediums, m.Medium) {
		return true, "to sort: your capture"
	}
	return false, "digest: no reply expected; sender and source not high"
}

// senderPriority is the priority of a sender that links a person or a group,
// and the document it comes from; "" for free text or a missing document.
func (sn *Snapshot) senderPriority(from string) (model.Priority, string) {
	if !strings.HasPrefix(from, "[[") {
		return "", ""
	}
	doc, err := sn.Get(model.Link(from).Target())
	if err != nil {
		return "", ""
	}
	return partyPriority(sn.Get, doc)
}

// sourceOf is the source document via names, or nil.
func (sn *Snapshot) sourceOf(via *model.IOVia) *model.Document {
	if via == nil {
		return nil
	}
	if sn.sources == nil {
		var docs []model.Document
		if sn.s != nil {
			docs, _ = loadSources(sn.ctx, sn.s)
		}
		r := newSourceResolver(docs)
		sn.sources = &r
	}
	id := sn.sources.resolve(via.Source)
	if id == "" {
		return nil
	}
	d := sn.sources.byID[strings.ToLower(id)]
	return &d
}

// ioSource is the source document an io item cites, read from the store; nil
// when via names none.
func ioSource(sources []model.Document, via *model.IOVia) *model.Document {
	if via == nil {
		return nil
	}
	r := newSourceResolver(sources)
	id := r.resolve(via.Source)
	if id == "" {
		return nil
	}
	d := r.byID[strings.ToLower(id)]
	return &d
}
