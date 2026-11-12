package upgrade

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// GoalsFormat is the format at which a finite outcome is a goal (type goal in
// goals/, id goal-<ashid>) and type project is the ongoing project
// (plan/paralog.md). Below it a file of type project is a goal.
const GoalsFormat = model.GoalsFormat

// ProjectSort is the health kind the owner dismisses on an area or a goal to
// keep it as it is: the judgement step projects-sort then leaves it.
const ProjectSort = "project_sort"

// Counts step goals reports, in this order.
const (
	CountGoals         = "goals"
	CountRePrefixed    = "goals_re_prefixed"
	CountMoved         = "files_moved"
	CountRelinked      = "docs_relinked"
	CountKeysMoved     = "keys_project_to_goal"
	CountParked        = "someday_to_parked"
	CountParkedNoWhen  = "parked_without_resume_when"
	CountRecordsLeft   = "records_kept"
	goalPrefixFrom     = "project-"
	goalPrefixTo       = "goal-"
	goalFoldedFromType = model.TypeProject
)

// CountOrder lists the counts of step goals in the order a report prints them.
var CountOrder = []string{CountGoals, CountRePrefixed, CountMoved, CountRelinked, CountKeysMoved, CountParked, CountParkedNoWhen, CountRecordsLeft}

// legacyGoal selects what step goals changes: every file of type project.
// The step reads the vault natively (store.Native), so a goal below format 11
// decodes as a project that keeps its goal keys; project ops refuse below
// format 11, so every such file is a goal.
func legacyGoal(d model.Document) bool {
	_, ok := d.Meta.(*model.Project)
	return ok
}

// goalID is the id a goal takes: project-<ashid> becomes goal-<ashid>; an id
// a person chose stays.
func goalID(id string) string {
	if rest, ok := strings.CutPrefix(id, goalPrefixFrom); ok && rest != "" {
		return goalPrefixTo + rest
	}
	return id
}

// asGoal retypes a legacy goal: type goal with folded_from project, in
// goals/ (an archived one stays in archive/). A someday goal is parked, and
// its deferred_reason becomes resume_when.
func asGoal(d model.Document) (model.Document, error) {
	folder := d.Folder
	out, err := model.Retype(d, model.TypeGoal, map[string]any{"folded_from": string(goalFoldedFromType)})
	if err != nil {
		return d, err
	}
	if g := out.Meta.(*model.Goal); parks(g) {
		g.Status = model.GoalParked
		g.DeferUntil = ""
		if r := strings.TrimSpace(g.DeferredReason); r != "" {
			g.ResumeWhen = r
			g.DeferredReason = ""
		}
	}
	if folder == model.FolderArchive {
		out.Folder = folder
	}
	return out, nil
}

// parks reports whether a legacy goal becomes parked: deferred to someday and
// not closed.
func parks(g *model.Goal) bool {
	return g.DeferUntil == model.SomedayWord && g.Status != model.GoalDone && g.Status != model.GoalDropped
}

// goalsPlan is what step goals would do, read once.
type goalsPlan struct {
	goals   map[string]bool   // legacy goal ids
	ids     map[string]string // re-prefix: old to new
	tasks   map[string]bool   // tasks that move to goals/ with their goal
	plan    []PlanLine
	counts  map[string]int
	docs    []string
	refDocs []string
}

