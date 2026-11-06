package paramd

import (
	"strings"
	"testing"
)

const annotatedBody = `Intro text.

> [!note] ann-top1
> Read the whole policy first.

## Retention

- R1 policy-logging: Logs are kept 30 days. ^R1

> [!exception] ann-ab12 · review_by 2027-01-31 · by [[person-x]] · kind underspecified · anchor ^R1
> Thirty days from what: write or rotate?
>
> Second paragraph.

> [!tip] Not para's
> Left alone.

> [!note] A plain Obsidian note
> Also left alone.

### Detail

` + "```" + `
> [!exception] ann-incode
` + "```" + `

## Access

> [!constraint] ann-cd34 · why audit · by [[person-y]] · source https://acme.example/a · applies_in staging, prod · review_by 2027-02-01
> No shared accounts.`

func TestCalloutsParse(t *testing.T) {
	cs := Callouts(annotatedBody)
	var got []string
	for _, c := range cs {
		got = append(got, c.Kind+" "+c.ID+" @"+c.Heading)
	}
	want := []string{"note ann-top1 @", "exception ann-ab12 @Retention", "constraint ann-cd34 @Access"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("callouts = %q, want %q", got, want)
	}
	ex := cs[1]
	if ex.Field("review_by") != "2027-01-31" || ex.Field("by") != "[[person-x]]" || ex.Field("kind") != "underspecified" || ex.Field("anchor") != "^R1" {
		t.Fatalf("fields = %+v", ex.Fields)
	}
	if ex.Text != "Thirty days from what: write or rotate?\n\nSecond paragraph." || ex.Line != 10 || ex.End != 13 {
		t.Fatalf("text = %q lines %d-%d", ex.Text, ex.Line, ex.End)
	}
	if v := cs[2].Field("applies_in"); v != "staging, prod" {
		t.Fatalf("applies_in = %q", v)
	}
}

// Print gives back each callout as written: parse then print is byte-exact.
func TestCalloutsRoundTrip(t *testing.T) {
	lines := strings.Split(annotatedBody, "\n")
	for _, c := range Callouts(annotatedBody) {
		src := strings.Join(lines[c.Line-1:c.End], "\n")
		if got := c.Print(); got != src {
			t.Fatalf("print of %s:\n%s\nwant\n%s", c.ID, got, src)
		}
	}
	// A file with callouts parses to the same bytes; Raw is untouched.
	data := doc(policyFM, "# Logging\n\n"+annotatedBody+"\n")
	f := Parse("policies/policy-logging.md", []byte(data))
	if string(f.Raw) != data || len(f.Callouts) != 3 || f.Callouts[0].Heading != "" || f.Callouts[1].Line != f.TitleLine+11 {
		t.Fatalf("file callouts = %+v (body line %d)", f.Callouts, f.BodyLine)
	}
}

func TestCalloutBrokenHeader(t *testing.T) {
	for _, body := range []string{
		"> [!exception] no id here\n> text",
		"> [!exception] ann-ab12 · review_by\n> text",
		"> [!convention] ann-Bad\n> text",
		"> [!constraint] ann-ab12 · by x · by y",
	} {
		cs := Callouts(body)
		if len(cs) != 1 || cs[0].Broken == "" || cs[0].ID != "" {
			t.Fatalf("%q: callouts = %+v", body, cs)
		}
		data := doc(personFM, "# P\n\n"+body+"\n")
		var found bool
		for _, fd := range Lint(Parse("org/p.md", []byte(data))) {
			if fd.Code == CodeAnnotation {
				found = fd.Severity == Warn
			}
		}
		if !found {
			t.Fatalf("%q: no annotation warning", body)
		}
	}
	if cs := Callouts("> [!note] Just a note\n> text"); len(cs) != 0 {
		t.Fatalf("a plain note = %+v", cs)
	}
}

func TestInsertAndRemoveCallout(t *testing.T) {
	base := "Intro.\n\n## Rules\n\n- R1 policy-x: Keep logs. ^R1\n- R2 policy-x: Rotate. ^R2\n\n## Access\n\nOnly staff."
	c := func(id string) Callout {
		return Callout{Kind: "exception", ID: id, Fields: []CalloutField{{"kind", "bug"}, {"by", "me"}}, Text: "Why."}
	}
	cases := []struct {
		anchor Anchor
		want   string
	}{
		{Anchor{}, "Intro.\n\n> [!exception] ann-a1 · kind bug · by me\n> Why.\n\n## Rules"},
		{Anchor{Heading: "Access"}, "Only staff.\n\n> [!exception] ann-a1 · kind bug · by me\n> Why."},
		{Anchor{Block: "R1"}, "^R1\n> [!exception] ann-a1 · kind bug · by me\n> Why.\n\n- R2"},
		{Anchor{Heading: "rules"}, "^R2\n\n> [!exception] ann-a1 · kind bug · by me\n> Why.\n\n## Access"},
	}
	for _, tc := range cases {
		got, err := InsertCallout(base, tc.anchor, c("ann-a1"))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, tc.want) {
			t.Fatalf("insert at %s:\n%s\nwant to contain\n%s", tc.anchor, got, tc.want)
		}
		// A second one under the same anchor goes after the first.
		two, err := InsertCallout(got, tc.anchor, c("ann-b2"))
		if err != nil {
			t.Fatal(err)
		}
		if i, j := strings.Index(two, "ann-a1"), strings.Index(two, "ann-b2"); i < 0 || j < i {
			t.Fatalf("order at %s:\n%s", tc.anchor, two)
		}
		if cs := Callouts(two); len(cs) != 2 {
			t.Fatalf("callouts after two inserts at %s = %+v", tc.anchor, cs)
		}
		back, err := RemoveCallout(two, "ann-b2")
		if err == nil {
			back, err = RemoveCallout(back, "ann-a1")
		}
		if err != nil || back != base {
			t.Fatalf("remove at %s: %v\n%q\nwant\n%q", tc.anchor, err, back, base)
		}
	}
	if _, err := InsertCallout(base, Anchor{Block: "R9"}, c("ann-a1")); err == nil {
		t.Fatal("insert at a missing block worked")
	}
	if _, err := InsertCallout(base, Anchor{Heading: "Nope"}, c("ann-a1")); err == nil {
		t.Fatal("insert at a missing heading worked")
	}
	if _, err := InsertCallout(base, Anchor{}, Callout{Kind: "note", ID: "ann-x", Fields: []CalloutField{{"by", "a · b"}}}); err == nil {
		t.Fatal("a value with the separator was printed")
	}
	// The top of a body that starts with a heading, and an empty body.
	for _, b := range []string{"## Only\n\nText.", ""} {
		got, err := InsertCallout(b, Anchor{}, c("ann-a1"))
		if err != nil {
			t.Fatal(err)
		}
		if back, err := RemoveCallout(got, "ann-a1"); err != nil || back != b {
			t.Fatalf("top of %q: %q -> %q, %v", b, got, back, err)
		}
	}
}

func TestReplaceCallout(t *testing.T) {
	cs := Callouts(annotatedBody)
	c := cs[1].Set("review_by", "2027-06-30").Set("anchor", "")
	got, err := ReplaceCallout(annotatedBody, c)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "> [!exception] ann-ab12 · review_by 2027-06-30 · by [[person-x]] · kind underspecified\n> Thirty") {
		t.Fatalf("replace:\n%s", got)
	}
	if strings.Count(got, "\n") != strings.Count(annotatedBody, "\n") {
		t.Fatal("replace changed other lines")
	}
}
