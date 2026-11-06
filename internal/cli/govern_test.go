package cli

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/export"
	"github.com/iodesystems/para/internal/ops"
)

const (
	govURL  = "https://wiki.acme.example/policies/logging"
	govText = "# Logging policy\r\n\r\nServices log every authentication failure.  \r\n\r\n\r\n## Page 1\r\n\r\nSecond block.\r\n"
)

// ingest runs `para ingest` with text on stdin.
func (e *env) ingest(text string, args ...string) ops.IngestOutput {
	e.t.Helper()
	var out ops.IngestOutput
	e.stdin = text
	e.json(&out, append([]string{"ingest", "--url", govURL}, args...)...)
	return out
}

func (e *env) stdinJSON(v any, text string, args ...string) {
	e.t.Helper()
	e.stdin = text
	e.json(v, args...)
}

// governanceParityCases builds the vault state the stream E ops need and
// returns their parity cases plus the stdin of each CLI run.
func governanceParityCases(e *env) (map[string]parityCase, map[string]string) {
	pol := e.ingest(govText)
	var rule ops.RuleAddOutput
	e.json(&rule, "rule", "add", pol.ID, "Services log every authentication failure.")
	var exc ops.ExceptionAddOutput
	e.json(&exc, "exception", "add", "--policy", pol.ID, "--rule", "R1", "--kind", "bug", "--by", "Jordan Example", "--review-by", "2020-01-01", "--text", "Synthetic.")
	var con ops.ConstraintAddOutput
	e.json(&con, "constraint", "add", "--says", "No prod access", "--why", "Audit scope", "--by", "Jordan Example",
		"--source", "https://wiki.acme.example/access", "--applies-in", "prod", "--not-in", "dev", "--review-by", "2027-01-01")
	var conv ops.ConventionAddOutput
	e.json(&conv, "convention", "add", "Branch names", "--applies-in", "acme")

	cases := map[string]parityCase{
		"annotate":           {`{"area":"area-u-seed","kind":"note","text":"Ask first."}`, []string{"annotate", "area-u-seed", "Ask", "first.", "--kind", "note"}, false},
		"annotation_list":    {`{"kind":"exception"}`, []string{"annotation", "list", "--kind", "exception"}, true},
		"ingest":             {`{"url":"` + govURL + `","text":"Body."}`, []string{"ingest", "--url", govURL}, false},
		"policy_list":        {`{}`, []string{"policy", "list"}, true},
		"policy_show":        {`{"id":"` + pol.ID + `"}`, []string{"policy", "show", pol.ID}, true},
		"policy_review":      {`{"id":"` + pol.ID + `","by":"Jordan Example"}`, []string{"policy", "review", pol.ID, "--by", "Jordan Example"}, false},
		"policy_refresh":     {`{"id":"` + pol.ID + `","text":"Body."}`, []string{"policy", "refresh", pol.ID}, false},
		"rule_add":           {`{"policy":"` + pol.ID + `","text":"Second rule."}`, []string{"rule", "add", pol.ID, "Second rule."}, false},
		"rule_list":          {`{}`, []string{"rule", "list"}, true},
		"exception_add":      {`{"policy":"` + pol.ID + `","kind":"bug","by":"Jordan Example"}`, []string{"exception", "add", "--policy", pol.ID, "--kind", "bug", "--by", "Jordan Example"}, false},
		"exception_list":     {`{}`, []string{"exception", "list"}, true},
		"exception_show":     {`{"id":"` + exc.ID + `"}`, []string{"exception", "show", exc.ID}, true},
		"exception_expiring": {`{}`, []string{"exception", "expiring"}, true},
		"constraint_add": {`{"says":"No x","why":"y","by":"Jordan Example","source":"https://wiki.acme.example/x","review_by":"2027-01-01"}`,
			[]string{"constraint", "add", "--says", "No x", "--why", "y", "--by", "Jordan Example", "--source", "https://wiki.acme.example/x", "--review-by", "2027-01-01"}, false},
		"constraint_list": {`{}`, []string{"constraint", "list"}, true},
		"constraint_show": {`{"id":"` + con.ID + `"}`, []string{"constraint", "show", con.ID}, true},
		"convention_add":  {`{"title":"Commit style"}`, []string{"convention", "add", "Commit style"}, false},
		"convention_list": {`{}`, []string{"convention", "list"}, true},
		"convention_show": {`{"id":"` + conv.ID + `"}`, []string{"convention", "show", conv.ID}, true},
		"copy":            {`{"id":"` + pol.ID + `","as":"jira"}`, []string{"copy", pol.ID, "--as", "jira"}, false},
	}
	return cases, map[string]string{"ingest": "Body.", "policy_refresh": "Body."}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestIngestRefreshFlow(t *testing.T) {
	e := newEnv(t)
	vault := e.initAt("acme", 8)
	pol := e.ingest(govText)
	if !strings.HasPrefix(pol.ID, "policy-") || pol.TranscriptID != pol.ID+".source" || pol.Verified || pol.Fidelity != "exact" {
		t.Fatalf("ingest = %+v", pol)
	}
	summaryPath := filepath.Join(vault, "policies", pol.ID+".md")
	transcript := readFile(t, filepath.Join(vault, "policies", pol.TranscriptID+".md"))
	for _, want := range []string{"type: transcript", "Source: " + govURL, "hash: " + pol.Hash, "status: current", "## Page 1", "## Logging policy"} {
		if !strings.Contains(transcript, want) {
			t.Errorf("transcript lacks %q:\n%s", want, transcript)
		}
	}
	if strings.Contains(transcript, "\r") || strings.Contains(transcript, "  \n") {
		t.Errorf("transcript is not normalized:\n%q", transcript)
	}
	summary := readFile(t, summaryPath)
	if !strings.Contains(summary, "verified: false") || !strings.Contains(summary, "Summary not written yet") {
		t.Errorf("summary:\n%s", summary)
	}

	var rule ops.RuleAddOutput
	e.json(&rule, "rule", "add", pol.ID, "Services", "log", "every", "failure.")
	if rule.ID != "R1" || string(rule.Link) != "[["+pol.ID+"#^R1]]" {
		t.Fatalf("rule = %+v", rule)
	}
	e.json(&rule, "rule", "add", pol.ID, "Second.")
	if rule.ID != "R2" {
		t.Fatalf("second rule = %+v", rule)
	}
	var exc ops.ExceptionAddOutput
	e.json(&exc, "exception", "add", "--policy", pol.ID, "--rule", "R2", "--kind", "unenforced", "--by", "Jordan Example")

	var lint ops.LintOutput
	e.json(&lint, "lint")
	if lint.Errors != 0 || lint.Warnings != 0 {
		t.Fatalf("lint = %+v", lint)
	}
	before := readFile(t, summaryPath)

	var show ops.PolicyShowOutput
	e.json(&show, "policy", "show", pol.ID)
	if show.SourceStatus != "current" || show.Verified || len(show.RuleList) != 2 || len(show.Exceptions) != 1 {
		t.Fatalf("show = %+v", show)
	}

	cases := []struct {
		name, text, backend, result string
		status                      string
		affected                    bool
	}{
		{"same text, other line endings", strings.ReplaceAll(govText, "\r\n", "\n"), "", ops.RefreshUnchanged, "current", false},
		{"changed text", govText + "\nA new clause.\n", "", ops.RefreshChanged, "changed", true},
		{"same changed text again", govText + "\nA new clause.\n", "", ops.RefreshUnchanged, "changed", false},
		{"other backend is a baseline", "Different words from another backend.", "cli", ops.RefreshBaseline, "changed", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			args := []string{"policy", "refresh", pol.ID}
			if c.backend != "" {
				args = append(args, "--backend", c.backend)
			}
			var out ops.PolicyRefreshOutput
			e.stdinJSON(&out, c.text, args...)
			if out.Result != c.result || out.Checked == "" {
				t.Fatalf("out = %+v", out)
			}
			if got := len(out.AffectedRules) == 2 && len(out.AffectedExceptions) == 1; got != c.affected {
				t.Fatalf("affected = %+v / %+v", out.AffectedRules, out.AffectedExceptions)
			}
			e.json(&show, "policy", "show", pol.ID)
			if show.SourceStatus != c.status || show.SourceChecked != out.Checked {
				t.Fatalf("show status %q checked %q, want %q %q", show.SourceStatus, show.SourceChecked, c.status, out.Checked)
			}
			if after := readFile(t, summaryPath); after != before {
				t.Fatalf("refresh edited the summary:\n%s", after)
			}
		})
	}
	if got := readFile(t, filepath.Join(vault, "policies", pol.TranscriptID+".md")); !strings.Contains(got, "Different words") || !strings.Contains(got, "backend: cli") {
		t.Errorf("transcript not replaced:\n%s", got)
	}

	var rv ops.PolicyReviewOutput
	e.json(&rv, "policy", "review", pol.ID, "--by", "Jordan Example")
	e.json(&show, "policy", "show", pol.ID)
	if !rv.Verified || !show.Verified || show.ReviewedBy != "Jordan Example" || show.SourceStatus != "current" {
		t.Fatalf("review = %+v show = %+v", rv, show)
	}
	e.json(&lint, "lint")
	if lint.Errors != 0 {
		t.Fatalf("lint = %+v", lint)
	}
}

