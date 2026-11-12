package ops

import (
	"cmp"

	"github.com/iodesystems/para/internal/model"
)

// Blocker is one open item an item waits on (depends_on), in words for the
// owner: Type is a label (output or input for an io item by direction, task,
// Ask, question, ...), State a label (unapproved, approved, waiting for
// reply, open, ...).
type Blocker struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	State string `json:"state"`
	Title string `json:"title"`
}

// blockersOf is the open items of ids, as Blockers, in order. get reads a
// document; a missing one or a closed one is left out.
func blockersOf(get func(id string) (model.Document, bool), ids []string) []Blocker {
	out := []Blocker{}
	for _, id := range ids {
		doc, ok := get(id)
		if !ok || resolved(doc) {
			continue
		}
		out = append(out, blockerOf(doc))
	}
	return out
}

// blockerOf labels one open item.
func blockerOf(doc model.Document) Blocker {
	b := Blocker{ID: doc.ID, Title: cmp.Or(doc.Title, doc.ID), Type: string(doc.Meta.Header().Type), State: statusOf(doc.Meta)}
	switch m := obligationShape(doc).Meta.(type) {
	case *model.IO:
		b.Type = "input"
		if m.Direction == model.IOOut {
			b.Type = "output"
		}
		switch {
		case m.State == model.IODraft:
			b.State = "unapproved"
		case m.State == model.IOReady && !m.Approved(doc.Body):
			b.State = "changed after approval"
		case m.State == model.IOReady:
			b.State = "approved"
		case m.Direction == model.IOIn:
			b.State = "to read"
		}
	case *model.Task:
		b.Type = cmp.Or(string(m.EffectiveKind()), "task")
		if m.Status == model.TaskTodo {
			b.State = "open"
		}
	case *model.RFX:
		b.Type = "Ask"
		if m.Direction == model.RFXIn {
			b.State = "asked of you"
		} else {
			b.State = "waiting for reply"
		}
	case *model.Question:
		b.State = "open"
		if m.HasProposal() {
			b.State = "answer to check"
		}
	case *model.Request:
		b.Type = "agent work"
	}
	return b
}
