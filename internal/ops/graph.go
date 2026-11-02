package ops

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/iodesystems/parasolve/internal/graph"
	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/store"
)

// Node states in GraphNode.State.
const (
	StateDone    = "done"    // resolved: finished, dropped, answered, or closed
	StateReady   = "ready"   // open, not blocked, every dependency resolved
	StateBlocked = "blocked" // marked blocked by a person
	StateWaiting = "waiting" // a dependency is not resolved
	StateMissing = "missing" // an edge names a document that does not exist
)

// GraphNode is one task or rfx in the graph. Type is "missing" for a
// dependency target that no document answers to.
type GraphNode struct {
	ID        string     `json:"id"`
	Type      model.Type `json:"type"`
	Title     string     `json:"title"`
	Status    string     `json:"status"`
	State     string     `json:"state"`
	Project   string     `json:"project,omitempty"`
	Area      string     `json:"area,omitempty"`
	BlockedBy []string   `json:"blocked_by"`
	WaitingOn []string   `json:"waiting_on"`
	// Kind and Scope are set on a question: question, decision, or input, and
	// the scope id or "root". An open question holds back what depends on it.
	Kind  model.QuestionKind `json:"kind,omitempty"`
	Scope string             `json:"scope,omitempty"`
	// Names are the titles of Project, Area and Scope, for people.
	ProjectName string `json:"project_name,omitempty" nameof:"Project"`
	AreaName    string `json:"area_name,omitempty" nameof:"Area"`
	ScopeName   string `json:"scope_name,omitempty" nameof:"Scope"`
}

// GraphEdge says From depends on To.
type GraphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// GraphInput is the input of graph.
type GraphInput struct {
	Project string `json:"project,omitempty" query:"project"` // project id
	Area    string `json:"area,omitempty" query:"area"`       // area id: its own tasks and those of its projects
	// With neither, the graph covers every task in the vault.
}

// GraphOutput is the output of graph. Order lists ids with dependencies first;
// it is empty when Cycles is not. Cycles is never dropped: the command prints
// the graph, then fails.
type GraphOutput struct {
	Nodes  []GraphNode `json:"nodes"`
	Edges  []GraphEdge `json:"edges"`
	Order  []string    `json:"order"`
	Cycles [][]string  `json:"cycles"`
}

// Failed implements Failing.
func (o GraphOutput) Failed() error { return cyclesError(o.Cycles) }

func cyclesError(c [][]string) error {
	if len(c) == 0 {
		return nil
	}
	return &graph.CycleError{Cycles: c}
}

// Graph returns the task dependency graph of a project, of an area, or of
// every task in the vault.
// Dependency targets outside the project are included, with their own
// dependencies, so readiness is right across projects.
var Graph = define("graph", "Show task and rfx dependencies", func(ctx context.Context, d Deps, in GraphInput) (GraphOutput, error) {
	if in.Project != "" && in.Area != "" {
		return GraphOutput{}, errors.New("give project or area, not both")
	}
	w, err := loadWork(ctx, d.Store, in.Project, in.Area)
	if err != nil {
		return GraphOutput{}, err
	}
	return w.output(), nil
})

// NextInput is the input of next.
type NextInput struct {
	Project string `json:"project,omitempty" query:"project"`
}

// NextOutput is the output of next. Cycles is as in GraphOutput.
type NextOutput struct {
	Tasks  []TaskView `json:"tasks"`
	Cycles [][]string `json:"cycles"`
}

// Failed implements Failing.
func (o NextOutput) Failed() error { return cyclesError(o.Cycles) }

// Next lists tasks that can start now: todo or doing, not blocked, every
// dependency resolved, and assigned to you (not to a person or an agent). Order: due date first (earliest first, undated last),
// then oldest created, then id.
var Next = define("next", "List tasks that are ready to work", func(ctx context.Context, d Deps, in NextInput) (NextOutput, error) {
	w, err := loadWork(ctx, d.Store, in.Project, "")
	if err != nil {
		return NextOutput{}, err
	}
	out := NextOutput{Tasks: []TaskView{}, Cycles: w.g.Cycles()}
	if out.Cycles == nil {
		out.Cycles = [][]string{}
	}
	for _, id := range w.g.Ready() {
		if v, ok := w.tasks[id]; ok && (in.Project == "" || v.Project == in.Project) && !IsOther(v.Assignee) {
			out.Tasks = append(out.Tasks, v)
		}
	}
	sortNext(out.Tasks)
	return out, nil
})

