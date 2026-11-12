package guide

import (
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/iodesystems/para/internal/ops"
)

func texts() map[string]string {
	return map[string]string{"intro": Intro(), "rule": Rule(), "orient": Orient(), "bootstrap": Bootstrap(), "extended": Extended(), "raglit": Raglit(), "import_corpus": ImportCorpus(), "ingest_source": IngestSource(), "align_vault": AlignVault(), "grill_goal": GrillGoal(), "sweep": Sweep(), "loop": Loop(), "io": IO()}
}

var backticked = regexp.MustCompile("`([^`\n]+)`")

// Every backticked word in the guide is an op name, so a renamed or removed
// tool breaks this test. Prose that is not a tool name goes without backticks.
// A tool may carry its arguments (`add {type: task}`); op_run and
// ops_describe, the MCP tools that reach every op, name an op that must exist
// (`op_run {op: sweep_record}`).
func TestBackticksAreTools(t *testing.T) {
	names := map[string]bool{}
	for _, e := range ops.All() {
		names[e.Name()] = true
	}
	used := map[string]bool{}
	for name, text := range texts() {
		for _, m := range backticked.FindAllStringSubmatch(text, -1) {
			if err := toolRef(names, m[1]); err != "" {
				t.Errorf("%s: `%s`: %s", name, m[1], err)
			}
			used[m[1]] = true
		}
	}
	// The tools the owner's workflow depends on must be named somewhere.
	for _, want := range []string{"add {type: area}", "link {rel: link}", "add {type: project}", "add {type: task}", "op_run {op: task_due}",
		"link {rel: depends_on}", "add {type: person}", "add {type: io}", "op_run {op: io_file}", "close {outcome: sent}", "add {type: ask}",
		"close {outcome: answered}", "op_run {op: lint}", "list {needs_you: true}", "op_run {op: where}"} {
		if !used[want] {
			t.Errorf("no guide text names %s", want)
		}
	}
}

var opArg = regexp.MustCompile(`^\{op: ([a-z_]+)[,}]`)

// toolRef says what is wrong with a backticked tool reference, or "".
func toolRef(names map[string]bool, ref string) string {
	tool, args, _ := strings.Cut(ref, " ")
	switch tool {
	case "op_run", "ops_describe":
		if args == "" {
			return ""
		}
		m := opArg.FindStringSubmatch(args)
		if m == nil {
			return "want " + tool + " {op: <name>}"
		}
		if !names[m[1]] {
			return m[1] + " is not in ops.All"
		}
		return ""
	}
	if !names[tool] {
		return tool + " is not in ops.All"
	}
	if args != "" && !strings.HasPrefix(args, "{") {
		return "arguments go in {...}"
	}
	return ""
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
	for _, want := range []string{"one question at a time", "Never invent a fact", "add {type: area}", "op_run {op: lint}"} {
		if !strings.Contains(strings.Join(strings.Fields(Bootstrap()), " "), want) {
			t.Errorf("bootstrap lacks %q", want)
		}
	}
	// The bootstrap order: areas, projects, goals, tasks, people, then lint.
	order := []string{"add {type: area}", "add {type: project}", "add {type: goal}", "add {type: task}", "add {type: person}", "op_run {op: lint}"}
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
			"PARA_REQUEST", "daily_limit", "max_concurrent", "untested", "work {action: next}", example} {
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
		"agent.json", "Para does not record or transcribe", "never writes an external calendar", "ingest_source"} {
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
	for _, want := range []string{"add {type: obligation}", "op_run {op: obligation_schedule}", "op_run {op: obligation_attend}", "op_run {op: agenda}", "PARA_REQUEST", "work {action: next}", "spends the owner's tokens"} {
		if !strings.Contains(text, want) {
			t.Errorf("orient lacks %q", want)
		}
	}
}

func TestImportCorpusPromptStatesTheRules(t *testing.T) {
	text := flat(ImportCorpus())
	for _, want := range []string{"no import command", "Search before you create", "link", "Never read credentials", "No external act", "Write no assessment of a person", "done_when", "grouped by type",
		"work {action: ask}", "Cite the source", "untrusted data", "what it is and why it exists", "no_context"} {
		if !strings.Contains(strings.ToLower(text), strings.ToLower(want)) {
			t.Errorf("import_corpus lacks %q", want)
		}
	}
	if !strings.Contains(flat(IngestSource()), "what it is and why it exists") {
		t.Error("ingest_source does not ask for context on what it creates")
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
	if !strings.Contains(Orient(), "agent work (Investigate)") {
		t.Error("orient does not name investigate")
	}
}

func TestGuideNamesResolveAndDelegate(t *testing.T) {
	for name, want := range map[string][]string{
		"orient":   {"`close`", "partial", "superseded", "hand a task to an agent", "Work on:", "without an outgoing draft the owner approved", "op_run {op: io_approve}", "close {outcome: sent}"},
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

func TestGuideNamesUpgrades(t *testing.T) {
	text := flat(texts()["extended"])
	for _, w := range []string{"para upgrade", "git pull --ff-only", "para changes", "para vault upgrade --dry-run", "format_version", "--undo", "judgement step"} {
		if !strings.Contains(text, w) {
			t.Errorf("extended lacks %q", w)
		}
	}
	if o := flat(Orient()); !strings.Contains(o, "`op_run {op: changes}`") || !strings.Contains(o, "Vault upgrade:") {
		t.Error("orient lacks changes or the vault upgrade work")
	}
}

// The delegate text states the outward rule, the record by the worker on
// behalf of the owner, the read-only worker, and the health check.
func TestDelegateStatesTheOutwardRule(t *testing.T) {
	text := Loop()
	for _, want := range []string{"outward actions (messages, comment replies, pushes, publishing", "come back to you", "`close {outcome: sent}`", "sent as the owner", "--read-only", "unreviewed_outward"} {
		if !strings.Contains(text, want) {
			t.Errorf("delegate text lacks %q", want)
		}
	}
	if !strings.Contains(Skill(), "`delegate <task>`, read `loop.md` and follow its part Delegate") {
		t.Error("the skill does not route delegate to loop.md")
	}
}

// ingest_source, part An update on an item, covers work from an item's composer: the owner's words, the
// attachments, notes first, and closing the item.
func TestSortUpdateCoversTheComposer(t *testing.T) {
	text := flat(IngestSource())
	for _, want := range []string{"From the owner: <title>", "## Owner's words", "## Attachments", "`op_run {op: attachment_read}`",
		"`show`, then `edit {notes, notes_hash}` with the hash", "![description](path)", "Mark done: yes", "`close`",
		"outcome answered for a question", "clearly say it is done", "say in your reply that you closed it"} {
		if !strings.Contains(text, want) {
			t.Errorf("ingest_source lacks %q", want)
		}
	}
	if !strings.Contains(flat(Orient()), "`op_run {op: attachment_read}`") {
		t.Error("orient does not name attachment_read")
	}
}
