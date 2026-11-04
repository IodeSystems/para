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

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
)

// newProposalCmd builds `para proposal`. An agent files through MCP and records
// the owner's decisions from the chat with proposal_decide; these commands read
// proposals and let a person at a terminal approve or deny.
func newProposalCmd(l *loader) *cobra.Command {
	var execText bool
	cmd := &cobra.Command{Use: "proposal", Short: "Items an agent suggests and the owner decides in the chat", Args: cobra.NoArgs}
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
					p.f("approved\t%s\t%s\t%s\t%s\n", out.Proposal, out.Item, out.Subtype, out.Target)
				}
				return p.err
			},
		}),
		opCommand(l, ops.ProposalApprove, binding[ops.ProposalApproveInput, ops.ProposalReviewOutput]{
			Use:  "approve <id> [item]... [--group <group>]",
			Args: cobra.MinimumNArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.ProposalApproveInput) {
				fs.StringVar(&in.Group, "group", "", "approve every waiting item of this group")
				fs.StringVar(&in.Reason, "reason", "", "optional: kept on the alternatives a pick closes")
			},
			Bind: func(args []string, in *ops.ProposalApproveInput) error {
				in.ID, in.Items = args[0], args[1:]
				return nil
			},
			Text: proposalReviewText,
		}),
		reasonCmd(l, ops.ProposalDeny, "deny <id> <item>"),
		opCommand(l, ops.ProposalDecide, binding[ops.ProposalDecideInput, ops.ProposalDecideOutput]{
			Use:   "decide <id> < decision.json",
			Args:  cobra.ExactArgs(1),
			Stdin: decodeStdin[ops.ProposalDecideInput],
			Bind:  func(args []string, in *ops.ProposalDecideInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.ProposalDecideOutput) error {
				p := &printer{w: w}
				p.f("%s\t%s\n", out.ID, out.Status)
				writeDecisions(p, out.Items)
				if out.Revision != "" {
					p.f("revision\t%s\n", out.Revision)
				}
				return p.err
			},
		}),
		newOpenCmd(l, "open <id|url>"),
		newProposalWaitCmd(l),
		opCommand(l, ops.ProposalExecuted, binding[ops.ProposalExecutedInput, ops.ProposalExecutedOutput]{
			Use:  "executed <id> <item> done|failed",
			Args: cobra.ExactArgs(3),
			Flags: func(fs *pflag.FlagSet, in *ops.ProposalExecutedInput) {
				fs.StringVar(&in.URL, "url", "", "where the result is")
				fs.StringVar(&in.Locator, "locator", "", "where the result is, when it has no URL")
				fs.StringVar(&in.Reason, "reason", "", "why it failed")
				fs.StringVar(&in.By, "by", "", "the agent that carried it out, when you record it for a worker")
				fs.StringVar(&in.OnBehalfOf, "on-behalf-of", "", "owner: it was done as the owner")
				fs.BoolVar(&execText, "text-stdin", false, "a done doc edit of a tracked document: read the document's new text from stdin")
			},
			Bind: func(args []string, in *ops.ProposalExecutedInput) error {
				in.ID, in.Item, in.Result = args[0], args[1], args[2]
				return nil
			},
			Stdin: func(r io.Reader, in *ops.ProposalExecutedInput) (err error) {
				if execText {
					in.Text, err = readStdin(r)
				}
				return err
			},
			Text: func(w io.Writer, out ops.ProposalExecutedOutput) error {
				p := &printer{w: w}
				p.f("%s\t%s\t%s\treceipt %s\n", out.ID, out.Item, out.State, out.Receipt)
				return p.err
			},
		}),
		opCommand(l, ops.ProposalDone, binding[ops.ProposalDoneInput, ops.ProposalExecutedOutput]{
			Use:  "done <id> <item> [--url <url>] [--locator <text>] [--text <text>] [--note <text>]",
			Args: cobra.ExactArgs(2),
			Flags: func(fs *pflag.FlagSet, in *ops.ProposalDoneInput) {
				fs.StringVar(&in.URL, "url", "", "where the result is")
				fs.StringVar(&in.Locator, "locator", "", "where it is, when it has no URL")
				fs.StringVar(&in.Text, "text", "", "what was sent, or the document after a doc edit")
				fs.StringVar(&in.Note, "note", "", "a note for the receipt")
				fs.StringVar(&in.At, "at", "", "RFC 3339 time it was done; default now")
			},
			Bind: func(args []string, in *ops.ProposalDoneInput) error { in.ID, in.Item = args[0], args[1]; return nil },
			Text: func(w io.Writer, out ops.ProposalExecutedOutput) error {
				p := &printer{w: w}
				p.f("%s\t%s\t%s\treceipt %s\n", out.ID, out.Item, out.State, out.Receipt)
				return p.err
			},
		}),
		opCommand(l, ops.ProposalSubmit, binding[ops.ProposalSubmitInput, ops.ProposalSubmitOutput]{
			Use:  "submit <id>",
			Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.ProposalSubmitInput) {
				fs.IntVar(&in.Revision, "revision", 0, "the revision you answered; default the one in the file now")
			},
			Bind: func(args []string, in *ops.ProposalSubmitInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.ProposalSubmitOutput) error {
				p := &printer{w: w}
				p.f("%s\t%s\n", out.ID, out.Status)
				writeDecisions(p, out.Items)
				return p.err
			},
		}),
		opCommand(l, ops.ProposalResponse, binding[ops.ProposalIDInput, ops.ProposalResponseOutput]{
			Use:  "response <id>",
			Args: cobra.ExactArgs(1),
			Bind: func(args []string, in *ops.ProposalIDInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.ProposalResponseOutput) error {
				p := &printer{w: w}
				p.f("%s\trev %d\tresponse %s\n", out.ID, out.Revision, out.Response)
				if out.Overall != "" {
					p.f("overall\t%s\n", out.Overall)
				}
				for k, v := range out.Sections {
					p.f("section %s\t%s\n", k, v)
				}
				for _, it := range out.Items {
					p.f("%s\t%s\t%s\t%s\t%s\n", it.ID, it.Kind, it.State, it.Stack, it.Decision)
					if a := it.Answer; a != nil {
						if len(a.Choices) > 0 {
							p.f("  choices\t%s\n", strings.Join(a.Choices, ", "))
						}
						if a.OwnWords != "" {
							p.f("  own words\t%s\n", a.OwnWords)
						}
						if a.Comment != "" {
							p.f("  comment\t%s\n", a.Comment)
						}
					}
				}
				p.f("todo claim\t%s\ntodo answered\t%s\ntodo reply\t%s\n", strings.Join(out.Todo.Claim, " "), strings.Join(out.Todo.Answered, " "), strings.Join(out.Todo.Reply, " "))
				return p.err
			},
		}),
		opCommand(l, ops.ProposalClaim, binding[ops.ProposalClaimInput, ops.ProposalClaimOutput]{
			Use:  "claim <id> [item]... [--group <group>]",
			Args: cobra.MinimumNArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.ProposalClaimInput) {
				fs.StringVar(&in.Group, "group", "", "claim every claimable item of this group")
				fs.IntVar(&in.LeaseSeconds, "lease", 0, "seconds the claim holds; default 3600")
			},
			Bind: func(args []string, in *ops.ProposalClaimInput) error { in.ID, in.Items = args[0], args[1:]; return nil },
			Text: func(w io.Writer, out ops.ProposalClaimOutput) error {
				p := &printer{w: w}
				p.f("%s\tclaimed %s\tuntil %s\n", out.ID, strings.Join(out.Claimed, " "), out.LeaseUntil)
				return p.err
			},
		}),
		opCommand(l, ops.ProposalReply, binding[ops.ProposalReplyInput, ops.ProposalReplyOutput]{
			Use:  "reply <id> <text> (--item <item> | --section <section>)",
			Args: cobra.ExactArgs(2),
			Flags: func(fs *pflag.FlagSet, in *ops.ProposalReplyInput) {
				fs.StringVar(&in.Item, "item", "", "the item whose comment you answer")
				fs.StringVar(&in.Section, "section", "", "the section id whose comment you answer, or overall")
			},
			Bind: func(args []string, in *ops.ProposalReplyInput) error { in.ID, in.Text = args[0], args[1]; return nil },
			Text: func(w io.Writer, out ops.ProposalReplyOutput) error {
				p := &printer{w: w}
				p.f("%s\t%s\n", out.ID, out.Reply)
				return p.err
			},
		}),
		opCommand(l, ops.ProposalStack, binding[ops.ProposalStackInput, ops.ProposalStackOutput]{
			Use:  "stack",
			Args: cobra.NoArgs,
			Flags: func(fs *pflag.FlagSet, in *ops.ProposalStackInput) {
				fs.StringVar(&in.State, "state", "", "only this stack state: proposed, decided, executing, done, failed, closed")
				fs.StringVar(&in.Kind, "kind", "", "only this item kind")
				fs.StringVar(&in.Group, "group", "", "only this group")
				fs.StringVar(&in.Proposal, "proposal", "", "only this dossier")
				fs.StringVar(&in.Actor, "actor-filter", "", "only dossiers this agent filed")
				fs.StringVar(&in.Cursor, "cursor", "", "next_cursor of the page before")
				fs.IntVar(&in.Limit, "limit", 0, "rows per page; default 200")
			},
			Text: func(w io.Writer, out ops.ProposalStackOutput) error {
				p := &printer{w: w}
				for _, c := range out.Counts {
					p.f("%s %d  ", c.State, c.Count)
				}
				p.f("\n")
				for _, r := range out.Rows {
					p.f("%s\t%s\t%s\t%s\t%s\t%s\n", r.Proposal, r.Item, r.Stack, r.Kind, r.Group, r.Summary)
				}
				if out.NextCursor != "" {
					p.f("next\t%s\n", out.NextCursor)
				}
				return p.err
			},
		}),
		opCommand(l, ops.ProposalDraft, binding[ops.ProposalDraftInput, ops.ProposalDraftOutput]{
			Use:   "draft <id> < draft.json",
			Args:  cobra.ExactArgs(1),
			Stdin: decodeStdin[ops.ProposalDraftInput],
			Bind:  func(args []string, in *ops.ProposalDraftInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.ProposalDraftOutput) error {
				p := &printer{w: w}
				p.f("%s\trev %d\t%d of %d drafted\n", out.ID, out.Revision, out.Drafted, out.Total)
				return p.err
			},
		}),
		opCommand(l, ops.ProposalUndone, binding[ops.ProposalUndoneInput, ops.ProposalUndoneOutput]{
			Use:  "undone",
			Args: cobra.NoArgs,
			Text: func(w io.Writer, out ops.ProposalUndoneOutput) error {
				p := &printer{w: w}
				for _, it := range out.Items {
					p.f("%s\t%s\t%s\n", it.Proposal, it.Item, it.Summary)
				}
				return p.err
			},
		}),
	)
	return cmd
}

