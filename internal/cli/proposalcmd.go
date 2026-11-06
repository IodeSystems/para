package cli

import (
	"encoding/json"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
)

// newProposalCmd builds `para proposal`: dossiers are read-only records since
// vault format 8. executed is an alias of io_send for one release.
func newProposalCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "proposal", Short: "Old dossiers, read-only: list and show them", Args: cobra.NoArgs}
	cmd.AddCommand(
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
		newOpenCmd(l, "open <id|url>"),
		opCommand(l, ops.ProposalExecuted, binding[ops.ProposalExecutedInput, ops.ProposalExecutedOutput]{
			Use:  "executed <id> <item> done",
			Args: cobra.ExactArgs(3),
			Flags: func(fs *pflag.FlagSet, in *ops.ProposalExecutedInput) {
				fs.StringVar(&in.URL, "url", "", "where the result is")
				fs.StringVar(&in.Locator, "locator", "", "where the result is, when it has no URL")
			},
			Bind: func(args []string, in *ops.ProposalExecutedInput) error {
				in.ID, in.Item, in.Result = args[0], args[1], args[2]
				return nil
			},
			Text: func(w io.Writer, out ops.ProposalExecutedOutput) error {
				p := &printer{w: w}
				p.f("%s\t%s\t%s\t(alias of io_send)\n", out.ID, out.State, out.Became)
				return p.err
			},
		}),
	)
	return cmd
}

// newStackCmd builds `para stack`: later and return, aliases of later and
// reopen for one release.
func newStackCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "stack", Short: "Aliases of para later and para reopen for one release", Args: cobra.NoArgs}
	cmd.AddCommand(
		stackLaterCmd(l),
		stackReturnCmd(l),
	)
	return cmd
}

// stackLaterCmd is `para stack later`: put stack items off as the owner.
func stackLaterCmd(l *loader) *cobra.Command {
	var items []string
	return opCommand(l, ops.StackLaterOp, binding[ops.StackLaterInput, ops.DeferOutput]{
		Use:  "later [--item <proposal>#<item>]... [--question <id>]... [--until YYYY-MM-DD]",
		Args: cobra.NoArgs,
		Flags: func(fs *pflag.FlagSet, in *ops.StackLaterInput) {
			fs.StringArrayVar(&items, "item", nil, "a dossier item as <proposal>#<item>; repeatable")
			fs.StringArrayVar(&in.Questions, "question", nil, "an open vault question; repeatable")
			fs.StringVar(&in.Until, "until", "", "YYYY-MM-DD it comes back; leave out for someday")
			fs.StringVar(&in.Reason, "reason", "", "why it can wait")
		},
		Bind: func(_ []string, in *ops.StackLaterInput) error {
			var err error
			in.Items, err = itemRefs(items)
			return err
		},
		Text: deferText,
	})
}

// stackReturnCmd is `para stack return`: the undo of stack later.
func stackReturnCmd(l *loader) *cobra.Command {
	var items []string
	return opCommand(l, ops.StackReturn, binding[ops.StackReturnInput, ops.DeferOutput]{
		Use:  "return [--item <proposal>#<item>]... [--question <id>]...",
		Args: cobra.NoArgs,
		Flags: func(fs *pflag.FlagSet, in *ops.StackReturnInput) {
			fs.StringArrayVar(&items, "item", nil, "a dossier item as <proposal>#<item>; repeatable")
			fs.StringArrayVar(&in.Questions, "question", nil, "a vault question; repeatable")
		},
		Bind: func(_ []string, in *ops.StackReturnInput) error {
			var err error
			in.Items, err = itemRefs(items)
			return err
		},
		Text: deferText,
	})
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
