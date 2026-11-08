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

// Later: a person puts a question, a task, a goal, an io item, or a dossier
// item that waits for them off. A dossier item and an io item return on their
// date only; the others may also wait on an item. It keeps its place in
// the dependency graph, so it still blocks what depends on it, and it comes
// back on its date in local time. An agent's defer applies, with a receipt
// the owner can undo; undefer is the owner's.
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
	case *model.IO:
		return &x.Deferral
	case *model.Obligation:
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
	// IDs are questions, tasks, goals, or io items to put off; a dossier's id
	// puts off every item of it that waits for the owner.
	IDs []string `json:"ids,omitempty"`
	// Items are dossier items that wait for the owner.
	Items []ItemRef `json:"items,omitempty"`
	// Tag is a topic: every open tagged question, task, goal, and io item,
	// and every tagged dossier item (or item of a tagged dossier) that waits
	// for the owner.
	Tag    string `json:"tag,omitempty"`
	Until  string `json:"until,omitempty"`  // YYYY-MM-DD; empty means someday
	Reason string `json:"reason,omitempty"` // why it can wait
	// Scope is a goal or an area: every open question in it that blocks nothing
	// and has no proposed answer is put off.
	Scope string `json:"scope,omitempty"`
	// After is the id of a task, question, ask, goal, or agent work to wait for:
	// the item stays under Later until it is done, through a dependency. When
	// is a condition in words: defer waits on the open task of that condition
	// (title and done_when the same text, compared without case, spacing, or
	// end punctuation) and lists the items in its body, or makes one. Give
	// After or When, not both. Place is the goal or area of a new task; it
	// defaults to the item's own, and a root question needs it. New always makes
	// a new task, even when an open task has the same or a similar condition;
	// the items put off are never a similar match.
	After string `json:"after,omitempty"`
	When  string `json:"when,omitempty"`
	Place string `json:"place,omitempty"`
	New   bool   `json:"new,omitempty"`
}

