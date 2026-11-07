// Package opsample holds a valid Input for each op and the documents those
// inputs name. The parity tests of every surface share it. Data is synthetic.
package opsample

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// Inputs holds a valid Input for each op that rejects the empty Input.
// An op added by another stream is called with {}; if that fails, a parity
// test fails and names the op: add its entry here.
var Inputs = map[string]string{
	"search":                    `{"query":"sample"}`,
	"feedback_add":              `{"text":"The error did not name the field.","kind":"bug","tool":"search"}`,
	"tag_add":                   `{"ids":["task-sample"],"tags":["socials"]}`,
	"tag_remove":                `{"ids":["task-sample"],"tags":["socials"]}`,
	"stack_later":               `{"questions":["question-sample"],"until":"2999-01-01"}`,
	"stack_return":              `{"questions":["question-sample"]}`,
	"attachment_add":            `{"id":"task-sample","name":"photo of board.txt","data":"U2FtcGxlIHRleHQu"}`,
	"attachment_read":           `{"file":"` + SampleAttachment + `"}`,
	"agent_send":                `{"id":"task-sample","text":"Met Sam; decided to ship.","attachments":["` + SampleAttachment + `"],"done":true}`,
	"item_close":                `{"id":"task-free"}`,
	"close":                     `{"id":"task-free","reason":"Sent it."}`,
	"later":                     `{"ids":["task-sample"],"until":"2027-01-15","reason":"After the audit."}`,
	"reopen":                    `{"tag":"sample"}`,
	"send_to_agent":             `{"id":"task-sample","note":"Find who owns the review.","title":"Find the review owner"}`,
	"show":                      `{"id":"task-sample"}`,
	"list":                      `{"type":"task"}`,
	"vault_upgrade":             `{"dry_run":true}`,
	"vault_config_get":          `{"key":"align.days"}`,
	"vault_config_set":          `{"key":"align.days","value":"14"}`,
	"vault_config_unset":        `{"key":"align.days"}`,
	"area_add":                  `{"title":"Health"}`,
	"area_show":                 `{"id":"area-sample"}`,
	"annotate":                  `{"area":"area-sample","kind":"note","text":"Ask the owner before a change."}`,
	"annotation_list":           `{"expiring":30}`,
	"area_review":               `{"id":"area-sample","note":"Checked the sample."}`,
	"rules_in_effect":           `{"id":"area-sample"}`,
	"area_link_add":             `{"id":"area-sample","label":"Repo","kind":"repo","target":"~/src/sample"}`,
	"area_link_remove":          `{"id":"area-sample","label":"Runbook"}`,
	"project_link_add":          `{"id":"project-sample","label":"Plan","kind":"plan","target":"~/doc/plan.md"}`,
	"project_link_remove":       `{"id":"project-sample","label":"Docs"}`,
	"where":                     `{"query":"sample"}`,
	"calendar_export":           `{"days":30,"to":"ssh:desk.example:/tmp/sample.ics"}`,
	"meeting_add":               `{"title":"Launch sync","with":["person-sample"],"rfx":["rfx-sample"],"start":"2026-11-04T10:00:00-05:00","end":"2026-11-04T10:30:00-05:00"}`,
	"meeting_show":              `{"id":"meeting-sample"}`,
	"brief":                     `{"id":"meeting-sample"}`,
	"meeting_schedule":          `{"id":"meeting-sample","start":"2026-11-05T09:00:00+01:00","end":"2026-11-05T09:30:00+01:00"}`,
	"meeting_hold":              `{"id":"meeting-sample","notes":"Agreed to ship in January.","replies":[{"id":"rfx-sample","from":"person-sample","outcome":"answered","text":"January works."}],"answers":[{"id":"question-sample","text":"Window is 2026-12-05.","sources":[{"ref":"note-sample","quote":"Sample text."}]}],"actions":[{"title":"Send the plan","assignee":"person-sample","project":"project-sample","done_when":"The plan is in the shared folder."}]}`,
	"meeting_cancel":            `{"id":"meeting-sample","reason":"Not needed."}`,
	"meeting_attend":            `{"id":"meeting-task-sample","attend":"no","optional":true,"all_future":true}`,
	"obligation_add":            `{"title":"Send the sample report","owed_to":["person-sample"],"due":"2026-11-20","project":"project-sample"}`,
	"obligation_schedule":       `{"id":"meeting-task-sample","start":"2026-11-07T10:00:00-05:00","end":"2026-11-07T10:30:00-05:00"}`,
	"obligation_attend":         `{"id":"meeting-task-sample","attend":"yes"}`,
	"obligation_parts":          `{"id":"obligation-sample"}`,
	"meeting_rsvp":              `{"id":"meeting-task-sample","attend":"yes"}`,
	"doc_rename":                `{"id":"note-sample","new_id":"note-sample-renamed"}`,
	"doc_retype":                `{"id":"note-sample","type":"policy_exception","fields":{"kind":"established_example","status":"open","by":"unknown","policy_text":"Sample policy, in words"}}`,
	"doc_edit":                  `{"id":"person-sample","title":"Riley Example, edited","fields":{"org":"acme"}}`,
	"task_move":                 `{"id":"task-sample","area":"area-sample"}`,
	"rfx_nudge":                 `{"id":"rfx-sample","date":"2026-12-01"}`,
	"attempt_add":               `{"target":"task-sample","what":"Asked in chat","result":"No reply yet","sources":["https://acme.example/m/3"]}`,
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
	"io_add":                    `{"direction":"in","text":"buy milk\nand eggs"}`,
	"doc_track":                 `{"url":"https://wiki.acme.example/runbooks/sample","system":"confluence","text":"# Runbook\n\nStep one."}`,
	"doc_version_add":           `{"id":"doc-sample","text":"Step one.\nStep two, then three."}`,
	"doc_tracked_show":          `{"id":"doc-sample"}`,
	"doc_tracked_status":        `{"id":"doc-sample","status":"paused"}`,
	"doc_diff":                  `{"id":"doc-sample"}`,
	"doc_seen":                  `{"id":"doc-sample"}`,
	"doc_comment_add":           `{"id":"doc-sample","by":"person-sample","text":"Why two steps?","quote":"Step two."}`,
	"doc_comments":              `{"id":"doc-sample"}`,
	"doc_comment_resolve":       `{"id":"doc-sample","comment":"c1","resolution":"resolved"}`,
	"doc_comment_file":          `{"id":"doc-sample","comment":"c1","to":"question"}`,
	"source_show":               `{"id":"source-sample"}`,
	"source_add":                `{"title":"Sample chat","example":"slack"}`,
	"source_status":             `{"id":"source-sample","status":"paused"}`,
	"source_example":            `{"system":"github"}`,
	"agent_sources":             `{"sources":["source-sample"]}`,
	"sweep_record":              `{"source":"source-sample","cursor":"c-2","counts":{"in":1}}`,
	"io_show":                   `{"id":"io-sample"}`,
	"io_edit":                   `{"id":"io-sample","state":"read"}`,
	"io_file":                   `{"id":"io-sample","to":"task","project":"project-sample","done_when":"The capture is handled."}`,
	"io_drop":                   `{"id":"io-sample","reason":"Handled elsewhere."}`,
	"io_send":                   `{"id":"io-draft-sample","url":"https://acme.example/m/2","observed":true}`,
	"io_approve":                `{"id":"io-draft-sample","owner_said":"Yes, send it."}`,
	"io_send_check":             `{"id":"io-ready-sample"}`,
	"ingest":                    `{"url":"https://wiki.acme.example/policies/sample","text":"Sample policy text."}`,
	"interaction_add":           `{"summary":"Agreed to review on Friday","with":["person-sample"],"medium":"slack","direction":"in","date":"2026-11-01","url":"https://acme.example/m/1"}`,
	"interaction_show":          `{"id":"interaction-sample"}`,
	"org_domain_add":            `{"name":"Billing","owner":"role-sample"}`,
	"org_domain_show":           `{"id":"domain-sample"}`,
	"org_group_add":             `{"name":"Atlas","org":"acme","kind":"team"}`,
	"org_group_show":            `{"id":"group-sample"}`,
	"priority_set":              `{"id":"group-sample","priority":"high"}`,
	"triage_run":                `{"dry_run":true}`,
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
	"goal_signal_add":           `{"id":"project-sample","what":"Sample checks passing","value":"4 of 5","source":"note-sample","date":"2026-11-02"}`,
	"goal_rank":                 `{"id":"project-sample","rank":1}`,
	"goal_compare":              `{"ids":"project-sample"}`,
	"goal_risk_set":             `{"id":"project-sample","text":"Vendor slips a quarter","likelihood":"medium","impact":"high","owner":"person-sample","source":"owner in chat, 2026-11-04"}`,
	"assumption_set":            `{"id":"task-sample","text":"The sample API stays stable","source":"note-sample"}`,
	"grill_context":             `{"id":"project-sample"}`,
	"recheck":                   `{"scope":"project-sample"}`,
	"recheck_record":            `{"id":"constraint-sample","result":"holds","source":"https://wiki.acme.example/access"}`,
	"project_edit":              `{"id":"project-sample","done_when":"Every sample check passes."}`,
	"task_edit":                 `{"id":"task-sample","done_when":"The sample is checked."}`,
	"item_prepared":             `{"id":"task-sample","kind":"plan"}`,
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
	"report_friction":           `{"by":"reason","since":"2026-01-01"}`,
	"rfx_paths":                 `{"id":"rfx-sample"}`,
	"rfx_path_add":              `{"id":"rfx-sample","to":"person-sample","medium":"phone","reason":"no_response","why":"No reply by chat in a week.","nudge":"2026-11-12"}`,
	"rfx_target_state":          `{"id":"rfx-sample","to":"person-sample","state":"sent","date":"2026-11-01"}`,
	"request_add":               `{"title":"Summarize the sample goal","ask":"Summarize the sample project","links":["project-sample"]}`,
	"request_cancel":            `{"id":"request-sample"}`,
	"request_accept":            `{"id":"request-proposed"}`,
	"request_done":              `{"id":"request-sample","resolution":"done","result":"Summary written.","links":["project-sample"],"followed":["policy-sample"]}`,
	"request_fail":              `{"id":"request-sample","reason":"No access."}`,
	"request_note":              `{"id":"request-sample","text":"Read the project."}`,
	"request_show":              `{"id":"request-sample"}`,
	"request_seen":              `{"id":"request-finished"}`,
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
	"task_attempt":              `{"id":"call-sample","result":"no_answer","retry":"2026-11-05"}`,
	"task_resolve":              `{"id":"task-sample","outcome":"done","note":"The sample is checked.","done_when":"The sample is checked."}`,
	"task_update":               `{"id":"task-sample","text":"Sam agreed to review by Friday. Do we still need the old parser?"}`,
	"task_delegate":             `{"id":"task-sample","agent":"scribe","hint":"Start with the sample notes."}`,
	"task_show":                 `{"id":"task-sample"}`,
	"task_start":                `{"id":"task-sample"}`,
	"who":                       `{"query":"Sample person"}`,
	"receipt_show":              `{"id":"receipt-sample"}`,
	"history":                   `{"id":"task-sample"}`,
	"context":                   `{"id":"task-sample"}`,
	"receipt_check":             `{"id":"receipt-sample","verdict":"verified"}`,
	"receipt_undo":              `{"id":"receipt-sample"}`,
	"receipt_revert":            `{"id":"receipt-sample"}`,
	"question_review":           `{"id":"question-sample","decision":"skip"}`,
	"io_done":                   `{"id":"io-draft-sample","locator":"sent by hand"}`,
	"doc_merge":                 `{"keep":"note-sample","alias":"note-receipted"}`,
	"doc_archive":               `{"id":"note-receipted","reason":"Sample is old."}`,
	"doc_merges":                `{"id":"note-sample"}`,
	"task_to_asks":              `{"id":"task-other","people":["person-sample"]}`,
	"notes_read":                `{"id":"task-sample"}`,
	"notes_edit":                `{"id":"task-sample","md":"Sample notes."}`,
	"notes_save":                `{"id":"task-free","md":"Sample notes."}`,
	"align_scope":               `{"scope":"project-sample"}`,
	"align_record":              `{"scope":"project-sample","checked":4,"matched":3,"differ":1,"unreachable":["jira"]}`,
	"import_verify":             `{"files":[{"path":"/old/notes/a.md","hash":"` + "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" + `"}],"root":"/old"}`,
	"calendar_compare":          `{"events":[{"uid":"sync@acme.example","title":"Portal sync","start":"2026-11-04T15:00:00Z","end":"2026-11-04T15:30:00Z"}]}`,
	"similar":                   `{"type":"task","title":"Sample task"}`,
	"request_respond":           `{"id":"request-finished","kind":"change","text":"Use the shorter summary."}`,
	"request_changes":           `{"id":"request-finished"}`,
	"proposal_executed":         `{"id":"proposal-moved","item":"i1","result":"done","url":"https://acme.example/m/3","observed":true}`,
	"proposal_show":             `{"id":"proposal-sample"}`,
	"proposal_open":             `{"id":"proposal-sample"}`,
	"dossier_refile":            `{"proposal":"proposal-sample","item":"i1","dropped":"Not needed now."}`,
}

