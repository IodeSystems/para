package ops

import (
	"slices"
	"strings"

	"github.com/iodesystems/para/internal/model"
)

// Why the owner does an item. An open work item has one or more reasons:
//
//   - goal: it leads to a goal (type project): its project, or through the
//     task or obligation it is an action item of (parent; an ask's parent
//     too), the item it unblocks (an open
//     item that depends on it, or an Ask's blocks), or a question's scope;
//   - owed: it is owed to someone: a task's requested_by, a commitment's
//     with, an obligation's owed_to (so prep for an obligation, whose parent
//     it is, is owed through it), an incoming Ask's targets, or an incoming
//     io item that expects a response and was filed into it;
//   - upkeep: it keeps something up: an area, a recurring task or
//     obligation, or an incident.
//
// With none, the item has no reason recorded. This one walker is what the
// item context, health (no-path), and the tick's reason check read.

// Reason kinds.
const (
	ReasonGoal   = "goal"
	ReasonOwed   = "owed"
	ReasonUpkeep = "upkeep"
	ReasonNone   = "none"
)

// How one item leads to the next on the way to a reason.
const (
	StepActionItemOf = "action_item_of" // the item is an action item of the next (parent)
	StepUnblocks     = "unblocks"       // the next item waits on this one
	StepAbout        = "about"          // a question about the next item (scope)
	StepFollowsUp    = "follows_up"     // a task that follows up an incident
)

// stepWords says a step for a person.
var stepWords = map[string]string{
	StepActionItemOf: "action item of", StepUnblocks: "unblocks", StepAbout: "about", StepFollowsUp: "follows up",
}

// NoReasonTask is the health message of an open task with no reason.
const NoReasonTask = "open task has no reason: no goal, no one it is owed to, no area"

// NoReasonText is what the owner reads for an item with no reason.
const NoReasonText = "No reason recorded: link a goal, say who it is for, put it under an area, or drop it"

// ReasonStep is one item on the way from an item to its reason.
type ReasonStep struct {
	How   string `json:"how"` // action_item_of, unblocks, about, or follows_up
	ID    string `json:"id"`
	Title string `json:"title,omitempty"`
}

// Reason is one reason an item is worth doing. Ref is the goal, area,
// person, or group; empty for free text, a recurring item, or none. Via are
// the items between the item and the reason, in order. Due and Status are a
// goal's. Text says it in words.
type Reason struct {
	Kind   string       `json:"kind"`
	Ref    string       `json:"ref,omitempty"`
	Title  string       `json:"title,omitempty"`
	Via    []ReasonStep `json:"via,omitempty"`
	Due    string       `json:"due,omitempty"`
	Status string       `json:"status,omitempty"`
	Text   string       `json:"text"`
}

// reasonGraph is the vault as the reason walker reads it.
type reasonGraph struct {
	docs map[string]model.Document
	// dependents maps an id to the open items that wait on it.
	dependents map[string][]string
	// cameFrom maps an id to the io items filed into it or linking it.
	cameFrom map[string][]string
}

// newReasonGraph indexes docs. A document may be raw or in the shape health
// reads (an obligation as a task, an incident task as an incident).
func newReasonGraph(docs map[string]model.Document) *reasonGraph {
	g := &reasonGraph{docs: docs, dependents: map[string][]string{}, cameFrom: map[string][]string{}}
	wait := func(id string, deps []model.Link) {
		for _, dep := range targets(deps) {
			g.dependents[dep] = append(g.dependents[dep], id)
		}
	}
	for id, d := range docs {
		switch m := d.Meta.(type) {
		case *model.Task:
			if !taskTerminal(m.Status) {
				wait(id, model.HardDeps(m))
			}
		case *model.Obligation:
			if m.Open() {
				wait(id, model.HardDeps(m))
			}
		case *model.Question:
			if m.Status == model.QuestionOpen {
				wait(id, model.HardDeps(m))
			}
		case *model.RFX:
			if m.Status == model.RFXOpen {
				wait(id, model.HardDeps(m))
				for _, b := range m.Blocks {
					g.dependents[id] = append(g.dependents[id], b.Target())
				}
			}
		case *model.IO:
			var to []string
			if b := m.Became.Target(); b != "" {
				to = append(to, b)
			}
			for _, l := range m.Links {
				if t := l.Target(); t != "" && !slices.Contains(to, t) {
					to = append(to, t)
				}
			}
			for _, t := range to {
				g.cameFrom[t] = append(g.cameFrom[t], id)
			}
		}
	}
	for _, m := range []map[string][]string{g.dependents, g.cameFrom} {
		for k := range m {
			slices.Sort(m[k])
		}
	}
	return g
}

