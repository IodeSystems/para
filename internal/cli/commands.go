package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
	"github.com/iodesystems/para/internal/vault"
)

func newInboxCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "inbox", Short: "Unsorted captures", Args: cobra.NoArgs}
	cmd.AddCommand(
		opCommand(l, ops.InboxAdd, binding[ops.InboxAddInput, ops.InboxAddOutput]{
			Use:  "add <text>",
			Args: cobra.MinimumNArgs(1),
			Bind: func(args []string, in *ops.InboxAddInput) error {
				in.Text = strings.Join(args, " ")
				return nil
			},
			Text: func(w io.Writer, out ops.InboxAddOutput) error {
				_, err := fmt.Fprintln(w, out.ID)
				return err
			},
		}),
		opCommand(l, ops.InboxList, binding[ops.InboxListInput, ops.InboxListOutput]{
			Use:  "list",
			Args: cobra.NoArgs,
			Flags: func(fs *pflag.FlagSet, in *ops.InboxListInput) {
				fs.BoolVar(&in.All, "all", false, "include triaged (archived) items")
			},
			Text: func(w io.Writer, out ops.InboxListOutput) error {
				p := &printer{w: w}
				for _, it := range out.Items {
					p.f("%s\t%s\t%s\t%s\n", it.ID, it.Folder, it.Created, it.Title)
				}
				return p.err
			},
		}),
		opCommand(l, ops.InboxShow, binding[ops.InboxShowInput, ops.InboxShowOutput]{
			Use:  "show <id>",
			Args: cobra.ExactArgs(1),
			Bind: func(args []string, in *ops.InboxShowInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.InboxShowOutput) error {
				_, err := fmt.Fprintf(w, "%s\t%s\t%s\n%s\n", out.Doc.ID, out.Doc.Folder, out.Doc.Title, out.Doc.Body)
				return err
			},
		}),
		newInboxTriageCmd(l),
	)
	return cmd
}

func newInboxTriageCmd(l *loader) *cobra.Command {
	var to, via []string
	return opCommand(l, ops.InboxTriage, binding[ops.InboxTriageInput, ops.InboxTriageOutput]{
		Use:  "triage <id> --to task|rfx|project|note|question|drop",
		Args: cobra.ExactArgs(1),
		Flags: func(fs *pflag.FlagSet, in *ops.InboxTriageInput) {
			fs.StringVar((*string)(&in.To), "to", "", "task, rfx, project, note, question, or drop (required)")
			fs.StringVar(&in.Project, "project", "", "task: project id (or --area); rfx, note: optional")
			fs.StringVar(&in.Owner, "owner", "", "project: owner (default the vault owner, else self)")
			fs.StringVar((*string)(&in.Direction), "direction", "", "rfx: in or out (default out)")
			fs.StringVar(&in.Route, "route", "", "rfx: domain id (required)")
			fs.StringArrayVar(&to, "target", nil, "rfx: person or group id asked; pairs with --via, repeatable")
			fs.StringArrayVar(&via, "via", nil, "rfx: medium for the matching --target: "+mediumList)
			fs.StringVar(&in.SelfServe, "self-serve", "", "rfx: what you checked before asking")
			fs.StringVar(&in.NextNudge, "next-nudge", "", "rfx: YYYY-MM-DD")
			fs.StringVar(&in.Area, "area", "", "task, project, or note: area id")
			fs.StringVar(&in.Person, "person", "", "note: person id; makes a dated line in their notes, not a note document")
			fs.StringVar(&in.Date, "date", "", "note: YYYY-MM-DD; default today")
			fs.StringVar(&in.Task, "task", "", "question: scope to this task (or --project, --area, --root: exactly one)")
			fs.BoolVar(&in.Root, "root", false, "question: scope to the whole vault")
			fs.StringVar((*string)(&in.Kind), "kind", "", "question: question, decision, or input")
			fs.StringArrayVar(&in.Options, "option", nil, "question: a choice; repeatable")
			fs.StringVar(&in.Recommended, "recommended", "", "question: the option you suggest")
		},
		Bind: func(args []string, in *ops.InboxTriageInput) error {
			in.ID = args[0]
			if len(to) != len(via) {
				return fmt.Errorf("%d --target and %d --via: give one --via for each --target", len(to), len(via))
			}
			in.Targets = nil
			for i := range to {
				in.Targets = append(in.Targets, ops.TargetInput{To: to[i], Medium: model.Medium(via[i])})
			}
			return nil
		},
		Text: func(w io.Writer, out ops.InboxTriageOutput) error {
			p := &printer{w: w}
			for _, wn := range out.Warnings {
				p.f("warning: %s\n", wn)
			}
			p.f("%s\t%s\t%s\t%s\n", out.ID, out.To, out.Created, out.Folder)
			return p.err
		},
	})
}

// newLintCmd wraps the lint op. With a path it lints files directly, needs no
// vault selection, and is not part of the op: a filesystem path has no
// meaning on a remote store or an agent surface.
func newLintCmd(l *loader) *cobra.Command {
	text := func(w io.Writer, out ops.LintOutput) error {
		for _, f := range out.Findings {
			if _, err := fmt.Fprintln(w, f); err != nil {
				return err
			}
		}
		_, err := fmt.Fprintf(w, "%d errors, %d warnings\n", out.Errors, out.Warnings)
		return err
	}
	cmd := opCommand(l, ops.Lint, binding[ops.LintInput, ops.LintOutput]{
		Use:  "lint [path]",
		Args: cobra.MaximumNArgs(1),
		Text: text,
	})
	runVault := cmd.RunE
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 {
			return runVault(cmd, args)
		}
		findings, err := vault.LintPath(args[0])
		if err != nil {
			return err
		}
		return emit(cmd, ops.NewLintOutput(findings), text)
	}
	return cmd
}

func newReconcileCmd(l *loader) *cobra.Command {
	return opCommand(l, ops.Reconcile, binding[ops.ReconcileInput, model.ReconcileResult]{
		Use:  "reconcile",
		Args: cobra.NoArgs,
		Text: func(w io.Writer, out model.ReconcileResult) error {
			if out.Baseline {
				if _, err := fmt.Fprintln(w, "no manifest; current files are the new baseline"); err != nil {
					return err
				}
			}
			for _, c := range out.Changes {
				if _, err := fmt.Fprintf(w, "%s\t%s\n", c.Kind, c.Path); err != nil {
					return err
				}
			}
			return nil
		},
	})
}

func newEventsCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "events", Short: "Read the vault event log", Args: cobra.NoArgs}
	cmd.AddCommand(opCommand(l, ops.EventsTail, binding[ops.EventsTailInput, ops.EventsTailOutput]{
		Use:  "tail",
		Args: cobra.NoArgs,
		Flags: func(fs *pflag.FlagSet, in *ops.EventsTailInput) {
			fs.IntVarP(&in.Limit, "limit", "n", 20, "number of events")
		},
		Text: func(w io.Writer, out ops.EventsTailOutput) error {
			for _, e := range out.Events {
				if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					e.TS.Format("2006-01-02T15:04:05Z"), e.Actor, e.Surface, e.Op, e.Doc, e.From, e.To); err != nil {
					return err
				}
			}
			return nil
		},
	}))
	return cmd
}
