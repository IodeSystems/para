package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/para/internal/export"
	"github.com/iodesystems/para/internal/ops"
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
	var byDate bool
	cmd := opCommand(l, ops.Agenda, binding[ops.AgendaInput, ops.AgendaOutput]{
		Use:  "agenda [--days n]",
		Args: cobra.NoArgs,
		Flags: func(fs *pflag.FlagSet, in *ops.AgendaInput) {
			fs.IntVar(&in.Days, "days", 0, "days from today, default 7")
			fs.StringVar(&in.Zone, "zone", "", "IANA zone to show times in, such as Europe/Berlin; default this machine's")
		},
		Text: writeAgenda,
	})
	cmd.Flags().BoolVar(&byDate, "by-date", false, "group by day: dated items of the next days, one vault (also set by --days or --zone)")
	cmd.Short = "What needs you in value order across all vaults; with --by-date, --days, or --zone, the dated items of one vault by day"
	dated := cmd.RunE
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if byDate || cmd.Flags().Changed("days") || cmd.Flags().Changed("zone") {
			return dated(cmd, args)
		}
		return runAgendaHome(l, cmd)
	}
	return cmd
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
	cmd.AddCommand(exp, newCalendarCompareCmd(l))
	return cmd
}

// newCalendarCompareCmd is `para calendar compare --ics <file>`: the CLI reads
// the local file and passes its text to the op.
func newCalendarCompareCmd(l *loader) *cobra.Command {
	var icsPath string
	return opCommand(l, ops.CalendarCompare, binding[ops.CalendarCompareInput, ops.CalendarCompareOutput]{
		Use:  "compare --ics <file|-> [--from date] [--to date]",
		Args: cobra.NoArgs,
		Flags: func(fs *pflag.FlagSet, in *ops.CalendarCompareInput) {
			fs.StringVar(&icsPath, "ics", "", "an .ics file exported from the calendar; - reads stdin")
			fs.StringVar(&in.From, "from", "", "start of the window: a date or an RFC 3339 time; default today")
			fs.StringVar(&in.To, "to", "", "end of the window: a date (included) or an RFC 3339 time; default 14 days after from")
			fs.StringVar(&in.Zone, "zone", "", "IANA zone for dates and floating times; default this machine's")
			fs.IntVar(&in.ToleranceMinutes, "tolerance", 0, "minutes two starts may differ and still match; default 15")
		},
		Stdin: func(r io.Reader, in *ops.CalendarCompareInput) (err error) {
			switch icsPath {
			case "":
				return errors.New("--ics is required: an .ics file, or - for stdin")
			case "-":
				in.ICS, err = readStdin(r)
				return err
			}
			b, err := os.ReadFile(icsPath)
			if err != nil {
				return fmt.Errorf("calendar: compare: %w", err)
			}
			in.ICS = string(b)
			return nil
		},
		Text: func(w io.Writer, out ops.CalendarCompareOutput) error {
			p := &printer{w: w}
			p.f("%s to %s: calendar %d, para %d: %d matched, %d calendar only, %d para only, %d mismatched\n",
				out.From, out.To, out.Calendar, out.Para, len(out.Matched), len(out.CalendarOnly), len(out.ParaOnly), len(out.Mismatched))
			for _, c := range out.CalendarOnly {
				p.f("calendar only\t%s\t%s\t%s", c.Start, c.Title, dashIfEmpty(c.UID))
				if c.SeriesInPara != "" {
					p.f("\tseries in para: %s", c.SeriesInPara)
				}
				if len(c.ParaMaybe) > 0 {
					p.f("\tmaybe: %s", strings.Join(c.ParaMaybe, ","))
				}
				p.f("\n")
			}
			for _, x := range out.ParaOnly {
				p.f("para only\t%s\t%s\t%s\t%s\n", x.Start, x.ID, x.Title, x.Reason)
			}
			for _, m := range out.Mismatched {
				p.f("mismatch %s\t%s\tpara %s\tcalendar %s\t%s\n", m.What, m.ID, m.ParaStart, m.CalendarStart, m.Title)
			}
			for _, m := range out.Matched {
				p.f("matched\t%s\t%s\t%s\n", m.Start, m.ID, m.Title)
			}
			for _, u := range out.Unexpanded {
				p.f("unexpanded\t%s\t%s\t%s\n", u.Title, u.Rule, u.Why)
			}
			for _, wn := range out.Warnings {
				p.f("warning\t%s\n", wn)
			}
			return p.err
		},
	})
}

// calendarDocName names the temp files of a transport.
const calendarDocName = "calendar"