func TestIngestFailures(t *testing.T) {
	cases := []struct {
		name  string
		args  []string
		stdin string
		want  string
	}{
		{"signed query", []string{"--url", "https://wiki.acme.example/p?X-Amz-Signature=abc"}, "Body.", "signed or credential"},
		{"token query", []string{"--url", "https://wiki.acme.example/p?token=abc"}, "Body.", "signed or credential"},
		{"credentials in url", []string{"--url", "https://u:p@wiki.acme.example/p"}, "Body.", "credentials"},
		{"not http", []string{"--url", "ftp://wiki.acme.example/p"}, "Body.", "http(s)"},
		{"empty text", []string{"--url", govURL}, " \n", "empty"},
		{"raw html in text", []string{"--url", govURL}, "see <b>this</b>", "raw-html"},
		{"signed link in text", []string{"--url", govURL}, "see https://x.example/a?sig=1", "url"},
		{"bad id", []string{"--url", govURL, "--id", "Bad_Id"}, "Body.", "not a valid policy id"},
		{"source id", []string{"--url", govURL, "--id", "policy-x.source"}, "Body.", "not a valid policy id"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			vault := e.init("acme")
			e.stdin = c.stdin
			_, _, err := e.run(append([]string{"ingest"}, c.args...)...)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v", err)
			}
			if entries, _ := os.ReadDir(filepath.Join(vault, "policies")); len(entries) != 0 {
				t.Fatalf("wrote %v", entries)
			}
		})
	}
}

