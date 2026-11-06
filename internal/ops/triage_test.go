package ops

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
)

// writeTriage writes .para/triage.yaml of the vault at root.
func writeTriage(t *testing.T, root, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, ".para", "triage.yaml"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func ioDoc(t *testing.T, d Deps, id string) *model.IO {
	t.Helper()
	doc, err := d.Store.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return doc.Meta.(*model.IO)
}

func needOf(t *testing.T, d Deps, id string) Need {
	t.Helper()
	sn, err := NewSnapshot(ctx, d.Store)
	if err != nil {
		t.Fatal(err)
	}
	return sn.Need(id)
}

// The needs_you rule of an open incoming message, over every combination of
// expects response, the sender's own priority, their group's priority, the
// source's priority, and a triage rule's verdict.
func TestInboundNeedsYouMatrix(t *testing.T) {
	now := time.Date(2026, 11, 6, 10, 0, 0, 0, time.Local)
	d, _ := sourceClockDeps(t, &now)
	must(t, OrgGroupAdd, d, GroupAddInput{Name: "Crew", Org: "acme.example", Kind: model.GroupTeam})
	must(t, OrgPersonAdd, d, OrgAddInput{Name: "Ada Example"})
	must(t, OrgPersonGroupAdd, d, PersonGroupInput{ID: "person-ada-example", Group: "group-crew"})
	must(t, SourceAdd, d, SourceAddInput{ID: "src-mail", Title: "Mail", System: model.SourceMail, Vendor: "examplemail"})
	io := map[model.IOExpects]string{}
	for _, ex := range []model.IOExpects{"", model.ExpectsResponse} {
		io[ex] = must(t, IOAdd, d, IOAddInput{Direction: model.IOIn, Text: "Hello " + string(ex), From: "person-ada-example", Expects: ex,
			Via: &model.IOVia{Source: "src-mail", Ref: "m-" + string(ex)}}).ID
	}
	n := 0
	for _, ex := range []model.IOExpects{"", model.ExpectsResponse} {
		for _, pp := range []model.Priority{"", model.PriorityHigh, model.PriorityNormal, model.PriorityLow} {
			for _, gp := range []model.Priority{"", model.PriorityHigh, model.PriorityLow} {
				for _, sp := range []model.Priority{"", model.PriorityHigh} {
					for _, rv := range []model.IOTriage{"", model.TriageNeedsYou, model.TriageDigest} {
						must(t, PrioritySet, d, PrioritySetInput{ID: "person-ada-example", Priority: pp})
						must(t, PrioritySet, d, PrioritySetInput{ID: "group-crew", Priority: gp})
						must(t, PrioritySet, d, PrioritySetInput{ID: "src-mail", Priority: sp})
						doc, _ := d.Store.Get(ctx, io[ex])
						doc.Meta.(*model.IO).Triage, doc.Meta.(*model.IO).TriageBy = rv, "rule 1"
						if _, err := d.Store.Update(ctx, doc); err != nil {
							t.Fatal(err)
						}
						sender := pp
						if sender == "" {
							sender = gp
						}
						want := ex == model.ExpectsResponse || sender == model.PriorityHigh || sp == model.PriorityHigh
						switch rv {
						case model.TriageNeedsYou:
							want = true
						case model.TriageDigest:
							want = false
						}
						got := needOf(t, d, io[ex])
						if got.Yes != want {
							t.Errorf("expects %q person %q group %q source %q rule %q: needs_you %v (%s), want %v", ex, pp, gp, sp, rv, got.Yes, got.Reason, want)
						}
						if !want && got.Turn != TurnNone {
							t.Errorf("digest case: turn %s", got.Turn)
						}
						n++
					}
				}
			}
		}
	}
	if n != 144 {
		t.Fatalf("%d cases", n)
	}
	// Free-text sender, no via: digest. No sender, no via, no link: the
	// owner's own capture needs them.
	other := must(t, IOAdd, d, IOAddInput{Direction: model.IOIn, Text: "Weekly news", From: "news@lists.example"}).ID
	if got := needOf(t, d, other); got.Yes || !strings.HasPrefix(got.Reason, "digest") {
		t.Errorf("free-text sender: %+v", got)
	}
	own := must(t, IOAdd, d, IOAddInput{Direction: model.IOIn, Text: "Call the bank"}).ID
	if got := needOf(t, d, own); !got.Yes {
		t.Errorf("own capture: %+v", got)
	}
	// io_list marks the digest rows.
	for _, it := range must(t, IOList, d, IOListInput{Direction: model.IOIn, Digest: true}).Items {
		if it.ID == own || it.NeedsYou || !it.Digest {
			t.Errorf("digest list row %+v", it)
		}
	}
}

const triageRules = `rules:
  - name: vendors
    match: {from: group-vendors}
    actions: [tag vendor]
  - name: release notes
    match: {subject: "release notes"}
    actions: [digest, link task-launch]
  - match: {from: group-vendors}
    actions: [needs_you]
  - match: {vendor: outlook, tag: spam}
    actions: [mute]
`

func triageVault(t *testing.T) (Deps, string) {
	t.Helper()
	now := time.Date(2026, 11, 6, 10, 0, 0, 0, time.Local)
	d, root := sourceClockDeps(t, &now)
	must(t, OrgGroupAdd, d, GroupAddInput{Name: "Vendors", Org: "acme.example", Kind: model.GroupList})
	must(t, OrgPersonAdd, d, OrgAddInput{Name: "Val Example"})
	must(t, OrgPersonGroupAdd, d, PersonGroupInput{ID: "person-val-example", Group: "group-vendors"})
	must(t, SourceAdd, d, SourceAddInput{ID: "src-outlook", Title: "Mail", System: model.SourceMail, Vendor: "outlook",
		RefURL: "https://mail.example.com/thread/{ref}"})
	proj := must(t, ProjectAdd, d, ProjectAddInput{Title: "Launch", Owner: "self", DoneWhen: "Shipped."}).ID
	createDoc(t, d, model.Document{ID: "task-launch", Folder: model.FolderProjects, Title: "Launch",
		Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskTodo, Project: model.LinkTo(proj)}})
	writeTriage(t, root, triageRules)
	return d, root
}

