package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/iodesystems/para/docs"
	"github.com/iodesystems/para/internal/props"
	"github.com/iodesystems/para/internal/registry"
	"github.com/iodesystems/para/internal/upgrade"
	"github.com/iodesystems/para/internal/vault"
)

func newInitCmd(l *loader) *cobra.Command {
	var noGit, makeDefault bool
	var kind string
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
			out, err := createVault(l, id, path, kind, noGit, makeDefault)
			if err != nil {
				return err
			}
			return emit(cmd, out, func(w io.Writer, v registry.Vault) error {
				if _, err := fmt.Fprintf(w, "%s\t%s\n", v.ID, v.Path); err != nil {
					return err
				}
				if makeDefault {
					if _, err := fmt.Fprintf(w, "default vault: %s\n", v.ID); err != nil {
						return err
					}
				}
				return writeNextSteps(cmd, l, v.ID)
			})
		},
	}
	cmd.Flags().BoolVar(&makeDefault, "default", false, "make this the default vault on this machine (as para vault set-default does; init does not do it on its own)")
	cmd.Flags().StringVar(&kind, "kind", "", "work or personal: records the machine kind when none is set; a vault of the other kind is refused (para setup asks for it)")
	cmd.Flags().BoolVar(&noGit, "no-git", false, "do not run git init (the vault may live in a file-sync folder)")
	addJSONFlag(cmd)
	return cmd
}

// machineKind is the kind of this machine. flag is --kind: it records the kind
// when none is set and must match it when one is. With neither, it refuses.
func machineKind(l *loader, flag string) (string, error) {
	if flag != "" && !registry.ValidKind(flag) {
		return "", fmt.Errorf("init: --kind %q must be work or personal", flag)
	}
	have, err := l.install.Kind()
	if err != nil {
		return "", err
	}
	switch {
	case have == "" && flag == "":
		return "", errors.New("init: no machine kind is recorded: run para setup, or pass --kind work|personal (work and personal never share a machine)")
	case have == "":
		if err := l.install.SetKind(flag); err != nil {
			return "", err
		}
		return flag, nil
	case flag != "" && flag != have:
		return "", fmt.Errorf("init: this machine is %s, not %s: work and personal never share a machine", have, flag)
	}
	return have, nil
}

// checkVaultKind refuses a vault of the other kind than this machine. A
// machine or vault with no kind recorded passes; para doctor warns about it.
func (l *loader) checkVaultKind(id, path string) error {
	mk, err := l.install.Kind()
	if err != nil || mk == "" {
		return err
	}
	cfg, err := props.Load(filepath.Join(path, ".para", "vault.properties"))
	if err != nil {
		return nil // not readable here: Open reports it
	}
	if vk := cfg[vault.KeyKind]; vk != "" && vk != mk {
		return fmt.Errorf("vault %s is %s and this machine is %s: work and personal never share a machine", id, vk, mk)
	}
	return nil
}

// createVault is para init: it makes the vault, registers it, and records the
// machine kind. It is also the first-vault step of para setup.
func createVault(l *loader, id, path, kindFlag string, noGit, makeDefault bool) (registry.Vault, error) {
	kind, err := machineKind(l, kindFlag)
	if err != nil {
		return registry.Vault{}, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return registry.Vault{}, fmt.Errorf("init: %w", err)
	}
	existing, err := l.install.List()
	if err != nil {
		return registry.Vault{}, err
	}
	for _, v := range existing {
		if v.ID == id {
			return registry.Vault{}, fmt.Errorf("init: vault %q is already registered at %s", id, v.Path)
		}
	}
	templates, err := fs.Sub(docs.Templates, "templates")
	if err != nil {
		return registry.Vault{}, fmt.Errorf("init: %w", err)
	}
	if err := vault.Init(abs, id, templates, vault.InitOptions{NoGit: noGit, FormatVersion: upgrade.Latest(), Kind: kind}); err != nil {
		return registry.Vault{}, err
	}
	if err := l.install.Add(id, abs); err != nil {
		return registry.Vault{}, err
	}
	if err := l.baseline(id, abs); err != nil {
		return registry.Vault{}, err
	}
	if makeDefault {
		// The same call as para vault set-default.
		if err := l.install.SetDefault(id); err != nil {
			return registry.Vault{}, err
		}
	}
	return registry.Vault{ID: id, Path: abs}, nil
}

// vaultMCPEntry is the MCP server entry of vault id.
func vaultMCPEntry(cmd *cobra.Command, l *loader, id string) (mcpServerEntry, error) {
	entries, err := mcpEntries(cmd, l)
	if err != nil {
		return mcpServerEntry{}, err
	}
	for _, e := range entries {
		if e.Name == "para-"+id {
			return e, nil
		}
	}
	return mcpServerEntry{}, fmt.Errorf("mcp config: unknown vault %q", id)
}

// claudeLine is the claude mcp add line para help claude prints for e.
func claudeLine(e mcpServerEntry) (string, error) {
	var line strings.Builder
	if err := writeMCPConfig(&line, "claude", []mcpServerEntry{e}); err != nil {
		return "", err
	}
	return strings.TrimSpace(line.String()), nil
}