func (g *reasonGraph) title(id string) string {
	if d, ok := g.docs[id]; ok && d.Title != "" {
		return d.Title
	}
	return id
}

// reasons lists every reason of an item, nearest first; nil for none.
func (g *reasonGraph) reasons(id string) []Reason {
	type hop struct {
		id  string
		via []ReasonStep
	}
	seen := map[string]bool{id: true}
	queue := []hop{{id: id}}
	var out []Reason
	have := map[string]bool{}
	for len(queue) > 0 {
		h := queue[0]
		queue = queue[1:]
		d, ok := g.docs[h.id]
		if !ok {
			continue
		}
		add := func(r Reason) {
			key := r.Kind + "|" + r.Ref + "|" + r.Title
			if have[key] {
				return
			}
			have[key] = true
			r.Via = h.via
			out = append(out, r)
		}
		// goal adds the goal (or area) a project link names.
		goal := func(l model.Link) {
			t := l.Target()
			gd, ok := g.docs[t]
			if t == "" || !ok {
				return
			}
			switch p := gd.Meta.(type) {
			case *model.Project:
				add(Reason{Kind: ReasonGoal, Ref: t, Title: g.title(t), Due: string(p.Due), Status: string(p.Status)})
			case *model.Area:
				add(Reason{Kind: ReasonUpkeep, Ref: t, Title: g.title(t)})
			}
		}
		owed := func(who string) {
			if who = strings.TrimSpace(who); who == "" {
				return
			}
			if t := model.Link(who).Target(); t != who || strings.HasPrefix(who, "[[") {
				add(Reason{Kind: ReasonOwed, Ref: t, Title: g.title(t)})
				return
			}
			add(Reason{Kind: ReasonOwed, Title: who})
		}
		recurring := func(recur string) {
			add(Reason{Kind: ReasonUpkeep, Title: strings.TrimSpace("recurring " + recur)})
		}
		next := func(to, how string) {
			if to == "" || seen[to] {
				return
			}
			if _, ok := g.docs[to]; !ok {
				return
			}
			seen[to] = true
			queue = append(queue, hop{id: to, via: append(slices.Clone(h.via), ReasonStep{How: how, ID: to, Title: g.title(to)})})
		}
		switch m := d.Meta.(type) {
		case *model.Project:
			add(Reason{Kind: ReasonGoal, Ref: h.id, Title: g.title(h.id), Due: string(m.Due), Status: string(m.Status)})
			continue
		case *model.Area:
			add(Reason{Kind: ReasonUpkeep, Ref: h.id, Title: g.title(h.id)})
			continue
		case *model.Task:
			goal(m.Project)
			goal(m.Area)
			owed(m.RequestedBy)
			if m.EffectiveKind() == model.KindCommitment {
				for _, w := range m.With {
					owed(string(w))
				}
			}
			if m.Recur != "" {
				recurring(m.Recur)
			}
			if m.Kind == model.KindIncident {
				add(Reason{Kind: ReasonUpkeep, Title: "incident"})
			}
			g.owedByIO(h.id, owed)
			next(m.Parent.Target(), StepActionItemOf)
			next(m.Incident.Target(), StepFollowsUp)
		case *model.Obligation:
			goal(m.Project)
			goal(m.Area)
			for _, w := range m.OwedTo {
				owed(string(w))
			}
			if m.Recur != "" {
				recurring(m.Recur)
			} else if m.OccurrenceOf != "" {
				recurring("")
			}
			g.owedByIO(h.id, owed)
			next(m.Parent.Target(), StepActionItemOf)
		case *model.Incident:
			goal(m.Area)
			add(Reason{Kind: ReasonUpkeep, Title: "incident"})
		case *model.RFX:
			goal(m.Project)
			if m.Direction == model.RFXIn {
				for _, t := range m.Targets {
					owed(string(t.To))
				}
			}
			g.owedByIO(h.id, owed)
			next(m.Parent.Target(), StepActionItemOf)
		case *model.Question:
			if !m.IsRoot() {
				next(m.Scope.Target(), StepAbout)
			}
		}
		for _, u := range g.dependents[h.id] {
			next(u, StepUnblocks)
		}
	}
	for i := range out {
		out[i].Text = reasonText(out[i], id)
	}
	return out
}

