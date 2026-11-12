package ops

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// The project bundle (plan/paralog.md § 7): what project_show returns. It is
// read from the vault each time and stores nothing.

// bundleTypes are the types a bundle reads beside goals.
var bundleTypes = []model.Type{model.TypeTask, model.TypeObligation, model.TypeRFX, model.TypeQuestion, model.TypeNote, model.TypeTrackedDoc}

// linksOf returns the goal and project links of a document that can have them.
func linksOf(m model.Meta) (goal, project string) {
	switch x := m.(type) {
	case *model.Task:
		return x.Goal.Target(), x.Project.Target()
	case *model.Obligation:
		return x.Goal.Target(), x.Project.Target()
	case *model.RFX:
		return x.Goal.Target(), x.Project.Target()
	case *model.Note:
		return x.Goal.Target(), x.Project.Target()
	case *model.TrackedDoc:
		return x.Goal.Target(), x.Project.Target()
	case *model.Question:
		return x.Scope.Target(), x.Scope.Target() // the scope is one link: a goal or the project
	}
	return "", ""
}

// projectBundle builds the bundle of the project doc.
func projectBundle(ctx context.Context, d Deps, doc model.Document, p *model.Project) (ProjectShowOutput, error) {
	out := ProjectShowOutput{Project: projectView(doc, p), Body: clip(doc.Body, projectBodyMax), StateAgeDays: -1,
		Goals: []BundleGoal{}, WaitsOn: []BundleItem{}, Icebox: []BundleItem{}, Crons: []string{}, Direct: []BundleItem{}, History: []BundleEvent{}, More: []string{}}
	today := d.Store.Now().Format(dateLayout)
	if p.StateDate != "" {
		out.StateAgeDays = daysBetween(string(p.StateDate), today)
	}
	latest := string(cmp.Or(p.Updated, p.Created))
	seen := func(c *model.Common) {
		if u := string(cmp.Or(c.Updated, c.Created)); u > latest {
			latest = u
		}
	}
	mine := func(id string) bool { return id != "" && (id == doc.ID || p.WasID(id)) }

	// Goals of the project, archived ones too: closed goals are history.
	grefs, err := d.Store.List(ctx, store.Filter{Type: model.TypeGoal, All: true})
	if err != nil {
		return out, err
	}
	goals := map[string]*BundleGoal{} // in flight, by id and former id
	parked := map[string]string{}     // parked goal by id and former id -> its id
	var closed []BundleEvent
	for _, r := range grefs {
		gd, err := d.Store.Get(ctx, r.ID)
		if err != nil {
			continue
		}
		g, ok := gd.Meta.(*model.Goal)
		if !ok || !mine(g.Project.Target()) || g.DuplicateOf != "" {
			continue
		}
		seen(&g.Common)
		switch g.Status {
		case model.GoalActive, model.GoalPaused:
			out.Goals = append(out.Goals, BundleGoal{ID: gd.ID, Title: gd.Title, Status: g.Status, DoneWhen: g.DoneWhen, Due: g.Due, Tasks: []BundleItem{}})
		case model.GoalParked:
			out.Icebox = append(out.Icebox, BundleItem{ID: gd.ID, Type: model.TypeGoal, Title: gd.Title, Status: string(g.Status), Note: cmp.Or(g.ResumeWhen, "no resume_when")})
			parked[gd.ID] = gd.ID
			for _, f := range g.FormerIDs {
				parked[f] = gd.ID
			}
		default:
			closed = append(closed, BundleEvent{At: string(cmp.Or(g.Updated, g.Created)), ID: gd.ID, Kind: "goal_" + string(g.Status), Summary: gd.Title})
		}
	}
	slices.SortFunc(out.Goals, func(a, b BundleGoal) int { return cmp.Or(cmpDue(a.Due, b.Due), cmp.Compare(a.ID, b.ID)) })
	slices.SortFunc(out.Icebox, func(a, b BundleItem) int { return cmp.Compare(a.ID, b.ID) })
	for i := range out.Goals {
		g := &out.Goals[i]
		goals[g.ID] = g
		if gd, err := d.Store.Get(ctx, g.ID); err == nil {
			for _, f := range gd.Meta.Header().FormerIDs {
				goals[f] = g
			}
		}
	}

	// Tasks, waits, and direct links. The open tasks of a parked goal are
	// only counted: Next and needs_you hide them until it is unparked.
	hidden := map[string]int{}
	for _, typ := range bundleTypes {
		refs, err := d.Store.List(ctx, store.Filter{Type: typ})
		if err != nil {
			return out, err
		}
		for _, r := range refs {
			x, err := d.Store.Get(ctx, r.ID)
			if err != nil || x.Meta == nil {
				continue
			}
			gid, pid := linksOf(x.Meta)
			if pg := parked[gid]; pg != "" && typ == model.TypeTask && lifeOf(x.Meta) == LifeOpen {
				hidden[pg]++
			}
			g := goals[gid]
			direct := mine(pid)
			if g == nil && !direct {
				continue
			}
			if lifeOf(x.Meta) != LifeOpen {
				continue
			}
			if o, ok := x.Meta.(*model.Obligation); ok && isCron(o) && mine(pid) {
				continue // a cron is a line of its own (crons)
			}
			seen(x.Meta.Header())
			it := BundleItem{ID: x.ID, Type: x.Meta.Header().Type, Title: x.Title, Status: statusOf(x.Meta)}
			if g != nil {
				it.Via, it.ViaTitle = g.ID, g.Title
			}
			switch m := x.Meta.(type) {
			case *model.Task:
				it.Due = m.Due
				if m.Status == model.TaskBlocked {
					it.Note = clip(strings.Join(m.BlockedBy, "; "), 120)
					out.WaitsOn = append(out.WaitsOn, it)
				}
				if g != nil {
					g.Tasks = append(g.Tasks, it)
				} else if m.Status != model.TaskBlocked {
					out.Direct = append(out.Direct, it)
				}
				continue
			case *model.RFX:
				it.Due = m.NextNudge
				if m.Direction == model.RFXOut {
					out.WaitsOn = append(out.WaitsOn, it)
					continue
				}
			case *model.Question:
				out.WaitsOn = append(out.WaitsOn, it)
				continue
			}
			if direct {
				out.Direct = append(out.Direct, it)
			}
		}
	}
	for i := range out.Icebox {
		out.Icebox[i].HiddenTasks = hidden[out.Icebox[i].ID]
	}
	byDue := func(a, b BundleItem) int { return cmp.Or(cmpDue(a.Due, b.Due), cmp.Compare(a.ID, b.ID)) }
	for i := range out.Goals {
		g := &out.Goals[i]
		slices.SortFunc(g.Tasks, byDue)
		if j := slices.IndexFunc(g.Tasks, func(t BundleItem) bool { return t.Status != string(model.TaskBlocked) }); j >= 0 {
			g.Next, g.NextTitle = g.Tasks[j].ID, g.Tasks[j].Title
		}
	}
	slices.SortFunc(out.WaitsOn, byDue)
	slices.SortFunc(out.Direct, func(a, b BundleItem) int { return cmp.Or(cmp.Compare(a.Type, b.Type), cmp.Compare(a.ID, b.ID)) })
	out.LastActivity = model.Date(latest)

	crons, cut, err := projectCrons(ctx, d.Store, mine)
	if err != nil {
		return out, err
	}
	out.Crons = append(out.Crons, crons...)

	// History: closed goals and the project's own timeline, newest first.
	rows, _, err := historyRows(ctx, d, doc.ID)
	if err != nil {
		return out, err
	}
	hist := closed
	for _, r := range rows {
		hist = append(hist, BundleEvent{At: r.t.In(d.Store.Now().Location()).Format(dateLayout), ID: doc.ID, Kind: r.Kind, Summary: clip(r.Summary, 160)})
	}
	slices.SortStableFunc(hist, func(a, b BundleEvent) int { return cmp.Compare(b.At, a.At) })
	more := map[string]int{}
	if cut > 0 {
		more["crons"] = cut
	}
	if len(hist) > projectHistoryMax {
		more["history"] = len(hist) - projectHistoryMax
		hist = hist[:projectHistoryMax]
	}
	out.History = hist
	capBundle(&out, more)
	return out, nil
}