// sortNext orders tasks by due date, undated last, then created, then id.
func sortNext(tasks []TaskView) {
	slices.SortStableFunc(tasks, func(a, b TaskView) int {
		if (a.Due == "") != (b.Due == "") {
			if a.Due == "" {
				return 1
			}
			return -1
		}
		return cmp.Or(cmp.Compare(a.Due, b.Due), cmp.Compare(a.Created, b.Created), cmp.Compare(a.ID, b.ID))
	})
}

type work struct {
	g     *graph.Graph
	nodes []GraphNode // sorted by id
	edges []GraphEdge
	tasks map[string]TaskView
}

// loadWork reads the tasks of a project (every task when project is empty),
// then every task and rfx they depend on, transitively.
func loadWork(ctx context.Context, s store.Store, project, area string) (*work, error) {
	if project != "" {
		if _, _, err := getProject(ctx, s, project); err != nil {
			return nil, err
		}
	}
	inArea := map[string]bool{}
	if area != "" {
		area = normID(area)
		if _, _, err := getArea(ctx, s, area); err != nil {
			return nil, err
		}
		projects, err := s.List(ctx, store.Filter{Type: model.TypeProject})
		if err != nil {
			return nil, err
		}
		for _, r := range projects {
			doc, err := s.Get(ctx, r.ID)
			if err != nil {
				return nil, err
			}
			if p, ok := doc.Meta.(*model.Project); ok && p.Area.Target() == area {
				inArea[r.ID] = true
			}
		}
	}
	all, err := listTasks(ctx, s, "", "")
	if err != nil {
		return nil, err
	}
	w := &work{tasks: map[string]TaskView{}}
	byID := map[string]GraphNode{}
	var gnodes []graph.Node
	var gedges []graph.Edge
	var queue []string
	add := func(n GraphNode, done, blocked bool) {
		byID[n.ID] = n
		gnodes = append(gnodes, graph.Node{ID: n.ID, Done: done, Blocked: blocked})
	}
	for _, t := range all {
		w.tasks[t.ID] = t
		if (project == "" && area == "" || t.Project == project && project != "") || (area != "" && (t.Area == area || inArea[t.Project])) {
			queue = append(queue, t.ID)
		}
	}
	// An open question that waits on something belongs in the graph, so the wait
	// shows: in the whole vault graph, and in the graph of its scope.
	qrefs, err := s.List(ctx, store.Filter{Type: model.TypeQuestion})
	if err != nil {
		return nil, err
	}
	for _, r := range qrefs {
		doc, err := s.Get(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		q, ok := doc.Meta.(*model.Question)
		if !ok || q.Status != model.QuestionOpen || len(q.DependsOn) == 0 {
			continue
		}
		sc := scopeID(q)
		if project == "" && area == "" || sc == project && project != "" || (area != "" && (sc == area || inArea[sc])) {
			queue = append(queue, r.ID)
		}
	}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if _, seen := byID[id]; seen {
			continue
		}
		doc, err := s.Get(ctx, id)
		switch {
		case errors.Is(err, model.ErrNotFound):
			add(GraphNode{ID: id, Type: "missing", Title: id, State: StateMissing}, false, false)
			continue
		case err != nil:
			return nil, err
		}
		var deps []string
		switch m := doc.Meta.(type) {
		case *model.Task:
			t := w.tasks[id]
			add(GraphNode{ID: id, Type: model.TypeTask, Title: doc.Title, Status: string(m.Status),
				Project: t.Project, Area: t.Area, BlockedBy: t.BlockedBy}, resolved(doc), m.Status == model.TaskBlocked)
			deps = t.DependsOn
		case *model.RFX:
			add(GraphNode{ID: id, Type: model.TypeRFX, Title: doc.Title, Status: string(m.Status),
				Project: m.Project.Target()}, resolved(doc), false)
		case *model.Question:
			gn := GraphNode{ID: id, Type: model.TypeQuestion, Title: doc.Title, Status: string(m.Status),
				Kind: m.AsKind(), Scope: scopeID(m)}
			if sd, err := scopeDoc(ctx, s, gn.Scope); err == nil {
				switch sm := sd.Meta.(type) {
				case *model.Project:
					gn.Project = sd.ID
				case *model.Area:
					gn.Area = sd.ID
				case *model.Task:
					gn.Project, gn.Area = sm.Project.Target(), sm.Area.Target()
				}
			}
			add(gn, resolved(doc), false)
			deps = targets(m.DependsOn)
		default:
			add(GraphNode{ID: id, Type: doc.Meta.Header().Type, Title: doc.Title}, false, false)
		}
		for _, to := range deps {
			gedges = append(gedges, graph.Edge{From: id, To: to})
			queue = append(queue, to)
		}
	}
	w.g, err = graph.New(gnodes, gedges)
	if err != nil {
		return nil, fmt.Errorf("ops: graph: %w", err)
	}
	ready := map[string]bool{}
	for _, id := range w.g.Ready() {
		ready[id] = true
	}
	for _, n := range gnodes {
		gn := byID[n.ID]
		gn.WaitingOn = append([]string{}, w.g.Waiting(n.ID)...)
		gn.BlockedBy = append([]string{}, gn.BlockedBy...)
		switch {
		case gn.State == StateMissing:
		case n.Done:
			gn.State = StateDone
		case ready[n.ID]:
			gn.State = StateReady
		case n.Blocked:
			gn.State = StateBlocked
		default:
			gn.State = StateWaiting
		}
		w.nodes = append(w.nodes, gn)
		for _, to := range w.g.Deps(n.ID) {
			w.edges = append(w.edges, GraphEdge{From: n.ID, To: to})
		}
	}
	sort.Slice(w.nodes, func(i, j int) bool { return w.nodes[i].ID < w.nodes[j].ID })
	return w, nil
}

