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
			fs.StringVar(&in.Tool, "tool", "", "the op it is about, such as sweep_record")
		},
		Bind: func(args []string, in *ops.FeedbackAddInput) error { in.Text = args[0]; return nil },
		Text: func(w io.Writer, out ops.FeedbackAddOutput) error {
			_, err := fmt.Fprintf(w, "%s\n", out.ID)
			return err
		},
	}))
	return cmd
}

func newSentCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "sent", Short: "Agent work you asked for: Send to agent, and what you asked an agent in the chat", Args: cobra.NoArgs}
	cmd.AddCommand(opCommand(l, ops.SentList, binding[ops.SentListInput, ops.SentListOutput]{
		Use:  "list",
		Args: cobra.NoArgs,
		Flags: func(fs *pflag.FlagSet, in *ops.SentListInput) {
			fs.IntVar(&in.Limit, "limit", 0, "at most this many; 0 means all")
		},
		Text: func(w io.Writer, out ops.SentListOutput) error {
			p := &printer{w: w}
			for _, it := range out.Items {
				status := string(it.Status)
				if it.ClaimedBy != "" {
					status += " by " + it.ClaimedBy
				}
				if it.Resolution != "" {
					status += ", " + string(it.Resolution)
				}
				if it.Stale {
					status += ", stale"
				}
				p.f("%s\t%s\t%s\t%s\t%s\n", it.Request, it.Kind, status, it.Item, it.Note)
			}
			p.f("open %d, stale %d\n", out.Open, out.Stale)
			return p.err
		},
	}))
	return cmd
}