// newStackCmd builds `para stack`: decide items of the review stack across
// dossiers. `para proposal stack` lists it.
func newStackCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "stack", Short: "The review stack: decide items across dossiers", Args: cobra.NoArgs}
	cmd.AddCommand(
		opCommand(l, ops.StackDecide, binding[ops.StackDecideInput, ops.StackDecideOutput]{
			Use:  "decide approve|reject|skip|ask [proposal:item]... [--group <group>]",
			Args: cobra.MinimumNArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.StackDecideInput) {
				fs.StringVar(&in.Group, "group", "", "every waiting item of this group")
				fs.StringVar(&in.Proposal, "proposal", "", "with --group: only this dossier")
				fs.StringVar(&in.Comment, "comment", "", "why; required to reject (the reason) and to ask (what the agent should do)")
				fs.StringArrayVar(&in.Refs, "ref", nil, "ask: a vault id the agent should read (repeatable)")
			},
			Bind: func(args []string, in *ops.StackDecideInput) error {
				in.Decision = args[0]
				for _, a := range args[1:] {
					prop, item, ok := strings.Cut(a, ":")
					if !ok {
						return fmt.Errorf("%q: want proposal:item", a)
					}
					in.Items = append(in.Items, ops.StackItemRef{Proposal: prop, Item: item})
				}
				return nil
			},
			Text: func(w io.Writer, out ops.StackDecideOutput) error {
				p := &printer{w: w}
				for _, r := range out.Proposals {
					p.f("%s\t%s\n", r.ID, r.Status)
					writeDecisions(p, r.Items)
				}
				return p.err
			},
		}),
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
	p.f("%s\trev %d\t%s\nopen: %s\n", out.ID, out.Revision, out.Status, out.ReviewCommand)
	if out.ReviewURL != "" {
		p.f("url: %s\n", out.ReviewURL)
	}
	return p.err
}

