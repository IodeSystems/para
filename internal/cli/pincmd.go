package cli

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/iodesystems/para/internal/ops"
)

// pinArg puts an argument in id or key: an item id goes in id, a #tag or
// channel key in key.
func pinArg(arg string, in *ops.PinInput) {
	var key, id string
	threadTarget(arg, &key, &id)
	in.ID, in.Key = id, key
}

func pinText(w io.Writer, out ops.PinOutput) error {
	p := &printer{w: w}
	p.f("%s\t%s\t%s\t%s\n", out.ID, out.Type, firstNonEmpty(out.Pinned, "not pinned"), out.Title)
	return p.err
}

// newPinCmd is `para pin <id|#tag|channel:...>`: watch an item or a thread.
func newPinCmd(l *loader) *cobra.Command {
	return opCommand(l, ops.Pin, binding[ops.PinInput, ops.PinOutput]{
		Use:  "pin <id|#tag|channel:<source>/<thread>>",
		Args: cobra.ExactArgs(1),
		Bind: func(args []string, in *ops.PinInput) error { pinArg(args[0], in); return nil },
		Text: pinText,
	})
}

// newUnpinCmd is `para unpin <id|key>`.
func newUnpinCmd(l *loader) *cobra.Command {
	return opCommand(l, ops.Unpin, binding[ops.PinInput, ops.PinOutput]{
		Use:  "unpin <id|#tag|channel:<source>/<thread>>",
		Args: cobra.ExactArgs(1),
		Bind: func(args []string, in *ops.PinInput) error { pinArg(args[0], in); return nil },
		Text: pinText,
	})
}

// newPinnedCmd is `para pinned`: the items the owner watches.
func newPinnedCmd(l *loader) *cobra.Command {
	return opCommand(l, ops.Pinned, binding[ops.PinnedInput, ops.PinnedOutput]{
		Use:  "pinned",
		Args: cobra.NoArgs,
		Bind: func([]string, *ops.PinnedInput) error { return nil },
		Text: func(w io.Writer, out ops.PinnedOutput) error {
			p := &printer{w: w}
			for _, c := range out.Items {
				flag := ""
				if c.NeedsYou {
					flag = "needs you"
				}
				p.f("%s\t%s\t%s\t%s\t%s\t%s\n", c.ID, c.Type, firstNonEmpty(c.NextDate, "-"), flag, firstNonEmpty(c.State, string(c.Life)), c.Title)
			}
			return p.err
		},
	})
}
