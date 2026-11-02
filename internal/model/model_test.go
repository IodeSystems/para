package model

import (
	"bytes"
	"slices"
	"strings"
	"testing"

	"github.com/iodesystems/parasolve/internal/paramd"
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
		{"area.status", strs(AreaStatuses), paramd.FieldEnum("area", "status")},
		{"pointer.kind", strs(PointerKinds), paramd.ItemFieldEnum("area", "links", "kind")},
		{"task.status", strs(TaskStatuses), paramd.FieldEnum("task", "status")},
		{"proposal.status", strs(ProposalStatuses), paramd.FieldEnum("proposal", "status")},
		{"rfx.direction", strs(RFXDirections), paramd.FieldEnum("rfx", "direction")},
		{"rfx.status", strs(RFXStatuses), paramd.FieldEnum("rfx", "status")},
		{"exception.kind", strs(ExceptionKinds), paramd.FieldEnum("policy_exception", "kind")},
		{"exception.status", strs(ExceptionStatuses), paramd.FieldEnum("policy_exception", "status")},
		{"convention.status", strs(ConventionStatuses), paramd.FieldEnum("convention", "status")},
		{"journal.scope", strs(JournalScopes), paramd.FieldEnum("journal", "scope")},
		{"group.kind", strs(GroupKinds), paramd.FieldEnum("group", "kind")},
		{"incident.severity", strs(IncidentSeverities), paramd.FieldEnum("incident", "severity")},
		{"incident.status", strs(IncidentStatuses), paramd.FieldEnum("incident", "status")},
		{"question.status", strs(QuestionStatuses), paramd.FieldEnum("question", "status")},
		{"question.kind", strs(QuestionKinds), paramd.FieldEnum("question", "kind")},
		{"request.status", strs(RequestStatuses), paramd.FieldEnum("request", "status")},
		{"interaction.medium", strs(Mediums), paramd.FieldEnum("interaction", "medium")},
		{"interaction.direction", strs(InteractionDirections), paramd.FieldEnum("interaction", "direction")},
		{"interaction.visibility", strs(Visibilities), paramd.FieldEnum("interaction", "visibility")},
		{"contact.method", strs(Mediums), paramd.ItemFieldEnum("person", "contacts", "method")},
		{"target.medium", strs(Mediums), paramd.ItemFieldEnum("rfx", "targets", "medium")},
		{"target.state", strs(TargetStates), paramd.ItemFieldEnum("rfx", "targets", "state")},
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
		{"acme-portal", &Project{Common: c(TypeProject), Status: ProjectActive, Owner: "Jordan Example", Area: LinkTo("health"),
			Links: []Pointer{{Label: "Plan", Kind: PointerPlan, Target: "/srv/plans/portal.md"}}}, ""},
		{"note-one", &Note{Common: c(TypeNote), Area: LinkTo("health"), Project: LinkTo("acme-portal")}, "Reference text."},
		{"health", &Area{Common: c(TypeArea), Status: AreaActive, Links: []Pointer{
			{Label: "Runbook", Kind: PointerRunbook, Target: "~/doc/runbook.md"}, {Label: "Repo", Kind: PointerRepo, Target: "https://git.acme.example/atlas"}}}, "Standard."},
		{"task-one", &Task{Common: c(TypeTask), Status: TaskBlocked, Project: LinkTo("acme-portal"), BlockedBy: []string{"waiting on rfx-one"}, DependsOn: []Link{LinkTo("rfx-one")}}, ""},
		{"task-area", &Task{Common: c(TypeTask), Status: TaskTodo, Area: LinkTo("health")}, ""},
		{"incident-one", &Incident{Common: c(TypeIncident), Severity: Sev2, Status: IncidentResolved, Started: "2026-11-01T09:00:00Z", Ended: "2026-11-01T10:30:00Z",
			Area: LinkTo("health"), Links: []Pointer{{Label: "Dashboard", Kind: PointerDashboard, Target: "https://grafana.acme.example/d/atlas"}}}, "## Timeline\n\n- 2026-11-01T09:00:00Z opened"},
		{"question-one", &Question{Common: c(TypeQuestion), Status: QuestionAnswered, Kind: KindDecision, Scope: LinkTo("acme-portal"),
			Options: []string{"/etc/atlas", "/opt/atlas"}, Recommended: "/etc/atlas", RFX: LinkTo("rfx-one"), Choice: "/etc/atlas",
			AnsweredBy: "user", Answered: "2026-11-01"}, "Which path?\n\n## Answer\n\n- Choice: /etc/atlas"},
		{"question-root", &Question{Common: c(TypeQuestion), Status: QuestionOpen, Scope: ScopeRoot}, ""},
		{"task-follow", &Task{Common: c(TypeTask), Status: TaskTodo, Area: LinkTo("health"), Incident: LinkTo("incident-one")}, ""},
		{"task-recur", &Task{Common: c(TypeTask), Status: TaskTodo, Area: LinkTo("health"), Due: "2026-11-08", Recur: "every 3 days", Assignee: "agent:scribe", Previous: LinkTo("task-area")}, ""},
		{"rfx-one", &RFX{Common: c(TypeRFX), Direction: RFXOut, Status: RFXOpen, Route: "email", SelfServeCheck: "searched wiki", Project: LinkTo("acme-portal")}, ""},
		{"rfx-two", &RFX{Common: c(TypeRFX), Direction: RFXIn, Status: RFXOpen, Route: "domain-logging", SelfServeCheck: "n/a", Targets: []Target{
			{To: LinkTo("jordan-example"), Medium: MediumSlack, Value: "@jordan", State: TargetSent, Ref: "https://acme.example.slack.com/archives/C0/p1", Sent: "2026-11-01"},
			{To: LinkTo("atlas-group"), Medium: MediumEmail, State: TargetDrafted},
			{To: LinkTo("atlas-group"), Medium: MediumMeeting, State: TargetScheduled}},
			Blocks: []Link{LinkTo("task-one")}, DependsOn: []Link{LinkTo("rfx-one")}, NextNudge: "2026-11-08", Playbook: LinkTo("branch-names")}, ""},
		{"meeting-one", &Meeting{Common: c(TypeMeeting), Status: MeetingScheduled, Start: "2026-11-04T10:00:00-05:00", End: "2026-11-04T10:30:00-05:00",
			With: []Link{LinkTo("jordan-example")}, RFX: []Link{LinkTo("rfx-one")}}, "## Questions\n\n- [[rfx-one]]\n\n## Notes"},
		{"meeting-ask", &Meeting{Common: c(TypeMeeting), Status: MeetingRequested, With: []Link{LinkTo("jordan-example")}}, ""},
		{"no-prod-logs", &Constraint{c(TypeConstraint), "PII", "Jordan", source(), []string{"prod"}, []string{"dev"}, "2027-01-01"}, ""},
		{"policy-logging", &Policy{c(TypePolicy), source(), false, ""}, "R1 policy-logging: Log failures. ^R1"},
		{"policy-logging.source", &Transcript{c(TypeTranscript), LinkTo("policy-logging"), source()}, "Source: https://wiki.acme.example/policies/logging"},
		{"exception-one", &PolicyException{c(TypePolicyException), LinkTo("policy-logging"), RuleLink("policy-logging", "R1"), ExceptionBug, ExceptionOpen, "Jordan", "", ""}, ""},
		{"jordan-example", &Person{Common: c(TypePerson), Org: "acme.example", Role: LinkTo("platform-engineer"), Team: LinkTo("atlas-team"),
			Aliases:  []string{"Jo"},
			Contacts: []Contact{{Method: MediumSlack, Value: "@jordan", Label: "day to day", Preferred: true}, {Method: MediumEmail, Value: "jordan@acme.example"}},
			Groups:   []Link{LinkTo("atlas-group")}}, "## Notes\n\n- 2026-11-01: Prefers Slack."},
		{"atlas-group", &Group{c(TypeGroup), "acme.example", GroupChannel}, ""},
		{"interaction-one", &Interaction{Common: c(TypeInteraction), Date: "2026-11-01", With: []Link{LinkTo("jordan-example")}, Medium: MediumSlack,
			Direction: InteractionObserved, Visibility: VisibilityPublic, URL: "https://acme.example.slack.com/archives/C0/p1"}, "Agreed to move the review."},
		{"atlas-team", &Team{c(TypeTeam), "acme.example"}, ""},
		{"proposal-one", &Proposal{Common: c(TypeProposal), Source: LinkTo("note-one"), Status: ProposalOpen, Revision: 2, Supersedes: LinkTo("proposal-zero"), ProposedBy: "agent"}, "Text."},
		{"platform-engineer", &Role{c(TypeRole), "acme.example"}, ""},
		{"logging-domain", &Domain{c(TypeDomain), LinkTo("jordan-example")}, ""},
		{"branch-names", &Convention{c(TypeConvention), ConventionDraft, []string{"repo"}}, ""},
		{"2026-11-01", &Journal{c(TypeJournal), "2026-11-01", JournalDay}, ""},
		{"inbox-one", &Inbox{c(TypeInbox)}, "Text."},
		{"request-one", &Request{Common: c(TypeRequest), Status: RequestClaimed, Runner: "inst-a", Queued: "2026-11-01T09:00:00.5Z",
			Claimed: "agent", Lease: "2026-11-01T09:30:00Z", Links: []Link{LinkTo("acme-portal")}, Results: []Link{LinkTo("acme-portal")}}, "Summarize.\n\n## Result\n\nDone."},
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

func TestRenderOntoPreservesFrontmatter(t *testing.T) {
	const old = `# hand-edited note
owner: jordan   # who runs it
status: active
type: project
tags: [b, a]
note: keep me
created: "2026-11-01"
`
	cases := []struct {
		name   string
		mutate func(d *Document)
		want   string
	}{
		{"no change", func(d *Document) {}, `---
# hand-edited note
owner: jordan # who runs it
status: active
type: project
tags: [b, a]
note: keep me
created: "2026-11-01"
---

# T
`},
		{"one key", func(d *Document) { d.Meta.(*Project).Status = "done" }, `---
# hand-edited note
owner: jordan # who runs it
status: done
type: project
tags: [b, a]
note: keep me
created: "2026-11-01"
---

# T
`},
		{"changed keeps line comment", func(d *Document) { d.Meta.(*Project).Owner = "sam" }, `---
# hand-edited note
owner: sam # who runs it
status: active
type: project
tags: [b, a]
note: keep me
created: "2026-11-01"
---

# T
`},
		{"new key appended", func(d *Document) { d.Meta.Header().Updated = "2026-11-02" }, `---
# hand-edited note
owner: jordan # who runs it
status: active
type: project
tags: [b, a]
note: keep me
created: "2026-11-01"
updated: 2026-11-02
---

# T
`},
		{"removed key dropped", func(d *Document) { d.Meta.Header().Tags = nil }, `---
# hand-edited note
owner: jordan # who runs it
status: active
type: project
note: keep me
created: "2026-11-01"
---

# T
`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			parsed, err := Decode("p", FolderProjects, []byte(old), "T", "")
			if err != nil {
				t.Fatal(err)
			}
			c.mutate(&parsed)
			got, err := RenderOnto(parsed, []byte(old))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != c.want {
				t.Fatalf("got:\n%s\nwant:\n%s", got, c.want)
			}
		})
	}
}

func TestRenderOntoIgnoresUnusableOld(t *testing.T) {
	d := Document{Title: "T", Meta: &Inbox{Common: Common{Type: TypeInbox, Created: "2026-11-01"}}}
	want, err := Render(d)
	if err != nil {
		t.Fatal(err)
	}
	for _, old := range []string{"", "- a\n- b\n", ":\n  : [", "plain"} {
		got, err := RenderOnto(d, []byte(old))
		if err != nil || string(got) != string(want) {
			t.Errorf("old %q: %v\n%s", old, err, got)
		}
	}
}
