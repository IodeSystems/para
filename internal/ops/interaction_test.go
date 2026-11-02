package ops

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/iodesystems/parasolve/internal/model"
)

func TestInteractionAdd(t *testing.T) {
	d := directoryDeps(t)
	long := "Short title.\n" + strings.Repeat("x", 501)
	cases := []struct {
		name     string
		in       InteractionAddInput
		wantErr  string
		warnings int
	}{
		{"person", InteractionAddInput{Summary: "Agreed to review Friday", With: []string{"person-ada-example"}, Medium: "slack", Direction: "in", Date: "2026-11-01", URL: "https://acme.example/m/1"}, "", 0},
		{"group and person, public", InteractionAddInput{Summary: "Announced the freeze", With: []string{"group-ops-chat", "[[person-bo-example]]", "person-bo-example"}, Medium: "slack", Direction: "observed", Visibility: "public"}, "", 0},
		{"body at limit", InteractionAddInput{Summary: "T\n" + strings.Repeat("x", 500), With: []string{"person-ada-example"}, Medium: "email", Direction: "out"}, "", 0},
		{"body over limit warns", InteractionAddInput{Summary: long, With: []string{"person-ada-example"}, Medium: "email", Direction: "out"}, "", 1},
		{"empty summary", InteractionAddInput{With: []string{"person-ada-example"}, Medium: "email", Direction: "out"}, "summary", 0},
		{"no one", InteractionAddInput{Summary: "x", Medium: "email", Direction: "out"}, "with", 0},
		{"unknown person", InteractionAddInput{Summary: "x", With: []string{"person-gone"}, Medium: "email", Direction: "out"}, "not found", 0},
		{"with a role", InteractionAddInput{Summary: "x", With: []string{"role-x"}, Medium: "email", Direction: "out"}, "not found", 0},
		{"bad medium", InteractionAddInput{Summary: "x", With: []string{"person-ada-example"}, Medium: "fax", Direction: "out"}, "medium", 0},
		{"missing direction", InteractionAddInput{Summary: "x", With: []string{"person-ada-example"}, Medium: "email"}, "direction", 0},
		{"bad visibility", InteractionAddInput{Summary: "x", With: []string{"person-ada-example"}, Medium: "email", Direction: "in", Visibility: "secret"}, "visibility", 0},
		{"bad date", InteractionAddInput{Summary: "x", With: []string{"person-ada-example"}, Medium: "email", Direction: "in", Date: "Nov 1"}, "date", 0},
		{"bad url", InteractionAddInput{Summary: "x", With: []string{"person-ada-example"}, Medium: "email", Direction: "in", URL: "ftp://x.example/a"}, "url", 0},
		{"signed url is refused by lint", InteractionAddInput{Summary: "x", With: []string{"person-ada-example"}, Medium: "email", Direction: "in", URL: "https://acme.example/a?token=abc"}, "url", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := InteractionAdd.Run(ctx, d, c.in)
			if (err == nil) != (c.wantErr == "") || (err != nil && !strings.Contains(err.Error(), c.wantErr)) {
				t.Fatalf("err = %v", err)
			}
			if err == nil && len(out.Warnings) != c.warnings {
				t.Errorf("warnings = %v", out.Warnings)
			}
		})
	}
	// The warning is a lint warning too: the document is stored.
	findings, err := d.Store.Lint(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 || findings[0].Code != "summary-long" || findings[0].Severity != "warn" {
		t.Errorf("lint = %v", findings)
	}
}

func TestInteractionListAndShow(t *testing.T) {
	d := directoryDeps(t)
	dates := map[string]string{}
	add := func(summary, date, with string, medium model.Medium, dir model.InteractionDirection) string {
		id := must(t, InteractionAdd, d, InteractionAddInput{Summary: summary, With: []string{with}, Medium: medium, Direction: dir, Date: date}).ID
		dates[id] = date
		return id
	}
	a := add("one", "2026-10-01", "person-ada-example", "email", "out")
	b := add("two", "2026-11-01", "person-ada-example", "slack", "in")
	c := add("three", "2026-10-15", "person-bo-example", "slack", "observed")
	e := add("four", "2026-11-01", "group-ops-chat", "slack", "observed")
	cases := []struct {
		name string
		in   InteractionListInput
		want []string
	}{
		{"all, newest first", InteractionListInput{}, newestFirst(dates, a, b, c, e)},
		{"with ada", InteractionListInput{With: "person-ada-example"}, newestFirst(dates, a, b)},
		{"with a group", InteractionListInput{With: "[[group-ops-chat]]"}, []string{e}},
		{"medium", InteractionListInput{Medium: "email"}, []string{a}},
		{"direction", InteractionListInput{Direction: "observed", Since: "2026-10-20"}, []string{e}},
		{"since is inclusive", InteractionListInput{Since: "2026-10-15"}, newestFirst(dates, b, c, e)},
		{"limit", InteractionListInput{With: "person-ada-example", Limit: 1}, []string{b}},
		{"nobody", InteractionListInput{With: "person-zed"}, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := InteractionList.Run(ctx, d, tc.in)
			if err != nil {
				t.Fatal(err)
			}
			got := []string{}
			for _, it := range out.Items {
				got = append(got, it.ID)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
	for _, bad := range []InteractionListInput{{Since: "x"}, {Limit: -1}} {
		if _, err := InteractionList.Run(ctx, d, bad); err == nil {
			t.Errorf("%+v accepted", bad)
		}
	}
	show, err := InteractionShow.Run(ctx, d, InteractionShowInput{ID: a})
	if err != nil || show.Doc.Title != "one" {
		t.Fatalf("show = %+v, %v", show, err)
	}
	if _, err := InteractionShow.Run(ctx, d, InteractionShowInput{ID: "person-ada-example"}); err == nil {
		t.Error("show accepted a person")
	}
	mustLintClean(t, d)
}

// newestFirst orders ids by date descending, ties by id descending.
func newestFirst(dates map[string]string, ids ...string) []string {
	out := append([]string{}, ids...)
	sort.Slice(out, func(i, j int) bool {
		if dates[out[i]] != dates[out[j]] {
			return dates[out[i]] > dates[out[j]]
		}
		return out[i] > out[j]
	})
	return out
}
