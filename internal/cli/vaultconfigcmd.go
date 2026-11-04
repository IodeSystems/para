package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/iodesystems/para/internal/ops"
)

// newVaultConfigCmd is `para vault config`: the settings in
// <vault>/.para/vault.properties, checked against one table of known keys.
func newVaultConfigCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "List, show, set, or unset vault settings (.para/vault.properties)",
		Long: `Vault settings live in <vault>/.para/vault.properties. Only known keys can be
set, and each value is checked. "list" shows every known key and any other key
in the file. An agent (--actor) may change only align.days, goals.active_limit, and
recheck.days; the rest are the owner's, egress.* among them. The event of a change holds the
key, never the value.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	cmd.AddCommand(
		opCommand(l, ops.VaultConfigList, binding[ops.VaultConfigListInput, ops.VaultConfigListOutput]{
			Use:  "list",
			Args: cobra.NoArgs,
			Text: func(w io.Writer, o ops.VaultConfigListOutput) error {
				for _, s := range o.Settings {
					if _, err := fmt.Fprintf(w, "%s\t%s\n", s.Key, settingText(s)); err != nil {
						return err
					}
				}
				return nil
			},
		}),
		opCommand(l, ops.VaultConfigGet, binding[ops.VaultConfigGetInput, ops.ConfigSetting]{
			Use:  "get <key>",
			Args: cobra.ExactArgs(1),
			Bind: func(args []string, in *ops.VaultConfigGetInput) error { in.Key = args[0]; return nil },
			Text: func(w io.Writer, s ops.ConfigSetting) error {
				_, err := fmt.Fprintln(w, s.Value)
				return err
			},
		}),
		opCommand(l, ops.VaultConfigSet, binding[ops.VaultConfigSetInput, ops.VaultConfigSetOutput]{
			Use:  "set <key> <value>",
			Args: cobra.ExactArgs(2),
			Bind: func(args []string, in *ops.VaultConfigSetInput) error {
				in.Key, in.Value = args[0], args[1]
				return nil
			},
			Text: func(w io.Writer, o ops.VaultConfigSetOutput) error {
				verb := "set"
				if !o.Changed {
					verb = "unchanged"
				}
				_, err := fmt.Fprintf(w, "%s %s=%s\n", verb, o.Key, o.Value)
				return err
			},
		}),
		opCommand(l, ops.VaultConfigUnset, binding[ops.VaultConfigUnsetInput, ops.VaultConfigSetOutput]{
			Use:  "unset <key>",
			Args: cobra.ExactArgs(1),
			Bind: func(args []string, in *ops.VaultConfigUnsetInput) error { in.Key = args[0]; return nil },
			Text: func(w io.Writer, o ops.VaultConfigSetOutput) error {
				verb := "unset"
				if !o.Changed {
					verb = "was not set:"
				}
				_, err := fmt.Fprintf(w, "%s %s\n", verb, o.Key)
				return err
			},
		}),
	)
	return cmd
}

func settingText(s ops.ConfigSetting) string {
	switch {
	case s.ReadOnly:
		return s.Value + "\t(read-only)"
	case !s.Known:
		return s.Value + "\t(unknown key)"
	case !s.Set:
		return "(unset)\t" + s.Help
	}
	return s.Value
}
