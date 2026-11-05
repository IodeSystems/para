package ops

import (
	"reflect"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

// rankAt is the options of the RankValue tests: today is Nov 4, the due soon
// window the default.
var rankAt = RankOptions{Today: "2026-11-04", DueSoonDays: DefaultDueSoonDays}

func rowIDs(rows []ValueRow) []string {
	out := []string{}
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
}

// The order is a rule, not a score. Each case states the rule it checks.
func TestRankValue(t *testing.T) {
	cases := []struct {
		name  string
		nodes []ValueNode
		want  []string
		why   map[string]string
		held  []string
	}{
		{"incident first, then by impact; the due date breaks a tie, none last",
			[]ValueNode{
				{ID: "t-undated", Kind: KindTask, Created: "2026-10-01"},
				{ID: "t-dated", Kind: KindTask, Due: "2026-11-20", Created: "2026-10-02"},
				{ID: "inc", Kind: KindIncident, Sev: "sev2", Created: "2026-11-01"},
				{ID: "t-sooner", Kind: KindTask, Due: "2026-11-10", Created: "2026-10-03"},
			},
			[]string{"inc", "t-sooner", "t-dated", "t-undated"}, nil, nil},
		{"worst incident first",
			[]ValueNode{{ID: "b", Kind: KindIncident, Sev: "sev2"}, {ID: "a", Kind: KindIncident, Sev: "sev1"}},
			[]string{"a", "b"}, nil, nil},
		{"a question inherits the due date of what it blocks, through the goal",
			[]ValueNode{
				{ID: "g", Kind: KindGoal, Due: "2026-11-12"},
				{ID: "task", Kind: KindTask, Goal: "g", Deps: []string{"q"}},
				{ID: "q", Kind: KindQuestion},
				{ID: "own", Kind: KindTask, Due: "2026-11-15"},
				{ID: "idle", Kind: KindQuestion},
			},
			[]string{"q", "task", "g", "own", "idle"},
			map[string]string{"q": "blocks 1, goal due Nov 12", "task": "goal due Nov 12, waits on q", "own": "due Nov 15", "idle": "blocks nothing, no date"}, nil},
		{"ties go to higher impact, a goal counts twice, then oldest, then id",
			[]ValueNode{
				{ID: "g", Kind: KindGoal, Due: "2026-11-12"},
				{ID: "a", Kind: KindQuestion, Created: "2026-10-01", Deps: nil},
				{ID: "t1", Kind: KindTask, Goal: "g", Deps: []string{"a"}, Created: "2026-10-01"},
				{ID: "b", Kind: KindQuestion, Created: "2026-10-02"},
				{ID: "t2", Kind: KindTask, Due: "2026-11-12", Deps: []string{"b"}, Created: "2026-10-03"},
				{ID: "t3", Kind: KindTask, Due: "2026-11-12", Deps: []string{"b"}, Created: "2026-10-03"},
			},
			// a: unblocks t1 and the goal (impact 3, due Nov 12); b: t2, t3 (impact 2, due Nov 12).
			[]string{"a", "t1", "b", "g", "t2", "t3"}, nil, nil},
		{"a question that waits on a policy task: the task rises with the dated work the question blocks",
			[]ValueNode{
				{ID: "policy", Kind: KindTask, Created: "2026-10-01"},
				{ID: "q", Kind: KindQuestion, Deps: []string{"policy"}, Deferred: true, WaitOnly: true, WaitDeps: []string{"policy"}},
				{ID: "ship", Title: "ship", Kind: KindTask, Due: "2026-11-08", Deps: []string{"q"}},
				{ID: "other", Kind: KindTask, Due: "2026-11-20"},
			},
			[]string{"policy", "ship", "other"},
			map[string]string{"policy": "blocks 2, ship due Nov 8"}, nil},
		{"a wait plus a date does not pass the date through",
			[]ValueNode{
				{ID: "policy", Kind: KindTask},
				{ID: "q", Kind: KindQuestion, Deps: []string{"policy"}, Deferred: true, DeferUntil: "2027-01-01", WaitDeps: []string{"policy"}},
				{ID: "ship", Kind: KindTask, Due: "2026-11-08", Deps: []string{"q"}},
			},
			[]string{"ship", "policy"}, nil, []string{"q"}},
		{"a ready question names what finished",
			[]ValueNode{{ID: "q", Kind: KindQuestion, ReadyWhy: "ready: Policy notes updated done"}},
			[]string{"q"}, map[string]string{"q": "ready: Policy notes updated done"}, nil},
		{"a deferred item adds no impact and gives no date, and is not listed",
			[]ValueNode{
				{ID: "q", Kind: KindQuestion},
				{ID: "later", Kind: KindTask, Due: "2026-11-05", Deps: []string{"q"}, Deferred: true, DeferUntil: "2027-01-01"},
				{ID: "after", Kind: KindTask, Deps: []string{"later"}},
			},
			[]string{"after", "q"}, map[string]string{"q": "blocks nothing, no date"}, nil},
		{"a deferred question still blocks and is reported when it holds up earlier work",
			[]ValueNode{
				{ID: "q", Kind: KindQuestion, Deferred: true, DeferUntil: "2026-12-01"},
				{ID: "task", Kind: KindTask, Due: "2026-11-10", Deps: []string{"q"}},
			},
			[]string{"task"}, nil, []string{"q"}},
		{"deferred until after the due date is not reported",
			[]ValueNode{
				{ID: "q", Kind: KindQuestion, Deferred: true, DeferUntil: "2026-11-05"},
				{ID: "task", Kind: KindTask, Due: "2026-11-10", Deps: []string{"q"}},
			},
			[]string{"task"}, nil, nil},
		{"someday holds up anything dated",
			[]ValueNode{
				{ID: "q", Kind: KindQuestion, Deferred: true, DeferUntil: "someday"},
				{ID: "task", Kind: KindTask, Due: "2030-01-01", Deps: []string{"q"}},
			},
			[]string{"task"}, nil, []string{"q"}},
		{"hidden work shapes the order and is not listed",
			[]ValueNode{
				{ID: "q", Kind: KindQuestion},
				{ID: "theirs", Kind: KindTask, Hidden: true, Due: "2026-11-09", Deps: []string{"q"}},
			},
			[]string{"q"}, nil, nil},
		{"overdue and due soon come before impact, by date",
			[]ValueNode{
				{ID: "big", Kind: KindQuestion},
				{ID: "w1", Kind: KindTask, Deps: []string{"big"}},
				{ID: "w2", Kind: KindTask, Deps: []string{"big"}},
				{ID: "soon", Kind: KindTask, Due: "2026-11-06"},
				{ID: "late", Kind: KindTask, Due: "2026-11-01"},
				{ID: "week", Kind: KindTask, Due: "2026-11-07"},
			},
			[]string{"late", "soon", "big", "week", "w1", "w2"},
			map[string]string{"late": "overdue; due Nov 1", "soon": "due soon; due Nov 6", "big": "blocks 2", "week": "due Nov 7"}, nil},
		{"a call scheduled after today and what waits on it come last, by start",
			[]ValueNode{
				{ID: "call", Title: "Vendor call", Kind: KindTask, Start: "2026-11-09"},
				{ID: "after", Title: "Follow-up", Kind: KindTask, Due: "2026-11-05", Deps: []string{"call"}},
				{ID: "meet", Title: "Sync", Kind: KindMeeting, Due: "2026-11-08", Start: "2026-11-08"},
				{ID: "now", Kind: KindTask},
				{ID: "today", Kind: KindTask, Start: "2026-11-04"},
			},
			[]string{"now", "today", "meet", "call", "after"},
			map[string]string{"call": "not yet, starts Nov 9; blocks 1, Follow-up due Nov 5", "after": "not yet, waits on Vendor call on Nov 9; due Nov 5, waits on Vendor call",
				"meet": "not yet, starts Nov 8; meeting Nov 8"}, nil},
		{"a dependency that is not open is ignored",
			[]ValueNode{{ID: "t", Kind: KindTask, Deps: []string{"gone"}}},
			[]string{"t"}, nil, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// These cases check the impact and date rules: one effort for
			// all keeps the score in impact order. TestRankValueEffort
			// checks effort.
			for i := range c.nodes {
				c.nodes[i].Effort = model.EffortM
			}
			got := RankValue(c.nodes, rankAt)
			if !reflect.DeepEqual(rowIDs(got.Rows), c.want) {
				t.Errorf("order = %v, want %v", rowIDs(got.Rows), c.want)
			}
			for _, r := range got.Rows {
				if w, ok := c.why[r.ID]; ok && r.Why != w {
					t.Errorf("why(%s) = %q, want %q", r.ID, r.Why, w)
				}
			}
			var held []string
			for _, h := range got.Held {
				held = append(held, h.ID)
			}
			if !reflect.DeepEqual(held, c.held) && (len(held) != 0 || len(c.held) != 0) {
				t.Errorf("held up = %v, want %v", held, c.held)
			}
		})
	}
}

