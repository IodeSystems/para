package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/para/internal/ops"
)

func newFeedbackCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "feedback", Short: "Feedback about para itself, filed as io tagged " + ops.FeedbackTag, Args: cobra.NoArgs}
	cmd.AddCommand(opCommand(l, ops.FeedbackAdd, binding[ops.FeedbackAddInput, ops.FeedbackAddOutput]{
		Use:  "add <text>",
		Args: cobra.ExactArgs(1),
		Flags: func(fs *pflag.FlagSet, in *ops.FeedbackAddInput) {
			fs.StringVar((*string)(&in.Kind), "kind", "", "bug, friction (default), or idea")
			fs.StringVar(&in.Tool, "tool", "", "the op it is about, such as proposal_decide")
		},
		Bind: func(args []string, in *ops.FeedbackAddInput) error { in.Text = args[0]; return nil },
		Text: func(w io.Writer, out ops.FeedbackAddOutput) error {
			_, err := fmt.Fprintf(w, "%s\n", out.ID)
			return err
		},
	}))
	return cmd
}
