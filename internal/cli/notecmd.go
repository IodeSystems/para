package cli

import (
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/para/internal/ops"
)

func newNoteCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "note", Short: "Notes: reference material in resources/", Args: cobra.NoArgs}
	var title, body string
	var tags []string
	cmd.AddCommand(
		opCommand(l, ops.NoteEdit, binding[ops.NoteEditInput, ops.NoteEditOutput]{
			Use:  "edit <id> [--title <title>] [--body <text>] [--tag <tag>]... (or the new body on stdin)",
			Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, _ *ops.NoteEditInput) {
				fs.StringVar(&title, "title", "", "the new title")
				fs.StringVar(&body, "body", "", "the new body")
				fs.StringSliceVar(&tags, "tag", nil, "the new tags, replacing the old (repeatable)")
			},
			Bind: func(args []string, in *ops.NoteEditInput) error {
				in.ID = args[0]
				if title != "" {
					in.Title = &title
				}
				if body != "" {
					in.Body = &body
				}
				if tags != nil {
					in.Tags = &tags
				}
				return nil
			},
			Stdin: func(r io.Reader, in *ops.NoteEditInput) error {
				if in.Title != nil || in.Body != nil || in.Tags != nil {
					return nil
				}
				text, err := readStdin(r)
				if err != nil {
					return err
				}
				in.Body = &text
				return nil
			},
			Text: printID(func(o ops.NoteEditOutput) string { return o.ID }),
		}),
		opCommand(l, ops.NoteAdd, binding[ops.NoteAddInput, ops.NoteAddOutput]{
			Use:  "add <title>",
			Args: cobra.MinimumNArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.NoteAddInput) {
				fs.StringVar(&in.Area, "area", "", "area id")
				fs.StringVarP(&in.Goal, "project", "p", "", "project id")
				fs.StringSliceVar(&in.Tags, "tag", nil, "tag (repeatable)")
				fs.StringVar(&in.Body, "body", "", "text after the title")
			},
			Bind: func(args []string, in *ops.NoteAddInput) error {
				in.Title = strings.Join(args, " ")
				return nil
			},
			Text: printID(func(o ops.NoteAddOutput) string { return o.ID }),
		}),
		opCommand(l, ops.NoteList, binding[ops.NoteListInput, ops.NoteListOutput]{
			Use:  "list",
			Args: cobra.NoArgs,
			Flags: func(fs *pflag.FlagSet, in *ops.NoteListInput) {
				fs.StringVar(&in.Area, "area", "", "only this area")
				fs.StringVarP(&in.Goal, "project", "p", "", "only this project")
				fs.StringVar(&in.Tag, "tag", "", "only this tag")
			},
			Text: func(w io.Writer, out ops.NoteListOutput) error { return writeNotes(w, out.Notes) },
		}),
		opCommand(l, ops.NoteShow, binding[ops.NoteShowInput, ops.NoteShowOutput]{
			Use:  "show <id>",
			Args: cobra.ExactArgs(1),
			Bind: func(args []string, in *ops.NoteShowInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.NoteShowOutput) error {
				p := &printer{w: w}
				n := out.Note
				p.f("%s\t%s\n", n.ID, n.Title)
				p.f("area\t%s\nproject\t%s\ntags\t%s\n%s\n", dash(n.Area), dash(n.Goal), joinOrDash(n.Tags), out.Body)
				return p.err
			},
		}),
	)
	return cmd
}

func dash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func writeNotes(w io.Writer, notes []ops.NoteView) error {
	p := &printer{w: w}
	for _, n := range notes {
		p.f("%s\t%s\t%s\t%s\n", n.ID, dash(n.Area), dash(n.Goal), n.Title)
	}
	return p.err
}