// The same nodes in any input order give the same rows.
func TestRankValueIsStable(t *testing.T) {
	nodes := []ValueNode{
		{ID: "a", Kind: KindQuestion, Created: "2026-10-01"},
		{ID: "b", Kind: KindQuestion, Created: "2026-10-01"},
		{ID: "c", Kind: KindTask, Due: "2026-11-01", Deps: []string{"a"}},
	}
	rev := []ValueNode{nodes[2], nodes[1], nodes[0]}
	if x, y := rowIDs(RankValue(nodes, rankAt).Rows), rowIDs(RankValue(rev, rankAt).Rows); !reflect.DeepEqual(x, y) {
		t.Errorf("%v != %v", x, y)
	}
}

// The owner's complaint: a meeting next week, and the work that can only
// start after it, sat above work that can be done now. Prep for the meeting is
// actionable now and ranks by impact unless it is due soon.
func TestRankValueActionableNowFirst(t *testing.T) {
	nodes := []ValueNode{
		{ID: "m", Title: "Design review", Kind: KindMeeting, Due: "2026-11-12", Start: "2026-11-12", Deps: []string{"p"}, Created: "2026-10-01"},
		{ID: "t1", Kind: KindTask, Due: "2026-11-13", Deps: []string{"m"}, Created: "2026-10-01"},
		{ID: "p", Kind: KindTask, Due: "2026-11-11", Created: "2026-10-01"},
		{ID: "h", Kind: KindTask, Created: "2026-10-01"},
		{ID: "d1", Kind: KindTask, Deps: []string{"h"}, Hidden: true},
		{ID: "d2", Kind: KindTask, Deps: []string{"h"}, Hidden: true},
		{ID: "d3", Kind: KindTask, Deps: []string{"h"}, Hidden: true},
		{ID: "l", Kind: KindTask, Due: "2026-11-20", Created: "2026-10-01"},
	}
	for _, c := range []struct {
		days int
		want []string
	}{
		{2, []string{"h", "p", "l", "m", "t1"}}, // p is due in 7 days: impact
		{7, []string{"p", "h", "l", "m", "t1"}}, // p is due soon
	} {
		got := RankValue(nodes, RankOptions{Today: "2026-11-04", DueSoonDays: c.days})
		if ids := rowIDs(got.Rows); !reflect.DeepEqual(ids, c.want) {
			t.Errorf("due soon %d: order = %v, want %v", c.days, ids, c.want)
		}
		for _, r := range got.Rows {
			wantBucket := BucketNow
			if r.ID == "m" || r.ID == "t1" {
				wantBucket = BucketNotYet
			}
			if r.Bucket != wantBucket {
				t.Errorf("%s bucket = %s, want %s", r.ID, r.Bucket, wantBucket)
			}
			if r.ID == "t1" && (r.After != "m" || r.Starts != "2026-11-12" || !strings.HasPrefix(r.Why, "not yet, waits on meeting Design review on Nov 12")) {
				t.Errorf("t1 = %+v", r)
			}
		}
	}
}