func triageReceipts(t *testing.T, d Deps, doc string) []ReceiptView {
	t.Helper()
	all, err := listReceipts(ctx, d.Store)
	if err != nil {
		t.Fatal(err)
	}
	var out []ReceiptView
	for _, r := range all {
		if r.Op == triageOp && r.Doc == doc {
			out = append(out, r)
		}
	}
	return out
}

// Rules apply in order when io is added: tag and link add up, the first
// verdict decides, and each change files a receipt naming its rule.
func TestTriageRuleOrderAndActions(t *testing.T) {
	d, _ := triageVault(t)
	rel := must(t, IOAdd, d, IOAddInput{Direction: model.IOIn, Text: "Release notes 2.1", From: "person-val-example"})
	m := ioDoc(t, d, rel.ID)
	if m.Triage != model.TriageDigest || m.TriageBy != "rule 2 (release notes)" {
		t.Errorf("triage %q by %q", m.Triage, m.TriageBy)
	}
	if !slices.Equal(m.Tags, []string{"vendor"}) || !slices.Equal(m.Links, []model.Link{model.LinkTo("task-launch")}) {
		t.Errorf("tags %v links %v", m.Tags, m.Links)
	}
	if len(rel.Triage) != 3 {
		t.Fatalf("acts %+v", rel.Triage)
	}
	rs := triageReceipts(t, d, rel.ID)
	if len(rs) != 3 {
		t.Fatalf("receipts %+v", rs)
	}
	for _, r := range rs {
		if !strings.HasPrefix(r.Why, "triage rule ") {
			t.Errorf("receipt why %q", r.Why)
		}
	}
	if got := needOf(t, d, rel.ID); got.Yes {
		t.Errorf("release notes need you: %+v", got)
	}
	hi := must(t, IOAdd, d, IOAddInput{Direction: model.IOIn, Text: "Your renewal", From: "person-val-example"}).ID
	if m := ioDoc(t, d, hi); m.Triage != model.TriageNeedsYou || m.TriageBy != "rule 3" {
		t.Errorf("rule 3: %q by %q", m.Triage, m.TriageBy)
	}
	if got := needOf(t, d, hi); !got.Yes || !strings.Contains(got.Reason, "rule 3") {
		t.Errorf("rule 3 need: %+v", got)
	}
	// A second run changes nothing and files nothing.
	run := must(t, TriageRun, d, TriageRunInput{})
	for _, a := range run.Acts {
		if a.Receipt != "" || a.Skipped == "" {
			t.Errorf("second run acted: %+v", a)
		}
	}
	if len(triageReceipts(t, d, rel.ID)) != 3 {
		t.Error("second run filed receipts")
	}
}

