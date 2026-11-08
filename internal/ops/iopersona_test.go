package ops

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/upgrade"
)

// A synthetic persona for the io tests: one source of each of mail, chat,
// wiki, and tracker, reached via mcp and via cli, under made-up vendor
// labels. Nothing here is anyone's real setup.
type ioPersona struct {
	rv           *reqVault
	owner, agent Deps
	mail, chat   string // sources
	wiki, track  string
	pat          string // a person
}

func newIOPersona(t *testing.T) *ioPersona {
	t.Helper()
	rv := newReqVault(t)
	p := &ioPersona{rv: rv, owner: rv.as("user", "cli", "install-a"), agent: rv.as("claude", "mcp", "install-a")}
	if err := p.owner.Store.SetConfig(t.Context(), upgrade.ConfigKey, strconv.Itoa(upgrade.Latest())); err != nil {
		t.Fatal(err)
	}
	add := func(title string, sys model.SourceSystem, vendor string, via model.SourceVia) string {
		in := SourceAddInput{Title: title, System: sys, Vendor: vendor, Via: via, Cadence: "daily"}
		switch via {
		case model.SourceViaMCP:
			in.Server, in.Tools = "made-up-"+vendor, []string{"search"}
			in.ScanIn = &model.SourceStep{Tool: "search", Query: "after:{since_date}"}
		case model.SourceViaCLI:
			in.ScanIn = &model.SourceStep{Command: vendor + " list --since {since}"}
		}
		return mustRun(t, SourceAdd, p.owner, in).ID
	}
	p.mail = add("Work mail", model.SourceMail, "postbird", model.SourceViaMCP)
	p.chat = add("Team chat", model.SourceChat, "chatterly", model.SourceViaCLI)
	p.wiki = add("Team wiki", model.SourceWiki, "wikiwise", model.SourceViaMCP)
	p.track = add("Work tracker", model.SourceTracker, "tasktrack", model.SourceViaCLI)
	p.pat = mustRun(t, OrgPersonAdd, p.owner, OrgAddInput{Name: "Pat Example", Org: "example.test"}).ID
	mustRun(t, AgentSources, p.agent, AgentSourcesInput{Sources: []string{p.mail, p.chat, p.wiki, p.track}})
	return p
}

// in adds an incoming chat message from from in conversation thread.
func (p *ioPersona) in(t *testing.T, from, text, thread string, expects model.IOExpects, at string) IOAddOutput {
	t.Helper()
	return mustRun(t, IOAdd, p.agent, IOAddInput{Direction: model.IOIn, Text: text, From: from, Medium: model.MediumSlack, Expects: expects, At: at,
		Ref: "https://chat.example.test/m/" + strings.ReplaceAll(at, ":", ""), Via: &model.IOVia{Source: p.chat, Ref: "m-" + at, Thread: thread}})
}

func personaIO(t *testing.T, d Deps, id string) (model.Document, *model.IO) {
	t.Helper()
	doc, m, err := getIO(t.Context(), d, id)
	if err != nil {
		t.Fatal(err)
	}
	return doc, m
}

