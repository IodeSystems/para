package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// Later: a person puts a question, a task, or a goal off. It keeps its place in
// the dependency graph, so it still blocks what depends on it, and it comes
// back on its date in local time. An agent's defer is held for review unless
// it carries the owner's words (owner_said), so an agent cannot hide work.
// Events carry ids and metadata, never the reason.

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

// addDepends adds one dependency with the depends op of the item's type.
func addDepends(ctx context.Context, d Deps, doc model.Document, dep string) error {
	var err error
	switch doc.Meta.(type) {
	case *model.Task:
		_, err = TaskDependsAdd.Run(ctx, d, TaskDependsInput{ID: doc.ID, DependsOn: []string{dep}})
	case *model.Question:
		_, err = QuestionDependsAdd.Run(ctx, d, QuestionDependsInput{ID: doc.ID, DependsOn: []string{dep}})
	}
	return err
}

// DeferInput is the input of defer. Give ids, scope, or both.
type DeferInput struct {
	IDs    []string `json:"ids,omitempty"`    // questions, tasks, or goals to put off
	Until  string   `json:"until,omitempty"`  // YYYY-MM-DD; empty means someday
	Reason string   `json:"reason,omitempty"` // why it can wait
	// Scope is a goal or an area: every open question in it that blocks nothing
	// and has no proposed answer is put off.
	Scope string `json:"scope,omitempty"`
	// After is the id of a task, question, ask, goal, or agent work to wait for:
	// the item stays under Later until it is done, through a dependency. When
	// is a condition in words: defer makes a task with that title and done_when
	// and waits on it. Give After or When, not both. Place is the goal or area
	// of that task; it defaults to the item's own, and a root question needs it.
	// New makes the task even when a similar open task exists.
	After string `json:"after,omitempty"`
	When  string `json:"when,omitempty"`
	Place string `json:"place,omitempty"`
	New   bool   `json:"new,omitempty"`
}

// DeferOutput is the output of defer and undefer.
type DeferOutput struct {
	Changed []string `json:"changed"` // ids that changed
	Skipped []string `json:"skipped"` // ids left as they were, with the reason after a colon
	Until   string   `json:"until,omitempty"`
	// After is the id the items wait on; Created is the task made for When.
	After   string `json:"after,omitempty"`
	Created string `json:"created,omitempty"`
	// Similar lists open tasks like the When text. Nothing was changed: repeat
	// with After set to one of them, or with New.
	Similar []Similar `json:"similar,omitempty"`
}

// dependsOf returns the depends_on list of a task or question, or nil.
func dependsOf(m model.Meta) *[]model.Link {
	switch x := m.(type) {
	case *model.Task:
		return &x.DependsOn
	case *model.Question:
		return &x.DependsOn
	}
	return nil
}

// dropAdded removes from depends_on the dependencies that defer added.
func dropAdded(m model.Meta, df *model.Deferral) {
	if dl := dependsOf(m); dl != nil {
		for _, id := range df.DeferredAdded {
			*dl = slices.DeleteFunc(*dl, func(x model.Link) bool { return x == model.LinkTo(id) })
		}
	}
}

func deferOnce(ctx context.Context, d Deps, id, until, reason, after string, clear bool) (changed bool, skip string, err error) {
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
		if df.DeferUntil == "" && len(df.DeferredAfter) == 0 {
			return false, doc.ID + ": not put off", nil
		}
		dropAdded(doc.Meta, df)
		*df = model.Deferral{}
	} else {
		var added []string
		if after != "" {
			dl := dependsOf(doc.Meta)
			if dl == nil {
				return false, "", fmt.Errorf("%s is a goal: a goal cannot wait on an item, put it off with a date", doc.ID)
			}
			dep, err := d.Store.Get(ctx, after)
			if err != nil {
				return false, "", fmt.Errorf("after %s: %w", after, err)
			}
			if resolved(dep) {
				return false, "", fmt.Errorf("after %s: already done, nothing to wait for", dep.ID)
			}
			manual := slices.Contains(*dl, model.LinkTo(after))
			// Add the wait with the existing depends op, which refuses a cycle.
			if !manual {
				if err := addDepends(ctx, d, doc, after); err != nil {
					return false, "", err
				}
				added = []string{after}
			}
			if doc, err = d.Store.Get(ctx, doc.ID); err != nil {
				return false, "", err
			}
			df = deferralOf(doc.Meta)
		}
		dropAdded(doc.Meta, df)
		*df = model.Deferral{DeferUntil: until, DeferredReason: reason, DeferredAdded: added}
		if after != "" {
			df.DeferredAfter = []string{after}
		}
	}
	if _, err := d.Store.Update(ctx, doc); err != nil {
		return false, "", err
	}
	return true, "", nil
}

