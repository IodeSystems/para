package upgrade

import (
	"context"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/docs"
	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/paramd"
	"github.com/iodesystems/para/internal/store"
	"github.com/iodesystems/para/internal/store/local"
	"github.com/iodesystems/para/internal/vault"
)

// policyFixture is a synthetic vault at format 8 with a policy and its
// transcript, an area, and exceptions, conventions, and constraints that
// fold into them or into new orphan areas.
func policyFixture(t *testing.T) (*fixture, store.Store) {
	t.Helper()
	dir := t.TempDir()
	f := &fixture{t: t, root: filepath.Join(dir, "v"), home: filepath.Join(dir, "state")}
	tpl, err := fs.Sub(docs.Templates, "templates")
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Init(f.root, "acme", tpl, vault.InitOptions{NoGit: true}); err != nil {
		t.Fatal(err)
	}
	s := f.open("one")
	ctx := context.Background()
	if err := s.SetConfig(ctx, ConfigKey, "8"); err != nil {
		t.Fatal(err)
	}
	src := model.Source{URL: "https://wiki.acme.example/logging", Retrieved: "2026-11-01T09:00:00Z", Fidelity: model.FidelityExact,
		Hash: "sha256:" + strings.Repeat("ab", 32), Backend: "manual"}
	trSrc := src
	trSrc.Status, trSrc.Checked = "current", src.Retrieved
	for _, d := range []model.Document{
		{ID: "area-ops", Folder: model.FolderAreas, Title: "Ops", Body: "## Logging\n\nWhere logs go.",
			Meta: &model.Area{Common: model.Common{Type: model.TypeArea}, Status: model.AreaActive}},
		{ID: "policy-logging", Folder: model.FolderPolicies, Title: "Logging policy",
			Body: "Summary of the logging policy.\n\n## Rules\n\n- R1 policy-logging: Keep logs 30 days. ^R1\n- R2 policy-logging: Rotate weekly. ^R2",
			Meta: &model.Policy{Common: model.Common{Type: model.TypePolicy}, Followed: model.Followed{Source: src, ReviewedBy: "me"}}},
		{ID: "policy-logging.source", Folder: model.FolderPolicies, Title: "Logging policy (transcript)",
			Body: "Source: " + src.URL + "\n\nKeep logs for thirty days. Rotate them weekly.",
			Meta: &model.Transcript{Common: model.Common{Type: model.TypeTranscript}, Of: model.LinkTo("policy-logging"), Source: trSrc}},
		{ID: "exc-rule", Folder: model.FolderPolicies, Title: "Thirty days from when", Body: "Write time or rotate time?",
			Meta: &model.PolicyException{Common: model.Common{Type: model.TypePolicyException}, Policy: model.LinkTo("policy-logging"),
				Rule: model.RuleLink("policy-logging", "R1"), Kind: model.ExceptionKind("underspecified"), Status: model.ExceptionOpen, By: "me", ReviewBy: "2027-01-31"}},
		{ID: "exc-words", Folder: model.FolderPolicies, Title: "Vendor rule unclear",
			Meta: &model.PolicyException{Common: model.Common{Type: model.TypePolicyException}, PolicyText: "the vendor policy",
				Kind: model.ExceptionKind("bug"), Status: model.ExceptionResolved, By: "me"}},
		{ID: "conv-ops", Folder: model.FolderConventions, Title: "Tag every log line", Body: "With the service name.",
			Meta: &model.Convention{Common: model.Common{Type: model.TypeConvention}, Status: model.ConventionAdopted, AppliesIn: []string{"area-ops"}}},
		{ID: "conv-words", Folder: model.FolderConventions, Title: "Short commit subjects",
			Meta: &model.Convention{Common: model.Common{Type: model.TypeConvention}, Status: model.ConventionDraft, AppliesIn: []string{"writing"}}},
		{ID: "con-ops", Folder: model.FolderConstraints, Title: "No shared accounts",
			Meta: &model.Constraint{Common: model.Common{Type: model.TypeConstraint}, Why: "audit", By: "[[person-x]]",
				Source: model.Source{URL: "https://acme.example/audit", Retrieved: "2026-11-01T09:00:00Z", Fidelity: model.FidelityNone, Backend: "manual"},
				AppliesIn: []string{"area-ops", "prod"}, NotIn: []string{"dev"}, ReviewBy: "2027-02-01"}},
		{ID: "person-x", Folder: model.FolderOrg, Title: "Person X", Meta: &model.Person{Common: model.Common{Type: model.TypePerson}}},
	} {
		// Each has updated, in para's key order: a fold sets updated (every
		// vault write does) and writes keys in that order, so an unfold gives
		// back such a file byte for byte, and not a file without updated or
		// with keys in another order.
		d.Meta.Header().Updated = "2026-11-03"
		if _, err := s.Create(ctx, d); err != nil {
			t.Fatalf("create %s: %v", d.ID, err)
		}
	}
	// An unknown key a person added by hand stays through the fold.
	d, _ := s.Get(ctx, "exc-rule")
	if d, err = d.WithFields(map[string]any{"x_ticket": "OPS-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(ctx, d); err != nil {
		t.Fatal(err)
	}
	return f, s
}

var policyFolded = []string{"con-ops", "conv-ops", "conv-words", "exc-rule", "exc-words", "policy-logging"}

// policyRels is where each fixture file is before the step.
var policyRels = map[string]string{
	"area-ops": "areas/area-ops.md", "policy-logging": "policies/policy-logging.md", "exc-rule": "policies/exc-rule.md",
	"exc-words": "policies/exc-words.md", "conv-ops": "conventions/conv-ops.md", "conv-words": "conventions/conv-words.md",
	"con-ops": "constraints/con-ops.md", "policy-logging.source": "policies/policy-logging.source.md",
}

func TestPolicyDocsStep(t *testing.T) {
	f, s := policyFixture(t)
	ctx := context.Background()
	before := map[string]string{}
	for id, rel := range policyRels {
		before[id] = raw(t, f, rel)
	}
	st, err := StatusOf(ctx, s)
	if err == nil && len(st.Pending) == 1 {
		slices.Sort(st.Pending[0].Docs)
	}
	if err != nil || len(st.Pending) != 1 || st.Pending[0].ID != "policy-docs" || strings.Join(st.Pending[0].Docs, ",") != strings.Join(policyFolded, ",") {
		t.Fatalf("status = %+v, %v", st, err)
	}

	dry, err := Run(ctx, s, true)
	if err != nil {
		t.Fatal(err)
	}
	pd := stepOut(dry, "policy-docs")
	want := []string{"area-ops", "con-ops", "conv-ops", "conv-words", "conventions", "exc-rule", "exc-words", "exceptions", "policy-logging"}
	if strings.Join(pd.Docs, ",") != strings.Join(want, ",") || pd.Batch != "" {
		t.Fatalf("dry run = %+v", dry)
	}
	for id, rel := range policyRels {
		if raw(t, f, rel) != before[id] {
			t.Fatalf("a dry run changed %s", id)
		}
	}

	res, err := Run(ctx, s, false)
	if err != nil {
		t.Fatal(err)
	}
	pd = stepOut(res, "policy-docs")
	if res.FormatVersion != PolicyDocsFormat || pd.Count != len(want) || pd.Batch == "" {
		t.Fatalf("run = %+v", res)
	}
	// The policy is an area that follows the same source; the summary text is verbatim.
	d, err := s.Get(ctx, "policy-logging")
	if err != nil {
		t.Fatal(err)
	}
	a, ok := d.Meta.(*model.Area)
	if !ok || d.Folder != model.FolderAreas || a.Follows == nil || a.Follows.Source.URL != "https://wiki.acme.example/logging" ||
		a.Follows.ReviewedBy != "me" || a.FoldedFrom != model.TypePolicy || a.Kind != model.AreaKindConcept {
		t.Fatalf("policy-logging = %s %+v", d.Folder, d.Meta)
	}
	text := raw(t, f, "areas/policy-logging.md")
	if !strings.Contains(text, "Summary of the logging policy.\n\n## Rules\n\n- R1 policy-logging: Keep logs 30 days. ^R1\n> [!exception] ann-") {
		t.Fatalf("policy-logging body:\n%s", text)
	}
	cs := paramd.Callouts(d.Body)
	if len(cs) != 1 || cs[0].Field("anchor") != "^R1" || cs[0].Field("kind") != "underspecified" || cs[0].Field("review_by") != "2027-01-31" ||
		cs[0].Field(FromField) != "[[exc-rule]]" || cs[0].Text != "Thirty days from when\n\nWrite time or rotate time?" || cs[0].Heading != "Rules" {
		t.Fatalf("callouts = %+v", cs)
	}
	for _, k := range []string{"source:", "verified:", "reviewed_by:"} {
		if strings.Contains(text, "\n"+k) {
			t.Fatalf("policy key %s left at the top level:\n%s", k, text)
		}
	}
	// The originals are in archive/folded, keys and text kept.
	ex := raw(t, f, "archive/folded/exc-rule.md")
	for _, k := range []string{"type: policy_exception", "x_ticket: OPS-1", "folded_into: '[[policy-logging]]'", "Write time or rotate time?"} {
		if !strings.Contains(ex, k) {
			t.Fatalf("archived exc-rule lost %q:\n%s", k, ex)
		}
	}
	ops, _ := s.Get(ctx, "area-ops")
	if cs := paramd.Callouts(ops.Body); len(cs) != 2 || cs[0].Kind != "constraint" || cs[1].Kind != "convention" || cs[0].Field("applies_in") != "area-ops, prod" ||
		cs[0].Field("not_in") != "dev" || cs[0].Field("by") != "[[person-x]]" || cs[1].Field("status") != "adopted" {
		t.Fatalf("area-ops callouts = %+v\n%s", cs, ops.Body)
	}
	for id, kind := range map[string]string{"exceptions": "exception", "conventions": "convention"} {
		o, err := s.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if cs := paramd.Callouts(o.Body); len(cs) != 1 || cs[0].Kind != kind {
			t.Fatalf("%s callouts = %+v", id, cs)
		}
	}
	// The transcript stays where it was and still names its area.
	if raw(t, f, "policies/policy-logging.source.md") != before["policy-logging.source"] {
		t.Fatal("the transcript changed")
	}
	findings, err := s.Lint(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, fd := range findings {
		if fd.Severity == model.SeverityError {
			t.Fatalf("lint after the fold: %s", fd)
		}
	}
	// The optional placement step lists the areas with folded annotations at
	// the top, and does not hold the format back.
	st, _ = StatusOf(ctx, s)
	if st.FormatVersion != PolicyDocsFormat || len(st.Pending) != 0 {
		t.Fatalf("status after = %+v", st)
	}
	if ids := placementOf(t, ctx, s); strings.Join(ids, ",") != "area-ops,conventions,exceptions" {
		t.Fatalf("placement selects %v", ids)
	}

	// A re-run, and a second machine at format 9, do nothing.
	if again, err := Run(ctx, s, false); err != nil || len(again.Steps) != 0 {
		t.Fatalf("re-run = %+v, %v", again, err)
	}
	two := f.open("two")
	if st, err := StatusOf(ctx, two); err != nil || st.FormatVersion != PolicyDocsFormat || len(st.Pending) != 0 {
		t.Fatalf("second machine = %+v, %v", st, err)
	}
	if r, err := Run(ctx, two, false); err != nil || len(r.Steps) != 0 {
		t.Fatalf("second machine run = %+v, %v", r, err)
	}
	// Even at an older format_version, the second machine finds nothing to fold.
	if err := two.SetConfig(ctx, ConfigKey, "8"); err != nil {
		t.Fatal(err)
	}
	if r, err := Run(ctx, two, false); err != nil || changed(r) || r.FormatVersion != PolicyDocsFormat {
		t.Fatalf("second machine at 8 = %+v, %v", r, err)
	}

	// The kept keys and archived files give each original back byte for byte
	// (what another machine, which has no batch, would do).
	for id, b := range unfold(t, ctx, s) {
		if b != before[id] {
			t.Fatalf("unfold of %s:\n%s\nwant\n%s", id, b, before[id])
		}
	}

	// Undo puts every file back, byte for byte, and removes the areas it made.
	un, err := Undo(ctx, s, pd.Batch)
	if err != nil {
		t.Fatal(err)
	}
	if len(un.Docs) != len(want) || len(un.Skipped) != 0 {
		t.Fatalf("undo = %+v", un)
	}
	for id, rel := range policyRels {
		if got := raw(t, f, rel); got != before[id] {
			t.Fatalf("undo of %s:\n%s\nwant\n%s", id, got, before[id])
		}
	}
	for _, id := range []string{"exceptions", "conventions"} {
		if _, err := s.Get(ctx, id); err == nil {
			t.Fatalf("undo left %s", id)
		}
	}
	if v, _ := s.Config(ConfigKey); v != "8" {
		t.Fatalf("format after undo = %q", v)
	}
}

func placementOf(t *testing.T, ctx context.Context, s store.Store) []string {
	t.Helper()
	for _, st := range Steps() {
		if st.ID == "annotation-placement" {
			ids, _, err := matching(ctx, s, st, st.Select)
			if err != nil {
				t.Fatal(err)
			}
			slices.Sort(ids)
			return ids
		}
	}
	t.Fatal("no step annotation-placement")
	return nil
}

// unfold rebuilds each folded original from the vault after the step: the
// archived file without folded_into, and the policy from its area, without
// the folded annotations. It renders them and writes nothing.
func unfold(t *testing.T, ctx context.Context, s store.Store) map[string]string {
	t.Helper()
	out := map[string]string{}
	render := func(d model.Document) string {
		b, err := model.Render(d)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	strip := func(body string) string {
		for _, c := range paramd.Callouts(body) {
			if c.Field(FromField) == "" {
				continue
			}
			var err error
			if body, err = paramd.RemoveCallout(body, c.ID); err != nil {
				t.Fatal(err)
			}
		}
		return body
	}
	refs, err := s.List(ctx, store.Filter{All: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range refs {
		d, err := s.Get(ctx, r.ID)
		if err != nil {
			t.Fatal(err)
		}
		switch m := d.Meta.(type) {
		case *model.Area:
			if m.FoldedFrom == model.TypePolicy {
				p, err := model.Retype(d, model.TypePolicy, map[string]any{"status": nil, "kind": nil, "follows": nil, "folded_from": nil,
					"source": m.Follows.Source, "verified": m.Follows.Verified, "reviewed_by": m.Follows.ReviewedBy})
				if err != nil {
					t.Fatal(err)
				}
				p.Body = strip(p.Body)
				out[d.ID] = render(p)
			} else if d.Folder == model.FolderAreas && d.ID == "area-ops" {
				d.Body = strip(d.Body)
				out[d.ID] = render(d)
			}
		case *model.PolicyException, *model.Convention, *model.Constraint:
			if d.Folder != model.FolderArchive {
				continue
			}
			u, err := d.WithFields(map[string]any{"folded_into": nil})
			if err != nil {
				t.Fatal(err)
			}
			out[d.ID] = render(u)
		}
	}
	if len(out) != 7 {
		t.Fatalf("unfold made %d files", len(out))
	}
	return out
}

func TestPolicyDocsStepRefusesAnOlderPara(t *testing.T) {
	f, s := policyFixture(t)
	ctx := context.Background()
	old, err := local.Open(f.root, vault.Options{ID: "acme", Actor: "user", Surface: "cli", Install: "old",
		StateDir: filepath.Join(f.home, "old"), Now: func() time.Time { return time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	if err := old.Record(ctx, OpSession, "", "v0.3.0", "8", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(ctx, s, false); err == nil || !strings.Contains(err.Error(), "format 8") {
		t.Fatalf("run with format 8 seen = %v", err)
	}
	if d, _ := s.Get(ctx, "policy-logging"); d.Meta.Header().Type != model.TypePolicy {
		t.Fatal("a refused run changed a file")
	}
}

// AnnotationOf is the one mapping: the step and the old add ops use it.
func TestAnnotationOfAnchorsAndOrphans(t *testing.T) {
	areas := vaultAreas{
		"policy-a": {ID: "policy-a", Folder: model.FolderPolicies, Body: "- R1 policy-a: X. ^R1", Meta: &model.Policy{}},
		"area-b":   {ID: "area-b", Folder: model.FolderAreas, Meta: &model.Area{}},
		"gone":     {ID: "gone", Folder: model.FolderArchive, Meta: &model.Area{}},
	}
	ex := func(rule string) model.Document {
		return model.Document{Title: "T", Meta: &model.PolicyException{Common: model.Common{Type: model.TypePolicyException},
			Policy: model.LinkTo("policy-a"), Rule: model.RuleLink("policy-a", rule), Kind: "bug", Status: model.ExceptionOpen, By: "me"}}
	}
	if f, _ := AnnotationOf(ex("R1"), areas); f.Area != "policy-a" || f.Anchor.Block != "R1" || f.Orphan {
		t.Fatalf("rule R1 = %+v", f)
	}
	if f, _ := AnnotationOf(ex("R7"), areas); f.Area != "policy-a" || f.Anchor.Block != "" || f.Callout.Field("anchor") != "" {
		t.Fatalf("a rule no longer there = %+v", f)
	}
	conv := model.Document{Title: "C · D", Meta: &model.Convention{Common: model.Common{Type: model.TypeConvention}, AppliesIn: []string{"gone", "[[area-b]]"}}}
	if f, _ := AnnotationOf(conv, areas); f.Area != "area-b" || f.Callout.Text != "C · D" {
		t.Fatalf("convention = %+v", f)
	}
	con := model.Document{Meta: &model.Constraint{Common: model.Common{Type: model.TypeConstraint}, Why: "a · b", AppliesIn: []string{"gone"}}}
	if f, _ := AnnotationOf(con, areas); f.Area != "constraints" || !f.Orphan || f.Callout.Field("why") != "a - b" {
		t.Fatalf("constraint = %+v", f)
	}
	taken := map[string]bool{AnnotationID("x", func(string) bool { return false }): true}
	if id := AnnotationID("x", func(s string) bool { return taken[s] }); taken[id] || !paramd.ValidAnnotationID(id) || len(id) != len("ann-")+6 {
		t.Fatalf("id after a clash = %q", id)
	}
}
