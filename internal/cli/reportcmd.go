package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/para/internal/export"
	"github.com/iodesystems/para/internal/ops"
)

// newReportCmd is `para report friction` and `para report outcomes`. The op builds the report and logs
// the egress; this command runs the transport, as export does.
func newReportCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "report", Short: "Reports over Asks and events", Args: cobra.NoArgs}
	var to string
	var friction *cobra.Command
	friction = opCommand(l, ops.ReportFriction, binding[ops.FrictionInput, ops.FrictionOutput]{
		Use:  "friction [--since <date>] [--by domain|policy|reason] [--to <transport>]",
		Args: cobra.NoArgs,
		Flags: func(fs *pflag.FlagSet, in *ops.FrictionInput) {
			fs.StringVar(&in.Since, "since", "", "only Asks created from this day, YYYY-MM-DD")
			fs.StringVar(&in.By, "by", "reason", "group by domain, policy, or reason (there is no per-person view)")
			fs.StringVar(&to, "to", "stdout", "stdout, file:<path>, clipboard, osc52, tmux, wormhole, qr, or ssh:<host>:<path>")
		},
		Bind: func(_ []string, in *ops.FrictionInput) error { in.To = to; return nil },
		Text: func(w io.Writer, out ops.FrictionOutput) error {
			if out.To == "" {
				_, err := io.WriteString(w, out.Text)
				return err
			}
			target, err := export.ParseTarget(out.To)
			if err != nil {
				return err
			}
			errw := friction.ErrOrStderr()
			detail, err := export.Send(friction.Context(), target, out.Rendered(), "friction-report", export.IO{
				Stdout: w, Stderr: errw, Terminal: openTerminal,
			})
			if err != nil {
				return err
			}
			if detail != "" {
				_, err = fmt.Fprintln(errw, detail)
			}
			return err
		},
	})
	cmd.AddCommand(friction, opCommand(l, ops.ReportOutcomes, binding[ops.ReportOutcomesInput, ops.ReportOutcomesOutput]{
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