// Defer puts questions, tasks, and goals off until a date or someday.
var Defer = withGateRule(define("defer", "Put questions, tasks, or goals off until a date, or someday with no date; they leave the agenda and next but still block what depends on them. With scope, put off every open question in a goal or area that blocks nothing. "+
	"Gate: an agent's defer is held for review unless the call carries owner_said (the owner's words in this session)", func(ctx context.Context, d Deps, in DeferInput) (DeferOutput, error) {
	until := strings.TrimSpace(in.Until)
	today := d.Store.Now().Format(dateLayout)
	after, when := normID(in.After), strings.Join(strings.Fields(in.When), " ")
	if after != "" && when != "" {
		return DeferOutput{}, errors.New("give after or when, not both")
	}
	if (after != "" || when != "") && strings.TrimSpace(in.Scope) != "" {
		return DeferOutput{}, errors.New("after and when apply to ids, not to a scope")
	}
	switch {
	case until == "" && (after != "" || when != ""):
		// Waits only: no date.
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
			return DeferOutput{}, linkErr("scope", scope, err)
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
	if when != "" {
		if err := lintText("when", when); err != nil {
			return DeferOutput{}, err
		}
		var err error
		if after, out.Similar, err = whenTask(ctx, d, ids, when, normID(in.Place), in.New); err != nil || len(out.Similar) > 0 {
			return out, err
		}
		out.Created = after
	}
	out.After = after
	seen := map[string]bool{}
	for _, id := range ids {
		if id = normID(id); id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ok, skip, err := deferOnce(ctx, d, id, until, reason, after, false)
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
}), deferGate)

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
		ok, skip, err := deferOnce(ctx, d, id, "", "", "", true)
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

// deferHold says why an item is under Later, or that it has just become ready.
type deferHold struct {
	Date      bool     // its own date or someday is in force
	Waits     []string // open items in deferred_after
	Inherited bool     // its goal or scope task is put off
	Ready     string   // "ready: <title> done" for 7 days after the last wait resolved
}

func (h deferHold) any() bool { return h.Date || len(h.Waits) > 0 || h.Inherited }

// waitOnly is true when only open waits hold the item.
func (h deferHold) waitOnly() bool { return !h.Date && !h.Inherited && len(h.Waits) > 0 }

// readyDays is how long a "ready" row shows after the wait resolved.
const readyDays = 7

// waitState reads the items a deferral waits on: those still open, and the
// "ready" reason when the last one resolved within readyDays of today. A
// missing item does not hold.
func waitState(ctx context.Context, s store.Store, df model.Deferral, today string) (waits []string, ready string, err error) {
	var latest, title string
	for _, id := range df.DeferredAfter {
		doc, gerr := s.Get(ctx, id)
		if errors.Is(gerr, model.ErrNotFound) {
			continue
		}
		if gerr != nil {
			return nil, "", gerr
		}
		if !resolved(doc) {
			waits = append(waits, id)
			continue
		}
		when := string(doc.Meta.Header().Updated)
		if q, ok := doc.Meta.(*model.Question); ok && q.Answered != "" {
			when = string(q.Answered)
		}
		if when == "" {
			when = string(doc.Meta.Header().Created)
		}
		if latest == "" || when > latest {
			latest, title = when, doc.Title
		}
	}
	if len(waits) == 0 && latest != "" {
		if t, perr := time.Parse(dateLayout, today); perr == nil && latest >= t.AddDate(0, 0, -readyDays).Format(dateLayout) {
			ready = "ready: " + title + " done"
		}
	}
	return waits, ready, nil
}

// deferredSet returns, by id, why an item is under Later on the date today: an
// item with its own deferral in force or an open wait, a task or question whose
// goal is put off, and a question whose scope task is put off. An item with
// only Ready set is not under Later.
func deferredSet(ctx context.Context, s store.Store, today string) (map[string]deferHold, error) {
	refs, err := s.List(ctx, store.Filter{})
	if err != nil {
		return nil, err
	}
	out := map[string]deferHold{}
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
		if df := deferralOf(doc.Meta); df != nil {
			h := deferHold{Date: df.IsDeferred(today)}
			if len(df.DeferredAfter) > 0 && openItem(doc.Meta) {
				if h.Waits, h.Ready, err = waitState(ctx, s, *df, today); err != nil {
					return nil, err
				}
			}
			// A blocked task's follow-up date is its defer date: it is under
			// Later until then, and back in next on the day.
			if t, ok := doc.Meta.(*model.Task); ok && t.Status == model.TaskBlocked && t.Due != "" && string(t.Due) > today {
				h.Date = true
			}
			if h.Date || h.Waits != nil || h.Ready != "" {
				out[doc.ID] = h
			}
		}
		switch m := doc.Meta.(type) {
		case *model.Task:
			goal[doc.ID] = m.Project.Target()
		case *model.Question:
			scope[doc.ID] = scopeID(m)
		}
	}
	for id, g := range goal {
		if out[g].any() {
			h := out[id]
			h.Inherited = true
			out[id] = h
		}
	}
	for id, sc := range scope {
		if out[sc].any() || out[goal[sc]].any() {
			h := out[id]
			h.Inherited = true
			out[id] = h
		}
	}
	return out, nil
}

// openItem is true for a task, question, or goal that is not finished. A ready
// reason shows only on a task not yet started and an open question.
func openItem(m model.Meta) bool {
	switch x := m.(type) {
	case *model.Task:
		return x.Status == model.TaskTodo
	case *model.Question:
		return x.Status == model.QuestionOpen
	case *model.Project:
		return x.Status == model.ProjectActive
	}
	return false
}

// whenTask makes the task a "when" condition waits on, in the goal or area of
// the first id (or place). A similar open task is returned instead, with
// nothing made, unless force is set.
func whenTask(ctx context.Context, d Deps, ids []string, when, place string, force bool) (string, []Similar, error) {
	if len(ids) == 0 {
		return "", nil, errors.New("nothing to put off: give ids")
	}
	if !force {
		sim, err := FindSimilar(ctx, d.Store, model.TypeTask, when, "")
		if err != nil {
			return "", nil, err
		}
		open := sim[:0]
		for _, x := range sim {
			if doc, err := d.Store.Get(ctx, x.ID); err == nil && !resolved(doc) {
				open = append(open, x)
			}
		}
		if len(open) > 0 {
			return "", open, nil
		}
	}
	in := TaskAddInput{Title: when, DoneWhen: when}
	if place != "" {
		pd, err := d.Store.Get(ctx, place)
		if err != nil {
			return "", nil, fmt.Errorf("place %s: %w", place, err)
		}
		switch pd.Meta.(type) {
		case *model.Project:
			in.Project = pd.ID
		case *model.Area:
			in.Area = pd.ID
		default:
			return "", nil, fmt.Errorf("place %s is a %s: give a goal or an area", pd.ID, pd.Meta.Header().Type)
		}
	} else {
		first, err := d.Store.Get(ctx, normID(ids[0]))
		if err != nil {
			return "", nil, err
		}
		switch m := first.Meta.(type) {
		case *model.Task:
			in.Project, in.Area = m.Project.Target(), m.Area.Target()
		case *model.Question:
			if sc := scopeID(m); sc != ScopeRootWord {
				sd, err := d.Store.Get(ctx, sc)
				if err != nil {
					return "", nil, err
				}
				switch sm := sd.Meta.(type) {
				case *model.Project:
					in.Project = sd.ID
				case *model.Area:
					in.Area = sd.ID
				case *model.Task:
					in.Project, in.Area = sm.Project.Target(), sm.Area.Target()
				}
			}
		}
	}
	if in.Project == "" && in.Area == "" {
		return "", nil, fmt.Errorf("%s has no goal or area: give place, a goal or an area for the task", normID(ids[0]))
	}
	out, err := TaskAdd.Run(ctx, d, in)
	if err != nil {
		return "", nil, fmt.Errorf("when: %w", err)
	}
	return out.ID, nil, nil
}

// deferGate holds an agent's defer for review unless the owner said it.
func deferGate(_ DeferInput, m WriteMeta) (gateVerdict, string) {
	if strings.TrimSpace(m.OwnerSaid) != "" {
		return gatePass, ""
	}
	return gateStage, "owner_said"
}