func TestIngestNoCopy(t *testing.T) {
	e := newEnv(t)
	vault := e.initAt("acme", 8)
	e.stdin = ""
	var out ops.IngestOutput
	e.json(&out, "ingest", "--url", govURL, "--no-copy", "--title", "Restricted policy")
	if out.Fidelity != "none" || out.TranscriptID != "" || out.Hash != "" {
		t.Fatalf("out = %+v", out)
	}
	entries, _ := os.ReadDir(filepath.Join(vault, "policies"))
	if len(entries) != 1 {
		t.Fatalf("files = %v", entries)
	}
	if s := readFile(t, filepath.Join(vault, "policies", out.ID+".md")); !strings.Contains(s, "fidelity: none") || strings.Contains(s, "hash:") {
		t.Fatalf("summary:\n%s", s)
	}
	var lint ops.LintOutput
	e.json(&lint, "lint")
	if lint.Errors != 0 {
		t.Fatalf("lint = %+v", lint)
	}
	var list ops.PolicyListOutput
	e.json(&list, "policy", "list")
	if len(list.Policies) != 1 || list.Policies[0].SourceStatus != "none" || list.Policies[0].Verified {
		t.Fatalf("list = %+v", list)
	}
	if _, _, err := e.run("policy", "refresh", out.ID); err == nil || !strings.Contains(err.Error(), "fidelity none") {
		t.Fatalf("refresh err = %v", err)
	}
	e.stdin = "text"
	if _, _, err := e.run("ingest", "--url", govURL, "--no-copy"); err != nil {
		t.Fatalf("no-copy must ignore stdin: %v", err)
	}
}

