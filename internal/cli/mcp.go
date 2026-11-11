package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/iodesystems/para/internal/guide"
	"github.com/iodesystems/para/internal/mcpserver"
	"github.com/iodesystems/para/internal/ops"
	"github.com/iodesystems/para/internal/registry"
	"github.com/iodesystems/para/internal/store/local"
	"github.com/iodesystems/para/internal/upgrade"
	"github.com/iodesystems/para/internal/vault"
)

const mcpSurface = "mcp"

// newMCPCmd serves ops as MCP tools over stdio. It is an admin command, not
// an op. The vault is chosen once at start by the normal rule and cannot
// change; selection errors stop the server.
func newMCPCmd(l *loader) *cobra.Command {
	var actor string
	var helpClaude, helpDun, channel, helpRaglit, readOnly, surface bool
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Serve ops as MCP tools over stdio (one vault)",
		Long: `Serve ops as MCP tools over stdio, for one vault.
--help-claude and --help-dun print setup steps for that harness, with the
server entries for this install. They write nothing.
--help-raglit prints how to add meaning search with a raglit daemon.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// A running para mcp asks the binary installed after it for this,
			// to decide what it can keep serving. It reads no vault.
			if surface {
				sf, err := mcpserver.CurrentSurface(l.version)
				if err != nil {
					return err
				}
				return json.NewEncoder(cmd.OutOrStdout()).Encode(sf)
			}
			if err := l.load(); err != nil {
				return err
			}
			if helpRaglit {
				_, err := fmt.Fprintln(cmd.OutOrStdout(), guide.Raglit())
				return err
			}
			if helpClaude || helpDun {
				if helpClaude && helpDun {
					return fmt.Errorf("mcp: choose --help-claude or --help-dun")
				}
				client := "dun"
				if helpClaude {
					client = "claude"
				}
				return writeHarnessHelp(cmd, l, client)
			}
			if actor == "" {
				return fmt.Errorf("mcp: --actor must not be empty")
			}
			sel, err := l.selectVault()
			if err != nil {
				return err
			}
			// A changed default must not move a running agent to another vault.
			if sel.Via == registry.ViaDefault {
				return fmt.Errorf("mcp: name the vault with --vault; the default vault does not apply to agents")
			}
			if err := l.install.CheckVault(sel.ID, sel.Path); err != nil {
				return err
			}
			install, err := l.install.ID()
			if err != nil {
				return err
			}
			s, err := local.Open(sel.Path, vault.Options{ID: sel.ID, Actor: actor, Surface: mcpSurface, Install: install, StateDir: l.stateDir(sel.ID)})
			if err == nil {
				err = upgrade.Check(s)
			}
			if err == nil && !readOnly {
				err = upgrade.RecordSession(cmd.Context(), s, l.stateDir(sel.ID), l.version)
			}
			if err != nil {
				return fmt.Errorf("vault %s: %w", sel.ID, err)
			}
			// An agent never signs the browser in: it opens the plain URL.
			base, open, signedIn := l.viewer(sel.ID, false)
			srv, err := mcpserver.New(ops.Deps{Store: s, ViewerBase: base, OpenURL: open, SignedIn: signedIn}, mcpserver.Options{
				Version:          l.version,
				Hide:             []string{filepath.Clean(sel.Path), l.install.Home},
				Log:              cmd.ErrOrStderr(),
				EventsDir:        filepath.Join(sel.Path, "events"),
				Channel:          channel,
				Executable:       mcpserver.ExecutablePath(),
				InstalledSurface: installedSurface,
				InstalledVersion: installedVersion,
				SeenPath:         filepath.Join(l.stateDir(sel.ID), "mcp_seen.json"),
				ReadOnly:         readOnly,
				PresenceDir:      l.stateDir(sel.ID),
				Notice:           l.updateNotice,
			})
			if err != nil {
				return err
			}
			return srv.Serve(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&actor, "actor", "agent", "actor name recorded in events")
	cmd.Flags().BoolVar(&channel, "channel", false, "also send notifications as Claude Code channel events (research preview; Claude Code needs --dangerously-load-development-channels); speaks only the initialize handshake, protocol 2025-11-25 and older")
	cmd.Flags().BoolVar(&readOnly, "read-only", false, "list only the tools that change nothing (policy, rules, search): for a worker agent that should not write")
	cmd.Flags().BoolVar(&helpClaude, "help-claude", false, "print setup steps for Claude Code")
	cmd.Flags().BoolVar(&helpRaglit, "help-raglit", false, "print how to connect a raglit daemon for meaning search")
	cmd.Flags().BoolVar(&helpDun, "help-dun", false, "print setup steps for dun")
	cmd.Flags().BoolVar(&surface, "surface", false, "print this binary's vault format, types, and tool schema hashes as JSON, for a running para mcp")
	_ = cmd.Flags().MarkHidden("surface")
	cmd.AddCommand(newMCPConfigCmd(l))
	return cmd
}

// mcpServerEntry is one `para mcp` launch line for a vault.
type mcpServerEntry struct {
	Name    string
	Command string
	Args    []string
}

// newMCPConfigCmd prints a ready MCP server entry per vault. It is an admin
// command, not an op, and writes nothing: the owner pastes the entry into the
// client's config. One entry per vault keeps vaults apart: a client never
// sees two vaults through one server.
func newMCPConfigCmd(l *loader) *cobra.Command {
	var client string
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Print an MCP server entry per vault, named para-<vault> (prints only; writes nothing)",
		Long: `Print a ready MCP server entry for each vault, or for the vault named by --vault.
Clients: json (the mcpServers object of .mcp.json and Claude Desktop; default),
claude (claude mcp add commands), dun (a servers list for dun's mcp.json).
The command is the absolute path of this binary, with --vault set.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := l.load(); err != nil {
				return err
			}
			if !slices.Contains([]string{"json", "claude", "dun"}, client) {
				return fmt.Errorf("mcp config: --client %q: want claude, dun, or json", client)
			}
			picked, err := mcpEntries(cmd, l)
			if err != nil {
				return err
			}
			return writeMCPConfig(cmd.OutOrStdout(), client, picked)
		},
	}
	cmd.Flags().StringVar(&client, "client", "json", "claude, dun, or json")
	return cmd
}

