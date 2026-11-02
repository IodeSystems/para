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
	"area_link_add":             `{"id":"area-sample","label":"Repo","kind":"repo","target":"~/src/sample"}`,
	"area_link_remove":          `{"id":"area-sample","label":"Runbook"}`,
	"project_link_add":          `{"id":"project-sample","label":"Plan","kind":"plan","target":"~/doc/plan.md"}`,
	"project_link_remove":       `{"id":"project-sample","label":"Docs"}`,
	"where":                     `{"query":"sample"}`,
	"calendar_export":           `{"days":30,"to":"ssh:desk.example:/tmp/sample.ics"}`,
	"meeting_add":               `{"title":"Launch sync","with":["person-sample"],"rfx":["rfx-sample"],"start":"2026-11-04T10:00:00-05:00","end":"2026-11-04T10:30:00-05:00"}`,
	"meeting_show":              `{"id":"meeting-sample"}`,
	"meeting_schedule":          `{"id":"meeting-sample","start":"2026-11-05T09:00:00+01:00","end":"2026-11-05T09:30:00+01:00"}`,
	"meeting_hold":              `{"id":"meeting-sample","notes":"Agreed to ship in January.","replies":[{"id":"rfx-sample","from":"person-sample","outcome":"answered","text":"January works."}],"answers":[{"id":"question-sample","text":"Window is 2026-12-05.","sources":[{"ref":"note-sample","quote":"Sample text."}]}],"actions":[{"title":"Send the plan","assignee":"person-sample","project":"project-sample","done_when":"The plan is in the shared folder."}]}`,
	"meeting_cancel":            `{"id":"meeting-sample","reason":"Not needed."}`,
	"note_edit":                 `{"id":"note-sample","title":"Sample note, revised"}`,
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
	"incident_add":              `{"title":"Cache outage","severity":"sev2","area":"area-sample"}`,
	"incident_show":             `{"id":"incident-sample"}`,
	"incident_note":             `{"id":"incident-sample","text":"Failed over to the replica."}`,
	"incident_status":           `{"id":"incident-sample","status":"mitigated"}`,
	"incident_link_add":         `{"id":"incident-sample","label":"Dashboard","kind":"dashboard","target":"https://grafana.acme.example/d/sample"}`,
	"incident_link_remove":      `{"id":"incident-sample","label":"Runbook"}`,
	"journal_show":              `{"id":"2026-11-01"}`,
	"journal_summary":           `{"id":"2026-11-01","text":"Moved the sample project."}`,
	"inbox_add":                 `{"text":"buy milk\nand eggs"}`,
	"ingest":                    `{"url":"https://wiki.acme.example/policies/sample","text":"Sample policy text."}`,
	"inbox_show":                `{"id":"inbox-sample"}`,
	"inbox_triage":              `{"id":"inbox-sample","to":"task","project":"project-sample","done_when":"The capture is handled."}`,
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
	"project_add":               `{"title":"Portal","owner":"Jordan Example","done_when":"The portal serves production traffic."}`,
	"project_close":             `{"id":"project-sample","force":true}`,
	"project_show":              `{"id":"project-sample"}`,
	"question_add":              `{"text":"Which config path does prod use?","project":"project-sample","kind":"decision","context":"The installer writes one of these.","option_details":[{"name":"/etc/sample","pros":[{"text":"Matches the runbook.","source":"note-sample"}],"cost":{"text":"One move.","source":"no source found"},"confidence":40,"confidence_basis":"One note.","recommendation":70,"examples":[{"kind":"success","text":"Atlas uses it.","source":"note-sample"}]},{"name":"/opt/sample","confidence":20,"confidence_basis":"No record.","recommendation":30}]}`,
	"question_show":             `{"id":"question-sample"}`,
	"question_answer":           `{"id":"question-sample","text":"Window is 2026-12-05.","why":"Agreed in review.","sources":[{"ref":"note-sample","quote":"Sample text."}]}`,
	"question_accept":           `{"id":"question-proposed","text":"Window is 2026-12-06."}`,
	"question_reject":           `{"id":"question-proposed","reason":"Not what the runbook says."}`,
	"project_edit":              `{"id":"project-sample","done_when":"Every sample check passes."}`,
	"task_edit":                 `{"id":"task-sample","done_when":"The sample is checked."}`,
	"question_edit":             `{"id":"question-sample","title":"Sample question, edited"}`,
	"question_depends_add":      `{"id":"question-sample","depends_on":["task-sample"]}`,
	"question_depends_remove":   `{"id":"question-waiting","depends_on":["task-sample"]}`,
	"defer":                     `{"ids":["task-sample"],"until":"2027-01-15","reason":"After the audit."}`,
	"undefer":                   `{"ids":["task-sample"]}`,
	"health_dismiss":            `{"id":"task-sample","kind":"task_no_done_when","reason":"The title says it all."}`,
	"health_undismiss":          `{"id":"task-sample","kind":"task_no_done_when"}`,
	"depends_cut":               `{"id":"task-other","dep":"task-sample","reason":"The sample is not needed for this."}`,
	"investigate":               `{"id":"question-sample","hint":"check the wiki"}`,
	"question_drop":             `{"id":"question-sample","reason":"No longer needed."}`,
	"question_ask":              `{"id":"question-sample","to":"person-sample","via":"slack","route":"domain-sample"}`,
	"rfx_add":                   `{"direction":"out","ask":"Can feature X launch?","route":"domain-sample","self_serve":"policy read"}`,
	"rfx_close":                 `{"id":"rfx-sample"}`,
	"rfx_draft":                 `{"id":"rfx-sample","text":"Hi, can we launch X?"}`,
	"rfx_reply":                 `{"id":"rfx-sample","from":"person-sample","outcome":"no","text":"Not now.","why":"Audit freeze","url":"https://acme.example/m/9"}`,
	"rfx_show":                  `{"id":"rfx-sample"}`,
	"rfx_status":                `{"id":"rfx-sample","status":"answered"}`,
	"rfx_target_add":            `{"id":"rfx-sample","to":"person-sample","medium":"slack"}`,
	"rfx_target_state":          `{"id":"rfx-sample","to":"person-sample","state":"sent","date":"2026-11-01"}`,
	"request_add":               `{"title":"Summarize the sample goal","ask":"Summarize the sample project","links":["project-sample"]}`,
	"request_cancel":            `{"id":"request-sample"}`,
	"request_accept":            `{"id":"request-proposed"}`,
	"request_done":              `{"id":"request-sample","resolution":"done","result":"Summary written.","links":["project-sample"],"followed":["policy-sample"]}`,
	"request_fail":              `{"id":"request-sample","reason":"No access."}`,
	"request_note":              `{"id":"request-sample","text":"Read the project."}`,
	"request_show":              `{"id":"request-sample"}`,
	"route":                     `{"query":"domain-sample"}`,
	"rule_add":                  `{"policy":"policy-sample","text":"Services log every failure."}`,
	"task_add":                  `{"project":"project-sample","title":"Write parser","done_when":"The parser passes its tests."}`,
	"task_to_question":          `{"id":"task-sample","options":["A","B"]}`,
	"task_assign":               `{"id":"task-sample","assignee":"person-sample"}`,
	"task_block":                `{"id":"task-sample","reason":"waiting on legal"}`,
	"task_due":                  `{"id":"task-sample","date":"2026-12-01","recur":"monthly"}`,
	"task_depends_add":          `{"id":"task-sample","depends_on":["task-free"]}`,
	"task_depends_remove":       `{"id":"task-other","depends_on":["task-sample"]}`,
	"task_done":                 `{"id":"task-sample"}`,
	"task_show":                 `{"id":"task-sample"}`,
	"task_start":                `{"id":"task-sample"}`,
	"who":                       `{"query":"Sample person"}`,
	"proposal_add": `{"title":"Sample proposal","tldr":"Add a task and tell Sam.","context":"Sam asked in the sample note.","source":"note-sample","items":[` +
		`{"kind":"para","op":"task_add","input":{"project":"project-sample","title":"From a proposal","done_when":"It exists."},"how":"Adds a task.","impact":"The sample project gets one more task.","reversible":"yes","confidence":70,"confidence_basis":"Sam said so.","pros":[{"text":"Tracks the work.","source":"note-sample"}],"evidence":{"quote":"Sample text.","locator":"line 1"}},` +
		`{"kind":"message","subtype":"email","people":["sample@acme.example"],"subject":"Sample","body":"Hi Sam.","reversible":"no","bundle":"b1","evidence":{"quote":"Sample text.","locator":"line 1"}}]}`,
	"receipt_show":      `{"id":"receipt-sample"}`,
	"receipt_check":     `{"id":"receipt-sample","verdict":"verified"}`,
	"receipt_revert":    `{"id":"receipt-sample"}`,
	"proposal_executed": `{"id":"proposal-decided","item":"i1","result":"done","url":"https://acme.example/m/2"}`,
	"align_scope":       `{"scope":"project-sample"}`,
	"align_record":      `{"scope":"project-sample","checked":4,"matched":3,"differ":1,"unreachable":["jira"],"proposal":"proposal-sample"}`,
	"gate_check":        `{"target":"` + sampleTarget + `","body":"` + sampleDraft + `"}`,
	"similar":           `{"type":"task","title":"Sample task"}`,
	"request_respond":   `{"id":"request-finished","kind":"change","text":"Use the shorter summary."}`,
	"request_changes":   `{"id":"request-finished"}`,
	"proposal_show":     `{"id":"proposal-sample"}`,
	"proposal_diff":     `{"id":"proposal-sample"}`,
	"proposal_wait":     `{"id":"proposal-decided"}`,
	"proposal_approved": `{"hash":"` + model.ExternalHash(sampleTarget, sampleDraft) + `"}`,
	"proposal_revise":   `{"id":"proposal-sample","answers":[{"annotation":"a1","outcome":"applied"}]}`,
	"proposal_approve":  `{"id":"proposal-sample","items":["i1"]}`,
	"proposal_deny":     `{"id":"proposal-sample","item":"i1","reason":"Not now."}`,
	"proposal_hold":     `{"id":"proposal-sample","item":"i1","kind":"request","text":"Check the budget first"}`,
	"proposal_request":  `{"id":"proposal-sample","item":"i2","intent":"change","note":"Use the full name."}`,
	"proposal_edit":     `{"id":"proposal-sample","item":"i2","draft":"Hi Sample."}`,
}