func TestRuleAndExceptionFailures(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	pol := e.ingest("Body.")
	var inbox ops.IOAddOutput
	e.json(&inbox, "inbox", "add", "synthetic")
	long := strings.Repeat("x", 600)
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"rule on missing policy", []string{"rule", "add", "policy-nope", "t"}, "not found"},
		{"rule on non-policy", []string{"rule", "add", inbox.ID, "t"}, "not a policy"},
		{"rule too long", []string{"rule", "add", pol.ID, long}, "limit is 500"},
		{"exception bad kind", []string{"exception", "add", "--policy", pol.ID, "--kind", "nope", "--by", "J"}, "must be one of"},
		{"exception no by", []string{"exception", "add", "--policy", pol.ID, "--kind", "bug"}, "by is required"},
		{"exception unknown rule", []string{"exception", "add", "--policy", pol.ID, "--kind", "bug", "--by", "J", "--rule", "R9"}, "no rule"},
		{"conflict needs with", []string{"exception", "add", "--policy", pol.ID, "--kind", "conflict_with", "--by", "J"}, "needs with"},
		{"with must exist", []string{"exception", "add", "--policy", pol.ID, "--kind", "conflict_with", "--by", "J", "--with", "policy-nope"}, "not found"},
		{"bad review date", []string{"exception", "add", "--policy", pol.ID, "--kind", "bug", "--by", "J", "--review-by", "2026-13-40"}, "not a date"},
		{"constraint signed source", []string{"constraint", "add", "--says", "s", "--why", "w", "--by", "b", "--source", "https://x.example/a?token=1", "--review-by", "2027-01-01"}, "source"},
		{"constraint no review", []string{"constraint", "add", "--says", "s", "--why", "w", "--by", "b", "--source", "https://x.example/a"}, "review_by"},
		{"convention bad status", []string{"convention", "add", "T", "--status", "nope"}, "must be one of"},
		{"show wrong type", []string{"constraint", "show", pol.ID}, "not a constraint"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, _, err := e.run(c.args...); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v", err)
			}
		})
	}
	var rules ops.RuleListOutput
	e.json(&rules, "rule", "list")
	if len(rules.Rules) != 0 {
		t.Fatalf("failed adds left rules: %+v", rules)
	}
}

func TestExceptionKindsAndExpiring(t *testing.T) {
	e := newEnv(t)
	e.initAt("acme", 8)
	pol := e.ingest("Body.")
	other := e.ingest("Other.")
	e.json(&ops.RuleAddOutput{}, "rule", "add", pol.ID, "A rule.")
	kinds := []string{"underspecified", "overly_broad", "bug", "unenforced", "conflict_with", "interpreted_as"}
	for _, k := range kinds {
		args := []string{"exception", "add", "--policy", pol.ID, "--kind", k, "--by", "Jordan Example", "--rule", "R1"}
		if k == "conflict_with" {
			args = append(args, "--with", other.ID)
		}
		e.json(&ops.ExceptionAddOutput{}, args...)
	}
	var list ops.ExceptionListOutput
	e.json(&list, "exception", "list", "--policy", pol.ID)
	if len(list.Exceptions) != len(kinds) {
		t.Fatalf("list = %+v", list)
	}
	e.json(&list, "exception", "list", "--kind", "conflict_with")
	if len(list.Exceptions) != 1 || list.Exceptions[0].With != other.ID || list.Exceptions[0].Rule != "R1" || list.Exceptions[0].Status != "open" {
		t.Fatalf("conflict = %+v", list)
	}

	var late, soon, far ops.ExceptionAddOutput
	e.json(&late, "exception", "add", "--policy", pol.ID, "--kind", "bug", "--by", "J", "--review-by", "2020-01-01")
	e.json(&soon, "exception", "add", "--policy", pol.ID, "--kind", "bug", "--by", "J", "--review-by", "2020-06-01")
	e.json(&far, "exception", "add", "--policy", pol.ID, "--kind", "bug", "--by", "J", "--review-by", "2999-01-01")
	var exp ops.ExceptionExpiringOutput
	e.json(&exp, "exception", "expiring", "--days", "30")
	if len(exp.Exceptions) != 2 || exp.Exceptions[0].ID != late.ID || exp.Exceptions[1].ID != soon.ID || exp.Exceptions[0].DaysLeft >= 0 {
		t.Fatalf("expiring = %+v", exp)
	}
	var lint ops.LintOutput
	e.json(&lint, "lint")
	if lint.Errors != 0 {
		t.Fatalf("lint = %+v", lint)
	}
}

