package graph

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func ns(spec string) []Node {
	// "a b:done c:blocked"
	var out []Node
	for _, f := range strings.Fields(spec) {
		id, flag, _ := strings.Cut(f, ":")
		out = append(out, Node{ID: id, Done: flag == "done", Blocked: flag == "blocked"})
	}
	return out
}

func es(spec string) []Edge {
	// "a>b c>d": a depends on b
	var out []Edge
	for _, f := range strings.Fields(spec) {
		from, to, _ := strings.Cut(f, ">")
		out = append(out, Edge{from, to})
	}
	return out
}

func TestNewErrors(t *testing.T) {
	cases := []struct {
		name  string
		nodes []Node
		edges []Edge
		want  string
	}{
		{"empty id", []Node{{ID: ""}}, nil, "empty node id"},
		{"duplicate", ns("a a"), nil, "duplicate"},
		{"unknown from", ns("a"), es("x>a"), `unknown node "x"`},
		{"unknown to", ns("a"), es("a>x"), `unknown node "x"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := New(c.nodes, c.edges)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestReadyAndBlocked(t *testing.T) {
	cases := []struct {
		name    string
		nodes   string
		edges   string
		ready   []string
		blocked []Held
	}{
		{"empty", "", "", nil, nil},
		{"all independent", "b a", "", []string{"a", "b"}, nil},
		{"chain", "a b c", "b>a c>b", []string{"a"},
			[]Held{{ID: "b", WaitingOn: []string{"a"}}, {ID: "c", WaitingOn: []string{"b"}}}},
		{"dep done", "a:done b", "b>a", []string{"b"}, nil},
		{"diamond partly done", "a:done b c d", "b>a c>a d>b d>c", []string{"b", "c"},
			[]Held{{ID: "d", WaitingOn: []string{"b", "c"}}}},
		{"marked blocked", "a:blocked b", "b>a", nil,
			[]Held{{ID: "a", Marked: true}, {ID: "b", WaitingOn: []string{"a"}}}},
		{"marked and waiting", "a b:blocked", "b>a", []string{"a"},
			[]Held{{ID: "b", Marked: true, WaitingOn: []string{"a"}}}},
		{"done is neither", "a:done b:done", "b>a", nil, nil},
		{"duplicate edge", "a b", "b>a b>a", []string{"a"}, []Held{{ID: "b", WaitingOn: []string{"a"}}}},
		{"cycle never ready", "a b c", "a>b b>a", []string{"c"},
			[]Held{{ID: "a", WaitingOn: []string{"b"}}, {ID: "b", WaitingOn: []string{"a"}}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g, err := New(ns(c.nodes), es(c.edges))
			if err != nil {
				t.Fatal(err)
			}
			if got := g.Ready(); !reflect.DeepEqual(got, c.ready) {
				t.Errorf("ready = %v, want %v", got, c.ready)
			}
			if got := g.Blocked(); !reflect.DeepEqual(got, c.blocked) {
				t.Errorf("blocked = %+v, want %+v", got, c.blocked)
			}
		})
	}
}

func TestOrder(t *testing.T) {
	cases := []struct {
		name, nodes, edges string
		want               []string
	}{
		{"empty", "", "", []string{}},
		{"ties by id", "c b a", "", []string{"a", "b", "c"}},
		{"chain against id order", "a b c", "a>b b>c", []string{"c", "b", "a"}},
		{"diamond", "a b c d", "d>b d>c b>a c>a", []string{"a", "b", "c", "d"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g, _ := New(ns(c.nodes), es(c.edges))
			got, err := g.Order()
			if err != nil {
				t.Fatal(err)
			}
			if len(got) == 0 {
				got = []string{}
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("order = %v, want %v", got, c.want)
			}
		})
	}
}

func TestCycles(t *testing.T) {
	cases := []struct {
		name, nodes, edges string
		want               [][]string
	}{
		{"none", "a b", "a>b", nil},
		{"self", "a b", "a>a", [][]string{{"a"}}},
		{"two", "a b", "a>b b>a", [][]string{{"a", "b"}}},
		{"three plus tail", "a b c d", "a>b b>c c>a d>a", [][]string{{"a", "b", "c"}}},
		{"two separate", "a b c d e", "a>b b>a d>e e>d c>a", [][]string{{"a", "b"}, {"d", "e"}}},
		{"shared back edges", "a b c", "a>b b>c c>b b>a", [][]string{{"a", "b", "c"}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			g, _ := New(ns(c.nodes), es(c.edges))
			if got := g.Cycles(); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("cycles = %v, want %v", got, c.want)
			}
			order, err := g.Order()
			var ce *CycleError
			switch {
			case c.want == nil && err != nil:
				t.Fatalf("order err = %v", err)
			case c.want != nil && !errors.As(err, &ce):
				t.Fatalf("order = %v, err = %v: want *CycleError", order, err)
			case c.want != nil && !reflect.DeepEqual(ce.Cycles, c.want):
				t.Fatalf("error cycles = %v", ce.Cycles)
			case c.want != nil && order != nil:
				t.Fatalf("order returned with a cycle: %v", order)
			}
		})
	}
}

func TestCycleErrorMessage(t *testing.T) {
	e := &CycleError{Cycles: [][]string{{"a", "b"}, {"c"}}}
	if got := e.Error(); got != "graph: dependency cycle: a b; c" {
		t.Fatal(got)
	}
}
