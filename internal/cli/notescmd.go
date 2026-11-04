package cli

import (
	"io"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/para/internal/ops"
)

// newNotesCmd: the ## Notes section of any item, edited against the hash read.
func newNotesCmd(l *loader) *cobra.Command {
	cmd := group("notes", "Notes on any item: the ## Notes section, edited against the hash you read")
	var md string
	edit := func(op *ops.Op[ops.NotesEditInput, ops.NotesEditOutput], use string) *cobra.Command {
		return opCommand(l, op, binding[ops.NotesEditInput, ops.NotesEditOutput]{
			Use: use, Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.NotesEditInput) {
				fs.StringVar(&in.Hash, "hash", "", "the hash notes read returned; leave out only when the item has no notes")
				fs.StringVar(&md, "md", "", "the new notes (or on stdin); empty removes them")
			},
			Bind: func(args []string, in *ops.NotesEditInput) error { in.ID, in.MD = args[0], md; return nil },
			Stdin: func(r io.Reader, in *ops.NotesEditInput) error {
				if in.MD != "" {
					return nil
				}
				text, err := readStdin(r)
				in.MD = text
				return err
			},
			Text: printID(func(o ops.NotesEditOutput) string { return o.Hash }),
		})
	}
	cmd.AddCommand(
		opCommand(l, ops.NotesRead, binding[ops.NotesReadInput, ops.NotesReadOutput]{
			Use: "read <id>", Args: cobra.ExactArgs(1),
			Bind: func(args []string, in *ops.NotesReadInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.NotesReadOutput) error {
				p := &printer{w: w}
				p.f("hash: %s\n\n%s\n", dash(out.Hash), out.MD)
				return p.err
			},
		}),
		edit(ops.NotesEdit, "edit <id> [--hash <h>] [--md <text>] (or the notes on stdin)"),
		edit(ops.NotesSave, "save <id> [--hash <h>] [--md <text>] (or the notes on stdin)"),
	)
	return cmd
}
