package guide_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/guide"
	"github.com/iodesystems/para/internal/ops"
)

// An external test package: ops imports guide for the guide op.
func guideTexts() map[string]string {
	return map[string]string{"intro": guide.Intro(), "rule": guide.Rule(), "orient": guide.Orient(), "bootstrap": guide.Bootstrap(), "extended": guide.Extended(), "raglit": guide.Raglit(), "import_corpus": guide.ImportCorpus(), "ingest_source": guide.IngestSource(), "align_vault": guide.AlignVault(), "grill_goal": guide.GrillGoal(), "sweep": guide.Sweep(), "loop": guide.Loop(), "io": guide.IO(), "skill": guide.Skill()}
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
	for name, text := range guideTexts() {
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
