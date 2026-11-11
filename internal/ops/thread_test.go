package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/presence"
)

// threadFix is a vault with an owner, an agent on the same install, a task,
// and a clock the test moves.
type threadFix struct {
	rv           *reqVault
	owner, agent Deps
	task, proj   string
}

func newThreadFix(t *testing.T) *threadFix {
	t.Helper()
	rv := newReqVault(t)
	f := &threadFix{rv: rv, owner: rv.as("user", "cli", "install-a"), agent: rv.as("claude", "mcp", "install-a")}
	proj := must(t, ProjectAdd, f.owner, ProjectAddInput{DoneWhen: "Done.", Title: "Portal", Owner: "Jordan Example"})
	f.proj = proj.ID
	f.task = must(t, TaskAdd, f.owner, TaskAddInput{Project: proj.ID, Title: "Write the launch post", DoneWhen: "It is posted."}).ID
	return f
}

func (f *threadFix) thread(t *testing.T, in ThreadInput) ThreadOutput {
	t.Helper()
	return must(t, Thread, f.owner, in)
}

// Turns of a task's thread come in time order: an owner turn on the task, a
// message in on the channel the task names in threads, and an owner turn on
// that channel key. The channel's own thread holds the message and its turn.
func TestThreadOrderAcrossItemIOAndOwner(t *testing.T) {
	f := newThreadFix(t)
	doc, err := f.owner.Store.Get(t.Context(), f.task)
	if err != nil {
		t.Fatal(err)
	}
	const channel = "channel:slack/C1"
	doc.Meta.(*model.Task).Threads = []string{channel}
	if _, err := f.owner.Store.Update(t.Context(), doc); err != nil {
		t.Fatal(err)
	}
	first := must(t, ThreadPost, f.owner, ThreadPostInput{ID: f.task, Text: "De-LLM it, follow my writing style.", Kind: model.RequestTurnModify})
	if first.Thread != f.task || first.Status != model.RequestQueued {
		t.Fatalf("first = %+v", first)
	}
	f.rv.advance(time.Minute)
	msg := must(t, IOAdd, f.owner, IOAddInput{Direction: model.IOIn, Text: "Can we ship Friday?", From: "sam@acme.example",
		Via: &model.IOVia{Source: "slack", Ref: "m1", Thread: "C1"}})
	f.rv.advance(time.Minute)
	second := must(t, ThreadPost, f.owner, ThreadPostInput{Key: channel, Text: "Why section B?", Kind: model.RequestTurnAsk, Quote: "Section B"})
	if second.Thread != channel {
		t.Fatalf("second = %+v", second)
	}

	got := f.thread(t, ThreadInput{ID: f.task})
	var order []string
	for _, tu := range got.Turns {
		order = append(order, tu.Request+tu.IO)
	}
	if want := []string{first.Request, msg.ID, second.Request}; strings.Join(order, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", order, want)
	}
	if got.Kind != "item" || len(got.Keys) != 2 || got.Keys[1] != channel || got.Queued != 2 {
		t.Fatalf("thread = %+v", got)
	}
	t0, t2 := got.Turns[0], got.Turns[2]
	if t0.TurnKind != model.RequestTurnModify || t0.OwnerWords != "De-LLM it, follow my writing style." || t0.Status != model.RequestQueued {
		t.Fatalf("first turn = %+v", t0)
	}
	if t2.TurnKind != model.RequestTurnAsk || t2.Quote != "Section B" || t2.OwnerWords != "Why section B?" {
		t.Fatalf("channel turn = %+v", t2)
	}
	if got.Turns[1].Kind != ThreadTurnIO || got.Turns[1].Direction != model.IOIn {
		t.Fatalf("io turn = %+v", got.Turns[1])
	}

	ch := f.thread(t, ThreadInput{Key: channel})
	if ch.Kind != "channel" || len(ch.Turns) != 2 || ch.Turns[0].IO != msg.ID || ch.Turns[1].Request != second.Request {
		t.Fatalf("channel thread = %+v", ch)
	}
	if len(ch.Members) != 1 || ch.Members[0] != f.task {
		t.Fatalf("members = %v", ch.Members)
	}
}