// DeferOutput is the output of defer and undefer.
type DeferOutput struct {
	Changed []string `json:"changed"` // ids, and proposal#item for dossier items, that changed
	Skipped []string `json:"skipped"` // ids left as they were, with the reason after a colon
	Until   string   `json:"until,omitempty"`
	// After is the id the items wait on; Created is the task made for When,
	// empty when an open one was reused.
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
		return false, "", fmt.Errorf("%s is a %s: only a question, a task, a goal, an obligation, an io item, or a dossier item can be put off", doc.ID, doc.Meta.Header().Type)
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
	case *model.IO:
		if !m.State.Open() {
			return false, doc.ID + ": already " + string(m.State), nil
		}
	case *model.Obligation:
		if !m.Open() {
			return false, doc.ID + ": already closed (" + string(m.Outcome) + ")", nil
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
				return false, "", fmt.Errorf("%s is a %s: it cannot wait on an item, put it off with a date", doc.ID, doc.Meta.Header().Type)
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
var Defer = withProposalReceipts(define("defer", "Put questions, tasks, goals, obligations, or io items off until a date, or someday with no date; they leave the agenda, next, and the order (state later) but still block what depends on them, and return on their date. Later only sets when an item surfaces: a view scoped by tag still lists it (state later), and an agent that builds a list for a topic may include it and say so, but must not undefer it. tag puts off a topic: every tagged open item. With scope, put off every open question in a goal or area that blocks nothing. The output lists every id changed", func(ctx context.Context, d Deps, in DeferInput) (DeferOutput, error) {
	until := strings.TrimSpace(in.Until)
	today := d.Store.Now().Format(dateLayout)
	after, when := normID(in.After), strings.Join(strings.Fields(in.When), " ")
	if after != "" && when != "" {
		return DeferOutput{}, errors.New("give after or when, not both")
	}
	if (after != "" || when != "") && (strings.TrimSpace(in.Scope) != "" || len(in.Items) > 0 || strings.TrimSpace(in.Tag) != "") {
		return DeferOutput{}, errors.New("after and when apply to ids, not to a scope, items, or a tag")
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
	ids, items, skipped, err := deferTargets(ctx, d, in.IDs, in.Items, in.Tag, false)
	if err != nil {
		return DeferOutput{}, err
	}
	if after != "" || when != "" {
		if len(items) > 0 {
			return DeferOutput{}, errors.New("after and when apply to vault items: a dossier item returns on a date")
		}
	}
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
	if len(ids) == 0 && len(items) == 0 {
		if len(skipped) > 0 {
			return DeferOutput{Changed: []string{}, Skipped: skipped, Until: until}, nil
		}
		return DeferOutput{}, errors.New("nothing to put off: give ids, items, a tag, or a scope with open questions that block nothing")
	}
	out := DeferOutput{Changed: []string{}, Skipped: skipped, Until: until}
	if when != "" {
		if err := lintText("when", when); err != nil {
			return DeferOutput{}, err
		}
		var err error
		var made bool
		if after, made, out.Similar, err = whenTask(ctx, d, ids, when, normID(in.Place), in.New); err != nil || len(out.Similar) > 0 {
			return out, err
		}
		if made {
			out.Created = after
		}
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
	if err := deferItems(ctx, d, items, until, reason, &out); err != nil {
		return out, err
	}
	return out, nil
}))

// UndeferInput is the input of undefer.
type UndeferInput struct {
	IDs   []string  `json:"ids,omitempty"`   // a dossier's id brings back every item of it put off
	Items []ItemRef `json:"items,omitempty"` // dossier items
	Tag   string    `json:"tag,omitempty"`   // a topic: every tagged item put off
}

// Undefer brings put-off items back.
var Undefer = withProposalReceipts(definePerson("undefer", "Bring questions, tasks, goals, io items, or dossier items that were put off back now: ids (a dossier's id: its items), items, or a tag", func(ctx context.Context, d Deps, in UndeferInput) (DeferOutput, error) {
	if len(in.IDs) == 0 && len(in.Items) == 0 && strings.TrimSpace(in.Tag) == "" {
		return DeferOutput{}, errors.New("nothing to bring back: give ids, items, or a tag")
	}
	ids, items, skipped, err := deferTargets(ctx, d, in.IDs, in.Items, in.Tag, true)
	if err != nil {
		return DeferOutput{}, err
	}
	out := DeferOutput{Changed: []string{}, Skipped: skipped}
	for _, id := range ids {
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
	if err := deferItems(ctx, d, items, "", "", &out); err != nil {
		return out, err
	}
	return out, nil
}))

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

// whenTask makes the task a "when" condition waits on, in the goal or area of
// the first id (or place). A similar open task is returned instead, with
// nothing made, unless force is set.
func whenTask(ctx context.Context, d Deps, ids []string, when, place string, force bool) (string, bool, []Similar, error) {
	if len(ids) == 0 {
		return "", false, nil, errors.New("nothing to put off: give ids")
	}
	self := map[string]bool{}
	for _, id := range ids {
		self[normID(id)] = true
	}
	// The same condition has one task: an open one is reused, and lists the
	// new waiters. Force (new) always makes one.
	if !force {
		doc, ok, err := openCondition(ctx, d, when, self)
		if err != nil {
			return "", false, nil, err
		}
		if ok {
			return doc.ID, false, nil, addWaiters(ctx, d, doc, ids)
		}
		sim, err := FindSimilar(ctx, d.Store, model.TypeTask, when, "")
		if err != nil {
			return "", false, nil, err
		}
		open := sim[:0]
		for _, x := range sim {
			if self[x.ID] {
				continue // the item put off is not a match for its own condition
			}
			if doc, err := d.Store.Get(ctx, x.ID); err == nil && !resolved(doc) {
				open = append(open, x)
			}
		}
		if len(open) > 0 {
			return "", false, open, nil
		}
	}
	in := TaskAddInput{Title: when, DoneWhen: when, Body: conditionBody(when, ids)}
	if place != "" {
		pd, err := d.Store.Get(ctx, place)
		if err != nil {
			return "", false, nil, fmt.Errorf("place %s: %w", place, err)
		}
		switch pd.Meta.(type) {
		case *model.Project:
			in.Project = pd.ID
		case *model.Area:
			in.Area = pd.ID
		default:
			return "", false, nil, fmt.Errorf("place %s is a %s: give a goal or an area", pd.ID, pd.Meta.Header().Type)
		}
	} else {
		first, err := d.Store.Get(ctx, normID(ids[0]))
		if err != nil {
			return "", false, nil, err
		}
		switch m := first.Meta.(type) {
		case *model.Task:
			in.Project, in.Area = m.Project.Target(), m.Area.Target()
		case *model.Question:
			if sc := scopeID(m); sc != ScopeRootWord {
				sd, err := d.Store.Get(ctx, sc)
				if err != nil {
					return "", false, nil, err
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
		return "", false, nil, fmt.Errorf("%s has no goal or area: give place, a goal or an area for the task", normID(ids[0]))
	}
	out, err := TaskAdd.Run(ctx, d, in)
	if err != nil {
		return "", false, nil, fmt.Errorf("when: %w", err)
	}
	return out.ID, true, nil, nil
}

// waitersHeading is the section of a condition task that lists what waits on it.
const waitersHeading = "Waiting on it"

// conditionKey is condition text as defer compares it: lower case, single
// spaces, no end punctuation.
func conditionKey(s string) string {
	return strings.TrimRight(strings.Join(strings.Fields(strings.ToLower(s)), " "), ".!?;:,")
}

// openCondition finds the open task a "when" condition made for the same
// text: its title and done_when are the condition. Ids in skip are not one.
func openCondition(ctx context.Context, d Deps, when string, skip map[string]bool) (model.Document, bool, error) {
	key := conditionKey(when)
	refs, err := d.Store.List(ctx, store.Filter{Type: model.TypeTask})
	if err != nil {
		return model.Document{}, false, err
	}
	for _, r := range refs {
		if skip[r.ID] {
			continue
		}
		doc, err := d.Store.Get(ctx, r.ID)
		if err != nil {
			return model.Document{}, false, err
		}
		t, ok := doc.Meta.(*model.Task)
		if !ok || resolved(doc) || conditionKey(doc.Title) != key || conditionKey(t.DoneWhen) != key {
			continue
		}
		return doc, true, nil
	}
	return model.Document{}, false, nil
}

// conditionBody is the body of a condition task: the condition and the items
// that wait on it.
func conditionBody(when string, ids []string) string {
	body := "Condition: " + when + "\n\nThe items below are put off until this task is done; then they come back."
	for i, id := range ids {
		line := "- [[" + normID(id) + "]]"
		if i == 0 {
			body = appendSection(body, waitersHeading, line)
			continue
		}
		body += "\n" + line
	}
	return body
}

// addWaiters lists ids on a reused condition task, each once.
func addWaiters(ctx context.Context, d Deps, doc model.Document, ids []string) error {
	before := doc.Body
	for _, id := range ids {
		id = normID(id)
		if id == "" || strings.Contains(doc.Body, "[["+id+"]]") {
			continue
		}
		line := "- [[" + id + "]]"
		if strings.Contains(doc.Body, "\n## "+waitersHeading+"\n") || strings.HasPrefix(doc.Body, "## "+waitersHeading+"\n") {
			doc.Body += "\n" + line
		} else {
			doc.Body = appendSection(doc.Body, waitersHeading, line)
		}
	}
	if doc.Body == before {
		return nil
	}
	_, err := d.Store.Update(ctx, doc)
	return err
}

// deferrable lists the vault types defer puts off by id.
var deferrable = []model.Type{model.TypeTask, model.TypeQuestion, model.TypeProject, model.TypeIO, model.TypeObligation}

// deferTargets splits what defer or undefer names into vault ids and dossier
// items: a dossier's id becomes its items, and a tag becomes every tagged
// open vault item and every tagged dossier item. For undefer (clear) only
// items now put off are taken. Skipped says why a named item was left.
func deferTargets(ctx context.Context, d Deps, ids []string, refs []ItemRef, tag string, clear bool) ([]string, []ItemRef, []string, error) {
	var vault, skipped []string
	items := slices.Clone(refs)
	today := d.Store.Now().Format(dateLayout)
	take := func(p *proposal, it model.ProposalItem, named bool) {
		switch {
		case clear && it.DeferUntil == "":
			if named {
				skipped = append(skipped, ItemRef{Proposal: p.doc.ID, Item: it.ID}.String()+": not put off")
			}
		case !clear && !answerable(it.State):
			if named {
				skipped = append(skipped, ItemRef{Proposal: p.doc.ID, Item: it.ID}.String()+": already "+string(it.State)+", only an item that waits for the owner is put off")
			}
		default:
			items = append(items, ItemRef{Proposal: p.doc.ID, Item: it.ID})
		}
	}
	for _, id := range ids {
		id = normID(id)
		if id == "" {
			continue
		}
		doc, err := d.Store.Get(ctx, id)
		if err != nil {
			return nil, nil, nil, linkErr("id", id, err)
		}
		if _, ok := doc.Meta.(*model.Proposal); !ok {
			vault = append(vault, id)
			continue
		}
		p, err := readProposal(ctx, d, id)
		if err != nil {
			return nil, nil, nil, err
		}
		if p.dossier == nil || p.broken != nil {
			return nil, nil, nil, fmt.Errorf("%s is not a readable dossier: only dossier items are put off", id)
		}
		// Defer says why each decided item stays; undefer names only what it brings back.
		for _, it := range p.content.Items {
			if it.State != model.ItemWithdrawn {
				take(p, it, !clear)
			}
		}
	}
	if tag = normTag(tag); tag != "" {
		refs, err := d.Store.List(ctx, store.Filter{})
		if err != nil {
			return nil, nil, nil, err
		}
		for _, r := range refs {
			if r.Type == model.TypeProposal || r.Type == model.TypeReceipt {
				continue
			}
			doc, err := d.Store.Get(ctx, r.ID)
			if err != nil || !slices.Contains(normTags(doc.Meta.Header().Tags), tag) {
				continue
			}
			df := deferralOf(doc.Meta)
			switch {
			case !slices.Contains(deferrable, r.Type) || df == nil:
				skipped = append(skipped, doc.ID+": a "+string(r.Type)+" is not put off")
			case !openDoc(doc):
			case clear && df.DeferUntil == "" && len(df.DeferredAfter) == 0:
			case !clear && df.IsDeferred(today):
			default:
				vault = append(vault, doc.ID)
			}
		}
		all, err := allProposals(ctx, d)
		if err != nil {
			return nil, nil, nil, err
		}
		for _, p := range all {
			if p.meta.Status == model.ProposalSuperseded || p.broken != nil || p.dossier == nil {
				continue
			}
			for _, it := range p.content.Items {
				if slices.Contains(normTags(model.ItemTags(p.meta.Tags, it)), tag) && (clear || !it.Deferred(today)) {
					take(p, it, false)
				}
			}
		}
	}
	return vault, items, skipped, nil
}

// deferItems puts dossier items off until until, or brings them back when
// until is empty. Each dossier is one write; an item that does not wait for
// the owner is skipped with the reason.
func deferItems(ctx context.Context, d Deps, refs []ItemRef, until, reason string, out *DeferOutput) error {
	byProp := map[string][]string{}
	var props []string
	seen := map[string]bool{}
	for _, r := range refs {
		p := normID(r.Proposal)
		if p == "" || r.Item == "" {
			return errors.New("an item needs proposal and item")
		}
		key := ItemRef{Proposal: p, Item: r.Item}.String()
		if seen[key] {
			continue
		}
		seen[key] = true
		if _, ok := byProp[p]; !ok {
			props = append(props, p)
		}
		byProp[p] = append(byProp[p], r.Item)
	}
	for _, id := range props {
		var changed, skipped []string
		p, err := changeProposal(ctx, d, id, func(p *proposal) error {
			changed, skipped = nil, nil
			if p.dossier == nil {
				return fmt.Errorf("%s is not a dossier: only dossier items are put off", p.doc.ID)
			}
			for _, item := range byProp[id] {
				it, err := p.item(item)
				if err != nil {
					return err
				}
				ref := ItemRef{Proposal: p.doc.ID, Item: item}.String()
				switch {
				case until == "" && it.DeferUntil == "":
					skipped = append(skipped, ref+": not put off")
					continue
				case until != "" && !answerable(it.State):
					skipped = append(skipped, ref+": already "+string(it.State)+", only an item that waits for the owner is put off")
					continue
				}
				it.DeferUntil, it.DeferredReason = until, reason
				changed = append(changed, ref)
			}
			if len(changed) == 0 {
				return errNoChange
			}
			return nil
		})
		if err != nil && !errors.Is(err, errNoChange) {
			return err
		}
		if p != nil {
			for _, ref := range changed {
				_, item, _ := strings.Cut(ref, "#")
				op := "proposal_defer"
				if until == "" {
					op = "proposal_undefer"
				}
				if err := d.Store.Record(ctx, op, p.doc.ID, item, "", ""); err != nil {
					return err
				}
			}
		}
		out.Changed = append(out.Changed, changed...)
		out.Skipped = append(out.Skipped, skipped...)
	}
	return nil
}

// answerable reports whether a dossier item still waits for the owner.
func answerable(s model.ItemState) bool {
	return s == model.ItemPending || s == model.ItemHeld || s == model.ItemSentBack || s == model.ItemSkipped
}
