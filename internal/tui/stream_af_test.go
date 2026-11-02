package tui

import (
	tea "charm.land/bubbletea/v2"

	"context"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/ops"
)

func TestWhenWordsIsLocalAndHuman(t *testing.T) {
	now := time.Date(2026, 11, 2, 12, 0, 0, 0, time.Local)
	for _, c := range []struct {
		t    time.Time
		want string
	}{
		{now.Add(-30 * time.Second), "just now"},
		{now.Add(-90 * time.Minute), "1 hour ago"},
		{now.Add(-5 * time.Hour), "5 hours ago"},
		{time.Date(2026, 10, 20, 8, 5, 0, 0, time.Local), "Oct 20, 08:05"},
		{time.Date(2025, 3, 1, 8, 5, 0, 0, time.Local), "Mar 1, 2025, 08:05"},
	} {
		if got := whenWords(c.t, now); got != c.want {
			t.Errorf("whenWords(%v) = %q, want %q", c.t, got, c.want)
		}
	}
	if got := dateWords("2026-11-03", now); got != "Nov 3" {
		t.Errorf("dateWords = %q", got)
	}
}

func TestOneWordPerState(t *testing.T) {
	for in, want := range map[string]string{"todo": "ready", "no_reply": "no reply", "in_progress": "in progress", "open": "open"} {
		if got := stateWord(in); got != want {
			t.Errorf("stateWord(%q) = %q, want %q", in, got, want)
		}
	}
	if strings.Contains(stripSGR(state("no_reply")), "_") {
		t.Error("a state shows its snake case")
	}
}

func TestTodayLeadsWithProposalsAndReviewTabCountsThem(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	v.fileProposal(mailItem("Hi Ada."))
	d := newDrv(t, v, 120, 40)
	f := d.plainFrame()
	if strings.Contains(f, "Needs you") {
		t.Errorf("a needs line:\n%s", f)
	}
	if p, i := strings.Index(f, "proposal "), strings.Index(f, "question "); p < 0 || (i >= 0 && p > i) {
		t.Errorf("proposals are not first:\n%s", f)
	}
	if !strings.Contains(strings.Split(f, "\n")[0], "Review 1") {
		t.Errorf("the Review tab has no count:\n%s", f)
	}
}

func TestNarrowTopLineKeepsShortTabNames(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	d := newDrv(t, v, 40, 20)
	top := strings.Split(d.plainFrame(), "\n")[0]
	for _, n := range shortNames {
		if !strings.Contains(top, n) {
			t.Errorf("no %s in %q", n, top)
		}
	}
}

func TestReviewWordsAndGrammar(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	v.agentWrites()
	d := newDrv(t, v, 120, 40)
	d.key("2")
	f := d.plainFrame()
	if !strings.Contains(f, "since your last review") || strings.Contains(f, "since the beginning") {
		t.Errorf("review heading:\n%s", f)
	}
	if strings.Contains(f, " 1 events") {
		t.Errorf("bad grammar:\n%s", f)
	}
	if !strings.Contains(d.footer(), "Mark reviewed") || strings.Contains(d.footer(), "Accept") {
		t.Errorf("footer: %q", d.footer())
	}
}

func TestSearchSlashIsAlwaysSearchAndPersonPrefixFindsPeople(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	d := newDrv(t, v, 100, 30)
	d.key("5")
	d.key("/")
	if !d.has("search:") || d.has("who:") {
		t.Fatalf("/ on People is not search:\n%s", d.plainFrame())
	}
	d.typeText("person:Ada Example")
	d.key("enter")
	if !d.has("Ada Example") || d.m.top() == nil || d.m.top().kind != pagePerson {
		t.Fatalf("person: did not open a person:\n%s", d.plainFrame())
	}
}

func TestSearchShowsResultsAsYouType(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	d := newDrv(t, v, 100, 30)
	d.key("/")
	for _, r := range "pars" {
		d.send(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	// The debounce ends: run the pending tick by hand.
	d.run(d.m.onLiveTick(liveTickMsg{seq: d.m.ov.liveSeq, text: "pars"}))
	if !d.has("Write the parser") || d.m.ov.kind != ovSearch {
		t.Fatalf("no live result:\n%s", d.plainFrame())
	}
}

func TestZoomShowsHowToLeave(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	d := newDrv(t, v, 100, 30)
	d.run(d.m.loadDoc(v.ids["project"], false))
	d.key(d.keyFor("act:zoom"))
	if !d.m.zoom || !d.has("leave zoom") {
		t.Fatalf("zoom has no hint:\n%s", d.plainFrame())
	}
	if n := len(strings.Split(d.m.frame, "\n")); n != 30 {
		t.Errorf("zoom frame has %d rows", n)
	}
}

func TestPolicyStubLinksTheSource(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	doc, err := v.env.Deps.Store.Get(context.Background(), "policy-logging")
	if err != nil {
		t.Fatal(err)
	}
	doc.Body = "Summary not written yet. Read the source.\n"
	got := docSource(doc)
	if !strings.Contains(got, "[Read the source](https://wiki.acme.example/policies/logging)") {
		t.Errorf("source is not linked:\n%s", got)
	}
}

func TestAnswerToastNamesWhatWasRecordedAndUnblocked(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	ctx := context.Background()
	q, err := ops.QuestionAdd.Run(ctx, v.env.Deps, ops.QuestionAddInput{Text: "Which region first?", Root: true, Kind: model.KindDecision, Options: []string{"eu", "us"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ops.TaskAdd.Run(ctx, v.env.Deps, ops.TaskAddInput{Project: v.ids["project"], Title: "Deploy the service", DependsOn: []string{q.ID}}); err != nil {
		t.Fatal(err)
	}
	d := newDrv(t, v, 120, 40)
	var qv ops.QuestionView
	for _, x := range d.m.snap.Questions {
		if x.ID == q.ID {
			qv = x
		}
	}
	d.run(d.m.runAnswer(qv, "1", "closest to users", nil))
	for _, want := range []string{`Recorded "eu" for "Which region first?"`, "Unblocked Deploy the service"} {
		if !strings.Contains(d.m.status, want) {
			t.Errorf("status %q lacks %q", d.m.status, want)
		}
	}
	if d.m.ov.kind != ovNone {
		t.Error("answering left an overlay open")
	}
}

func TestApprovedItemShowsOutcomeByNameAndHashStaysInDetails(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	id := v.fileProposal(v.taskItem("Write the docs"))
	d := newDrv(t, v, 110, 30)
	d.openProposal(id)
	d.key("a")
	if !d.has("made Write the docs") || strings.Contains(d.plainFrame(), "sha256:") {
		t.Fatalf("outcome:\n%s", d.plainFrame())
	}
	d.key("enter")
	f := d.plainFrame()
	if !strings.Contains(f, coversText) || strings.Contains(f, "sha256:") || strings.Contains(f, "task-") || strings.Contains(f, `"title"`) {
		t.Errorf("the full view shows a hash, an id or raw input:\n%s", f)
	}
	d.key(d.keyFor("act:source"))
	if f := d.plainFrame(); !strings.Contains(f, "Approval binds to hash sha256:") || !strings.Contains(f, `"title"`) {
		t.Errorf("Source does not show the raw input and hash:\n%s", f)
	}
}