// The owner posts a second turn while the agent holds the first; the agent
// sees the kind, quote, and item; its notes, reply, and receipts land on the
// first turn. With a turn queued and no agent connected, waiting_no_agent is
// true; a connected agent clears it. The task stays out of Next while a turn
// is the agent's.
func TestThreadPostWhileHeldReplyAndReceipts(t *testing.T) {
	f := newThreadFix(t)
	sn, err := NewSnapshot(t.Context(), f.owner.Store)
	if err != nil {
		t.Fatal(err)
	}
	if !sn.Need(f.task).Yes {
		t.Fatalf("a new task needs the owner: %+v", sn.Need(f.task))
	}
	first := must(t, ThreadPost, f.owner, ThreadPostInput{ID: f.task, Text: "Shorten the intro.", Kind: model.RequestTurnModify, Quote: "Intro paragraph"})
	f.rv.advance(time.Minute)
	next := must(t, RequestNext, f.agent, RequestNextInput{})
	if next.Request == nil || next.Request.ID != first.Request {
		t.Fatalf("next = %+v", next.Request)
	}
	r := next.Request
	if r.Thread != f.task || r.TurnKind != model.RequestTurnModify || r.Quote != "Intro paragraph" || len(r.Links) != 1 || r.Links[0] != f.task || r.Kind != SentKind {
		t.Fatalf("claimed = %+v", r.RequestView)
	}
	for _, want := range []string{"## Owner's words\n\nShorten the intro.", "## Quote\n\n> Intro paragraph", "[[" + f.task + "]]", "Kind: modify"} {
		if !strings.Contains(r.Body, want) {
			t.Fatalf("body lacks %q:\n%s", want, r.Body)
		}
	}
	f.rv.advance(time.Minute)
	second, err := ThreadPost.Run(t.Context(), f.owner, ThreadPostInput{ID: f.task, Text: "Also investigate and apply what you learn.", Kind: model.RequestTurnRedirect})
	if err != nil || second.Request == first.Request || second.Status != model.RequestQueued {
		t.Fatalf("second while held = %+v %v", second, err)
	}
	must(t, RequestNote, f.agent, RequestNoteInput{ID: first.Request, Text: "Reading the post."})
	sub, err := TaskAdd.Run(writeCtx(t), f.agent, TaskAddInput{Project: f.proj, Title: "Trim the intro", DoneWhen: "It is short."})
	if err != nil {
		t.Fatal(err)
	}
	must(t, RequestDone, f.agent, RequestDoneInput{ID: first.Request, Resolution: model.ResolutionDone, Result: "Intro cut to two lines.", Reply: "I cut the intro to two lines.", Links: []string{sub.ID}})

	got := f.thread(t, ThreadInput{ID: f.task})
	if len(got.Turns) != 2 || got.Turns[0].Request != first.Request || got.Turns[1].Request != second.Request {
		t.Fatalf("turns = %+v", got.Turns)
	}
	t0 := got.Turns[0]
	if t0.Status != model.RequestDone || t0.Reply != "I cut the intro to two lines." || t0.Result != "Intro cut to two lines." || t0.ClaimedBy != "claude" || t0.ClaimedAt == "" {
		t.Fatalf("first turn = %+v", t0)
	}
	if len(t0.Notes) != 1 || t0.Notes[0].Actor != "claude" || t0.Notes[0].Text != "Reading the post." {
		t.Fatalf("notes = %+v", t0.Notes)
	}
	if len(t0.Receipts) == 0 || t0.Receipts[0].Doc != sub.ID {
		t.Fatalf("receipts = %+v", t0.Receipts)
	}
	if got.Turns[1].TurnKind != model.RequestTurnRedirect || got.Queued != 1 || got.Claimed != 0 {
		t.Fatalf("second turn = %+v queued %d", got.Turns[1], got.Queued)
	}
	if !got.WaitingNoAgent || got.Agents != 0 {
		t.Fatalf("no agent: waiting_no_agent %v agents %d", got.WaitingNoAgent, got.Agents)
	}
	sn, err = NewSnapshot(t.Context(), f.owner.Store)
	if err != nil {
		t.Fatal(err)
	}
	if n := sn.Need(f.task); n.Yes || n.Turn != TurnAgent {
		t.Fatalf("a task with a queued turn needs the owner: %+v", n)
	}

	state := filepath.Join(filepath.Dir(f.rv.root), "state-install-a")
	if err := presence.Write(state, presence.Record{PID: os.Getpid(), Actor: "claude", Client: "claude-code", StartedAt: "2026-11-01T09:00:00Z", Push: []string{"message"}}); err != nil {
		t.Fatal(err)
	}
	got = f.thread(t, ThreadInput{ID: f.task})
	if got.WaitingNoAgent || got.Agents != 1 {
		t.Fatalf("agent connected: waiting_no_agent %v agents %d", got.WaitingNoAgent, got.Agents)
	}
}

