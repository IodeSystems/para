package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/iodesystems/parasolve/internal/mcpserver"
	"github.com/iodesystems/parasolve/internal/ops"
	"github.com/iodesystems/parasolve/internal/store/local"
	"github.com/iodesystems/parasolve/internal/vault"
)

const mcpSurface = "mcp"

// newMCPCmd serves ops as MCP tools over stdio. It is an admin command, not
// an op. The vault is chosen once at start by the normal rule and cannot
// change; selection errors stop the server.
func newMCPCmd(l *loader) *cobra.Command {
	var actor string
	cmd := &cobra.Command{
		Use:   "mcp",
		Short: "Serve ops as MCP tools over stdio (one vault)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := l.load(); err != nil {
				return err
			}
			if actor == "" {
				return fmt.Errorf("mcp: --actor must not be empty")
			}
			sel, err := l.selectVault()
			if err != nil {
				return err
			}
			install, err := l.install.ID()
			if err != nil {
				return err
			}
			s, err := local.Open(sel.Path, vault.Options{ID: sel.ID, Actor: actor, Surface: mcpSurface, Install: install, StateDir: l.stateDir(sel.ID)})
			if err != nil {
				return fmt.Errorf("vault %s: %w", sel.ID, err)
			}
			srv, err := mcpserver.New(ops.Deps{Store: s}, mcpserver.Options{
				Version: l.version,
				Hide:    []string{filepath.Clean(sel.Path), l.install.Home},
				Log:     cmd.ErrOrStderr(),
			})
			if err != nil {
				return err
			}
			return srv.Serve(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&actor, "actor", "agent", "actor name recorded in events")
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
			exe, err := l.executable()
			if err != nil {
				return fmt.Errorf("mcp config: %w", err)
			}
			entries, err := l.install.Entries()
			if err != nil {
				return err
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
						return fmt.Errorf("mcp config: %w", err)
					}
					args = append([]string{"--home", home}, args...)
				}
				picked = append(picked, mcpServerEntry{Name: "para-" + e.ID, Command: exe, Args: args})
			}
			if len(picked) == 0 {
				if l.vaultFlag != "" {
					return fmt.Errorf("mcp config: unknown vault %q", l.vaultFlag)
				}
				return fmt.Errorf("mcp config: no vaults in %s; run para init <id>", l.install.Home)
			}
			return writeMCPConfig(cmd.OutOrStdout(), client, picked)
		},
	}
	cmd.Flags().StringVar(&client, "client", "json", "claude, dun, or json")
	return cmd
}

func writeMCPConfig(w io.Writer, client string, entries []mcpServerEntry) error {
	switch client {
	case "claude":
		for _, e := range entries {
			parts := append([]string{"claude", "mcp", "add", e.Name, "--", e.Command}, e.Args...)
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
