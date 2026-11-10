package ops

import (
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

func TestResolveRefs(t *testing.T) {
	f := newAgentFix(t)
	ctx := t.Context()
	mail, err := SourceAdd.Run(ctx, f.owner, SourceAddInput{Title: "Work mailbox", Example: model.SourceGmail})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SourceAdd.Run(ctx, f.owner, SourceAddInput{Title: "Team chat", Example: model.SourceSlack}); err != nil {
		t.Fatal(err)
	}
	got := ResolveRefs(ctx, f.owner.Store, []string{
		"work-mailbox:1a1a", "work-mailbox:1a1a", // repeat
		"gmail:1a1a", // the one source of the system: same address, dropped
		"https://example.com/x", "https://example.com/x",
		"team-chat:C1/p123",          // source without ref_url
		"nope:abc",                   // unknown source
		f.source,                     // vault id
		"sources/" + mail.ID + ".md", // vault path
		"zzz-missing",
	})
	want := []RefLink{
		{Text: "work-mailbox:1a1a", URL: "https://mail.google.com/mail/u/0/#all/1a1a"},
		{Text: "https://example.com/x", URL: "https://example.com/x"},
		{Text: "team-chat:C1/p123 (Team chat)"},
		{Text: "nope:abc"},
		{Text: f.source, URL: "/docs/" + f.source, Internal: true},
		{Text: "sources/" + mail.ID + ".md", URL: "/docs/" + mail.ID, Internal: true},
		{Text: "zzz-missing"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d: got %+v want %+v", i, got[i], want[i])
		}
	}
	if l := got[0].Line(); l != "work-mailbox:1a1a https://mail.google.com/mail/u/0/#all/1a1a" {
		t.Errorf("line = %q", l)
	}
}

func TestResolveRefsTemplateAndURLRef(t *testing.T) {
	f := newAgentFix(t)
	ctx := t.Context()
	if _, err := SourceAdd.Run(ctx, f.owner, SourceAddInput{Title: "Tracker", System: model.SourceJira, RefURL: "http://x.test/{ref}"}); err != nil {
		t.Fatal(err)
	}
	got := ResolveRefs(ctx, f.owner.Store, []string{"tracker:AB-1"})
	if len(got) != 1 || got[0].URL != "http://x.test/AB-1" {
		t.Fatalf("got %+v", got)
	}
	if _, err := SourceAdd.Run(ctx, f.owner, SourceAddInput{Title: "Bad", System: model.SourceJira, RefURL: "javascript:{ref}"}); err == nil {
		t.Fatal("ref_url that is not http(s) must be refused")
	}
}

func TestReceiptSourceLinks(t *testing.T) {
	f := newAgentFix(t)
	if _, err := SourceAdd.Run(t.Context(), f.owner, SourceAddInput{Title: "Work mailbox", Example: model.SourceGmail}); err != nil {
		t.Fatal(err)
	}
	ctx := WithWrite(t.Context(), WriteMeta{Why: "Mail asked.", Sources: []string{"work-mailbox:m1", "work-mailbox:m1"}, Verify: "Open it."})
	task, err := TaskAdd.Run(ctx, f.agent, TaskAddInput{Project: f.proj, Title: "From mail", DoneWhen: "Done."})
	if err != nil {
		t.Fatal(err)
	}
	list, err := ReceiptList.Run(t.Context(), f.owner, ReceiptListInput{Doc: task.ID})
	if err != nil || len(list.Items) == 0 {
		t.Fatalf("%v %v", list, err)
	}
	r := list.Items[0]
	if len(r.Sources) != 2 || len(r.SourceLinks) != 1 || !strings.HasSuffix(r.SourceLinks[0].URL, "#all/m1") {
		t.Fatalf("receipt = %+v", r)
	}
}

func TestTaskHowFieldsAndWarning(t *testing.T) {
	f := newAgentFix(t)
	ctx := t.Context()
	bare, err := TaskAdd.Run(ctx, f.agent, TaskAddInput{Project: f.proj, Title: "Renew the API token acli-download before it expires", DoneWhen: "The token works."})
	if err != nil || !hasWarn(bare.Warnings, "say how to do it") {
		t.Fatalf("agent task with no how: %+v %v", bare, err)
	}
	out, err := TaskAdd.Run(ctx, f.agent, TaskAddInput{Project: f.proj, Title: "Rotate the signing key for the deploy bot", DoneWhen: "The token works.",
		DoURL: "https://id.example.test/manage-profile/security/api-tokens", DoCommand: "acli auth login --token -",
		Steps: "1. Create a token on the page.\n2. Run the command.\n3. Paste the token when asked."})
	if err != nil || hasWarn(out.Warnings, "say how to do it") {
		t.Fatalf("with how: %+v %v", out, err)
	}
	show, err := TaskShow.Run(ctx, f.owner, TaskShowInput{ID: out.ID})
	if err != nil || show.Task.DoURL == "" || show.Task.DoCommand != "acli auth login --token -" || !strings.Contains(show.Task.Steps, "Paste the token") {
		t.Fatalf("show = %+v %v", show.Task, err)
	}
	// A person captures without a how and gets no warning.
	own, err := TaskAdd.Run(ctx, f.owner, TaskAddInput{Project: f.proj, Title: "Pay the invoice"})
	if err != nil || hasWarn(own.Warnings, "say how to do it") {
		t.Fatalf("owner: %+v %v", own, err)
	}
	// task_edit sets the how on its own, keeps done_when, and clears it.
	ed, err := TaskEdit.Run(ctx, f.agent, TaskEditInput{ID: bare.ID, DoCommand: "acli auth status"})
	if err != nil || ed.DoCommand != "acli auth status" || ed.DoneWhen != "The token works." {
		t.Fatalf("edit = %+v %v", ed, err)
	}
	if ed, err = TaskEdit.Run(ctx, f.agent, TaskEditInput{ID: bare.ID, ClearHow: true}); err != nil || ed.DoCommand != "" {
		t.Fatalf("clear = %+v %v", ed, err)
	}
	if _, err := TaskAdd.Run(ctx, f.agent, TaskAddInput{Project: f.proj, Title: "Bad url", DoneWhen: "x.", DoURL: "ftp://x"}); err == nil {
		t.Fatal("do_url must be http(s)")
	}
}

func hasWarn(ws []string, sub string) bool {
	for _, w := range ws {
		if strings.Contains(w, sub) {
			return true
		}
	}
	return false
}
