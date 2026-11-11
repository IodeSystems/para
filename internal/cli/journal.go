package cli

import (
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/para/internal/ops"
)

func newJournalCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "journal", Short: "Journals by level (day, week, month): list, show, generate, summarize", Args: cobra.NoArgs}
	addJSONFlag(cmd)
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
	list := opCommand(l, ops.JournalList, binding[ops.JournalListInput, ops.JournalListOutput]{
		Use: "list", Args: cobra.NoArgs,
		Flags: func(fs *pflag.FlagSet, in *ops.JournalListInput) {
			fs.StringVar(&in.Level, "level", "", "day, week, or month; default all")
			fs.IntVar(&in.Limit, "limit", 0, "at most this many; default all")
		},
		Text: func(w io.Writer, out ops.JournalListOutput) error {
			p := &printer{w: w}
			for _, j := range out.Items {
				mark := "mechanical"
				if j.Summary {
					mark = "summary"
				}
				p.f("%s\t%s\t%s\t%s\n", j.Level, j.ID, j.Title, mark)
			}
			return p.err
		},
	})
	// `para journal --period <p> [date]` is the journal op; bare `para
	// journal` lists the journals and their levels.
	gen := opCommand(l, ops.Journal, binding[ops.JournalPeriodInput, ops.JournalOutput]{
		Flags: func(fs *pflag.FlagSet, in *ops.JournalPeriodInput) {
			fs.StringVar((*string)(&in.Period), "period", "", "generate the journal of a period: daily, weekly, or monthly")
		},
		Bind: func(args []string, in *ops.JournalPeriodInput) error {
			if len(args) > 0 {
				in.Date = args[0]
			}
			return nil
		},
		Text: text,
	})
	cmd.Annotations = gen.Annotations
	cmd.Use = "journal [--period daily|weekly|monthly [date]]"
	cmd.Args = cobra.MaximumNArgs(1)
	cmd.Flags().AddFlagSet(gen.Flags())
	cmd.Flags().AddFlagSet(list.Flags())
	cmd.RunE = func(c *cobra.Command, args []string) error {
		if c.Flags().Changed("period") {
			return gen.RunE(c, args)
		}
		if len(args) > 0 {
			return errors.New("a date goes with --period")
		}
		return list.RunE(c, args)
	}
	cmd.AddCommand(
		list,
		opCommand(l, ops.JournalShow, binding[ops.JournalShowInput, ops.JournalShowOutput]{
			Use: "show <period>", Args: cobra.ExactArgs(1),
			Bind: func(args []string, in *ops.JournalShowInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.JournalShowOutput) error {
				_, err := fmt.Fprintf(w, "%s\n\n%s\n", out.Path, out.Body)
				return err
			},
		}),
		opCommand(l, ops.JournalSummary, binding[ops.JournalSummaryInput, ops.JournalSummaryOutput]{
			Use: "summary <period>", Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.JournalSummaryInput) {
				fs.StringVar(&in.Text, "text", "", "the summary; default: standard input. Empty lists what to read and writes nothing")
			},
			Bind: func(args []string, in *ops.JournalSummaryInput) error { in.ID = args[0]; return nil },
			Stdin: func(r io.Reader, in *ops.JournalSummaryInput) (err error) {
				if in.Text == "" {
					in.Text, err = readPiped(r)
				}
				return err
			},
			Text: func(w io.Writer, out ops.JournalSummaryOutput) error {
				p := &printer{w: w}
				p.f("%s\t%s\t%s\n", out.Action, out.Level, out.Path)
				if out.Action == "read" {
					for _, s := range out.Sources {
						p.f("%s\t%s\t%s\t%s\t%s\n", s.Kind, s.ID, s.At, s.Op, s.Title)
					}
					if out.Omitted > 0 {
						p.f("%d more events omitted\n", out.Omitted)
					}
				}
				return p.err
			},
		}),
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
