package cli

import (
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/iodesystems/parasolve/internal/today"
)

// newTodayCmd is CLI-only on purpose. It reads every vault of the install,
// so it is not an op: ops are what an agent surface can reach, and an agent
// bound to one vault must not see another's titles. A test enforces this.
func newTodayCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "today",
		Short: "Overdue and due-soon tasks, due, expiring, blocked, unverified, and inbox counts across all vaults (CLI only)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := l.load(); err != nil {
				return err
			}
			rep, err := today.Read(cmd.Context(), l.install, cliActor, cliSurface, time.Now().Format("2006-01-02"))
			if err != nil {
				return err
			}
			return emit(cmd, rep, func(w io.Writer, r today.Report) error {
				for _, v := range r.Vaults {
					if v.Error != "" {
						if _, err := fmt.Fprintf(w, "%s\terror: %s\n", v.Vault, v.Error); err != nil {
							return err
						}
						continue
					}
					if _, err := fmt.Fprintf(w, "%s\tdue %d\toverdue %d\tsoon %d\tnudges %d\texpiring %d\tblocked %d\tunverified %d\tinbox %d\n",
						v.Vault, v.Due.Count, v.Overdue.Count, v.DueSoon.Count, v.Nudges.Count, v.Expiring.Count, v.Blocked.Count, v.Unverified, v.Inbox); err != nil {
						return err
					}
					for _, d := range v.LostUpdates {
						if _, err := fmt.Fprintf(w, "  lost-update\t%s\n", d); err != nil {
							return err
						}
					}
					for _, c := range v.SyncConflicts {
						if _, err := fmt.Fprintf(w, "  sync-conflict\t%s\n", c); err != nil {
							return err
						}
					}
					for _, g := range []struct {
						name string
						g    today.Group
					}{{"overdue", v.Overdue}, {"soon", v.DueSoon}, {"due", v.Due}, {"nudge", v.Nudges}, {"expiring", v.Expiring}, {"blocked", v.Blocked}} {
						for _, it := range g.g.Items {
							if _, err := fmt.Fprintf(w, "  %s\t%s\t%s\t%s\n", g.name, it.ID, dash(it.Due), it.Title); err != nil {
								return err
							}
						}
					}
				}
				return nil
			})
		},
	}
	addJSONFlag(cmd)
	return cmd
}
