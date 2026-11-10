package cli

import (
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
)

// threadTarget puts a thread argument in key or id: an item id goes in id,
// a #tag or channel key in key.
func threadTarget(arg string, key, id *string) {
	if kind, _, err := model.ParseThreadKey(strings.TrimSpace(arg)); err == nil && kind == model.ThreadItem {
		*id = arg
		return
	}
	*key = arg
}

// newThreadCmd builds `para thread`: the conversation about an item, a tag,
// or a channel.
func newThreadCmd(l *loader) *cobra.Command {
	var atts []string
	cmd := opCommand(l, ops.Thread, binding[ops.ThreadInput, ops.ThreadOutput]{
		Use:  "thread <id|#tag|channel:<source>/<thread>>",
		Args: cobra.ExactArgs(1),
		Bind: func(args []string, in *ops.ThreadInput) error {
			threadTarget(args[0], &in.Key, &in.ID)
			return nil
		},
		Text: func(w io.Writer, out ops.ThreadOutput) error {
			p := &printer{w: w}
			p.f("%s\t%s\tqueued %d\tclaimed %d\n", out.Key, out.Kind, out.Queued, out.Claimed)
			if out.WaitingNoAgent {
				p.f("note: turns are queued and no agent is connected on this machine\n")
			}
			for _, t := range out.Turns {
				switch t.Kind {
				case ops.ThreadTurnIO:
					p.f("%s\t%s %s\t%s\t%s\n", t.At, t.Direction, t.Medium, t.IO, firstLine(firstNonEmpty(t.Subject, t.Title)))
				default:
					p.f("%s\towner %s\t%s\t%s\t%s\n", t.At, t.TurnKind, t.Request, t.Status, firstLine(t.OwnerWords))
					if t.ClaimedBy != "" {
						p.f("\t%s has it since %s\n", t.ClaimedBy, t.ClaimedAt)
					}
					if t.Reply != "" {
						p.f("\treply: %s\n", firstLine(t.Reply))
					}
					if t.Error != "" {
						p.f("\terror: %s\n", firstLine(t.Error))
					}
				}
				for _, r := range t.Receipts {
					p.f("\t%s\t%s\t%s\n", r.ID, r.Op, r.Doc)
				}
			}
			return p.err
		},
	})
	cmd.AddCommand(
		opCommand(l, ops.ThreadPost, binding[ops.ThreadPostInput, ops.ThreadPostOutput]{
			Use:  "post <id|#tag|channel:<source>/<thread>> [text] (or the text on stdin)",
			Args: cobra.MinimumNArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.ThreadPostInput) {
				fs.StringVar((*string)(&in.Kind), "kind", "", "modify, ask, redirect, or other (default other)")
				fs.StringVar(&in.Quote, "quote", "", "the text the turn is about, quoted from the item")
				fs.StringArrayVar(&atts, "attach", nil, "vault path of an attachment (para attachment add); repeat for more")
			},
			Bind: func(args []string, in *ops.ThreadPostInput) error {
				threadTarget(args[0], &in.Key, &in.ID)
				in.Text, in.Attachments = strings.Join(args[1:], " "), atts
				return nil
			},
			Stdin: func(r io.Reader, in *ops.ThreadPostInput) error {
				if in.Text != "" || len(in.Attachments) > 0 {
					return nil
				}
				text, err := readStdin(r)
				in.Text = text
				return err
			},
			Text: func(w io.Writer, out ops.ThreadPostOutput) error {
				p := &printer{w: w}
				p.f("%s\t%s\t%s\t%s\n", out.Request, out.Status, out.Thread, out.Title)
				if out.Note != "" {
					p.f("note: %s\n", out.Note)
				}
				return p.err
			},
		}),
	)
	return cmd
}