func (w *work) output() GraphOutput {
	out := GraphOutput{Nodes: w.nodes, Edges: w.edges, Cycles: w.g.Cycles()}
	if out.Edges == nil {
		out.Edges = []GraphEdge{}
	}
	if out.Cycles == nil {
		out.Cycles = [][]string{}
	}
	out.Order = []string{}
	if order, err := w.g.Order(); err == nil {
		out.Order = order
	}
	return out
}

// Mermaid renders the graph as mermaid flowchart source. An arrow runs from
// a dependency to the node that needs it. Titles go in quoted labels with
// quotes removed, so a title cannot break the syntax.
func Mermaid(o GraphOutput) string {
	var b strings.Builder
	b.WriteString("flowchart TD\n")
	alias := map[string]string{}
	for i, n := range o.Nodes {
		alias[n.ID] = fmt.Sprintf("n%d", i)
	}
	for _, n := range o.Nodes {
		label := mermaidText(n.Title)
		if n.Title == n.ID || n.Title == "" {
			label = mermaidText(n.ID)
		}
		open, closing := "[", "]"
		switch n.Type {
		case model.TypeRFX:
			open, closing = "([", "])"
		case model.TypeQuestion:
			open, closing = "{{", "}}"
		}
		fmt.Fprintf(&b, "  %s%s\"%s\"%s\n", alias[n.ID], open, label, closing)
	}
	for _, e := range o.Edges {
		fmt.Fprintf(&b, "  %s --> %s\n", alias[e.To], alias[e.From])
	}
	for _, state := range []string{StateDone, StateReady, StateBlocked, StateWaiting, StateMissing} {
		var ids []string
		for _, n := range o.Nodes {
			if n.State == state {
				ids = append(ids, alias[n.ID])
			}
		}
		if len(ids) > 0 {
			fmt.Fprintf(&b, "  class %s %s\n", strings.Join(ids, ","), state)
		}
	}
	b.WriteString("  classDef done fill:#ddd,stroke:#999,color:#666\n")
	b.WriteString("  classDef ready fill:#cfc,stroke:#393\n")
	b.WriteString("  classDef blocked fill:#fcc,stroke:#c33\n")
	b.WriteString("  classDef waiting fill:#ffc,stroke:#cc3\n")
	b.WriteString("  classDef missing fill:#fff,stroke:#c33,stroke-dasharray:4\n")
	return b.String()
}

func mermaidText(s string) string {
	return strings.NewReplacer(`"`, "'", "\n", " ", "\r", " ").Replace(s)
}
