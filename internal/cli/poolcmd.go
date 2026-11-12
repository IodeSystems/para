package cli

import (
	"io"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
)

// newPoolCmd is `para pool list|run|pick`: idea pools and their triggers
// (.para/pools.yaml).
func newPoolCmd(l *loader) *cobra.Command {
	cmd := group("pool", "Idea pools (.para/pools.yaml): ideas put off someday with a tag, picked when a trigger comes")
	text := func(w io.Writer, pools []ops.PoolView) error {
		p := &printer{w: w}
		for _, v := range pools {
			state := "waits"
			if v.Fires {
				state = "fires: " + v.Title
			} else if v.Next != "" {
				state = "next " + v.Next
			}
			p.f("%s\t%s\t%d ideas\t%s\n", v.Name, v.Trigger, v.Size, state)
			if v.Problem != "" {
				p.f("  problem: %s\n", v.Problem)
			}
			for _, it := range v.Ideas {
				p.f("  %s\t%s\t%s\tscore %.2f\n", it.ID, it.Title, it.Effort, it.Score)
			}
		}
		return p.err
	}
	cmd.AddCommand(
		opCommand(l, ops.PoolList, binding[ops.PoolListInput, ops.PoolListOutput]{
			Use:  "list [name]",
			Args: cobra.MaximumNArgs(1),
			Bind: func(args []string, in *ops.PoolListInput) error {
				if len(args) == 1 {
					in.Name = args[0]
				}
				return nil
			},
			Text: func(w io.Writer, out ops.PoolListOutput) error { return text(w, out.Pools) },
		}),
		opCommand(l, ops.PoolRun, binding[ops.PoolRunInput, ops.PoolRunOutput]{
			Use:  "run",
			Args: cobra.NoArgs,
			Flags: func(fs *pflag.FlagSet, in *ops.PoolRunInput) {
				fs.BoolVar(&in.DryRun, "dry-run", false, "list the pools that fire now; make no agent work")
			},
			Text: func(w io.Writer, out ops.PoolRunOutput) error {
				if err := text(w, out.Pools); err != nil {
					return err
				}
				p := &printer{w: w}
				for _, id := range out.Gaps {
					p.f("fill-gap\t%s\n", id)
				}
				for _, wn := range out.Warnings {
					p.f("warning: %s\n", wn)
				}
				return p.err
			},
		}),
		opCommand(l, ops.PoolPick, binding[ops.PoolPickInput, ops.PoolPickOutput]{
			Use:  "pick <pool> [id...]",
			Args: cobra.MinimumNArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.PoolPickInput) {
				fs.BoolVar(&in.Skip, "skip", false, "pass this round with no picks")
			},
			Bind: func(args []string, in *ops.PoolPickInput) error {
				in.Pool, in.IDs = args[0], args[1:]
				return nil
			},
			Text: func(w io.Writer, out ops.PoolPickOutput) error {
				p := &printer{w: w}
				if out.Skipped {
					p.f("%s\t%s\tskipped\n", out.Pool, out.Instance)
				}
				for _, id := range out.Picked {
					p.f("picked\t%s\n", id)
				}
				for _, id := range out.Linked {
					p.f("prep of\t%s\t%s\n", out.Instance, id)
				}
				for _, wn := range out.Warnings {
					p.f("warning: %s\n", wn)
				}
				return p.err
			},
		}),
	)
	return cmd
}

// newIdeaCmd is `para idea add <pool> <title>`: keep an idea in a pool.
func newIdeaCmd(l *loader) *cobra.Command {
	cmd := group("idea", "Ideas kept in a pool until its trigger comes")
	var effort string
	cmd.AddCommand(opCommand(l, ops.IdeaAdd, binding[ops.IdeaAddInput, ops.IdeaAddOutput]{
		Use:  "add <pool> <title>",
		Args: cobra.ExactArgs(2),
		Flags: func(fs *pflag.FlagSet, in *ops.IdeaAddInput) {
			fs.StringVar(&in.Text, "text", "", "a sentence or two")
			fs.StringVar(&in.Kind, "kind", "", "task (default) or goal")
			fs.StringVar(&in.Area, "area", "", "area id; default the pool's")
			fs.StringVar(&in.Goal, "goal", "", "goal id; default the pool's")
			fs.StringVar(&effort, "effort", "", "xs, s, m, or l")
		},
		Bind: func(args []string, in *ops.IdeaAddInput) error {
			in.Pool, in.Title, in.Effort = args[0], args[1], model.Effort(effort)
			return nil
		},
		Text: func(w io.Writer, out ops.IdeaAddOutput) error {
			p := &printer{w: w}
			p.f("%s\tpool %s\ttag %s\n", out.ID, out.Pool, out.Tag)
			for _, wn := range out.Warnings {
				p.f("warning: %s\n", wn)
			}
			return p.err
		},
	}))
	return cmd
}
