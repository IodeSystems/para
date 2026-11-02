package cli

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/iodesystems/paradize/internal/mcpserver"
	"github.com/iodesystems/paradize/internal/ops"
	"github.com/iodesystems/paradize/internal/store/local"
	"github.com/iodesystems/paradize/internal/vault"
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
			s, err := local.Open(sel.Path, vault.Options{ID: sel.ID, Actor: actor, Surface: mcpSurface, Install: install, StateDir: filepath.Join(l.install.Home, "vaults", sel.ID)})
			if err != nil {
				return fmt.Errorf("vault %s: %w", sel.ID, err)
			}
			srv, err := mcpserver.New(ops.Deps{Store: s}, mcpserver.Options{
				Version: l.version,
				Hide:    []string{filepath.Clean(sel.Path), l.install.Home},
			})
			if err != nil {
				return err
			}
			return srv.Serve(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&actor, "actor", "agent", "actor name recorded in events")
	return cmd
}