// writeNextSteps prints what to run after init: the optional agent hookup, the
// web UI, and sign-in. The generic MCP command comes first; Claude Code and dun
// are two clients of it.
func writeNextSteps(cmd *cobra.Command, l *loader, id string) error {
	e, err := vaultMCPEntry(cmd, l, id)
	if err != nil {
		return err
	}
	line, err := claudeLine(e)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), `
Next steps
  Hook up an agent (optional):
  Claude Code (then restart the session):
    %s
    para skill install
  dun: para help dun
  Any other MCP client over HTTP (needs the server running): para setup --agent generic --agent-name <name>
  Web UI: para serve (foreground, port 7700), or para service install (starts at login).
  Sign in to the web UI: para serve login (with the server running).
`, line)
	return err
}

func newVaultCmd(l *loader) *cobra.Command {
	var prune, dry bool
	cmd := &cobra.Command{
		Use:   "vault",
		Short: "Inspect registered vaults; --prune drops entries whose directory is gone",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if dry && !prune {
				return errors.New("vault: --dry-run needs --prune")
			}
			if !prune {
				return cmd.Help()
			}
			if err := l.load(); err != nil {
				return err
			}
			// Admin command, not an op: it edits the install registry, which
			// no agent surface may reach. It deletes only vault.ref files.
			gone, err := l.install.Prune(dry)
			out := vaultPruneOutput{DryRun: dry, Pruned: gone}
			if out.Pruned == nil {
				out.Pruned = []registry.Pruned{}
			}
			if err != nil {
				return err
			}
			return emit(cmd, out, func(w io.Writer, o vaultPruneOutput) error {
				verb := "pruned"
				if o.DryRun {
					verb = "would prune"
				}
				for _, p := range o.Pruned {
					if _, err := fmt.Fprintf(w, "%s\t%s\t%s\n", verb, p.ID, p.Path); err != nil {
						return err
					}
				}
				return nil
			})
		},
	}
	cmd.Flags().BoolVar(&prune, "prune", false, "remove registry entries whose vault directory no longer exists (keeps keys and state)")
	cmd.Flags().BoolVar(&dry, "dry-run", false, "with --prune: print what would be pruned and change nothing")
	addJSONFlag(cmd)
	var clearDefault bool
	setDefault := &cobra.Command{
		Use:   "set-default [vault]",
		Short: "Set the vault commands use on this machine when none is named",
		Long: `Set the vault used on this machine when --vault and PARA_VAULT are unset.
It applies to you, not to agents: para mcp still needs --vault.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := l.load(); err != nil {
				return err
			}
			if clearDefault == (len(args) == 1) {
				return errors.New("vault set-default: give a vault id, or --clear")
			}
			id := ""
			if len(args) == 1 {
				id = args[0]
			}
			if err := l.install.SetDefault(id); err != nil {
				return err
			}
			if id == "" {
				id = "(none)"
			}
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "default vault: %s\n", id)
			return err
		},
	}
	setDefault.Flags().BoolVar(&clearDefault, "clear", false, "remove the default")
	cmd.AddCommand(setDefault, newVaultUpgradeCmd(l), newVaultCheckCmd(l), newVaultConfigCmd(l))
	list := &cobra.Command{
		Use:   "list",
		Short: "List registered vaults",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := l.load(); err != nil {
				return err
			}
			// Entries, not List: a broken entry must show, not hide the others.
			entries, err := l.install.Entries()
			if err != nil {
				return err
			}
			def, err := l.install.Default()
			if err != nil {
				return err
			}
			out := vaultListOutput{Vaults: []vaultListEntry{}}
			problems := 0
			for _, e := range entries {
				v := vaultListEntry{Entry: e, Default: e.ID == def}
				if e.Err == "" {
					if _, err := os.Stat(e.Path); errors.Is(err, fs.ErrNotExist) {
						v.Missing = true
					}
				}
				if e.Err != "" || v.Missing {
					problems++
				}
				out.Vaults = append(out.Vaults, v)
			}
			return emit(cmd, out, func(w io.Writer, o vaultListOutput) error {
				for _, v := range o.Vaults {
					var err error
					switch {
					case v.Err != "":
						_, err = fmt.Fprintf(w, "%s\tbroken: %s\n", v.ID, v.Err)
					case v.Missing:
						_, err = fmt.Fprintf(w, "%s\t%s\tmissing\n", v.ID, v.Path)
					default:
						mark := ""
						if v.Default {
							mark = "\tdefault"
						}
						_, err = fmt.Fprintf(w, "%s\t%s%s\n", v.ID, v.Path, mark)
					}
					if err != nil {
						return err
					}
				}
				if problems > 0 {
					_, err := fmt.Fprintln(cmd.ErrOrStderr(), "para: some entries are missing or broken; para vault --prune --dry-run lists the ones it can drop")
					return err
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

type vaultPruneOutput struct {
	DryRun bool              `json:"dry_run"`
	Pruned []registry.Pruned `json:"pruned"`
}

// vaultListEntry is a registry entry. Missing means the path does not exist
// (an unmounted drive looks the same); Err means vault.ref is unreadable.
type vaultListEntry struct {
	registry.Entry
	Missing bool `json:"missing,omitempty"`
	Default bool `json:"default,omitempty"`
}

type vaultListOutput struct {
	Vaults []vaultListEntry `json:"vaults"`
}
