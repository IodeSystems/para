package cli

import (
	"io"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/parasolve/internal/ops"
)

// newActionCmd is `para action record`: the receipt of an outward action done
// with other tools. Para sends nothing.
func newActionCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "action", Short: "Record an outward action done with other tools (para sends nothing)", Args: cobra.NoArgs}
	cmd.AddCommand(
		opCommand(l, ops.ActionRecord, binding[ops.ActionRecordInput, ops.ActionRecordOutput]{
			Use:  "record --what <text> --where <system>",
			Args: cobra.NoArgs,
			Flags: func(fs *pflag.FlagSet, in *ops.ActionRecordInput) {
				fs.StringVar(&in.What, "what", "", "what was done, in plain words")
				fs.StringVar(&in.Where, "where", "", "the system: a repo, a channel, a ticket queue")
				fs.StringVar(&in.URL, "url", "", "where the result can be seen")
				fs.StringVar(&in.OwnerSaid, "owner-said", "", "the owner's go-ahead, in their words")
				fs.StringArrayVar(&in.Sources, "source", nil, "a document id or URL the action rests on (repeat)")
			},
			Text: func(w io.Writer, out ops.ActionRecordOutput) error {
				p := &printer{w: w}
				p.f("%s\n", out.Receipt)
				return p.err
			},
		}),
	)
	return cmd
}
