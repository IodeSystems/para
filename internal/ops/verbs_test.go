package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

// verbVault is a vault with a goal, a person, the owner on cli and pwa, and an
// agent on mcp. The clock is fixed, so two of them get the same ids.
type verbVault struct {
	rv              *reqVault
	cli, pwa, agent Deps
	proj            string
	person          string
}

func newVerbVault(t *testing.T) *verbVault {
	t.Helper()
	rv := newReqVault(t)
	v := &verbVault{rv: rv, cli: rv.as("user", "cli", "install-a"), pwa: rv.as("user", "pwa", "install-a"), agent: rv.as("claude", "mcp", "install-a")}
	p, err := GoalAdd.Run(t.Context(), v.cli, GoalAddInput{DoneWhen: "Done.", Title: "Portal", Owner: "Jordan Example"})
	if err != nil {
		t.Fatal(err)
	}
	v.proj = p.ID
	per, err := OrgPersonAdd.Run(t.Context(), v.cli, OrgAddInput{Name: "Sam Example"})
	if err != nil {
		t.Fatal(err)
	}
	v.person = per.ID
	return v
}

func (v *verbVault) task(t *testing.T, title string) string {
	t.Helper()
	out, err := TaskAdd.Run(t.Context(), v.cli, TaskAddInput{Goal: v.proj, Title: title, DoneWhen: "It is done."})
	if err != nil {
		t.Fatal(err)
	}
	return out.ID
}

