package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/ops"
)

// newRequestCmd builds `para request`: the agent request queue. Para starts an
// agent only when dispatch is configured on this machine (para agent status);
// `request wait` only blocks until a request can be claimed, so a shell loop
// can start one.
func newRequestCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "request", Short: "Queue of asks for agents", Args: cobra.NoArgs}
	cmd.AddCommand(
		opCommand(l, ops.RequestAdd, binding[ops.RequestAddInput, ops.RequestAddOutput]{
			Use:  "add <ask>",
			Args: cobra.MinimumNArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.RequestAddInput) {
				fs.StringSliceVar(&in.Links, "link", nil, "task or project id the ask is about (repeatable)")
				fs.StringVar(&in.Runner, "runner", "", "install id that may claim; default vault.properties request.runner, else this install")
			},
			Bind: func(args []string, in *ops.RequestAddInput) error { in.Ask = strings.Join(args, " "); return nil },
			Text: func(w io.Writer, out ops.RequestAddOutput) error {
				p := &printer{w: w}
				p.f("%s\t%s\trunner %s\n", out.ID, out.Status, out.Runner)
				return p.err
			},
		}),
		opCommand(l, ops.RequestList, binding[ops.RequestListInput, ops.RequestListOutput]{
			Use:  "list",
			Args: cobra.NoArgs,
			Flags: func(fs *pflag.FlagSet, in *ops.RequestListInput) {
				fs.StringVar((*string)(&in.Status), "status", "", "only this status: proposed, queued, claimed, done, failed, cancelled")
				fs.IntVar(&in.Limit, "limit", 0, "most to list; 0 means all")
			},
			Text: func(w io.Writer, out ops.RequestListOutput) error {
				p := &printer{w: w}
				for _, r := range out.Items {
					p.f("%s\t%s\t%s\t%s\t%s\n", r.ID, r.Status, r.ClaimedBy, r.LeaseUntil, r.Title)
				}
				return p.err
			},
		}),
		opCommand(l, ops.RequestShow, binding[ops.RequestShowInput, ops.RequestShowOutput]{
			Use:  "show <id>",
			Args: cobra.ExactArgs(1),
			Bind: func(args []string, in *ops.RequestShowInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.RequestShowOutput) error {
				m := out.Doc.Meta.(*model.Request)
				p := &printer{w: w}
				p.f("%s\t%s\nstatus\t%s\nrunner\t%s\nqueued_at\t%s\nclaimed_by\t%s\nlease_until\t%s\nlinks\t%s\nresult_links\t%s\n%s\n",
					out.Doc.ID, out.Doc.Title, m.Status, m.Runner, m.Queued, m.Claimed, m.Lease,
					strings.Join(linkIDs(m.Links), ","), strings.Join(linkIDs(m.Results), ","), out.Doc.Body)
				return p.err
			},
		}),
		opCommand(l, ops.RequestCancel, binding[ops.RequestCancelInput, ops.RequestChangeOutput]{
			Use:  "cancel <id>",
			Args: cobra.ExactArgs(1),
			Bind: func(args []string, in *ops.RequestCancelInput) error { in.ID = args[0]; return nil },
			Text: requestChangeText,
		}),
		opCommand(l, ops.RequestNext, binding[ops.RequestNextInput, ops.RequestNextOutput]{
			Use:  "next",
			Args: cobra.NoArgs,
			Flags: func(fs *pflag.FlagSet, in *ops.RequestNextInput) {
				fs.IntVar(&in.LeaseSeconds, "lease", 0, "lease in seconds; default 1800")
			},
			Text: func(w io.Writer, out ops.RequestNextOutput) error {
				p := &printer{w: w}
				if out.Request == nil {
					p.f("none\n")
					return p.err
				}
				r := out.Request
				p.f("%s\t%s\tlease %s\n%s\n", r.ID, r.Title, r.LeaseUntil, r.Body)
				return p.err
			},
		}),
		opCommand(l, ops.RequestNote, binding[ops.RequestNoteInput, ops.RequestChangeOutput]{
			Use:  "note <id> <text>",
			Args: cobra.MinimumNArgs(2),
			Flags: func(fs *pflag.FlagSet, in *ops.RequestNoteInput) {
				fs.IntVar(&in.LeaseSeconds, "lease", 0, "new lease in seconds; default 1800")
			},
			Bind: func(args []string, in *ops.RequestNoteInput) error {
				in.ID, in.Text = args[0], strings.Join(args[1:], " ")
				return nil
			},
			Text: requestChangeText,
		}),
		opCommand(l, ops.RequestDone, binding[ops.RequestDoneInput, ops.RequestChangeOutput]{
			Use:  "done <id> <result>",
			Args: cobra.MinimumNArgs(2),
			Flags: func(fs *pflag.FlagSet, in *ops.RequestDoneInput) {
				fs.StringSliceVar(&in.Links, "link", nil, "id of a document made or changed (repeatable)")
			},
			Bind: func(args []string, in *ops.RequestDoneInput) error {
				in.ID, in.Result = args[0], strings.Join(args[1:], " ")
				return nil
			},
			Text: requestChangeText,
		}),
		opCommand(l, ops.RequestFail, binding[ops.RequestFailInput, ops.RequestChangeOutput]{
			Use:  "fail <id> <reason>",
			Args: cobra.MinimumNArgs(2),
			Bind: func(args []string, in *ops.RequestFailInput) error {
				in.ID, in.Reason = args[0], strings.Join(args[1:], " ")
				return nil
			},
			Text: requestChangeText,
		}),
		newRequestAcceptCmd(l),
		newRequestWaitCmd(l),
	)
	return cmd
}

