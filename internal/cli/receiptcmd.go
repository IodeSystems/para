package cli

import (
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
)

// newReceiptCmd builds `para receipt`: what agents wrote, and why.
func newReceiptCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "receipt", Short: "What agents wrote, why, and how to verify it", Args: cobra.NoArgs}
	cmd.AddCommand(
		opCommand(l, ops.ReceiptList, binding[ops.ReceiptListInput, ops.ReceiptListOutput]{
			Use:  "list",
			Args: cobra.NoArgs,
			Flags: func(fs *pflag.FlagSet, in *ops.ReceiptListInput) {
				fs.StringVar(&in.Doc, "doc", "", "only writes to this document")
				fs.StringVar(&in.Request, "request", "", "only writes for this agent work")
				fs.StringVar(&in.Actor, "actor", "", "only this agent")
				fs.StringVar((*string)(&in.Check), "check", "", "only unchecked, verified, or wrong")
				fs.StringVar(&in.Op, "op", "", "only writes of this op")
				fs.StringVar(&in.Since, "since", "", "only writes on or after this day, YYYY-MM-DD")
				fs.StringVar(&in.Until, "until", "", "only writes on or before this day, YYYY-MM-DD")
				fs.IntVar(&in.Limit, "limit", 0, "most to list; 0 means all")
			},
			Text: func(w io.Writer, out ops.ReceiptListOutput) error {
				p := &printer{w: w}
				for _, r := range out.Items {
					p.f("%s\t%s\t%s\t%s\t%s\t%s\n", r.ID, r.Check, r.Actor, r.Op, r.Doc, r.Why)
				}
				return p.err
			},
		}),
		opCommand(l, ops.ReceiptShow, binding[ops.ReceiptIDInput, ops.ReceiptDetail]{
			Use:  "show <id>",
			Args: cobra.ExactArgs(1),
			Bind: func(args []string, in *ops.ReceiptIDInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.ReceiptDetail) error {
				p := &printer{w: w}
				r := out.Receipt
				p.f("%s\t%s\nactor\t%s\nop\t%s\ndoc\t%s\nwhy\t%s\nsources\t%s\nverify\t%s\ncheck\t%s\n", r.ID, r.Title, r.Actor, r.Op, r.Doc, r.Why, strings.Join(refLines(r), ", "), r.Verify, r.Check)
				for _, l := range out.Lines {
					p.f("%s\n", l)
				}
				return p.err
			},
		}),
		opCommand(l, ops.ReceiptCheck, binding[ops.ReceiptCheckInput, ops.ReceiptCheckOutput]{
			Use:  "check <id> verified|wrong [what is wrong]",
			Args: cobra.MinimumNArgs(2),
			Bind: func(args []string, in *ops.ReceiptCheckInput) error {
				in.ID, in.Verdict, in.Note = args[0], model.ReceiptCheck(args[1]), strings.Join(args[2:], " ")
				return nil
			},
			Text: func(w io.Writer, out ops.ReceiptCheckOutput) error {
				p := &printer{w: w}
				p.f("%s\t%s\n", out.ID, out.Check)
				if out.Request != "" {
					p.f("request\t%s\n", out.Request)
				}
				return p.err
			},
		}),
		opCommand(l, ops.ReceiptUndo, binding[ops.ReceiptUndoInput, ops.ReceiptUndoOutput]{
			Use:  "undo <id>...",
			Args: cobra.MinimumNArgs(1),
			Bind: func(args []string, in *ops.ReceiptUndoInput) error { in.IDs = args; return nil },
			Text: func(w io.Writer, out ops.ReceiptUndoOutput) error {
				p := &printer{w: w}
				for _, u := range out.Undone {
					p.f("%s\tundone\t%s\n", u.Doc, u.Receipt)
				}
				return p.err
			},
		}),
		opCommand(l, ops.ReceiptRevert, binding[ops.ReceiptIDInput, ops.ReceiptUndoOutput]{
			Use:  "revert <id>",
			Args: cobra.ExactArgs(1),
			Bind: func(args []string, in *ops.ReceiptIDInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.ReceiptUndoOutput) error {
				p := &printer{w: w}
				for _, u := range out.Undone {
					p.f("%s\tundone\t%s\n", u.Doc, u.Receipt)
				}
				return p.err
			},
		}),
	)
	return cmd
}

