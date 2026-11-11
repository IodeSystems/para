package ops

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/upgrade"
)

// A fold never makes data vanish from a read: each reader of teams, roles,
// domains, interactions, and incidents gives the same answer on one synthetic
// vault before and after upgrade step small-types.

type smallVault struct {
	d                      Deps
	team, role, dom, ada   string
	in, out, seen          string
	sev1, sev3, fix, other string
}

func newSmallVault(t *testing.T) smallVault {
	t.Helper()
	now := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)
	d := clockDeps(t, &now)
	if err := d.Store.SetConfig(ctx, upgrade.ConfigKey, "9"); err != nil {
		t.Fatal(err)
	}
	v := smallVault{d: d}
	createDoc(t, d, model.Document{ID: "area-ops", Folder: model.FolderAreas, Title: "Ops", Meta: &model.Area{Common: model.Common{Type: model.TypeArea}, Status: model.AreaActive}})
	v.team = must(t, OrgTeamAdd, d, OrgAddInput{Name: "Atlas", Org: "acme.example", Said: []string{"Runs the portal"}, Source: "wiki"}).ID
	v.role = must(t, OrgRoleAdd, d, OrgAddInput{Name: "Lead", Org: "acme.example"}).ID
	v.dom = must(t, OrgDomainAdd, d, OrgAddInput{Name: "Logging", Owner: v.role}).ID
	v.ada = must(t, OrgPersonAdd, d, OrgAddInput{Name: "Ada Example", Org: "acme.example", Role: v.role, Team: v.team}).ID
	v.in = must(t, InteractionAdd, d, InteractionAddInput{Summary: "Ada asked for the logs\n\nBy Friday.", With: []string{v.ada}, Medium: model.MediumSlack,
		Direction: model.InteractionIn, URL: "https://acme.example.slack.com/archives/C1/p1", Date: "2026-11-01"}).ID
	v.out = must(t, InteractionAdd, d, InteractionAddInput{Summary: "Sent Ada the logs", With: []string{v.ada}, Medium: model.MediumEmail,
		Direction: model.InteractionOut, Date: "2026-11-02"}).ID
	v.seen = must(t, InteractionAdd, d, InteractionAddInput{Summary: "Atlas demo", With: []string{v.ada}, Medium: model.MediumMeeting,
		Direction: model.InteractionObserved, Visibility: model.VisibilityPublic, Date: "2026-10-30"}).ID
	v.sev3 = must(t, IncidentAdd, d, IncidentAddInput{Title: "Slow search", Severity: model.Sev3, Started: "2026-11-05T09:00:00Z"}).ID
	v.sev1 = must(t, IncidentAdd, d, IncidentAddInput{Title: "Logs down", Severity: model.Sev1, Area: "area-ops", Started: "2026-11-06T08:00:00Z", Body: "Pages fire."}).ID
	must(t, IncidentStatus, d, IncidentStatusInput{ID: v.sev3, Status: model.IncidentMitigated, Text: "cache warmed"})
	must(t, IncidentLinkAdd, d, LinkAddInput{ID: v.sev1, Label: "Dashboard", Kind: model.PointerDashboard, Target: "https://grafana.acme.example/d/1"})
	v.fix = must(t, TaskAdd, d, TaskAddInput{Area: "area-ops", Title: "Add a log alert", Incident: v.sev1, DoneWhen: "The alert fires in staging."}).ID
	v.other = must(t, TaskAdd, d, TaskAddInput{Area: "area-ops", Title: "Rotate the keys", Due: "2026-11-06", DoneWhen: "Rotated."}).ID
	return v
}

