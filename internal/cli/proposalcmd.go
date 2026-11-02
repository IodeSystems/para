package cli

import (
	"context"
	"encoding/json"
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

// newProposalCmd builds `para proposal`. An agent files through MCP; these
// commands read proposals and let the owner decide from a shell. The usual way
// to decide is `para review`.
func newProposalCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "proposal", Short: "Items an agent suggests and the owner reviews", Args: cobra.NoArgs}
	cmd.AddCommand(
		opCommand(l, ops.ProposalAdd, binding[ops.ProposalAddInput, ops.ProposalFiled]{
			Use:   "add < proposal.json",
			Args:  cobra.NoArgs,
			Stdin: decodeStdin[ops.ProposalAddInput],
			Text:  proposalFiledText,
		}),
		opCommand(l, ops.ProposalRevise, binding[ops.ProposalReviseInput, ops.ProposalFiled]{
			Use:   "revise <id> < revision.json",
			Args:  cobra.ExactArgs(1),
			Stdin: decodeStdin[ops.ProposalReviseInput],
			Bind:  func(args []string, in *ops.ProposalReviseInput) error { in.ID = args[0]; return nil },
			Text:  proposalFiledText,
		}),
		opCommand(l, ops.ProposalList, binding[ops.ProposalListInput, ops.ProposalListOutput]{
			Use:  "list",
			Args: cobra.NoArgs,
			Flags: func(fs *pflag.FlagSet, in *ops.ProposalListInput) {
				fs.StringVar((*string)(&in.Status), "status", "", "only this status: open, sent_back, closed, superseded")
				fs.IntVar(&in.Limit, "limit", 0, "most to list; 0 means all")
			},
			Text: func(w io.Writer, out ops.ProposalListOutput) error {
				p := &printer{w: w}
				for _, v := range out.Items {
					p.f("%s\t%s\trev %d\t%d of %d pending\t%s\t%s\n", v.ID, v.Status, v.Revision, v.Pending, v.ItemCount, v.ProposedBy, v.Title)
				}
				return p.err
			},
		}),
		opCommand(l, ops.ProposalShow, binding[ops.ProposalIDInput, ops.ProposalDetail]{
			Use:  "show <id>",
			Args: cobra.ExactArgs(1),
			Bind: func(args []string, in *ops.ProposalIDInput) error { in.ID = args[0]; return nil },
			Text: proposalDetailText,
		}),
		opCommand(l, ops.ProposalDiff, binding[ops.ProposalIDInput, ops.ProposalDiffOutput]{
			Use:  "diff <id>",
			Args: cobra.ExactArgs(1),
			Bind: func(args []string, in *ops.ProposalIDInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.ProposalDiffOutput) error {
				p := &printer{w: w}
				p.f("%s\trev %d\tfrom %s\n", out.ID, out.Revision, out.Previous)
				for _, it := range out.Items {
					p.f("%s\t%s\n", it.Item, it.Change)
					for _, f := range it.Fields {
						p.f("  %s\n", f.Field)
						for _, ln := range f.Lines {
							p.f("    %s\n", ln)
						}
					}
				}
				return p.err
			},
		}),
		opCommand(l, ops.ProposalApproved, binding[ops.ProposalApprovedInput, ops.ProposalApprovedOutput]{
			Use:  "approved <hash>",
			Args: cobra.ExactArgs(1),
			Bind: func(args []string, in *ops.ProposalApprovedInput) error { in.Hash = args[0]; return nil },
			Text: func(w io.Writer, out ops.ProposalApprovedOutput) error {
				p := &printer{w: w}
				if out.Approved {
					p.f("approved\t%s\t%s\t%s\t%s\n", out.Proposal, out.Item, out.Medium, out.Target)
				}
				return p.err
			},
		}),
		opCommand(l, ops.ProposalApprove, binding[ops.ProposalApproveInput, ops.ProposalReviewOutput]{
			Use:  "approve <id> <item>...",
			Args: cobra.MinimumNArgs(2),
			Bind: func(args []string, in *ops.ProposalApproveInput) error {
				in.ID, in.Items = args[0], args[1:]
				return nil
			},
			Text: proposalReviewText,
		}),
		reasonCmd(l, ops.ProposalDeny, "deny <id> <item>"),
		reasonCmd(l, ops.ProposalRework, "rework <id> <item>"),
		opCommand(l, ops.ProposalSendback, binding[ops.ProposalSendbackInput, ops.ProposalReviewOutput]{
			Use:  "sendback <id> <item>",
			Args: cobra.ExactArgs(2),
			Flags: func(fs *pflag.FlagSet, in *ops.ProposalSendbackInput) {
				fs.StringVar(&in.Reason, "reason", "", "optional: the edits and annotations already say why")
			},
			Bind: func(args []string, in *ops.ProposalSendbackInput) error {
				in.ID, in.Item = args[0], args[1]
				return nil
			},
			Text: proposalReviewText,
		}),
		opCommand(l, ops.ProposalHold, binding[ops.ProposalHoldInput, ops.ProposalReviewOutput]{
			Use:  "hold <id> <item> --kind task|request --text <text>",
			Args: cobra.ExactArgs(2),
			Flags: func(fs *pflag.FlagSet, in *ops.ProposalHoldInput) {
				fs.StringVar((*string)(&in.Kind), "kind", "", "task or request (required)")
				fs.StringVar(&in.Text, "text", "", "the task title, or the ask (required)")
				fs.StringVar(&in.Project, "project", "", "task: project id")
				fs.StringVar(&in.Area, "area", "", "task: area id")
			},
			Bind: func(args []string, in *ops.ProposalHoldInput) error {
				in.ID, in.Item = args[0], args[1]
				return nil
			},
			Text: proposalReviewText,
		}),
		opCommand(l, ops.ProposalAnnotate, binding[ops.ProposalAnnotateInput, ops.ProposalReviewOutput]{
			Use:  "annotate <id> <item> --quote <text> --note <text>",
			Args: cobra.ExactArgs(2),
			Flags: func(fs *pflag.FlagSet, in *ops.ProposalAnnotateInput) {
				fs.StringVar(&in.Quote, "quote", "", "text from the item the note is about (required)")
				fs.StringVar(&in.Note, "note", "", "the note (required)")
			},
			Bind: func(args []string, in *ops.ProposalAnnotateInput) error {
				in.ID, in.Item = args[0], args[1]
				return nil
			},
			Text: proposalReviewText,
		}),
		newProposalEditCmd(l),
		newProposalWaitCmd(l),
	)
	return cmd
}

