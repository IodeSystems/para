package ops

import (
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

func ioFix(t *testing.T) *agentFix {
	t.Helper()
	f := newAgentFix(t)
	mustRun(t, OrgPersonAdd, f.owner, OrgAddInput{Name: "Ada Example", Org: "acme.example"})
	return f
}

func outDraft(t *testing.T, f *agentFix, expects model.IOExpects, route string) string {
	t.Helper()
	return mustRun(t, IOAdd, f.owner, IOAddInput{Direction: model.IOOut, Text: "Hi Ada,\n\ncan you send the quote?", Subject: "Quote",
		To: []string{"person-ada-example", "billing@vendor.example"}, Medium: model.MediumEmail, Expects: expects, Route: route}).ID
}

func TestIOAddChecks(t *testing.T) {
	f := ioFix(t)
	cases := []struct {
		name string
		in   IOAddInput
		want string
	}{
		{"no direction", IOAddInput{Text: "x"}, "direction"},
		{"no text", IOAddInput{Direction: model.IOIn}, "text is empty"},
		{"in route", IOAddInput{Direction: model.IOIn, Text: "x", Route: "domain-logging"}, "outgoing"},
		{"in bad expects", IOAddInput{Direction: model.IOIn, Text: "x", Expects: "maybe"}, "expects"},
		{"bad effort", IOAddInput{Direction: model.IOIn, Text: "x", Effort: "huge"}, "effort"},
		{"out no expects", IOAddInput{Direction: model.IOOut, Text: "x", To: []string{"person-ada-example"}}, "expects"},
		{"out no person", IOAddInput{Direction: model.IOOut, Text: "x", To: []string{"ada@acme.example"}, Expects: model.ExpectsResponse, Route: "domain-logging"}, "person or group"},
		{"action to nowhere", IOAddInput{Direction: model.IOOut, Text: "x", Expects: model.ExpectsNone}, "person or group"},
		{"response no route", IOAddInput{Direction: model.IOOut, Text: "x", To: []string{"person-ada-example"}, Expects: model.ExpectsResponse}, "route"},
		{"link to a task", IOAddInput{Direction: model.IOIn, Text: "x", From: "[[" + f.proj + "]]"}, "want a person"},
		{"missing link", IOAddInput{Direction: model.IOIn, Text: "x", From: "[[person-nobody]]"}, "person-nobody"},
		{"half via", IOAddInput{Direction: model.IOIn, Text: "x", Via: &model.IOVia{Source: "gmail"}}, "via"},
		{"bad at", IOAddInput{Direction: model.IOIn, Text: "x", At: "yesterday"}, "RFC 3339"},
		{"thread to a goal", IOAddInput{Direction: model.IOIn, Text: "x", Thread: f.proj}, "thread"},
	}
	for _, c := range cases {
		if _, err := IOAdd.Run(t.Context(), f.owner, c.in); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want %q", c.name, err, c.want)
		}
	}
	in := mustRun(t, IOAdd, f.owner, IOAddInput{Direction: model.IOIn, Text: "Your invoice\nis attached.", From: "person-ada-example",
		To: []string{"me@acme.example"}, Medium: model.MediumEmail, Ref: "https://mail.acme.example/m/1",
		Via: &model.IOVia{Source: "gmail", Ref: "18c0ffee"}, At: "2026-11-04T08:00:00Z"})
	show := mustRun(t, IOShow, f.owner, IOShowInput{ID: in.ID})
	if show.Doc.Title != "Your invoice" || show.Item.From != "[[person-ada-example]]" || show.Item.To[0] != "me@acme.example" || show.Item.State != model.IOUnread {
		t.Fatalf("show = %+v", show)
	}
	mustLintClean(t, f.owner)
}