// reads is every reader's answer, as JSON, with what a fold may change (the
// rev and folder of a document, its type in a raw doc) left out.
func (v smallVault) reads(t *testing.T) map[string]string {
	t.Helper()
	d := v.d
	out := map[string]string{}
	put := func(name string, x any) {
		b, err := json.Marshal(x)
		if err != nil {
			t.Fatal(err)
		}
		s := string(b)
		for _, k := range []string{"rev", "folder"} {
			s = dropKey(s, k)
		}
		out[name] = s
	}
	doc := func(x model.Document) []string { return []string{x.ID, x.Title, x.Body} }
	put("org_team_list", must(t, OrgTeamList, d, OrgListInput{}))
	put("org_role_list", must(t, OrgRoleList, d, OrgListInput{}))
	put("org_domain_list", must(t, OrgDomainList, d, OrgListInput{}))
	put("org_team_show", doc(must(t, OrgTeamShow, d, OrgShowInput{ID: v.team}).Doc))
	put("org_role_show", doc(must(t, OrgRoleShow, d, OrgShowInput{ID: v.role}).Doc))
	put("org_domain_show", doc(must(t, OrgDomainShow, d, OrgShowInput{ID: v.dom}).Doc))
	put("who", must(t, Who, d, WhoInput{Query: "Ada Example"}))
	r := must(t, Route, d, RouteInput{Query: "logging"})
	put("route", []any{r.Domain, r.Owner, r.People, r.Candidates})
	put("interaction_list", must(t, InteractionList, d, InteractionListInput{}))
	put("interaction_list observed", must(t, InteractionList, d, InteractionListInput{Direction: model.InteractionObserved}))
	for _, id := range []string{v.in, v.out, v.seen} {
		s := must(t, InteractionShow, d, InteractionShowInput{ID: id})
		m, _ := model.InteractionOf(s.Doc)
		put("interaction_show "+id, []any{doc(s.Doc), m.Date, m.With, m.Medium, m.Direction, m.Visibility, m.URL})
	}
	put("incident_list", must(t, IncidentList, d, IncidentListInput{All: true}))
	for _, id := range []string{v.sev1, v.sev3} {
		put("incident_show "+id, must(t, IncidentShow, d, IncidentShowInput{ID: id}))
		s := must(t, Show, d, ShowInput{ID: id})
		put("show "+id, []any{s.Status, s.Life, s.NeedsYou, s.Reason, s.Op})
	}
	put("task_list", must(t, TaskList, d, TaskListInput{}))
	put("list incident", must(t, List, d, ListInput{Type: model.TypeIncident}))
	put("order", must(t, OrderOp, d, OrderInput{}).Rows)
	put("agenda", must(t, Agenda, d, AgendaInput{Days: 7}).Items)
	return out
}

// dropKey removes "key":"..." pairs from JSON text (string values only).
func dropKey(s, key string) string {
	for {
		i := strings.Index(s, `"`+key+`":"`)
		if i < 0 {
			return s
		}
		j := strings.Index(s[i+len(key)+4:], `"`)
		end := i + len(key) + 4 + j + 1
		if end < len(s) && s[end] == ',' {
			end++
		} else if i > 0 && s[i-1] == ',' {
			i--
		}
		s = s[:i] + s[end:]
	}
}