func planGoals(ctx context.Context, s store.Store) (goalsPlan, error) {
	s = store.Native(s)
	g := goalsPlan{goals: map[string]bool{}, ids: map[string]string{}, tasks: map[string]bool{}, counts: map[string]int{}}
	refs, err := s.List(ctx, store.Filter{All: true})
	if err != nil {
		return g, err
	}
	docs := map[string]model.Document{}
	exists := map[string]bool{}
	for _, r := range refs {
		exists[r.ID] = true
		d, err := s.Get(ctx, r.ID)
		if err != nil {
			continue // lint reports a file that does not decode
		}
		docs[r.ID] = d
		if legacyGoal(d) {
			g.goals[d.ID] = true
		}
	}
	for _, id := range sortedKeys(g.goals) {
		d := docs[id]
		gd, err := model.Retype(d, model.TypeGoal, nil)
		if err != nil {
			return g, fmt.Errorf("%s: %w", id, err)
		}
		p := gd.Meta.(*model.Goal)
		g.counts[CountGoals]++
		lines := []string{"type project → goal (folded_from: project)"}
		if nw := goalID(id); nw != id {
			if exists[nw] {
				return g, fmt.Errorf("%s: the new id %s is taken", id, nw)
			}
			g.ids[id] = nw
			g.counts[CountRePrefixed]++
			lines = append(lines, "id "+id+" → "+nw+" (the old id stays in former_ids)")
		}
		if d.Folder != model.FolderArchive {
			g.counts[CountMoved]++
			lines = append(lines, string(d.Folder)+"/ → goals/")
		}
		if parks(p) {
			g.counts[CountParked]++
			if strings.TrimSpace(p.DeferredReason) == "" {
				g.counts[CountParkedNoWhen]++
				lines = append(lines, "someday → parked, no resume_when (health asks for one)")
			} else {
				lines = append(lines, "someday → parked, resume_when from deferred_reason")
			}
		}
		g.plan = append(g.plan, PlanLine{Doc: id, Lines: lines})
		g.docs = append(g.docs, id)
	}
	// The types after the step, for the key move.
	newToOld := make(map[string]string, len(g.ids))
	for old, nw := range g.ids {
		newToOld[nw] = old
	}
	typeOf := func(id string) model.Type {
		if old, ok := newToOld[id]; ok {
			id = old
		}
		if g.goals[id] {
			return model.TypeGoal
		}
		if d, ok := docs[id]; ok {
			return d.Meta.Header().Type
		}
		return ""
	}
	for _, id := range sortedKeys(docs) {
		d := docs[id]
		if g.goals[id] {
			continue
		}
		if t, ok := d.Meta.(*model.Task); ok && g.goals[t.Project.Target()] && d.Folder == model.FolderProjects {
			g.tasks[id] = true
			g.counts[CountMoved]++
			g.plan = append(g.plan, PlanLine{Doc: id, Lines: []string{"projects/ → goals/ with its goal"}})
			g.docs = append(g.docs, id)
		}
		if model.IsRecord(d.Meta.Header().Type) {
			if names(d, g.ids) {
				g.counts[CountRecordsLeft]++
			}
			continue
		}
		before := topLinks(d, "project")
		nd, changed, err := d.Relink(g.ids, typeOf)
		if err != nil {
			return g, fmt.Errorf("%s: %w", id, err)
		}
		if changed {
			g.counts[CountRelinked]++
			g.refDocs = append(g.refDocs, id)
			if before != "" && topLinks(nd, "project") == "" {
				g.counts[CountKeysMoved]++
			}
		}
	}
	for _, k := range CountOrder {
		if _, ok := g.counts[k]; !ok {
			g.counts[k] = 0
		}
	}
	return g, nil
}

// names reports whether the text of d names an old id.
func names(d model.Document, ids map[string]string) bool {
	b, err := model.Render(d)
	if err != nil {
		return false
	}
	for old := range ids {
		if strings.Contains(string(b), old) {
			return true
		}
	}
	return false
}

// topLinks returns the value of the top-level link key of d, or "".
func topLinks(d model.Document, key string) string {
	fields, err := d.FieldText()
	if err != nil {
		return ""
	}
	return fields[key]
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// runGoals is step goals. A dry run plans and counts. A run writes the
// snapshot first, then retypes and moves in one batch (undoable on this
// machine), then changes the ids and every link (vault ChangeIDs), which
// also moves project: links that name a goal to goal:.
func runGoals(ctx context.Context, s store.Store, dry bool, batch string) (StepOutcome, error) {
	s = store.Native(s)
	g, err := planGoals(ctx, s)
	so := StepOutcome{Docs: g.docs, Count: len(g.docs), Plan: g.plan, Counts: g.counts}
	if err != nil || dry || len(g.goals) == 0 {
		return so, err
	}
	if so.Snapshot, err = s.Snapshot(ctx, batch); err != nil {
		return so, err
	}
	out, err := s.RewriteExpand(ctx, batch, false, func(d model.Document) (model.Document, []model.Document, bool, error) {
		switch {
		case g.goals[d.ID]:
			nd, err := asGoal(d)
			return nd, nil, err == nil, err
		case g.tasks[d.ID]:
			d.Folder = model.FolderGoals
			return d, nil, true, nil
		}
		return d, nil, false, nil
	})
	if err != nil {
		return so, err
	}
	if len(out.Docs) > 0 {
		so.Batch = batch
	}
	// The files are format 11 now, even while an earlier judgement step
	// holds format_version below it.
	if err := s.SetConfig(ctx, model.GoalsStepKey, batch); err != nil {
		return so, err
	}
	ch, err := s.ChangeIDs(ctx, g.ids)
	if err != nil {
		return so, fmt.Errorf("the batch %s ran; the id change stopped (run again to finish): %w", batch, err)
	}
	so.Docs = slices.Clone(out.Docs)
	for _, id := range ch.Rewritten {
		if !slices.Contains(so.Docs, id) {
			so.Docs = append(so.Docs, id)
		}
	}
	for i, id := range so.Docs {
		if nw, ok := g.ids[id]; ok {
			so.Docs[i] = nw
		}
	}
	so.Count = len(so.Docs)
	return so, nil
}

// sortPending selects what the judgement step projects-sort leaves to an
// agent: an area of kind project (or no kind), and an active goal with no
// done_when and no due, unless the owner dismissed it.
func sortPending(d model.Document) bool {
	if d.Folder == model.FolderArchive {
		return false
	}
	for _, x := range d.Meta.Header().HealthDismissed {
		if x.Kind == ProjectSort {
			return false
		}
	}
	switch m := d.Meta.(type) {
	case *model.Area:
		return m.Kind.Or() == model.AreaKindProject && m.Follows == nil
	case *model.Goal:
		return m.Status == model.GoalActive && strings.TrimSpace(m.DoneWhen) == "" && m.Due == ""
	}
	return false
}
