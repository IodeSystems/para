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

func rfxIDs(w RFXWaitingOutput) []string {
	var out []string
	for _, x := range w.Items {
		out = append(out, x.ID)
	}
	return out
}