func TestConstraintAndConvention(t *testing.T) {
	e := newEnv(t)
	vault := e.initAt("acme", 8)
	var con ops.ConstraintAddOutput
	e.json(&con, "constraint", "add", "--says", "No prod access", "--why", "Audit scope", "--by", "Jordan Example",
		"--source", "https://wiki.acme.example/access", "--applies-in", "prod,staging", "--applies-in", "dr", "--review-by", "2027-01-01", "--text", "Details.")
	var show ops.ConstraintShowOutput
	e.json(&show, "constraint", "show", con.ID)
	if show.Says != "No prod access" || show.Why != "Audit scope" || show.By != "Jordan Example" || show.SourceURL != "https://wiki.acme.example/access" ||
		strings.Join(show.AppliesIn, ",") != "prod,staging,dr" || len(show.NotIn) != 0 || show.ReviewBy != "2027-01-01" || show.Body != "Details." {
		t.Fatalf("show = %+v", show)
	}
	if _, err := os.Stat(filepath.Join(vault, "constraints", con.ID+".md")); err != nil {
		t.Fatal(err)
	}
	var conv ops.ConventionAddOutput
	e.json(&conv, "convention", "add", "Branch", "names", "--status", "adopted", "--applies-in", "acme", "--text", "Use kebab-case.")
	var cs ops.ConventionShowOutput
	e.json(&cs, "convention", "show", conv.ID)
	if cs.Title != "Branch names" || cs.Status != "adopted" || cs.Body != "Use kebab-case." {
		t.Fatalf("convention = %+v", cs)
	}
	var list ops.ConventionListOutput
	e.json(&list, "convention", "list")
	var clist ops.ConstraintListOutput
	e.json(&clist, "constraint", "list")
	if len(list.Conventions) != 1 || len(clist.Constraints) != 1 {
		t.Fatalf("lists %+v %+v", list, clist)
	}
	var lint ops.LintOutput
	e.json(&lint, "lint")
	if lint.Errors != 0 {
		t.Fatalf("lint = %+v", lint)
	}
}

