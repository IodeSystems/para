package ops

import (
	"testing"

	"github.com/iodesystems/para/internal/model"
)

// setIO changes the meta of an io item directly, for fields no op sets yet.
func setIO(t *testing.T, f *agentFix, id string, fn func(m *model.IO)) {
	t.Helper()
	doc, m, err := getIO(t.Context(), f.owner, id)
	if err != nil {
		t.Fatal(err)
	}
	fn(m)
	if _, err := f.owner.Store.Update(t.Context(), doc); err != nil {
		t.Fatal(err)
	}
}

func draftTo(t *testing.T, f *agentFix, text string) string {
	t.Helper()
	return mustRun(t, IOAdd, f.owner, IOAddInput{Direction: model.IOOut, Text: text, Subject: "Quote",
		To: []string{"person-ada-example"}, Medium: model.MediumEmail, Expects: model.ExpectsNone}).ID
}

func TestIOProposalsGroupsAndVariationSets(t *testing.T) {
	f := ioFix(t)
	a, b := draftTo(t, f, "Draft A."), draftTo(t, f, "Draft B.")
	v1, v2 := draftTo(t, f, "Variant one."), draftTo(t, f, "Variant two.")
	lone := draftTo(t, f, "Alone.")
	for _, id := range []string{a, b} {
		setIO(t, f, id, func(m *model.IO) { m.Group = "batch-1"; m.Why = "Because." })
	}
	for _, id := range []string{v1, v2} {
		setIO(t, f, id, func(m *model.IO) { m.Group = "vary"; m.Assessments = []model.IOAssessment{{Name: "risk", Percent: 10}} })
	}
	setIO(t, f, v2, func(m *model.IO) { m.VariantOf = v1 })
	mustRun(t, IOEdit, f.owner, IOEditInput{ID: a, Text: "Draft A, edited."})
	mustRun(t, IOApprove, f.owner, IOApproveInput{ID: b})

	out := mustRun(t, IOProposals, f.owner, IOProposalsInput{})
	if out.Count != 5 || len(out.Groups) != 3 {
		t.Fatalf("out = %+v", out)
	}
	byGroup := map[string]IOProposalGroup{}
	for _, g := range out.Groups {
		byGroup[g.Group] = g
	}
	if g := byGroup[lone]; len(g.Sets) != 1 || len(g.Sets[0].Drafts) != 1 || g.Sets[0].Root != lone {
		t.Fatalf("lone = %+v", g)
	}
	batch := byGroup["batch-1"]
	if len(batch.Sets) != 2 {
		t.Fatalf("batch sets = %+v", batch.Sets)
	}
	vary := byGroup["vary"]
	if len(vary.Sets) != 1 || vary.Sets[0].Root != v1 || len(vary.Sets[0].Drafts) != 2 {
		t.Fatalf("vary = %+v", vary)
	}
	var da, db IOProposal
	for _, s := range batch.Sets {
		for _, d := range s.Drafts {
			if d.ID == a {
				da = d
			} else {
				db = d
			}
		}
	}
	if da.Revision != 2 || da.Revisions != 1 || da.Approval != "not_approved" || da.Why != "Because." || da.Preview != "Draft A, edited." ||
		len(da.NotStated) != 3 || len(da.Assessments) != 0 || da.Thread.Key != a || len(da.To) != 1 || da.Medium != model.MediumEmail {
		t.Fatalf("a = %+v", da)
	}
	if db.Approval != "approved" || db.ApprovedHash == "" || db.State != model.IOReady {
		t.Fatalf("b = %+v", db)
	}
	if d := vary.Sets[0].Drafts[0]; len(d.NotStated) != 2 || len(d.Assessments) != 1 {
		t.Fatalf("variant = %+v", d)
	}
	// Edit after approval: changed.
	mustRun(t, IOEdit, f.owner, IOEditInput{ID: b, Text: "Draft B, later."})
	if got := mustRun(t, IOProposals, f.owner, IOProposalsInput{Group: "batch-1"}); got.Count != 2 {
		t.Fatalf("group filter = %+v", got)
	}
	if got := mustRun(t, IOProposals, f.owner, IOProposalsInput{State: model.IOReady}); got.Count != 0 {
		t.Fatalf("ready after edit = %+v", got)
	}
}

func TestIOSentReplyOnThreadAndAskState(t *testing.T) {
	f := ioFix(t)
	// An incoming message and a reply draft that answers it.
	in := mustRun(t, IOAdd, f.owner, IOAddInput{Direction: model.IOIn, Text: "Can you send the quote?", From: "[[person-ada-example]]",
		Via: &model.IOVia{Source: "mail", Ref: "m1", Thread: "t1"}, At: "2026-11-01T09:00:00Z"}).ID
	out1 := mustRun(t, IOAdd, f.owner, IOAddInput{Direction: model.IOOut, Text: "Here is the quote.", Subject: "Quote", To: []string{"person-ada-example"},
		Medium: model.MediumEmail, Expects: model.ExpectsResponse, Route: "domain-logging", Thread: in}).ID
	out2 := mustRun(t, IOAdd, f.owner, IOAddInput{Direction: model.IOOut, Text: "FYI.", Subject: "FYI", To: []string{"person-ada-example"},
		Medium: model.MediumEmail, Expects: model.ExpectsNone}).ID
	setIO(t, f, out1, func(m *model.IO) { m.VariantOf = "" })
	mustRun(t, IOEdit, f.owner, IOEditInput{ID: out1, Text: "Here is the quote, revised."})
	mustRun(t, IOSend, f.owner, IOSendInput{ID: out1, URL: "https://mail.acme.example/1", At: "2026-11-02T09:00:00Z"})
	mustRun(t, IOSend, f.owner, IOSendInput{ID: out2, URL: "https://mail.acme.example/2", At: "2026-11-03T09:00:00Z"})
	// The reply: later, in the same conversation.
	reply := mustRun(t, IOAdd, f.owner, IOAddInput{Direction: model.IOIn, Text: "Thanks!", From: "[[person-ada-example]]",
		Via: &model.IOVia{Source: "mail", Ref: "m2", Thread: "t1"}, At: "2026-11-04T09:00:00Z"}).ID

	got := mustRun(t, IOSent, f.owner, IOSentInput{})
	if len(got.Rows) != 2 || got.Rows[0].ID != out2 || got.Rows[1].ID != out1 {
		t.Fatalf("rows = %+v", got.Rows)
	}
	if r := got.Rows[0]; r.Reply != nil || r.Ask == nil || r.Ask.Status != "" || r.TextChanged || r.URL != "https://mail.acme.example/2" {
		t.Fatalf("out2 = %+v", r)
	}
	r := got.Rows[1]
	if r.Reply == nil || r.Reply.ID != reply || r.Ask == nil || r.Ask.Type != string(model.TypeRFX) || r.Ask.Status != "open" || !r.TextChanged || r.URL != "https://mail.acme.example/1" {
		t.Fatalf("out1 = %+v", r)
	}
	if got := mustRun(t, IOSent, f.owner, IOSentInput{Since: "2026-11-03"}); len(got.Rows) != 1 {
		t.Fatalf("since = %+v", got.Rows)
	}
	if got := mustRun(t, IOSent, f.owner, IOSentInput{Medium: model.MediumSlack}); len(got.Rows) != 0 {
		t.Fatalf("medium = %+v", got.Rows)
	}
	if got := mustRun(t, IOSent, f.owner, IOSentInput{Limit: 1}); len(got.Rows) != 1 {
		t.Fatalf("limit = %+v", got.Rows)
	}
}