// The complaint in a vault: order and the due soon setting.
func TestOrderPutsWorkThatCanStartNowFirst(t *testing.T) {
	d, ada := agendaVault(t, "2026-11-04T12:00:00Z")
	area := must(t, AreaAdd, d, AreaAddInput{Title: "Platform"}).ID
	task := func(title, due string, deps ...string) string {
		return must(t, TaskAdd, d, TaskAddInput{Area: area, Title: title, Due: due, DependsOn: deps}).ID
	}
	p := task("Prep the review deck", "2026-11-11")
	m := meet(t, d, ada, "Design review", "2026-11-12T15:00:00Z", "2026-11-12T16:00:00Z")
	must(t, TaskDependsAdd, d, TaskDependsInput{ID: m, DependsOn: []string{p}})
	t1 := task("Apply review outcome", "2026-11-13", m)
	h := task("Fix the build cache", "")
	for _, n := range []string{"one", "two", "three"} {
		task("Ship service "+n, "", h)
	}
	l := task("Tidy the wiki", "2026-11-20")
	pos := func() map[string]int {
		out := map[string]int{}
		for i, r := range must(t, OrderOp, d, OrderInput{}).Rows {
			out[r.ID] = i
		}
		return out
	}
	check := func(label string, want ...string) {
		t.Helper()
		at := pos()
		for i := 1; i < len(want); i++ {
			if at[want[i-1]] >= at[want[i]] {
				t.Errorf("%s: %s is not before %s: %v", label, want[i-1], want[i], at)
			}
		}
	}
	// p is due within 14 days, so its focus score doubles and passes h.
	check("default", p, h, l, m, t1)
	must(t, VaultConfigSet, d, VaultConfigSetInput{Key: DueSoonKey, Value: "7"})
	check("due soon 7", p, h, l, m, t1)
	for _, r := range must(t, OrderOp, d, OrderInput{}).Rows {
		if r.ID == t1 && (r.Bucket != BucketNotYet || r.After != m || !strings.HasPrefix(r.Why, "not yet, waits on meeting Design review on Nov 12")) {
			t.Errorf("t1 = %+v", r)
		}
	}
}

