package mcpserver

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/iodesystems/para/internal/guide"
	"github.com/iodesystems/para/internal/ops"
)

func TestPrompts(t *testing.T) {
	s, _ := newVault(t)
	c := connect(t, ops.Deps{Store: s})
	list, err := c.ListPrompts(t.Context(), mcp.ListPromptsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, p := range list.Prompts {
		names[p.Name] = true
	}
	want := map[string]string{"orient": guide.Orient(), "bootstrap": guide.Bootstrap(), "import_corpus": guide.ImportCorpus(), "ingest_source": guide.IngestSource(), "align_vault": guide.AlignVault(), "grill_goal": guide.GrillGoal(), "sweep": guide.Sweep(), "loop": guide.Loop(), "io": guide.IO()}
	if len(names) != len(want) {
		t.Fatalf("prompts = %v", names)
	}
	for name, text := range want {
		if !names[name] {
			t.Errorf("no prompt %s", name)
			continue
		}
		res, err := c.GetPrompt(t.Context(), mcp.GetPromptRequest{Params: mcp.GetPromptParams{Name: name}})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Messages) != 1 {
			t.Fatalf("%s: messages = %d", name, len(res.Messages))
		}
		tc, ok := res.Messages[0].Content.(mcp.TextContent)
		if !ok || tc.Text != text || res.Messages[0].Role != mcp.RoleUser {
			t.Errorf("%s: message = %+v", name, res.Messages[0])
		}
		if !strings.Contains(tc.Text, guide.Rule()) {
			t.Errorf("%s lacks the rule", name)
		}
		if !strings.Contains(Instructions, name) {
			t.Errorf("instructions do not name the prompt %s", name)
		}
	}
	if !strings.Contains(Instructions, guide.Rule()) {
		t.Error("instructions lack the rule")
	}
	if _, err := c.GetPrompt(t.Context(), mcp.GetPromptRequest{Params: mcp.GetPromptParams{Name: "nope"}}); err == nil {
		t.Error("unknown prompt must fail")
	}
}

// The prompts and the skill name a tool an agent sees in a core listing, or
// reach the op with op_run: a backticked op off Core is `op_run {op: name}`,
// and that op is one an agent may run.
func TestGuidesNameCoreTools(t *testing.T) {
	agent := map[string]bool{requestWaitTool: true}
	for _, e := range ops.ForAgents() {
		agent[e.Name()] = true
	}
	known := map[string]bool{}
	for _, e := range ops.All() {
		known[e.Name()] = true
	}
	tick := regexp.MustCompile("`([^`\n]+)`")
	opArg := regexp.MustCompile(`^\{op: ([a-z_]+)[,}]`)
	texts := map[string]string{"orient": guide.Orient(), "bootstrap": guide.Bootstrap(), "import_corpus": guide.ImportCorpus(), "ingest_source": guide.IngestSource(),
		"align_vault": guide.AlignVault(), "grill_goal": guide.GrillGoal(), "sweep": guide.Sweep(), "loop": guide.Loop(), "io": guide.IO()}
	// The skill past its frontmatter and argument routing (/para health is an
	// argument, not a tool).
	_, body, ok := strings.Cut(guide.Skill(), "With no arguments, work as below.")
	if !ok {
		t.Fatal("the skill lost its routing end")
	}
	texts["skill"] = body
	for name, text := range texts {
		for _, m := range tick.FindAllStringSubmatch(text, -1) {
			tool, args, _ := strings.Cut(m[1], " ")
			switch {
			case tool == opRunTool || tool == opsDescribeTool:
				if a := opArg.FindStringSubmatch(args); a != nil && !agent[a[1]] {
					t.Errorf("%s: `%s`: %s is not an op an agent may run", name, m[1], a[1])
				}
			case known[tool] && !slices.Contains(Core, tool):
				t.Errorf("%s: `%s` is not a core tool: write `op_run {op: %s}`", name, m[1], tool)
			}
		}
	}
}