func oblDoc(t *testing.T, d Deps, id string) *model.Obligation {
	t.Helper()
	_, o, err := getObligation(t.Context(), d, id)
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func TestReplyObligationMadeOnIOAdd(t *testing.T) {
	p := newIOPersona(t)
	// From a person in the vault: owed to them, due reply_days after at.
	a := p.in(t, p.pat, "Can you review the plan by Friday?", "t-1", model.ExpectsResponse, "2026-11-01T08:00:00Z")
	if a.Reply == "" {
		t.Fatalf("no reply obligation: %+v", a)
	}
	o := oblDoc(t, p.owner, a.Reply)
	if !o.Open() || o.Schedule != model.ScheduleOnce || o.Due != "2026-11-03" || len(o.OwedTo) != 1 || o.OwedTo[0] != model.LinkTo(p.pat) ||
		o.ReplyTo != model.LinkTo(a.ID) || len(o.OwedBy) != 0 {
		t.Fatalf("obligation = %+v", o)
	}
	odoc, _ := p.owner.Store.Get(t.Context(), a.Reply)
	if !strings.HasPrefix(odoc.Title, "Reply to Pat Example: ") {
		t.Fatalf("title = %q", odoc.Title)
	}
	if _, m := personaIO(t, p.owner, a.ID); m.Reply != model.LinkTo(a.Reply) {
		t.Fatalf("io reply = %q", m.Reply)
	}
	// Free-text sender: no link, the address in the title and body.
	mustRun(t, VaultConfigSet, p.owner, VaultConfigSetInput{Key: ReplyDaysKey, Value: "5"})
	b := p.in(t, "lee@outside.example.test", "Are you free Tuesday?", "t-2", model.ExpectsResponse, "2026-11-01T08:30:00Z")
	ob := oblDoc(t, p.owner, b.Reply)
	bdoc, _ := p.owner.Store.Get(t.Context(), b.Reply)
	if len(ob.OwedTo) != 0 || ob.Due != "2026-11-06" || !strings.Contains(bdoc.Title, "lee@outside.example.test") || !strings.Contains(bdoc.Body, "From: lee@outside.example.test") {
		t.Fatalf("free text = %+v %q %q", ob, bdoc.Title, bdoc.Body)
	}
	// No reply expected, or the owner's own capture: none.
	if c := p.in(t, p.pat, "FYI the build is green.", "t-3", "", "2026-11-01T08:40:00Z"); c.Reply != "" {
		t.Fatalf("fyi made %s", c.Reply)
	}
	if v := mustRun(t, IOAdd, p.owner, IOAddInput{Direction: model.IOIn, Text: "Pat asked in the hall", Medium: model.MediumVerbal, From: p.pat, Expects: model.ExpectsResponse}); v.Reply != "" {
		t.Fatalf("verbal made %s", v.Reply)
	}
	// Shown once: the obligation needs the owner, the message does not.
	sn, err := NewSnapshot(t.Context(), p.owner.Store)
	if err != nil {
		t.Fatal(err)
	}
	if n := sn.Need(a.ID); n.Yes || !strings.Contains(n.Reason, a.Reply) {
		t.Fatalf("message need = %+v", n)
	}
	if n := sn.Need(a.Reply); !n.Yes {
		t.Fatalf("obligation need = %+v", n)
	}
	// io_edit expects none drops it; expects response again reopens it.
	mustRun(t, IOEdit, p.agent, IOEditInput{ID: a.ID, Expects: model.ExpectsNone})
	if o := oblDoc(t, p.owner, a.Reply); o.Open() || o.Outcome != model.OutcomeDropped {
		t.Fatalf("after expects none = %+v", o)
	}
	mustRun(t, IOEdit, p.agent, IOEditInput{ID: a.ID, Expects: model.ExpectsResponse})
	if o := oblDoc(t, p.owner, a.Reply); !o.Open() {
		t.Fatalf("not reopened = %+v", o)
	}
	// Idempotent: the tick makes no second one.
	if err := p.owner.Store.SetConfig(t.Context(), "request.runner", "install-a"); err != nil {
		t.Fatal(err)
	}
	tk, err := Tick(t.Context(), p.owner, TickInput{})
	if err != nil || len(tk.Replies) != 0 {
		t.Fatalf("tick = %+v %v", tk.Replies, err)
	}
	mustLintClean(t, p.owner)
}

func TestReplyObligationTickBackfills(t *testing.T) {
	p := newIOPersona(t)
	// A message from before reply obligations: no reply link.
	old, err := p.owner.Store.Create(t.Context(), model.Document{Folder: model.FolderIO, Title: "Old ask", Body: "Can you send the numbers?",
		Meta: &model.IO{Common: model.Common{Type: model.TypeIO}, Direction: model.IOIn, Medium: model.MediumEmail, State: model.IORead,
			From: "[[" + p.pat + "]]", At: "2026-10-30T10:00:00Z", Expects: model.ExpectsResponse, Ref: "https://mail.example.test/m/9"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.owner.Store.SetConfig(t.Context(), "request.runner", "install-a"); err != nil {
		t.Fatal(err)
	}
	tk, err := Tick(t.Context(), p.owner, TickInput{})
	if err != nil || len(tk.Replies) != 1 {
		t.Fatalf("tick = %+v %v", tk.Replies, err)
	}
	if _, m := personaIO(t, p.owner, old.ID); m.Reply != model.LinkTo(tk.Replies[0]) {
		t.Fatalf("io = %+v", m)
	}
	if o := oblDoc(t, p.owner, tk.Replies[0]); o.Due != "2026-11-01" {
		t.Fatalf("due = %s", o.Due)
	}
	again, err := Tick(t.Context(), p.owner, TickInput{})
	if err != nil || len(again.Replies) != 0 {
		t.Fatalf("second tick = %+v %v", again.Replies, err)
	}
}

func TestReplyObligationClosesOnReplyAndKeepsOnFile(t *testing.T) {
	p := newIOPersona(t)
	a := p.in(t, p.pat, "Can we move the sync?", "t-1", model.ExpectsResponse, "2026-11-01T08:00:00Z")
	// The owner's reply, threaded: approved, checked, sent: done.
	r := mustRun(t, IOAdd, p.agent, IOAddInput{Direction: model.IOOut, Text: "Yes, Thursday works.", To: []string{p.pat}, Medium: model.MediumSlack,
		Expects: model.ExpectsNone, Thread: a.Reply})
	if _, rm := personaIO(t, p.owner, r.ID); rm.Thread != model.LinkTo(a.ID) {
		t.Fatalf("a draft threaded to the obligation answers the message: %q", rm.Thread)
	}
	mustRun(t, IOApprove, p.owner, IOApproveInput{ID: r.ID})
	mustRun(t, IOSendCheck, p.agent, IOSendCheckInput{ID: r.ID})
	mustRun(t, IOSend, p.agent, IOSendInput{ID: r.ID, URL: "https://chat.example.test/m/r1"})
	if o := oblDoc(t, p.owner, a.Reply); o.Open() || o.Outcome != model.OutcomeDone || !strings.Contains(o.Reason, r.ID) {
		t.Fatalf("after reply = %+v", o)
	}
	// Filed into a task with no reply: still owed, and it links the task.
	b := p.in(t, p.pat, "Can you look at the outage notes?", "t-2", model.ExpectsResponse, "2026-11-01T08:10:00Z")
	area := mustRun(t, AreaAdd, p.owner, AreaAddInput{Title: "Ops"}).ID
	bf := mustRun(t, IOFile, p.agent, IOFileInput{ID: b.ID, To: TriageTask, Area: area, DoneWhen: "The notes are read."})
	if o := oblDoc(t, p.owner, b.Reply); !o.Open() {
		t.Fatalf("after file = %+v", o)
	}
	if od, _ := p.owner.Store.Get(t.Context(), b.Reply); !strings.Contains(od.Body, "[["+bf.Became+"]]") {
		t.Fatalf("obligation body does not link %s: %q", bf.Became, od.Body)
	}
	rs := mustRun(t, ReceiptList, p.owner, ReceiptListInput{Doc: b.ID, Op: "io_file"}).Items
	if len(rs) != 1 {
		t.Fatalf("receipts = %+v", rs)
	}
	mustRun(t, ReceiptUndo, p.owner, ReceiptUndoInput{IDs: []string{rs[0].ID}})
	if _, m := personaIO(t, p.owner, b.ID); !m.State.Open() {
		t.Fatalf("io after undo = %s", m.State)
	}
	if o := oblDoc(t, p.owner, b.Reply); !o.Open() {
		t.Fatalf("obligation after undo = %+v", o)
	}
	// Dropped with no reply: dropped.
	mustRun(t, IODrop, p.agent, IODropInput{ID: b.ID, Reason: "Answered on a call."})
	if o := oblDoc(t, p.owner, b.Reply); o.Open() || !strings.Contains(o.Reason, "Answered on a call.") {
		t.Fatalf("after drop = %+v", o)
	}
	mustLintClean(t, p.owner)
}

func TestPreSendCheckHoldsWhenTheThreadMoved(t *testing.T) {
	p := newIOPersona(t)
	a := p.in(t, p.pat, "Can you approve the rollout?", "t-9", model.ExpectsResponse, "2026-11-01T08:00:00Z")
	draft := func() string {
		return mustRun(t, IOAdd, p.agent, IOAddInput{Direction: model.IOOut, Text: "Approved, go ahead.", To: []string{p.pat}, Medium: model.MediumSlack,
			Expects: model.ExpectsNone, Thread: a.ID}).ID
	}
	d := draft()
	// No check: refused.
	mustRun(t, IOApprove, p.agent, IOApproveInput{ID: d, OwnerSaid: "Send it."})
	if _, err := IOSend.Run(t.Context(), p.agent, IOSendInput{ID: d, URL: "https://chat.example.test/m/x"}); err == nil || !strings.Contains(err.Error(), "pre-send check") {
		t.Fatalf("send without check: %v", err)
	}
	// A new message in the same tool thread (no thread link): held.
	p.rv.advance(10 * time.Minute)
	later := p.in(t, "kim@example.test", "Wait, the rollout is paused.", "t-9", "", "2026-11-01T09:05:00Z")
	chk := mustRun(t, IOSendCheck, p.agent, IOSendCheckInput{ID: d})
	if chk.OK || !chk.Held || len(chk.New) != 1 || chk.New[0].ID != later.ID || !strings.HasPrefix(chk.Reason, "Situation changed: 1 new message") {
		t.Fatalf("check = %+v", chk)
	}
	ddoc, dm := personaIO(t, p.owner, d)
	if dm.State != model.IODraft || dm.ApprovedHash != "" || dm.Held == "" || len(dm.HeldBy) != 1 {
		t.Fatalf("held draft = %+v", dm)
	}
	sn, err := NewSnapshot(t.Context(), p.owner.Store)
	if err != nil {
		t.Fatal(err)
	}
	if n := sn.Need(ddoc.ID); !n.Yes || !strings.HasPrefix(n.Reason, "Situation changed") {
		t.Fatalf("held need = %+v", n)
	}
	if _, err := IOSend.Run(t.Context(), p.agent, IOSendInput{ID: d, URL: "https://chat.example.test/m/x"}); err == nil || !strings.Contains(err.Error(), "held") {
		t.Fatalf("send of held: %v", err)
	}
	// The owner approves again: the new message is now seen; the check passes.
	mustRun(t, IOApprove, p.owner, IOApproveInput{ID: d})
	if chk := mustRun(t, IOSendCheck, p.agent, IOSendCheckInput{ID: d}); !chk.OK || chk.Held {
		t.Fatalf("check after re-approval = %+v", chk)
	}
	// A stale check is refused.
	p.rv.advance(31 * time.Minute)
	if _, err := IOSend.Run(t.Context(), p.agent, IOSendInput{ID: d, URL: "https://chat.example.test/m/x"}); err == nil || !strings.Contains(err.Error(), "older than") {
		t.Fatalf("stale check: %v", err)
	}
	mustRun(t, IOSendCheck, p.agent, IOSendCheckInput{ID: d})
	mustRun(t, IOSend, p.agent, IOSendInput{ID: d, URL: "https://chat.example.test/m/x"})

	// A message read in after the approval with an older at counts; a
	// message from the recipient on the same medium with no thread counts.
	e := draft()
	mustRun(t, IOApprove, p.owner, IOApproveInput{ID: e})
	mustRun(t, IOAdd, p.agent, IOAddInput{Direction: model.IOIn, Text: "One more thing", From: p.pat, Medium: model.MediumSlack, At: "2026-11-01T09:30:00Z",
		Ref: "https://chat.example.test/m/late"})
	if chk := mustRun(t, IOSendCheck, p.agent, IOSendCheckInput{ID: e}); !chk.Held {
		t.Fatalf("recipient on the same medium: %+v", chk)
	}
	// Another medium and another person: not this conversation.
	f := draft()
	mustRun(t, IOApprove, p.owner, IOApproveInput{ID: f})
	mustRun(t, IOAdd, p.agent, IOAddInput{Direction: model.IOIn, Text: "Lunch?", From: "sam@example.test", Medium: model.MediumEmail, At: "2026-11-01T10:00:00Z",
		Ref: "https://mail.example.test/m/lunch"})
	if chk := mustRun(t, IOSendCheck, p.agent, IOSendCheckInput{ID: f}); !chk.OK {
		t.Fatalf("unrelated message held it: %+v", chk)
	}
	mustLintClean(t, p.owner)
}

func TestSweepReportAndIOStats(t *testing.T) {
	p := newIOPersona(t)
	a := p.in(t, p.pat, "Can you sign off?", "t-1", model.ExpectsResponse, "2026-11-01T08:00:00Z")
	p.in(t, "bot@example.test", "Build 41 passed", "t-2", "", "2026-11-01T08:05:00Z")
	mustRun(t, IOAdd, p.agent, IOAddInput{Direction: model.IOIn, Text: "Ticket TT-7 assigned to you", From: "tracker", Medium: model.MediumOther,
		At: "2026-11-01T08:06:00Z", Via: &model.IOVia{Source: p.track, Ref: "TT-7"}})
	mustRun(t, SweepRecord, p.agent, SweepRecordInput{Source: p.chat, Counts: map[string]int{"in": 2}})
	mustRun(t, SweepRecord, p.agent, SweepRecordInput{Source: p.track, Failed: true, Errors: []string{"command timed out"}})
	rep := mustRun(t, SweepReport, p.agent, SweepReportInput{})
	var chat, track *ReportSource
	for i := range rep.Sources {
		switch rep.Sources[i].ID {
		case p.chat:
			chat = &rep.Sources[i]
		case p.track:
			track = &rep.Sources[i]
		}
	}
	if chat == nil || track == nil || len(chat.In) != 2 || len(chat.Replies) != 1 || chat.Replies[0] != a.Reply || chat.Via != model.SourceViaCLI ||
		chat.Vendor != "chatterly" || chat.Sweeps != 1 || len(chat.ReachedBy) != 1 {
		t.Fatalf("chat = %+v", chat)
	}
	if !strings.Contains(chat.In[0].Became, "reply owed by 2026-11-03") || chat.In[1].Became != "digest" {
		t.Fatalf("became = %q, %q", chat.In[0].Became, chat.In[1].Became)
	}
	if len(track.In) != 1 || len(track.Errors) != 1 || !strings.Contains(track.Errors[0], "command timed out") {
		t.Fatalf("tracker = %+v", track)
	}
	if !strings.Contains(rep.Text, p.chat) || !strings.Contains(rep.Text, "error") {
		t.Fatalf("text:\n%s", rep.Text)
	}

	// The reply goes out a day later: stats time it.
	p.rv.advance(26 * time.Hour)
	r := mustRun(t, IOAdd, p.agent, IOAddInput{Direction: model.IOOut, Text: "Signed.", To: []string{p.pat}, Medium: model.MediumSlack,
		Expects: model.ExpectsNone, Thread: a.ID})
	mustRun(t, IOApprove, p.owner, IOApproveInput{ID: r.ID})
	mustRun(t, IOSendCheck, p.agent, IOSendCheckInput{ID: r.ID})
	mustRun(t, IOSend, p.agent, IOSendInput{ID: r.ID, URL: "https://chat.example.test/m/r1", At: "2026-11-02T08:00:00Z"})
	st := mustRun(t, IOStats, p.agent, IOStatsInput{})
	var cs, ts *IOSourceStats
	for i := range st.Sources {
		switch st.Sources[i].ID {
		case p.chat:
			cs = &st.Sources[i]
		case p.track:
			ts = &st.Sources[i]
		}
	}
	if cs == nil || ts == nil || len(cs.Windows) != 2 || cs.Windows[0].Days != 7 || cs.Windows[0].In != 2 || cs.Windows[0].Out != 1 ||
		cs.Windows[0].Sweeps != 1 || cs.Windows[0].ReplyHours != 24 || cs.OpenReplies != 0 {
		t.Fatalf("chat stats = %+v", cs)
	}
	if ts.LastFailure == "" || ts.LastError != "command timed out" || ts.Windows[0].Failed != 1 || ts.LastScan != "" {
		t.Fatalf("tracker stats = %+v", ts)
	}
	// An open reply past due is overdue.
	p.in(t, p.pat, "Can you check the invoice?", "t-5", model.ExpectsResponse, "2026-11-02T09:00:00Z")
	p.rv.advance(5 * 24 * time.Hour)
	st = mustRun(t, IOStats, p.agent, IOStatsInput{Days: 30})
	for _, s := range st.Sources {
		if s.ID == p.chat && (s.OpenReplies != 1 || s.Overdue != 1 || len(s.Windows) != 1) {
			t.Fatalf("overdue = %+v", s)
		}
	}
	for _, id := range []string{p.mail, p.wiki} {
		found := false
		for _, s := range st.Sources {
			found = found || s.ID == id
		}
		if !found {
			t.Fatalf("source %s missing from stats", id)
		}
	}
}

// Filing a message that waits for the owner's reply, into any target, new or
// existing, keeps the reply owed: it stays open, lists in the reply group,
// and links what the message was filed into. A threaded reply sent later
// closes it done.
func TestReplyObligationKeptOnEveryFilingTarget(t *testing.T) {
	p := newIOPersona(t)
	area := mustRun(t, AreaAdd, p.owner, AreaAddInput{Title: "Ops"}).ID
	task := mustRun(t, TaskAdd, p.owner, TaskAddInput{Area: area, Title: "Read the outage notes", DoneWhen: "The notes are read."}).ID
	cases := []struct {
		name string
		in   IOFileInput
	}{
		{"existing task", IOFileInput{Existing: task}},
		{"new task", IOFileInput{To: TriageTask, Area: area, DoneWhen: "Done."}},
		{"note", IOFileInput{To: TriageNote, Area: area}},
		{"question", IOFileInput{To: TriageQuestion, Root: true}},
		{"interaction", IOFileInput{To: TriageInteraction}},
	}
	for i, c := range cases {
		at := "2026-11-01T09:0" + strconv.Itoa(i) + ":00Z"
		a := p.in(t, p.pat, "Can you check "+c.name+"?", "t-k"+strconv.Itoa(i), model.ExpectsResponse, at)
		if a.Reply == "" {
			t.Fatalf("%s: no reply obligation", c.name)
		}
		c.in.ID = a.ID
		f := mustRun(t, IOFile, p.agent, c.in)
		o := oblDoc(t, p.owner, a.Reply)
		if !o.Open() {
			t.Fatalf("%s: filed into %s, reply closed: %+v", c.name, f.Became, o)
		}
		if od, _ := p.owner.Store.Get(t.Context(), a.Reply); !strings.Contains(od.Body, "[["+f.Became+"]]") {
			t.Fatalf("%s: obligation body does not link %s: %q", c.name, f.Became, od.Body)
		}
		// A tick leaves it open and makes no second one.
		if tk, err := Tick(t.Context(), p.owner, TickInput{}); err != nil || len(tk.Replies) != 0 {
			t.Fatalf("%s: tick = %+v %v", c.name, tk.Replies, err)
		}
		if o := oblDoc(t, p.owner, a.Reply); !o.Open() {
			t.Fatalf("%s: tick closed it: %+v", c.name, o)
		}
		if i > 0 {
			continue
		}
		// The owner replies on the thread after the message was filed: done.
		r := mustRun(t, IOAdd, p.agent, IOAddInput{Direction: model.IOOut, Text: "Reading them now.", To: []string{p.pat}, Medium: model.MediumSlack,
			Expects: model.ExpectsNone, Thread: a.Reply})
		mustRun(t, IOApprove, p.owner, IOApproveInput{ID: r.ID})
		mustRun(t, IOSendCheck, p.agent, IOSendCheckInput{ID: r.ID})
		mustRun(t, IOSend, p.agent, IOSendInput{ID: r.ID, URL: "https://chat.example.test/m/rk"})
		if o := oblDoc(t, p.owner, a.Reply); o.Open() || o.Outcome != model.OutcomeDone || !strings.Contains(o.Reason, r.ID) {
			t.Fatalf("after reply to a filed message = %+v", o)
		}
		if _, m := personaIO(t, p.owner, a.ID); m.Became != model.LinkTo(task) {
			t.Fatalf("the reply moved became: %s", m.Became)
		}
	}
	mustLintClean(t, p.owner)
}
