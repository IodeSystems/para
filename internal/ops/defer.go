package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/store"
)

// Later: a person puts a question, a task, or a goal off. It keeps its place in
// the dependency graph, so it still blocks what depends on it, and it comes
// back on its date in local time. Only a person defers, so an agent cannot hide
// work. Events carry ids and metadata, never the reason.

// deferralOf returns the deferral of a deferrable document, or nil.
func deferralOf(m model.Meta) *model.Deferral {
	switch x := m.(type) {
	case *model.Task:
		return &x.Deferral
	case *model.Question:
		return &x.Deferral
	case *model.Project:
		return &x.Deferral
	}
	return nil
}

// DeferInput is the input of defer. Give ids, scope, or both.
type DeferInput struct {
	IDs    []string `json:"ids,omitempty"`    // questions, tasks, or goals to put off
	Until  string   `json:"until,omitempty"`  // YYYY-MM-DD; empty means someday
	Reason string   `json:"reason,omitempty"` // why it can wait
	// Scope is a goal or an area: every open question in it that blocks nothing
	// and has no proposed answer is put off.
	Scope string `json:"scope,omitempty"`
}

// DeferOutput is the output of defer and undefer.
type DeferOutput struct {
	Changed []string `json:"changed"` // ids that changed
	Skipped []string `json:"skipped"` // ids left as they were, with the reason after a colon
	Until   string   `json:"until,omitempty"`
}

func deferOnce(ctx context.Context, d Deps, id, until, reason string, clear bool) (changed bool, skip string, err error) {
	doc, err := d.Store.Get(ctx, normID(id))
	if err != nil {
		return false, "", err
	}
	df := deferralOf(doc.Meta)
	if df == nil {
		return false, "", fmt.Errorf("%s is a %s: only a question, a task, or a goal can be put off", doc.ID, doc.Meta.Header().Type)
	}
	switch m := doc.Meta.(type) {
	case *model.Task:
		if taskTerminal(m.Status) {
			return false, doc.ID + ": already " + string(m.Status), nil
		}
	case *model.Question:
		if m.Status != model.QuestionOpen {
			return false, doc.ID + ": already " + string(m.Status), nil
		}
	case *model.Project:
		if m.Status == model.ProjectDone || m.Status == model.ProjectDropped {
			return false, doc.ID + ": already " + string(m.Status), nil
		}
	}
	if clear {
		if df.DeferUntil == "" {
			return false, doc.ID + ": not put off", nil
		}
		*df = model.Deferral{}
	} else {
		*df = model.Deferral{DeferUntil: until, DeferredReason: reason}
	}
	if _, err := d.Store.Update(ctx, doc); err != nil {
		return false, "", err
	}
	return true, "", nil
}

// Defer puts questions, tasks, and goals off until a date or someday.
var Defer = definePerson("defer", "Put questions, tasks, or goals off until a date, or someday with no date; they leave Today and next but still block what depends on them. With scope, put off every open question in a goal or area that blocks nothing", func(ctx context.Context, d Deps, in DeferInput) (DeferOutput, error) {
	until := strings.TrimSpace(in.Until)
	today := d.Store.Now().Format(dateLayout)
	switch {
	case until == "":
		until = model.SomedayWord
	case until != model.SomedayWord:
		t, err := time.Parse(dateLayout, until)
		if err != nil {
			return DeferOutput{}, fmt.Errorf("until %q: want YYYY-MM-DD, or leave it out for someday", until)
		}
		until = t.Format(dateLayout)
		if until <= today {
			return DeferOutput{}, fmt.Errorf("until %s is not after today (%s)", until, today)
		}
	}
	reason := strings.Join(strings.Fields(in.Reason), " ")
	if err := lintText("reason", reason); err != nil {
		return DeferOutput{}, err
	}
	ids := slices.Clone(in.IDs)
	if scope := normID(in.Scope); scope != "" {
		sd, err := d.Store.Get(ctx, scope)
		if err != nil {
			return DeferOutput{}, fmt.Errorf("scope %s: %w", scope, err)
		}
		var qs []QuestionView
		switch sd.Meta.(type) {
		case *model.Project:
			qs, err = openQuestions(ctx, d.Store, func(x qrow) bool { return x.project == sd.ID })
		case *model.Area:
			qs, err = openQuestions(ctx, d.Store, func(x qrow) bool { return x.area == sd.ID })
		default:
			return DeferOutput{}, fmt.Errorf("scope %s is a %s: give a goal or an area", sd.ID, sd.Meta.Header().Type)
		}
		if err != nil {
			return DeferOutput{}, err
		}
		for _, q := range qs {
			if q.Blocking == 0 && !q.HasProposal && !q.Deferred {
				ids = append(ids, q.ID)
			}
		}
	}
	if len(ids) == 0 {
		return DeferOutput{}, errors.New("nothing to put off: give ids or a scope with open questions that block nothing")
	}
	out := DeferOutput{Changed: []string{}, Skipped: []string{}, Until: until}
	seen := map[string]bool{}
	for _, id := range ids {
		if id = normID(id); id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ok, skip, err := deferOnce(ctx, d, id, until, reason, false)
		if err != nil {
			return out, err
		}
		if ok {
			out.Changed = append(out.Changed, id)
		} else {
			out.Skipped = append(out.Skipped, skip)
		}
	}
	return out, nil
})

// UndeferInput is the input of undefer.
type UndeferInput struct {
	IDs []string `json:"ids"`
}

// Undefer brings put-off items back.
var Undefer = definePerson("undefer", "Bring questions, tasks, or goals that were put off back to Today", func(ctx context.Context, d Deps, in UndeferInput) (DeferOutput, error) {
	if len(in.IDs) == 0 {
		return DeferOutput{}, errors.New("ids is empty")
	}
	out := DeferOutput{Changed: []string{}, Skipped: []string{}}
	for _, id := range in.IDs {
		if id = normID(id); id == "" {
			continue
		}
		ok, skip, err := deferOnce(ctx, d, id, "", "", true)
		if err != nil {
			return out, err
		}
		if ok {
			out.Changed = append(out.Changed, id)
		} else {
			out.Skipped = append(out.Skipped, skip)
		}
	}
	return out, nil
})

// deferredSet returns the ids that are put off on the date today: an item with
// its own deferral in force, a task or question whose goal is put off, and a
// question whose scope task is put off.
func deferredSet(ctx context.Context, s store.Store, today string) (map[string]bool, error) {
	refs, err := s.List(ctx, store.Filter{})
	if err != nil {
		return nil, err
	}
	out := map[string]bool{}
	goal := map[string]string{} // task id -> goal id
	scope := map[string]string{}
	for _, r := range refs {
		switch r.Type {
		case model.TypeTask, model.TypeQuestion, model.TypeProject:
		default:
			continue
		}
		doc, err := s.Get(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		if df := deferralOf(doc.Meta); df != nil && df.IsDeferred(today) {
			out[doc.ID] = true
		}
		switch m := doc.Meta.(type) {
		case *model.Task:
			goal[doc.ID] = m.Project.Target()
		case *model.Question:
			scope[doc.ID] = scopeID(m)
		}
	}
	for id, g := range goal {
		if out[g] {
			out[id] = true
		}
	}
	for id, sc := range scope {
		if out[sc] || out[goal[sc]] {
			out[id] = true
		}
	}
	return out, nil
}
