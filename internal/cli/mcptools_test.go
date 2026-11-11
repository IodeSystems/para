package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/iodesystems/para/internal/mcpserver"
	"github.com/iodesystems/para/internal/ops"
	"github.com/iodesystems/para/internal/registry"
)

// The tool listing: --tools, else mcp.tools in install.properties, else core.
func TestMCPToolsSetting(t *testing.T) {
	home := t.TempDir()
	l := &loader{install: registry.Install{Home: home}}
	if v, err := l.mcpTools(""); err != nil || v != mcpserver.ToolsCore {
		t.Fatalf("no setting = %q %v", v, err)
	}
	if err := os.WriteFile(filepath.Join(home, "install.properties"), []byte("mcp.tools=all\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if v, err := l.mcpTools(""); err != nil || v != mcpserver.ToolsAll {
		t.Fatalf("mcp.tools=all = %q %v", v, err)
	}
	if v, err := l.mcpTools("core"); err != nil || v != mcpserver.ToolsCore {
		t.Fatalf("--tools core = %q %v", v, err)
	}
	if _, err := l.mcpTools("some"); err == nil {
		t.Fatal("--tools some accepted")
	}
}

// para changes reads the notes embedded in the binary, so it works with no
// vault registered (the print after para upgrade on a new machine).
func TestChangesWithNoVault(t *testing.T) {
	e := newEnv(t)
	out, _, err := e.run("changes", "--since", "0.1.0")
	if err != nil || strings.TrimSpace(out) == "" {
		t.Fatalf("changes with no vault: %v\n%s", err, out)
	}
	var got ops.ChangesOutput
	e.json(&got, "changes")
	if got.Current == "" {
		t.Fatalf("changes --json = %+v", got)
	}
}

// para mcp with no setting lists the core tools: exactly mcpserver.Core.
func TestMCPDefaultListsCore(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	c := startMCP(t, e, "--tools", "")
	tools, err := c.ListTools(t.Context(), mcp.ListToolsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tl := range tools.Tools {
		got = append(got, tl.Name)
	}
	slices.Sort(got)
	want := slices.Sorted(slices.Values(mcpserver.Core))
	if !slices.Equal(got, want) {
		t.Errorf("default tools = %v, want %v", got, want)
	}
}
