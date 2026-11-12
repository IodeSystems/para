package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/iodesystems/para/internal/model"
)

// DependsSetInput is the input of depends_set. Give on or reason, not both.
type DependsSetInput struct {
	ID string `json:"id"` // the item that waits: a task, obligation, rfx, or question
	// On is the item it waits on: a task, obligation, rfx, question, goal, or
	// agent work.
	On string `json:"on,omitempty"`
	// Reason is a text reason a task waits (blocked_by), for what is not an
	// item: adding one marks the task blocked, removing the last unblocks it.
	Reason string `json:"reason,omitempty"`
	// Optional puts on in optional_after: better done first, but it never
	// holds the item. false puts it in depends_on. Setting an edge that is
	// there already moves it between the two.
	Optional bool `json:"optional,omitempty"`
	Remove   bool `json:"remove,omitempty"` // take on or reason away
}

// DependsSetOutput is what the item waits on after the change.
type DependsSetOutput struct {
	ID            string   `json:"id"`
	DependsOn     []string `json:"depends_on"`
	OptionalAfter []string `json:"optional_after"`
	BlockedBy     []string `json:"blocked_by"`
	Rev           string   `json:"rev"`
}

// DependsSet adds, removes, or changes one dependency or text reason of an
// item.
var DependsSet = defineSafe("depends_set", "Record what an item (task, obligation, rfx, question) waits on: on (an item id) or reason (text, a task only; marks it blocked). optional true: better before, shown but never holds the item (no wait, start date, inherited due date, or cycle); false (default): it waits. Setting an edge that is there moves it between hard and optional. remove takes on or reason away. An item cannot wait on itself; a hard edge that closes a cycle is refused", func(ctx context.Context, d Deps, in DependsSetInput) (DependsSetOutput, error) {
	id := normID(in.ID)
	on, reason := normID(in.On), strings.Join(strings.Fields(in.Reason), " ")
	if (on == "") == (reason == "") {
		return DependsSetOutput{}, errors.New("give on (an item id) or reason (text), not both")
	}
	doc, err := d.Store.Get(ctx, id)
	if err != nil {
		return DependsSetOutput{}, linkErr("id", id, err)
	}
	hard, opt := model.DepLists(doc.Meta)
	if hard == nil {
		return DependsSetOutput{}, fmt.Errorf("%s is a %s: want a task, obligation, rfx, or question", id, doc.Meta.Header().Type)
	}
	if !in.Remove && lifeOf(doc.Meta) == LifeClosed {
		return DependsSetOutput{}, fmt.Errorf("%s is already %s", id, statusOf(doc.Meta))
	}
	if reason != "" {
		err = setReason(doc, reason, in.Remove, in.Optional)
	} else {
		err = setEdge(ctx, d, doc.ID, hard, opt, on, in.Remove, in.Optional)
	}
	if err != nil {
		return DependsSetOutput{}, err
	}
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return DependsSetOutput{}, err
	}
	out := DependsSetOutput{ID: doc.ID, DependsOn: targets(*hard), OptionalAfter: targets(*opt), BlockedBy: []string{}, Rev: saved.Rev}
	if t, ok := doc.Meta.(*model.Task); ok {
		out.BlockedBy = append(out.BlockedBy, t.BlockedBy...)
	}
	return out, nil
})

// setEdge adds, moves, or removes the edge id -> on.
func setEdge(ctx context.Context, d Deps, id string, hard, opt *[]model.Link, on string, remove, optional bool) error {
	l := model.LinkTo(on)
	is := func(x model.Link) bool { return x.Target() == on }
	if remove {
		if !slices.ContainsFunc(*hard, is) && !slices.ContainsFunc(*opt, is) {
			return fmt.Errorf("%s does not wait on %s", id, on)
		}
		*hard, *opt = slices.DeleteFunc(*hard, is), slices.DeleteFunc(*opt, is)
		return nil
	}
	if on == id {
		return fmt.Errorf("%s cannot depend on itself", id)
	}
	dep, err := d.Store.Get(ctx, on)
	if err != nil {
		return linkErr("on", on, err)
	}
	switch dep.Meta.Header().Type {
	case model.TypeTask, model.TypeObligation, model.TypeRFX, model.TypeQuestion, model.TypeGoal, model.TypeRequest, model.TypeIO:
	default:
		return fmt.Errorf("on %s is a %s: want a task, obligation, rfx, question, goal, agent work, or io item", on, dep.Meta.Header().Type)
	}
	if optional {
		// An optional edge never waits, so it cannot deadlock: no cycle check.
		*hard = slices.DeleteFunc(*hard, is)
		if !slices.ContainsFunc(*opt, is) {
			*opt = append(*opt, l)
		}
		return nil
	}
	if slices.ContainsFunc(*hard, is) {
		return nil
	}
	// The edge id -> on closes a cycle when on already reaches id.
	back, err := dependencyPath(ctx, d.Store, on, id)
	if err != nil {
		return err
	}
	if back != nil {
		return fmt.Errorf("%s -> %s would make a dependency cycle: %s -> %s", id, on, id, strings.Join(back, " -> "))
	}
	*opt = slices.DeleteFunc(*opt, is)
	*hard = append(*hard, l)
	return nil
}

// setReason adds or removes a text reason of a task. Adding one marks the
// task blocked, as task_block does; removing the last one of a blocked task
// makes it todo and drops the follow-up date the block set.
func setReason(doc model.Document, reason string, remove, optional bool) error {
	t, ok := doc.Meta.(*model.Task)
	if !ok {
		return fmt.Errorf("%s is a %s: a text reason is for a task; give on for an item", doc.ID, doc.Meta.Header().Type)
	}
	if optional {
		return errors.New("a text reason cannot be optional: it blocks the task, or remove it")
	}
	if !remove {
		if !slices.Contains(t.BlockedBy, reason) {
			t.BlockedBy = append(t.BlockedBy, reason)
		}
		t.Status = model.TaskBlocked
		return nil
	}
	if !slices.Contains(t.BlockedBy, reason) {
		return fmt.Errorf("%s is not blocked by %q", doc.ID, reason)
	}
	t.BlockedBy = slices.DeleteFunc(t.BlockedBy, func(s string) bool { return s == reason })
	if len(t.BlockedBy) == 0 && t.Status == model.TaskBlocked {
		t.Status = model.TaskTodo
		if blockDeferral(t) {
			t.DeferUntil, t.DeferredReason = "", ""
		}
	}
	return nil
}
