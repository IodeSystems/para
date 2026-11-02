package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/parasolve/internal/export"
	"github.com/iodesystems/parasolve/internal/ops"
)

// agendaLine is one item as text: the time or kind, the title, who, and the id.
func agendaLine(it ops.AgendaItem) string {
	when := string(it.Kind)
	if it.Kind == ops.AgendaMeeting {
		until := ""
		if t, err := time.Parse(time.RFC3339, it.Until); err == nil {
			until = "-" + t.Format("15:04")
			if s, err := time.Parse(time.RFC3339, it.At); err == nil && t.Format("2006-01-02") != s.Format("2006-01-02") {
				until += "+1d"
			}
		}
		when = it.Time + until
	}
	line := "  " + when + "\t" + it.Title
	if names := joinNonEmpty(it.WhoNames); names != "" {
		line += "  with " + names
	}
	if it.ProjectName != "" {
		line += "  " + it.ProjectName
	}
	if it.Overdue {
		line += "  overdue"
	}
	if it.Detail != "" && it.Kind != ops.AgendaMeeting {
		line += "  " + it.Detail
	}
	return line + "\t" + it.ID
}

func joinNonEmpty(ss []string) string {
	var out []string
	for _, s := range ss {
		if s != "" {
			out = append(out, s)
		}
	}
	return strings.Join(out, ", ")
}

func writeAgenda(w io.Writer, out ops.AgendaOutput) error {
	p := &printer{w: w}
	p.f("agenda %s to %s (%s)\n", out.From, out.To, out.Zone)
	day := ""
	for _, it := range out.Items {
		if it.Date != day {
			day = it.Date
			name := day
			if t, err := time.Parse("2006-01-02", day); err == nil {
				name += " " + t.Weekday().String()[:3]
			}
			p.f("%s\n", name)
		}
		p.f("%s\n", agendaLine(it))
	}
	if len(out.Items) == 0 {
		p.f("nothing dated\n")
	}
	return p.err
}

func newAgendaCmd(l *loader) *cobra.Command {
	return opCommand(l, ops.Agenda, binding[ops.AgendaInput, ops.AgendaOutput]{
		Use:  "agenda [--days n]",
		Args: cobra.NoArgs,
		Flags: func(fs *pflag.FlagSet, in *ops.AgendaInput) {
			fs.IntVar(&in.Days, "days", 0, "days from today, default 7")
			fs.StringVar(&in.Zone, "zone", "", "IANA zone to show times in, such as Europe/Berlin; default this machine's")
		},
		Text: writeAgenda,
	})
}

// newCalendarCmd is `para calendar export`. The op renders and logs; this
// command runs the transport, as `para export` does.
func newCalendarCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "calendar", Short: "Calendar feed of meetings and due tasks (para never writes an external calendar)", Args: cobra.NoArgs}
	var cmdRef *cobra.Command
	exp := opCommand(l, ops.CalendarExport, binding[ops.CalendarExportInput, ops.CalendarExportOutput]{
		Use:  "export [--days n] [--to stdout|file:<path>|clipboard|osc52|tmux|wormhole|qr|ssh:<host>:<path>]",
		Args: cobra.NoArgs,
		Flags: func(fs *pflag.FlagSet, in *ops.CalendarExportInput) {
			fs.IntVar(&in.Days, "days", 0, "days from today, default 30")
			fs.StringVar(&in.To, "to", "stdout", "stdout, file:<path>, clipboard, osc52, tmux, wormhole, qr, or ssh:<host>:<path>")
			fs.StringVar(&in.Zone, "zone", "", "IANA zone whose midnight starts the window; default this machine's")
		},
		Text: func(w io.Writer, out ops.CalendarExportOutput) error {
			target, err := export.ParseTarget(out.To)
			if err != nil {
				return err
			}
			errw := cmdRef.ErrOrStderr()
			detail, err := export.Send(cmdRef.Context(), target, out.Rendered(), calendarDocName, export.IO{
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
	run := exp.RunE
	exp.RunE = func(c *cobra.Command, args []string) error {
		cmdRef = c
		if j, _ := c.Flags().GetBool("json"); j {
			if to, _ := c.Flags().GetString("to"); to != "stdout" && to != "" {
				return errors.New("--to and --json cannot be combined: JSON output is not sent to a transport")
			}
		}
		return run(c, args)
	}
	cmd.AddCommand(exp)
	return cmd
}

// calendarDocName names the temp files of a transport.
const calendarDocName = "calendar"