func groupOf(o ValueOrder) map[string]ValueRow {
	out := map[string]ValueRow{}
	for _, r := range o.Rows {
		out[r.ID] = r
	}
	return out
}

// Effort, the reply group, and the focus group.
func TestRankValueEffort(t *testing.T) {
	t.Run("an ask to the owner is listed, quick by default, in reply", func(t *testing.T) {
		got := RankValue([]ValueNode{{ID: "ask", Kind: KindAsk, Owed: true, Prep: true, Created: "2026-10-22"}}, rankAt)
		r := groupOf(got)["ask"]
		if r.Group != GroupReply || r.Effort != model.EffortXS || r.EffortSet || !r.Prep || r.WaitingDays != 13 {
			t.Errorf("row = %+v", r)
		}
		if r.Why != "someone waits 13 days; about 10 min; asked of you" {
			t.Errorf("why = %q", r.Why)
		}
		if got.Summary != (OrderSummary{Reply: 1, ReplyMinutes: 10, ReplyPrep: 1}) {
			t.Errorf("summary = %+v", got.Summary)
		}
	})
	t.Run("defaults: question xs, task m; a set effort is kept and marked", func(t *testing.T) {
		rows := groupOf(RankValue([]ValueNode{
			{ID: "q", Kind: KindQuestion},
			{ID: "t", Kind: KindTask},
			{ID: "s", Kind: KindTask, Effort: model.EffortS},
		}, rankAt))
		for id, want := range map[string]model.Effort{"q": model.EffortXS, "t": model.EffortM, "s": model.EffortS} {
			if rows[id].Effort != want || rows[id].EffortSet != (id == "s") {
				t.Errorf("%s = %+v, want effort %s", id, rows[id], want)
			}
		}
	})
	t.Run("reply: longest waiting cost first; days times the people waiting", func(t *testing.T) {
		got := RankValue([]ValueNode{
			{ID: "old", Kind: KindAsk, Owed: true, Created: "2026-10-25"},                                              // 10 days, 1 person
			{ID: "many", Kind: KindAsk, Owed: true, Created: "2026-10-31", Waiters: 3},                                 // 4 days, 3 people
			{ID: "msg", Kind: KindIO, Owed: true, Created: "2026-10-01", Arrived: "2026-11-02", Effort: model.EffortS}, // 2 days
			{ID: "big", Kind: KindAsk, Owed: true, Created: "2026-09-01", Effort: model.EffortM},                       // too big for reply
		}, rankAt)
		if ids := rowIDs(got.Rows); !reflect.DeepEqual(ids, []string{"many", "old", "msg", "big"}) {
			t.Errorf("order = %v", ids)
		}
		rows := groupOf(got)
		if rows["many"].WaitingDays != 12 || rows["big"].Group != GroupImpact || rows["big"].WaitingDays != 64 {
			t.Errorf("rows = %+v", rows)
		}
		if w := rows["many"].Why; !strings.HasPrefix(w, "3 people wait 4 days; about 10 min") {
			t.Errorf("why = %q", w)
		}
		if got.Summary.Reply != 3 || got.Summary.ReplyMinutes != 50 {
			t.Errorf("summary = %+v", got.Summary)
		}
	})
	// small blocks 1 (impact 1, s): (1+1)/2 = 1. large blocks 4 (impact 4, l):
	// 5/8. small wins though it unblocks less.
	nodes := []ValueNode{
		{ID: "small", Kind: KindTask, Effort: model.EffortS},
		{ID: "a", Kind: KindTask, Deps: []string{"small"}, Effort: model.EffortL, Hidden: true},
		{ID: "large", Kind: KindTask, Effort: model.EffortL},
		{ID: "b1", Kind: KindTask, Deps: []string{"large"}, Hidden: true},
		{ID: "b2", Kind: KindTask, Deps: []string{"large"}, Hidden: true},
		{ID: "b3", Kind: KindTask, Deps: []string{"large"}, Hidden: true},
		{ID: "b4", Kind: KindTask, Deps: []string{"large"}, Hidden: true},
		{ID: "mid", Kind: KindTask, Effort: model.EffortM},
		{ID: "waits", Kind: KindTask, Effort: model.EffortXS, Deps: []string{"mid"}},
		{ID: "g", Kind: KindGoal, Effort: model.EffortXS},
	}
	t.Run("focus picks by impact per effort; not a goal or an item that waits", func(t *testing.T) {
		opt := rankAt
		opt.FocusCount = 2
		got := RankValue(nodes, opt)
		// mid unblocks waits: 2/4. waits and g score 1/1 but are not picked.
		if ids := rowIDs(got.Rows); !reflect.DeepEqual(ids, []string{"small", "large", "g", "waits", "mid"}) {
			t.Errorf("order = %v", ids)
		}
		rows := groupOf(got)
		if rows["small"].Group != GroupFocus || rows["large"].Group != GroupFocus || rows["mid"].Group != GroupImpact || got.Summary.Focus != 2 {
			t.Errorf("rows = %+v", rows)
		}
		if w := rows["large"].Why; w != "focus: unblocks 4, a day or more" {
			t.Errorf("why = %q", w)
		}
	})
	t.Run("focus count", func(t *testing.T) {
		for _, n := range []int{0, 1, 3} {
			opt := rankAt
			opt.FocusCount = n
			got := RankValue(nodes, opt)
			if got.Summary.Focus != n {
				t.Errorf("focus count %d: got %d", n, got.Summary.Focus)
			}
			if n == 1 && got.Rows[0].ID != "small" {
				t.Errorf("focus count 1: first = %s", got.Rows[0].ID)
			}
		}
	})
}

