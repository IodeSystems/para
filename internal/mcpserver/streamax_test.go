package mcpserver

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/iodesystems/para/changes"
	"github.com/iodesystems/para/internal/ops"
)

func initializeWith(t *testing.T, o Options) (*client.Client, string) {
	t.Helper()
	s, _ := newVault(t)
	srv, err := New(ops.Deps{Store: s}, o)
	if err != nil {
		t.Fatal(err)
	}
	c, err := client.NewInProcessClient(srv.MCP())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	if err := c.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	res, err := c.Initialize(t.Context(), mcp.InitializeRequest{})
	if err != nil {
		t.Fatal(err)
	}
	return c, res.Instructions
}

func TestInstructionsAskForChangesOnceWhenTheBinaryChanged(t *testing.T) {
	seen := filepath.Join(t.TempDir(), "state", "mcp_seen.json")
	_, first := initializeWith(t, Options{Version: "v1", SeenPath: seen})
	if strings.Contains(first, ChangedLine) {
		t.Fatal("the first session was told para changed")
	}
	_, same := initializeWith(t, Options{Version: "v1", SeenPath: seen})
	if strings.Contains(same, ChangedLine) {
		t.Fatal("the same binary was told para changed")
	}
	// The last session saw an older release, so changes has something to list.
	if err := os.WriteFile(seen, []byte(`{"key":"v1@0.0.0","release":"0.0.0"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, upgraded := initializeWith(t, Options{Version: "v2", SeenPath: seen})
	if !strings.Contains(upgraded, ChangedLine) {
		t.Fatal("a new binary was not announced")
	}
	_, after := initializeWith(t, Options{Version: "v2", SeenPath: seen})
	if strings.Contains(after, ChangedLine) {
		t.Fatal("the line repeated in the next session")
	}
	// changes answers with the releases after the one the last session saw.
	res := call(t, c, "changes", `{}`)
	if res.IsError || !strings.Contains(textOf(t, res), changes.Latest()) {
		t.Fatalf("changes = %v %s", res.IsError, textOf(t, res))
	}
}

func TestSeenRecordIsPerPathAndNeverInTheVault(t *testing.T) {
	_, root := newVault(t)
	seen := filepath.Join(t.TempDir(), "mcp_seen.json")
	initializeWith(t, Options{Version: "v1", SeenPath: seen})
	if _, err := os.Stat(seen); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "mcp_seen.json")); err == nil {
		t.Fatal("record in the vault")
	}
}

func TestStaleErrorNamesBothVersions(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "para")
	script := "#!/bin/sh\necho v2-new\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	c, _ := initializeWith(t, Options{Version: "v1-old", Executable: bin, InstalledVersion: func() string {
		out, _ := exec.Command(bin, "version").Output()
		return string(out)
	}})
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	tmp := bin + ".new"
	if err := os.WriteFile(tmp, []byte(script+"# changed\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, bin); err != nil {
		t.Fatal(err)
	}
	res := call(t, c, "lint", "")
	msg := textOf(t, res)
	for _, want := range []string{"v1-old", "v2-new", "/mcp", "changes"} {
		if !res.IsError || !strings.Contains(msg, want) {
			t.Fatalf("stale error lacks %q: %s", want, msg)
		}
	}
}

func TestOrientListsPendingUpgradeSteps(t *testing.T) {
	c, _ := initializeWith(t, Options{Version: "test"})
	res, err := c.GetPrompt(t.Context(), mcp.GetPromptRequest{Params: mcp.GetPromptParams{Name: "orient"}})
	if err != nil {
		t.Fatal(err)
	}
	text := res.Messages[0].Content.(mcp.TextContent).Text
	// newVault records no format_version and holds no document a step selects.
	if strings.Contains(text, "pending upgrade steps") {
		t.Fatalf("orient lists a step for an empty vault: %s", text)
	}
}

func TestChangedLineNeedsReleasesAfterTheLastSeen(t *testing.T) {
	seen := filepath.Join(t.TempDir(), "mcp_seen.json")
	// A new binary version with the same release notes: changes lists nothing, so no notice.
	rec := `{"key":"v1@` + changes.Latest() + `","release":"` + changes.Latest() + `"}`
	if err := os.WriteFile(seen, []byte(rec), 0o600); err != nil {
		t.Fatal(err)
	}
	c, got := initializeWith(t, Options{Version: "v2", SeenPath: seen})
	if strings.Contains(got, ChangedLine) {
		t.Fatal("the notice showed with no release to list")
	}
	res := call(t, c, "changes", `{}`)
	if res.IsError || !strings.Contains(textOf(t, res), "No changes after") {
		t.Fatalf("changes = %v %s", res.IsError, textOf(t, res))
	}
}
