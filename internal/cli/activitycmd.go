package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/para/internal/ops"
)

// newActivityFeedCmd is `para activity`: what changed in the vault.
func newActivityFeedCmd(l *loader) *cobra.Command {
	return opCommand(l, ops.Activity, binding[ops.ActivityInput, ops.ActivityOutput]{
		Use:  "activity",
		Args: cobra.NoArgs,
		Flags: func(fs *pflag.FlagSet, in *ops.ActivityInput) {
			fs.StringVar(&in.Since, "since", "", "from this date or time")
			fs.StringVar(&in.Until, "until", "", "to this date (the end of that day) or time")
			fs.StringVar(&in.Actor, "actor", "", "owner, agents, external, remote, or an agent name")
			fs.StringSliceVar(&in.Types, "type", nil, "only this kind (created, edited, closed, sent, ...); repeat for more")
			fs.StringVar(&in.Doc, "doc", "", "only changes to this item")
			fs.IntVar(&in.Limit, "limit", 0, "most rows; default 50")
			fs.StringVar(&in.Cursor, "cursor", "", "next_cursor of the last page")
		},
		Text: func(w io.Writer, out ops.ActivityOutput) error {
			p := &printer{w: w}
			for _, r := range out.Items {
				n := ""
				if r.Count > 1 {
					n = fmt.Sprintf(" x%d", r.Count)
				}
				p.f("%s\t%s\t%s%s\t%s\t%s\t%s\n", r.At, r.Who, r.Kind, n, r.Summary, r.Doc, r.Why)
			}
			if out.More {
				p.f("more\t--cursor %s\n", out.NextCursor)
			}
			return p.err
		},
	})
}
