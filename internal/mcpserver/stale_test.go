package mcpserver

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/iodesystems/para/internal/ops"
	"github.com/iodesystems/para/internal/store"
	"github.com/iodesystems/para/internal/upgrade"
)

// staleServer starts a server on a fake binary and returns a client, the
// store, and a func that installs a new binary by rename, as make install does.
func staleServer(t *testing.T, installed func() (Surface, error)) (*client.Client, store.Store, func()) {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "para")
	if err := os.WriteFile(bin, []byte("v1"), 0o755); err != nil {
		t.Fatal(err)
	}
	s, _ := newVault(t)
	srv, err := New(ops.Deps{Store: s}, Options{Version: "test", Executable: bin, InstalledSurface: installed})
	if err != nil {
		t.Fatal(err)
	}
	c, err := client.NewInProcessClient(srv.MCP())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Initialize(ctx, mcp.InitializeRequest{}); err != nil {
		t.Fatal(err)
	}
	n := 0
	install := func() {
		n++
		tmp := bin + ".new"
		if err := os.WriteFile(tmp, []byte("v2, longer"+strings.Repeat("!", n)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(tmp, bin); err != nil {
			t.Fatal(err)
		}
	}
	return c, s, install
}

func thisSurface(t *testing.T) Surface {
	t.Helper()
	sf, err := CurrentSurface("v2")
	if err != nil {
		t.Fatal(err)
	}
	return sf
}

func allText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, p := range res.Content {
		if tc, ok := p.(mcp.TextContent); ok {
			b.WriteString(tc.Text + "\n")
		}
	}
	return b.String()
}

func TestUpgradedBinaryIsRefusedWithARestartMessage(t *testing.T) {
	// No InstalledSurface: the server cannot tell what changed.
	c, _, install := staleServer(t, nil)
	if res := call(t, c, "lint", ""); res.IsError {
		t.Fatalf("lint before the upgrade: %s", textOf(t, res))
	}
	install()
	res := call(t, c, "lint", "")
	if !res.IsError || !strings.Contains(textOf(t, res), "reconnect para") {
		t.Fatalf("after the upgrade: error=%v %s", res.IsError, textOf(t, res))
	}
}

func TestSameSurfaceKeepsWritingWithOneNote(t *testing.T) {
	asked := 0
	c, _, install := staleServer(t, func() (Surface, error) { asked++; return thisSurface(t), nil })
	install()
	for i := range 3 {
		res := call(t, c, "io_add", `{"direction":"in","text":"Capture `+strconv.Itoa(i)+`"}`)
		if res.IsError {
			t.Fatalf("write %d after a compatible upgrade: %s", i, allText(res))
		}
		// The first answer after the install says once that a reconnect
		// brings the fixes inside the tools.
		wantParts := 1
		if i == 0 {
			wantParts = 2
			if msg := allText(res); !strings.Contains(msg, "upgraded to v2") || !strings.Contains(msg, "unchanged") || !strings.Contains(msg, "/mcp") {
				t.Fatalf("note: %s", msg)
			}
		}
		if len(res.Content) != wantParts {
			t.Fatalf("write %d: parts = %d, want %d: %s", i, len(res.Content), wantParts, allText(res))
		}
	}
	if asked != 1 {
		t.Fatalf("asked the installed binary %d times, want once per install", asked)
	}
	install()
	if res := call(t, c, "lint", ""); res.IsError || asked != 2 || len(res.Content) != 2 {
		t.Fatalf("second install: error=%v asked=%d %s", res.IsError, asked, allText(res))
	}
}

func TestChangedToolAloneIsRefused(t *testing.T) {
	c, _, install := staleServer(t, func() (Surface, error) {
		sf := thisSurface(t)
		sf.Tools["io_add"] = "changed"
		sf.Tools["brand_new"] = "x"
		return sf, nil
	})
	install()
	res := call(t, c, "lint", "")
	msg := allText(res)
	if res.IsError || !strings.Contains(msg, "io_add") || !strings.Contains(msg, "brand_new") || !strings.Contains(msg, "/mcp") {
		t.Fatalf("first answer after the upgrade lacks the note: error=%v %s", res.IsError, msg)
	}
	if res := call(t, c, "lint", ""); res.IsError || len(res.Content) != 1 {
		t.Fatalf("the note is said once: %s", allText(res))
	}
	res = call(t, c, "io_add", `{"direction":"in","text":"Capture"}`)
	if !res.IsError || !strings.Contains(textOf(t, res), "this tool changed") || !strings.Contains(textOf(t, res), "reconnect para") {
		t.Fatalf("changed tool: error=%v %s", res.IsError, allText(res))
	}
	if res := call(t, c, "note_add", `{"title":"Kept","body":"x"}`); res.IsError {
		t.Fatalf("unchanged write tool: %s", allText(res))
	}
}

func TestNewFormatOrTypesRefuseEveryTool(t *testing.T) {
	for name, edit := range map[string]func(*Surface){
		"format": func(sf *Surface) { sf.Format++ },
		"types":  func(sf *Surface) { sf.Types = append(sf.Types, "gadget") },
	} {
		t.Run(name, func(t *testing.T) {
			c, _, install := staleServer(t, func() (Surface, error) { sf := thisSurface(t); edit(&sf); return sf, nil })
			install()
			res := call(t, c, "lint", "")
			if !res.IsError || !strings.Contains(textOf(t, res), "cannot serve") {
				t.Fatalf("error=%v %s", res.IsError, allText(res))
			}
		})
	}
}

func TestNewerVaultFormatRefusesWrites(t *testing.T) {
	c, s, install := staleServer(t, func() (Surface, error) { return thisSurface(t), nil })
	install()
	if err := s.SetConfig(context.Background(), upgrade.ConfigKey, strconv.Itoa(upgrade.Latest()+1)); err != nil {
		t.Fatal(err)
	}
	res := call(t, c, "io_add", `{"direction":"in","text":"Capture"}`)
	if !res.IsError || !strings.Contains(textOf(t, res), "newer than this para") {
		t.Fatalf("write to a newer vault: error=%v %s", res.IsError, allText(res))
	}
}