// mute drops the message with the rule in the reason; undoing its receipt
// opens it again, and later runs leave it open.
func TestTriageMuteUndo(t *testing.T) {
	d, _ := triageVault(t)
	id := must(t, IOAdd, d, IOAddInput{Direction: model.IOIn, Text: "WIN A PRIZE", From: "prize@spam.example", Tags: []string{"spam"},
		Via: &model.IOVia{Source: "src-outlook", Ref: "t-1"}, Verify: "inbox, subject WIN A PRIZE"}).ID
	m := ioDoc(t, d, id)
	if m.State != model.IODropped || m.Reason != "muted by rule 4" {
		t.Fatalf("state %s reason %q", m.State, m.Reason)
	}
	rs := triageReceipts(t, d, id)
	if len(rs) != 1 || !strings.HasPrefix(rs[0].Why, "triage rule 4: mute.") {
		t.Fatalf("receipts %+v", rs)
	}
	must(t, ReceiptUndo, d, ReceiptUndoInput{ID: rs[0].ID})
	if m := ioDoc(t, d, id); m.State != model.IOUnread {
		t.Fatalf("after undo: %s", m.State)
	}
	run := must(t, TriageRun, d, TriageRunInput{IDs: []string{id}})
	if len(run.Acts) != 1 || run.Acts[0].Skipped != "its receipt was undone" {
		t.Fatalf("run after undo: %+v", run.Acts)
	}
	if m := ioDoc(t, d, id); m.State != model.IOUnread {
		t.Fatalf("muted again: %s", m.State)
	}
}

// triage_run covers io added before the rules, and dry_run changes nothing.
func TestTriageRunDryRun(t *testing.T) {
	d, root := triageVault(t)
	writeTriage(t, root, "rules: []\n")
	id := must(t, IOAdd, d, IOAddInput{Direction: model.IOIn, Text: "Release notes 3", From: "person-val-example"}).ID
	writeTriage(t, root, triageRules)
	dry := must(t, TriageRun, d, TriageRunInput{DryRun: true})
	if !dry.DryRun || dry.Rules != 4 || dry.Checked != 1 || len(dry.Acts) != 3 {
		t.Fatalf("dry %+v", dry)
	}
	if m := ioDoc(t, d, id); m.Triage != "" || len(m.Tags) != 0 {
		t.Fatalf("dry run wrote: %+v", m)
	}
	run := must(t, TriageRun, d, TriageRunInput{})
	if len(run.Acts) != 3 || run.Acts[0].Receipt == "" {
		t.Fatalf("run %+v", run.Acts)
	}
	// A broken file is an error of triage_run and a warning of io_add.
	writeTriage(t, root, "rules:\n  - match: {from: x}\n    actions: [explode]\n")
	if _, err := TriageRun.Run(ctx, d, TriageRunInput{}); err == nil || !strings.Contains(err.Error(), "rule 1") {
		t.Errorf("bad rule: %v", err)
	}
	out := must(t, IOAdd, d, IOAddInput{Direction: model.IOIn, Text: "Hi", From: "x@example.com"})
	if len(out.Warnings) == 0 || !strings.Contains(out.Warnings[0], "triage rules not applied") {
		t.Errorf("io_add warnings %v", out.Warnings)
	}
	writeTriage(t, root, "rules:\n  - match: {colour: red}\n    actions: [mute]\n")
	if _, err := TriageRun.Run(ctx, d, TriageRunInput{}); err == nil {
		t.Error("unknown match key taken")
	}
}

