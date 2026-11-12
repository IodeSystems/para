package ops

import (
	"cmp"
	"context"
	"slices"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// GoalRow is the summary of a goal on list {type: goal}: what the Goals view
// shows per row, in one read for every goal.
type GoalRow struct {
	Project      string     `json:"project,omitempty"`
	ProjectTitle string     `json:"project_title,omitempty"`
	Due          model.Date `json:"due,omitempty"`
	// OpenTasks counts the open tasks linked to the goal.
	OpenTasks int `json:"open_tasks"`
	// Next is the first open task that is not blocked, by due date.
	Next      string `json:"next,omitempty"`
	NextTitle string `json:"next_title,omitempty"`
}

type goalAcc struct {
	row      *GoalRow
	nextDue  model.Date
	nextSeen bool
}

// goalRows builds the summary of each listed goal in one pass over the open
// tasks, keyed by goal id.
func goalRows(ctx context.Context, d Deps, sn *Snapshot, rows []ListRow) (map[string]*GoalRow, error) {
	out := map[string]*GoalRow{}
	accs := map[string]*goalAcc{} // by goal id and former id
	for _, r := range rows {
		doc, err := sn.Get(r.ID)
		if err != nil {
			continue
		}
		g, ok := doc.Meta.(*model.Goal)
		if !ok {
			continue
		}
		gr := &GoalRow{Due: g.Due}
		if pid := g.Project.Target(); pid != "" {
			gr.Project = pid
			if pd, err := sn.Get(pid); err == nil {
				gr.Project, gr.ProjectTitle = pd.ID, pd.Title
			}
		}
		a := &goalAcc{row: gr}
		out[doc.ID], accs[doc.ID] = gr, a
		for _, f := range g.FormerIDs {
			accs[f] = a
		}
	}
	if len(out) == 0 {
		return out, nil
	}
	refs, err := d.Store.List(ctx, store.Filter{Type: model.TypeTask})
	if err != nil {
		return nil, err
	}
	for _, r := range refs {
		x, err := d.Store.Get(ctx, r.ID)
		if err != nil || x.Meta == nil || lifeOf(x.Meta) != LifeOpen {
			continue
		}
		t, ok := x.Meta.(*model.Task)
		if !ok {
			continue
		}
		a := accs[t.Goal.Target()]
		if a == nil {
			continue
		}
		a.row.OpenTasks++
		if t.Status == model.TaskBlocked {
			continue
		}
		if !a.nextSeen || cmp.Or(cmpDue(t.Due, a.nextDue), cmp.Compare(x.ID, a.row.Next)) < 0 {
			a.nextSeen, a.nextDue, a.row.Next, a.row.NextTitle = true, t.Due, x.ID, x.Title
		}
	}
	return out, nil
}

// sortGoalRows orders goals by due date (none last), then title, then id.
func sortGoalRows(rows []ListRow) {
	due := func(r ListRow) model.Date {
		if r.Goal == nil {
			return ""
		}
		return r.Goal.Due
	}
	slices.SortStableFunc(rows, func(a, b ListRow) int {
		return cmp.Or(cmpDue(due(a), due(b)), cmp.Compare(a.Title, b.Title), cmp.Compare(a.ID, b.ID))
	})
}
