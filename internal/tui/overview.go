package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/ops"
	"github.com/iodesystems/parasolve/internal/store"
)

// overview is what a detail page adds to the document's own text, so the page
// holds at least what its list row held. Glance goes under the title, extra
// after the text. Both are para-md, with links by title.
type overview struct{ glance, extra string }

// wl is a wikilink that shows a title.
func wl(id, title string) string {
	clean := strings.NewReplacer("|", "/", "[", "(", "]", ")").Replace(strings.TrimSpace(title))
	if clean == "" {
		clean = "(untitled)"
	}
	return "[[" + id + "|" + clean + "]]"
}

// wlOrName links an id by the name found for it; with no name there is no link
// and no id: a page shows a name or nothing.
func wlOrName(id, name string) string {
	if id == "" || name == "" {
		return ""
	}
	return wl(id, name)
}

func section(b *strings.Builder, title string, lines []string) {
	if len(lines) == 0 {
		return
	}
	fmt.Fprintf(b, "\n## %s (%d)\n\n", title, len(lines))
	for _, l := range lines {
		b.WriteString("- " + l + "\n")
	}
}

func glanceLine(parts ...string) string {
	var out []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " · ")
}

func pointerLines(ps []model.Pointer) []string {
	var out []string
	for _, k := range ps {
		target := k.Target
		if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
			target = "<" + target + ">"
		}
		out = append(out, fmt.Sprintf("%s: %s  %s", k.Kind, k.Label, target))
	}
	return out
}

// taskLine is one task of a goal or area. states maps a task id to its state in
// the work graph: a task that waits on an open question or task is "waiting",
// never "ready". A task the graph does not list shows its own status.
func taskLine(t ops.TaskView, states map[string]string) string {
	word := stateWord(string(t.Status))
	if st := states[t.ID]; st != "" && t.Status == model.TaskTodo {
		word = st
	}
	s := wl(t.ID, t.Title) + " · " + word
	if t.Due != "" {
		s += " · due " + dateWords(string(t.Due), time.Time{})
	}
	if t.AssigneeName != "" {
		s += " · with " + t.AssigneeName
	}
	return s
}

func questionLine(q ops.QuestionView) string {
	s := wl(q.ID, q.Title) + " · " + string(q.Kind)
	if q.Blocking > 0 {
		s += fmt.Sprintf(" · blocks %d", q.Blocking)
	}
	return s
}

