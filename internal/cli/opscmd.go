package cli

import (
	"fmt"
	"io"
	"sort"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/para/internal/ops"
)

// newOpsCmd is `para ops`: facts about the ops themselves.
func newOpsCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "ops", Short: "Facts about the ops", Args: cobra.NoArgs}
	var unused bool
	stats := opCommand(l, ops.OpsStats, binding[ops.OpsStatsInput, ops.OpsStatsOutput]{
		Use:  "stats",
		Args: cobra.NoArgs,
		Flags: func(fs *pflag.FlagSet, in *ops.OpsStatsInput) {
			fs.StringVar(&in.Since, "since", "", "count only calls last made on or after this date (YYYY-MM-DD)")
			fs.StringVar(&in.Surface, "surface", "", "count only this surface: mcp-stdio, mcp-http, web, or cli")
			fs.BoolVar(&unused, "unused", false, "list only the ops with no calls (since --since)")
		},
		Text: func(w io.Writer, out ops.OpsStatsOutput) error {
			rows := out.Ops
			if unused {
				rows = nil
				for _, o := range out.Ops {
					if o.Calls == 0 {
						rows = append(rows, o)
					}
				}
				sort.Slice(rows, func(i, j int) bool { return rows[i].Op < rows[j].Op })
			}
			for _, o := range rows {
				if unused {
					if _, err := fmt.Fprintln(w, o.Op); err != nil {
						return err
					}
					continue
				}
				if _, err := fmt.Fprintf(w, "%d\t%s\t%s\t%v\n", o.Calls, o.Op, o.Last, o.Surfaces); err != nil {
					return err
				}
			}
			return nil
		},
	})
	stats.Short = "Count op calls on this machine by surface; --unused lists ops never called"
	cmd.AddCommand(stats)
	return cmd
}
