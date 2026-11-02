package ops

import (
	"reflect"
	"strings"
	"testing"

	"github.com/iodesystems/paradize/internal/model"
)

func whoDeps(t *testing.T) Deps {
	t.Helper()
	d := targetDeps(t)
	must(t, OrgPersonAdd, d, OrgAddInput{Name: "Adam Example", Org: "acme.example", Aliases: []string{"Ada"}})
	must(t, OrgPersonAdd, d, OrgAddInput{Name: "Cy Other", Org: "other.example"})
	must(t, OrgPersonContactAdd, d, PersonContactInput{ID: "person-cy-other", Method: "github", Value: "cy-codes"})
	must(t, OrgPersonGroupAdd, d, PersonGroupInput{ID: "person-ada-example", Group: "group-atlas"})
	must(t, OrgPersonNote, d, PersonNoteInput{ID: "person-ada-example", Text: "Works with Cy Other on the audit.", Date: "2026-11-01"})
	return d
}

func TestWho(t *testing.T) {
	d := whoDeps(t)
	cases := []struct {
		name       string
		query      string
		wantID     string
		candidates []string
	}{
		{"exact name, any case", "ADA EXAMPLE", "person-ada-example", nil},
		{"id", "person-bo-example", "person-bo-example", nil},
		{"alias", "ada e", "person-ada-example", nil},
		{"contact value", "ADA@acme.example", "person-ada-example", nil},
		{"contact handle", "cy-codes", "person-cy-other", nil},
		{"exact alias beats partial name", "Ada", "person-adam-example", nil},
		{"partial, one match", "other", "person-cy-other", nil},
		{"partial, several matches", "example", "", []string{"person-ada-example", "person-adam-example", "person-bo-example"}},
		{"partial matches name and alias", "ad", "", []string{"person-ada-example", "person-adam-example"}},
		{"note text alone does not match", "audit", "", nil},
		{"no match", "zed", "", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := Who.Run(ctx, d, WhoInput{Query: c.query})
			if err != nil {
				t.Fatal(err)
			}
			switch {
			case c.wantID != "":
				if out.Person == nil || out.Person.ID != c.wantID || len(out.Candidates) != 0 {
					t.Fatalf("out = %+v", out)
				}
			default:
				if out.Person != nil {
					t.Fatalf("picked %s", out.Person.ID)
				}
				got := refIDs(out.Candidates)
				if len(c.candidates) == 0 {
					got = nil
				}
				if !reflect.DeepEqual(got, c.candidates) {
					t.Errorf("candidates = %v, want %v", got, c.candidates)
				}
			}
		})
	}
	if _, err := Who.Run(ctx, d, WhoInput{Query: "  "}); err == nil {
		t.Error("empty query accepted")
	}
	if _, err := Who.Run(ctx, d, WhoInput{Query: "ada", Recent: -1}); err == nil {
		t.Error("negative recent accepted")
	}
}

