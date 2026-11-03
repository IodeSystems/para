package changes

import (
	"strings"
	"testing"
)

func TestEveryEmbeddedFileParses(t *testing.T) {
	all, err := All()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) == 0 {
		t.Fatal("no release notes")
	}
	for _, r := range all {
		if len(r.People) == 0 || len(r.Agents) == 0 {
			t.Errorf("%s: a section is empty", r.Version)
		}
	}
	if Latest() != all[len(all)-1].Version {
		t.Errorf("Latest = %s", Latest())
	}
}

func TestSinceReturnsOnlyNewerVersionsOldestFirst(t *testing.T) {
	data := func(v string) Release {
		r, err := Parse(v, []byte("# x\n\n## People\n\n- a\n  continued\n\n## Agents\n\n- b\n"))
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	if r := data("0.2.0"); r.People[0] != "a continued" || r.Agents[0] != "b" {
		t.Fatalf("parse = %+v", r)
	}
	for _, c := range []struct {
		a, b string
		want int
	}{{"0.1.0", "0.2.0", -1}, {"0.10.0", "0.9.0", 1}, {"1.0", "1.0.0", 0}, {"0.1.1", "0.1.0", 1}} {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%s, %s) = %d", c.a, c.b, got)
		}
	}
	rs, err := Since("0.0.1")
	if err != nil || len(rs) == 0 || rs[0].Version != "0.1.0" {
		t.Fatalf("since 0.0.1 = %v, %v", rs, err)
	}
	rs, err = Since(Latest())
	if err != nil || len(rs) != 0 {
		t.Fatalf("since latest = %v, %v", rs, err)
	}
	if rs, _ = Since(""); len(rs) != 1 || rs[0].Version != Latest() {
		t.Fatalf("since empty = %v", rs)
	}
	if _, err := Since("abc"); err == nil {
		t.Fatal("a bad version was accepted")
	}
}

func TestParseRefusesAFileWithoutBothSections(t *testing.T) {
	for _, body := range []string{"## People\n\n- a\n", "## Agents\n\n- a\n", "- a\n\n## People\n## Agents\n", "## Other\n"} {
		if _, err := Parse("0.1.0", []byte(body)); err == nil {
			t.Errorf("accepted %q", body)
		}
	}
}

func TestRenderFiltersByAudience(t *testing.T) {
	rs, _ := Since("")
	if txt := Render(rs, "people"); strings.Contains(txt, "Agents") || !strings.Contains(txt, "People") {
		t.Errorf("people render = %q", txt)
	}
	if txt := Render(rs, "agents"); strings.Contains(txt, "People") || !strings.Contains(txt, "Agents") {
		t.Errorf("agents render = %q", txt)
	}
}
