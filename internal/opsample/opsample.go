// Package opsample holds a valid Input for each op and the documents those
// inputs name. The parity tests of every surface share it. Data is synthetic.
package opsample

import (
	"context"
	"fmt"
	"strings"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/store"
)

// Inputs holds a valid Input for each op that rejects the empty Input.
// An op added by another stream is called with {}; if that fails, a parity
// test fails and names the op: add its entry here.
var Inputs = map[string]string{
	"search":                    `{"query":"sample"}`,
	"area_add":                  `{"title":"Health"}`,
	"area_show":                 `{"id":"area-sample"}`,
	"note_add":                  `{"title":"Recipe","area":"area-sample"}`,
	"note_show":                 `{"id":"note-sample"}`,
	"constraint_add":            `{"says":"No prod access","why":"Audit scope","by":"Jordan Example","source":"https://wiki.acme.example/access","review_by":"2027-01-01"}`,
	"constraint_show":           `{"id":"constraint-sample"}`,
	"convention_add":            `{"title":"Branch names"}`,
	"convention_show":           `{"id":"convention-sample"}`,
	"copy":                      `{"id":"policy-sample","as":"slack"}`,
	"exception_add":             `{"policy":"policy-sample","kind":"bug","by":"Jordan Example"}`,
	"exception_show":            `{"id":"exception-sample"}`,
	"export":                    `{"id":"policy-sample","as":"slack","to":"ssh:desk.example:/tmp/sample.txt"}`,
	"inbox_add":                 `{"text":"buy milk\nand eggs"}`,
	"ingest":                    `{"url":"https://wiki.acme.example/policies/sample","text":"Sample policy text."}`,
	"inbox_show":                `{"id":"inbox-sample"}`,
	"inbox_triage":              `{"id":"inbox-sample","to":"task","project":"project-sample"}`,
	"interaction_add":           `{"summary":"Agreed to review on Friday","with":["person-sample"],"medium":"slack","direction":"in","date":"2026-11-01","url":"https://acme.example/m/1"}`,
	"interaction_show":          `{"id":"interaction-sample"}`,
	"org_domain_add":            `{"name":"Billing","owner":"role-sample"}`,
	"org_domain_show":           `{"id":"domain-sample"}`,
	"org_group_add":             `{"name":"Atlas","org":"acme","kind":"team"}`,
	"org_group_show":            `{"id":"group-sample"}`,
	"org_person_add":            `{"name":"Riley Example","org":"acme","aliases":["Ri"]}`,
	"org_person_alias_add":      `{"id":"person-sample","alias":"Ri"}`,
	"org_person_alias_remove":   `{"id":"person-sample","alias":"Sam"}`,
	"org_person_contact_add":    `{"id":"person-sample","method":"slack","value":"@sample","preferred":true}`,
	"org_person_contact_remove": `{"id":"person-sample","method":"email","value":"sample@acme.example"}`,
	"org_person_group_add":      `{"id":"person-sample","group":"group-sample"}`,
	"org_person_group_remove":   `{"id":"person-sample","group":"group-sample"}`,
	"org_person_note":           `{"id":"person-sample","text":"Prefers email for records.","date":"2026-11-01"}`,
	"org_person_show":           `{"id":"person-sample"}`,
	"org_role_add":              `{"name":"Auditor","org":"acme"}`,
	"org_role_show":             `{"id":"role-sample"}`,
	"org_team_add":              `{"name":"Platform","org":"acme"}`,
	"org_team_show":             `{"id":"team-sample"}`,
	"policy_refresh":            `{"id":"policy-sample","text":"Sample policy text."}`,
	"policy_review":             `{"id":"policy-sample","by":"Jordan Example"}`,
	"policy_show":               `{"id":"policy-sample"}`,
	"project_add":               `{"title":"Portal","owner":"Jordan Example"}`,
	"project_close":             `{"id":"project-sample","force":true}`,
	"project_show":              `{"id":"project-sample"}`,
	"rfx_add":                   `{"direction":"out","ask":"Can feature X launch?","route":"domain-sample","self_serve":"policy read"}`,
	"rfx_close":                 `{"id":"rfx-sample"}`,
	"rfx_draft":                 `{"id":"rfx-sample","text":"Hi, can we launch X?"}`,
	"rfx_reply":                 `{"id":"rfx-sample","from":"person-sample","outcome":"no","text":"Not now.","why":"Audit freeze","url":"https://acme.example/m/9"}`,
	"rfx_show":                  `{"id":"rfx-sample"}`,
	"rfx_status":                `{"id":"rfx-sample","status":"answered"}`,
	"rfx_target_add":            `{"id":"rfx-sample","to":"person-sample","medium":"slack"}`,
	"rfx_target_state":          `{"id":"rfx-sample","to":"person-sample","state":"sent","date":"2026-11-01"}`,
	"request_add":               `{"ask":"Summarize the sample project","links":["project-sample"]}`,
	"request_cancel":            `{"id":"request-sample"}`,
	"request_done":              `{"id":"request-sample","result":"Summary written.","links":["project-sample"]}`,
	"request_fail":              `{"id":"request-sample","reason":"No access."}`,
	"request_note":              `{"id":"request-sample","text":"Read the project."}`,
	"request_show":              `{"id":"request-sample"}`,
	"route":                     `{"query":"domain-sample"}`,
	"rule_add":                  `{"policy":"policy-sample","text":"Services log every failure."}`,
	"task_add":                  `{"project":"project-sample","title":"Write parser"}`,
	"task_block":                `{"id":"task-sample","reason":"waiting on legal"}`,
	"task_done":                 `{"id":"task-sample"}`,
	"task_show":                 `{"id":"task-sample"}`,
	"task_start":                `{"id":"task-sample"}`,
	"who":                       `{"query":"Sample person"}`,
}

