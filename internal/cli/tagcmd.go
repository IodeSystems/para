package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/para/internal/ops"
)

func newTagCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "tag", Short: "Topics: tag vault documents and dossier items where they are", Args: cobra.NoArgs}
	cmd.AddCommand(
		tagWriteCmd(l, ops.TagAdd, "add"),
		tagWriteCmd(l, ops.TagRemove, "remove"),
		opCommand(l, ops.TagList, binding[ops.TagListInput, ops.TagListOutput]{
			Use:  "list",
			Args: cobra.NoArgs,
			Text: func(w io.Writer, out ops.TagListOutput) error {
				p := &printer{w: w}
				for _, t := range out.Tags {
					p.f("%s\topen %d\tstack %d\tlater %d\n", t.Tag, t.Open, t.Stack, t.Later)
				}
				return p.err
			},
		}),
	)
	return cmd
}

// tagWriteCmd is tag add or tag remove: tags as arguments, items as flags.
func tagWriteCmd(l *loader, op *ops.Op[ops.TagInput, ops.TagOutput], verb string) *cobra.Command {
	var items []string
	return opCommand(l, op, binding[ops.TagInput, ops.TagOutput]{
		Use:  verb + " <tag>... [--id <id>]... [--item <proposal>#<item>]...",
		Args: cobra.MinimumNArgs(1),
		Flags: func(fs *pflag.FlagSet, in *ops.TagInput) {
			fs.StringArrayVar(&in.IDs, "id", nil, "a vault document; a dossier's id tags all its items; repeatable")
			fs.StringArrayVar(&items, "item", nil, "a dossier item as <proposal>#<item>; repeatable")
		},
		Bind: func(args []string, in *ops.TagInput) error {
			in.Tags = args
			for _, s := range items {
				p, it, ok := strings.Cut(s, "#")
				if !ok || p == "" || it == "" {
					return fmt.Errorf("--item %q: want <proposal>#<item>", s)
				}
				in.Items = append(in.Items, ops.ItemRef{Proposal: p, Item: it})
			}
			return nil
		},
		Text: func(w io.Writer, out ops.TagOutput) error {
			p := &printer{w: w}
			for _, c := range out.Changed {
				p.f("changed\t%s\n", c)
			}
			for _, s := range out.Skipped {
				p.f("skipped\t%s\n", s)
			}
			return p.err
		},
	})
}
