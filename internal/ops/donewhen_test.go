package ops

import (
	"errors"
	"strings"
	"testing"

	"github.com/iodesystems/parasolve/internal/model"
)

func TestDoneWhen(t *testing.T) {
	d := directoryDeps(t)
	if _, err := ProjectAdd.Run(ctx, d, ProjectAddInput{Title: "Portal"}); err == nil || !strings.Contains(err.Error(), "area") {
		t.Fatalf("goal with no done condition: %v", err)
	}
	proj := must(t, ProjectAdd, d, ProjectAddInput{Title: "Portal", DoneWhen: "Launched and used by ten people."}).ID
	if v := must(t, ProjectShow, d, ProjectShowInput{ID: proj}).Project; v.DoneWhen != "Launched and used by ten people." {
		t.Fatalf("view = %+v", v)
	}
	must(t, ProjectEdit, d, ProjectEditInput{ID: proj, DoneWhen: "Ten people use it."})

	// A person captures fast; an agent says how we know.
	quick := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Quick"}).ID
	agent := agentOf(d)
	if _, err := TaskAdd.Run(ctx, agent, TaskAddInput{Project: proj, Title: "From an agent"}); err == nil || !strings.Contains(err.Error(), "done_when") {
		t.Fatalf("agent task with no done_when: %v", err)
	}
	ok := must(t, TaskAdd, agent, TaskAddInput{Project: proj, Title: "From an agent", DoneWhen: "The parser passes its tests."}).ID
	if v := must(t, TaskShow, d, TaskShowInput{ID: ok}).Task; v.DoneWhen != "The parser passes its tests." {
		t.Fatalf("task = %+v", v)
	}
	must(t, TaskEdit, d, TaskEditInput{ID: quick, DoneWhen: "Checked."})
	if out := must(t, TaskDone, d, TaskDoneInput{ID: quick}); out.DoneWhen != "Checked." {
		t.Fatalf("done = %+v", out)
	}

	// A recurring task hands its condition to the next one.
	rec := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Weekly", Recur: "weekly", Due: "2026-11-03", DoneWhen: "Report sent."}).ID
	next := must(t, TaskDone, d, TaskDoneInput{ID: rec}).Next
	if v := must(t, TaskShow, d, TaskShowInput{ID: next}).Task; v.DoneWhen != "Report sent." {
		t.Fatalf("successor = %+v", v)
	}

	// A proposal item that names task_add must say it, and so must a hold action.
	f := newPropFix(t)
	item := ItemInput{Kind: model.ItemOp, Op: "task_add", Input: map[string]any{"project": f.proj, "title": "x"}, Evidence: ev}
	if _, err := ProposalAdd.Run(ctx, f.agent, ProposalAddInput{Title: "A", Source: f.source, Items: []ItemInput{item}}); err == nil {
		t.Fatal("filed a task with no done_when")
	}
	mt := must(t, MeetingAdd, d, MeetingAddInput{Title: "Sync", With: []string{"person-ada-example"}, Start: "2026-11-05T10:00:00Z", End: "2026-11-05T10:30:00Z"}).ID
	if _, err := MeetingHold.Run(ctx, agent, MeetingHoldInput{ID: mt, Actions: []HoldAction{{Title: "t", Project: proj}}}); err == nil {
		t.Fatal("hold with an action lacking done_when")
	}
	if m := must(t, MeetingShow, d, MeetingShowInput{ID: mt}).Meeting; m.Status == model.MeetingHeld {
		t.Fatal("a refused hold marked the meeting held")
	}
	mustLintClean(t, d)
}

func TestRequestTitle(t *testing.T) {
	rv := newReqVault(t)
	agent := rv.as("claude", "mcp", "inst-a")
	person := rv.as("user", "cli", "inst-a")
	if _, err := RequestAdd.Run(ctx, agent, RequestAddInput{Ask: "Check the backups"}); err == nil || !strings.Contains(err.Error(), "title") {
		t.Fatalf("agent request with no title: %v", err)
	}
	if _, err := RequestAdd.Run(ctx, agent, RequestAddInput{Title: "Revise proposal-sync-1a2b", Ask: "Do it"}); err == nil {
		t.Fatal("a title with an id was accepted")
	}
	out, err := RequestAdd.Run(ctx, agent, RequestAddInput{Title: "Check backups", Ask: "Check the nightly backups.\n\nReport gaps."})
	if err != nil {
		t.Fatal(err)
	}
	if doc, _, _ := getRequest(ctx, agent.Store, out.ID); doc.Title != "Check backups" || !strings.HasPrefix(doc.Body, "Check the nightly backups.") {
		t.Fatalf("doc = %q / %q", doc.Title, doc.Body)
	}
	// A person's title comes from the first line.
	p, err := RequestAdd.Run(ctx, person, RequestAddInput{Ask: "Summarize the week\n\nShort."})
	if err != nil {
		t.Fatal(err)
	}
	if doc, _, _ := getRequest(ctx, person.Store, p.ID); doc.Title != "Summarize the week" {
		t.Fatalf("title = %q", doc.Title)
	}
	// An old request titled with an id reads in words.
	old := must(t, NoteAdd, person, NoteAddInput{Title: "Runbook", Body: "x"}).ID
	oldReq, err := RequestAdd.Run(ctx, person, RequestAddInput{Ask: "Update " + old + " now"})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range must(t, RequestList, person, RequestListInput{}).Items {
		if it.ID == oldReq.ID && it.Title != "Update Runbook now" {
			t.Fatalf("title = %q", it.Title)
		}
	}
	_ = errors.New
}