func TestIOSendLogsAnInteraction(t *testing.T) {
	f := ioFix(t)
	id := outDraft(t, f, model.ExpectsNone, "")
	if _, err := IOSend.Run(t.Context(), f.owner, IOSendInput{ID: id}); err == nil {
		t.Fatal("sent with no url or locator")
	}
	out := mustRun(t, IOSend, f.owner, IOSendInput{ID: id, URL: "https://mail.acme.example/sent/1", At: "2026-11-04T10:00:00Z"})
	if out.State != model.IOSent || out.BecameType != model.TypeInteraction {
		t.Fatalf("send = %+v", out)
	}
	doc, err := f.owner.Store.Get(t.Context(), out.Became)
	m, _ := doc.Meta.(*model.Interaction)
	if err != nil || m == nil || m.Direction != model.InteractionOut || m.Date != "2026-11-04" || m.URL != "https://mail.acme.example/sent/1" ||
		len(m.With) != 1 || m.With[0] != "[[person-ada-example]]" || !strings.Contains(doc.Body, "[["+id+"]]") {
		t.Fatalf("interaction = %+v %+v %v", doc, m, err)
	}
	_, io, _ := getIO(t.Context(), f.owner, id)
	if io.Became != model.LinkTo(out.Became) || io.At != "2026-11-04T10:00:00Z" || io.Ref != "https://mail.acme.example/sent/1" {
		t.Fatalf("io = %+v", io)
	}
	again := mustRun(t, IOSend, f.owner, IOSendInput{ID: id, URL: "https://mail.acme.example/sent/2"})
	if again.Became != out.Became {
		t.Fatalf("a second report made %s", again.Became)
	}
	if got := mustRun(t, IOList, f.owner, IOListInput{Direction: model.IOOut}); len(got.Items) != 0 {
		t.Fatalf("outbox still lists the sent message: %+v", got.Items)
	}
	mustLintClean(t, f.owner)
}

// An action (a PR, a ticket) is a draft to where it lands, not to a person:
// it is approved and sent like a message, and its send logs nothing else.
func TestIOSendOfAnAction(t *testing.T) {
	f := ioFix(t)
	id := mustRun(t, IOAdd, f.owner, IOAddInput{Direction: model.IOOut, Text: "What: open a PR\nHow: bump the version", Subject: "Bump",
		To: []string{"github.com/acme/site"}, Medium: model.MediumGithub, Expects: model.ExpectsNone}).ID
	mustRun(t, IOApprove, f.owner, IOApproveInput{ID: id})
	out := mustRun(t, IOSend, f.owner, IOSendInput{ID: id, URL: "https://github.example/acme/site/pull/2"})
	if out.State != model.IOSent || out.Became != "" || out.BecameType != "" {
		t.Fatalf("send = %+v", out)
	}
	_, io, _ := getIO(t.Context(), f.owner, id)
	if io.Became != "" || io.Ref != "https://github.example/acme/site/pull/2" {
		t.Fatalf("io = %+v", io)
	}
	mustLintClean(t, f.owner)
}

func TestIOSendOpensAnAsk(t *testing.T) {
	f := ioFix(t)
	id := outDraft(t, f, model.ExpectsResponse, "billing")
	out := mustRun(t, IOSend, f.owner, IOSendInput{ID: id, Locator: "Gmail, Sent, 2026-11-04 10:00"})
	if out.BecameType != model.TypeRFX {
		t.Fatalf("send = %+v", out)
	}
	doc, err := f.owner.Store.Get(t.Context(), out.Became)
	m, _ := doc.Meta.(*model.RFX)
	if err != nil || m == nil || m.Direction != model.RFXOut || m.Route != "billing" || len(m.Targets) != 1 {
		t.Fatalf("rfx = %+v %v", doc, err)
	}
	if tg := m.Targets[0]; tg.State != model.TargetSent || tg.Ref != "Gmail, Sent, 2026-11-04 10:00" || tg.Medium != model.MediumEmail || tg.Sent == "" {
		t.Fatalf("target = %+v", tg)
	}
	if doc.Title != "Quote" || !strings.Contains(doc.Body, "can you send the quote?") {
		t.Fatalf("ask = %q / %q", doc.Title, doc.Body)
	}
	if w := mustRun(t, RFXWaiting, f.owner, RFXWaitingInput{}); !strings.Contains(strings.Join(rfxIDs(w), ","), out.Became) {
		t.Fatalf("the Ask is not waiting: %+v", w)
	}
	mustLintClean(t, f.owner)
}