// cmpDue orders dates with the empty date last.
func cmpDue(a, b model.Date) int {
	switch {
	case a == b:
		return 0
	case a == "":
		return 1
	case b == "":
		return -1
	}
	return cmp.Compare(a, b)
}

// bundleSize is the encoded size of the bundle.
func bundleSize(out *ProjectShowOutput) int {
	b, err := json.Marshal(out)
	if err != nil {
		return 0
	}
	return len(b)
}

// capBundle cuts the lists of out until it encodes in projectBundleMax bytes:
// each round takes the last item of the longest list (history, direct, icebox,
// waits, a goal's tasks, then goals), so every list keeps its head. more holds
// counts cut before; the "more" lines are written last.
func capBundle(out *ProjectShowOutput, more map[string]int) {
	id := out.Project.ID
	lines := func() []string {
		var l []string
		add := func(key, how string) {
			if n := more[key]; n > 0 {
				l = append(l, fmt.Sprintf("%s: %d more: %s", key, n, how))
			}
		}
		add("goals", "list {type: goal} (goals with project "+id+")")
		add("tasks", "show {id: <goal>} for each goal")
		add("waits_on", "show {id: <goal>}, list {type: rfx}")
		add("icebox", "list {type: goal, status: parked}")
		add("crons", "list {type: obligation} (crons with project "+id+")")
		add("direct", "search {query: "+id+"}")
		add("history", "op_run {op: history, input: {id: "+id+"}}")
		return l
	}
	for {
		out.More = lines()
		if out.More == nil {
			out.More = []string{}
		}
		if bundleSize(out) <= projectBundleMax {
			return
		}
		// The longest cuttable list, by item count; ties go to the later
		// section (history before goals).
		type cut struct {
			key string
			n   int
			fn  func()
		}
		var cuts []cut
		cuts = append(cuts, cut{"goals", len(out.Goals), func() { out.Goals = out.Goals[:len(out.Goals)-1] }})
		for i := range out.Goals {
			g := &out.Goals[i]
			cuts = append(cuts, cut{"tasks", len(g.Tasks), func() {
				g.Tasks = g.Tasks[:len(g.Tasks)-1]
				g.MoreTasks++
			}})
		}
		cuts = append(cuts,
			cut{"waits_on", len(out.WaitsOn), func() { out.WaitsOn = out.WaitsOn[:len(out.WaitsOn)-1] }},
			cut{"icebox", len(out.Icebox), func() { out.Icebox = out.Icebox[:len(out.Icebox)-1] }},
			cut{"direct", len(out.Direct), func() { out.Direct = out.Direct[:len(out.Direct)-1] }},
			cut{"history", len(out.History), func() { out.History = out.History[:len(out.History)-1] }},
		)
		best := -1
		for i, c := range cuts {
			if c.n > 0 && (best < 0 || c.n >= cuts[best].n) {
				best = i
			}
		}
		if best < 0 {
			// Only the header is left; the body goes before the header would.
			if out.Body == "" {
				return
			}
			out.Body = clip(out.Body, len([]rune(out.Body))/2)
			if len([]rune(out.Body)) < 20 {
				out.Body = ""
			}
			continue
		}
		cuts[best].fn()
		more[cuts[best].key]++
	}
}
