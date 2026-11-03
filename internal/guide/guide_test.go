package guide

import (
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/iodesystems/parasolve/internal/ops"
)

func texts() map[string]string {
	return map[string]string{"intro": Intro(), "rule": Rule(), "orient": Orient(), "bootstrap": Bootstrap(), "extended": Extended(), "review_cycle": ReviewCycle(), "process_meeting": ProcessMeeting(), "journal_lod": JournalLOD(), "raglit": Raglit(), "import_corpus": ImportCorpus(), "health_triage": HealthTriage(), "ingest_source": IngestSource(), "align_vault": AlignVault(), "sort_update": SortUpdate(), "validate_goal": ValidateGoal()}
}

var backticked = regexp.MustCompile("`([^`\n]+)`")

// Every backticked word in the guide is an op name, so a renamed or removed
// tool breaks this test. Prose that is not a tool name goes without backticks.
func TestBackticksAreTools(t *testing.T) {
	names := map[string]bool{}
	for _, e := range ops.All() {
		names[e.Name()] = true
	}
	used := map[string]bool{}
	for name, text := range texts() {
		for _, m := range backticked.FindAllStringSubmatch(text, -1) {
			if !names[m[1]] {
				t.Errorf("%s: `%s` is not in ops.All", name, m[1])
			}
			used[m[1]] = true
		}
	}
	// The tools the owner's workflow depends on must be named somewhere.
	for _, want := range []string{"area_add", "area_link_add", "project_add", "project_link_add", "task_add", "task_due",
		"task_depends_add", "org_person_add", "inbox_add", "inbox_triage", "rfx_add", "rfx_reply", "lint", "next", "where"} {
		if !used[want] {
			t.Errorf("no guide text names %s", want)
		}
	}
}

func TestIntroIsShort(t *testing.T) {
	if n := len(strings.Split(Intro(), "\n")); n >= 12 {
		t.Errorf("intro is %d lines, want under 12", n)
	}
	for _, want := range []string{"what you owe and who you wait on", "is not a documentation system", "pager", "scheduler", "calendar", "sync tool", "para links to them"} {
		if !strings.Contains(strings.Join(strings.Fields(Intro()), " "), want) {
			t.Errorf("intro lacks %q", want)
		}
	}
}

func TestRuleReachesEverySurface(t *testing.T) {
	for _, name := range []string{"orient", "bootstrap"} {
		if !strings.Contains(texts()[name], Rule()) {
			t.Errorf("%s lacks the rule", name)
		}
	}
	for _, want := range []string{"one question at a time", "Never invent a fact", "area_add", "lint"} {
		if !strings.Contains(strings.Join(strings.Fields(Bootstrap()), " "), want) {
			t.Errorf("bootstrap lacks %q", want)
		}
	}
	// The bootstrap order: areas, projects, tasks, people, then lint.
	order := []string{"area_add", "project_add", "task_add", "org_person_add", "lint"}
	at := -1
	for _, tool := range order {
		i := strings.Index(Bootstrap(), tool)
		if i < at {
			t.Errorf("bootstrap names %s out of order", tool)
		}
		at = i
	}
}

func TestSkillFrontmatterParses(t *testing.T) {
	parts := strings.SplitN(Skill(), "---\n", 3)
	if len(parts) != 3 {
		t.Fatalf("no frontmatter: %q", Skill()[:40])
	}
	var fm map[string]string
	if err := yaml.Unmarshal([]byte(parts[1]), &fm); err != nil {
		t.Fatal(err)
	}
	if fm["name"] != "para" || fm["description"] == "" {
		t.Fatalf("frontmatter = %v", fm)
	}
}

func TestRaglitHelpCoversWhatItMust(t *testing.T) {
	text := strings.Join(strings.Fields(Raglit()), " ")
	for _, want := range []string{"para raglit connect", "embedding model", "--embed", "One daemon per trust domain",
		"local or company-approved models", "polls the vault every 5 seconds", "exact only", "vault close --destroy", "untrusted"} {
		if !strings.Contains(text, want) {
			t.Errorf("raglit help lacks %q", want)
		}
	}
}

func flat(s string) string { return strings.Join(strings.Fields(s), " ") }