const (
	sampleTarget = "sample@acme.example"
	sampleDraft  = "Hi Sam."
)

func sampleProposals() ([]model.Document, error) {
	ev := model.Evidence{Quote: "Sample text.", Locator: "line 1"}
	open, err := model.RenderProposalBody(model.ProposalContent{
		Items: []model.ProposalItem{
			{ID: "i1", Kind: model.ItemPara, Op: "task_add", Input: map[string]any{"project": "project-sample", "title": "From a proposal", "done_when": "It exists."}, Evidence: ev, State: model.ItemPending},
			{ID: "i2", Kind: model.ItemAction, Subtype: "email", Target: sampleTarget, Body: sampleDraft, Evidence: ev, State: model.ItemPending},
			{ID: "i3", Kind: model.ItemMessage, Subtype: "slack", Channel: "#sample", Body: "Sample note.", Evidence: ev, State: model.ItemPending,
				Thread: []model.Turn{{ID: "a1", Kind: model.TurnClarify, Quote: "Sample note", Text: "Reword this.", By: "user", Revision: 1, State: model.TurnOpen}}},
		},
	})
	if err != nil {
		return nil, err
	}
	closed, err := model.RenderProposalBody(model.ProposalContent{
		Items: []model.ProposalItem{{ID: "i1", Kind: model.ItemAction, Subtype: "email", Target: sampleTarget, Body: sampleDraft,
			Evidence: ev, State: model.ItemApproved, ApprovedHash: model.ExternalHash(sampleTarget, sampleDraft)}},
	})
	if err != nil {
		return nil, err
	}
	meta := func(s model.ProposalStatus) *model.Proposal {
		return &model.Proposal{Common: model.Common{Type: model.TypeProposal}, Source: model.LinkTo("note-sample"), Status: s, Revision: 1, ProposedBy: "claude-agent"}
	}
	return []model.Document{
		{ID: "proposal-sample", Folder: model.FolderProposals, Title: "Sample proposal", Body: open, Meta: meta(model.ProposalOpen)},
		{ID: "proposal-decided", Folder: model.FolderProposals, Title: "Decided proposal", Body: closed, Meta: meta(model.ProposalClosed)},
	}, nil
}

