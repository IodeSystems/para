package ops

import (
	"context"
	"fmt"
	"slices"

	"github.com/iodesystems/para/internal/model"
)

// Importance is vault data, not a list in config: a person, a group, and a
// source each may carry priority high, normal, or low (empty is normal). A
// person with no priority of their own takes the highest one set on their
// groups. An incoming message from a high sender or source needs the owner
// (needs.go).

// priorityRank orders priorities; unset ranks below low so any set value
// wins.
var priorityRank = map[model.Priority]int{"": 0, model.PriorityLow: 1, model.PriorityNormal: 2, model.PriorityHigh: 3}

// checkPriority refuses a value that is not a priority; empty is allowed.
func checkPriority(p model.Priority) error {
	if p != "" && !slices.Contains(model.Priorities, p) {
		return fmt.Errorf("priority %q: want high, normal, or low", p)
	}
	return nil
}

// partyPriority is the priority of a person or a group, and where it comes
// from: the document itself, or for a person with none, the group with the
// highest priority. get reads a document; a missing group is skipped. Unset
// everywhere is "" with from "".
func partyPriority(get func(string) (model.Document, error), doc model.Document) (p model.Priority, from string) {
	switch m := doc.Meta.(type) {
	case *model.Group:
		if m.Priority != "" {
			return m.Priority, doc.ID
		}
	case *model.Person:
		if m.Priority != "" {
			return m.Priority, doc.ID
		}
		for _, l := range m.Groups {
			g, err := get(l.Target())
			if err != nil {
				continue
			}
			if gm, ok := g.Meta.(*model.Group); ok && priorityRank[gm.Priority] > priorityRank[p] {
				p, from = gm.Priority, g.ID
			}
		}
	}
	return p, from
}

// PrioritySetInput is the input of priority_set.
type PrioritySetInput struct {
	ID string `json:"id"` // a person, a group, or a source
	// Priority is high, normal, or low; empty clears it (a person then takes
	// their groups' priority, a group or a source is normal).
	Priority model.Priority `json:"priority,omitempty"`
}

// PrioritySetOutput is the output of priority_set.
type PrioritySetOutput struct {
	ID       string         `json:"id"`
	Priority model.Priority `json:"priority,omitempty"`
	Rev      string         `json:"rev"`
}

// PrioritySet sets the priority of a person, a group, or a source from the
// web app or the terminal. An agent sets it with doc_edit fields.priority.
var PrioritySet = definePerson("priority_set", "Set how much a person, a group, or a source matters: priority high (every incoming message from it needs the owner), normal, or low; empty clears it, and a person then takes the highest priority of their groups. People only; an agent uses doc_edit with fields.priority", func(ctx context.Context, d Deps, in PrioritySetInput) (PrioritySetOutput, error) {
	if err := checkPriority(in.Priority); err != nil {
		return PrioritySetOutput{}, err
	}
	doc, err := d.Store.Get(ctx, normID(in.ID))
	if err != nil {
		return PrioritySetOutput{}, err
	}
	var cur *model.Priority
	switch m := doc.Meta.(type) {
	case *model.Person:
		cur = &m.Priority
	case *model.Group:
		cur = &m.Priority
	case *model.SourceDoc:
		cur = &m.Priority
	default:
		return PrioritySetOutput{}, fmt.Errorf("%s is %s: priority is set on a person, a group, or a source", doc.ID, doc.Meta.Header().Type)
	}
	out := PrioritySetOutput{ID: doc.ID, Priority: in.Priority, Rev: doc.Rev}
	if *cur == in.Priority {
		return out, nil
	}
	*cur = in.Priority
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return PrioritySetOutput{}, err
	}
	out.Rev = saved.Rev
	return out, nil
})
