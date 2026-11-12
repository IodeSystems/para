package ops

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

// requestIDs lists the ids of every request in the vault.
func requestIDs(t *testing.T, d Deps) []string {
	t.Helper()
	docs, err := loadRequests(context.Background(), d.Store)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, doc := range docs {
		ids = append(ids, doc.ID)
	}
	return ids
}

// oneNewRequest runs fn and returns the one request it made. It fails when fn
// made none or more than one.
func oneNewRequest(t *testing.T, d Deps, fn func()) (model.Document, *model.Request) {
	t.Helper()
	before := requestIDs(t, d)
	fn()
	var made []string
	for _, id := range requestIDs(t, d) {
		if !slices.Contains(before, id) {
			made = append(made, id)
		}
	}
	if len(made) != 1 {
		t.Fatalf("made %d requests, want 1: %v", len(made), made)
	}
	doc, r, err := getRequest(context.Background(), d.Store, made[0])
	if err != nil {
		t.Fatal(err)
	}
	return doc, r
}

// Every entry point that asks an agent for work makes exactly one request,
// with its kind, linked to the item. A second ask on an item with open work of
// the same kind adds to it.
func TestAgentWorkEntryPointsMakeOneLinkedRequest(t *testing.T) {
	ctx := context.Background()
	rv := newReqVault(t)
	person := rv.as("user", "cli", "inst-a")
	agent := rv.as("agent-1", "mcp", "inst-a")
	project := must(t, GoalAdd, person, GoalAddInput{Title: "Launch", DoneWhen: "Shipped."}).ID
	task := must(t, TaskAdd, person, TaskAddInput{Goal: project, Title: "Write the plan"}).ID
	other := must(t, TaskAdd, person, TaskAddInput{Goal: project, Title: "Book the hall"}).ID
	q1 := must(t, QuestionAdd, person, QuestionAddInput{Text: "Which region first?", Goal: project}).ID
	q2 := must(t, QuestionAdd, person, QuestionAddInput{Text: "Which vendor?", Goal: project}).ID

	cases := []struct {
		name string
		kind string
		item string
		run  func() error
	}{
		{"agent_send", SentKind, task, func() error {
			_, err := AgentSend.Run(ctx, person, AgentSendInput{ID: task, Text: "Sam agreed."})
			return err
		}},
		{"send_to_agent (owner)", SentKind, other, func() error {
			_, err := SendToAgent.Run(ctx, person, SendToAgentInput{ID: other, Note: "Ask for the quote."})
			return err
		}},
		{"send_to_agent (agent)", FromAgentKind, task, func() error {
			_, err := SendToAgent.Run(ctx, agent, SendToAgentInput{ID: task, Title: "Check the plan", Note: "Read it."})
			return err
		}},
		{"investigate", InvestigateKind, q1, func() error {
			_, err := Investigate.Run(ctx, person, InvestigateInput{ID: q1})
			return err
		}},
		{"task_delegate", DelegateKind, task, func() error {
			_, err := TaskDelegate.Run(ctx, person, TaskDelegateInput{ID: task, Agent: "scribe"})
			return err
		}},
		{"task_update", TaskUpdateKind, task, func() error {
			_, err := TaskUpdate.Run(ctx, person, TaskUpdateInput{ID: task, Text: "Sam agreed to review by Friday."})
			return err
		}},
		{"question_review ask", SentKind, q2, func() error {
			_, err := QuestionReview.Run(ctx, person, QuestionReviewInput{ID: q2, Decision: ReviewAsk, Note: "Ask Sam."})
			return err
		}},
		{"owner-ask", OwnerAskKind, task, func() error {
			_, err := RequestAdd.Run(ctx, agent, RequestAddInput{Title: "Check backups", Ask: "x", Kind: OwnerAskKind, OwnerSaid: "check the backups", Links: []string{task}})
			return err
		}},
	}
	for _, c := range cases {
		doc, r := oneNewRequest(t, person, func() {
			if err := c.run(); err != nil {
				t.Fatalf("%s: %v", c.name, err)
			}
		})
		if r.Kind != c.kind || !slices.Equal(targets(r.Links), []string{c.item}) {
			t.Errorf("%s: kind %q links %v, want %q [%s]", c.name, r.Kind, r.Links, c.kind, c.item)
		}
		if requestKind(doc.Title, r) != c.kind {
			t.Errorf("%s: requestKind %q", c.name, requestKind(doc.Title, r))
		}
	}

	// A second ask on open work adds to it: no new request.
	before := len(requestIDs(t, person))
	must(t, Investigate, person, InvestigateInput{ID: q1, Hint: "Look in the wiki."})
	must(t, TaskDelegate, person, TaskDelegateInput{ID: task, Agent: "scribe", Hint: "Start with the outline."})
	must(t, TaskUpdate, person, TaskUpdateInput{ID: task, Text: "And the budget."})
	if after := len(requestIDs(t, person)); after != before {
		t.Errorf("appends made %d requests", after-before)
	}

	// The tick's sweep goes through request_add too.
	doc, r := oneNewRequest(t, person, func() {
		if _, err := sweepRequest(ctx, person, model.Document{ID: "source-mail", Title: "Mailbox"}); err != nil {
			t.Fatal(err)
		}
	})
	if r.Kind != SweepKind || r.RequiresSource.Target() != "source-mail" || r.ActiveAt == "" || doc.Title != "Sweep Mailbox" || r.Status != model.RequestQueued {
		t.Errorf("sweep = %+v %q", r, doc.Title)
	}
}