func TestWhoDetails(t *testing.T) {
	d := whoDeps(t)
	role := must(t, OrgRoleAdd, d, OrgAddInput{Name: "Lead", Org: "acme.example"})
	team := must(t, OrgTeamAdd, d, OrgAddInput{Name: "Platform", Org: "acme.example"})
	// Link role and team by writing the person directly.
	doc, p, err := getPerson(ctx, d, "person-ada-example")
	if err != nil {
		t.Fatal(err)
	}
	p.Role, p.Team = model.LinkTo(role.ID), model.LinkTo(team.ID)
	if _, err := d.Store.Update(ctx, doc); err != nil {
		t.Fatal(err)
	}
	for i, date := range []string{"2026-10-01", "2026-10-02", "2026-10-03"} {
		must(t, InteractionAdd, d, InteractionAddInput{Summary: "talk " + date, With: []string{"person-ada-example"}, Medium: "slack", Direction: "in", Date: date})
		_ = i
	}
	must(t, InteractionAdd, d, InteractionAddInput{Summary: "someone else", With: []string{"person-bo-example"}, Medium: "slack", Direction: "in", Date: "2026-10-09"})
	open := addRFX(t, d, RFXAddInput{Ask: "open ask", Targets: []TargetInput{{To: "person-ada-example", Medium: "slack"}, {To: "person-bo-example", Medium: "email"}}})
	closed := addRFX(t, d, RFXAddInput{Ask: "closed ask", Targets: []TargetInput{{To: "person-ada-example", Medium: "email"}}})
	must(t, RFXClose, d, RFXCloseInput{ID: closed})
	addRFX(t, d, RFXAddInput{Ask: "someone else's", Targets: []TargetInput{{To: "person-bo-example", Medium: "email"}}})

	out, err := Who.Run(ctx, d, WhoInput{Query: "person-ada-example", Recent: 2})
	if err != nil {
		t.Fatal(err)
	}
	p2 := out.Person
	if p2 == nil {
		t.Fatal("no person")
	}
	if p2.Name != "Ada Example" || p2.Org != "acme.example" || !reflect.DeepEqual(p2.Aliases, []string{"Ada E"}) {
		t.Errorf("identity = %+v", p2)
	}
	if p2.Role == nil || p2.Role.Title != "Lead" || p2.Team == nil || p2.Team.ID != team.ID {
		t.Errorf("role/team = %+v / %+v", p2.Role, p2.Team)
	}
	if len(p2.Groups) != 1 || p2.Groups[0].ID != "group-atlas" || p2.Groups[0].Title != "Atlas" {
		t.Errorf("groups = %+v", p2.Groups)
	}
	var pref []string
	for _, c := range p2.Contacts {
		if c.Preferred {
			pref = append(pref, c.Value)
		}
	}
	if len(p2.Contacts) != 5 || !reflect.DeepEqual(pref, []string{"@ada-oncall"}) {
		t.Errorf("contacts = %+v", p2.Contacts)
	}
	if !reflect.DeepEqual(p2.Notes, []string{"- 2026-11-01: Works with Cy Other on the audit."}) {
		t.Errorf("notes = %v", p2.Notes)
	}
	var titles []string
	for _, i := range p2.Interactions {
		titles = append(titles, i.Title)
	}
	if !reflect.DeepEqual(titles, []string{"talk 2026-10-03", "talk 2026-10-02"}) {
		t.Errorf("interactions = %v", titles)
	}
	if len(p2.OpenRFX) != 1 || p2.OpenRFX[0].ID != open || len(p2.OpenRFX[0].Targets) != 1 || p2.OpenRFX[0].Targets[0].Medium != "slack" {
		t.Errorf("open rfx = %+v", p2.OpenRFX)
	}
	// A person with nothing recorded has empty lists, never null.
	bo, err := Who.Run(ctx, d, WhoInput{Query: "cy other"})
	if err != nil || bo.Person == nil {
		t.Fatalf("cy: %+v %v", bo, err)
	}
	q := bo.Person
	if q.Role != nil || q.Team != nil || q.Groups == nil || q.Notes == nil || q.Interactions == nil || q.OpenRFX == nil || q.Aliases == nil {
		t.Errorf("empty person = %+v", q)
	}
	mustLintClean(t, d)
}

// TestScanFinderSearchText: the Finder sees aliases and contact values.
func TestScanFinderSearchText(t *testing.T) {
	d := whoDeps(t)
	cases := []struct{ term, want string }{
		{"ada e", "person-ada-example"},
		{"cy-codes", "person-cy-other"},
		{"+1 555 0100", "person-ada-example"},
	}
	for _, c := range cases {
		refs, err := ScanFinder{}.Find(ctx, d.Store, Query{Types: []model.Type{model.TypePerson}, Terms: []string{strings.ToLower(c.term)}})
		if err != nil {
			t.Fatal(err)
		}
		if len(refs) == 0 || refs[0].ID != c.want {
			t.Errorf("%q: refs = %+v", c.term, refs)
		}
	}
}
