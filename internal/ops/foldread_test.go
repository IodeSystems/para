package ops

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/upgrade"
)

// A fold never makes data vanish from a read: each reader of policies,
// exceptions, conventions, and constraints gives the same answer on one
// synthetic vault before and after upgrade step policy-docs.

type foldVault struct {
	d                         Deps
	pol, exRule, exWords, con string
	conv, convWords, goal     string
	task, ask                 string
}

func newFoldVault(t *testing.T) foldVault {
	t.Helper()
	now := time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)
	d := clockDeps(t, &now)
	if err := d.Store.SetConfig(ctx, upgrade.ConfigKey, "8"); err != nil {
		t.Fatal(err)
	}
	v := foldVault{d: d}
	must(t, OrgRoleAdd, d, OrgAddInput{Name: "Lead", Org: "acme.example"})
	must(t, OrgDomainAdd, d, OrgAddInput{Name: "Logging", Owner: "role-lead"})
	must(t, OrgPersonAdd, d, OrgAddInput{Name: "Ada Example", Org: "acme.example"})
	must(t, OrgPersonAdd, d, OrgAddInput{Name: "Bo Example", Org: "acme.example"})
	must(t, OrgPersonContactAdd, d, PersonContactInput{ID: "person-ada-example", Method: "email", Value: "ada@acme.example"})
	createDoc(t, d, model.Document{ID: "area-ops", Folder: model.FolderAreas, Title: "Ops", Body: "## Logs\n\nWhere logs go.",
		Meta: &model.Area{Common: model.Common{Type: model.TypeArea}, Status: model.AreaActive}})
	v.pol = must(t, Ingest, d, IngestInput{URL: "https://wiki.acme.example/logging", Title: "Logging policy", Text: "Keep logs for thirty days."}).ID
	must(t, RuleAdd, d, RuleAddInput{Policy: v.pol, Text: "Keep logs 30 days."})
	v.exRule = must(t, ExceptionAdd, d, ExceptionAddInput{Policy: v.pol, Rule: "R1", Kind: model.ExceptionUnderspecified, By: "Ada Example",
		ReviewBy: "2026-11-01", Title: "Thirty days of logs from when", Text: "Write or rotate?"}).ID
	v.exWords = must(t, ExceptionAdd, d, ExceptionAddInput{PolicyText: "the vendor logging rule", Kind: model.ExceptionBug, By: "Bo Example",
		ReviewBy: "2026-11-20", Title: "Vendor logs unclear"}).ID
	v.conv = must(t, ConventionAdd, d, ConventionAddInput{Title: "Tag every log line", Status: model.ConventionAdopted, AppliesIn: []string{"area-ops"},
		Text: "With the logging service name."}).ID
	v.convWords = must(t, ConventionAdd, d, ConventionAddInput{Title: "Short log subjects", AppliesIn: []string{"writing"}, Text: "For logging tickets."}).ID
	v.con = must(t, ConstraintAdd, d, ConstraintAddInput{Says: "No shared log accounts", Why: "audit", By: "Ada Example", Source: "https://acme.example/audit",
		AppliesIn: []string{"area-ops", "prod"}, NotIn: []string{"dev"}, ReviewBy: "2026-11-01", Text: "Per the logging audit."}).ID
	must(t, TagAdd, d, TagInput{IDs: []string{v.exRule, v.con}, Tags: []string{"audit"}})
	v.goal = must(t, ProjectAdd, d, ProjectAddInput{Title: "Ship log retention", Owner: "me", DoneWhen: "Shipped.", Area: "area-ops"}).ID
	v.task = must(t, TaskAdd, d, TaskAddInput{Project: v.goal, Title: "Set log retention", Body: "Per [[" + v.pol + "]] and [[" + v.exRule + "]]."}).ID
	v.ask = addRFX(t, d, RFXAddInput{Ask: "Can we keep logs longer?", Targets: []TargetInput{{To: "person-ada-example", Medium: "email"}}})
	must(t, RFXPathAdd, d, RFXPathAddInput{ID: v.ask, To: "person-bo-example", Medium: "email", Reason: model.FrictionPolicyConflict,
		Why: "The rule is unclear.", About: v.exRule})
	return v
}

