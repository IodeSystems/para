package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func nodesOf(ids ...string) []DNode {
	var ns []DNode
	for _, id := range ids {
		ns = append(ns, DNode{ID: id, Label: "task " + id})
	}
	return ns
}

func arrows(s string) int { return strings.Count(s, "▼") + strings.Count(s, "▲") }

func TestDAGPlain(t *testing.T) {
	out := RenderDAG(nodesOf("a", "b", "c"), []DEdge{{"a", "b"}, {"b", "c"}, {"a", "c"}}, nil, 80)
	for _, id := range []string{"task a", "task b", "task c"} {
		if !strings.Contains(out, id) {
			t.Fatalf("missing %s:\n%s", id, out)
		}
	}
	if arrows(out) != 3 {
		t.Fatalf("arrows %d:\n%s", arrows(out), out)
	}
}

func TestDAGCycleIsShown(t *testing.T) {
	nodes := nodesOf("a", "b", "c", "d")
	edges := []DEdge{{"a", "b"}, {"b", "c"}, {"c", "a"}, {"c", "d"}}
	out := RenderDAG(nodes, edges, [][]string{{"a", "b", "c"}}, 80)
	for _, n := range nodes {
		if !strings.Contains(out, n.Label) {
			t.Fatalf("node %s dropped:\n%s", n.ID, out)
		}
	}
	if got := arrows(out); got != len(edges) {
		t.Fatalf("arrowheads %d, edges %d:\n%s", got, len(edges), out)
	}
	if !strings.Contains(out, "▲") {
		t.Fatalf("no upward arrow for the back edge:\n%s", out)
	}
	if !strings.Contains(out, "cycle: a -> b -> c -> a") {
		t.Fatalf("no legend:\n%s", out)
	}
}

func TestDAGAllNodesInCycle(t *testing.T) {
	// No node is a root: the old renderer dropped all of them.
	out := RenderDAG(nodesOf("a", "b"), []DEdge{{"a", "b"}, {"b", "a"}}, [][]string{{"a", "b"}}, 80)
	if !strings.Contains(out, "task a") || !strings.Contains(out, "task b") || arrows(out) != 2 {
		t.Fatalf("\n%s", out)
	}
}

func TestDAGSelfLoop(t *testing.T) {
	out := RenderDAG(nodesOf("a", "b"), []DEdge{{"a", "a"}, {"a", "b"}}, [][]string{{"a"}}, 80)
	if !strings.Contains(out, "task a ↻") {
		t.Fatalf("self loop not marked:\n%s", out)
	}
}

func TestDAGCrossingDiffersFromJunction(t *testing.T) {
	// K3,3 forces crossings.
	var edges []DEdge
	for _, a := range []string{"a", "b", "c"} {
		for _, b := range []string{"x", "y", "z"} {
			edges = append(edges, DEdge{a, b})
		}
	}
	out := RenderDAG(nodesOf("a", "b", "c", "x", "y", "z"), edges, nil, 100)
	if !strings.ContainsRune(out, crossRune) {
		t.Fatalf("no crossing glyph:\n%s", out)
	}
	if arrows(out) != 9 {
		t.Fatalf("arrows %d:\n%s", arrows(out), out)
	}
}

func TestDAGWidthAndCJK(t *testing.T) {
	ns := []DNode{{"a", "日本語のタスク"}, {"b", "🚀 launch the thing now"}, {"c", "plain"}}
	out := RenderDAG(ns, []DEdge{{"a", "c"}, {"b", "c"}}, nil, 40)
	for _, l := range strings.Split(out, "\n") {
		if ansi.StringWidth(l) > 40 {
			t.Errorf("line is %d wide: %q", ansi.StringWidth(l), l)
		}
	}
	// Box borders line up: every row of a box ends at the same cell.
	if !strings.Contains(out, "日本語のタスク") {
		t.Fatalf("\n%s", out)
	}
}
