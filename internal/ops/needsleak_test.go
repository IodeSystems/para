package ops

import (
	"slices"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
)

// Regression tests for what the owner saw needing them when it did not. Each
// view reads NeedsYou, so each leak is checked in every view that showed it.

func leakVault(t *testing.T) (Deps, string) {
	t.Helper()
	old := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = old })
	d := newDepsAt(t, needsNow, "m1")
	seedOrg(t, d)
	proj := must(t, ProjectAdd, d, ProjectAddInput{DoneWhen: "Done.", Title: "Portal"}).ID
	return d, proj
}

func setOf(t *testing.T, d Deps) map[string]bool {
	t.Helper()
	sn, err := NewSnapshot(ctx, d.Store)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, id := range sn.Set() {
		out[id] = true
	}
	return out
}

func agendaIDs(t *testing.T, d Deps, days int) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, it := range must(t, Agenda, d, AgendaInput{Days: days, Zone: "UTC"}).Items {
		out[it.ID] = true
	}
	return out
}

func stackNeeds(t *testing.T, d Deps) (map[string]bool, int) {
	t.Helper()
	st := must(t, ProposalStack, d, ProposalStackInput{NeedsYou: true})
	out := map[string]bool{}
	for _, r := range st.Rows {
		out[r.Item] = true
	}
	for _, r := range st.Requests {
		out[r.Request] = true
	}
	return out, st.NeedsYou
}

// Leak 1: the agenda listed done items and items put off.
func TestLeakAgendaHidesDoneAndLater(t *testing.T) {
	d, proj := leakVault(t)
	open := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Open, due today", Due: "2026-11-05", DoneWhen: "Done."}).ID
	done := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Done, due today", Due: "2026-11-05", DoneWhen: "Done."}).ID
	must(t, TaskDone, d, TaskDoneInput{ID: done})
	failed := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Failed, retry today", DoneWhen: "Done."}).ID
	must(t, TaskResolve, d, TaskResolveInput{ID: failed, Outcome: model.OutcomeFailed, Tried: "Ran it.", StoppedBecause: "Down.", RetryAfter: "2026-11-05"})
	later := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Put off, due today", Due: "2026-11-05", DoneWhen: "Done."}).ID
	in := must(t, IOAdd, d, IOAddInput{Direction: model.IOIn, Text: "Can you look at the invoice?", From: "person-ada-example"}).ID
	inLater := must(t, IOAdd, d, IOAddInput{Direction: model.IOIn, Text: "Newsletter", From: "person-bo-example"}).ID
	must(t, Defer, d, DeferInput{IDs: []string{later, inLater}, Until: "2026-11-12"})

	set := setOf(t, d)
	ag := agendaIDs(t, d, 7)
	order := orderIDs(t, d)
	for _, id := range []string{done, failed, later, inLater} {
		if set[id] || ag[id] {
			t.Errorf("%s: in needs_you %v, in agenda %v; want neither", id, set[id], ag[id])
		}
		if _, ok := order[id]; ok {
			t.Errorf("%s in the order", id)
		}
	}
	if !set[open] || !ag[open] || !set[in] {
		t.Errorf("open task in set %v, agenda %v; inbound io in set %v", set[open], ag[open], set[in])
	}
	// The inbox the agenda page lists marks what needs the owner.
	for _, it := range must(t, IOList, d, IOListInput{Direction: model.IOIn}).Items {
		if it.NeedsYou != (it.ID == in) {
			t.Errorf("io_list %s: needs_you %v", it.ID, it.NeedsYou)
		}
	}
}