// In a vault: an incoming message that expects a response is in reply, an
// outgoing draft is listed with prep, and order.focus_count is read.
func TestOrderReplyAndFocus(t *testing.T) {
	d, ada := agendaVault(t, "2026-11-04T12:00:00Z")
	area := must(t, AreaAdd, d, AreaAddInput{Title: "Platform"}).ID
	msg := must(t, IOAdd, d, IOAddInput{Direction: model.IOIn, Text: "Can you confirm the cutover date?", From: ada, Expects: model.ExpectsResponse, At: "2026-11-01T09:00:00Z"}).ID
	must(t, IOAdd, d, IOAddInput{Direction: model.IOIn, Text: "Newsletter", From: ada})
	draft := must(t, IOAdd, d, IOAddInput{Direction: model.IOOut, Text: "Status update", To: []string{ada}, Expects: model.ExpectsNone}).ID
	for _, n := range []string{"one", "two", "three", "four"} {
		must(t, TaskAdd, d, TaskAddInput{Area: area, Title: "Task " + n, Effort: model.EffortS})
	}
	out := must(t, OrderOp, d, OrderInput{})
	rows := map[string]ValueRow{}
	for _, r := range out.Rows {
		rows[r.ID] = r
	}
	if len(out.Rows) != 6 {
		t.Errorf("rows = %v, want the message, the draft, and four tasks", rowIDs(out.Rows))
	}
	if r := rows[msg]; r.Kind != KindIO || r.Group != GroupReply || r.WaitingDays != 3 || !r.Prep || out.Rows[0].ID != msg {
		t.Errorf("message = %+v", r)
	}
	if r := rows[draft]; !r.Prep || r.Owed || r.Group == GroupReply {
		t.Errorf("draft = %+v", r)
	}
	if out.Summary.Reply != 1 || out.Summary.ReplyPrep != 1 || out.Summary.Focus != DefaultFocusCount {
		t.Errorf("summary = %+v", out.Summary)
	}
	must(t, VaultConfigSet, d, VaultConfigSetInput{Key: FocusCountKey, Value: "1"})
	if got := must(t, OrderOp, d, OrderInput{}).Summary.Focus; got != 1 {
		t.Errorf("focus count 1: got %d", got)
	}
}