func proposalReviewText(w io.Writer, out ops.ProposalReviewOutput) error {
	p := &printer{w: w}
	p.f("%s\t%s\n", out.ID, out.Status)
	writeDecisions(p, out.Items)
	return p.err
}

func writeDecisions(p *printer, list []ops.Decision) {
	for _, d := range list {
		extra := d.Reason
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
	p.f("%s\t%s\nstatus\t%s\nrevision\t%d\nproposed_by\t%s\nsource\t%s\nopen\t%s\n", v.ID, v.Title, v.Status, v.Revision, v.ProposedBy, out.SourceDoc.ID, out.ReviewCommand)
	if out.ReviewURL != "" {
		p.f("url\t%s\n", out.ReviewURL)
	}
	if out.SupersededBy != "" {
		p.f("superseded_by\t%s\n", out.SupersededBy)
	}
	if out.TLDR != "" {
		p.f("tldr\t%s\n", out.TLDR)
	}
	if out.Context != "" {
		p.f("context\t%s\n", out.Context)
	}
	p.f("note\t%s\n", out.Untrusted)
	for _, it := range out.Items {
		p.f("\n%s\t%s\t%s", it.ID, kindWord(it.ProposalItem), it.State)
		if it.Edited {
			p.f("\tedited")
		}
		if it.Reason != "" {
			p.f("\treason: %s", it.Reason)
		}
		p.f("\n")
		if dc := it.Decided; dc != nil && dc.OwnerSaid != "" {
			p.f("owner said (self-reported)\t%s\n", dc.OwnerSaid)
		}
		writeItemBody(p, it)
		p.f("evidence (untrusted)\t%s\t%s\n", it.Evidence.Locator, it.Evidence.Quote)
		p.f("actions\t%s\n", strings.Join(it.Actions, ", "))
	}
	return p.err
}

func kindWord(it model.ProposalItem) string {
	if it.Subtype != "" {
		return string(it.Kind) + "/" + it.Subtype
	}
	return string(it.Kind)
}

func writeItemBody(p *printer, it ops.ItemView) {
	row := func(k, v string) {
		if v != "" {
			p.f("%s\t%s\n", k, v)
		}
	}
	if it.Kind == model.ItemPara {
		p.f("op\t%s %s\n", it.Op, inputText(it.Input))
	} else {
		p.f("to\t%s\n", it.GateTarget())
	}
	row("subject", it.Subject)
	row("what", it.What)
	row("previous", it.Previous)
	row("how", it.How)
	row("impact", it.Impact)
	row("title", it.Title)
	if it.Body != "" {
		p.f("%s\n", it.Body)
	}
	if it.Diff != "" {
		p.f("%s\n", it.Diff)
	}
	row("reversible", strings.TrimSpace(string(it.Reversible)+" "+it.ReversibleHow))
	row("after", it.After)
	row("bundle", it.Bundle)
	row("choice", it.Choice)
	row("group", it.Group)
	for _, c := range it.Pros {
		p.f("pro\t%s [%s]\n", c.Text, c.Source)
	}
	for _, c := range it.Cons {
		p.f("con\t%s [%s]\n", c.Text, c.Source)
	}
	if it.Cost != nil {
		p.f("cost\t%s [%s]\n", it.Cost.Text, it.Cost.Source)
	}
	if it.Benefit != nil {
		p.f("benefit\t%s [%s]\n", it.Benefit.Text, it.Benefit.Source)
	}
	if it.Confidence > 0 {
		p.f("confidence\t%d%% %s\n", it.Confidence, it.ConfidenceBasis)
	}
	if it.Recommendation > 0 {
		p.f("recommendation\t%d%%\n", it.Recommendation)
	}
	if pv := it.Preview; pv != nil {
		if pv.Error != "" {
			p.f("would fail\t%s\n", pv.Error)
		}
		for _, c := range pv.Changes {
			p.f("changes\t%s\t%s\n", c.Doc, c.Title)
			for _, ln := range c.Lines {
				p.f("  %s\n", ln)
			}
		}
		for _, w := range pv.Downstream {
			p.f("downstream\t%s\t%s\t%s\n", w.Doc, w.Title, w.Effect)
		}
	}
	for _, t := range it.Thread {
		p.f("thread\t%s\t%s\t%s\t%s\t%s\n", t.ID, t.Kind, t.State, t.By, t.Text)
		if t.Answer != "" {
			p.f("answer\t%s\n", t.Answer)
		}
	}
}

func inputText(in map[string]any) string {
	b, err := json.Marshal(in)
	if err != nil {
		return ""
	}
	return string(b)
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
