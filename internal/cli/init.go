package cli

import (
	"fmt"
	"io"
	"io/fs"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/iodesystems/parasolve/docs"
	"github.com/iodesystems/parasolve/internal/registry"
	"github.com/iodesystems/parasolve/internal/vault"
)

func newInitCmd(l *loader) *cobra.Command {
	var noGit bool
	cmd := &cobra.Command{
		Use:   "init <id> [path]",
		Short: "Create a vault and register it (default path: ./<id>)",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := l.load(); err != nil {
				return err
			}
			id := args[0]
			path := id
			if len(args) == 2 {
				path = args[1]
			}
			abs, err := filepath.Abs(path)
			if err != nil {
				return fmt.Errorf("init: %w", err)
			}
			existing, err := l.install.List()
			if err != nil {
				return err
			}
			for _, v := range existing {
				if v.ID == id {
					return fmt.Errorf("init: vault %q is already registered at %s", id, v.Path)
				}
			}
			templates, err := fs.Sub(docs.Templates, "templates")
			if err != nil {
				return fmt.Errorf("init: %w", err)
			}
			if err := vault.Init(abs, id, templates, vault.InitOptions{NoGit: noGit}); err != nil {
				return err
			}
			if err := l.install.Add(id, abs); err != nil {
				return err
			}
			if err := l.baseline(id, abs); err != nil {
				return err
			}
			out := registry.Vault{ID: id, Path: abs}
			return emit(cmd, out, func(w io.Writer, v registry.Vault) error {
				_, err := fmt.Fprintf(w, "%s\t%s\n", v.ID, v.Path)
				return err
			})
		},
	}
	cmd.Flags().BoolVar(&noGit, "no-git", false, "do not run git init (the vault may live in a file-sync folder)")
	addJSONFlag(cmd)
	return cmd
}

func newVaultCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "vault", Short: "Inspect registered vaults", Args: cobra.NoArgs}
	list := &cobra.Command{
		Use:   "list",
		Short: "List registered vaults",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := l.load(); err != nil {
				return err
			}
			vaults, err := l.install.List()
			if err != nil {
				return err
			}
			return emit(cmd, vaultListOutput{Vaults: vaults}, func(w io.Writer, o vaultListOutput) error {
				for _, v := range o.Vaults {
					if _, err := fmt.Fprintf(w, "%s\t%s\n", v.ID, v.Path); err != nil {
						return err
					}
				}
				return nil
			})
		},
	}
	useCheck := &cobra.Command{
		Use:   "use-check",
		Short: "Show which vault a command would use, or why none",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := l.load(); err != nil {
				return err
			}
			sel, err := l.selectVault()
			if err != nil {
				return err
			}
			if _, err := vault.Open(sel.Path, vault.Options{ID: sel.ID, ReadOnly: true}); err != nil {
				return err
			}
			return emit(cmd, sel, func(w io.Writer, s registry.Selection) error {
				_, err := fmt.Fprintf(w, "%s\t%s\t(%s)\n", s.ID, s.Path, s.Via)
				return err
			})
		},
	}
	addJSONFlag(list)
	addJSONFlag(useCheck)
	cmd.AddCommand(list, useCheck)
	return cmd
}

type vaultListOutput struct {
	Vaults []registry.Vault `json:"vaults"`
}
