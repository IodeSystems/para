package cli

import (
	"io"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/para/internal/ops"
)

// newDelegationCmd is `para delegation run <id>`: the dry run of the
// delegation rules of .para/delegation.yaml on one outgoing draft (op
// delegation_run).
func newDelegationCmd(l *loader) *cobra.Command {
	cmd := group("delegation", "Delegation rules (.para/delegation.yaml): which drafts an agent may send for you, stamped, without your review")
	cmd.AddCommand(opCommand(l, ops.DelegationRun, binding[ops.DelegationRunInput, ops.DelegationRunOutput]{
		Use:  "run <id> [--agent <name>]",
		Args: cobra.ExactArgs(1),
		Flags: func(fs *pflag.FlagSet, in *ops.DelegationRunInput) {
			fs.StringVar(&in.Agent, "agent", "", "for a draft that names no agent: the agent to judge it for")
		},
		Bind: func(args []string, in *ops.DelegationRunInput) error { in.ID = args[0]; return nil },
		Text: func(w io.Writer, out ops.DelegationRunOutput) error {
			p := &printer{w: w}
			p.f("%s\tvoice %s\tagent %s\n", out.ID, out.Voice, firstNonEmpty(out.Agent, "-"))
			p.f("rule\t%s\n", firstNonEmpty(out.Rule, "none"))
			p.f("stamp\t%s\t(stamped: %t)\n", out.Stamp, out.Stamped)
			for _, m := range out.Misses {
				p.f("  rule %d\t%s\t%s\n", m.Rule, m.Name, m.Why)
			}
			p.f("next\t%s\n", out.Next)
			return p.err
		},
	}))
	return cmd
}