func source() model.Source {
	return model.Source{URL: "https://wiki.acme.example/policies/sample", Retrieved: "2026-11-01T09:00:00Z",
		Hash: "sha256:" + strings.Repeat("ab", 32), Fidelity: model.FidelityExact}
}

// Seed creates the documents that Inputs name.
func Seed(ctx context.Context, s store.Store) error {
	for _, d := range []model.Document{
		{ID: "project-sample", Folder: model.FolderProjects, Title: "Sample project",
			Meta: &model.Project{Common: model.Common{Type: model.TypeProject}, Status: model.ProjectActive, Owner: "Jordan Example"}},
		{ID: "area-sample", Folder: model.FolderAreas, Title: "Sample area",
			Meta: &model.Area{Common: model.Common{Type: model.TypeArea}, Status: model.AreaActive}},
		{ID: "note-sample", Folder: model.FolderResources, Title: "Sample note", Body: "Sample text.",
			Meta: &model.Note{Common: model.Common{Type: model.TypeNote}, Area: model.LinkTo("area-sample")}},
		{ID: "task-sample", Folder: model.FolderProjects, Title: "Sample task",
			Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskTodo, Project: model.LinkTo("project-sample")}},
		{ID: "inbox-sample", Folder: model.FolderInbox, Title: "Sample capture",
			Meta: &model.Inbox{Common: model.Common{Type: model.TypeInbox}}},
		{ID: "role-sample", Folder: model.FolderOrg, Title: "Sample role",
			Meta: &model.Role{Common: model.Common{Type: model.TypeRole}, Org: "acme"}},
		{ID: "team-sample", Folder: model.FolderOrg, Title: "Sample team",
			Meta: &model.Team{Common: model.Common{Type: model.TypeTeam}, Org: "acme"}},
		{ID: "person-sample", Folder: model.FolderOrg, Title: "Sample person",
			Meta: &model.Person{Common: model.Common{Type: model.TypePerson}, Org: "acme", Aliases: []string{"Sam"},
				Contacts: []model.Contact{{Method: model.MediumEmail, Value: "sample@acme.example", Preferred: true}},
				Groups:   []model.Link{model.LinkTo("group-sample")}}},
		{ID: "group-sample", Folder: model.FolderOrg, Title: "Sample group",
			Meta: &model.Group{Common: model.Common{Type: model.TypeGroup}, Org: "acme", Kind: model.GroupTeam}},
		{ID: "interaction-sample", Folder: model.FolderInteractions, Title: "Sample interaction",
			Meta: &model.Interaction{Common: model.Common{Type: model.TypeInteraction}, Date: "2026-11-01", With: []model.Link{model.LinkTo("person-sample")},
				Medium: model.MediumSlack, Direction: model.InteractionIn, Visibility: model.VisibilityPrivate}},
		{ID: "request-sample", Folder: model.FolderRequests, Title: "Sample request",
			Meta: &model.Request{Common: model.Common{Type: model.TypeRequest}, Status: model.RequestClaimed, Runner: "install-test",
				Queued: "2026-11-01T09:00:00Z", Claimed: "agent", Lease: "2099-01-01T00:00:00Z"}},
		{ID: "request-queued", Folder: model.FolderRequests, Title: "Queued sample request",
			Meta: &model.Request{Common: model.Common{Type: model.TypeRequest}, Status: model.RequestQueued, Runner: "install-test",
				Queued: "2026-11-01T09:00:00Z"}},
		{ID: "domain-sample", Folder: model.FolderOrg, Title: "Sample domain",
			Meta: &model.Domain{Common: model.Common{Type: model.TypeDomain}, Owner: model.LinkTo("role-sample")}},
		{ID: "rfx-sample", Folder: model.FolderRFX, Title: "Sample request",
			Meta: &model.RFX{Common: model.Common{Type: model.TypeRFX}, Direction: model.RFXOut, Status: model.RFXOpen, Route: "domain-sample", SelfServeCheck: "policy read",
				Targets: []model.Target{{To: model.LinkTo("person-sample"), Medium: model.MediumEmail, Value: "sample@acme.example", State: model.TargetDrafted}}}},
		{ID: "policy-sample", Folder: model.FolderPolicies, Title: "Sample policy",
			Meta: &model.Policy{Common: model.Common{Type: model.TypePolicy}, Source: source()}},
		{ID: "policy-sample.source", Folder: model.FolderPolicies, Title: "Sample policy (transcript)",
			Body: "Source: https://wiki.acme.example/policies/sample\n\nSample policy text.",
			Meta: &model.Transcript{Common: model.Common{Type: model.TypeTranscript}, Of: model.LinkTo("policy-sample"), Source: source()}},
		{ID: "exception-sample", Folder: model.FolderPolicies, Title: "Sample exception",
			Meta: &model.PolicyException{Common: model.Common{Type: model.TypePolicyException}, Policy: model.LinkTo("policy-sample"),
				Kind: model.ExceptionBug, Status: model.ExceptionOpen, By: "Jordan Example"}},
		{ID: "constraint-sample", Folder: model.FolderConstraints, Title: "Sample no",
			Meta: &model.Constraint{Common: model.Common{Type: model.TypeConstraint}, Why: "w", By: "Jordan Example",
				Source: model.Source{URL: "https://wiki.acme.example/a", Retrieved: "2026-11-01", Fidelity: model.FidelityNone}, ReviewBy: "2027-01-01"}},
		{ID: "convention-sample", Folder: model.FolderConventions, Title: "Sample convention",
			Meta: &model.Convention{Common: model.Common{Type: model.TypeConvention}, Status: model.ConventionDraft}},
	} {
		if _, err := s.Create(ctx, d); err != nil {
			return fmt.Errorf("opsample: seed %s: %w", d.ID, err)
		}
	}
	return nil
}
