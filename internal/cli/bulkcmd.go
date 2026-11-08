package cli

import (
	"errors"
	"io"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/para/internal/ops"
)

// newBulkCmd is `para bulk <verb> [id]...`: one decision on many items, each
// by the verb's own op, with one batch to undo. It is its own command, not
// more ids on close, later, and reopen: those make one write that is refused
// whole, and bulk reports per item and keeps going.
func newBulkCmd(l *loader) *cobra.Command {
	var fields []string
	return opCommand(l, ops.Bulk, binding[ops.BulkInput, ops.BulkOutput]{
		Use:  "bulk <close|later|reopen|optional> [id]... [--tag t] [--scope id] [--dry-run], or bulk undo <batch>",
		Args: cobra.MinimumNArgs(1),
		Flags: func(fs *pflag.FlagSet, in *ops.BulkInput) {
			fs.StringVar(&in.Tag, "tag", "", "a topic: every tagged open item (reopen: closed or put off)")
			fs.StringVar(&in.Scope, "scope", "", "a goal or an area: the items in it")
			fs.BoolVar(&in.DryRun, "dry-run", false, "say what each item would do; write nothing")
			fs.StringVar(&in.Reason, "reason", "", "why, as the verb keeps it")
			fs.StringVar(&in.Outcome, "outcome", "", "close: how it ended, as para close --outcome")
			fs.StringArrayVar(&fields, "field", nil, "close: an input of the type's close op, key=value; repeatable")
			fs.StringVar(&in.Until, "until", "", "later: YYYY-MM-DD it comes back; leave out for someday")
			fs.StringVar(&in.After, "after", "", "later: id of an item to wait for")
			fs.StringVar(&in.When, "when", "", "later: a condition in words")
			fs.StringVar(&in.Place, "place", "", "later: goal or area of the task --when makes")
			fs.StringVar(&in.On, "on", "", "optional: the item each dependency points at")
		},
		Bind: func(args []string, in *ops.BulkInput) error {
			in.Verb = args[0]
			if in.Verb == ops.BulkUndo {
				if len(args) != 2 {
					return errors.New("bulk undo takes one batch id")
				}
				in.Batch = args[1]
			} else {
				in.IDs = args[1:]
			}
			var err error
			in.Fields, err = fieldFlags(fields)
			return err
		},
		Text: func(w io.Writer, out ops.BulkOutput) error {
			p := &printer{w: w}
			for _, it := range out.Items {
				switch {
				case it.Error != "":
					p.f("%s\t%s\t%s\n", it.State, it.ID, it.Error)
				case it.Receipt != "":
					p.f("%s\t%s\t%s\treceipt %s\n", it.State, it.ID, it.Op, it.Receipt)
				default:
					p.f("%s\t%s\t%s\n", it.State, it.ID, it.Op)
				}
			}
			p.f("%d applied, %d refused\n", out.Applied, out.Refused)
			if out.Batch != "" && out.Verb != ops.BulkUndo {
				p.f("batch\t%s\t(para bulk undo %s)\n", out.Batch, out.Batch)
			}
			return p.err
		},
	})
}
