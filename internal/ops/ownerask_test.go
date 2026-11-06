package ops

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
)

func ownerAsk(t *testing.T, d Deps, title, said string) RequestAddOutput {
	t.Helper()
	out, err := RequestAdd.Run(context.Background(), d, RequestAddInput{Title: title, Ask: "Do what the owner asked.", Kind: OwnerAskKind, OwnerSaid: said})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestOwnerAskRoundTrip(t *testing.T) {
	ctx := context.Background()
	rv := newReqVault(t)
	person := rv.as("user", "cli", "inst-a")
	agent := rv.as("agent-1", "mcp", "inst-a")

	if _, err := RequestAdd.Run(ctx, agent, RequestAddInput{Title: "Check backups", Ask: "x", Kind: OwnerAskKind}); err == nil || !strings.Contains(err.Error(), "owner_said") {
		t.Fatalf("owner-ask with no owner_said: %v", err)
	}
	if _, err := RequestAdd.Run(ctx, agent, RequestAddInput{Title: "Check backups", Ask: "x", OwnerSaid: "hi"}); err == nil {
		t.Fatal("owner_said on another kind")
	}
	add := ownerAsk(t, agent, "Check the backups", "can you check the backups ran last night?")
	if add.Status != model.RequestClaimed || add.LeaseUntil == "" {
		t.Fatalf("add = %+v", add)
	}
	// Another agent's request_next does not take it, and a lease that ends
	// does not put it back in the queue.
	rv.advance(DefaultLease + time.Minute)
	if out := nextReq(t, rv.as("agent-2", "mcp", "inst-a")); out.Request != nil || len(out.Requeued) != 0 {
		t.Fatalf("another agent got the owner-ask: %+v", out)
	}
	if ids, err := NewRequestWatch(person).ClaimableIDs(ctx); err != nil || len(ids) != 0 {
		t.Fatalf("watch: %v %v", ids, err)
	}

	sent := must(t, SentList, person, SentListInput{})
	if len(sent.Items) != 1 || sent.Open != 1 || sent.Stale != 0 {
		t.Fatalf("sent = %+v", sent)
	}
	it := sent.Items[0]
	if it.Kind != OwnerAskKind || it.ClaimedBy != "agent-1" || it.ClaimedAt == "" || it.Note != "can you check the backups ran last night?" || it.Title != "Check the backups" {
		t.Fatalf("row = %+v", it)
	}

	if _, err := RequestDone.Run(ctx, agent, RequestDoneInput{ID: add.ID, Resolution: model.ResolutionDone, Result: "Backups ran at 02:00."}); err != nil {
		t.Fatal(err)
	}
	sent = must(t, SentList, person, SentListInput{})
	if it := sent.Items[0]; it.Status != model.RequestDone || it.Summary != "Backups ran at 02:00." || sent.Open != 0 {
		t.Fatalf("after done = %+v", sent)
	}
	if got, _ := person.Store.Lint(ctx); len(got) != 0 {
		t.Fatalf("lint: %v", got)
	}

	// A person queues an owner-ask like any work: it is queued, not claimed.
	q, err := RequestAdd.Run(ctx, person, RequestAddInput{Ask: "Tidy the wiki", Kind: OwnerAskKind})
	if err != nil || q.Status != model.RequestQueued {
		t.Fatalf("person owner-ask = %+v, %v", q, err)
	}
	// The session notification request_claimable reads this watch.
	if ids, err := NewRequestWatch(person).ClaimableIDs(ctx); err != nil || len(ids) != 1 || ids[0] != q.ID {
		t.Fatalf("watch = %v %v, want [%s]", ids, err, q.ID)
	}
}

func TestRequestNextTakesSweepsThenOwnerAsks(t *testing.T) {
	rv := newReqVault(t)
	person := rv.as("user", "cli", "inst-a")
	agent := rv.as("agent-1", "mcp", "inst-a")
	other := addReq(t, person, "Other work")
	rv.advance(time.Second)
	asked, err := RequestAdd.Run(context.Background(), person, RequestAddInput{Ask: "Owner's ask", Kind: OwnerAskKind})
	if err != nil {
		t.Fatal(err)
	}
	if got := nextReq(t, agent).Request; got == nil || got.ID != asked.ID {
		t.Fatalf("first = %+v, want the owner-ask %s", got, asked.ID)
	}
	if got := nextReq(t, agent).Request; got == nil || got.ID != other.ID {
		t.Fatalf("second = %+v, want %s", got, other.ID)
	}
}

func TestAgentWorkStale(t *testing.T) {
	ctx := context.Background()
	rv := newReqVault(t)
	person := rv.as("user", "cli", "inst-a")
	agent := rv.as("agent-1", "mcp", "inst-a")
	held := ownerAsk(t, agent, "Draft the plan", "draft the plan for Q1")
	queued, err := RequestAdd.Run(ctx, person, RequestAddInput{Ask: "Owner's ask", Kind: OwnerAskKind})
	if err != nil {
		t.Fatal(err)
	}
	addReq(t, person, "Other work") // not asked by the owner: no finding

	stale := func() []string {
		h, err := Health(ctx, person.Store)
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for _, g := range h.Groups {
			for _, f := range g.Findings {
				if f.Kind == HealthAgentWorkStale {
					ids = append(ids, f.ID)
				}
			}
		}
		return ids
	}
	rv.advance(23 * time.Hour)
	if got := stale(); len(got) != 0 {
		t.Fatalf("stale before a day: %v", got)
	}
	// A note is a change: the held one is not stale; the queued one is.
	if _, err := RequestNote.Run(ctx, agent, RequestNoteInput{ID: held.ID, Text: "outline done"}); err != nil {
		t.Fatal(err)
	}
	rv.advance(2 * time.Hour)
	if got := stale(); len(got) != 1 || got[0] != queued.ID {
		t.Fatalf("stale = %v, want [%s]", got, queued.ID)
	}
	rv.advance(24 * time.Hour)
	if got := stale(); len(got) != 2 {
		t.Fatalf("stale = %v, want both", got)
	}
	if s := must(t, SentList, person, SentListInput{}); s.Open != 2 || s.Stale != 2 {
		t.Fatalf("sent counts = %d open, %d stale", s.Open, s.Stale)
	}
}