const (
	sampleTarget = "sample@acme.example"
	sampleDraft  = "Hi Sam."
)

func sampleProposals() ([]model.Document, error) {
	ev := model.Evidence{Quote: "Sample text.", Locator: "https://acme.example/line-1"}
	open, err := model.NewDossier(model.DossierHeader{TLDR: "Add a task and tell Sam.", Context: "Sam asked in the sample note."}, "Items",
		[]model.ProposalItem{
			{ID: "i1", Kind: model.ItemPara, Op: "task_add", Input: map[string]any{"project": "project-sample", "title": "From a proposal", "done_when": "It exists."}, Evidence: ev, State: model.ItemPending, Rev: 1},
			{ID: "i2", Kind: model.ItemAction, Subtype: "email", Target: sampleTarget, Body: sampleDraft, Evidence: ev, State: model.ItemPending, Rev: 1},
			{ID: "i3", Kind: model.ItemMessage, Subtype: "slack", Channel: "#sample", Body: "Sample note.", Evidence: ev, State: model.ItemPending, Rev: 1},
		}).Render()
	if err != nil {
		return nil, err
	}
	closed, err := model.RenderProposalBody(model.ProposalContent{
		Items: []model.ProposalItem{{ID: "i1", Kind: model.ItemAction, Subtype: "email", Target: sampleTarget, Body: sampleDraft,
			Evidence: ev, State: model.ItemApproved, ApprovedHash: model.ExternalHash(sampleTarget, sampleDraft),
			Decided: &model.ItemDecision{Decision: model.DecideApprove, By: "owner", Surface: "web", At: "2026-11-01T09:00:00Z"}}},
	})
	if err != nil {
		return nil, err
	}
	moved, err := model.NewDossier(model.DossierHeader{TLDR: "Tell Sam."}, "Items", []model.ProposalItem{
		{ID: "i1", Kind: model.ItemAction, Subtype: "email", Target: sampleTarget, Body: sampleDraft, Evidence: ev, State: model.ItemMoved, Result: "io-moved-sample"},
	}).Render()
	if err != nil {
		return nil, err
	}
	meta := func(s model.ProposalStatus) *model.Proposal {
		return &model.Proposal{Common: model.Common{Type: model.TypeProposal}, Source: model.LinkTo("note-sample"), Status: s, Revision: 1, ProposedBy: "claude-agent"}
	}
	return []model.Document{
		{ID: "proposal-sample", Folder: model.FolderProposals, Title: "Sample proposal", Body: open, Meta: meta(model.ProposalOpen)},
		{ID: "proposal-decided", Folder: model.FolderProposals, Title: "Decided proposal", Body: closed, Meta: meta(model.ProposalClosed)},
		{ID: "proposal-moved", Folder: model.FolderProposals, Title: "Moved proposal", Body: moved, Meta: meta(model.ProposalSuperseded)},
		{ID: "io-moved-sample", Folder: model.FolderIO, Title: "Moved draft", Body: sampleDraft,
			Meta: &model.IO{Common: model.Common{Type: model.TypeIO}, Direction: model.IOOut, Medium: model.MediumEmail, State: model.IODraft,
				To: []string{sampleTarget}, Expects: model.ExpectsNone, Links: []model.Link{model.LinkTo("proposal-moved")}}},
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

// docVersion is version n of doc-sample with text.
func docVersion(n int, text string) model.Document {
	sum := sha256.Sum256([]byte(text))
	url := "https://wiki.acme.example/runbooks/sample"
	return model.Document{ID: fmt.Sprintf("doc-sample-v%d.source", n), Folder: model.FolderTracked, Title: fmt.Sprintf("Sample runbook (version %d)", n),
		Body: "Source: " + url + "\n\n" + text,
		Meta: &model.Transcript{Common: model.Common{Type: model.TypeTranscript}, Of: model.LinkTo("doc-sample"), Version: n, FetchedBy: "agent",
			Source: model.Source{URL: url, Retrieved: fmt.Sprintf("2026-11-0%dT09:00:00Z", n), Hash: "sha256:" + hex.EncodeToString(sum[:]), Fidelity: model.FidelityExact}}}
}

// readyDraft is an outgoing draft the owner approved: io_send_check passes it.
func readyDraft() model.Document {
	m := &model.IO{Common: model.Common{Type: model.TypeIO}, Direction: model.IOOut, Medium: model.MediumEmail, State: model.IOReady,
		To: []string{"[[person-sample]]"}, Expects: model.ExpectsNone, ApprovedAt: "2026-11-01T09:00:00Z"}
	m.ApprovedHash = m.DraftHash("Approved draft")
	return model.Document{ID: "io-ready-sample", Folder: model.FolderIO, Title: "Approved draft", Body: "Approved draft", Meta: m}
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
				Links:     []model.Pointer{{Label: "Docs", Kind: model.PointerDocs, Target: "https://wiki.acme.example/sample"}},
				GoalBrief: model.GoalBrief{Signals: []model.Signal{{What: "Sample checks passing", How: "Run the sample suite", EveryDays: 7, Expected: "5 of 5"}}}}},
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
		{ID: "meeting-task-sample", Folder: model.FolderProjects, Title: "Sample task meeting",
			Body: "## Questions\n\nNone yet.\n\n## Notes\n",
			Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskTodo, Kind: model.KindMeeting,
				Occasion: model.Occasion{Scheduled: "2026-11-06T10:00:00-05:00", End: "2026-11-06T10:30:00-05:00", With: []model.Link{model.LinkTo("person-sample")}}}},
		{ID: "obligation-sample", Folder: model.FolderObligations, Title: "Send the sample plan",
			Meta: &model.Obligation{Common: model.Common{Type: model.TypeObligation}, Kind: model.KindCommitment, State: model.StateOpen,
				Schedule: model.ScheduleOnce, OwedTo: []model.Link{model.LinkTo("person-sample")}, Due: "2099-01-10"}},
		{ID: "call-sample", Folder: model.FolderProjects, Title: "Sample call",
			Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskTodo, Kind: model.KindCall,
				Project: model.LinkTo("project-sample"), Occasion: model.Occasion{Scheduled: "2026-11-04", With: []model.Link{model.LinkTo("person-sample")}}}},
		{ID: "task-free", Folder: model.FolderProjects, Title: "Free task",
			Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskTodo, Project: model.LinkTo("project-sample")}},
		{ID: "task-other", Folder: model.FolderProjects, Title: "Other task",
			Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskTodo, Project: model.LinkTo("project-sample"),
				DependsOn: []model.Link{model.LinkTo("task-sample")}}},
		{ID: "note-sample", Folder: model.FolderResources, Title: "Sample note", Body: "Sample text.",
			Meta: &model.Note{Common: model.Common{Type: model.TypeNote}, Area: model.LinkTo("area-sample")}},
		{ID: "task-sample", Folder: model.FolderProjects, Title: "Sample task",
			Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskTodo, Project: model.LinkTo("project-sample")}},
		{ID: "io-sample", Folder: model.FolderIO, Title: "Sample capture", Body: "Sample capture",
			Meta: &model.IO{Common: model.Common{Type: model.TypeIO}, Direction: model.IOIn, Medium: model.MediumOther, State: model.IOUnread}},
		{ID: "io-draft-sample", Folder: model.FolderIO, Title: "Sample draft", Body: "Sample draft",
			Meta: &model.IO{Common: model.Common{Type: model.TypeIO}, Direction: model.IOOut, Medium: model.MediumEmail, State: model.IODraft,
				To: []string{"[[person-sample]]"}, Expects: model.ExpectsNone}},
		readyDraft(),
		{ID: "doc-sample", Folder: model.FolderTracked, Title: "Sample runbook", Body: "Tracked from https://wiki.acme.example/runbooks/sample.",
			Meta: &model.TrackedDoc{Common: model.Common{Type: model.TypeTrackedDoc}, URL: "https://wiki.acme.example/runbooks/sample",
				System: model.SystemConfluence, Status: model.DocWatching, Project: model.LinkTo("project-sample"), Latest: 2, LastSeen: 1,
				Checked: "2026-11-02T09:00:00Z", Comments: []model.DocComment{{ID: "c1", By: "[[person-sample]]", At: "2026-11-01T10:00:00Z",
					Text: "Is step two needed?", Quote: "Step two.", Version: 1}}}},
		docVersion(1, "Step one.\nStep two."),
		docVersion(2, "Step one.\nStep two.\nStep three."),
		{ID: "source-sample", Folder: model.FolderSources, Title: "Sample tracker", Body: "Issues of the sample project.",
			Meta: &model.SourceDoc{Common: model.Common{Type: model.TypeSource}, System: model.SourceGitHub, Status: model.SourceActive,
				Via: model.SourceViaCLI, Fetch: &model.SourceStep{Command: "gh issue view {url}"}, Cadence: "daily", Cursor: "c-1"}},
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
			Meta: &model.Policy{Common: model.Common{Type: model.TypePolicy}, Followed: model.Followed{Source: source()}}},
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
	if _, err := s.AttachmentAdd(ctx, "task-sample", strings.TrimPrefix(SampleAttachment, "attachments/task-sample/"), []byte("Sample text.")); err != nil {
		return fmt.Errorf("opsample: seed attachment: %w", err)
	}
	return nil
}

// SampleAttachment is the vault path of the text file Seed attaches to
// task-sample.
const SampleAttachment = "attachments/task-sample/20261101-090000-sample.txt"