func TestIOEditDraft(t *testing.T) {
	f := ioFix(t)
	id := outDraft(t, f, model.ExpectsNone, "")
	if e := mustRun(t, IOEdit, f.owner, IOEditInput{ID: id, State: model.IOReady}); e.State != model.IOReady {
		t.Fatalf("edit = %+v", e)
	}
	if e := mustRun(t, IOEdit, f.owner, IOEditInput{ID: id, Text: "Hi Ada, thanks."}); e.State != model.IODraft {
		t.Fatalf("an edited ready message stayed %s", e.State)
	}
	if e := mustRun(t, IOEdit, f.owner, IOEditInput{ID: id, Subject: "Thanks", State: model.IOReady}); e.State != model.IOReady {
		t.Fatalf("edit with ready = %+v", e)
	}
	if _, err := IOEdit.Run(t.Context(), f.owner, IOEditInput{ID: id, Expects: model.ExpectsResponse}); err == nil || !strings.Contains(err.Error(), "route") {
		t.Fatalf("expects response with no route: %v", err)
	}
	if _, err := IOEdit.Run(t.Context(), f.owner, IOEditInput{ID: id, State: model.IOSent}); err == nil {
		t.Fatal("io_edit sent a message")
	}
	doc, m, _ := getIO(t.Context(), f.owner, id)
	if doc.Title != "Thanks" || doc.Body != "Hi Ada, thanks." || m.Expects != model.ExpectsNone {
		t.Fatalf("draft = %q %q %+v", doc.Title, doc.Body, m)
	}
	out := mustRun(t, IOList, f.owner, IOListInput{Direction: model.IOOut, Expects: model.ExpectsNone})
	if len(out.Items) != 1 || out.Counts.Ready != 1 {
		t.Fatalf("outbox = %+v", out)
	}
}

