package mcpserver

import (
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