// request_respond routes through thread_post: a turn on finished work reopens
// it as its next turn. revert has no turn kind and runs as before. thread_post
// on the finished request does the same and says so. A failed turn's error
// and reply show in the thread.
func TestRequestRespondAliasOfThreadPost(t *testing.T) {
	f := newThreadFix(t)
	turn := must(t, ThreadPost, f.owner, ThreadPostInput{ID: f.task, Text: "Draft the post."})
	must(t, RequestNext, f.agent, RequestNextInput{})
	must(t, RequestFail, f.agent, RequestFailInput{ID: turn.Request, Reason: "No access to the blog.", Reply: "I could not open the blog."})
	got := f.thread(t, ThreadInput{ID: f.task})
	if len(got.Turns) != 1 || got.Turns[0].Error != "No access to the blog." || got.Turns[0].Reply != "I could not open the blog." {
		t.Fatalf("failed turn = %+v", got.Turns)
	}

	// request_respond is internal: the CLI's respond runs it.
	if !IsInternal("request_respond") {
		t.Fatal("request_respond is listed")
	}
	if ch := must(t, RequestRespond, f.owner, RequestRespondInput{ID: turn.Request, Kind: RespondClarify, Text: "Which blog?"}); ch.ID != turn.Request || ch.Status != model.RequestQueued {
		t.Fatalf("respond = %+v", ch)
	}
	_, r, err := getRequest(t.Context(), f.owner.Store, turn.Request)
	if err != nil || r.Turn != 1 || r.TurnKind != model.RequestTurnAsk || r.Reply != "" {
		t.Fatalf("reopened = %+v %v", r, err)
	}

	must(t, RequestNext, f.agent, RequestNextInput{})
	must(t, RequestDone, f.agent, RequestDoneInput{ID: turn.Request, Resolution: model.ResolutionDone, Result: "Used the team blog.", Reply: "The team blog."})
	again := must(t, ThreadPost, f.owner, ThreadPostInput{ID: turn.Request, Text: "Make it shorter.", Kind: model.RequestTurnModify, Quote: "the team blog"})
	if !again.Reopened || again.Request != turn.Request || again.Thread != f.task {
		t.Fatalf("thread_post on finished work = %+v", again)
	}
	got = f.thread(t, ThreadInput{ID: turn.Request})
	if len(got.Turns) != 1 || got.Key != f.task || got.Item != f.task {
		t.Fatalf("thread of a request = %+v", got)
	}
	tu := got.Turns[0]
	if tu.Turn != 2 || tu.Quote != "the team blog" || tu.Error != "" || tu.Result != "" || len(tu.Steps) == 0 {
		t.Fatalf("reopened turn = %+v", tu)
	}
}

// agent_send is an owner turn in the item's own thread.
func TestAgentSendIsThreadTurn(t *testing.T) {
	f := newThreadFix(t)
	out := must(t, AgentSend, f.owner, AgentSendInput{ID: f.task, Text: "Met Sam; ship it."})
	_, r, err := getRequest(t.Context(), f.owner.Store, out.Request)
	if err != nil || r.Thread != f.task || r.TurnKind != model.RequestTurnOther || r.Kind != SentKind {
		t.Fatalf("agent_send request = %+v %v", r, err)
	}
	got := f.thread(t, ThreadInput{Key: f.task})
	if len(got.Turns) != 1 || got.Turns[0].OwnerWords != "Met Sam; ship it." {
		t.Fatalf("thread = %+v", got.Turns)
	}
	if _, err := ThreadPost.Run(t.Context(), f.agent, ThreadPostInput{ID: f.task, Text: "x"}); err == nil {
		t.Fatal("an agent posted an owner turn")
	}
	if _, err := ThreadPost.Run(t.Context(), f.owner, ThreadPostInput{Key: "#Launch Plan", Text: "Group these."}); err != nil {
		t.Fatalf("tag turn: %v", err)
	}
	tag := f.thread(t, ThreadInput{Key: "#launch-plan"})
	if tag.Kind != "tag" || len(tag.Turns) != 1 {
		t.Fatalf("tag thread = %+v", tag)
	}
}