// mcpEntries is one server entry per usable vault, or for --vault.
func mcpEntries(cmd *cobra.Command, l *loader) ([]mcpServerEntry, error) {
	exe, err := l.executable()
	if err != nil {
		return nil, fmt.Errorf("mcp config: %w", err)
	}
	entries, err := l.install.Entries()
	if err != nil {
		return nil, err
	}
	var picked []mcpServerEntry
	for _, e := range entries {
		if l.vaultFlag != "" && e.ID != l.vaultFlag {
			continue
		}
		if e.Err != "" {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "para: skipping vault %s: %s\n", e.ID, e.Err)
			continue
		}
		args := []string{"mcp", "--vault", e.ID}
		if l.homeFlag != "" {
			home, err := filepath.Abs(l.homeFlag)
			if err != nil {
				return nil, fmt.Errorf("mcp config: %w", err)
			}
			args = append([]string{"--home", home}, args...)
		}
		picked = append(picked, mcpServerEntry{Name: "para-" + e.ID, Command: exe, Args: args})
	}
	if len(picked) == 0 {
		if l.vaultFlag != "" {
			return nil, fmt.Errorf("mcp config: unknown vault %q", l.vaultFlag)
		}
		return nil, fmt.Errorf("mcp config: no vaults in %s; run para init <id>", l.install.Home)
	}
	return picked, nil
}

// writeHarnessHelp prints setup steps for a harness. With no vaults yet, the
// steps still print, and say to create one.
func writeHarnessHelp(cmd *cobra.Command, l *loader, client string) error {
	var config strings.Builder
	picked, err := mcpEntries(cmd, l)
	switch {
	case err != nil:
		config.WriteString("   No vault here yet. Run: para init <id>, then this command again.")
	default:
		if err := writeMCPConfig(&config, client, picked); err != nil {
			return err
		}
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), guide.Harness(client, config.String()))
	return err
}

// claudeAddArgs is the claude mcp add command for e, with its actor already in
// e.Args. User scope: the vault is the same in every project.
func claudeAddArgs(e mcpServerEntry) []string {
	return append([]string{"claude", "mcp", "add", "--scope", "user", e.Name, "--", e.Command}, e.Args...)
}

func writeMCPConfig(w io.Writer, client string, entries []mcpServerEntry) error {
	// A named harness gets its own actor, so events show who wrote what.
	if client == "claude" || client == "dun" {
		named := make([]mcpServerEntry, len(entries))
		for i, e := range entries {
			e.Args = append(slices.Clone(e.Args), "--actor", client)
			named[i] = e
		}
		entries = named
	}
	switch client {
	case "claude":
		for _, e := range entries {
			// User scope: the vault is the same in every project.
			parts := claudeAddArgs(e)
			for i, p := range parts {
				parts[i] = shellQuote(p)
			}
			if _, err := fmt.Fprintln(w, strings.Join(parts, " ")); err != nil {
				return err
			}
		}
		return nil
	case "dun":
		type spec struct {
			ID      string   `json:"id"`
			Command string   `json:"command"`
			Args    []string `json:"args"`
		}
		servers := []spec{}
		for _, e := range entries {
			servers = append(servers, spec{e.Name, e.Command, e.Args})
		}
		return writeJSON(w, map[string]any{"servers": servers})
	}
	type spec struct {
		Command string   `json:"command"`
		Args    []string `json:"args"`
	}
	servers := map[string]spec{}
	for _, e := range entries {
		servers[e.Name] = spec{e.Command, e.Args}
	}
	return writeJSON(w, map[string]any{"mcpServers": servers})
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

var shellSafe = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

// shellQuote quotes s for a POSIX shell when it needs it.
func shellQuote(s string) string {
	if shellSafe.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// newBootstrapCmd prints the bootstrap interview, the text of the MCP prompt
// of the same name, so it can be pasted into any agent. It reads no vault.
func newBootstrapCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "bootstrap",
		Short: "Print the instructions that make an agent interview you and fill a new vault",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), guide.Bootstrap())
			return err
		},
	}
}

// installedVersion asks the binary now on disk for its version. It gives up
// after a few seconds and reports nothing.
func installedVersion() string {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, mcpserver.ExecutablePath(), "version").Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// installedSurface asks the binary now on disk for its surface. A binary
// older than --surface fails, and the running server then refuses every tool.
func installedSurface() (mcpserver.Surface, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, mcpserver.ExecutablePath(), "mcp", "--surface").Output()
	if err != nil {
		return mcpserver.Surface{}, fmt.Errorf("mcp: installed surface: %w", err)
	}
	var sf mcpserver.Surface
	if err := json.Unmarshal(out, &sf); err != nil {
		return mcpserver.Surface{}, fmt.Errorf("mcp: installed surface: %w", err)
	}
	return sf, nil
}