// buildOverview reads what a document of a kind that has related things shows.
// It returns nothing for the rest. A failed read leaves the page as it was.
func buildOverview(ctx context.Context, e Env, d model.Document) overview {
	var names map[string]string
	name := func(id string) string {
		if names == nil {
			names = map[string]string{}
			if refs, err := e.Deps.Store.List(ctx, store.Filter{}); err == nil {
				for _, r := range refs {
					names[r.ID] = r.Title
				}
			}
		}
		return names[model.Link(id).Target()]
	}
	var b strings.Builder
	var ov overview
	switch m := d.Meta.(type) {
	case *model.Area:
		o, err := ops.AreaShow.Run(ctx, e.Deps, ops.AreaShowInput{ID: d.ID})
		if err != nil {
			return ov
		}
		ov.glance = glanceLine("Area", stateWord(string(m.Status)))
		section(&b, "Links", pointerLines(o.Area.Links))
		states := graphStates(ctx, e, ops.GraphInput{Area: d.ID})
		count := map[string]int{}
		var loose []string
		for _, t := range o.Tasks {
			if t.Project != "" {
				count[t.Project]++
			} else {
				loose = append(loose, taskLine(t, states))
			}
		}
		var ps []string
		for _, p := range o.Projects {
			n := "no tasks"
			if count[p.ID] > 0 {
				n = plural(count[p.ID], "task")
			}
			ps = append(ps, wl(p.ID, p.Title)+" · "+stateWord(string(p.Status))+" · "+n)
		}
		section(&b, "Goals", ps)
		section(&b, "Tasks in the area itself", loose)
		var inc []string
		for _, i := range o.Incidents {
			inc = append(inc, wl(i.ID, i.Title)+" · "+string(i.Severity)+" "+stateWord(string(i.Status)))
		}
		section(&b, "Incidents", inc)
		var qs []string
		for _, q := range o.Questions {
			qs = append(qs, questionLine(q))
		}
		section(&b, "Open questions", qs)
	case *model.Project:
		o, err := ops.ProjectShow.Run(ctx, e.Deps, ops.ProjectShowInput{ID: d.ID})
		if err != nil {
			return ov
		}
		ov.glance = glanceLine("Goal", stateWord(string(m.Status)), prefixed("Area ", wlOrName(m.Area.Target(), name(m.Area.Target()))))
		if m.DoneWhen != "" {
			section(&b, "Done when", []string{m.DoneWhen})
		} else {
			section(&b, "Done when", []string{stDim.Render("not set: para project edit " + d.ID + " --done-when <how we know>")})
		}
		section(&b, "Brief", briefLines(o.Project.Brief))
		section(&b, "Links", pointerLines(o.Project.Links))
		states := graphStates(ctx, e, ops.GraphInput{Project: d.ID})
		var ts []string
		for _, t := range o.Tasks {
			ts = append(ts, taskLine(t, states))
		}
		section(&b, "Tasks", ts)
		var qs []string
		for _, q := range o.Questions {
			qs = append(qs, questionLine(q))
		}
		section(&b, "Open questions", qs)
	case *model.Task:
		var deps []string
		for _, l := range m.DependsOn {
			if w := wlOrName(l.Target(), name(l.Target())); w != "" {
				deps = append(deps, w)
			}
		}
		assignee := ""
		if m.Assignee != "" && m.Assignee != "self" {
			assignee = "with " + firstNonEmpty(name(m.Assignee), strings.TrimPrefix(m.Assignee, "agent:"))
		}
		proj := m.Project.Target()
		if proj == "" {
			proj = m.Area.Target()
		}
		ov.glance = glanceLine("Task", stateWord(string(m.Status)), prefixed("due ", dateWords(string(m.Due), time.Time{})), prefixed("in ", wlOrName(proj, name(proj))), assignee,
			prefixed("needs ", strings.Join(deps, ", ")))
		if m.DoneWhen != "" {
			section(&b, "Done when", []string{m.DoneWhen})
		}
		if len(m.BlockedBy) > 0 {
			section(&b, "Blocked by", m.BlockedBy)
		}
		if p := m.Parent.Target(); p != "" {
			if w := wlOrName(p, name(p)); w != "" {
				ov.glance = glanceLine(ov.glance, "item of "+w)
			}
		}
		if o, err := ops.TaskShow.Run(ctx, e.Deps, ops.TaskShowInput{ID: d.ID}); err == nil {
			ref := func(r ops.PartRef) string {
				return glanceLine(wl(r.ID, r.Title), stateWord(r.Status), prefixed("due ", dateWords(r.Due, time.Time{})))
			}
			var items, commits, answered, policy, follow []string
			for _, r := range o.Parts.ActionItems {
				items = append(items, ref(r))
			}
			for _, r := range o.Parts.Commitments {
				commits = append(commits, ref(r))
			}
			for _, q := range o.Parts.Answered {
				answered = append(answered, glanceLine(wl(q.ID, q.Title), q.Choice))
			}
			for _, r := range o.Parts.Policy {
				policy = append(policy, glanceLine(wl(r.ID, r.Title), r.Kind))
			}
			for _, r := range o.Parts.FollowUps {
				follow = append(follow, ref(r))
			}
			section(&b, "Action items", items)
			section(&b, "Open questions", func() []string {
				var l []string
				for _, q := range o.Questions {
					l = append(l, questionLine(q))
				}
				return l
			}())
			section(&b, "Answered", answered)
			section(&b, "Commitments", commits)
			section(&b, "Policy", policy)
			section(&b, "Follow-ups", follow)
		}
	case *model.Question:
		scope := m.Scope.Target()
		ov.glance = glanceLine("Question", string(m.AsKind()), stateWord(string(m.Status)), prefixed("about ", wlOrName(scope, name(scope))))
		var opts []string
		for _, o := range m.Options {
			if o == m.Recommended {
				o += " (recommended)"
			}
			opts = append(opts, o)
		}
		section(&b, "Options", opts)
		var waits []string
		for _, l := range m.DependsOn {
			if w := wlOrName(l.Target(), name(l.Target())); w != "" {
				waits = append(waits, w)
			}
		}
		section(&b, "Waits on", waits)
		if m.HasProposal() {
			lines := []string{strings.TrimSpace(m.ProposedChoice+" "+m.ProposedAnswer) + " (from " + m.ProposedBy + ", not yet accepted)"}
			for _, s := range m.ProposedSources {
				ref := s.Ref
				if !strings.HasPrefix(ref, "http") {
					ref = wlOrName(ref, name(ref))
				}
				lines = append(lines, "source "+ref+" "+s.Quote)
			}
			section(&b, "Answer to check", lines)
		}
		if m.RejectedReason != "" {
			section(&b, "Last answer rejected", []string{m.RejectedBy + ": " + m.RejectedReason})
		}
	case *model.RFX:
		o, err := ops.RFXShow.Run(ctx, e.Deps, ops.RFXShowInput{ID: d.ID})
		if err != nil {
			return ov
		}
		dir := "You asked"
		if m.Direction == model.RFXIn {
			dir = "Asks you"
		}
		ov.glance = glanceLine(dir, stateWord(string(m.Status)), prefixed("route ", name(m.Route)), prefixed("follow up ", dateWords(string(m.NextNudge), time.Time{})))
		var ts []string
		for _, t := range m.Targets {
			who := firstNonEmpty(wlOrName(t.To.Target(), name(t.To.Target())), "someone")
			s := who + " · " + string(t.Medium) + " · " + stateWord(string(t.State))
			if t.Sent != "" {
				s += " " + dateWords(string(t.Sent), time.Time{})
			}
			ts = append(ts, s)
		}
		section(&b, "Sent to", ts)
		if strings.TrimSpace(o.Draft) != "" && !strings.Contains(d.Body, strings.TrimSpace(o.Draft)) {
			b.WriteString("\n## Draft\n\n")
			for _, l := range strings.Split(strings.TrimSpace(o.Draft), "\n") {
				b.WriteString("> " + l + "\n>\n")
			}
		}
		var chain []string
		for _, c := range o.Chain {
			chain = append(chain, wl(c.ID, c.Title)+" · "+stateWord(c.Status))
		}
		section(&b, "Waits on", chain)
		var by []string
		for _, id := range o.DependedOnBy {
			if w := wlOrName(id, name(id)); w != "" {
				by = append(by, w)
			}
		}
		section(&b, "Holds up", by)
	}
	ov.extra = b.String()
	return ov
}

