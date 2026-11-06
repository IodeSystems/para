package ops

import (
	"slices"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/paramd"
	"github.com/iodesystems/para/internal/store"
	"github.com/iodesystems/para/internal/upgrade"
)

// policyDocsVault is a vault at the policy docs format with an area and a
// followed source (ingest makes an area of it) with one rule.
func policyDocsVault(t *testing.T) (Deps, string) {
	t.Helper()
	d := newDeps(t)
	if err := d.Store.SetConfig(ctx, upgrade.ConfigKey, "9"); err != nil {
		t.Fatal(err)
	}
	createDoc(t, d, model.Document{ID: "area-ops", Folder: model.FolderAreas, Title: "Ops", Body: "Intro.\n\n## Logging\n\nWhere logs go.\n\n## Access\n\nWho gets in.",
		Meta: &model.Area{Common: model.Common{Type: model.TypeArea}, Status: model.AreaActive}})
	pol := must(t, Ingest, d, IngestInput{URL: "https://wiki.acme.example/logging", Title: "Logging policy",
		Text: "Keep logs for thirty days.\n\nRotate them every week."}).ID
	if doc, _ := d.Store.Get(ctx, pol); doc.Folder != model.FolderAreas || doc.Meta.(*model.Area).Follows == nil {
		t.Fatalf("ingest at format 9 = %s %+v", doc.Folder, doc.Meta)
	}
	must(t, RuleAdd, d, RuleAddInput{Policy: pol, Text: "Keep logs 30 days."})
	return d, pol
}