// The day journal lists that day's incoming messages that did not need the
// owner, inside generated markers; a muted one is counted.
func TestJournalInboxDigest(t *testing.T) {
	d, _ := triageVault(t)
	at := "2026-11-06T09:00:00Z"
	digest := must(t, IOAdd, d, IOAddInput{Direction: model.IOIn, Text: "Monthly update\n\nNothing to do.", From: "news@lists.example",
		Via: &model.IOVia{Source: "src-outlook", Ref: "t-9"}, Verify: "inbox, subject Monthly update", At: at}).ID
	asks := must(t, IOAdd, d, IOAddInput{Direction: model.IOIn, Text: "Can you sign?", From: "boss@acme.example", Expects: model.ExpectsResponse, At: at}).ID
	must(t, IOAdd, d, IOAddInput{Direction: model.IOIn, Text: "Spam", From: "s@spam.example", Tags: []string{"spam"},
		Via: &model.IOVia{Source: "src-outlook", Ref: "t-10"}, Verify: "inbox", At: at})
	yesterday := must(t, IOAdd, d, IOAddInput{Direction: model.IOIn, Text: "Old news", From: "news@lists.example", At: "2026-11-04T09:00:00Z"}).ID
	must(t, JournalDaily, d, JournalInput{Date: "2026-11-06"})
	j, err := d.Store.Get(ctx, "2026-11-06")
	if err != nil {
		t.Fatal(err)
	}
	sec := sectionText(j.Body, inboxDigestSection)
	for _, want := range []string{inboxDigestHeading, "- [[" + digest + "]] from news@lists.example: Monthly update https://mail.example.com/thread/t-9", "- 1 muted by triage rules"} {
		if !strings.Contains(sec, want) {
			t.Errorf("digest has no %q:\n%s", want, sec)
		}
	}
	for _, no := range []string{asks, yesterday} {
		if strings.Contains(sec, no) {
			t.Errorf("digest lists %s:\n%s", no, sec)
		}
	}
	// The message stays in the inbox, under digest.
	got := must(t, IOList, d, IOListInput{Direction: model.IOIn, Digest: true}).Items
	if !slices.ContainsFunc(got, func(it IOItem) bool { return it.ID == digest }) {
		t.Errorf("io_list digest %+v", got)
	}
}

