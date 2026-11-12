package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
)

// The delegation tests use the io persona and a made-up worker agent,
// worker-q, with its own stamp. Nothing here is anyone's real wording.

func writeDelegation(t *testing.T, p *ioPersona, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(p.rv.root, ".para", "delegation.yaml"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

const delegationRules = `stamp: "-- sent by {agent} for {owner} (file default)"
agents:
  worker-q: {stamp: "-- worker-q writing for {owner} on {date}"}
rules:
  - name: chat acks
    medium: slack
    to: [group-crew]
    kind: [acknowledgement, scheduling]
    max_chars: 80
    agent: [worker-q]
  - name: outside mail
    medium: email
    to: ["*@outside.example.test"]
    stamp: "-- {agent} for {owner}, {rule}"
`

type delegationFixture struct {
	*ioPersona
	worker Deps
	crew   string
}

func newDelegationFixture(t *testing.T) *delegationFixture {
	t.Helper()
	p := newIOPersona(t)
	f := &delegationFixture{ioPersona: p, worker: p.rv.as("worker-q", "mcp", "install-a")}
	f.crew = mustRun(t, OrgGroupAdd, p.owner, GroupAddInput{Name: "Crew", Org: "example.test", Kind: model.GroupTeam}).ID
	mustRun(t, OrgPersonGroupAdd, p.owner, PersonGroupInput{ID: p.pat, Group: f.crew})
	mustRun(t, AgentSources, f.worker, AgentSourcesInput{Sources: []string{p.mail, p.chat}})
	return f
}

// draft adds a for_me draft as the worker: a short Slack ack to Pat.
func (f *delegationFixture) draft(t *testing.T, text, kind string) IOAddOutput {
	t.Helper()
	return mustRun(t, IOAdd, f.worker, IOAddInput{Direction: model.IOOut, Text: text, To: []string{f.pat}, Medium: model.MediumSlack,
		Expects: model.ExpectsNone, Voice: model.VoiceForMe, Kind: kind, Via: &model.IOVia{Source: f.chat, Ref: "d-" + text, Thread: "t-ack"}})
}

func sendErr(t *testing.T, d Deps, id string) error {
	t.Helper()
	_, err := IOSend.Run(t.Context(), d, IOSendInput{ID: id, URL: "https://chat.example.test/m/" + id})
	return err
}

func TestForMeWithoutRulesNeedsApproval(t *testing.T) {
	f := newDelegationFixture(t)
	a := f.draft(t, "Got it, thanks.", "acknowledgement")
	if len(a.Warnings) == 0 || !strings.Contains(a.Warnings[0], "no delegation rules") {
		t.Fatalf("warnings = %v", a.Warnings)
	}
	doc, m := personaIO(t, f.owner, a.ID)
	want := "— sent by worker-q for the owner; not reviewed by the owner"
	if m.Voice != model.VoiceForMe || m.Agent != "worker-q" || m.Stamp != want || doc.Body != "Got it, thanks.\n\n"+want || m.AllowedBy != "" {
		t.Fatalf("draft = %+v %q", m, doc.Body)
	}
	if n := needOf(t, f.owner, a.ID); !n.Yes || !strings.Contains(n.Reason, "no delegation rule") {
		t.Fatalf("need = %+v", n)
	}
	if _, err := IOSendCheck.Run(t.Context(), f.worker, IOSendCheckInput{ID: a.ID}); err == nil || !strings.Contains(err.Error(), "no delegation rule") {
		t.Fatalf("check: %v", err)
	}
	if err := sendErr(t, f.worker, a.ID); err == nil || !strings.Contains(err.Error(), "no delegation rule") {
		t.Fatalf("send: %v", err)
	}
	// The vault owner name and io.stamp fill the default.
	mustRun(t, VaultConfigSet, f.owner, VaultConfigSetInput{Key: "owner", Value: f.pat})
	mustRun(t, VaultConfigSet, f.owner, VaultConfigSetInput{Key: StampKey, Value: "-- {agent} for {owner}"})
	b := f.draft(t, "Noted.", "")
	if _, m := personaIO(t, f.owner, b.ID); m.Stamp != "-- worker-q for Pat Example" {
		t.Fatalf("io.stamp = %q", m.Stamp)
	}
	if _, err := VaultConfigSet.Run(t.Context(), f.owner, VaultConfigSetInput{Key: StampKey, Value: "-- {nobody}"}); err == nil {
		t.Fatal("unknown placeholder accepted")
	}
	mustLintClean(t, f.owner)
}

func TestForMeRuleLetsTheAgentSend(t *testing.T) {
	f := newDelegationFixture(t)
	writeDelegation(t, f.ioPersona, delegationRules)
	a := f.draft(t, "Tuesday 10:00 works.", "scheduling")
	doc, m := personaIO(t, f.owner, a.ID)
	if m.AllowedBy != "rule 1 (chat acks)" || m.Stamp != "-- worker-q writing for the owner on "+string(m.Created) || !strings.HasSuffix(doc.Body, m.Stamp) {
		t.Fatalf("draft = %+v %q", m, doc.Body)
	}
	if n := needOf(t, f.owner, a.ID); n.Yes || n.Turn != TurnAgent {
		t.Fatalf("need = %+v", n)
	}
	// No check yet: refused. Another agent: refused.
	if err := sendErr(t, f.worker, a.ID); err == nil || !strings.Contains(err.Error(), "no pre-send check") {
		t.Fatalf("no check: %v", err)
	}
	chk := mustRun(t, IOSendCheck, f.worker, IOSendCheckInput{ID: a.ID})
	if !chk.OK {
		t.Fatalf("check = %+v", chk)
	}
	if err := sendErr(t, f.agent, a.ID); err == nil || !strings.Contains(err.Error(), "is for agent worker-q") {
		t.Fatalf("other agent: %v", err)
	}
	// A stale check: refused; a fresh one: sent, with the rule recorded.
	f.rv.advance(31 * time.Minute)
	if err := sendErr(t, f.worker, a.ID); err == nil || !strings.Contains(err.Error(), "older than") {
		t.Fatalf("stale: %v", err)
	}
	mustRun(t, IOSendCheck, f.worker, IOSendCheckInput{ID: a.ID})
	sent := mustRun(t, IOSend, f.worker, IOSendInput{ID: a.ID, URL: "https://chat.example.test/m/ack1"})
	sdoc, sm := personaIO(t, f.owner, a.ID)
	if sent.State != model.IOSent || sm.AllowedBy != "rule 1 (chat acks)" || sm.Voice != model.VoiceForMe {
		t.Fatalf("sent = %+v %+v", sent, sm)
	}
	rs := mustRun(t, ReceiptList, f.owner, ReceiptListInput{Doc: a.ID, Op: "io_send"}).Items
	if len(rs) != 1 || !strings.Contains(rs[0].Why, "allowed by delegation rule 1 (chat acks)") {
		t.Fatalf("receipts = %+v", rs)
	}
	// Outward seen at its URL is recorded: not unreviewed_outward.
	if got := unreviewedOutward("## Outward seen\n- 2026-11-01T09:00:00Z https://chat.example.test/m/ack1 a chat post", recordedURLs(map[string]model.Document{sdoc.ID: sdoc})); len(got) != 0 {
		t.Fatalf("unreviewed = %+v", got)
	}
	// The Outbox audit: sent for you.
	l := mustRun(t, IOList, f.owner, IOListInput{Direction: model.IOOut, Voice: model.VoiceForMe, All: true})
	if len(l.Items) != 1 || l.Items[0].ID != a.ID || l.Items[0].AllowedBy == "" || l.Items[0].Stamp == "" {
		t.Fatalf("for_me list = %+v", l.Items)
	}
	mustLintClean(t, f.owner)
}

func TestDelegationRuleMisses(t *testing.T) {
	f := newDelegationFixture(t)
	writeDelegation(t, f.ioPersona, delegationRules)
	other := mustRun(t, OrgPersonAdd, f.owner, OrgAddInput{Name: "Lee Example", Org: "example.test"}).ID
	cases := []struct {
		name string
		in   IOAddInput
		d    Deps
		want string
	}{
		{"kind", IOAddInput{Text: "Status: all green.", Kind: "status"}, f.worker, `kind "status"`},
		{"medium", IOAddInput{Text: "Got it.", Kind: "acknowledgement", Medium: model.MediumEmail}, f.worker, "medium email"},
		{"recipient", IOAddInput{Text: "Got it.", Kind: "acknowledgement", To: []string{other}}, f.worker, "a recipient"},
		{"length", IOAddInput{Text: strings.Repeat("long ", 30), Kind: "acknowledgement"}, f.worker, "max_chars 80"},
		{"agent", IOAddInput{Text: "Got it.", Kind: "acknowledgement"}, f.agent, `agent "claude"`},
	}
	for _, c := range cases {
		in := c.in
		in.Direction, in.Expects, in.Voice = model.IOOut, model.ExpectsNone, model.VoiceForMe
		if in.Medium == "" {
			in.Medium = model.MediumSlack
		}
		if in.To == nil {
			in.To = []string{f.pat}
		}
		a := mustRun(t, IOAdd, c.d, in)
		run := mustRun(t, DelegationRun, c.d, DelegationRunInput{ID: a.ID})
		if run.Rule != "" || len(run.Misses) == 0 || !strings.Contains(run.Misses[0].Why, c.want) {
			t.Errorf("%s: run = %+v", c.name, run)
		}
		if err := sendErr(t, c.d, a.ID); err == nil || !strings.Contains(err.Error(), "no delegation rule") {
			t.Errorf("%s: send: %v", c.name, err)
		}
	}
	// A recipient address pattern and a rule stamp: rule > agent.
	b := mustRun(t, IOAdd, f.worker, IOAddInput{Direction: model.IOOut, Text: "Received, thank you.", To: []string{"kim@outside.example.test"},
		Medium: model.MediumEmail, Expects: model.ExpectsNone, Voice: model.VoiceForMe})
	if _, m := personaIO(t, f.owner, b.ID); m.AllowedBy != "rule 2 (outside mail)" || m.Stamp != "-- worker-q for the owner, rule 2 (outside mail)" {
		t.Fatalf("pattern = %+v", m)
	}
	// An agent with no stamp of its own: the file default.
	c := mustRun(t, IOAdd, f.agent, IOAddInput{Direction: model.IOOut, Text: "Thanks.", To: []string{f.pat}, Medium: model.MediumSlack,
		Expects: model.ExpectsNone, Voice: model.VoiceForMe})
	if _, m := personaIO(t, f.owner, c.ID); m.Stamp != "-- sent by claude for the owner (file default)" {
		t.Fatalf("file default = %q", m.Stamp)
	}
	// The dry run changes nothing, and is a read.
	before, _ := f.owner.Store.Get(t.Context(), c.ID)
	mustRun(t, DelegationRun, f.agent, DelegationRunInput{ID: c.ID})
	after, _ := f.owner.Store.Get(t.Context(), c.ID)
	if before.Rev != after.Rev || DelegationRun.Risk() != RiskRead {
		t.Fatal("delegation_run wrote")
	}
}

func TestForMeStampIsKept(t *testing.T) {
	f := newDelegationFixture(t)
	writeDelegation(t, f.ioPersona, delegationRules)
	a := f.draft(t, "Got it.", "acknowledgement")
	_, m := personaIO(t, f.owner, a.ID)
	// An edit that drops the stamp is refused; one that keeps it is not.
	if _, err := IOEdit.Run(t.Context(), f.worker, IOEditInput{ID: a.ID, Text: "Got it, will do."}); err == nil || !strings.Contains(err.Error(), "ends with its stamp") {
		t.Fatalf("removed: %v", err)
	}
	mustRun(t, IOEdit, f.worker, IOEditInput{ID: a.ID, Text: "Got it, will do.\n\n" + m.Stamp})
	// A stamp taken off by hand: the check and the send refuse it.
	doc, _ := personaIO(t, f.owner, a.ID)
	doc.Body = "Got it, will do."
	if _, err := f.owner.Store.Update(t.Context(), doc); err != nil {
		t.Fatal(err)
	}
	if _, err := IOSendCheck.Run(t.Context(), f.worker, IOSendCheckInput{ID: a.ID}); err == nil || !strings.Contains(err.Error(), "does not end with its stamp") {
		t.Fatalf("check: %v", err)
	}
	if err := sendErr(t, f.worker, a.ID); err == nil || !strings.Contains(err.Error(), "does not end with its stamp") {
		t.Fatalf("send: %v", err)
	}
	// voice for_me stamps it again.
	mustRun(t, IOEdit, f.worker, IOEditInput{ID: a.ID, Voice: model.VoiceForMe})
	if doc, m := personaIO(t, f.owner, a.ID); doc.Body != "Got it, will do.\n\n"+m.Stamp {
		t.Fatalf("restamped = %q", doc.Body)
	}
	// A rule's stamp that changes: the next edit stamps it again, not twice.
	writeDelegation(t, f.ioPersona, strings.Replace(delegationRules, "    agent: [worker-q]\n", "    agent: [worker-q]\n    stamp: \"-- acks by {agent}\"\n", 1))
	mustRun(t, IOEdit, f.worker, IOEditInput{ID: a.ID, Kind: "scheduling"})
	if doc, m := personaIO(t, f.owner, a.ID); doc.Body != "Got it, will do.\n\n-- acks by worker-q" || m.Stamp != "-- acks by worker-q" {
		t.Fatalf("new rule stamp = %q", doc.Body)
	}
	mustLintClean(t, f.owner)
}

func TestForMeHeldGoesToTheOwner(t *testing.T) {
	f := newDelegationFixture(t)
	writeDelegation(t, f.ioPersona, delegationRules)
	f.in(t, f.pat, "Can we move to Tuesday?", "t-ack", model.ExpectsResponse, "2026-11-01T08:00:00Z")
	a := f.draft(t, "Tuesday works.", "scheduling")
	// A message that came after the draft, in its thread: held.
	f.in(t, f.pat, "Actually, Wednesday.", "t-ack", "", "2026-11-01T09:30:00Z")
	chk := mustRun(t, IOSendCheck, f.worker, IOSendCheckInput{ID: a.ID})
	if !chk.Held || !strings.Contains(chk.Reason, "since the agent drafted it") {
		t.Fatalf("check = %+v", chk)
	}
	if err := sendErr(t, f.worker, a.ID); err == nil || !strings.Contains(err.Error(), "held") {
		t.Fatalf("send held: %v", err)
	}
	if n := needOf(t, f.owner, a.ID); !n.Yes || !strings.HasPrefix(n.Reason, "Situation changed") {
		t.Fatalf("need = %+v", n)
	}
}

func TestOwnerApprovesForMeAsMe(t *testing.T) {
	f := newDelegationFixture(t)
	writeDelegation(t, f.ioPersona, delegationRules)
	a := f.draft(t, "Got it.", "acknowledgement")
	out := mustRun(t, IOApprove, f.owner, IOApproveInput{ID: a.ID})
	doc, m := personaIO(t, f.owner, a.ID)
	if m.Voice != "" || m.Stamp != "" || m.Agent != "" || m.AllowedBy != "" || doc.Body != "Got it." ||
		out.ApprovedHash != m.DraftHash("Got it.") || !m.Approved(doc.Body) {
		t.Fatalf("approved = %+v %q", m, doc.Body)
	}
	// It is the owner's now: any agent sends it after the check, no rule.
	mustRun(t, IOSendCheck, f.agent, IOSendCheckInput{ID: a.ID})
	if s := mustRun(t, IOSend, f.agent, IOSendInput{ID: a.ID, URL: "https://chat.example.test/m/own"}); s.State != model.IOSent {
		t.Fatalf("send = %+v", s)
	}
	if _, m := personaIO(t, f.owner, a.ID); m.AllowedBy != "" {
		t.Fatalf("allowed_by on an as_me send: %q", m.AllowedBy)
	}
	// The owner sets for_me explicitly, naming the agent; then as_me takes it off.
	b := mustRun(t, IOAdd, f.owner, IOAddInput{Direction: model.IOOut, Text: "See you then.", To: []string{f.pat}, Medium: model.MediumSlack, Expects: model.ExpectsNone})
	if _, err := IOEdit.Run(t.Context(), f.owner, IOEditInput{ID: b.ID, Voice: model.VoiceForMe}); err == nil || !strings.Contains(err.Error(), "needs agent") {
		t.Fatalf("no agent: %v", err)
	}
	mustRun(t, IOEdit, f.owner, IOEditInput{ID: b.ID, Voice: model.VoiceForMe, Agent: "agent:worker-q", Kind: "acknowledgement"})
	if doc, m := personaIO(t, f.owner, b.ID); m.Agent != "worker-q" || m.AllowedBy != "rule 1 (chat acks)" || !strings.HasSuffix(doc.Body, m.Stamp) {
		t.Fatalf("owner for_me = %+v %q", m, doc.Body)
	}
	mustRun(t, IOEdit, f.owner, IOEditInput{ID: b.ID, Voice: model.VoiceAsMe})
	if doc, m := personaIO(t, f.owner, b.ID); m.Voice != "" || doc.Body != "See you then." {
		t.Fatalf("as_me = %+v %q", m, doc.Body)
	}
	mustLintClean(t, f.owner)
}

func TestOwnerApprovesForMeKeepingTheStamp(t *testing.T) {
	f := newDelegationFixture(t)
	// No delegation file: the draft waits for the owner.
	a := f.draft(t, "Got it.", "acknowledgement")
	stamp := func() (string, *model.IO) {
		doc, m := personaIO(t, f.owner, a.ID)
		return doc.Body, m
	}
	before, m0 := stamp()
	mustRun(t, IOApprove, f.owner, IOApproveInput{ID: a.ID, Voice: model.VoiceForMe})
	body, m := stamp()
	if body != before || m.Voice != model.VoiceForMe || m.Agent != "worker-q" || m.Stamp != m0.Stamp || m.AllowedBy != "" || !m.Approved(body) {
		t.Fatalf("approved = %+v %q", m, body)
	}
	if n := needOf(t, f.owner, a.ID); n.Yes && strings.Contains(n.Reason, "approve") {
		t.Fatalf("need = %+v", n)
	}
	mustRun(t, IOSendCheck, f.worker, IOSendCheckInput{ID: a.ID})
	if s := mustRun(t, IOSend, f.worker, IOSendInput{ID: a.ID, URL: "https://chat.example.test/m/kept"}); s.State != model.IOSent {
		t.Fatalf("send = %+v", s)
	}
	body, m = stamp()
	if !strings.HasSuffix(body, m.Stamp) || m.Voice != model.VoiceForMe || m.AllowedBy != "" {
		t.Fatalf("sent = %+v %q", m, body)
	}
}

func TestApproveForMeRefusedOnAsMeDraft(t *testing.T) {
	f := newDelegationFixture(t)
	b := mustRun(t, IOAdd, f.owner, IOAddInput{Direction: model.IOOut, Text: "See you then.", To: []string{f.pat}, Medium: model.MediumSlack, Expects: model.ExpectsNone})
	_, err := IOApprove.Run(t.Context(), f.owner, IOApproveInput{ID: b.ID, Voice: model.VoiceForMe})
	if err == nil || !strings.Contains(err.Error(), "approve it with voice as_me") {
		t.Fatalf("err = %v", err)
	}
	if _, m := personaIO(t, f.owner, b.ID); m.State == model.IOReady {
		t.Fatal("refused draft was approved")
	}
}
