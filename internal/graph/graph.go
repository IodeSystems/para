// Package graph is a pure dependency graph over string ids. It knows nothing
// about documents or stores. An edge From -> To means From depends on To.
package graph

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Node is one vertex. Done means the node is resolved: it no longer holds
// back its dependents and is not itself work. Blocked means a person marked
// the node blocked, whatever its dependencies say.
type Node struct {
	ID      string
	Done    bool
	Blocked bool
}

// Edge says From depends on To.
type Edge struct {
	From string
	To   string
}

// CycleError reports every dependency cycle. Each cycle is the sorted ids of
// one strongly connected group, so a self-edge is a cycle of one.
type CycleError struct {
	Cycles [][]string
}

func (e *CycleError) Error() string {
	parts := make([]string, len(e.Cycles))
	for i, c := range e.Cycles {
		parts[i] = strings.Join(c, " ")
	}
	return "graph: dependency cycle: " + strings.Join(parts, "; ")
}

// Graph is an immutable, validated graph.
type Graph struct {
	nodes []Node          // sorted by id
	byID  map[string]Node //
	deps  map[string][]string
}

// New validates and builds a graph. It returns an error for an empty or
// repeated node id and for an edge that names an unknown node. Repeated edges
// collapse. Cycles are not an error here: see Order and Cycles.
func New(nodes []Node, edges []Edge) (*Graph, error) {
	g := &Graph{byID: map[string]Node{}, deps: map[string][]string{}}
	for _, n := range nodes {
		if n.ID == "" {
			return nil, errors.New("graph: new: empty node id")
		}
		if _, dup := g.byID[n.ID]; dup {
			return nil, fmt.Errorf("graph: new: duplicate node %q", n.ID)
		}
		g.byID[n.ID] = n
		g.nodes = append(g.nodes, n)
	}
	sort.Slice(g.nodes, func(i, j int) bool { return g.nodes[i].ID < g.nodes[j].ID })
	seen := map[Edge]bool{}
	for _, e := range edges {
		for _, id := range []string{e.From, e.To} {
			if _, ok := g.byID[id]; !ok {
				return nil, fmt.Errorf("graph: new: edge %s -> %s names unknown node %q", e.From, e.To, id)
			}
		}
		if seen[e] {
			continue
		}
		seen[e] = true
		g.deps[e.From] = append(g.deps[e.From], e.To)
	}
	for _, d := range g.deps {
		sort.Strings(d)
	}
	return g, nil
}

// Deps returns the direct dependencies of id, sorted.
func (g *Graph) Deps(id string) []string { return g.deps[id] }

// Waiting returns the dependencies of id that are not Done, sorted.
func (g *Graph) Waiting(id string) []string {
	var out []string
	for _, d := range g.deps[id] {
		if !g.byID[d].Done {
			out = append(out, d)
		}
	}
	return out
}

// Ready returns the ids of nodes that are not Done, not Blocked, and have
// every dependency Done, sorted. A node in a cycle waits on a node that is
// not Done, so it is never ready.
func (g *Graph) Ready() []string {
	var out []string
	for _, n := range g.nodes {
		if !n.Done && !n.Blocked && len(g.Waiting(n.ID)) == 0 {
			out = append(out, n.ID)
		}
	}
	return out
}

// Held is a node that cannot start, with the reasons.
type Held struct {
	ID        string
	Marked    bool     // the node itself is marked Blocked
	WaitingOn []string // dependencies that are not Done
}

// Blocked returns the nodes that are not Done and are either marked Blocked
// or wait on a dependency, sorted by id.
func (g *Graph) Blocked() []Held {
	var out []Held
	for _, n := range g.nodes {
		if n.Done {
			continue
		}
		w := g.Waiting(n.ID)
		if n.Blocked || len(w) > 0 {
			out = append(out, Held{ID: n.ID, Marked: n.Blocked, WaitingOn: w})
		}
	}
	return out
}

// Order returns every id with dependencies before dependents. Ties break by
// id, so the result is stable. With a cycle it returns a *CycleError and no
// order.
func (g *Graph) Order() ([]string, error) {
	if cycles := g.Cycles(); len(cycles) > 0 {
		return nil, &CycleError{Cycles: cycles}
	}
	pending := map[string]int{}
	users := map[string][]string{}
	for _, n := range g.nodes {
		pending[n.ID] = len(g.deps[n.ID])
		for _, d := range g.deps[n.ID] {
			users[d] = append(users[d], n.ID)
		}
	}
	var queue, out []string
	for _, n := range g.nodes {
		if pending[n.ID] == 0 {
			queue = append(queue, n.ID)
		}
	}
	for len(queue) > 0 {
		sort.Strings(queue)
		id := queue[0]
		queue = queue[1:]
		out = append(out, id)
		for _, u := range users[id] {
			if pending[u]--; pending[u] == 0 {
				queue = append(queue, u)
			}
		}
	}
	return out, nil
}

// Cycles returns every cycle as the sorted ids of a strongly connected group
// (Tarjan). A node with an edge to itself is a cycle of one. Groups are
// ordered by first id. The result is nil when the graph is acyclic.
func (g *Graph) Cycles() [][]string {
	var (
		index   = map[string]int{}
		low     = map[string]int{}
		onStack = map[string]bool{}
		stack   []string
		next    int
		out     [][]string
	)
	var visit func(string)
	visit = func(v string) {
		index[v], low[v] = next, next
		next++
		stack = append(stack, v)
		onStack[v] = true
		for _, w := range g.deps[v] {
			if _, seen := index[w]; !seen {
				visit(w)
				low[v] = min(low[v], low[w])
			} else if onStack[w] {
				low[v] = min(low[v], index[w])
			}
		}
		if low[v] != index[v] {
			return
		}
		var group []string
		for {
			w := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			onStack[w] = false
			group = append(group, w)
			if w == v {
				break
			}
		}
		if len(group) > 1 || hasEdge(g.deps[v], v) {
			sort.Strings(group)
			out = append(out, group)
		}
	}
	for _, n := range g.nodes {
		if _, seen := index[n.ID]; !seen {
			visit(n.ID)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0] < out[j][0] })
	return out
}

func hasEdge(deps []string, to string) bool {
	for _, d := range deps {
		if d == to {
			return true
		}
	}
	return false
}