// Effort is set through the add and edit ops; a bad value is refused.
func TestEffortThroughOps(t *testing.T) {
	d, _ := agendaVault(t, "2026-11-04T12:00:00Z")
	area := must(t, AreaAdd, d, AreaAddInput{Title: "Platform"}).ID
	task := must(t, TaskAdd, d, TaskAddInput{Area: area, Title: "Ship", DoneWhen: "Tagged."}).ID
	if out := must(t, TaskEdit, d, TaskEditInput{ID: task, Effort: model.EffortL}); out.Effort != model.EffortL || out.DoneWhen != "Tagged." {
		t.Errorf("task_edit = %+v", out)
	}
	q := must(t, QuestionAdd, d, QuestionAddInput{Text: "Which region?", Root: true, Effort: model.EffortS}).ID
	must(t, QuestionEdit, d, QuestionEditInput{ID: q, Effort: model.EffortM})
	rows := map[string]ValueRow{}
	for _, r := range must(t, OrderOp, d, OrderInput{}).Rows {
		rows[r.ID] = r
	}
	if rows[task].Effort != model.EffortL || rows[q].Effort != model.EffortM || !rows[q].EffortSet {
		t.Errorf("rows = %+v", rows)
	}
	if _, err := TaskEdit.Run(ctx, d, TaskEditInput{ID: task, Effort: "huge"}); err == nil || !strings.Contains(err.Error(), "effort") {
		t.Errorf("bad effort: %v", err)
	}
	if _, err := TaskEdit.Run(ctx, d, TaskEditInput{ID: task}); err == nil {
		t.Error("task_edit with nothing accepted")
	}
}

// An ask made to the owner is listed and marked owed; an ask the owner made
// is listed too, not owed.
func TestOrderListsAskToOwner(t *testing.T) {
	d, _ := agendaVault(t, "2026-11-04T12:00:00Z")
	in := addRFX(t, d, RFXAddInput{Direction: model.RFXIn, Ask: "Review the retention numbers", Targets: []TargetInput{{To: "person-bo-example", Medium: "email"}}})
	out := addRFX(t, d, RFXAddInput{Ask: "Send the access list"})
	rows := map[string]ValueRow{}
	for _, r := range must(t, OrderOp, d, OrderInput{}).Rows {
		rows[r.ID] = r
	}
	if r, ok := rows[in]; !ok || r.Kind != KindAsk || !r.Owed || !strings.Contains(r.Why, "asked of you") {
		t.Errorf("inbound ask = %+v, %v; want listed, owed", r, ok)
	}
	if r, ok := rows[out]; !ok || r.Owed {
		t.Errorf("outbound ask = %+v, %v; want listed, not owed", r, ok)
	}
}

