package tui

import (
	"strings"
	"testing"
)

// An email says what approving it does, and after approval says it is not sent.
func TestExternalItemPromiseAndNotSent(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	id := v.fileProposal(mailItem("Hi Ada."), v.taskItem("Write the docs"))
	d := newDrv(t, v, 120, 30)
	d.openProposal(id)
	if !d.has(externalPromise) {
		t.Fatalf("no promise on a pending email:\n%s", d.plainFrame())
	}
	if d.has("Copy draft") {
		t.Errorf("Copy draft before approval:\n%s", d.plainFrame())
	}
	d.key("a")
	if !d.has(notSentYet) {
		t.Fatalf("approved email does not say it is not sent:\n%s", d.plainFrame())
	}
	// The cursor moved to the next item that waits.
	if it, ok := d.m.proposalItem(d.m.top(), d.m.listWidth()); !ok || it.ID != "i2" {
		t.Fatalf("cursor did not advance: %+v", it)
	}
	// Back on the approved email, Copy draft is offered with a key.
	d.key("k")
	if !strings.Contains(d.footer(), "Copy draft") {
		t.Fatalf("no Copy draft on an approved email:\n%s", d.plainFrame())
	}
	_ = d.keyFor("act:copydraft")
	// Its full view: one line about the hash, no hash, Source behind a key.
	d.key("enter")
	f := d.plainFrame()
	if !strings.Contains(f, coversText) || strings.Contains(f, "sha256:") || !strings.Contains(f, notSentYet) {
		t.Errorf("full view:\n%s", f)
	}
	if !strings.Contains(d.footer(), "Copy draft") || !strings.Contains(d.footer(), "Source") {
		t.Errorf("full view footer: %q", d.footer())
	}
}

// Every decision that applies is in the footer, at every size.
func TestProposalFooterShowsDecisions(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	id := v.fileProposal(mailItem("Hi Ada."))
	for _, size := range [][2]int{{120, 36}, {80, 24}} {
		d := newDrv(t, v, size[0], size[1])
		d.openProposal(id)
		for _, want := range []string{"Approve", "Deny", "Send back", "Note"} {
			if !strings.Contains(d.footer(), want) {
				t.Errorf("%dx%d: footer lacks %s: %q", size[0], size[1], want, d.footer())
			}
		}
	}
}

// Key letters are not inside titles or URLs: the list body holds none.
func TestNoKeyLettersInsideBodyText(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	id := v.fileProposal(mailItem("Hi Ada."), v.taskItem("Order a second staging node pool"))
	for _, size := range [][2]int{{120, 36}, {80, 24}, {40, 20}} {
		d := newDrv(t, v, size[0], size[1])
		check := func(what string) {
			lines := strings.Split(d.m.frame, "\n")
			for _, l := range lines[1 : len(lines)-1] {
				if strings.Contains(l, KeyStyle.On) {
					t.Errorf("%dx%d %s: a key letter in the body: %q", size[0], size[1], what, stripSGR(l))
				}
			}
		}
		check("today")
		d.openProposal(id)
		check("proposal")
	}
}

// A detail page says "ready", not "todo", and a date as Nov 3.
func TestDetailPagesUseTheOneStateAndDateMap(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	d := newDrv(t, v, 120, 40)
	d.run(d.m.loadDoc(v.ids["project"], false))
	f := d.plainFrame()
	if !strings.Contains(f, "ready") || strings.Contains(f, "todo") {
		t.Errorf("project page:\n%s", f)
	}
	d.key("esc")
	d.run(d.m.loadDoc(v.ids["rfx"], false))
	f = d.plainFrame()
	if strings.Contains(f, "2026-") || strings.Contains(f, "_") {
		t.Errorf("rfx page has an ISO date or a snake case word:\n%s", f)
	}
}
