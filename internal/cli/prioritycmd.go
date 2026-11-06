package cli

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
)

// newPriorityCmd is `para priority set <id> high|normal|low|unset`: how much
// a person, a group, or a source matters (op priority_set).
func newPriorityCmd(l *loader) *cobra.Command {
	cmd := group("priority", "How much a person, a group, or a source matters: an incoming message from a high one needs you")
	cmd.AddCommand(opCommand(l, ops.PrioritySet, binding[ops.PrioritySetInput, ops.PrioritySetOutput]{
		Use:  "set <id> high|normal|low|unset",
		Args: cobra.ExactArgs(2),
		Bind: func(args []string, in *ops.PrioritySetInput) error {
			in.ID = args[0]
			if args[1] != "unset" {
				in.Priority = model.Priority(args[1])
			}
			return nil
		},
		Text: func(w io.Writer, out ops.PrioritySetOutput) error {
			p := &printer{w: w}
			p.f("%s\t%s\n", out.ID, firstNonEmpty(string(out.Priority), "unset"))
			return p.err
		},
	}))
	return cmd
}
