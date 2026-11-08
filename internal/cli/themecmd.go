package cli

import (
	"errors"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/para/internal/ops"
)

// newThemeCmd is `para theme items|close`: what hangs on a goal, an
// obligation, a question, or a #tag, and closing it with the owner's review
// of those items as one batch (para bulk undo <batch> takes it all back).
func newThemeCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "theme", Short: "What hangs on a goal, obligation, question, or #tag, and closing it"}
	cmd.AddCommand(opCommand(l, ops.ThemeItems, binding[ops.ThemeItemsInput, ops.ThemeItemsOutput]{
		Use:  "items <id|#tag>",
		Args: cobra.ExactArgs(1),
		Bind: func(args []string, in *ops.ThemeItemsInput) error { in.Theme = args[0]; return nil },
		Text: func(w io.Writer, out ops.ThemeItemsOutput) error {
			p := &printer{w: w}
			for _, it := range out.Items {
				p.f("%s\t%s\t%s\t%s\n", it.ID, it.Life, it.Title, it.WhyText)
			}
			p.f("%d items hang on %s\n", len(out.Items), out.Theme)
			return p.err
		},
	}))
	var drop, keep, moves []string
	cmd.AddCommand(opCommand(l, ops.ThemeClose, binding[ops.ThemeCloseInput, ops.ThemeCloseOutput]{
		Use:  "close <id|#tag> [--drop id,...] [--keep id,...] [--move id=area] [--outcome o] [--reason text]",
		Args: cobra.ExactArgs(1),
		Flags: func(fs *pflag.FlagSet, in *ops.ThemeCloseInput) {
			fs.StringSliceVar(&drop, "drop", nil, "ids of items to drop (para theme items lists them)")
			fs.StringSliceVar(&keep, "keep", nil, "ids of items to keep as they are")
			fs.StringArrayVar(&moves, "move", nil, "keep an item and move it to an area: id=area; repeatable")
			fs.StringVar(&in.Outcome, "outcome", "", "how the theme ended, as para close --outcome")
			fs.StringVar(&in.Reason, "reason", "", "why the theme ended, as para close --reason")
			fs.StringVar(&in.DropReason, "drop-reason", "", "kept on each dropped item; default \"<theme> ended\"")
		},
		Bind: func(args []string, in *ops.ThemeCloseInput) error {
			in.Theme, in.Drop = args[0], drop
			for _, id := range keep {
				in.Keep = append(in.Keep, ops.ThemeKeep{ID: id})
			}
			for _, m := range moves {
				id, area, ok := strings.Cut(m, "=")
				if !ok || id == "" || area == "" {
					return errors.New("--move takes id=area")
				}
				in.Keep = append(in.Keep, ops.ThemeKeep{ID: id, Area: area})
			}
			return nil
		},
		Text: func(w io.Writer, out ops.ThemeCloseOutput) error {
			p := &printer{w: w}
			for _, it := range out.Items {
				if it.Error != "" {
					p.f("%s\t%s\t%s\n", it.State, it.ID, it.Error)
				} else {
					p.f("%s\t%s\t%s\n", it.State, it.ID, it.Op)
				}
			}
			if out.Closed != "" {
				p.f("closed\t%s\t%s\n", out.Theme, out.Closed)
			}
			for _, id := range out.Left {
				p.f("left open\t%s\n", id)
			}
			if out.Batch != "" {
				p.f("batch\t%s\t(para bulk undo %s)\n", out.Batch, out.Batch)
			}
			return p.err
		},
	}))
	return cmd
}