// Dispatch is the one place para starts a process. The setup texts say so, show
// the file, give the claude -p and dun -p examples marked untested, and say it
// spends tokens.
func TestHarnessTextsDescribeDispatch(t *testing.T) {
	for client, example := range map[string]string{"claude": `"claude", "-p"`, "dun": `"dun", "-p"`} {
		text := flat(Harness(client, ""))
		for _, want := range []string{"agent.json", "THIS SPENDS TOKENS", "never in the vault", "argument list, not a shell string",
			"PARA_REQUEST", "daily_limit", "max_concurrent", "untested", "request_next", example} {
			if !strings.Contains(text, want) {
				t.Errorf("%s setup lacks %q", client, want)
			}
		}
	}
}

func TestExtendedHasTheStreamVAndXUseCases(t *testing.T) {
	text := flat(Extended())
	for _, want := range []string{"para meeting add", "para meeting hold", "para agenda", "para calendar export", "para tick",
		"request.runner=<install id>", "tick.interval", "request.surfaces", "para agent status", "dispatch spends tokens",
		"agent.json", "Para does not record or transcribe", "never writes an external calendar", "process_meeting"} {
		if !strings.Contains(text, want) {
			t.Errorf("extended lacks %q", want)
		}
	}
	if strings.Contains(text, "Para has no scheduler") {
		t.Error("extended still says para has no scheduler")
	}
}

func TestOrientNamesTheMeetingOpsAndDispatch(t *testing.T) {
	text := flat(Orient())
	for _, want := range []string{"meeting_add", "meeting_hold", "meeting_schedule", "meeting_cancel", "agenda", "PARA_REQUEST", "request_next", "spends the owner's tokens"} {
		if !strings.Contains(text, want) {
			t.Errorf("orient lacks %q", want)
		}
	}
}

func TestImportCorpusPromptStatesTheRules(t *testing.T) {
	text := flat(ImportCorpus())
	for _, want := range []string{"no import command", "proposal_add", "Search before you create", "link", "Never read credentials", "No external action", "Write no assessment of a person", "done_when", "One item per change",
		"request_add", "Cite the source", "untrusted data"} {
		if !strings.Contains(strings.ToLower(text), strings.ToLower(want)) {
			t.Errorf("import_corpus lacks %q", want)
		}
	}
	if !strings.Contains(flat(Orient()), "import_corpus") {
		t.Error("orient does not name the prompt import_corpus")
	}
	if !strings.Contains(flat(Extended()), "import_corpus") {
		t.Error("extended lacks the import use case")
	}
}

func TestExtendedAndOrientNameInvestigate(t *testing.T) {
	text := flat(Extended())
	for _, want := range []string{"para investigate", "Investigate: <title>", "Ask: <title>", "Find evidence", "Agent looking", "Agent replied", "a guess is not a source", "adds the text"} {
		if !strings.Contains(text, want) {
			t.Errorf("extended lacks %q", want)
		}
	}
	if !strings.Contains(Orient(), "`investigate`") {
		t.Error("orient does not name investigate")
	}
}

func TestGuideNamesResolveAndDelegate(t *testing.T) {
	for name, want := range map[string][]string{
		"orient":   {"task_resolve", "partial", "superseded", "file a proposal", "task_delegate", "Work on:", "without an approved proposal"},
		"extended": {"para task resolve", "para task delegate", "--follow-up", "## Outcome", "Agent working", "request.runner"},
	} {
		text := flat(texts()[name])
		for _, w := range want {
			if !strings.Contains(text, w) {
				t.Errorf("%s lacks %q", name, w)
			}
		}
	}
}

func TestGuideNamesNextMoves(t *testing.T) {
	text := flat(texts()["extended"])
	for _, w := range []string{"para rfx paths", "para rfx path add", "away_until", "quiet_days", "wrong_owner", "--override", "rfx_path_add"} {
		if !strings.Contains(text, w) {
			t.Errorf("extended lacks %q", w)
		}
	}
}

func TestGuideNamesFrictionReport(t *testing.T) {
	text := flat(texts()["extended"])
	for _, w := range []string{"para report friction", "no per-person view", "evidence", "egress"} {
		if !strings.Contains(text, w) {
			t.Errorf("extended lacks %q", w)
		}
	}
}

func TestGuideNamesUpgrades(t *testing.T) {
	text := flat(texts()["extended"])
	for _, w := range []string{"para upgrade", "git pull --ff-only", "para changes", "para vault upgrade --dry-run", "format_version", "--undo", "judgement step"} {
		if !strings.Contains(text, w) {
			t.Errorf("extended lacks %q", w)
		}
	}
	if o := flat(Orient()); !strings.Contains(o, "`changes`") || !strings.Contains(o, "Vault upgrade:") {
		t.Error("orient lacks changes or the vault upgrade work")
	}
}