func reasonCmd(l *loader, op *ops.Op[ops.ProposalReasonInput, ops.ProposalReviewOutput], use string) *cobra.Command {
	return opCommand(l, op, binding[ops.ProposalReasonInput, ops.ProposalReviewOutput]{
		Use:  use,
		Args: cobra.ExactArgs(2),
		Flags: func(fs *pflag.FlagSet, in *ops.ProposalReasonInput) {
			fs.StringVar(&in.Reason, "reason", "", "why (required)")
		},
		Bind: func(args []string, in *ops.ProposalReasonInput) error {
			in.ID, in.Item = args[0], args[1]
			return nil
		},
		Text: proposalReviewText,
	})
}

func newProposalEditCmd(l *loader) *cobra.Command {
	var draft, target, input string
	return opCommand(l, ops.ProposalEdit, binding[ops.ProposalEditInput, ops.ProposalReviewOutput]{
		Use:  "edit <id> <item>",
		Args: cobra.ExactArgs(2),
		Flags: func(fs *pflag.FlagSet, _ *ops.ProposalEditInput) {
			fs.StringVar(&draft, "draft", "", "external: the new draft")
			fs.StringVar(&target, "target", "", "external: the new target")
			fs.StringVar(&input, "input", "", "op: the new input, in full, as JSON")
		},
		Bind: func(args []string, in *ops.ProposalEditInput) error {
			in.ID, in.Item = args[0], args[1]
			if draft != "" {
				in.Draft = &draft
			}
			if target != "" {
				in.Target = &target
			}
			if input != "" {
				return json.Unmarshal([]byte(input), &in.Input)
			}
			return nil
		},
		Text: proposalReviewText,
	})
}

// decodeStdin reads a whole op Input as JSON. An unknown field is an error.
func decodeStdin[I any](r io.Reader, in *I) error {
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(in); err != nil {
		return fmt.Errorf("stdin: %w", err)
	}
	return nil
}

func proposalFiledText(w io.Writer, out ops.ProposalFiled) error {
	p := &printer{w: w}
	p.f("%s\trev %d\t%s\nreview: %s\n", out.ID, out.Revision, out.Status, out.ReviewCommand)
	if out.ReviewURL != "" {
		p.f("review: %s\n", out.ReviewURL)
	}
	return p.err
}

func proposalReviewText(w io.Writer, out ops.ProposalReviewOutput) error {
	p := &printer{w: w}
	p.f("%s\t%s\n", out.ID, out.Status)
	writeDecisions(p, out.Items)
	if out.Request != "" {
		p.f("request\t%s\n", out.Request)
	}
	return p.err
}

func writeDecisions(p *printer, list []ops.Decision) {
	for _, d := range list {
		extra := d.Reason
		if d.HeldBy != "" {
			extra = "held by " + d.HeldBy
		}
		if d.ApprovedHash != "" {
			extra = strings.TrimSpace(d.ApprovedHash + " " + extra)
		}
		if d.Result != "" {
			extra += " made " + d.Result
		}
		p.f("%s\t%s\t%s\n", d.Item, d.State, strings.TrimSpace(extra))
	}
}

