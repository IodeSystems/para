package cli

import (
	"io"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/para/internal/ops"
)

// newReportCmd is `para report outcomes`.
func newReportCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "report", Short: "Reports over events", Args: cobra.NoArgs}
	cmd.AddCommand(opCommand(l, ops.ReportOutcomes, binding[ops.ReportOutcomesInput, ops.ReportOutcomesOutput]{
		Use:  "outcomes [--since <date>] [--until <date>]",
		Args: cobra.NoArgs,
		Flags: func(fs *pflag.FlagSet, in *ops.ReportOutcomesInput) {
			fs.StringVar(&in.Since, "since", "", "first day, YYYY-MM-DD; default 30 days before until")
			fs.StringVar(&in.Until, "until", "", "last day, YYYY-MM-DD; default today")
		},
		Text: func(w io.Writer, out ops.ReportOutcomesOutput) error {
			p := &printer{w: w}
			p.f("%s to %s\t%d task closes\n", out.Since, out.Until, out.Closes)
			for _, o := range out.Outcomes {
				if o.Count > 0 {
					p.f("outcome\t%s\t%d\n", o.Key, o.Count)
				}
			}
			for _, a := range out.ByActor {
				for _, o := range a.Outcomes {
					p.f("actor\t%s\t%s\t%d\n", a.Actor, o.Key, o.Count)
				}
			}
			for _, u := range out.Undos {
				p.f("undo\t%s\t%s\t%s\n", u.Receipt, u.Doc, u.By)
			}
			return p.err
		},
	}))
	return cmd
}