// Recheck: overdue by more than order.stale_days is not due soon.
func TestRankValueRecheck(t *testing.T) {
	opt := rankAt // today Nov 4
	opt.StaleDays = DefaultStaleDays
	nodes := []ValueNode{
		{ID: "old", Kind: KindTask, Due: "2026-07-23"},                  // 104 days
		{ID: "older", Kind: KindTask, Due: "2026-07-01"},                // 126 days
		{ID: "edge", Kind: KindTask, Due: "2026-10-05"},                 // 30 days: inside
		{ID: "late", Kind: KindTask, Due: "2026-10-06"},                 // 29 days: due soon
		{ID: "over", Kind: KindTask, Due: "2026-10-04"},                 // 31 days: recheck
		{ID: "inc", Kind: KindIncident, Sev: "sev1", Due: "2026-01-01"}, // never recheck
		{ID: "rest", Kind: KindTask},
	}
	got := groupOf(RankValue(nodes, opt))
	for id, want := range map[string]string{"old": GroupRecheck, "older": GroupRecheck, "over": GroupRecheck, "edge": GroupDueSoon, "late": GroupDueSoon, "inc": GroupIncident, "rest": GroupImpact} {
		if got[id].Group != want {
			t.Errorf("%s: group %s, want %s", id, got[id].Group, want)
		}
	}
	if w := got["old"].Why; w != "overdue since Jul 23 (3 months); recheck: still needed?" {
		t.Errorf("why = %q", w)
	}
	order := rowIDs(RankValue(nodes, opt).Rows)
	if !reflect.DeepEqual(order, []string{"inc", "edge", "late", "older", "old", "over", "rest"}) {
		t.Errorf("order = %v", order)
	}
	t.Run("stale_days respected", func(t *testing.T) {
		o := opt
		o.StaleDays = 120
		g := groupOf(RankValue(nodes, o))
		if g["older"].Group != GroupRecheck || g["old"].Group != GroupDueSoon {
			t.Errorf("older %s, old %s", g["older"].Group, g["old"].Group)
		}
		o.StaleDays = 0
		if g := groupOf(RankValue(nodes, o)); g["older"].Group != GroupDueSoon {
			t.Errorf("off: older %s", g["older"].Group)
		}
	})
}

// Goal boost: a date within order.goal_soon_days doubles the focus score.
func TestRankValueGoalBoost(t *testing.T) {
	opt := rankAt // today Nov 4
	opt.FocusCount = 1
	opt.GoalSoonDays = DefaultGoalSoonDays
	nodes := []ValueNode{
		{ID: "gfar", Kind: KindGoal, Title: "Far", Due: "2027-01-03"},   // 60 days
		{ID: "gnear", Kind: KindGoal, Title: "Near", Due: "2026-11-14"}, // 10 days
		{ID: "far", Kind: KindTask, Goal: "gfar", Created: "2026-10-01"},
		{ID: "near", Kind: KindTask, Goal: "gnear", Created: "2026-10-02"},
		{ID: "more", Kind: KindTask, Deps: []string{"far"}, Hidden: true}, // far unblocks one more
	}
	g := groupOf(RankValue(nodes, opt))
	if g["near"].Group != GroupFocus || g["far"].Group != GroupImpact {
		t.Fatalf("near %s, far %s", g["near"].Group, g["far"].Group)
	}
	if w := g["near"].Why; w != "focus: goal due Nov 14; half a day" {
		t.Errorf("why = %q", w)
	}
	// Without the window far scores 3/4 against 2/4.
	off := opt
	off.GoalSoonDays = 0
	if g := groupOf(RankValue(nodes, off)); g["far"].Group != GroupFocus {
		t.Errorf("off: far %s", g["far"].Group)
	}
	// goal_soon_days respected: 5 days is too short for 10.
	short := opt
	short.GoalSoonDays = 5
	if g := groupOf(RankValue(nodes, short)); g["far"].Group != GroupFocus {
		t.Errorf("short: far %s", g["far"].Group)
	}
}