// newActivityCmd is `para agent activity`: what each agent did.
func newActivityCmd(l *loader) *cobra.Command {
	return opCommand(l, ops.AgentActivity, binding[ops.AgentActivityInput, ops.AgentActivityOutput]{
		Use:  "activity",
		Args: cobra.NoArgs,
		Flags: func(fs *pflag.FlagSet, in *ops.AgentActivityInput) {
			fs.StringVar(&in.Actor, "actor", "", "only this agent")
			fs.StringVar(&in.Since, "since", "", "from this date or time")
			fs.IntVar(&in.Limit, "limit", 0, "most to list; default 200")
		},
		Text: func(w io.Writer, out ops.AgentActivityOutput) error {
			p := &printer{w: w}
			for _, r := range out.Items {
				mark := ""
				if r.SelfReported {
					mark = "\tself-reported"
				}
				p.f("%s\t%s\t%s\t%s\t%s\t%s%s\n", r.TS.Format("2006-01-02T15:04:05Z07:00"), r.Actor, r.Op, r.Doc, r.Request, r.Receipt, mark)
			}
			return p.err
		},
	})
}

// newHistoryCmd is `para history <id>`: the timeline of an item.
func newHistoryCmd(l *loader) *cobra.Command {
	return opCommand(l, ops.History, binding[ops.HistoryInput, ops.HistoryOutput]{
		Use:  "history <id>",
		Args: cobra.ExactArgs(1),
		Bind: func(args []string, in *ops.HistoryInput) error { in.ID = args[0]; return nil },
		Flags: func(fs *pflag.FlagSet, in *ops.HistoryInput) {
			fs.StringVar((*string)(&in.Filter), "filter", "", "all, or conversation: notes, messages, agent results, and what the owner asked")
			fs.IntVar(&in.Limit, "limit", 0, "most rows; default 50")
			fs.StringVar(&in.Before, "before", "", "only rows before this RFC 3339 time (next_before of the last page)")
		},
		Text: func(w io.Writer, out ops.HistoryOutput) error {
			p := &printer{w: w}
			for _, r := range out.Items {
				at := r.At
				if r.DateOnly && len(at) >= 10 {
					at = at[:10]
				}
				p.f("%s\t%s\t%s\t%s\t%s\n", at, r.Who, r.Kind, r.Summary, r.Ref)
			}
			if out.More {
				p.f("more\t--before %s\n", out.NextBefore)
			}
			return p.err
		},
	})
}

// newContextCmd is `para context <id>`: what an item is for.
func newContextCmd(l *loader) *cobra.Command {
	return opCommand(l, ops.ItemContext, binding[ops.ItemContextInput, ops.ItemContextOutput]{
		Use:  "context <id>",
		Args: cobra.ExactArgs(1),
		Bind: func(args []string, in *ops.ItemContextInput) error { in.ID = args[0]; return nil },
		Text: func(w io.Writer, out ops.ItemContextOutput) error {
			p := &printer{w: w}
			for _, c := range out.CameFrom {
				p.f("came from\t%s\t%s\t%s\n", c.Text, c.ID, c.URL)
			}
			for _, f := range out.For {
				p.f("for\t%s\t%s\t%s\n", f.Role, f.Name, f.ID)
			}
			for _, r := range out.Reasons {
				p.f("why\t%s\t%s\n", r.Kind, r.Text)
			}
			if out.Looking != "" {
				p.f("looking\t%s\n", out.Looking)
			}
			return p.err
		},
	})
}

// refLines prints the sources of a receipt resolved: the address after each
// ref that has one, repeats dropped. Falls back to the stored strings.
func refLines(r ops.ReceiptView) []string {
	if len(r.SourceLinks) == 0 {
		return r.Sources
	}
	out := make([]string, 0, len(r.SourceLinks))
	for _, l := range r.SourceLinks {
		out = append(out, l.Line())
	}
	return out
}
