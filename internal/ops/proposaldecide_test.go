package ops

import (
	"errors"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

func decideOne(item, decision, text string) []DecideItem {
	return []DecideItem{{Item: item, Decision: decision, Text: text}}
}

func TestDecideNeedsOwnerSaid(t *testing.T) {
	f := newPropFix(t)
	id := f.add(paraTask(f, "Needs words"))
	for _, said := range []string{"", "  \n "} {
		_, err := ProposalDecide.Run(t.Context(), f.agent, ProposalDecideInput{ID: id, OwnerSaid: said, Items: decideOne("i1", model.DecideApprove, "")})
		if err == nil || !strings.Contains(err.Error(), "owner_said") {
			t.Fatalf("owner_said %q: %v", said, err)
		}
	}
	for name, items := range map[string][]DecideItem{
		"no items":       nil,
		"bad decision":   decideOne("i1", "maybe", ""),
		"unknown item":   decideOne("i9", model.DecideApprove, ""),
		"change no text": decideOne("i1", model.DecideChange, " "),
		"twice":          {{Item: "i1", Decision: model.DecideApprove}, {Item: "i1", Decision: model.DecideDeny}},
	} {
		if _, err := ProposalDecide.Run(t.Context(), f.agent, ProposalDecideInput{ID: id, OwnerSaid: "yes", Items: items}); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	// The web app reads; it does not decide.
	pwa := Deps{Store: f.owner.Store.ActAs("user", "pwa")}
	if _, err := ProposalDecide.Run(t.Context(), pwa, ProposalDecideInput{ID: id, OwnerSaid: "yes", Items: decideOne("i1", model.DecideApprove, "")}); !errors.Is(err, ErrPersonOnly) {
		t.Errorf("pwa: %v", err)
	}
	if tasks, _ := TaskList.Run(t.Context(), f.owner, TaskListInput{}); len(tasks.Tasks) != 0 {
		t.Fatalf("a refused decision ran: %+v", tasks.Tasks)
	}
}

func TestDecideApproveRunsParaItemWithReceipt(t *testing.T) {
	f := newPropFix(t)
	id := f.add(paraTask(f, "From the chat"))
	said := "Yes, add that task."
	out, err := ProposalDecide.Run(t.Context(), f.agent, ProposalDecideInput{ID: id, OwnerSaid: said, Items: decideOne("i1", model.DecideApprove, "")})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 1 || out.Items[0].State != model.ItemApproved || out.Items[0].Result == "" || out.Items[0].OwnerSaid != said || out.Status != model.ProposalClosed {
		t.Fatalf("out = %+v", out)
	}
	show, err := ProposalShow.Run(t.Context(), f.owner, ProposalIDInput{ID: id})
	if err != nil {
		t.Fatal(err)
	}
	dec := show.Items[0].Decided
	if dec == nil || dec.Decision != model.DecideApprove || dec.OwnerSaid != said || !dec.SelfReported || dec.By != "claude" {
		t.Fatalf("decided = %+v", dec)
	}
	// The task exists, made under the proposing agent, with a receipt that
	// carries the owner's words and is self-reported.
	task := out.Items[0].Result
	rl, err := ReceiptList.Run(t.Context(), f.owner, ReceiptListInput{Doc: task})
	if err != nil {
		t.Fatal(err)
	}
	if len(rl.Items) != 1 || rl.Items[0].Op != "task_add" || rl.Items[0].OwnerSaid != said || !rl.Items[0].SelfReported || rl.Items[0].Actor != "claude" {
		t.Fatalf("receipts = %+v", rl.Items)
	}
	// Events hold ids and states, never the owner's words.
	evs, err := f.owner.Store.Events(t.Context(), 1000)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range evs {
		if strings.Contains(e.From+e.To+e.Hash, "add that task") {
			t.Fatalf("event holds the owner's words: %+v", e)
		}
	}
}

func TestDecideApproveOutwardStaysNotDone(t *testing.T) {
	f := newPropFix(t)
	id := f.add(reviewItem("Hi Ada."))
	out, err := ProposalDecide.Run(t.Context(), f.agent, ProposalDecideInput{ID: id, OwnerSaid: "Send it.", Items: decideOne("i1", model.DecideApprove, "")})
	if err != nil {
		t.Fatal(err)
	}
	if out.Items[0].State != model.ItemApproved || out.Items[0].ApprovedHash == "" {
		t.Fatalf("out = %+v", out.Items)
	}
	un, err := ProposalUndone.Run(t.Context(), f.owner, ProposalUndoneInput{})
	if err != nil || len(un.Items) != 1 || un.Items[0].Item != "i1" {
		t.Fatalf("undone = %+v %v", un.Items, err)
	}
	done, err := ProposalExecuted.Run(t.Context(), f.agent, ProposalExecutedInput{ID: id, Item: "i1", Result: ExecDone, URL: "https://mail.acme.example/m/1"})
	if err != nil || done.State != model.ItemDone || !done.SelfReported {
		t.Fatalf("executed = %+v %v", done, err)
	}
	r, err := ReceiptShow.Run(t.Context(), f.owner, ReceiptIDInput{ID: done.Receipt})
	if err != nil || r.Receipt.OwnerSaid != "Send it." {
		t.Fatalf("receipt = %+v %v", r.Receipt, err)
	}
	if un, _ := ProposalUndone.Run(t.Context(), f.owner, ProposalUndoneInput{}); len(un.Items) != 0 {
		t.Fatalf("undone after = %+v", un.Items)
	}
}

func TestDecideChangeMakesRevisionStillPending(t *testing.T) {
	f := newPropFix(t)
	id := f.add(reviewItem("Hi Ada."), paraTask(f, "Old title"))
	in := `{"project":"` + f.proj + `","title":"New title","done_when":"It exists."}`
	out, err := ProposalDecide.Run(t.Context(), f.agent, ProposalDecideInput{ID: id, OwnerSaid: "Say hello instead, and rename the task.",
		Items: []DecideItem{{Item: "i1", Decision: model.DecideChange, Text: "Hello Ada."}, {Item: "i2", Decision: model.DecideChange, Text: in}}})
	if err != nil {
		t.Fatal(err)
	}
	if out.Revision == "" || out.Revision == id || out.Status != model.ProposalOpen {
		t.Fatalf("out = %+v", out)
	}
	old, _ := ProposalShow.Run(t.Context(), f.owner, ProposalIDInput{ID: id})
	if old.Proposal.Status != model.ProposalSuperseded || old.SupersededBy != out.Revision {
		t.Fatalf("old = %+v", old.Proposal)
	}
	show, err := ProposalShow.Run(t.Context(), f.owner, ProposalIDInput{ID: out.Revision})
	if err != nil {
		t.Fatal(err)
	}
	a, b := show.Items[0], show.Items[1]
	if a.State != model.ItemPending || a.Body != "Hello Ada." || !a.Edited || a.Decided == nil || a.Decided.Decision != model.DecideChange {
		t.Fatalf("i1 = %+v", a)
	}
	if b.State != model.ItemPending || b.Input["title"] != "New title" {
		t.Fatalf("i2 = %+v", b)
	}
	if show.Proposal.Revision != 2 {
		t.Fatalf("revision = %d", show.Proposal.Revision)
	}
	// A para item takes JSON input only.
	if _, err := ProposalDecide.Run(t.Context(), f.agent, ProposalDecideInput{ID: out.Revision, OwnerSaid: "Rename it.",
		Items: decideOne("i2", model.DecideChange, "call it something else")}); err == nil || !strings.Contains(err.Error(), "JSON") {
		t.Fatalf("free text on a para item: %v", err)
	}
	if tasks, _ := TaskList.Run(t.Context(), f.owner, TaskListInput{}); len(tasks.Tasks) != 0 {
		t.Fatal("a change ran the item")
	}
}

func TestDecideDeny(t *testing.T) {
	f := newPropFix(t)
	a, b := reviewItem("One."), reviewItem("Two.")
	a.Bundle, b.Bundle = "b", "b"
	id := f.add(a, b, paraTask(f, "Keep pending"))
	// A bundle is decided as one.
	if _, err := ProposalDecide.Run(t.Context(), f.agent, ProposalDecideInput{ID: id, OwnerSaid: "Half.",
		Items: []DecideItem{{Item: "i1", Decision: model.DecideApprove}, {Item: "i2", Decision: model.DecideDeny}}}); err == nil {
		t.Fatal("a bundle was split")
	}
	out, err := ProposalDecide.Run(t.Context(), f.agent, ProposalDecideInput{ID: id, OwnerSaid: "No, not now.", Items: decideOne("i1", model.DecideDeny, "")})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 2 || out.Items[0].State != model.ItemDenied || out.Items[1].State != model.ItemDenied || !strings.Contains(out.Items[0].Reason, "not now") {
		t.Fatalf("out = %+v", out.Items)
	}
	if out.Status != model.ProposalOpen {
		t.Fatalf("status = %s", out.Status)
	}
	if _, err := ProposalDecide.Run(t.Context(), f.agent, ProposalDecideInput{ID: id, OwnerSaid: "Yes after all.", Items: decideOne("i1", model.DecideApprove, "")}); err == nil {
		t.Fatal("a denied item was approved")
	}
}

func TestProposalOpen(t *testing.T) {
	f := newPropFix(t)
	id := f.add(reviewItem("Hi."))
	var opened []string
	d := f.agent
	d.ViewerBase = func() (string, error) { return "http://acme.localhost:7799/", nil }
	d.OpenURL = func(u string) (string, error) {
		opened = append(opened, u)
		return "fake-open " + u, nil
	}
	out, err := ProposalOpen.Run(t.Context(), d, ProposalOpenInput{ID: id})
	if err != nil {
		t.Fatal(err)
	}
	want := "http://acme.localhost:7799/proposals/" + id
	if out.URL != want || !out.Opened || out.Command != "fake-open "+want || len(opened) != 1 || opened[0] != want {
		t.Fatalf("out = %+v, opened %v", out, opened)
	}
	// Any other document opens at /doc/<id>.
	if out, _ := ProposalOpen.Run(t.Context(), d, ProposalOpenInput{ID: f.source}); out.URL != "http://acme.localhost:7799/doc/"+f.source {
		t.Fatalf("doc = %+v", out)
	}
	// A URL of the web app opens as given; any other URL is refused.
	if out, err := ProposalOpen.Run(t.Context(), d, ProposalOpenInput{ID: want}); err != nil || out.URL != want {
		t.Fatalf("url = %+v %v", out, err)
	}
	if _, err := ProposalOpen.Run(t.Context(), d, ProposalOpenInput{ID: "https://evil.example/x"}); err == nil {
		t.Fatal("opened a foreign URL")
	}
	if _, err := ProposalOpen.Run(t.Context(), d, ProposalOpenInput{ID: "proposal-nope"}); err == nil {
		t.Fatal("opened a missing document")
	}
	// An opener that fails still returns the URL.
	d.OpenURL = func(string) (string, error) { return "xdg-open", errors.New("no display") }
	if out, _ := ProposalOpen.Run(t.Context(), d, ProposalOpenInput{ID: id}); out.Opened || out.URL != want || !strings.Contains(out.Note, "no display") {
		t.Fatalf("failed opener = %+v", out)
	}
	// No server: say so, with the command to start one; nothing opens.
	opened = nil
	d.ViewerBase = func() (string, error) { return "", ErrNoServer }
	d.OpenURL = func(u string) (string, error) { opened = append(opened, u); return "", nil }
	out, err = ProposalOpen.Run(t.Context(), d, ProposalOpenInput{ID: id})
	if err != nil || out.Opened || out.URL != "" || !strings.Contains(out.Note, "para serve") || len(opened) != 0 {
		t.Fatalf("no server = %+v %v", out, err)
	}
	// proposal_add names the page when a server is known.
	filed, err := ProposalAdd.Run(t.Context(), Deps{Store: f.agent.Store, ViewerBase: func() (string, error) { return "http://acme.localhost:7799", nil }},
		ProposalAddInput{Title: "Again", TLDR: "t", Context: "c", Source: f.source, Items: []ItemInput{reviewItem("Hi again.")}})
	if err != nil || filed.ReviewURL != "http://acme.localhost:7799/proposals/"+filed.ID || filed.ReviewCommand != "para open "+filed.ID {
		t.Fatalf("filed = %+v %v", filed, err)
	}
}
