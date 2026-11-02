package ops

import (
	"context"
	"errors"
	"fmt"
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
	BlockedBy []string   `json:"blocked_by"`
	WaitingOn []string   `json:"waiting_on"`
}

// GraphEdge says From depends on To.
type GraphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// GraphInput is the input of graph.
type GraphInput struct {
	Project string `json:"project,omitempty" query:"project"` // project id; empty means every task
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

// Graph returns the task dependency graph of a project, or of every task.
// Dependency targets outside the project are included, with their own
// dependencies, so readiness is right across projects.
var Graph = define("graph", "Show task and rfx dependencies", func(ctx context.Context, d Deps, in GraphInput) (GraphOutput, error) {
	w, err := loadWork(ctx, d.Store, in.Project)
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
// dependency resolved.
var Next = define("next", "List tasks that are ready to work", func(ctx context.Context, d Deps, in NextInput) (NextOutput, error) {
	w, err := loadWork(ctx, d.Store, in.Project)
	if err != nil {
		return NextOutput{}, err
	}
	out := NextOutput{Tasks: []TaskView{}, Cycles: w.g.Cycles()}
	if out.Cycles == nil {
		out.Cycles = [][]string{}
	}
	for _, id := range w.g.Ready() {
		if v, ok := w.tasks[id]; ok && (in.Project == "" || v.Project == in.Project) {
			out.Tasks = append(out.Tasks, v)
		}
	}
	return out, nil
})

type work struct {
	g     *graph.Graph
	nodes []GraphNode // sorted by id
	edges []GraphEdge
	tasks map[string]TaskView
}

// loadWork reads the tasks of a project (every task when project is empty),
// then every task and rfx they depend on, transitively.
func loadWork(ctx context.Context, s store.Store, project string) (*work, error) {
	if project != "" {
		if _, _, err := getProject(ctx, s, project); err != nil {
			return nil, err
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
		if project == "" || t.Project == project {
			queue = append(queue, t.ID)
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
				Project: t.Project, BlockedBy: t.BlockedBy}, resolved(doc), m.Status == model.TaskBlocked)
			deps = t.DependsOn
		case *model.RFX:
			add(GraphNode{ID: id, Type: model.TypeRFX, Title: doc.Title, Status: string(m.Status),
				Project: m.Project.Target()}, resolved(doc), false)
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
		if n.Type == model.TypeRFX {
			open, closing = "([", "])"
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