func requestChangeText(w io.Writer, out ops.RequestChangeOutput) error {
	p := &printer{w: w}
	p.f("%s\t%s\n", out.ID, out.Status)
	return p.err
}

// errWaitTimeout is the error of a wait that ran out of time.
var errWaitTimeout = errors.New("request wait: timed out with no claimable request")

// newRequestWaitCmd is CLI-only on purpose and not an op: an agent that needs
// the next request calls request_next. Wait watches the vault's event files
// with no LLM, checks the requests folder on each change, and exits 0 as soon
// as this install can claim one. It claims nothing.
func newRequestWaitCmd(l *loader) *cobra.Command {
	var timeout, interval time.Duration
	cmd := &cobra.Command{
		Use:   "wait",
		Short: "Block until a request can be claimed, then exit 0 (CLI only)",
		Long: "Watches the vault's event files, with no LLM use, and checks the request files on each change, until this install can claim a request.\n" +
			"Exits 0 then, and claims nothing. With --timeout it exits non-zero when time runs out.\n" +
			"Example: while para request wait; do my-agent-launcher; done",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if interval <= 0 {
				return errors.New("request wait: --interval must be positive")
			}
			if timeout < 0 {
				return errors.New("request wait: --timeout is negative")
			}
			if err := l.load(); err != nil {
				return err
			}
			deps, err := l.deps()
			if err != nil {
				return err
			}
			watch := ops.NewRequestWatch(deps)
			err = l.waitFor(cmd.Context(), interval, timeout, errWaitTimeout, watch.Claimable)
			if err != nil && !errors.Is(err, errWaitTimeout) && !errors.Is(err, context.Canceled) {
				return fmt.Errorf("request wait: %w", err)
			}
			return err
		},
	}
	cmd.Flags().DurationVar(&timeout, "timeout", 0, "give up after this long and exit non-zero; 0 waits forever")
	cmd.Flags().DurationVar(&interval, "interval", waitFallback, "longest time between checks when no event arrives")
	return cmd
}

// newRequestAcceptCmd is CLI-only on purpose and not an op: a proposal came
// from an agent surface, and only a person's surface may queue it.
func newRequestAcceptCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "accept <id>",
		Short: "Accept a proposed request into the queue (CLI only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := l.load(); err != nil {
				return err
			}
			deps, err := l.deps()
			if err != nil {
				return err
			}
			out, err := ops.RequestAccept(cmd.Context(), deps, ops.RequestAcceptInput{ID: args[0]})
			if err != nil {
				return err
			}
			return emit(cmd, out, requestChangeText)
		},
	}
	addJSONFlag(cmd)
	return cmd
}