// A second synthetic persona: no corporate tools; mail is outlook, chat is
// teams, the tracker is linear. The same mechanism runs: io_add, triage,
// digest, and needs_you, with every name in vault data.
func TestPersonaOutlookTeamsLinear(t *testing.T) {
	now := time.Date(2026, 11, 6, 10, 0, 0, 0, time.Local)
	d, root := sourceClockDeps(t, &now)
	must(t, SourceAdd, d, SourceAddInput{ID: "home-mail", Title: "Home mail", System: model.SourceMail, Vendor: "outlook", Cadence: "daily"})
	must(t, SourceAdd, d, SourceAddInput{ID: "club-chat", Title: "Club chat", System: model.SourceChat, Vendor: "teams", Cadence: "daily"})
	must(t, SourceAdd, d, SourceAddInput{ID: "side-project", Title: "Side project", System: model.SourceTracker, Vendor: "linear",
		Priority: model.PriorityHigh, RefURL: "https://tracker.example/issue/{ref}"})
	must(t, OrgGroupAdd, d, GroupAddInput{Name: "Family", Org: "home", Kind: model.GroupList})
	must(t, OrgPersonAdd, d, OrgAddInput{Name: "Kim Example"})
	must(t, OrgPersonAdd, d, OrgAddInput{Name: "Lee Example"})
	must(t, OrgPersonGroupAdd, d, PersonGroupInput{ID: "person-kim-example", Group: "group-family"})
	must(t, PrioritySet, d, PrioritySetInput{ID: "group-family", Priority: model.PriorityHigh})
	writeTriage(t, root, `rules:
  - name: club chatter
    match: {vendor: teams, subject: "lunch"}
    actions: [digest, tag club]
  - match: {class: chat, from: person-lee-example}
    actions: [needs_you]
`)
	claude := agentDeps(d, "claude")
	must(t, AgentSources, claude, AgentSourcesInput{Sources: []string{"home-mail", "club-chat", "side-project"}})
	plan := must(t, SweepPlan, claude, SweepPlanInput{Sources: []string{"home-mail", "club-chat", "side-project"}})
	media := map[string]model.Medium{}
	for _, s := range plan.Sources {
		media[s.ID] = s.Medium
	}
	if media["home-mail"] != model.MediumEmail || media["club-chat"] != model.MediumTeams || media["side-project"] != "" {
		t.Fatalf("media %v", media)
	}
	add := func(text, from, src string, ex model.IOExpects) string {
		in := IOAddInput{Direction: model.IOIn, Text: text, From: from, Expects: ex, Medium: model.Medium(firstOf(string(media[src]), string(model.MediumOther))),
			Via: &model.IOVia{Source: src, Ref: slug(text)}, Verify: "search " + text, At: "2026-11-06T08:00:00Z"}
		return must(t, IOAdd, claude, in).ID
	}
	ids := map[string]string{
		"family":   add("Dinner Sunday?", "person-kim-example", "home-mail", ""),
		"promo":    add("Sale this week", "shop@store.example", "home-mail", ""),
		"question": add("Can you send the form?", "school@school.example", "home-mail", model.ExpectsResponse),
		"lunch":    add("Lunch on Friday", "person-lee-example", "club-chat", ""),
		"lee":      add("Are you coming", "person-lee-example", "club-chat", ""),
		"issue":    add("Bug filed", "bot@tracker.example", "side-project", ""),
	}
	for _, r := range must(t, SweepRecord, claude, SweepRecordInput{Source: "club-chat"}).Warnings {
		t.Errorf("sweep warning %s", r)
	}
	want := map[string]bool{"family": true, "promo": false, "question": true, "lunch": false, "lee": true, "issue": true}
	sn, err := NewSnapshot(ctx, d.Store)
	if err != nil {
		t.Fatal(err)
	}
	for k, id := range ids {
		if got := sn.Need(id); got.Yes != want[k] {
			t.Errorf("%s: needs_you %v (%s), want %v", k, got.Yes, got.Reason, want[k])
		}
	}
	if m := ioDoc(t, d, ids["lunch"]); !slices.Contains(m.Tags, "club") || m.TriageBy != "rule 1 (club chatter)" {
		t.Errorf("lunch: %+v", m)
	}
	must(t, JournalDaily, d, JournalInput{Date: "2026-11-06"})
	j, _ := d.Store.Get(ctx, "2026-11-06")
	sec := sectionText(j.Body, inboxDigestSection)
	for k, id := range ids {
		if strings.Contains(sec, "[["+id+"]]") == want[k] {
			t.Errorf("digest and %s (needs_you %v):\n%s", k, want[k], sec)
		}
	}
	if n := strings.Count(sec, "\n- "); n != 2 {
		t.Errorf("digest lines %d:\n%s", n, sec)
	}
}
