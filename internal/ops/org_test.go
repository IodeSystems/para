package ops

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/store"
)

func TestOrgAdd(t *testing.T) {
	d := newDeps(t)
	role := must(t, OrgRoleAdd, d, OrgAddInput{Name: "Platform Lead", Org: "acme.example"})
	team := must(t, OrgTeamAdd, d, OrgAddInput{Name: "Atlas", Org: "acme.example"})
	if role.ID != "role-platform-lead" {
		t.Fatalf("id = %s", role.ID)
	}
	cases := []struct {
		name    string
		run     func() (OrgAddOutput, error)
		wantErr string
	}{
		{"person with role and team", func() (OrgAddOutput, error) {
			return OrgPersonAdd.Run(ctx, d, OrgAddInput{Name: "Jordan Example", Org: "acme.example", Role: role.ID, Team: "[[" + team.ID + "]]",
				Said: []string{"approvals take two days"}, Source: "chat with Jordan, 2026-10-30"})
		}, ""},
		{"domain with owner", func() (OrgAddOutput, error) {
			return OrgDomainAdd.Run(ctx, d, OrgAddInput{Name: "Logging", Owner: role.ID})
		}, ""},
		{"said without source", func() (OrgAddOutput, error) {
			return OrgPersonAdd.Run(ctx, d, OrgAddInput{Name: "Sam", Org: "o", Said: []string{"x"}})
		}, "source"},
		{"team needs org", func() (OrgAddOutput, error) { return OrgTeamAdd.Run(ctx, d, OrgAddInput{Name: "Sam"}) }, "org"},
		{"empty name", func() (OrgAddOutput, error) { return OrgTeamAdd.Run(ctx, d, OrgAddInput{Org: "o"}) }, "name"},
		{"name with no slug", func() (OrgAddOutput, error) { return OrgTeamAdd.Run(ctx, d, OrgAddInput{Name: "!!!", Org: "o"}) }, "id"},
		{"domain needs owner", func() (OrgAddOutput, error) { return OrgDomainAdd.Run(ctx, d, OrgAddInput{Name: "Billing"}) }, "owner"},
		{"owner must exist", func() (OrgAddOutput, error) {
			return OrgDomainAdd.Run(ctx, d, OrgAddInput{Name: "Billing", Owner: "role-gone"})
		}, "not found"},
		{"owner wrong type", func() (OrgAddOutput, error) {
			return OrgDomainAdd.Run(ctx, d, OrgAddInput{Name: "Billing", Owner: "domain-logging"})
		}, "want one of"},
		{"role must be a role", func() (OrgAddOutput, error) {
			return OrgPersonAdd.Run(ctx, d, OrgAddInput{Name: "Sam", Org: "o", Role: team.ID})
		}, "want one of"},
		{"owner on a person", func() (OrgAddOutput, error) {
			return OrgPersonAdd.Run(ctx, d, OrgAddInput{Name: "Sam", Org: "o", Owner: role.ID})
		}, "owner applies"},
		{"role on a team", func() (OrgAddOutput, error) {
			return OrgTeamAdd.Run(ctx, d, OrgAddInput{Name: "Beta", Org: "o", Role: role.ID})
		}, "person"},
		{"duplicate", func() (OrgAddOutput, error) { return OrgTeamAdd.Run(ctx, d, OrgAddInput{Name: "Atlas", Org: "o"}) }, "in use"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := c.run()
			if (err == nil) != (c.wantErr == "") || (err != nil && !strings.Contains(err.Error(), c.wantErr)) {
				t.Fatalf("err = %v", err)
			}
		})
	}
	mustLintClean(t, d)

	show, err := OrgPersonShow.Run(ctx, d, OrgShowInput{ID: "person-jordan-example"})
	if err != nil {
		t.Fatal(err)
	}
	p := show.Doc.Meta.(*model.Person)
	if p.Role.Target() != role.ID || p.Team.Target() != team.ID {
		t.Fatalf("person = %+v", p)
	}
	if want := "- approvals take two days (source: chat with Jordan, 2026-10-30)"; !strings.Contains(show.Doc.Body, want) {
		t.Fatalf("body = %q", show.Doc.Body)
	}
	if _, err := OrgTeamShow.Run(ctx, d, OrgShowInput{ID: "person-jordan-example"}); err == nil {
		t.Error("team show accepted a person")
	}
	list, err := OrgPersonList.Run(ctx, d, OrgListInput{})
	if err != nil || len(list.Items) != 1 || list.Items[0].ID != "person-jordan-example" {
		t.Fatalf("list = %+v, %v", list, err)
	}
	empty, _ := OrgDomainList.Run(ctx, newDeps(t), OrgListInput{})
	if empty.Items == nil {
		t.Error("empty list encodes as null")
	}
}

