package cli

import (
	"io"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/para/internal/ops"
)

// newTriageCmd is `para triage run [ids...] [--dry-run]`: apply the triage
// rules of .para/triage.yaml to open incoming io (op triage_run).
func newTriageCmd(l *loader) *cobra.Command {
	cmd := group("triage", "Triage rules (.para/triage.yaml) on incoming io: needs_you, digest, link, tag, mute")
	cmd.AddCommand(opCommand(l, ops.TriageRun, binding[ops.TriageRunInput, ops.TriageRunOutput]{
		Use:  "run [id...]",
		Args: cobra.ArbitraryArgs,
		Flags: func(fs *pflag.FlagSet, in *ops.TriageRunInput) {
			fs.BoolVar(&in.DryRun, "dry-run", false, "list what the rules would do; change nothing")
		},
		Bind: func(args []string, in *ops.TriageRunInput) error {
			in.IDs = args
			return nil
		},
		Text: func(w io.Writer, out ops.TriageRunOutput) error {
			p := &printer{w: w}
			for _, a := range out.Acts {
				what := firstNonEmpty(a.Receipt, "dry run")
				if a.Skipped != "" {
					what = "skipped: " + a.Skipped
				}
				p.f("%s\trule %d\t%s\t%s\n", a.ID, a.Rule, a.Action, what)
			}
			p.f("%d rules, %d open incoming checked, %d acts\n", out.Rules, out.Checked, len(out.Acts))
			return p.err
		},
	}))
	return cmd
}
