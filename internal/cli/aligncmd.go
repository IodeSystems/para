package cli

import (
	"io"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/parasolve/internal/ops"
)

// newAlignCmd is `para align`: what to check against reality, and the record of
// a check. Para fetches nothing: the agent compares with its own tools.
func newAlignCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "align", Short: "Compare the vault with reality: what to check, and the record of a run", Args: cobra.NoArgs}
	cmd.AddCommand(
		opCommand(l, ops.AlignScope, binding[ops.AlignScopeInput, ops.AlignScopeOutput]{
			Use:  "scope <goal|area|vault>",
			Args: cobra.ExactArgs(1),
			Bind: func(args []string, in *ops.AlignScopeInput) error { in.Scope = args[0]; return nil },
			Text: func(w io.Writer, out ops.AlignScopeOutput) error {
				p := &printer{w: w}
				p.f("%s\t%s\t%s\tlast aligned %s\n", out.Scope, out.Kind, out.Title, dashIfEmpty(out.LastAligned))
				for _, k := range out.Links {
					p.f("  link\t%s\t%s\t%s\n", k.Kind, k.Label, k.Target)
				}
				for _, it := range out.Items {
					p.f("%s\t%s\t%s\t%s\t%s\n", it.ID, it.Kind, it.State, dashIfEmpty(it.Due), it.Title)
					for _, t := range it.Targets {
						p.f("  target\t%s\t%s\t%s\t%s\n", t.To, t.Medium, t.State, t.Ref)
					}
				}
				return p.err
			},
		}),
		opCommand(l, ops.AlignRecord, binding[ops.AlignRecordInput, ops.AlignRecordOutput]{
			Use:  "record <goal|area|vault> --checked N --matched N --differ N",
			Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.AlignRecordInput) {
				fs.IntVar(&in.Checked, "checked", 0, "items compared with reality")
				fs.IntVar(&in.Matched, "matched", 0, "items that matched")
				fs.IntVar(&in.Differ, "differ", 0, "items that differ ")
				fs.StringArrayVar(&in.Unreachable, "unreachable", nil, "a source you could not reach (repeat)")
			},
			Bind: func(args []string, in *ops.AlignRecordInput) error { in.Scope = args[0]; return nil },
			Text: func(w io.Writer, out ops.AlignRecordOutput) error {
				p := &printer{w: w}
				p.f("%s\t%s\t%s\t%s\n", out.Scope, out.Date, out.Receipt, out.Summary)
				return p.err
			},
		}),
	)
	return cmd
}

func dashIfEmpty(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