func prefixed(p, v string) string {
	if strings.TrimSpace(v) == "" {
		return ""
	}
	return p + v
}

// withOverview puts the glance under the title and the extra after the text.
func withOverview(src, title string, ov overview) string {
	if ov.glance != "" {
		if i := strings.Index(src, "\n# "+title+"\n"); i >= 0 {
			at := i + len("\n# "+title+"\n")
			src = src[:at] + "\n" + ov.glance + "\n" + src[at:]
		}
	}
	if ov.extra != "" {
		src = strings.TrimRight(src, "\n") + "\n" + ov.extra
	}
	return src
}

// briefLines is the brief of a goal in a line per part; none when it has none.
func briefLines(b *model.GoalBrief) []string {
	if b == nil {
		return nil
	}
	var out []string
	if b.Validation != "" {
		out = append(out, "State: "+stateWord(string(b.Validation)))
	}
	if b.Rank > 0 {
		out = append(out, fmt.Sprintf("Rank: %d", b.Rank))
	}
	if b.Why != nil {
		out = append(out, "Why: "+b.Why.Text)
	}
	if c := b.Confidence; c != nil {
		out = append(out, strings.TrimSpace(fmt.Sprintf("Confidence: %d%% %s", c.Percent, c.Basis)))
	}
	if len(b.Alternatives) > 0 {
		var names []string
		for _, a := range b.Alternatives {
			names = append(names, a.Name)
		}
		out = append(out, "Alternatives: "+strings.Join(names, ", "))
	}
	for _, u := range b.Unknowns {
		out = append(out, "Unknown: "+u.Text)
	}
	for _, s := range b.Signals {
		read := "not read yet"
		if r := s.Latest(); r != nil {
			read = fmt.Sprintf("read %s on %s", r.Value, r.Date)
		}
		out = append(out, fmt.Sprintf("Signal: %s, expected %s, %s", s.What, firstNonEmpty(s.Expected, "not stated"), read))
	}
	if pv := b.Pivot; pv != nil {
		by := ""
		if pv.Date != "" {
			by = " by " + string(pv.Date)
		}
		out = append(out, fmt.Sprintf("Pivot: %s%s if %s", pv.Action, by, pv.Condition))
	}
	return out
}

// graphStates maps each task id in the work graph to its state.
func graphStates(ctx context.Context, e Env, in ops.GraphInput) map[string]string {
	out := map[string]string{}
	if g, err := ops.Graph.Run(ctx, e.Deps, in); err == nil {
		for _, n := range g.Nodes {
			out[n.ID] = n.State
		}
	}
	return out
}