// An agent's request for another runner stays proposed: hostile text an agent
// reads cannot queue work.
func TestAgentRequestForAnotherRunnerStaysProposed(t *testing.T) {
	rv := newReqVault(t)
	agent := rv.as("agent-1", "mcp", "inst-a")
	out := must(t, RequestAdd, agent, RequestAddInput{Title: "Summarize", Ask: "x", Runner: "inst-b"})
	if out.Status != model.RequestProposed {
		t.Fatalf("status = %s", out.Status)
	}
	route := must(t, SendToAgent, agent, SendToAgentInput{ID: out.ID, Title: "Look again", Note: "y"})
	if st := route.Result["status"]; st != string(model.RequestProposed) {
		t.Fatalf("send_to_agent from an agent: %s", st)
	}
}

// needs_you for agent work: proposed, a turn that ended stuck, blocked, or
// needs_clarification, failed, and a result not yet seen. Seen clears done
// and failed, not a turn that waits for a response.
func TestRequestNeedsYouByEnd(t *testing.T) {
	ctx := context.Background()
	rv := newReqVault(t)
	person := rv.as("user", "cli", "inst-a")
	agent := rv.as("agent-1", "mcp", "inst-a")
	end := func(title string, fn func(id string) error) string {
		t.Helper()
		add := must(t, RequestAdd, person, RequestAddInput{Title: title, Ask: "Do " + title + "."})
		next := nextReq(t, agent)
		if next.Request == nil || next.Request.ID != add.ID {
			t.Fatalf("%s: next = %+v", title, next.Request)
		}
		if err := fn(add.ID); err != nil {
			t.Fatal(err)
		}
		return add.ID
	}
	done := func(res model.RequestResolution) func(string) error {
		return func(id string) error {
			_, err := RequestDone.Run(ctx, agent, RequestDoneInput{ID: id, Resolution: res, Result: "Ended " + string(res) + "."})
			return err
		}
	}
	want := map[string]string{
		end("clarify", done(model.ResolutionNeedsClarification)): "agent work ended needs_clarification",
		end("stuck", done(model.ResolutionStuck)):                "agent work ended stuck",
		end("blocked", done(model.ResolutionBlocked)):            "agent work ended blocked",
		end("done", done(model.ResolutionDone)):                  "result to check",
		end("partial", done(model.ResolutionPartial)):            "result to check",
		end("failed", func(id string) error {
			_, err := RequestFail.Run(ctx, agent, RequestFailInput{ID: id, Reason: "No access to the mailbox."})
			return err
		}): "failed: No access to the mailbox.",
	}
	sn, err := NewSnapshot(ctx, person.Store)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for id, reason := range want {
		ids = append(ids, id)
		if n := sn.Need(id); !n.Yes || n.Reason != reason {
			t.Errorf("%s: need = %v %q, want %q", id, n.Yes, n.Reason, reason)
		}
	}
	// list {needs_you} has them all.
	listed := must(t, List, person, ListInput{NeedsYou: true})
	for _, id := range ids {
		if !slices.ContainsFunc(listed.Rows, func(r ListRow) bool { return r.ID == id }) {
			t.Errorf("%s not in list needs_you", id)
		}
	}

	// An agent cannot mark a result seen.
	if _, err := RequestSeen.Run(ctx, agent, RequestSeenInput{IDs: ids}); err == nil {
		t.Fatal("agent marked seen")
	}
	seen := must(t, RequestSeen, person, RequestSeenInput{IDs: ids})
	if len(seen.Seen) != 6 {
		t.Fatalf("seen = %+v", seen)
	}
	sn, err = NewSnapshot(ctx, person.Store)
	if err != nil {
		t.Fatal(err)
	}
	for id, reason := range want {
		n := sn.Need(id)
		waits := strings.HasPrefix(reason, "agent work ended")
		if n.Yes != waits {
			t.Errorf("%s after seen: need = %v %q", id, n.Yes, n.Reason)
		}
		if !waits && n.Life != LifeClosed {
			t.Errorf("%s after seen: life %s", id, n.Life)
		}
	}
	// Seen again is skipped.
	if again := must(t, RequestSeen, person, RequestSeenInput{IDs: ids}); len(again.Seen) != 0 {
		t.Errorf("seen twice: %+v", again)
	}
}