// reads is every reader's answer, as JSON with what a fold may change (the
// rev and folder of a document, and the exceptions' order) left out.
func (v foldVault) reads(t *testing.T) map[string]string {
	t.Helper()
	d := v.d
	out := map[string]string{}
	put := func(name string, x any) {
		b, err := json.Marshal(x)
		if err != nil {
			t.Fatal(err)
		}
		out[name] = string(b)
	}
	put("exception_list", must(t, ExceptionList, d, ExceptionListInput{}))
	put("exception_expiring", must(t, ExceptionExpiring, d, ExceptionExpiringInput{Days: 3650}))
	put("convention_list", must(t, ConventionList, d, ConventionListInput{}))
	put("constraint_list", must(t, ConstraintList, d, ConstraintListInput{}))
	for _, id := range []string{v.exRule, v.exWords} {
		s := must(t, ExceptionShow, d, ExceptionShowInput{ID: id})
		s.Rev = ""
		put("exception_show "+id, s)
	}
	for _, id := range []string{v.conv, v.convWords} {
		s := must(t, ConventionShow, d, ConventionShowInput{ID: id})
		s.Rev = ""
		put("convention_show "+id, s)
	}
	s := must(t, ConstraintShow, d, ConstraintShowInput{ID: v.con})
	s.Rev = ""
	put("constraint_show", s)
	ps := must(t, PolicyShow, d, PolicyShowInput{ID: v.pol})
	put("policy_show exceptions", ps.Exceptions)
	put("policy_show rules", ps.RuleList)
	r := must(t, Route, d, RouteInput{Query: "logging"})
	refs := func(rs []model.Ref) []string {
		var ids []string
		for _, x := range rs {
			ids = append(ids, x.ID+"|"+x.Title)
		}
		return ids
	}
	put("route", [][]string{refs(r.SelfServe.Policies), refs(r.SelfServe.Constraints), refs(r.SelfServe.Conventions)})
	put("recheck", must(t, Recheck, d, RecheckInput{DryRun: true}).Items)
	put("recheck tag", must(t, Recheck, d, RecheckInput{DryRun: true, Tag: "audit"}).Items)
	put("recheck scope", must(t, Recheck, d, RecheckInput{DryRun: true, Scope: v.goal}).Items)
	put("rules_in_effect", must(t, RulesInEffect, d, RulesInEffectInput{ID: v.task}).Rules)
	g := must(t, GrillContext, d, GrillContextInput{ID: v.goal})
	var probes []GrillProbe
	for _, p := range g.Probes {
		if p.Kind == ProbeRule {
			probes = append(probes, p)
		}
	}
	put("grill_context", []any{probes, g.Rules})
	f := must(t, ReportFriction, d, FrictionInput{By: "policy"})
	f.Text = ""
	put("report_friction", f)
	return out
}

func TestReadersSameAfterPolicyDocsFold(t *testing.T) {
	v := newFoldVault(t)
	before := v.reads(t)
	// The fixture holds what each reader must find.
	for name, want := range map[string]string{
		"exception_list": v.exWords, "convention_list": v.convWords, "constraint_list": "No shared log accounts", "route": v.convWords,
		"recheck": v.exRule, "recheck tag": v.con, "recheck scope": v.con, "rules_in_effect": v.conv, "grill_context": v.con, "report_friction": `"exceptions_hit":1`,
		"policy_show exceptions": v.exRule,
	} {
		if !strings.Contains(before[name], want) {
			t.Fatalf("%s before the fold lacks %q: %s", name, want, before[name])
		}
	}
	res, err := upgrade.RunForced(ctx, v.d.Store, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.FormatVersion != upgrade.PolicyDocsFormat {
		t.Fatalf("run = %+v", res)
	}
	if doc, _ := v.d.Store.Get(ctx, v.pol); doc.Meta.Header().Type != model.TypeArea {
		t.Fatalf("%s was not folded", v.pol)
	}
	after := v.reads(t)
	// One subtest per reader.
	for name, b := range before {
		t.Run(name, func(t *testing.T) {
			if after[name] != b {
				t.Errorf("changed in the fold:\nbefore %s\nafter  %s", b, after[name])
			}
		})
	}
	// What an Ask is about still takes the policy and the exception by their ids.
	for i, about := range []string{v.pol, v.exRule} {
		to := must(t, OrgPersonAdd, v.d, OrgAddInput{Name: []string{"Cy Example", "Di Example"}[i], Org: "acme.example"}).ID
		if _, err := RFXPathAdd.Run(ctx, v.d, RFXPathAddInput{ID: v.ask, To: to, Medium: "email", Reason: model.FrictionPolicyConflict,
			Why: "Still unclear.", About: about, Override: true}); err != nil {
			t.Fatalf("about %s after the fold: %v", about, err)
		}
	}
	// recheck_record does not write a folded file in archive/.
	if _, err := RecheckRecord.Run(ctx, v.d, RecheckRecordInput{ID: v.con, Result: model.AssumptionHolds, Source: "the owner"}); err == nil || !strings.Contains(err.Error(), "annotate") {
		t.Fatalf("recheck_record of a folded constraint = %v", err)
	}
	mustLintClean(t, v.d)
}