func TestReadersSameAfterSmallTypesFold(t *testing.T) {
	v := newSmallVault(t)
	before := v.reads(t)
	for name, want := range map[string]string{
		"org_team_list": v.team, "org_role_list": v.role, "org_domain_list": v.dom, "who": `"type":"role"`, "route": v.ada,
		"interaction_list": v.seen, "interaction_list observed": v.seen, "incident_list": "mitigated", "incident_show " + v.sev1: v.fix, "incident_show " + v.sev3: "cache warmed",
		"order": v.sev1, "task_list": v.other,
	} {
		if !strings.Contains(before[name], want) {
			t.Fatalf("%s before the fold lacks %q: %s", name, want, before[name])
		}
	}
	res, err := upgrade.RunForced(ctx, v.d.Store, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.FormatVersion != upgrade.SmallTypesFormat {
		t.Fatalf("run = %+v", res)
	}
	for id, want := range map[string]model.Type{v.team: model.TypeGroup, v.dom: model.TypeGroup, v.in: model.TypeIO, v.sev1: model.TypeTask} {
		if doc, _ := v.d.Store.Get(ctx, id); doc.Meta.Header().Type != want {
			t.Fatalf("%s was not folded: %s", id, doc.Meta.Header().Type)
		}
	}
	after := v.reads(t)
	for name, b := range before {
		t.Run(name, func(t *testing.T) {
			if after[name] != b {
				t.Errorf("changed in the fold:\nbefore %s\nafter  %s", b, after[name])
			}
		})
	}
	// Incidents still come first in the value order, worst first.
	rows := must(t, OrderOp, v.d, OrderInput{}).Rows
	if len(rows) < 3 || rows[0].ID != v.sev1 || rows[0].Kind != KindIncident || rows[1].ID != v.sev3 {
		t.Fatalf("order after the fold = %+v", rows)
	}
	// The incident ops write the task form; close resolves it.
	must(t, IncidentNote, v.d, IncidentNoteInput{ID: v.sev1, Text: "found the bad config"})
	c := must(t, Close, v.d, CloseInput{ID: v.sev1, Reason: "config reverted"})
	if doc, _ := v.d.Store.Get(ctx, v.sev1); doc.Meta.(*model.Task).Status != model.TaskDone || doc.Meta.(*model.Task).Ended == "" ||
		!strings.Contains(doc.Body, "status resolved: config reverted") || c.Op != IncidentStatus.Name() {
		t.Fatalf("close of an incident task = %+v\n%s", c, doc.Body)
	}
	mustLintClean(t, v.d)
}

// At format 10 the older add ops write the new forms (what the step makes),
// below it the older types; the outputs keep their shape.
func TestSmallTypesAddOpsWriteByFormat(t *testing.T) {
	for _, format := range []string{"9", "10"} {
		t.Run(format, func(t *testing.T) {
			now := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)
			d := clockDeps(t, &now)
			if err := d.Store.SetConfig(ctx, upgrade.ConfigKey, format); err != nil {
				t.Fatal(err)
			}
			on := format == "10"
			role := must(t, OrgRoleAdd, d, OrgAddInput{Name: "Lead", Org: "acme.example"}).ID
			dom := must(t, OrgDomainAdd, d, OrgAddInput{Name: "Logs", Owner: role}).ID
			ada := must(t, OrgPersonAdd, d, OrgAddInput{Name: "Ada", Org: "acme.example", Role: role}).ID
			in := must(t, InteractionAdd, d, InteractionAddInput{Summary: "Talked", With: []string{ada}, Medium: model.MediumPhone, Direction: model.InteractionOut})
			inc := must(t, IncidentAdd, d, IncidentAddInput{Title: "Down", Severity: model.Sev2})
			task := must(t, TaskAdd, d, TaskAddInput{Kind: model.KindIncident, Title: "Also down", Severity: model.Sev1})
			want := map[string]model.Type{role: model.TypeRole, dom: model.TypeDomain, in.ID: model.TypeInteraction, inc.ID: model.TypeIncident, task.ID: model.TypeIncident}
			if on {
				want = map[string]model.Type{role: model.TypeGroup, dom: model.TypeGroup, in.ID: model.TypeIO, inc.ID: model.TypeTask, task.ID: model.TypeTask}
			}
			for id, typ := range want {
				doc, err := d.Store.Get(ctx, id)
				if err != nil || doc.Meta.Header().Type != typ {
					t.Fatalf("%s = %v %v, want %s", id, doc.Meta, err, typ)
				}
				b, _ := model.Render(doc)
				if strings.Contains(string(b), "folded_from") {
					t.Fatalf("%s says folded_from:\n%s", id, b)
				}
			}
			if on && (in.Folder != model.FolderIO || inc.Folder != model.FolderProjects) {
				t.Fatalf("folders %s %s", in.Folder, inc.Folder)
			}
			// The old add ops are internal; incident_status stays listed (it
			// sets mitigated, which no close outcome is).
			for _, name := range []string{"org_team_add", "org_domain_add", "interaction_add", "incident_add"} {
				if !IsInternal(name) {
					t.Fatalf("%s is listed", name)
				}
			}
			if IsInternal("incident_status") {
				t.Fatal("incident_status is internal")
			}
			res, err := IncidentStatus.Invoke(ctx, d, json.RawMessage(`{"id":"`+inc.ID+`","status":"resolved","text":"fixed"}`))
			if err != nil {
				t.Fatal(err)
			}
			if b, _ := json.Marshal(res); !strings.Contains(string(b), `"status":"resolved"`) {
				t.Fatalf("incident_status = %s", b)
			}
			if i, ok := mustIncident(t, d, inc.ID); !ok || i.Status != model.IncidentResolved {
				t.Fatalf("incident after the alias = %+v", i)
			}
			// At format 10 a note that links an item is a health finding (step
			// notes-placement); a note that links nothing is not.
			createDoc(t, d, model.Document{ID: "area-x", Folder: model.FolderAreas, Title: "X", Meta: &model.Area{Common: model.Common{Type: model.TypeArea}, Status: model.AreaActive}})
			linked := must(t, NoteAdd, d, NoteAddInput{Title: "Paths", Area: "area-x"}).ID
			must(t, NoteAdd, d, NoteAddInput{Title: "Stray"})
			h, err := HealthAt(ctx, d.Store, "2026-11-06")
			if err != nil {
				t.Fatal(err)
			}
			b, _ := json.Marshal(h)
			if got := strings.Count(string(b), `"kind":"`+HealthNotePlacement+`"`); got != map[bool]int{false: 0, true: 1}[on] || on && !strings.Contains(string(b), linked) {
				t.Fatalf("note_placement findings = %d:\n%s", got, b)
			}
			mustLintClean(t, d)
		})
	}
}

func mustIncident(t *testing.T, d Deps, id string) (*model.Incident, bool) {
	t.Helper()
	doc, err := d.Store.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return model.IncidentOf(doc)
}
