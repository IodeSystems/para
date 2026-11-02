package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/iodesystems/parasolve/internal/ops"
)

func newJournalCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "journal", Short: "Generated journals", Args: cobra.NoArgs}
	text := func(w io.Writer, out ops.JournalOutput) error {
		_, err := fmt.Fprintf(w, "%s\t%s\t%d events\n", out.Action, out.Path, out.Events)
		return err
	}
	bind := func(args []string, in *ops.JournalInput) error {
		if len(args) > 0 {
			in.Date = args[0]
		}
		return nil
	}
	cmd.AddCommand(
		opCommand(l, ops.JournalDaily, binding[ops.JournalInput, ops.JournalOutput]{
			Use: "daily [date]", Args: cobra.MaximumNArgs(1), Bind: bind, Text: text,
		}),
		opCommand(l, ops.JournalWeekly, binding[ops.JournalInput, ops.JournalOutput]{
			Use: "weekly [date]", Args: cobra.MaximumNArgs(1), Bind: bind, Text: text,
		}),
		opCommand(l, ops.JournalMonthly, binding[ops.JournalInput, ops.JournalOutput]{
			Use: "monthly [date]", Args: cobra.MaximumNArgs(1), Bind: bind, Text: text,
		}),
	)
	return cmd
}
