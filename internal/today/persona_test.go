package today

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
	"github.com/iodesystems/para/internal/registry"
	"github.com/iodesystems/para/internal/store"
	"github.com/iodesystems/para/internal/store/local"
	"github.com/iodesystems/para/internal/upgrade"
	"github.com/iodesystems/para/internal/vault"
)

// The tools of the owner's own vaults. A second persona that does not use
// them must never see one of their names from code.
var otherVendors = []string{"gmail", "google", "slack", "jira", "confluence", "atlassian", "github"}

// PU-GN (e): a second synthetic persona, a freelance designer whose mail is
// outlook, chat is teams, and tracker is linear, with no corporate tools, no
// meeting series, and one weekly invoice reminder an agent owes. The full
// loop runs through ops: sweep in, triage, digest or needs you, the order,
// tick (prepare and produce), the agent prepares, the owner approves, the
// agent sends, the item closes; a brief of a non-meeting is refused; later and
// reopen. After each step every view agrees with NeedsYou.
func TestPersonaFreelanceDesignerFullLoop(t *testing.T) {
	ctx := context.Background()
	in, owner := fixture(t)
	root := filepath.Join(in.Home, "vaults", "acme")
	props := filepath.Join(root, ".para", "vault.properties")
	cur, err := os.ReadFile(props)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(props, append(cur, []byte("request.runner=m1\nrequest.surfaces=cli,pwa,tick\nprep.ahead=5\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	ast, err := local.Open(root, vault.Options{ID: "acme", Actor: "claude", Surface: "mcp", Install: "m1", StateDir: filepath.Join(in.Home, "state")})
	if err != nil {
		t.Fatal(err)
	}
	agent := ops.Deps{Store: ast}
	if err := owner.Store.SetConfig(ctx, upgrade.ConfigKey, strconv.Itoa(upgrade.Latest())); err != nil {
		t.Fatal(err)
	}

	// The owner's data: a client, a studio area, a goal, three sources, triage
	// rules, and the weekly invoice reminder the agent owes.
	client := must(t, ops.OrgPersonAdd, owner, ops.OrgAddInput{Name: "Rae Client", Org: "rae.example"}).ID
	must(t, ops.OrgPersonContactAdd, owner, ops.PersonContactInput{ID: client, Method: "email", Value: "rae@rae.example"})
	must(t, ops.PrioritySet, owner, ops.PrioritySetInput{ID: client, Priority: model.PriorityHigh})
	studio := must(t, ops.AreaAdd, owner, ops.AreaAddInput{Title: "Studio"}).ID
	goal := must(t, ops.ProjectAdd, owner, ops.ProjectAddInput{Title: "Rae brand refresh", DoneWhen: "Logo files delivered.", Area: studio}).ID
	for _, s := range []ops.SourceAddInput{
		{ID: "studio-mail", Title: "Studio mail", System: model.SourceMail, Vendor: "outlook", Cadence: "daily"},
		{ID: "client-chat", Title: "Client chat", System: model.SourceChat, Vendor: "teams", Cadence: "daily"},
		{ID: "design-board", Title: "Design board", System: model.SourceTracker, Vendor: "linear", Cadence: "daily", RefURL: "https://board.example/issue/{ref}"},
	} {
		must(t, ops.SourceAdd, owner, s)
	}
	if err := os.WriteFile(filepath.Join(root, ".para", "triage.yaml"), []byte(`rules:
  - name: font newsletter
    match: {from: news@fonts.example}
    actions: [digest, tag newsletter]
`), 0o600); err != nil {
		t.Fatal(err)
	}
	invoice := must(t, ops.ObligationAdd, owner, ops.ObligationAddInput{Kind: model.KindCommitment, Title: "Weekly invoice reminder",
		OwedBy: []string{"agent:claude"}, OwedTo: []string{client}, Recur: "weekly", Due: day(0), Area: studio,
		Produce: &ops.ProduceInput{Kind: model.ProduceMessage, Template: "Hi Rae, this week's invoice is attached. It is due on Friday.", To: []string{client}, Medium: model.MediumEmail}}).ID
	task := must(t, ops.TaskAdd, owner, ops.TaskAddInput{Project: goal, Title: "Export the logo files", DoneWhen: "Files in the shared folder.", Due: day(1)}).ID

	views := func(step string) map[string]bool {
		t.Helper()
		return viewsAgree(t, ctx, in, owner, step)
	}

	// The agent sweeps: declares its sources, plans, captures what came in,
	// and records each source.
	sources := []string{"studio-mail", "client-chat", "design-board"}
	must(t, ops.AgentSources, agent, ops.AgentSourcesInput{Sources: sources})
	plan := must(t, ops.SweepPlan, agent, ops.SweepPlanInput{Sources: sources})
	medium := map[string]model.Medium{}
	for _, s := range plan.Sources {
		medium[s.ID] = s.Medium
	}
	capture := func(src, from, subject string, ex model.IOExpects) string {
		in := ops.IOAddInput{Direction: model.IOIn, Text: subject, Subject: subject, From: from, Expects: ex, At: "2026-11-06T08:00:00Z",
			Medium: model.Medium(cmpOr(string(medium[src]), string(model.MediumOther))), Via: &model.IOVia{Source: src, Ref: strings.ReplaceAll(strings.ToLower(subject), " ", "-")},
			Verify: "search the " + src + " for " + subject}
		if ex == model.ExpectsResponse {
			in.Effort = model.EffortS
		}
		return must(t, ops.IOAdd, agent, in).ID
	}
	ask := capture("studio-mail", client, "Can you send the revised logo by Thursday", model.ExpectsResponse)
	news := capture("studio-mail", "news@fonts.example", "New fonts this week", "")
	lunch := capture("client-chat", "pat@studio.example", "Lunch later", "")
	comment := capture("design-board", "bot@board.example", "New comment on LOGO-12", "")
	for _, src := range sources {
		must(t, ops.SweepRecord, agent, ops.SweepRecordInput{Source: src, Counts: map[string]int{"in": 1}})
	}
	set := views("after the sweep")
	for id, want := range map[string]bool{ask: true, news: false, lunch: false, comment: false, task: true, invoice: false} {
		if set[id] != want {
			t.Errorf("after the sweep: %s needs you %v, want %v", id, set[id], want)
		}
	}
	if m := ioMeta(t, owner, news); m.Triage != model.TriageDigest || !slices.Contains(m.Tags, "newsletter") {
		t.Errorf("newsletter not sorted by the rule: %+v", m)
	}
	digest := map[string]bool{}
	for _, it := range must(t, ops.IOList, owner, ops.IOListInput{Direction: model.IOIn, Digest: true}).Items {
		digest[it.ID] = true
	}
	for id, want := range map[string]bool{ask: false, news: true, lunch: true, comment: true} {
		if digest[id] != want {
			t.Errorf("digest %s: %v, want %v", id, digest[id], want)
		}
	}

	// Tick: prepare work for the reply, produce work for the invoice.
	out, err := ops.Tick(ctx, owner, ops.TickInput{})
	if err != nil {
		t.Fatal(err)
	}
	prep, produce := requestFor(t, owner, out.Prepares, ask), requestFor(t, owner, out.Produce, invoice)
	if prep == "" || produce == "" {
		t.Fatalf("tick: prepares %v, produce %v: none for %s or %s", out.Prepares, out.Produce, ask, invoice)
	}
	views("after tick")

	// The agent works the queue: a reply draft for the message, the invoice
	// message for the obligation.
	for i := 0; i < len(out.Requests)+len(out.Prepares)+len(out.Gaps)+2; i++ {
		if got := must(t, ops.RequestNext, agent, ops.RequestNextInput{}); got.Request == nil {
			break
		}
	}
	reply := must(t, ops.IOAdd, agent, ops.IOAddInput{Direction: model.IOOut, Text: "Yes: the revised logo comes on Thursday.", Subject: "Re: revised logo",
		To: []string{client}, Medium: model.MediumEmail, Expects: model.ExpectsNone, Thread: ask}).ID
	must(t, ops.ItemPrepared, agent, ops.ItemPreparedInput{ID: ask, Kind: model.PreparedDraft, Ref: reply})
	must(t, ops.RequestDone, agent, ops.RequestDoneInput{ID: prep, Resolution: model.ResolutionDone, Result: "Drafted the reply.", Links: []string{reply}})
	bill := must(t, ops.IOAdd, agent, ops.IOAddInput{Direction: model.IOOut, Text: "Hi Rae, this week's invoice is attached. It is due on Friday.", Subject: "Invoice",
		To: []string{client}, Medium: model.MediumEmail, Expects: model.ExpectsNone}).ID
	must(t, ops.ItemPrepared, agent, ops.ItemPreparedInput{ID: invoice, Kind: model.PreparedArtifact, Ref: bill})
	must(t, ops.RequestDone, agent, ops.RequestDoneInput{ID: produce, Resolution: model.ResolutionDone, Result: "Drafted the invoice message.", Links: []string{bill}})
	set = views("after the agent prepared")
	for _, id := range []string{ask, invoice} {
		r := orderRow(t, owner, id)
		if !set[id] || !r.Ready {
			t.Errorf("%s after prepare: needs you %v, row %+v", id, set[id], r)
		}
	}

	// The owner approves each draft; the agent sends it. The sent reply files
	// the message it answers; the agent closes the invoice occurrence.
	for _, x := range []struct {
		draft, item string
		close       bool
	}{{reply, ask, false}, {bill, invoice, true}} {
		if got := must(t, ops.IOApprove, owner, ops.IOApproveInput{ID: x.draft}); got.State != model.IOReady || got.SelfReported {
			t.Fatalf("approve %s: %+v", x.draft, got)
		}
		must(t, ops.IOSend, agent, ops.IOSendInput{ID: x.draft, URL: "https://mail.example/sent/" + x.draft})
		if x.close {
			must(t, ops.Close, agent, ops.CloseInput{ID: x.item})
		}
		if sn := snapshot(t, owner); sn.Need(x.item).Life != ops.LifeClosed {
			t.Errorf("%s still open after its draft was sent: %+v", x.item, sn.Need(x.item))
		}
	}
	set = views("after send")
	if set[ask] || set[invoice] {
		t.Errorf("a closed item needs you: ask %v invoice %v", set[ask], set[invoice])
	}
	var next string
	refs, err := owner.Store.List(ctx, store.Filter{Type: model.TypeObligation})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range refs {
		if doc, _ := owner.Store.Get(ctx, r.ID); doc.Meta.(*model.Obligation).Open() && doc.Title == "Weekly invoice reminder" {
			next = r.ID
		}
	}
	if next == "" || set[next] {
		t.Errorf("the next invoice occurrence: %q, needs you %v", next, set[next])
	} else if o, _ := owner.Store.Get(ctx, next); string(o.Meta.(*model.Obligation).Due) != day(7) {
		t.Errorf("next invoice due %s, want %s", o.Meta.(*model.Obligation).Due, day(7))
	}

	// No meetings: a brief of a task with no people is refused, and says why.
	if _, err := ops.Brief.Run(ctx, owner, ops.BriefInput{ID: task}); err == nil || !strings.Contains(err.Error(), "nothing to brief") {
		t.Errorf("brief of a task: %v", err)
	}

	// Later takes the task off every view; reopen brings it back.
	must(t, ops.Later, owner, ops.LaterInput{ID: task, Until: day(3)})
	if set = views("after later"); set[task] {
		t.Error("a task put off needs you")
	}
	must(t, ops.Reopen, owner, ops.ReopenInput{ID: task})
	if set = views("after reopen"); !set[task] {
		t.Error("a task brought back does not need you")
	}

	// No other vendor's name reaches this owner from code.
	var blob strings.Builder
	for _, v := range []any{must(t, ops.OrderOp, owner, ops.OrderInput{}), must(t, ops.Agenda, owner, ops.AgendaInput{Days: 7}), plan,
		must(t, ops.IOList, owner, ops.IOListInput{All: true})} {
		b, _ := json.Marshal(v)
		blob.Write(b)
	}
	for _, id := range append(append(out.Requests, out.Prepares...), out.Gaps...) {
		doc, err := owner.Store.Get(ctx, id)
		if err == nil {
			blob.WriteString(doc.Body)
		}
	}
	rep, err := Read(ctx, in, "user", "cli", day(0))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(rep)
	blob.Write(b)
	text := strings.ToLower(blob.String())
	for _, name := range otherVendors {
		if strings.Contains(text, name) {
			i := strings.Index(text, name)
			t.Errorf("%q reaches the persona: ...%s...", name, text[max(0, i-80):min(len(text), i+80)])
		}
	}
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func snapshot(t *testing.T, d ops.Deps) *ops.Snapshot {
	t.Helper()
	sn, err := ops.NewSnapshot(context.Background(), d.Store)
	if err != nil {
		t.Fatal(err)
	}
	return sn
}

func ioMeta(t *testing.T, d ops.Deps, id string) *model.IO {
	t.Helper()
	doc, err := d.Store.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return doc.Meta.(*model.IO)
}

func orderRow(t *testing.T, d ops.Deps, id string) ops.ValueRow {
	t.Helper()
	for _, r := range must(t, ops.OrderOp, d, ops.OrderInput{}).Rows {
		if r.ID == id {
			return r
		}
	}
	return ops.ValueRow{}
}

// requestFor is the request of ids that links item.
func requestFor(t *testing.T, d ops.Deps, ids []string, item string) string {
	t.Helper()
	for _, id := range ids {
		_, r, err := getRequest(d, id)
		if err != nil {
			t.Fatal(err)
		}
		if slices.ContainsFunc(r.Links, func(l model.Link) bool { return l.Target() == item }) {
			return id
		}
	}
	return ""
}

// viewsAgree checks that Today, the agenda, the order (the Next deck), and
// the inbox list only what NeedsYou holds, and together list all of it. It
// returns the set.
func viewsAgree(t *testing.T, ctx context.Context, in registry.Install, d ops.Deps, step string) map[string]bool {
	t.Helper()
	sn := snapshot(t, d)
	set := map[string]bool{}
	for _, id := range sn.Set() {
		set[id] = true
	}
	union := map[string]bool{}
	part := func(name string, ids []string) {
		t.Helper()
		for _, id := range ids {
			if !set[id] {
				t.Errorf("%s: %s lists %s, which does not need the owner: %+v", step, name, id, sn.Need(id))
			}
			union[id] = true
		}
	}
	rep, err := Read(ctx, in, "user", "cli", day(0))
	if err != nil || rep.Failed() != nil {
		t.Fatalf("%s: today: %v %v", step, err, rep.Failed())
	}
	vt := rep.Vaults[0]
	for name, g := range map[string]Group{"incidents": vt.Incidents, "agenda": vt.Agenda, "overdue": vt.Overdue, "due_soon": vt.DueSoon,
		"blocked": vt.Blocked, "nudges": vt.Nudges, "due": vt.Due, "work": vt.Work} {
		var ids []string
		for _, it := range g.Items {
			ids = append(ids, it.ID)
		}
		part("today "+name, ids)
	}
	if vt.NeedsYou != len(set) {
		t.Errorf("%s: today counts %d, the set holds %d", step, vt.NeedsYou, len(set))
	}
	var ag []string
	for _, it := range must(t, ops.Agenda, d, ops.AgendaInput{Days: 1}).Items {
		if it.Kind == ops.AgendaGoal {
			continue
		}
		if it.NeedsYou != set[it.ID] {
			t.Errorf("%s: agenda %s needs_you %v, set %v", step, it.ID, it.NeedsYou, set[it.ID])
		}
		if it.NeedsYou {
			ag = append(ag, it.ID)
		}
	}
	part("agenda", ag)
	var next []string
	for _, r := range must(t, ops.OrderOp, d, ops.OrderInput{}).Rows {
		if !r.NeedsYou {
			t.Errorf("%s: order row %s does not need you", step, r.ID)
		}
		next = append(next, r.ID)
	}
	part("order", next)
	var inbox []string
	for _, it := range must(t, ops.IOList, d, ops.IOListInput{Direction: model.IOIn}).Items {
		if it.NeedsYou {
			inbox = append(inbox, it.ID)
		}
	}
	part("inbox", inbox)
	var missing []string
	for id := range set {
		if !union[id] {
			missing = append(missing, id)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%s: needs the owner but no view lists it: %v", step, missing)
	}
	return set
}
