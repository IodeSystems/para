package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/iodesystems/para/internal/ops"
)

// tickAllOutput is the output of para tick --all.
type tickAllOutput struct {
	Vaults []ops.TickOutput `json:"vaults"`
}

// Failed makes the command exit non-zero when any vault could not be ticked or
// names no runner.
func (o tickAllOutput) Failed() error {
	n := 0
	for _, v := range o.Vaults {
		if v.Failed() != nil {
			n++
		}
	}
	if n > 0 {
		return fmt.Errorf("tick: %d vaults failed", n)
	}
	return nil
}

func tickLine(v ops.TickOutput) string {
	switch {
	case v.Error != "":
		return fmt.Sprintf("%s\tfailed: %s", v.Vault, v.Error)
	case !v.Ran:
		return fmt.Sprintf("%s\tskipped: %s", v.Vault, v.Skipped)
	}
	line := fmt.Sprintf("%s\tran\trevived %d\trequests %d\tjournals %d\tnudges %d\tchanges %d", v.Vault, len(v.Revived), len(v.Requests), len(v.Journals), len(v.Nudges), v.Changes)
	if len(v.Goals) > 0 {
		line += fmt.Sprintf("\tgoals %d", len(v.Goals))
	}
	if v.Proposed > 0 {
		line += fmt.Sprintf("\t%d proposed: add tick to request.surfaces in .para/vault.properties to queue them, or run para request accept", v.Proposed)
	}
	return line
}

func writeTick(w io.Writer, vs ...ops.TickOutput) error {
	p := &printer{w: w}
	for _, v := range vs {
		p.f("%s\n", tickLine(v))
		for _, wn := range v.Warnings {
			p.f("warning: %s\n", wn)
		}
	}
	return p.err
}

func newTickCmd(l *loader) *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "tick [--all]",
		Short: "One scheduled pass: recurring work, agent work, follow-ups, reconcile. Only on the vault's named runner",
		Long: "One pass over a vault, safe to run twice: it makes the next occurrence of a\n" +
			"recurring task that was marked done by hand, queues a request for a due\n" +
			"recurring task assigned to agent:<name> that has none, names the nudges that\n" +
			"are due, and reconciles. It runs only on the install named by\n" +
			"request.runner in .para/vault.properties, because a synced vault lives on\n" +
			"more than one machine. Run it from cron, or let para serve run it on a timer\n" +
			"(tick.interval in server.properties). Requests made by tick are queued only\n" +
			"when request.surfaces lists tick; otherwise they wait for para request accept.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := l.load(); err != nil {
				return err
			}
			if !all {
				deps, err := l.deps()
				if err != nil {
					return err
				}
				out, err := ops.Tick(cmd.Context(), deps, ops.TickInput{})
				if err != nil {
					return err
				}
				return emit(cmd, out, func(w io.Writer, o ops.TickOutput) error { return writeTick(w, o) })
			}
			entries, err := l.install.Entries()
			if err != nil {
				return err
			}
			var res tickAllOutput
			res.Vaults = []ops.TickOutput{}
			for _, e := range entries {
				v := ops.TickOutput{Vault: e.ID, Revived: []string{}, Requests: []string{}, Journals: []string{}, Nudges: []string{}, Warnings: []string{}}
				if e.Err != "" {
					v.Error = "registry entry is broken: " + e.Err
				} else if deps, err := l.depsFor(e.ID, e.Path); err != nil {
					v.Error = err.Error()
				} else if out, err := ops.Tick(cmd.Context(), deps, ops.TickInput{}); err != nil {
					v.Error = strings.TrimSpace(err.Error())
				} else {
					v = out
				}
				res.Vaults = append(res.Vaults, v)
			}
			return emit(cmd, res, func(w io.Writer, o tickAllOutput) error { return writeTick(w, o.Vaults...) })
		},
	}
	addJSONFlag(cmd)
	cmd.Flags().BoolVar(&all, "all", false, "tick every vault of this install")
	return cmd
}