// A message block that names an io draft shows the draft's text, writes the
// owner's edit to the draft, makes it ready on approval, and sends it when the
// agent reports it done.
func TestDossierMessageNamesIODraft(t *testing.T) {
	f := ioFix(t)
	io1 := outDraft(t, f, model.ExpectsNone, "")
	io2 := outDraft(t, f, model.ExpectsNone, "")
	ev := "evidence: {quote: \"Ship it.\", locator: \"https://acme.example/notes#L1\"}\n"
	block := func(id, ioID string) string {
		return "```yaml para-message id=" + id + "\nio: \"[[" + ioID + "]]\"\n" + ev + "```\n\n"
	}
	header := "```yaml para-dossier\ntldr: Ask Ada for the quote\n```\n\n## Messages\n\n"
	bad := header + "```yaml para-message id=m1\nio: \"[[" + io1 + "]]\"\nbody: Hi.\n" + ev + "```\n"
	if _, err := ProposalAdd.Run(writeCtx(t), f.agent, ProposalAddInput{Title: "Quote", Markdown: bad}); err == nil || !strings.Contains(err.Error(), "io draft") {
		t.Fatalf("a block with io and body: %v", err)
	}
	in := mustRun(t, IOAdd, f.owner, IOAddInput{Direction: model.IOIn, Text: "incoming"}).ID
	if _, err := ProposalAdd.Run(writeCtx(t), f.agent, ProposalAddInput{Title: "Quote", Markdown: header + block("m1", in)}); err == nil || !strings.Contains(err.Error(), "incoming") {
		t.Fatalf("a block naming an incoming item: %v", err)
	}
	id := fileDossierMD(t, f, header+block("m1", io1)+block("m2", io2))
	raw, _ := f.owner.Store.Get(t.Context(), id)
	if strings.Contains(raw.Body, "can you send the quote?") {
		t.Fatal("the dossier file holds a copy of the draft's text")
	}
	show := mustRun(t, ProposalShow, f.owner, ProposalIDInput{ID: id})
	if show.Items[0].Body != "Hi Ada,\n\ncan you send the quote?" || show.Items[0].Subject != "Quote" || len(show.Items[0].People) != 2 {
		t.Fatalf("m1 = %+v", show.Items[0])
	}
	if _, err := draft(t, f.owner, id, 1,
		DraftAnswer{Item: "m1", Answer: &model.ItemAnswer{Decision: model.AnswerEdit, Body: "Hi Ada,\n\nplease send the quote by Friday.\n"}},
		DraftAnswer{Item: "m2", Answer: &model.ItemAnswer{Decision: model.AnswerApprove}}); err != nil {
		t.Fatal(err)
	}
	if _, m, _ := getIO(t.Context(), f.owner, io1); m.State != model.IODraft {
		t.Fatal("a draft answer changed the io item")
	}
	if sub := mustRun(t, ProposalSubmit, f.owner, ProposalSubmitInput{ID: id, Revision: 1}); len(sub.Failed) != 0 {
		t.Fatalf("submit failed: %v", sub.Failed)
	}
	doc, m, _ := getIO(t.Context(), f.owner, io1)
	if m.State != model.IOReady || doc.Body != "Hi Ada,\n\nplease send the quote by Friday." {
		t.Fatalf("io1 after submit = %s %q", m.State, doc.Body)
	}
	if _, m, _ := getIO(t.Context(), f.owner, io2); m.State != model.IOReady {
		t.Fatalf("io2 after submit = %s", m.State)
	}
	// An edit after approval stops the report.
	mustRun(t, IOEdit, f.owner, IOEditInput{ID: io2, Text: "Changed after approval."})
	if _, err := ProposalExecuted.Run(t.Context(), f.agent, ProposalExecutedInput{ID: id, Item: "m2", Result: ExecDone, URL: "https://mail.acme.example/2"}); err == nil ||
		!strings.Contains(err.Error(), "changed since it was approved") {
		t.Fatalf("executed after an edit: %v", err)
	}
	// The approved text is reported done: the draft is sent and filed.
	ex := mustRun(t, ProposalExecuted, f.agent, ProposalExecutedInput{ID: id, Item: "m1", Result: ExecDone, URL: "https://mail.acme.example/1"})
	if ex.IO == nil || ex.IO.State != model.IOSent || ex.IO.BecameType != model.TypeInteraction {
		t.Fatalf("executed = %+v", ex)
	}
	if _, m, _ := getIO(t.Context(), f.owner, io1); m.State != model.IOSent || m.Became != model.LinkTo(ex.IO.Became) {
		t.Fatalf("io1 = %+v", m)
	}
	mustLintClean(t, f.owner)
}

func rfxIDs(w RFXWaitingOutput) []string {
	var out []string
	for _, x := range w.Items {
		out = append(out, x.ID)
	}
	return out
}

// Inbox triage in a dossier: a change item names io_file and runs on submit.
func TestDossierFilesIO(t *testing.T) {
	f := ioFix(t)
	in := mustRun(t, IOAdd, f.owner, IOAddInput{Direction: model.IOIn, Text: "Renew the cert", From: "person-ada-example"}).ID
	ev := "evidence: {quote: \"Ship it.\", locator: \"https://acme.example/notes#L1\"}\n"
	md := "```yaml para-dossier\ntldr: Sort the inbox\n```\n\n" +
		"```yaml para-change id=c1\nop: io_file\ninput: {id: " + in + ", to: task, project: " + f.proj + ", done_when: Renewed.}\nhow: file as a task\nimpact: one task\ngroup: io_file\n" + ev + "```\n"
	id := fileDossierMD(t, f, md)
	if _, err := draft(t, f.owner, id, 1, DraftAnswer{Item: "c1", Answer: &model.ItemAnswer{Decision: model.AnswerApprove}}); err != nil {
		t.Fatal(err)
	}
	if sub := mustRun(t, ProposalSubmit, f.owner, ProposalSubmitInput{ID: id, Revision: 1}); len(sub.Failed) != 0 {
		t.Fatalf("failed: %v", sub.Failed)
	}
	_, m, _ := getIO(t.Context(), f.owner, in)
	if m.State != model.IOFiled || !strings.HasPrefix(m.Became.Target(), "task-") {
		t.Fatalf("io = %+v", m)
	}
}