func TestCopy(t *testing.T) {
	e := newEnv(t)
	vault := e.initAt("acme", 8)
	pol := e.ingest("# Logging policy\n\nServices **must** log failures. See [the wiki](https://wiki.acme.example/x).\n\n| Level | Use |\n|---|---|\n| info | normal |\n")
	e.json(&ops.RuleAddOutput{}, "rule", "add", pol.ID, "Log every failure.")
	// Marking comes from vault.properties.
	propsPath := filepath.Join(vault, ".para", "vault.properties")
	raw := readFile(t, propsPath)
	if !strings.Contains(raw, "egress.default=deny") {
		t.Fatalf("props = %q", raw)
	}
	if err := os.WriteFile(propsPath, []byte(raw+"classification=ACME INTERNAL\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		as    string
		wants []string
		nots  []string
	}{
		{"plain", []string{"[ACME INTERNAL]", "Summary not written yet", "R1 " + pol.ID + ": Log every failure.", "Source: " + govURL}, []string{"type: policy", "---", "**", "^R1"}},
		{"email", []string{"Subject: [ACME INTERNAL] ", "[ACME INTERNAL]"}, []string{"type: policy", "---"}},
		{"slack", []string{"*[ACME INTERNAL]*", "*Rules*", "• R1 "}, []string{"type: policy", "---"}},
		{"jira", []string{"*[ACME INTERNAL]*", "h1. ", "h2. Rules", "* R1 "}, []string{"type: policy", "---"}},
	}
	for _, c := range cases {
		t.Run(c.as, func(t *testing.T) {
			out := e.ok("copy", pol.ID, "--as", c.as)
			for _, w := range c.wants {
				if !strings.Contains(out, w) {
					t.Errorf("lacks %q:\n%s", w, out)
				}
			}
			for _, n := range c.nots {
				if strings.Contains(out, n) {
					t.Errorf("has %q:\n%s", n, out)
				}
			}
			if !strings.HasSuffix(out, "[ACME INTERNAL]\n") && !strings.HasSuffix(out, "*[ACME INTERNAL]*\n") {
				t.Errorf("no closing marking:\n%s", out)
			}
		})
	}
	if _, _, err := e.run("copy", pol.ID, "--as", "fax"); err == nil || !strings.Contains(err.Error(), "must be one of") {
		t.Fatalf("bad format err = %v", err)
	}
	if _, _, err := e.run("copy", pol.ID, "--clipboard", "--json"); err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("clipboard+json err = %v", err)
	}

	var tail ops.EventsTailOutput
	e.json(&tail, "events", "tail", "-n", "100")
	var dests []string
	for _, ev := range tail.Events {
		if ev.Op == "egress" {
			if ev.Doc != pol.ID || ev.From != "deny" {
				t.Errorf("egress event = %+v", ev)
			}
			dests = append(dests, ev.To)
		}
	}
	if got := strings.Join(dests, " "); got != "copy:plain copy:email copy:slack copy:jira" {
		t.Errorf("egress destinations = %q (a rejected copy must not log)", got)
	}
	var cp ops.CopyOutput
	e.json(&cp, "copy", pol.ID)
	if !cp.EgressAllowed || cp.EgressDefault != "deny" || cp.Marking != "ACME INTERNAL" {
		t.Fatalf("copy = %+v", cp)
	}
	evRaw := e.ok("events", "tail", "-n", "100", "--json")
	if strings.Contains(evRaw, "ACME INTERNAL") || strings.Contains(evRaw, "Log every failure") {
		t.Fatalf("content in events:\n%s", evRaw)
	}
}

func TestCopyUnmarked(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	pol := e.ingest("Body.")
	if out := e.ok("copy", pol.ID); !strings.HasPrefix(out, "[UNMARKED]\n") {
		t.Fatalf("out = %q", out)
	}
}

func TestClipboardCopy(t *testing.T) {
	missing := func(string) (string, error) { return "", errors.New("not found") }
	only := func(name string) func(string) (string, error) {
		return func(n string) (string, error) {
			if n == name {
				return "/bin/" + n, nil
			}
			return "", errors.New("not found")
		}
	}
	cases := []struct {
		name     string
		look     func(string) (string, error)
		runErr   error
		wantTool string
		wantArgv string
		wantErr  string
	}{
		{"pbcopy", only("pbcopy"), nil, "pbcopy", "pbcopy", ""},
		{"wl-copy", only("wl-copy"), nil, "wl-copy", "wl-copy", ""},
		{"xclip", only("xclip"), nil, "xclip", "xclip -selection clipboard", ""},
		{"none", missing, nil, "", "", "none of"},
		{"tool fails", only("pbcopy"), errors.New("boom"), "", "", "boom"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var argv, stdin string
			tool, err := export.ClipboardCopy("hello", c.look, func(a []string, in string) error {
				argv, stdin = strings.Join(a, " "), in
				return c.runErr
			})
			if c.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), c.wantErr) {
					t.Fatalf("err = %v", err)
				}
				return
			}
			if err != nil || tool != c.wantTool || argv != c.wantArgv || stdin != "hello" {
				t.Fatalf("tool %q argv %q stdin %q err %v", tool, argv, stdin, err)
			}
		})
	}
}

func TestGovernanceStdoutIsOnlyJSON(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	pol := e.ingest("Body.")
	cases := [][]string{
		{"policy", "list", "--json"},
		{"policy", "show", pol.ID, "--json"},
		{"rule", "list", "--json"},
		{"exception", "expiring", "--json"},
		{"constraint", "list", "--json"},
		{"copy", pol.ID, "--json"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			out, errOut, err := e.run(args...)
			if err != nil || !json.Valid([]byte(out)) || errOut != "" {
				t.Fatalf("err %v stderr %q stdout %q", err, errOut, out)
			}
		})
	}
}