// reasonRank ranks an item's best reason for a tie in the value order:
// owed 0, goal 1, upkeep 2, none 3.
func (g *reasonGraph) reasonRank(id string) int {
	best := 3
	for _, r := range g.reasons(id) {
		switch r.Kind {
		case ReasonOwed:
			return 0
		case ReasonGoal:
			best = min(best, 1)
		case ReasonUpkeep:
			best = min(best, 2)
		}
	}
	return best
}

// owedByIO names the sender of each incoming io item filed into id that
// expects a response.
func (g *reasonGraph) owedByIO(id string, owed func(string)) {
	for _, ioID := range g.cameFrom[id] {
		if m, ok := g.docs[ioID].Meta.(*model.IO); ok && m.Direction == model.IOIn && m.Expects == model.ExpectsResponse {
			owed(m.From)
		}
	}
}

// reasonsOrNone is reasons, or one reason of kind none.
func (g *reasonGraph) reasonsOrNone(id string) []Reason {
	if rs := g.reasons(id); len(rs) > 0 {
		return rs
	}
	return []Reason{{Kind: ReasonNone, Text: NoReasonText}}
}

// reasonText says a reason in words: "goal X (directly)", "goal Y, through:
// unblocks T → goal Y", "owed to Dana", "keeps up area Z".
func reasonText(r Reason, self string) string {
	var head string
	switch r.Kind {
	case ReasonGoal:
		if r.Ref == self {
			head = "this is the goal " + r.Title
		} else {
			head = "goal " + r.Title
		}
		if r.Due != "" {
			head += ", due " + r.Due
		}
		if r.Status != "" && r.Status != string(model.ProjectActive) {
			head += ", " + r.Status
		}
	case ReasonOwed:
		head = "owed to " + r.Title
	case ReasonUpkeep:
		switch {
		case r.Ref == self:
			head = "this is the area " + r.Title
		case r.Ref != "":
			head = "keeps up area " + r.Title
		default:
			head = r.Title
		}
	case ReasonNone:
		return NoReasonText
	}
	if r.Ref == self {
		return head
	}
	if len(r.Via) == 0 {
		return head + " (directly)"
	}
	parts := make([]string, 0, len(r.Via)+1)
	for _, s := range r.Via {
		parts = append(parts, stepWords[s.How]+" "+firstOf(s.Title, s.ID))
	}
	end := r.Kind + " " + r.Title
	if r.Kind == ReasonUpkeep && r.Ref != "" {
		end = "area " + r.Title
	} else if r.Kind == ReasonUpkeep {
		end = r.Title
	} else if r.Kind == ReasonOwed {
		end = r.Title
	}
	return head + ", through: " + strings.Join(parts, " → ") + " → " + end
}
