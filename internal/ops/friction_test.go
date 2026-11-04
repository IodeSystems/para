package ops

import (
	"encoding/json"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/docs"
	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store/local"
	"github.com/iodesystems/para/internal/vault"
)

// clockDeps is a vault whose clock the test moves.
func clockDeps(t *testing.T, now *time.Time) Deps {
	t.Helper()
	root := filepath.Join(t.TempDir(), "v")
	tpl, err := fs.Sub(docs.Templates, "templates")
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Init(root, "acme", tpl, vault.InitOptions{}); err != nil {
		t.Fatal(err)
	}
	noGate(t, root)
	s, err := local.Open(root, vault.Options{ID: "acme", Actor: "user", Surface: "cli", Install: "m1",
		StateDir: filepath.Join(filepath.Dir(root), "state"), Now: func() time.Time { return *now }})
	if err != nil {
		t.Fatal(err)
	}
	return Deps{Store: s}
}

func TestFrictionReport(t *testing.T) {
	now := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	d := clockDeps(t, &now)
	must(t, OrgRoleAdd, d, OrgAddInput{Name: "Lead", Org: "acme.example"})
	must(t, OrgDomainAdd, d, OrgAddInput{Name: "Logging", Owner: "role-lead"})
	must(t, OrgPersonAdd, d, OrgAddInput{Name: "Ada Example", Org: "acme.example"})
	must(t, OrgPersonAdd, d, OrgAddInput{Name: "Bo Example", Org: "acme.example"})
	must(t, OrgPersonContactAdd, d, PersonContactInput{ID: "person-ada-example", Method: "email", Value: "ada@acme.example"})
	a := addRFX(t, d, RFXAddInput{Ask: "Can we log request bodies?", Route: "domain-logging", SelfServe: "searched",
		Targets: []TargetInput{{To: "person-ada-example", Medium: "email"}}})
	must(t, RFXTargetState, d, RFXTargetStateInput{ID: a, To: "person-ada-example", State: model.TargetSent, Date: "2026-11-01"})
	now = now.Add(48 * time.Hour)
	must(t, RFXTargetState, d, RFXTargetStateInput{ID: a, To: "person-ada-example", State: model.TargetNoReply, Reason: model.FrictionNoResponse})
	ex := must(t, ExceptionAdd, d, ExceptionAddInput{PolicyText: "Logging standard", Kind: model.ExceptionUnderspecified, By: "unknown"})
	must(t, RFXPathAdd, d, RFXPathAddInput{ID: a, To: "person-bo-example", Medium: "email", Reason: model.FrictionPolicyConflict, Why: "The standard is unclear.", About: ex.ID})
	now = now.Add(24 * time.Hour)
	must(t, RFXStatus, d, RFXStatusInput{ID: a, Status: model.RFXAnswered})
	now = now.Add(24 * time.Hour)
	must(t, RFXClose, d, RFXCloseInput{ID: a})
	// A second Ask that only stalled, with no reason code: not friction.
	addRFX(t, d, RFXAddInput{Ask: "Can we log request bodies?", Route: "domain-logging", SelfServe: "searched"})

	out := must(t, ReportFriction, d, FrictionInput{})
	if out.Asks != 2 || out.WithFriction != 1 {
		t.Fatalf("asks %d with friction %d", out.Asks, out.WithFriction)
	}
	rows := map[string]FrictionRow{}
	for _, r := range out.Rows {
		rows[r.Key] = r
	}
	if len(rows) != 2 || len(rows["policy_conflict"].Asks) != 1 || rows["policy_conflict"].Asks[0] != a || rows["policy_conflict"].PolicyConflicts != 1 || rows["policy_conflict"].ExceptionsHit != 1 {
		t.Fatalf("rows = %+v", out.Rows)
	}
	r := rows["no_response"]
	if r.Stalls != 1 || r.Redirects != 1 || r.HopsPerAsk != 2 {
		t.Fatalf("no_response = %+v", r)
	}
	if r.FirstAnswerDays == nil || *r.FirstAnswerDays != 3 || r.ResolutionDays == nil || *r.ResolutionDays != 4 {
		t.Fatalf("times: first %v resolution %v", r.FirstAnswerDays, r.ResolutionDays)
	}
	if len(out.Repeats) != 1 || len(out.Repeats[0].Asks) != 2 || r.PlaybookCandidates != 1 {
		t.Fatalf("repeats = %+v, candidates %d", out.Repeats, r.PlaybookCandidates)
	}
	// By domain: one row, titled by the domain.
	out = must(t, ReportFriction, d, FrictionInput{By: FrictionByDomain})
	if len(out.Rows) != 1 || out.Rows[0].Key != "domain-logging" || out.Rows[0].Name != "Logging" {
		t.Fatalf("by domain = %+v", out.Rows)
	}
	// By policy with no policy linked: one row for none.
	out = must(t, ReportFriction, d, FrictionInput{By: FrictionByPolicy})
	if len(out.Rows) != 1 || out.Rows[0].Key != "(none)" {
		t.Fatalf("by policy = %+v", out.Rows)
	}
	// Window: nothing is created after the since date.
	out = must(t, ReportFriction, d, FrictionInput{Since: "2026-12-01"})
	if out.Asks != 0 || len(out.Rows) != 0 {
		t.Fatalf("since = %+v", out)
	}
}

// The report has no per-person view and names no person.
func TestFrictionReportNeverRanksPeople(t *testing.T) {
	now := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	d := clockDeps(t, &now)
	must(t, OrgPersonAdd, d, OrgAddInput{Name: "Ada Example", Org: "acme.example"})
	a := addRFX(t, d, RFXAddInput{Ask: "Q", SelfServe: "x", Targets: []TargetInput{{To: "person-ada-example", Medium: "email"}}})
	must(t, RFXTargetState, d, RFXTargetStateInput{ID: a, To: "person-ada-example", State: model.TargetNoReply})
	for _, by := range []string{"person", "owner", "actor"} {
		if _, err := ReportFriction.Run(ctx, d, FrictionInput{By: by}); err == nil || !strings.Contains(err.Error(), "no per-person") {
			t.Errorf("by %s: err = %v", by, err)
		}
	}
	for _, by := range []string{"reason", "domain", "policy"} {
		out := must(t, ReportFriction, d, FrictionInput{By: by})
		b, _ := json.Marshal(out)
		if strings.Contains(string(b), "ada") {
			t.Errorf("by %s names a person: %s", by, b)
		}
	}
}

func TestFrictionExportLogsEgress(t *testing.T) {
	now := time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)
	d := clockDeps(t, &now)
	out := must(t, ReportFriction, d, FrictionInput{To: "file:" + filepath.Join(t.TempDir(), "r.txt")})
	if out.Destination == "" || out.Ext != "txt" || !strings.Contains(out.Text, "Friction by reason") {
		t.Fatalf("out = %+v", out)
	}
	evs, err := d.Store.Events(ctx, 50)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range evs {
		found = found || (e.Doc == "friction-report" && strings.HasPrefix(e.To, "export:"))
	}
	if !found {
		t.Errorf("no egress event: %+v", evs)
	}
}
