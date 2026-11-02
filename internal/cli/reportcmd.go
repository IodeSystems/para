package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/parasolve/internal/export"
	"github.com/iodesystems/parasolve/internal/ops"
)

// newReportCmd is `para report friction`. The op builds the report and logs
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
	cmd.AddCommand(friction)
	return cmd
}
