package tui

import (
	"context"
	"fmt"
	"strings"

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

func taskLine(t ops.TaskView) string {
	s := wl(t.ID, t.Title) + " · " + string(t.Status)
	if t.Due != "" {
		s += " · due " + string(t.Due)
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
		ov.glance = glanceLine("Area", string(m.Status))
		section(&b, "Links", pointerLines(o.Area.Links))
		count := map[string]int{}
		var loose []string
		for _, t := range o.Tasks {
			if t.Project != "" {
				count[t.Project]++
			} else {
				loose = append(loose, taskLine(t))
			}
		}
		var ps []string
		for _, p := range o.Projects {
			n := "no tasks"
			if count[p.ID] > 0 {
				n = plural(count[p.ID], "task")
			}
			ps = append(ps, wl(p.ID, p.Title)+" · "+string(p.Status)+" · "+n)
		}
		section(&b, "Projects", ps)
		section(&b, "Tasks in the area itself", loose)
		var inc []string
		for _, i := range o.Incidents {
			inc = append(inc, wl(i.ID, i.Title)+" · "+string(i.Severity)+" "+string(i.Status))
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
		ov.glance = glanceLine("Project", string(m.Status), prefixed("Area ", wlOrName(m.Area.Target(), name(m.Area.Target()))))
		section(&b, "Links", pointerLines(o.Project.Links))
		var ts []string
		for _, t := range o.Tasks {
			ts = append(ts, taskLine(t))
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
		ov.glance = glanceLine("Task", string(m.Status), prefixed("due ", string(m.Due)), prefixed("in ", wlOrName(proj, name(proj))), assignee,
			prefixed("needs ", strings.Join(deps, ", ")))
		if len(m.BlockedBy) > 0 {
			section(&b, "Blocked by", m.BlockedBy)
		}
	case *model.Question:
		scope := m.Scope.Target()
		ov.glance = glanceLine("Question", string(m.AsKind()), string(m.Status), prefixed("about ", wlOrName(scope, name(scope))))
		var opts []string
		for _, o := range m.Options {
			if o == m.Recommended {
				o += " (recommended)"
			}
			opts = append(opts, o)
		}
		section(&b, "Options", opts)
	case *model.RFX:
		o, err := ops.RFXShow.Run(ctx, e.Deps, ops.RFXShowInput{ID: d.ID})
		if err != nil {
			return ov
		}
		dir := "You asked"
		if m.Direction == model.RFXIn {
			dir = "Asks you"
		}
		ov.glance = glanceLine(dir, string(m.Status), prefixed("route ", name(m.Route)), prefixed("nudge ", string(m.NextNudge)))
		var ts []string
		for _, t := range m.Targets {
			who := firstNonEmpty(wlOrName(t.To.Target(), name(t.To.Target())), "someone")
			s := who + " · " + string(t.Medium) + " · " + string(t.State)
			if t.Sent != "" {
				s += " " + string(t.Sent)
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
			chain = append(chain, wl(c.ID, c.Title)+" · "+c.Status)
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
