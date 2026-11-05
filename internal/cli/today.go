package cli

import (
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/iodesystems/para/internal/today"
)

// newTodayCmd is CLI-only on purpose. It reads every vault of the install,
// so it is not an op: ops are what an agent surface can reach, and an agent
// bound to one vault must not see another's titles. A test enforces this.
func newTodayCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{
		Use:    "today",
		Short:  "Alias of `para agenda`: what needs you in value order across all vaults (CLI only)",
		Args:   cobra.NoArgs,
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAgendaHome(l, cmd)
		},
	}
	addJSONFlag(cmd)
	return cmd
}

// runAgendaHome prints the home view: what needs you in value order
// (actionable now: incidents, due soon by date, the rest by impact; then not
// yet startable, by start; see ops.RankValue), plus due, expiring,
// blocked, unverified, and inbox counts across all vaults. `para agenda` runs
// it without a date flag.
func runAgendaHome(l *loader, cmd *cobra.Command) error {
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
			if _, err := fmt.Fprintf(w, "%s\tincidents %d\tagenda %d\tother questions %d\tlater %d\tdue %d\toverdue %d\tsoon %d\tnudges %d\texpiring %d\tblocked %d\twaiting %d\tunverified %d\tanswers to check %d\thealth %d\tupgrade steps %d\tinbox %d\toutbox %d\tdocs unseen %d\tdoc comments %d\n",
				v.Vault, v.Incidents.Count, v.Agenda.Count, v.OtherOpen, v.Deferred, v.Due.Count, v.Overdue.Count, v.DueSoon.Count, v.Nudges.Count, v.Expiring.Count, v.Blocked.Count, v.Waiting, v.Unverified, v.AnswersToCheck, v.Health, v.UpgradeSteps, v.Inbox, v.Outbox, v.DocsUnseen, v.DocComments); err != nil {
				return err
			}
			p := &printer{w: w}
			printRows(p, "  ", v.OrderSummary, len(v.Agenda.Items), func(i int) (group, kind, id, why, title string) {
				it := v.Agenda.Items[i]
				return it.Group, it.Kind, it.ID, it.Why, it.Title
			})
			if p.err != nil {
				return p.err
			}
			if v.OtherOpen > 0 {
				if _, err := fmt.Fprintf(w, "  %d other open questions: para question list\n", v.OtherOpen); err != nil {
					return err
				}
			}
			for _, it := range v.Optional.Items {
				if _, err := fmt.Fprintf(w, "  optional meeting\t%s\t%s\t%s\n", it.ID, localStamp(it.At), it.Title); err != nil {
					return err
				}
			}
			if v.Skipped.Count > 0 {
				if _, err := fmt.Fprintf(w, "  %d meetings skipped: para agenda --by-date\n", v.Skipped.Count); err != nil {
					return err
				}
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
}