// sampleReceipt records the creation of note-receipted.
func sampleReceipt() (model.Document, model.Document, error) {
	note := model.Document{ID: "note-receipted", Folder: model.FolderResources, Title: "Receipted note", Body: "Later text.",
		Meta: &model.Note{Common: model.Common{Type: model.TypeNote, Created: "2026-11-01"}}}
	after, err := model.Render(note)
	if err != nil {
		return note, model.Document{}, err
	}
	body, err := model.RenderReceiptBody(model.ReceiptContent{Folder: model.FolderResources, After: strings.TrimRight(string(after), "\n")})
	if err != nil {
		return note, model.Document{}, err
	}
	return note, model.Document{ID: "receipt-sample", Folder: model.FolderReceipts, Title: "Note add: Receipted note", Body: body,
		Meta: &model.Receipt{Common: model.Common{Type: model.TypeReceipt}, Actor: "agent", Op: "note_add", Doc: "note-receipted",
			Why: "The owner asked for it.", Sources: []string{"note-sample"}, Verify: "Open the note.", Confidence: model.ConfidenceMedium, Check: model.ReceiptUnchecked}}, nil
}

func source() model.Source {
	return model.Source{URL: "https://wiki.acme.example/policies/sample", Retrieved: "2026-11-01T09:00:00Z",
		Hash: "sha256:" + strings.Repeat("ab", 32), Fidelity: model.FidelityExact}
}