func proposalDetailText(w io.Writer, out ops.ProposalDetail) error {
	p := &printer{w: w}
	v := out.Proposal
	p.f("%s\t%s\nstatus\t%s\nrevision\t%d\nproposed_by\t%s\nsource\t%s\nreview\t%s\n", v.ID, v.Title, v.Status, v.Revision, v.ProposedBy, out.SourceDoc.ID, out.ReviewCommand)
	if out.ReviewURL != "" {
		p.f("review\t%s\n", out.ReviewURL)
	}
	if out.SupersededBy != "" {
		p.f("superseded_by\t%s\n", out.SupersededBy)
	}
	p.f("note\t%s\n", out.Untrusted)
	for _, it := range out.Items {
		p.f("\n%s\t%s\t%s", it.ID, it.Kind, it.State)
		if it.Edited {
			p.f("\tedited")
		}
		if it.Reason != "" {
			p.f("\treason: %s", it.Reason)
		}
		if it.HeldBy != "" {
			p.f("\theld by %s", it.HeldBy.Target())
		}
		p.f("\n")
		if it.Kind == model.ItemOp {
			raw, err := json.Marshal(it.Input)
			if err != nil {
				return err
			}
			p.f("op\t%s %s\n", it.Op, raw)
		} else {
			p.f("to\t%s %s\n%s\n", it.Medium, it.Target, it.Draft)
		}
		p.f("evidence (untrusted)\t%s\t%s\n", it.Evidence.Locator, it.Evidence.Quote)
		p.f("actions\t%s\n", strings.Join(it.Actions, ", "))
	}
	for _, a := range out.Annotations {
		p.f("\n%s\t%s\t%s\t%q: %s\n", a.ID, a.Item, a.State, a.Quote, a.Note)
		if a.Answer != "" {
			p.f("answer\t%s\n", a.Answer)
		}
	}
	return p.err
}

// errProposalTimeout is the error of a wait that ran out of time.
var errProposalTimeout = errors.New("proposal wait: timed out with no decision")

// newProposalWaitCmd is `para proposal wait`. It carries the name of the op
// proposal_wait and its Output, but it blocks as long as it is told: it watches
// event files, so an agent harness can leave it running as a background job at no
// token cost. It exits 0 on a decision, non-zero on a timeout.
func newProposalWaitCmd(l *loader) *cobra.Command {
	var timeout, interval time.Duration
	cmd := &cobra.Command{
		Use:         "wait <id>",
		Short:       "Block until the owner decides, then print the decisions and exit 0; exit non-zero on a timeout",
		Long:        "Watches the vault's event files, with no LLM use, and reads the proposal on each change. A decision is made when no item waits for the owner.\nExits 0 and prints the decisions then. With --timeout it exits non-zero when time runs out.",
		Args:        cobra.ExactArgs(1),
		Annotations: map[string]string{opAnnotation: ops.ProposalWait.Name()},
		RunE: func(cmd *cobra.Command, args []string) error {
			if interval <= 0 {
				return errors.New("proposal wait: --interval must be positive")
			}
			if timeout < 0 {
				return errors.New("proposal wait: --timeout is negative")
			}
			if err := l.load(); err != nil {
				return err
			}
			deps, err := l.deps()
			if err != nil {
				return err
			}
			var out ops.ProposalWaitOutput
			err = l.waitFor(cmd.Context(), interval, timeout, errProposalTimeout, func(ctx context.Context) (bool, error) {
				var err error
				out, err = ops.ProposalPoll(ctx, deps, args[0])
				return err == nil && !out.Pending, err
			})
			if err != nil {
				if errors.Is(err, errProposalTimeout) || errors.Is(err, context.Canceled) {
					return err
				}
				return fmt.Errorf("proposal wait: %w", err)
			}
			return emit(cmd, out, proposalWaitText)
		},
	}
	addJSONFlag(cmd)
	cmd.Flags().DurationVar(&timeout, "timeout", 0, "give up after this long and exit non-zero; 0 waits forever")
	cmd.Flags().DurationVar(&interval, "interval", waitFallback, "longest time between checks when no event arrives")
	return cmd
}

func proposalWaitText(w io.Writer, out ops.ProposalWaitOutput) error {
	p := &printer{w: w}
	p.f("%s\t%s\n", out.ID, out.Status)
	if out.SupersededBy != "" {
		p.f("superseded by\t%s\n", out.SupersededBy)
	}
	writeDecisions(p, out.Decisions)
	return p.err
}