// Leak 2: optional meetings asked the owner to confirm attendance. Only an
// optional meeting the owner goes to (attend yes) counts; attend no never.
func TestLeakOptionalMeetingsDoNotCount(t *testing.T) {
	d, _ := leakVault(t)
	person := "person-ada-example"
	opt := addMeeting(t, d, "Optional sync", person, "2026-11-05T15:00:00Z", "2026-11-05T15:30:00Z", true)
	req := addMeeting(t, d, "Required sync", person, "2026-11-05T16:00:00Z", "2026-11-05T16:30:00Z", false)
	check := func(label string, want bool, inSkipped bool) {
		t.Helper()
		set := setOf(t, d)
		ag := must(t, Agenda, d, AgendaInput{Days: 1, Zone: "UTC"})
		_, inOrder := orderIDs(t, d)[opt]
		inItems := slices.ContainsFunc(ag.Items, func(x AgendaItem) bool { return x.ID == opt })
		if set[opt] != want || inOrder != want || inItems != want || slices.Contains(nextIDs(t, d), opt) != want {
			t.Errorf("%s: set %v, order %v, agenda %v; want %v", label, set[opt], inOrder, inItems, want)
		}
		skipped := slices.ContainsFunc(ag.Skipped, func(x AgendaItem) bool { return x.ID == opt })
		optional := slices.ContainsFunc(ag.Optional, func(x AgendaItem) bool { return x.ID == opt })
		if skipped != inSkipped || optional != (!want && !inSkipped) {
			t.Errorf("%s: skipped %v, optional %v", label, skipped, optional)
		}
		if !set[req] {
			t.Errorf("%s: the required meeting does not need the owner", label)
		}
	}
	check("unset", false, false)
	must(t, MeetingAttend, d, MeetingAttendInput{ID: opt, Attend: "maybe"})
	check("maybe", false, false)
	must(t, MeetingAttend, d, MeetingAttendInput{ID: opt, Attend: "yes"})
	check("yes", true, false)
	must(t, MeetingAttend, d, MeetingAttendInput{ID: opt, Attend: "no"})
	check("no", false, true)
	must(t, MeetingAttend, d, MeetingAttendInput{ID: req, Attend: "no"})
	if setOf(t, d)[req] {
		t.Error("a required meeting the owner skips needs the owner")
	}
}

// Leaks 3 and 4: an item stayed pending in the stack after the owner answered
// it or sent it to an agent, and an item whose agent work is queued or
// claimed showed as needing the owner.
func TestLeakSentToAgentLeavesEveryView(t *testing.T) {
	d, proj := leakVault(t)
	q := must(t, QuestionAdd, d, QuestionAddInput{Text: "Which region?", Project: proj}).ID
	answered := must(t, QuestionAdd, d, QuestionAddInput{Text: "Which vendor?", Project: proj}).ID
	tk := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "File the form", Due: "2026-11-05", DoneWhen: "Filed."}).ID
	if st, _ := stackNeeds(t, d); !st[q] || !st[answered] {
		t.Fatalf("open questions not in the stack: %v", st)
	}
	must(t, QuestionAnswer, d, QuestionAnswerInput{ID: answered, Text: "Vendor A.", Why: "Cheapest.", Source: "the owner"})
	sq := must(t, AgentSend, d, AgentSendInput{ID: q, Text: "Find out what legal said."})
	stk := must(t, AgentSend, d, AgentSendInput{ID: tk, Text: "Do this for me."})
	if sq.Status != model.RequestQueued || stk.Status != model.RequestQueued {
		t.Fatalf("agent_send status %s, %s; want queued", sq.Status, stk.Status)
	}
	views := func(label string, want bool) {
		t.Helper()
		set := setOf(t, d)
		st, _ := stackNeeds(t, d)
		order := orderIDs(t, d)
		ag := agendaIDs(t, d, 1)
		for _, id := range []string{q, tk} {
			_, inOrder := order[id]
			if set[id] != want || st[id] != (want && id == q) || inOrder != want || ag[id] != (want && id == tk) {
				t.Errorf("%s %s: set %v, stack %v, order %v, agenda %v; want %v", label, id, set[id], st[id], inOrder, ag[id], want)
			}
		}
		if slices.Contains(nextIDs(t, d), tk) != want {
			t.Errorf("%s: next has the task: %v", label, !want)
		}
		if set[answered] || st[answered] {
			t.Errorf("%s: an answered question still needs the owner", label)
		}
	}
	views("queued", false)
	// The stack row says an agent has it.
	for _, r := range must(t, ProposalStack, d, ProposalStackInput{}).Rows {
		if r.Item == q && (r.State != model.ItemSentBack || r.NeedsYou) {
			t.Errorf("stack row of a question sent to an agent: %+v", r)
		}
	}
	// Claimed is still the agent's.
	if _, err := RequestNext.Run(ctx, d, RequestNextInput{}); err != nil {
		t.Fatal(err)
	}
	views("claimed", false)
	// Taken back, the items need the owner again.
	for _, id := range []string{sq.Request, stk.Request} {
		doc, r, err := getRequest(ctx, d.Store, id)
		if err != nil {
			t.Fatal(err)
		}
		if r.Status == model.RequestQueued {
			must(t, AgentUnsend, d, AgentUnsendInput{Request: doc.ID})
			continue
		}
		must(t, RequestFail, d, RequestFailInput{ID: doc.ID, Reason: "Gave up."})
	}
	views("ended", true)
}