// close on finished agent work marks its result seen.
func TestCloseFinishedRequestMarksSeen(t *testing.T) {
	ctx := context.Background()
	rv := newReqVault(t)
	person := rv.as("user", "cli", "inst-a")
	agent := rv.as("agent-1", "mcp", "inst-a")
	add := must(t, RequestAdd, person, RequestAddInput{Title: "Check backups", Ask: "x"})
	nextReq(t, agent)
	must(t, RequestDone, agent, RequestDoneInput{ID: add.ID, Resolution: model.ResolutionDone, Result: "Ran."})
	out := must(t, Close, person, CloseInput{ID: add.ID})
	if out.Op != RequestSeen.Name() {
		t.Fatalf("op = %s", out.Op)
	}
	if _, r, err := getRequest(ctx, person.Store, add.ID); err != nil || r.Seen == "" {
		t.Fatalf("seen = %q, %v", r.Seen, err)
	}
}

// Requests made before kinds and seen still list, with the kind their title
// says, and do not flood needs_you: work with no active_at ended before seen.
func TestLegacyRequestsStillList(t *testing.T) {
	ctx := context.Background()
	rv := newReqVault(t)
	person := rv.as("user", "cli", "inst-a")
	project := must(t, GoalAdd, person, GoalAddInput{Title: "Launch", DoneWhen: "Shipped."}).ID
	task := must(t, TaskAdd, person, TaskAddInput{Goal: project, Title: "Write the plan"}).ID
	mk := func(id, title string, status model.RequestStatus, res model.RequestResolution) {
		t.Helper()
		r := &model.Request{Common: model.Common{Type: model.TypeRequest}, Status: status, Runner: "inst-a", Queued: "2026-10-01T09:00:00Z",
			Resolution: res, Links: []model.Link{model.LinkTo(task)}}
		if _, err := person.Store.Create(ctx, model.Document{ID: id, Folder: model.FolderRequests, Title: title, Body: "Old work.\n\n## Result\n\nDone.", Meta: r}); err != nil {
			t.Fatal(err)
		}
	}
	mk("request-old-work", "Work on: Write the plan", model.RequestQueued, "")
	mk("request-old-sort", "Sort update: Write the plan", model.RequestDone, model.ResolutionDone)
	mk("request-old-find", "Investigate: Write the plan", model.RequestDone, model.ResolutionDone)
	mk("request-old-sent", "From the owner: Write the plan", model.RequestFailed, model.ResolutionFailed)
	want := map[string]string{
		"request-old-work": DelegateKind, "request-old-sort": TaskUpdateKind,
		"request-old-find": InvestigateKind, "request-old-sent": SentKind,
	}
	out := must(t, List, person, ListInput{Type: model.TypeRequest, State: "all"})
	if len(out.Rows) != 4 {
		t.Fatalf("rows = %+v", out.Rows)
	}
	for _, row := range out.Rows {
		if row.Request == nil || row.Request.Kind != want[row.ID] || row.Request.Item != task {
			t.Errorf("%s: request = %+v", row.ID, row.Request)
		}
		if row.ID != "request-old-work" && (row.NeedsYou || row.Life != LifeClosed) {
			t.Errorf("%s: an old finished request needs you: %+v", row.ID, row)
		}
	}
	list := must(t, RequestList, person, RequestListInput{})
	for _, v := range list.Items {
		if v.Kind != want[v.ID] {
			t.Errorf("%s: request_list kind %q", v.ID, v.Kind)
		}
		if v.ID == "request-old-work" && !v.Delegates {
			t.Errorf("old delegated work does not show as Agent working")
		}
	}
	// The old work counts as asked of agents.
	if sent := must(t, SentList, person, SentListInput{}); len(sent.Items) != 4 {
		t.Errorf("sent = %d items", len(sent.Items))
	}
}