func must[I, O any](t *testing.T, op *Op[I, O], d Deps, in I) O {
	t.Helper()
	out, err := op.Run(ctx, d, in)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// seedOrg builds domain-logging -> role-platform-lead -> two people, and a
// second domain owned directly by a person.
func seedOrg(t *testing.T, d Deps) {
	t.Helper()
	must(t, OrgRoleAdd, d, OrgAddInput{Name: "Platform Lead", Org: "acme.example"})
	must(t, OrgTeamAdd, d, OrgAddInput{Name: "Atlas", Org: "acme.example"})
	must(t, OrgPersonAdd, d, OrgAddInput{Name: "Ada Example", Org: "acme.example", Role: "role-platform-lead"})
	must(t, OrgPersonAdd, d, OrgAddInput{Name: "Bo Example", Org: "acme.example", Role: "role-platform-lead"})
	must(t, OrgPersonAdd, d, OrgAddInput{Name: "Cy Example", Org: "acme.example", Team: "team-atlas"})
	must(t, OrgDomainAdd, d, OrgAddInput{Name: "Logging", Owner: "role-platform-lead"})
	must(t, OrgDomainAdd, d, OrgAddInput{Name: "Log Retention", Owner: "team-atlas"})
	must(t, OrgDomainAdd, d, OrgAddInput{Name: "Billing", Owner: "person-cy-example"})
}

func createDoc(t *testing.T, d Deps, doc model.Document) {
	t.Helper()
	if _, err := d.Store.Create(ctx, doc); err != nil {
		t.Fatal(err)
	}
}

func src() model.Source {
	return model.Source{URL: "https://wiki.acme.example/x", Retrieved: "2026-11-01", Fidelity: model.FidelityNone}
}

func refIDs(rs []model.Ref) []string {
	out := []string{}
	for _, r := range rs {
		out = append(out, r.ID)
	}
	return out
}

func TestRoute(t *testing.T) {
	d := newDeps(t)
	seedOrg(t, d)
	cc := func(typ model.Type) model.Common { return model.Common{Type: typ} }
	createDoc(t, d, model.Document{ID: "policy-logging", Folder: model.FolderPolicies, Title: "Logging standard",
		Meta: &model.Policy{Common: cc(model.TypePolicy), Source: src()}})
	createDoc(t, d, model.Document{ID: "policy-billing", Folder: model.FolderPolicies, Title: "Invoice rules",
		Meta: &model.Policy{Common: cc(model.TypePolicy), Source: src()}})
	createDoc(t, d, model.Document{ID: "no-prod-logs", Folder: model.FolderConstraints, Title: "No production logs in dev",
		Body: "Applies to logging pipelines.", Meta: &model.Constraint{Common: cc(model.TypeConstraint), Why: "PII", By: "Ada", Source: src(),
			AppliesIn: []string{"prod"}, NotIn: []string{"dev"}, ReviewBy: "2027-01-01"}})
	createDoc(t, d, model.Document{ID: "branch-names", Folder: model.FolderConventions, Title: "Branch names",
		Meta: &model.Convention{Common: cc(model.TypeConvention), Status: model.ConventionAdopted, AppliesIn: []string{"repo"}}})
	byRoute := addRFX(t, d, RFXAddInput{Route: "domain-logging", Ask: "unrelated words"})
	byText := addRFX(t, d, RFXAddInput{Route: "elsewhere", Ask: "Need logging access"})
	addRFX(t, d, RFXAddInput{Route: "domain-billing", Ask: "invoice question"})

	cases := []struct {
		name       string
		query      string
		domain     string
		owner      string
		people     []string
		candidates []string
		policies   []string
		constrs    []string
		rfx        []string
	}{
		{"domain id", "domain-logging", "domain-logging", "role-platform-lead",
			[]string{"person-ada-example", "person-bo-example"}, []string{}, []string{"policy-logging"}, []string{"no-prod-logs"}, sorted(byRoute, byText)},
		{"bare slug", "logging", "domain-logging", "role-platform-lead",
			[]string{"person-ada-example", "person-bo-example"}, []string{}, []string{"policy-logging"}, []string{"no-prod-logs"}, sorted(byRoute, byText)},
		{"title, any case", "BILLING", "domain-billing", "person-cy-example",
			[]string{"person-cy-example"}, []string{}, []string{"policy-billing"}, []string{}, nil},
		{"team owner", "log retention", "domain-log-retention", "team-atlas",
			[]string{"person-cy-example"}, []string{}, nil, nil, nil},
		{"free text names one domain", "who approves billing exceptions?", "domain-billing", "person-cy-example",
			[]string{"person-cy-example"}, []string{}, []string{"policy-billing"}, []string{}, nil},
		{"free text names none", "kubernetes upgrade", "", "", []string{}, []string{}, []string{}, []string{}, []string{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := Route.Run(ctx, d, RouteInput{Query: c.query})
			if err != nil {
				t.Fatal(err)
			}
			if c.domain == "" {
				if out.Domain != nil || out.Owner != nil {
					t.Fatalf("resolved %+v", out.Domain)
				}
				return
			}
			if out.Domain == nil || out.Domain.ID != c.domain {
				t.Fatalf("domain = %+v", out.Domain)
			}
			if out.Owner == nil || out.Owner.ID != c.owner {
				t.Fatalf("owner = %+v", out.Owner)
			}
			if got := refIDs(out.People); !reflect.DeepEqual(got, c.people) {
				t.Errorf("people = %v", got)
			}
			if c.policies != nil {
				if got := refIDs(out.SelfServe.Policies); !reflect.DeepEqual(got, c.policies) {
					t.Errorf("policies = %v", got)
				}
			}
			if c.constrs != nil {
				if got := refIDs(out.SelfServe.Constraints); !reflect.DeepEqual(got, c.constrs) {
					t.Errorf("constraints = %v", got)
				}
			}
			if c.rfx != nil {
				var ids []string
				for _, s := range out.SelfServe.PriorRFX {
					ids = append(ids, s.ID)
				}
				if !reflect.DeepEqual(ids, c.rfx) {
					t.Errorf("rfx = %v", ids)
				}
			}
		})
	}

	t.Run("tie lists candidates, picks none", func(t *testing.T) {
		out, err := Route.Run(ctx, d, RouteInput{Query: "logging retention policy"})
		if err != nil {
			t.Fatal(err)
		}
		if out.Domain != nil || out.Owner != nil || !reflect.DeepEqual(refIDs(out.Candidates), []string{"domain-log-retention", "domain-logging"}) {
			t.Fatalf("domain = %+v candidates = %v", out.Domain, refIDs(out.Candidates))
		}
		if !reflect.DeepEqual(out.Terms, []string{"logging", "policy", "retention"}) {
			t.Errorf("terms = %v", out.Terms)
		}
	})
	t.Run("empty query", func(t *testing.T) {
		if _, err := Route.Run(ctx, d, RouteInput{Query: " "}); err == nil {
			t.Fatal("empty query accepted")
		}
	})
	t.Run("lists never encode as null", func(t *testing.T) {
		out, _ := Route.Run(ctx, newDeps(t), RouteInput{Query: "anything"})
		ss := out.SelfServe
		if ss.Policies == nil || ss.Constraints == nil || ss.Conventions == nil || ss.PriorRFX == nil || out.Candidates == nil || out.People == nil {
			t.Fatalf("nil list in %+v", out)
		}
	})
}

type fakeFinder struct{ got []Query }

func (f *fakeFinder) Find(_ context.Context, _ store.Store, q Query) ([]model.Ref, error) {
	f.got = append(f.got, q)
	return []model.Ref{{ID: "policy-from-index", Type: q.Types[0], Title: "from the index"}}, nil
}

// DefaultFinder is the seam for the index.
func TestRouteUsesDefaultFinder(t *testing.T) {
	d := newDeps(t)
	f := &fakeFinder{}
	old := DefaultFinder
	DefaultFinder = f
	t.Cleanup(func() { DefaultFinder = old })
	_, err := Route.Run(ctx, d, RouteInput{Query: "certificate rotation"})
	// the fake returns an rfx ref that does not exist: route must report it, not hide it
	if err == nil {
		t.Fatal("dangling index hit ignored")
	}
	if len(f.got) < 3 || !reflect.DeepEqual(f.got[0].Terms, []string{"certificate", "rotation"}) {
		t.Fatalf("queries = %+v", f.got)
	}
}

func TestOrgAddPrivatePerson(t *testing.T) {
	d := newDeps(t)
	p := must(t, OrgPersonAdd, d, OrgAddInput{Name: "Pat Example", Role: "attorney"})
	if len(p.Notes) != 1 || !strings.Contains(p.Notes[0], "role_text") {
		t.Fatalf("notes = %v", p.Notes)
	}
	doc, err := d.Store.Get(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	m := doc.Meta.(*model.Person)
	if m.Org != "" || m.Role != "" || m.RoleText != "attorney" {
		t.Fatalf("person = %+v", m)
	}
	home := must(t, OrgDomainAdd, d, OrgAddInput{Name: "Home", Owner: "self"})
	dd, _ := d.Store.Get(ctx, home.ID)
	if dd.Meta.(*model.Domain).Owner != "self" {
		t.Fatalf("owner = %v", dd.Meta)
	}
	r, err := Route.Run(ctx, d, RouteInput{Query: "home"})
	if err != nil || r.Domain == nil || r.Owner != nil {
		t.Fatalf("route = %+v %v", r, err)
	}
	mustLintClean(t, d)
}
