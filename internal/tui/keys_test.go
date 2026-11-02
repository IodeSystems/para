package tui

import (
	"fmt"
	"strings"
	"testing"
)

func TestAssignFirstFreeLetterInLabel(t *testing.T) {
	got := Assign(nil, []Target{{ID: "a", Label: "Beta"}, {ID: "b", Label: "Bold"}, {ID: "c", Label: "Bee"}}, "")
	want := map[string]Key{"a": {"b", 0}, "b": {"o", 1}, "c": {"e", 1}}
	for id, k := range want {
		if got[id] != k {
			t.Errorf("%s: got %+v want %+v", id, got[id], k)
		}
	}
}

func TestAssignSkipsReservedAndFallsBackToAnyFreeLetter(t *testing.T) {
	// "jqk" holds only reserved letters: any free letter, shown after the label.
	got := Assign(nil, []Target{{ID: "x", Label: "jqk"}}, "")
	k := got["x"]
	if k.At != -1 || strings.ContainsAny(k.Code, "jkgq") || len(k.Code) != 1 {
		t.Fatalf("got %+v", k)
	}
	if m := Mark("jqk", k); !strings.HasPrefix(m, "jqk ") || !strings.Contains(m, keyed(k.Code)) {
		t.Fatalf("mark %q", m)
	}
}

func TestAssignOneKeyPerTarget(t *testing.T) {
	got := Assign(nil, []Target{{ID: "t", Label: "Alpha"}, {ID: "t", Label: "other words"}, {ID: "u", Label: "Alpha"}}, "")
	if len(got) != 2 || got["t"].Code != "a" || got["u"].Code != "l" {
		t.Fatalf("got %+v", got)
	}
}

func TestAssignActionsBeforeLinks(t *testing.T) {
	// The link comes first in the slice order but actions pick first.
	got := Assign([]Target{{ID: "act:zoom", Label: "Zoom", Pref: 'z'}}, []Target{{ID: "wiki:z", Label: "Zed"}}, "")
	if got["act:zoom"].Code != "z" || got["wiki:z"].Code != "e" {
		t.Fatalf("got %+v", got)
	}
}

func TestAssignPrefersKeyOnlyWhenInLabel(t *testing.T) {
	got := Assign([]Target{{ID: "a", Label: "Open", Pref: 'z'}}, nil, "")
	if got["a"].Code != "o" {
		t.Fatalf("got %+v", got["a"])
	}
}

func TestAssignOverflowUsesSemicolonAndTwoLetters(t *testing.T) {
	var links []Target
	for i := 0; i < 40; i++ {
		links = append(links, Target{ID: fmt.Sprintf("l%02d", i), Label: "x"})
	}
	got := Assign(nil, links, "")
	seen := map[string]bool{}
	over := 0
	for _, l := range links {
		k := got[l.ID]
		if seen[k.Code] {
			t.Fatalf("duplicate code %q", k.Code)
		}
		seen[k.Code] = true
		if strings.HasPrefix(k.Code, ";") {
			over++
			if len(k.Code) != 3 {
				t.Fatalf("overflow code %q", k.Code)
			}
		}
	}
	// 26 letters less jkgq = 22 single keys; the rest overflow.
	if over != 40-22 {
		t.Fatalf("overflow count %d", over)
	}
}

func TestAssignDeterministic(t *testing.T) {
	var links []Target
	for i := 0; i < 30; i++ {
		links = append(links, Target{ID: fmt.Sprintf("l%d", i), Label: fmt.Sprintf("item %d name", i)})
	}
	a, b := Assign(nil, links, "zx"), Assign(nil, links, "zx")
	for id, k := range a {
		if b[id] != k {
			t.Fatalf("%s differs", id)
		}
	}
}