// Seed creates the documents that Inputs name.
func Seed(ctx context.Context, s store.Store) error {
	proposals, err := sampleProposals()
	if err != nil {
		return fmt.Errorf("opsample: seed: %w", err)
	}
	note, receipt, err := sampleReceipt()
	if err != nil {
		return fmt.Errorf("opsample: seed: %w", err)
	}
	for _, d := range append([]model.Document{
		note, receipt,
		{ID: "project-sample", Folder: model.FolderProjects, Title: "Sample project",
			Meta: &model.Project{Common: model.Common{Type: model.TypeProject}, Status: model.ProjectActive, Owner: "Jordan Example",
				Links: []model.Pointer{{Label: "Docs", Kind: model.PointerDocs, Target: "https://wiki.acme.example/sample"}}}},
		{ID: "area-sample", Folder: model.FolderAreas, Title: "Sample area",
			Meta: &model.Area{Common: model.Common{Type: model.TypeArea}, Status: model.AreaActive,
				Links: []model.Pointer{{Label: "Runbook", Kind: model.PointerRunbook, Target: "~/doc/runbook.md"}}}},
		{ID: "incident-sample", Folder: model.FolderIncidents, Title: "Sample incident",
			Body: "## Timeline\n\n- 2026-11-01T09:00:00Z opened sev2",
			Meta: &model.Incident{Common: model.Common{Type: model.TypeIncident}, Severity: model.Sev2, Status: model.IncidentOpen,
				Started: "2026-11-01T09:00:00Z", Area: model.LinkTo("area-sample"),
				Links: []model.Pointer{{Label: "Runbook", Kind: model.PointerRunbook, Target: "~/doc/runbook.md"}}}},
		{ID: "question-sample", Folder: model.FolderQuestions, Title: "Sample question", Body: "What is the window?",
			Meta: &model.Question{Common: model.Common{Type: model.TypeQuestion}, Status: model.QuestionOpen, Scope: model.LinkTo("project-sample")}},
		{ID: "question-proposed", Folder: model.FolderQuestions, Title: "Proposed question", Body: "What is the window?",
			Meta: &model.Question{Common: model.Common{Type: model.TypeQuestion}, Status: model.QuestionOpen, Scope: model.LinkTo("project-sample"),
				ProposedAnswer: "Window is 2026-12-05.", ProposedBy: "claude-agent", Proposed: "2026-11-01",
				ProposedSources: []model.AnswerSource{{Ref: "note-sample", Quote: "Sample text."}}}},
		{ID: "question-waiting", Folder: model.FolderQuestions, Title: "Waiting question",
			Meta: &model.Question{Common: model.Common{Type: model.TypeQuestion}, Status: model.QuestionOpen, Scope: model.LinkTo("project-sample"),
				DependsOn: []model.Link{model.LinkTo("task-sample")}}},
		{ID: "meeting-sample", Folder: model.FolderMeetings, Title: "Sample meeting",
			Body: "## Questions\n\n- [[rfx-sample]] Sample request\n\n## Notes\n",
			Meta: &model.Meeting{Common: model.Common{Type: model.TypeMeeting}, Status: model.MeetingScheduled,
				Start: "2026-11-04T10:00:00-05:00", End: "2026-11-04T10:30:00-05:00",
				With: []model.Link{model.LinkTo("person-sample")}, RFX: []model.Link{model.LinkTo("rfx-sample")}}},
		{ID: "task-free", Folder: model.FolderProjects, Title: "Free task",
			Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskTodo, Project: model.LinkTo("project-sample")}},
		{ID: "task-other", Folder: model.FolderProjects, Title: "Other task",
			Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskTodo, Project: model.LinkTo("project-sample"),
				DependsOn: []model.Link{model.LinkTo("task-sample")}}},
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
		{ID: "request-proposed", Folder: model.FolderRequests, Title: "Proposed sample request",
			Meta: &model.Request{Common: model.Common{Type: model.TypeRequest}, Status: model.RequestProposed, Runner: "install-test",
				Queued: "2026-11-01T09:00:00Z"}},
		{ID: "request-finished", Folder: model.FolderRequests, Title: "Finished sample request", Body: "Summarize.\n\n## Result\n\ndone: Written.",
			Meta: &model.Request{Common: model.Common{Type: model.TypeRequest}, Status: model.RequestDone, Runner: "install-test",
				Queued: "2026-11-01T09:00:00Z", Resolution: model.ResolutionDone}},
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
		{ID: "2026-11-01", Folder: model.FolderJournal, Title: "Day 2026-11-01",
			Body: "%% para:begin summary %%\nevents: 0\n%% para:end summary %%\n\n%% para:begin activity %%\nNo activity.\n%% para:end activity %%",
			Meta: &model.Journal{Common: model.Common{Type: model.TypeJournal}, Date: "2026-11-01", Scope: model.JournalDay}},
		{ID: "convention-sample", Folder: model.FolderConventions, Title: "Sample convention",
			Meta: &model.Convention{Common: model.Common{Type: model.TypeConvention}, Status: model.ConventionDraft}},
	}, proposals...) {
		if _, err := s.Create(ctx, d); err != nil {
			return fmt.Errorf("opsample: seed %s: %w", d.ID, err)
		}
	}
	return nil
}