func calloutOf(t *testing.T, d Deps, area, id string) paramd.Callout {
	t.Helper()
	doc, err := d.Store.Get(ctx, area)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range paramd.Callouts(doc.Body) {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("%s has no %s:\n%s", area, id, doc.Body)
	return paramd.Callout{}
}

func TestAnnotateAddMoveAndChange(t *testing.T) {
	d, pol := policyDocsVault(t)
	ex := must(t, Annotate, d, AnnotateInput{Area: pol, Anchor: "^R1", Kind: "exception", Text: "Thirty days from what?",
		Fields: map[string]string{"kind": "underspecified", "by": "me", "review_by": "2027-01-31"}})
	if ex.Heading != "Rules" || ex.Anchor != "^R1" || !paramd.ValidAnnotationID(ex.ID) {
		t.Fatalf("annotate = %+v", ex)
	}
	c := calloutOf(t, d, pol, ex.ID)
	if c.Field("kind") != "underspecified" || c.Field("by") != "me" || c.Field("anchor") != "^R1" || c.Text != "Thirty days from what?" {
		t.Fatalf("callout = %+v", c)
	}
	// A rule added after the annotation is not read as its text.
	must(t, RuleAdd, d, RuleAddInput{Policy: pol, Text: "Rotate weekly."})
	if rl := must(t, RuleList, d, RuleListInput{Policy: pol}); len(rl.Rules) != 2 {
		t.Fatalf("rules = %+v", rl.Rules)
	}
	if c := calloutOf(t, d, pol, ex.ID); c.Text != "Thirty days from what?" {
		t.Fatalf("the rule joined the callout: %q", c.Text)
	}
	mustLintClean(t, d)

	note := must(t, Annotate, d, AnnotateInput{Area: "area-ops", Kind: "note", Text: "Ask Ops first."})
	conv := must(t, Annotate, d, AnnotateInput{Area: "area-ops", Anchor: "## Logging", Kind: "convention", Text: "Tag every line.",
		Fields: map[string]string{"applies_in": "area-ops, prod"}})
	if note.Heading != "" || conv.Heading != "Logging" {
		t.Fatalf("note %+v conv %+v", note, conv)
	}
	// Move the note to Access, then change a field in place.
	if mv := must(t, Annotate, d, AnnotateInput{Area: "area-ops", ID: note.ID, Anchor: "Access"}); mv.Heading != "Access" {
		t.Fatalf("move = %+v", mv)
	}
	must(t, Annotate, d, AnnotateInput{Area: "area-ops", ID: note.ID, Fields: map[string]string{"by": "[[area-ops]]"}})
	if c := calloutOf(t, d, "area-ops", note.ID); c.Heading != "Access" || c.Field("by") != "[[area-ops]]" || c.Text != "Ask Ops first." {
		t.Fatalf("changed note = %+v", c)
	}

	for name, in := range map[string]AnnotateInput{
		"no kind":          {Area: "area-ops", Text: "x"},
		"no text":          {Area: "area-ops", Kind: "note"},
		"unknown field":    {Area: "area-ops", Kind: "note", Text: "x", Fields: map[string]string{"color": "red"}},
		"missing required": {Area: "area-ops", Kind: "exception", Text: "x", Fields: map[string]string{"kind": "bug"}},
		"bad date":         {Area: "area-ops", Kind: "note", Text: "x", Fields: map[string]string{"review_by": "soon"}},
		"bad kind value":   {Area: "area-ops", Kind: "exception", Text: "x", Fields: map[string]string{"kind": "meh", "by": "me"}},
		"separator":        {Area: "area-ops", Kind: "note", Text: "x", Fields: map[string]string{"by": "a · b"}},
		"no such heading":  {Area: "area-ops", Kind: "note", Text: "x", Anchor: "Nope"},
		"no such block":    {Area: "area-ops", Kind: "note", Text: "x", Anchor: "^R9"},
		"quote, no source": {Area: "area-ops", Kind: "note", Text: "x", Quote: "logs"},
		"not an area":      {Area: "nope", Kind: "note", Text: "x"},
	} {
		if _, err := Annotate.Run(ctx, d, in); err == nil {
			t.Errorf("%s: annotate worked", name)
		}
	}
	mustLintClean(t, d)

	// area_show: each section with its text and annotations in body order.
	show := must(t, AreaShow, d, AreaShowInput{ID: "area-ops"})
	var got []string
	for _, s := range show.Sections {
		for _, p := range s.Parts {
			if p.Annotation != nil {
				got = append(got, s.Heading+":"+p.Annotation.Kind)
			} else {
				got = append(got, s.Heading+":text")
			}
		}
	}
	if want := []string{":text", "Logging:text", "Logging:convention", "Access:text", "Access:note"}; !slices.Equal(got, want) {
		t.Fatalf("sections = %v, want %v", got, want)
	}
	if len(show.Annotations) != 2 || show.Follows != nil {
		t.Fatalf("area_show = %+v", show)
	}
	if ps := must(t, AreaShow, d, AreaShowInput{ID: pol}); ps.Follows == nil || ps.Follows.URL != "https://wiki.acme.example/logging" || len(ps.Annotations) != 1 {
		t.Fatalf("followed area_show = %+v", ps)
	}
	// policy_list and policy_show read the followed area; its exception shows.
	if pl := must(t, PolicyList, d, PolicyListInput{}); len(pl.Policies) != 1 || pl.Policies[0].ID != pol || pl.Policies[0].Rules != 2 {
		t.Fatalf("policy_list = %+v", pl)
	}
	if ps := must(t, PolicyShow, d, PolicyShowInput{ID: pol}); len(ps.Exceptions) != 1 || ps.Exceptions[0].ID != ex.ID || ps.Exceptions[0].Rule != model.RuleLink(pol, "R1") {
		t.Fatalf("policy_show = %+v", ps.Exceptions)
	}
}

func TestAnnotateReceiptUndo(t *testing.T) {
	d, _ := policyDocsVault(t)
	before, _ := d.Store.Get(ctx, "area-ops")
	ag := agentDeps(d, "agent")
	if _, err := Annotate.Run(WithWrite(ctx, WriteMeta{Why: "The owner said so."}), ag, AnnotateInput{Area: "area-ops", Kind: "note", Text: "From the agent."}); err != nil {
		t.Fatal(err)
	}
	recs := must(t, ReceiptList, d, ReceiptListInput{Op: "annotate"})
	if len(recs.Items) != 1 {
		t.Fatalf("receipts = %+v", recs.Items)
	}
	must(t, ReceiptUndo, d, ReceiptUndoInput{ID: recs.Items[0].ID})
	if after, _ := d.Store.Get(ctx, "area-ops"); after.Body != before.Body {
		t.Fatalf("undo left:\n%s", after.Body)
	}
}

// Quote anchors are found again in each version; refresh never rewrites the summary.
func TestAnnotationQuoteAcrossVersions(t *testing.T) {
	d, pol := policyDocsVault(t)
	q := must(t, Annotate, d, AnnotateInput{Area: pol, Anchor: "Rules", Kind: "note", Text: "Which week?", Quote: "Rotate them every week."})
	if c := calloutOf(t, d, pol, q.ID); c.Field("quote") != "Rotate them every week." || c.Field("version") != "1" {
		t.Fatalf("quote callout = %+v", c)
	}
	summary, _ := d.Store.Get(ctx, pol)
	// Version 2 moves the quoted words: found again.
	r := must(t, PolicyRefresh, d, PolicyRefreshInput{ID: pol, Text: "New intro.\n\nKeep logs for thirty days.\n\nRotate them every week."})
	if r.Result != RefreshChanged || r.Version != 2 || len(r.AnchorsLost) != 0 {
		t.Fatalf("refresh 2 = %+v", r)
	}
	if v1, err := d.Store.Get(ctx, pol+"-v1.source"); err != nil || !strings.Contains(v1.Body, "Keep logs for thirty days.\n\nRotate") || v1.Meta.(*model.Transcript).Version != 1 {
		t.Fatalf("version 1 kept = %+v, %v", v1, err)
	}
	if hasHealth(t, d, HealthAnnotationLost, pol) {
		t.Fatal("a found quote is a lost-anchor finding")
	}
	vs := must(t, AnnotationList, d, AnnotationListInput{Area: pol})
	if len(vs.Annotations) != 1 || slices.ContainsFunc(vs.Annotations, func(r AnnotationRow) bool { return r.Lost }) {
		t.Fatalf("annotations after v2 = %+v", vs.Annotations)
	}
	// Version 3 drops them: lost, and health says so.
	r = must(t, PolicyRefresh, d, PolicyRefreshInput{ID: pol, Text: "Keep logs for thirty days."})
	if r.Version != 3 || !slices.Equal(r.AnchorsLost, []string{q.ID}) {
		t.Fatalf("refresh 3 = %+v", r)
	}
	if !hasHealth(t, d, HealthAnnotationLost, pol) {
		t.Fatal("a lost quote is not a finding")
	}
	if after, _ := d.Store.Get(ctx, pol); after.Body != summary.Body {
		t.Fatalf("refresh rewrote the summary:\n%s", after.Body)
	}
	mustLintClean(t, d)
}

func hasHealth(t *testing.T, d Deps, kind, id string) bool {
	t.Helper()
	h, err := HealthAt(ctx, d.Store, "2026-11-06")
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range h.Groups {
		for _, f := range g.Findings {
			if f.Kind == kind && f.ID == id {
				return true
			}
		}
	}
	return false
}

func TestAnnotationHealthAndNeedsYou(t *testing.T) {
	d, pol := policyDocsVault(t)
	soon := must(t, Annotate, d, AnnotateInput{Area: "area-ops", Kind: "exception", Text: "Soon.",
		Fields: map[string]string{"kind": "bug", "by": "me", "review_by": "2026-11-15"}})
	must(t, Annotate, d, AnnotateInput{Area: pol, Kind: "constraint", Text: "Far.",
		Fields: map[string]string{"why": "audit", "by": "me", "review_by": "2026-12-31"}})
	sn, err := SnapshotAt(ctx, d.Store, "2026-11-06")
	if err != nil {
		t.Fatal(err)
	}
	if n := sn.Need("area-ops"); !n.Yes || !strings.Contains(n.Reason, soon.ID) {
		t.Fatalf("area with an exception due in 9 days = %+v", n)
	}
	if n := sn.Need(pol); n.Yes || n.On != "2026-12-17" {
		t.Fatalf("area with a constraint due in 55 days = %+v", n)
	}
	// Resolved: no longer the owner's.
	must(t, Annotate, d, AnnotateInput{Area: "area-ops", ID: soon.ID, Fields: map[string]string{"status": "resolved"}})
	if sn, _ = SnapshotAt(ctx, d.Store, "2026-11-06"); sn.Need("area-ops").Yes {
		t.Fatal("a resolved exception needs the owner")
	}
	exp := must(t, AnnotationList, d, AnnotationListInput{Expiring: 60})
	if len(exp.Annotations) != 1 || exp.Annotations[0].Area != pol || exp.Annotations[0].DaysLeft == nil {
		t.Fatalf("expiring = %+v", exp.Annotations)
	}

	// A hand-broken header is a finding; its text stays.
	doc, _ := d.Store.Get(ctx, "area-ops")
	doc.Body += "\n\n> [!exception] broken by hand\n> Still here."
	if _, err := d.Store.Update(ctx, doc); err != nil {
		t.Fatal(err)
	}
	if !hasHealth(t, d, HealthAnnotationBroken, "area-ops") {
		t.Fatal("a broken header is not a finding")
	}
	if show := must(t, AreaShow, d, AreaShowInput{ID: "area-ops"}); !strings.Contains(show.Sections[len(show.Sections)-1].Parts[len(show.Sections[len(show.Sections)-1].Parts)-1].Text, "Still here.") {
		t.Fatalf("the broken callout's text is not shown: %+v", show.Sections)
	}
}

// The old add ops are aliases of annotate at the policy docs format: they
// write the annotation the upgrade step makes of the same file.
func TestOldAddOpsAnnotate(t *testing.T) {
	d, pol := policyDocsVault(t)
	for _, name := range []string{"exception_add", "convention_add", "constraint_add"} {
		if AliasOf(name) != "annotate" || !strings.HasPrefix(Alias(name).Description(), "Alias of annotate") {
			t.Fatalf("%s alias of %q", name, AliasOf(name))
		}
	}
	in := ExceptionAddInput{Policy: pol, Rule: "R1", Kind: model.ExceptionBug, By: "me", ReviewBy: "2027-01-31", Title: "Thirty days", Text: "From when?"}
	got := must(t, ExceptionAdd, d, in)
	c := calloutOf(t, d, pol, got.ID)
	e := &model.PolicyException{Common: model.Common{Type: model.TypePolicyException}, Policy: model.LinkTo(pol), Rule: model.RuleLink(pol, "R1"),
		Kind: model.ExceptionBug, Status: model.ExceptionOpen, By: "me", ReviewBy: "2027-01-31"}
	f, _ := upgrade.AnnotationOf(model.Document{Title: "Thirty days", Body: "From when?", Meta: e}, storeAreas{ctx, d.Store})
	if !slices.Equal(c.Fields, f.Callout.Fields) || c.Text != f.Callout.Text || c.Heading != "Rules" {
		t.Fatalf("exception_add = %+v\nfold = %+v", c, f.Callout)
	}
	// Through the alias entry too, with alias_of.
	raw, err := Alias("convention_add").Invoke(ctx, d, []byte(`{"title":"Tag lines","applies_in":["area-ops"],"status":"adopted"}`))
	if err != nil {
		t.Fatal(err)
	}
	ao := raw.(AliasOutput)
	cid := ao.Value().(ConventionAddOutput).ID
	if ao.Of() != "annotate" || calloutOf(t, d, "area-ops", cid).Field("status") != "adopted" {
		t.Fatalf("convention_add alias = %+v", ao)
	}
	if out := must(t, ConstraintAdd, d, ConstraintAddInput{Says: "No shared accounts", Why: "audit", By: "me", Source: "https://acme.example/a",
		AppliesIn: []string{"area-ops"}, ReviewBy: "2027-02-01"}); calloutOf(t, d, "area-ops", out.ID).Field("source") != "https://acme.example/a" {
		t.Fatalf("constraint_add = %+v", out)
	}
	// With no area of its own, it goes in the area of its kind, made once,
	// as the upgrade step does; no convention file is written.
	for _, title := range []string{"Short subjects", "Plain words"} {
		o := must(t, ConventionAdd, d, ConventionAddInput{Title: title, AppliesIn: []string{"writing"}})
		if c := calloutOf(t, d, "conventions", o.ID); c.Field("applies_in") != "writing" {
			t.Fatalf("orphan convention = %+v", c)
		}
	}
	if refs, _ := d.Store.List(ctx, store.Filter{Type: model.TypeConvention}); len(refs) != 0 {
		t.Fatalf("convention files = %v", refs)
	}
	// rules_in_effect reads conventions and constraints in areas.
	rs := must(t, RulesInEffect, d, RulesInEffectInput{ID: "area-ops"})
	if len(rs.Rules) != 2 || rs.Rules[0].Type != model.TypeConvention || rs.Rules[0].Title != "Tag lines" || rs.Rules[1].Title != "No shared accounts" {
		t.Fatalf("rules in effect = %+v", rs.Rules)
	}
	mustLintClean(t, d)
}
