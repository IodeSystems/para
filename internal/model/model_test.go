package model

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/iodesystems/paradize/internal/paramd"
)

func strs[T ~string](in []T) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = string(v)
	}
	return out
}

// The enums repeat paramd's lists; this fails when the two drift.
func TestEnumsMatchParamd(t *testing.T) {
	cases := []struct {
		name string
		got  []string
		want []string
	}{
		{"type", strs(Types), paramd.Types},
		{"fidelity", strs(Fidelities), paramd.Fidelities()},
		{"project.status", strs(ProjectStatuses), paramd.FieldEnum("project", "status")},
		{"task.status", strs(TaskStatuses), paramd.FieldEnum("task", "status")},
		{"rfx.direction", strs(RFXDirections), paramd.FieldEnum("rfx", "direction")},
		{"rfx.status", strs(RFXStatuses), paramd.FieldEnum("rfx", "status")},
		{"exception.kind", strs(ExceptionKinds), paramd.FieldEnum("policy_exception", "kind")},
		{"exception.status", strs(ExceptionStatuses), paramd.FieldEnum("policy_exception", "status")},
		{"convention.status", strs(ConventionStatuses), paramd.FieldEnum("convention", "status")},
		{"journal.scope", strs(JournalScopes), paramd.FieldEnum("journal", "scope")},
	}
	for _, c := range cases {
		if !slices.Equal(c.got, c.want) {
			t.Errorf("%s: model %v, paramd %v", c.name, c.got, c.want)
		}
	}
}

func source() Source {
	return Source{
		URL:       "https://wiki.acme.example/policies/logging",
		Retrieved: "2026-11-01T09:00:00Z",
		Hash:      "sha256:" + strings.Repeat("ab", 32),
		Fidelity:  FidelityExact,
	}
}

func TestRenderedTypesLintClean(t *testing.T) {
	c := func(typ Type) Common { return Common{Type: typ, Created: "2026-11-01", Tags: []string{"x"}} }
	cases := []struct {
		id   string
		meta Meta
		body string
	}{
		{"acme-portal", &Project{c(TypeProject), ProjectActive, "Jordan Example"}, ""},
		{"task-one", &Task{c(TypeTask), TaskBlocked, LinkTo("acme-portal"), []string{"waiting on rfx-one"}, []Link{LinkTo("rfx-one")}}, ""},
		{"rfx-one", &RFX{c(TypeRFX), RFXOut, RFXOpen, "email", "searched wiki", LinkTo("acme-portal")}, ""},
		{"no-prod-logs", &Constraint{c(TypeConstraint), "PII", "Jordan", source(), []string{"prod"}, []string{"dev"}, "2027-01-01"}, ""},
		{"policy-logging", &Policy{c(TypePolicy), source(), false}, "R1 policy-logging: Log failures. ^R1"},
		{"policy-logging.source", &Transcript{c(TypeTranscript), LinkTo("policy-logging"), source()}, "Source: https://wiki.acme.example/policies/logging"},
		{"exception-one", &PolicyException{c(TypePolicyException), LinkTo("policy-logging"), RuleLink("policy-logging", "R1"), ExceptionBug, ExceptionOpen, "Jordan", ""}, ""},
		{"jordan-example", &Person{c(TypePerson), "acme.example"}, ""},
		{"atlas-team", &Team{c(TypeTeam), "acme.example"}, ""},
		{"platform-engineer", &Role{c(TypeRole), "acme.example"}, ""},
		{"logging-domain", &Domain{c(TypeDomain), LinkTo("jordan-example")}, ""},
		{"branch-names", &Convention{c(TypeConvention), ConventionDraft, []string{"repo"}}, ""},
		{"2026-11-01", &Journal{c(TypeJournal), "2026-11-01", JournalDay}, ""},
		{"inbox-one", &Inbox{c(TypeInbox)}, "Text."},
	}
	seen := map[Type]bool{}
	for _, c := range cases {
		t.Run(string(c.meta.Header().Type), func(t *testing.T) {
			seen[c.meta.Header().Type] = true
			data, err := Render(Document{ID: c.id, Title: "A title", Body: c.body, Meta: c.meta})
			if err != nil {
				t.Fatal(err)
			}
			f := paramd.Parse(c.id+".md", data)
			if got := paramd.Lint(f); len(got) != 0 {
				t.Fatalf("%v\n%s", got, data)
			}
			d, err := Decode(c.id, FolderInbox, f.FrontmatterYAML(), f.Title, c.body)
			if err != nil {
				t.Fatal(err)
			}
			again, err := Render(d)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(data, again) {
				t.Fatalf("round trip differs:\n%s\n---\n%s", data, again)
			}
		})
	}
	for _, typ := range Types {
		if !seen[typ] {
			t.Errorf("no case for %s", typ)
		}
	}
}

func TestDecodeKeepsUnknownKeys(t *testing.T) {
	fm := []byte("type: inbox\ncreated: 2026-11-01\nreviewed: 2026-10-01\nnote: keep me\n")
	d, err := Decode("inbox-one", FolderInbox, fm, "T", "")
	if err != nil {
		t.Fatal(err)
	}
	data, err := Render(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"created: 2026-11-01\n", "note: keep me\n", "reviewed: 2026-10-01\n"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("missing %q in:\n%s", want, data)
		}
	}
}

func TestDecodeErrors(t *testing.T) {
	cases := []struct{ name, fm string }{
		{"unknown type", "type: nope\n"},
		{"no type", "created: 2026-11-01\n"},
		{"bad yaml", "type: [\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := Decode("x", FolderInbox, []byte(c.fm), "", ""); err == nil {
				t.Fatal("want error")
			}
		})
	}
}

func TestLinks(t *testing.T) {
	if got := RuleLink("policy-x", "R4").Target(); got != "policy-x" {
		t.Errorf("target %q", got)
	}
	if got := LinkTo("acme").Target(); got != "acme" {
		t.Errorf("target %q", got)
	}
}
