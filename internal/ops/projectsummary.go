package ops

import (
	"cmp"
	"context"
	"slices"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// ProjectRow is the summary of a project on list {type: project}: what the
// Projects view shows per row, in one read for every project. It follows
// the rules of the bundle (projectbundle.go); show {id} has the detail.
type ProjectRow struct {
	StateLine string     `json:"state_line,omitempty"`
	StateDate model.Date `json:"state_date,omitempty"`
	// StateAgeDays is the age of state_line in days; -1 with no state_date.
	StateAgeDays int `json:"state_age_days"`
	// OpenGoals counts the goals in flight (active and paused).
	OpenGoals int `json:"open_goals"`
	// WaitsOn counts what the project waits on; FirstWait is the title of
	// the first by due date.
	WaitsOn   int    `json:"waits_on"`
	FirstWait string `json:"first_wait,omitempty"`
	// NextCron is the earliest next run of its crons; a paused cron (goal
	// parked) has none.
	NextCron model.Date `json:"next_cron,omitempty"`
	// KPIAlerts counts the KPIs off target or stale (one each when both).
	KPIAlerts    int        `json:"kpi_alerts"`
	LastActivity model.Date `json:"last_activity,omitempty"`
}

// projectAcc gathers one project's summary in the pass.
type projectAcc struct {
	row       *ProjectRow
	latest    string
	firstDue  model.Date
	firstID   string
	firstSeen bool
}

func (a *projectAcc) seen(c *model.Common) {
	if u := string(cmp.Or(c.Updated, c.Created)); u > a.latest {
		a.latest = u
	}
}

func (a *projectAcc) wait(id, title string, due model.Date) {
	a.row.WaitsOn++
	if !a.firstSeen || cmp.Or(cmpDue(due, a.firstDue), cmp.Compare(id, a.firstID)) < 0 {
		a.firstSeen, a.firstDue, a.firstID, a.row.FirstWait = true, due, id, cmp.Or(title, id)
	}
}

// projectRows builds the summary of each listed project in one pass over the
// goals and the bundle types, keyed by project id.
func projectRows(ctx context.Context, d Deps, sn *Snapshot, rows []ListRow) (map[string]*ProjectRow, error) {
	out := map[string]*ProjectRow{}
	accs := map[string]*projectAcc{} // by project id and former id
	today := d.Store.Now().Format(dateLayout)
	for _, r := range rows {
		doc, err := sn.Get(r.ID)
		if err != nil {
			continue
		}
		p, ok := doc.Meta.(*model.Project)
		if !ok {
			continue
		}
		pr := &ProjectRow{StateLine: p.StateLine, StateDate: p.StateDate, StateAgeDays: -1}
		if p.StateDate != "" {
			pr.StateAgeDays = daysBetween(string(p.StateDate), today)
		}
		for _, k := range projectKPIs(p, today) {
			if k.alert() {
				pr.KPIAlerts++
			}
		}
		a := &projectAcc{row: pr, latest: string(cmp.Or(p.Updated, p.Created))}
		out[doc.ID], accs[doc.ID] = pr, a
		for _, f := range p.FormerIDs {
			accs[f] = a
		}
	}
	if len(out) == 0 {
		return out, nil
	}

	grefs, err := d.Store.List(ctx, store.Filter{Type: model.TypeGoal, All: true})
	if err != nil {
		return nil, err
	}
	inFlight := map[string]*projectAcc{} // goal id and former id -> its project
	parked := map[string]bool{}          // parked goal by id and former id
	for _, r := range grefs {
		gd, err := d.Store.Get(ctx, r.ID)
		if err != nil {
			continue
		}
		g, ok := gd.Meta.(*model.Goal)
		if !ok || g.DuplicateOf != "" {
			continue
		}
		ids := append([]string{gd.ID}, g.FormerIDs...)
		if g.Status == model.GoalParked {
			for _, id := range ids {
				parked[id] = true
			}
		}
		a := accs[g.Project.Target()]
		if a == nil {
			continue
		}
		a.seen(&g.Common)
		if g.Status == model.GoalActive || g.Status == model.GoalPaused {
			a.row.OpenGoals++
			for _, id := range ids {
				inFlight[id] = a
			}
		}
	}

	for _, typ := range bundleTypes {
		refs, err := d.Store.List(ctx, store.Filter{Type: typ})
		if err != nil {
			return nil, err
		}
		for _, r := range refs {
			x, err := d.Store.Get(ctx, r.ID)
			if err != nil || x.Meta == nil || lifeOf(x.Meta) != LifeOpen {
				continue
			}
			gid, pid := linksOf(x.Meta)
			via, direct := inFlight[gid], accs[pid]
			a := cmp.Or(via, direct)
			if a == nil {
				continue
			}
			if o, ok := x.Meta.(*model.Obligation); ok && isCron(o) && direct != nil {
				if o.Makes != model.MakesTask || !parked[o.Goal.Target()] {
					if next := cmp.Or(o.Due, model.Date(o.Scheduled)); next != "" && (direct.row.NextCron == "" || cmpDue(next, direct.row.NextCron) < 0) {
						direct.row.NextCron = model.Date(string(next)[:min(len(next), len(dateLayout))])
					}
				}
				continue
			}
			a.seen(x.Meta.Header())
			switch m := x.Meta.(type) {
			case *model.Task:
				if m.Status == model.TaskBlocked {
					a.wait(x.ID, x.Title, m.Due)
				}
			case *model.RFX:
				if m.Direction == model.RFXOut {
					a.wait(x.ID, x.Title, m.NextNudge)
				}
			case *model.Question:
				a.wait(x.ID, x.Title, "")
			}
		}
	}
	for _, a := range accs {
		a.row.LastActivity = model.Date(a.latest)
	}
	return out, nil
}

// sortProjectRows puts the stalest state first: no state line, then the
// oldest state_date; ties by title, then id.
func sortProjectRows(rows []ListRow) {
	age := func(r ListRow) int {
		switch {
		case r.Project == nil:
			return -2
		case r.Project.StateLine == "" || r.Project.StateAgeDays < 0:
			return 1 << 30
		}
		return r.Project.StateAgeDays
	}
	slices.SortStableFunc(rows, func(a, b ListRow) int {
		return cmp.Or(cmp.Compare(age(b), age(a)), cmp.Compare(a.Title, b.Title), cmp.Compare(a.ID, b.ID))
	})
}