func (v *verbVault) meeting(t *testing.T, title string) string {
	t.Helper()
	out, err := MeetingAdd.Run(t.Context(), v.cli, MeetingAddInput{Title: title, With: []string{v.person},
		Start: "2026-10-30T10:00:00Z", End: "2026-10-30T10:30:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	return out.ID
}

func (v *verbVault) question(t *testing.T, text string) string {
	t.Helper()
	out, err := QuestionAdd.Run(t.Context(), v.cli, QuestionAddInput{Text: text, Goal: v.proj})
	if err != nil {
		t.Fatal(err)
	}
	return out.ID
}

func (v *verbVault) io(t *testing.T, dir model.IODirection, text string) string {
	t.Helper()
	in := IOAddInput{Direction: dir, Text: text, Medium: model.MediumEmail}
	if dir == model.IOOut {
		in.To, in.Expects = []string{v.person}, model.ExpectsNone
	} else {
		in.From, in.Ref = v.person, "https://mail.example/m/9"
	}
	out, err := IOAdd.Run(t.Context(), v.cli, in)
	if err != nil {
		t.Fatal(err)
	}
	return out.ID
}

func (v *verbVault) rfx(t *testing.T, ask string) string {
	t.Helper()
	return addRFX(t, v.cli, RFXAddInput{Ask: ask})
}

func (v *verbVault) incident(t *testing.T, title string) string {
	t.Helper()
	out, err := IncidentAdd.Run(t.Context(), v.cli, IncidentAddInput{Title: title, Severity: model.Sev3})
	if err != nil {
		t.Fatal(err)
	}
	return out.ID
}

func (v *verbVault) request(t *testing.T, ask string) string {
	t.Helper()
	out, err := RequestAdd.Run(t.Context(), v.cli, RequestAddInput{Ask: ask})
	if err != nil {
		t.Fatal(err)
	}
	return out.ID
}

func (v *verbVault) on(surface string) Deps {
	switch surface {
	case "pwa":
		return v.pwa
	case "mcp":
		return v.agent
	}
	return v.cli
}

// parityCase calls an old name and its verb on two vaults made the same way.
type parityCase struct {
	old, verb, surface string
	seed               func(t *testing.T, v *verbVault) string
	oldIn, verbIn      string // %s is the seeded id
}

func fill(s, id string) string { return strings.ReplaceAll(s, "%s", id) }

// receiptOps lists op, actor, and surface of every receipt of doc, oldest
// first.
func receiptOps(t *testing.T, d Deps, doc string) []string {
	t.Helper()
	list, err := listReceipts(t.Context(), d.Store)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for i := len(list) - 1; i >= 0; i-- {
		if list[i].Doc == doc {
			out = append(out, list[i].Op+" "+list[i].Actor+" "+list[i].Surface)
		}
	}
	return out
}

// sameIDs is v as JSON with id b written as a, the ids of documents the
// call made written as ID, and revs dropped.
func sameIDs(v any, a, b string) string {
	raw, _ := json.Marshal(v)
	s := strings.ReplaceAll(string(raw), b, a)
	s = strings.ReplaceAll(s, a, "SEED")
	s = madeID.ReplaceAllString(s, "ID")
	return revRe.ReplaceAllString(s, `"rev":""`)
}

var (
	revRe  = regexp.MustCompile(`"rev":"[^"]*"`)
	madeID = regexp.MustCompile(`\b[a-z]+-[0-9a-z]{20,}\b`)
)

// wrapsOp are the internal ops that run another op's code under their own
// name (TestOldOpParity).
var wrapsOp = map[string]bool{"item_close": true, "stack_later": true, "stack_return": true}

// An internal op the CLI still runs by its old name, with its old input, does
// what the verb an agent calls instead does: the same output, document, and
// receipts.
func TestOldOpParity(t *testing.T) {
	recur := func(t *testing.T, v *verbVault) string {
		id := v.task(t, "Water plants")
		if _, err := DocEdit.Run(t.Context(), v.cli, DocEditInput{ID: id, Fields: map[string]any{"recur": "weekly", "due": "2026-11-01"}}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	deferred := func(t *testing.T, v *verbVault) string {
		id := v.question(t, "Which vendor?")
		if _, err := Defer.Run(t.Context(), v.cli, DeferInput{IDs: []string{id}, Until: "2026-12-01"}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	task := func(t *testing.T, v *verbVault) string { return v.task(t, "Send the plan") }
	person, member, group := personSeeds()
	cases := []parityCase{
		{"task_done", "close", "mcp", task, `{"id":"%s"}`, `{"id":"%s"}`},
		{"task_done", "close", "mcp", task, `{"id":"%s","note":"Sent."}`, `{"id":"%s","reason":"Sent."}`},
		{"task_done", "close", "cli", recur, `{"id":"%s","skip":true}`, `{"id":"%s","outcome":"skipped"}`},
		{"task_resolve", "close", "mcp", task, `{"id":"%s","outcome":"dropped","note":"Not needed."}`, `{"id":"%s","outcome":"dropped","reason":"Not needed."}`},
		{"task_resolve", "close", "mcp", task, `{"id":"%s","note":"Shipped."}`, `{"id":"%s","outcome":"done","reason":"Shipped."}`},
		{"task_resolve", "close", "cli", task, `{"id":"%s","outcome":"declined","reason":"Sam said no.","declined_by":"other","date":"2026-11-01"}`,
			`{"id":"%s","outcome":"declined","reason":"Sam said no.","fields":{"declined_by":"other","date":"2026-11-01"}}`},
		{"meeting_hold", "close", "mcp", func(t *testing.T, v *verbVault) string { return v.meeting(t, "Launch sync") },
			`{"id":"%s","notes":"Agreed to ship."}`, `{"id":"%s","reason":"Agreed to ship."}`},
		{"meeting_cancel", "close", "mcp", func(t *testing.T, v *verbVault) string { return v.meeting(t, "Launch sync") },
			`{"id":"%s","reason":"Not needed."}`, `{"id":"%s","outcome":"dropped","reason":"Not needed."}`},
		{"question_drop", "close", "mcp", func(t *testing.T, v *verbVault) string { return v.question(t, "Which vendor?") },
			`{"id":"%s","reason":"Moot."}`, `{"id":"%s","outcome":"dropped","reason":"Moot."}`},
		{"goal_close", "close", "cli", func(t *testing.T, v *verbVault) string {
			p, err := GoalAdd.Run(t.Context(), v.cli, GoalAddInput{DoneWhen: "Done.", Title: "Side goal", Owner: "Jordan Example"})
			if err != nil {
				t.Fatal(err)
			}
			return p.ID
		}, `{"id":"%s","dropped":true}`, `{"id":"%s","outcome":"dropped"}`},
		{"rfx_close", "close", "mcp", func(t *testing.T, v *verbVault) string { return v.rfx(t, "Review the plan") }, `{"id":"%s"}`, `{"id":"%s"}`},
		{"io_drop", "close", "mcp", func(t *testing.T, v *verbVault) string { return v.io(t, model.IOIn, "Newsletter") },
			`{"id":"%s","reason":"Needs nothing."}`, `{"id":"%s","outcome":"dropped","reason":"Needs nothing."}`},
		{"io_done", "close", "pwa", func(t *testing.T, v *verbVault) string { return v.io(t, model.IOOut, "Plan attached") },
			`{"id":"%s","url":"https://mail.example/m/1"}`, `{"id":"%s","outcome":"sent","fields":{"url":"https://mail.example/m/1"}}`},
		{"request_cancel", "close", "cli", func(t *testing.T, v *verbVault) string { return v.request(t, "Check backups") }, `{"id":"%s"}`, `{"id":"%s"}`},
		{"item_close", "close", "pwa", task, `{"id":"%s"}`, `{"id":"%s"}`},
		{"item_close", "close", "pwa", func(t *testing.T, v *verbVault) string { return v.meeting(t, "Launch sync") }, `{"id":"%s"}`, `{"id":"%s"}`},
		{"defer", "later", "cli", task, `{"ids":["%s"],"until":"2026-12-01"}`, `{"ids":["%s"],"until":"2026-12-01"}`},
		{"stack_later", "later", "pwa", func(t *testing.T, v *verbVault) string { return v.question(t, "Which vendor?") },
			`{"questions":["%s"],"until":"2026-12-01"}`, `{"ids":["%s"],"until":"2026-12-01"}`},
		{"undefer", "reopen", "cli", deferred, `{"ids":["%s"]}`, `{"ids":["%s"]}`},
		{"stack_return", "reopen", "pwa", deferred, `{"questions":["%s"]}`, `{"id":"%s"}`},
		{"agent_send", "send_to_agent", "pwa", task, `{"id":"%s","text":"Met Sam; ship it."}`, `{"id":"%s","note":"Met Sam; ship it."}`},
		{"task_show", "show", "mcp", task, `{"id":"%s"}`, `{"id":"%s"}`},
		{"org_person_group_add", "link", "mcp", person, `{"id":"%s","group":"group-atlas"}`, `{"from":"%s","to":"group-atlas","rel":"member_of"}`},
		{"org_person_group_add", "link", "mcp", person, `{"id":"%s","group":"group-atlas"}`, `{"from":"%s","to":"group-atlas"}`},
		{"org_person_group_remove", "link", "mcp", member, `{"id":"%s","group":"group-atlas"}`, `{"from":"%s","to":"group-atlas","rel":"member_of","remove":true}`},
		{"org_group_show", "show", "mcp", group, `{"id":"%s"}`, `{"id":"%s"}`},
	}
	for _, c := range cases {
		t.Run(c.old+"_"+c.surface, func(t *testing.T) {
			v := newVerbVault(t)
			ia, ib := c.seed(t, v), c.seed(t, v)
			da, db := v.on(c.surface), v.on(c.surface)
			wctx := writeCtx(t)
			if !IsInternal(c.old) {
				t.Fatalf("%s is listed", c.old)
			}
			old, err := runRoute(wctx, da, verbRoute{op: registry[c.old], input: json.RawMessage(fill(c.oldIn, ia)), receipt: registry[c.verb].Risk() != RiskRead})
			if err != nil {
				t.Fatalf("old op: %v", err)
			}
			oldOut := old.raw
			newOut, err := registry[c.verb].Invoke(wctx, db, json.RawMessage(fill(c.verbIn, ib)))
			if err != nil {
				t.Fatalf("verb: %v", err)
			}
			var want any
			switch o := newOut.(type) {
			case VerbOutput:
				want = o.raw
			case ShowOutput:
				want = o.raw
			}
			if c.old == "item_close" {
				if got := asMap(oldOut); got["by"] != newOut.(VerbOutput).Op {
					t.Fatalf("item_close by %v, verb ran %s", got["by"], newOut.(VerbOutput).Op)
				}
			} else if a, b := sameIDs(asMap(oldOut), ia, ib), sameIDs(asMap(want), ia, ib); a != b {
				t.Fatalf("output\nold  %s\nverb %s", a, b)
			}
			docA, err := da.Store.Get(t.Context(), ia)
			if err != nil {
				t.Fatal(err)
			}
			docB, err := db.Store.Get(t.Context(), ib)
			if err != nil {
				t.Fatal(err)
			}
			if ta, tb := madeID.ReplaceAllString(strings.ReplaceAll(renderText(docA), ia, "SEED"), "ID"), madeID.ReplaceAllString(strings.ReplaceAll(renderText(docB), ib, "SEED"), "ID"); ta != tb {
				t.Fatalf("doc state\nold  %s\nverb %s", ta, tb)
			}
			ra, rb := receiptOps(t, da, ia), receiptOps(t, db, ib)
			if wrapsOp[c.old] && len(ra) == len(rb) {
				// item_close and the stack ops call the routed op's code
				// themselves: their receipt keeps their own name.
				for i := range ra {
					ra[i] = strings.Replace(ra[i], c.old, strings.Fields(rb[i])[0], 1)
				}
			}
			if !reflect.DeepEqual(ra, rb) {
				t.Fatalf("receipts\nold  %v\nverb %v", ra, rb)
			}
		})
	}
}

// An internal op keeps its gate class: an agent's call applies the same way
// through the old op and the verb.
func TestInternalGateClass(t *testing.T) {
	cases := []struct{ old, oldIn, verb, verbIn string }{
		{"task_done", `{"id":"%s"}`, "close", `{"id":"%s"}`},
		{"task_resolve", `{"id":"%s","outcome":"failed","tried":"Twice.","stopped_because":"No access."}`, "close",
			`{"id":"%s","outcome":"failed","fields":{"tried":"Twice.","stopped_because":"No access."}}`},
		{"defer", `{"ids":["%s"],"until":"2026-12-01"}`, "later", `{"ids":["%s"],"until":"2026-12-01"}`},
	}
	for _, c := range cases {
		run := func(t *testing.T, call func(ctx context.Context, d Deps, id string) error) {
			v := newVerbVault(t)
			id := v.task(t, "Send the plan")
			if err := call(WithWrite(t.Context(), WriteMeta{Why: "The owner asked."}), v.agent, id); err != nil {
				t.Fatalf("%s: %v", c.old, err)
			}
		}
		run(t, func(ctx context.Context, d Deps, id string) error {
			_, err := registry[c.old].Invoke(ctx, d, json.RawMessage(fill(c.oldIn, id)))
			return err
		})
		run(t, func(ctx context.Context, d Deps, id string) error {
			_, err := registry[c.verb].Invoke(ctx, d, json.RawMessage(fill(c.verbIn, id)))
			return err
		})
	}
}

// Who may call what: an agent is refused what only a person may do (io_done,
// undefer, the person aliases); a person runs it; send_to_agent picks the
// op by surface.
func TestVerbScopeMatrix(t *testing.T) {
	type call struct {
		name, input string
		seed        func(t *testing.T, v *verbVault) string
		agentOK     bool
	}
	deferred := func(t *testing.T, v *verbVault) string {
		id := v.task(t, "Later task")
		if _, err := Defer.Run(t.Context(), v.cli, DeferInput{IDs: []string{id}, Until: "2026-12-01"}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	calls := []call{
		{"close", `{"id":"%s"}`, func(t *testing.T, v *verbVault) string { return v.task(t, "T") }, true},
		{"close", `{"id":"%s","outcome":"sent","fields":{"url":"https://mail.example/m/1"}}`, func(t *testing.T, v *verbVault) string { return v.io(t, model.IOOut, "Draft") }, false},
		{"later", `{"id":"%s","until":"2026-12-01"}`, func(t *testing.T, v *verbVault) string { return v.task(t, "T") }, true},
		{"reopen", `{"id":"%s"}`, deferred, false},
		{"reopen", `{"id":"%s","reason":"Not done after all."}`, func(t *testing.T, v *verbVault) string {
			id := v.task(t, "T")
			if _, err := TaskDone.Run(t.Context(), v.cli, TaskDoneInput{ID: id}); err != nil {
				t.Fatal(err)
			}
			return id
		}, true},
		{"send_to_agent", `{"id":"%s","note":"Find the owner.","title":"Find the owner"}`, func(t *testing.T, v *verbVault) string { return v.task(t, "T") }, true},
		{"stack_later", `{"questions":["%s"],"until":"2026-12-01"}`, func(t *testing.T, v *verbVault) string { return v.question(t, "Q?") }, false},
		{"stack_return", `{"questions":["%s"]}`, deferred, false},
		{"item_close", `{"id":"%s"}`, func(t *testing.T, v *verbVault) string { return v.task(t, "T") }, false},
		{"agent_send", `{"id":"%s","text":"Hi."}`, func(t *testing.T, v *verbVault) string { return v.task(t, "T") }, false},
	}
	entry := func(name string) Entry { return registry[name] }
	for _, c := range calls {
		for _, surface := range []string{"mcp", "cli", "pwa"} {
			v := newVerbVault(t)
			id := c.seed(t, v)
			out, err := entry(c.name).Invoke(writeCtx(t), v.on(surface), json.RawMessage(fill(c.input, id)))
			agent := surface == "mcp"
			switch {
			case agent && !c.agentOK:
				// An agent's close sent runs io_send_check first, which
				// refuses a draft the owner has not approved.
				sendRefused := err != nil && strings.Contains(err.Error(), "io_send_check: ") && strings.Contains(err.Error(), "the owner approves it first")
				if !errors.Is(err, ErrPersonOnly) && !sendRefused {
					t.Errorf("%s %s on %s: err %v, want person only", c.name, c.input, surface, err)
				}
			case err != nil:
				t.Errorf("%s %s on %s: %v", c.name, c.input, surface, err)
			}
			if c.name == "send_to_agent" && err == nil {
				want := "agent_send"
				if agent {
					want = "request_add"
				}
				if got := out.(VerbOutput).Op; got != want {
					t.Errorf("send_to_agent on %s ran %s, want %s", surface, got, want)
				}
			}
			if !agent && err == nil && c.name != "send_to_agent" && !strings.HasPrefix(c.name, "stack_") && c.name != "item_close" && c.name != "agent_send" {
				if r := out.(VerbOutput).Receipt; r == "" {
					t.Errorf("%s on %s: no receipt for the web's Undo", c.name, surface)
				}
			}
		}
	}
}

// reopen opens a closed item of every type that closes, with a receipt; the
// receipt's undo closes it again, and the close's undo opens it.
func TestReopenRoundTrip(t *testing.T) {
	cases := []struct {
		name    string
		seed    func(t *testing.T, v *verbVault) string
		closeIn string
	}{
		{"task", func(t *testing.T, v *verbVault) string { return v.task(t, "Send the plan") }, `{"id":"%s"}`},
		{"task dropped", func(t *testing.T, v *verbVault) string { return v.task(t, "Send the plan") }, `{"id":"%s","outcome":"dropped","reason":"Not needed."}`},
		{"meeting", func(t *testing.T, v *verbVault) string { return v.meeting(t, "Launch sync") }, `{"id":"%s","reason":"Agreed."}`},
		{"question", func(t *testing.T, v *verbVault) string { return v.question(t, "Which vendor?") }, `{"id":"%s","outcome":"dropped","reason":"Moot."}`},
		{"goal", func(t *testing.T, v *verbVault) string { return v.proj }, `{"id":"%s"}`},
		{"ask", func(t *testing.T, v *verbVault) string { return v.rfx(t, "Review the plan") }, `{"id":"%s"}`},
		{"ask declined", func(t *testing.T, v *verbVault) string { return v.rfx(t, "Review the plan") }, `{"id":"%s","outcome":"declined"}`},
		{"io in", func(t *testing.T, v *verbVault) string { return v.io(t, model.IOIn, "Newsletter") }, `{"id":"%s","reason":"Needs nothing."}`},
		{"io out", func(t *testing.T, v *verbVault) string { return v.io(t, model.IOOut, "Draft") }, `{"id":"%s","reason":"Will not send."}`},
		{"incident", func(t *testing.T, v *verbVault) string { return v.incident(t, "Login down") }, `{"id":"%s","reason":"Fixed."}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := newVerbVault(t)
			id := c.seed(t, v)
			life := func() Lifecycle {
				doc, err := v.pwa.Store.Get(t.Context(), id)
				if err != nil {
					t.Fatal(err)
				}
				return lifeOf(doc.Meta)
			}
			closed, err := Close.Invoke(t.Context(), v.pwa, json.RawMessage(fill(c.closeIn, id)))
			if err != nil {
				t.Fatal(err)
			}
			if life() != LifeClosed {
				t.Fatal("not closed")
			}
			reopened, err := Reopen.Run(t.Context(), v.pwa, ReopenInput{ID: id, Reason: "Not done after all."})
			if err != nil {
				t.Fatal(err)
			}
			if life() != LifeOpen || reopened.Op != "reopen" || reopened.Receipt == "" {
				t.Fatalf("reopen = %+v, life %s", reopened, life())
			}
			if _, err := Reopen.Run(t.Context(), v.pwa, ReopenInput{ID: id}); err == nil {
				t.Fatal("reopen of an open item")
			}
			if _, err := ReceiptRevert.Run(t.Context(), v.pwa, ReceiptIDInput{ID: reopened.Receipt}); err != nil {
				t.Fatal(err)
			}
			if life() != LifeClosed {
				t.Fatal("undo of reopen did not close it")
			}
			if _, err := ReceiptRevert.Run(t.Context(), v.pwa, ReceiptIDInput{ID: closed.(VerbOutput).Receipt}); err != nil {
				t.Fatal(err)
			}
			if life() != LifeOpen {
				t.Fatal("undo of close did not open it")
			}
		})
	}
}

func TestReopenRefusesSentAndAgentWork(t *testing.T) {
	v := newVerbVault(t)
	io := v.io(t, model.IOOut, "Draft")
	if _, err := Close.Invoke(t.Context(), v.pwa, json.RawMessage(`{"id":"`+io+`","outcome":"sent"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := Reopen.Run(t.Context(), v.pwa, ReopenInput{ID: io}); err == nil || !strings.Contains(err.Error(), "was sent") {
		t.Fatalf("reopen of a sent io = %v", err)
	}
	req := v.request(t, "Check backups")
	if _, err := Close.Invoke(t.Context(), v.cli, json.RawMessage(`{"id":"`+req+`"}`)); err != nil {
		t.Fatal(err)
	}
	if _, err := Reopen.Run(t.Context(), v.cli, ReopenInput{ID: req}); err == nil {
		t.Fatal("reopened agent work")
	}
}

func TestCloseEvidenceMustBeVerifiable(t *testing.T) {
	v := newVerbVault(t)
	id := v.task(t, "Send the plan")
	_, err := Close.Run(t.Context(), v.cli, CloseInput{ID: id, Evidence: &model.Evidence{Quote: "Sent it.", Locator: "slack #ops"}})
	if err == nil {
		t.Fatal("free-text evidence accepted")
	}
	out, err := Close.Run(writeCtx(t), v.agent, CloseInput{ID: id, Evidence: &model.Evidence{Locator: "https://mail.example/m/1"}})
	if err != nil {
		t.Fatal(err)
	}
	list, err := ReceiptList.Run(t.Context(), v.cli, ReceiptListInput{Doc: id})
	if err != nil || len(list.Items) == 0 {
		t.Fatalf("%v %v", list, err)
	}
	if !strings.Contains(strings.Join(list.Items[0].Sources, " "), "https://mail.example/m/1") || out.Op != "task_done" {
		t.Fatalf("receipt sources %v, op %s", list.Items[0].Sources, out.Op)
	}
	if _, err := Close.Run(t.Context(), v.cli, CloseInput{ID: v.proj, Reason: "Shipped."}); err == nil {
		t.Fatal("goal_close took a reason it cannot keep")
	}
}

func TestListAndShow(t *testing.T) {
	v := newVerbVault(t)
	open := v.task(t, "Open one")
	done := v.task(t, "Done one")
	if _, err := Close.Run(t.Context(), v.cli, CloseInput{ID: done}); err != nil {
		t.Fatal(err)
	}
	ids := func(in ListInput) []string {
		out, err := List.Run(t.Context(), v.cli, in)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, r := range out.Rows {
			got = append(got, r.ID)
		}
		return got
	}
	if got := ids(ListInput{Type: model.TypeTask}); !reflect.DeepEqual(got, []string{open}) {
		t.Fatalf("open tasks = %v", got)
	}
	if got := ids(ListInput{Type: model.TypeTask, State: "closed"}); !reflect.DeepEqual(got, []string{done}) {
		t.Fatalf("closed tasks = %v", got)
	}
	if got := ids(ListInput{NeedsYou: true}); !strings.Contains(strings.Join(got, " "), open) {
		t.Fatalf("needs you = %v", got)
	}
	// list {} is needs_you; goal is project.
	if got := ids(ListInput{}); !reflect.DeepEqual(got, ids(ListInput{NeedsYou: true})) {
		t.Fatalf("list {} = %v", got)
	}
	if got, want := ids(ListInput{Type: "goal"}), ids(ListInput{Type: model.TypeGoal}); len(want) == 0 || !reflect.DeepEqual(got, want) {
		t.Fatalf("list goal = %v, project = %v", got, want)
	}
	// needs_you is ranked as order ranks it, and every ranked row comes first.
	ny, err := List.Run(t.Context(), v.cli, ListInput{NeedsYou: true})
	if err != nil {
		t.Fatal(err)
	}
	ord, err := OrderOp.Run(t.Context(), v.cli, OrderInput{})
	if err != nil {
		t.Fatal(err)
	}
	last := 0
	for _, r := range ny.Rows {
		switch {
		case r.Rank == 0:
			last = len(ord.Rows) + 1
		case r.Rank <= last, ord.Rows[r.Rank-1].ID != r.ID:
			t.Fatalf("needs you not in order rank: %+v (order %+v)", ny.Rows, ord.Rows)
		default:
			last = r.Rank
		}
		if r.ID == open && r.Rank == 0 {
			t.Fatalf("open task has no rank: %+v", ny.Rows)
		}
	}
	s, err := Show.Run(t.Context(), v.cli, ShowInput{ID: open})
	if err != nil {
		t.Fatal(err)
	}
	if task, _ := s.Detail["task"].(map[string]any); s.Op != "task_show" || s.Life != LifeOpen || !s.NeedsYou || task["id"] != open {
		t.Fatalf("show = %+v", s)
	}
}

// personSeeds seed a person (person-riley-N, named the same each time), a
// person in group-atlas, and a group (group-crew-N).
func personSeeds() (person, member, group func(t *testing.T, v *verbVault) string) {
	n := 0
	atlas := func(t *testing.T, v *verbVault) {
		if _, err := v.cli.Store.Get(t.Context(), "group-atlas"); err == nil {
			return
		}
		if _, err := OrgGroupAdd.Run(t.Context(), v.cli, GroupAddInput{Name: "Atlas", Org: "acme", Kind: model.GroupTeam}); err != nil {
			t.Fatal(err)
		}
	}
	person = func(t *testing.T, v *verbVault) string {
		atlas(t, v)
		n++
		out, err := OrgPersonAdd.Run(t.Context(), v.cli, OrgAddInput{Name: "Riley Example", ID: fmt.Sprintf("person-riley-%d", n), Aliases: []string{"Ri"}, Email: "riley@acme.example"})
		if err != nil {
			t.Fatal(err)
		}
		return out.ID
	}
	member = func(t *testing.T, v *verbVault) string {
		id := person(t, v)
		if _, err := OrgPersonGroupAdd.Run(t.Context(), v.cli, PersonGroupInput{ID: id, Group: "group-atlas"}); err != nil {
			t.Fatal(err)
		}
		return id
	}
	group = func(t *testing.T, v *verbVault) string {
		n++
		out, err := OrgGroupAdd.Run(t.Context(), v.cli, GroupAddInput{Name: "Crew", Org: "acme", Kind: model.GroupTeam, ID: fmt.Sprintf("group-crew-%d", n)})
		if err != nil {
			t.Fatal(err)
		}
		return out.ID
	}
	return person, member, group
}

// The alias and contact ops an agent no longer calls: edit with the whole
// list leaves the person as the old op does, and refuses what it refuses.
func TestPersonListsByEdit(t *testing.T) {
	person, _, _ := personSeeds()
	cases := []struct{ old, oldIn, editIn string }{
		{"org_person_alias_add", `{"id":"%s","alias":"Riles"}`, `{"id":"%s","fields":{"aliases":["Ri","Riles"]}}`},
		{"org_person_alias_remove", `{"id":"%s","alias":"ri"}`, `{"id":"%s","fields":{"aliases":[]}}`},
		{"org_person_contact_remove", `{"id":"%s","method":"email","value":"riley@acme.example"}`, `{"id":"%s","fields":{"contacts":[]}}`},
	}
	for _, c := range cases {
		t.Run(c.old, func(t *testing.T) {
			v := newVerbVault(t)
			ia, ib := person(t, v), person(t, v)
			if !IsInternal(c.old) {
				t.Fatalf("%s is listed", c.old)
			}
			if _, err := registry[c.old].Invoke(writeCtx(t), v.cli, json.RawMessage(fill(c.oldIn, ia))); err != nil {
				t.Fatalf("old op: %v", err)
			}
			out, err := Edit.Invoke(writeCtx(t), v.agent, json.RawMessage(fill(c.editIn, ib)))
			if err != nil {
				t.Fatalf("edit: %v", err)
			}
			if op := out.(VerbOutput).Op; op != "org_person_edit" {
				t.Fatalf("edit ran %s", op)
			}
			da, _ := v.cli.Store.Get(t.Context(), ia)
			db, _ := v.cli.Store.Get(t.Context(), ib)
			if ta, tb := strings.ReplaceAll(renderText(da), ia, "SEED"), strings.ReplaceAll(renderText(db), ib, "SEED"); ta != tb {
				t.Fatalf("doc state\nold  %s\nedit %s", ta, tb)
			}
		})
	}
	v := newVerbVault(t)
	id := person(t, v)
	for _, c := range []struct{ in, want string }{
		{`{"id":"%s","fields":{"aliases":["Ri","ri"]}}`, `alias "ri" given twice`},
		{`{"id":"%s","fields":{"aliases":["  "]}}`, "alias is empty"},
		{`{"id":"%s","fields":{"contacts":[{"method":"email","value":"a@acme.example"},{"method":"email","value":"A@acme.example"}]}}`, "given twice"},
		{`{"id":"%s","fields":{"contacts":[{"method":"pigeon","value":"x"}]}}`, "medium"},
		{`{"id":"%s","fields":{"contacts":[{"method":"email","value":"a@acme.example","preferred":true},{"method":"email","value":"b@acme.example","preferred":true}]}}`, "two preferred email"},
	} {
		if _, err := Edit.Invoke(writeCtx(t), v.agent, json.RawMessage(fill(c.in, id))); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err %v, want %q", c.in, err, c.want)
		}
	}
	// The old op refuses an alias the person has; edit is given the whole list.
	if _, err := OrgPersonAliasAdd.Run(writeCtx(t), v.cli, PersonAliasInput{ID: id, Alias: "ri"}); err == nil || !strings.Contains(err.Error(), "already recorded") {
		t.Errorf("alias_add of a recorded alias: %v", err)
	}
	// link to an item that is not a group, or from one that is not a person.
	if _, err := Link.Invoke(writeCtx(t), v.agent, json.RawMessage(fill(`{"from":"%s","to":"`+v.proj+`"}`, id))); err == nil {
		t.Error("member_of a goal: want error")
	}
	if _, err := Link.Invoke(writeCtx(t), v.agent, json.RawMessage(`{"from":"`+v.proj+`","to":"group-atlas","rel":"member_of"}`)); err == nil || !strings.Contains(err.Error(), "links from a person") {
		t.Errorf("member_of from a goal: %v", err)
	}
}
