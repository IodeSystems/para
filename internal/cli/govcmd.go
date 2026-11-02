package cli

import (
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/parasolve/internal/export"
	"github.com/iodesystems/parasolve/internal/ops"
)

// addGovernanceCmds adds the policy, rule, exception, constraint, convention,
// ingest, copy, and export commands.
func addGovernanceCmds(root *cobra.Command, l *loader) {
	root.AddCommand(
		newIngestCmd(l),
		newPolicyCmd(l),
		newRuleCmd(l),
		newExceptionCmd(l),
		newConstraintCmd(l),
		newConventionCmd(l),
		newCopyCmd(l),
		newExportCmd(l),
	)
}

func group(use, short string) *cobra.Command {
	return &cobra.Command{Use: use, Short: short, Args: cobra.NoArgs}
}

// readStdin reads all of r. A terminal would wait for input; the error text
// says what the command expects.
func readStdin(r io.Reader) (string, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return "", fmt.Errorf("read stdin: %w", err)
	}
	return string(b), nil
}

func newIngestCmd(l *loader) *cobra.Command {
	return opCommand(l, ops.Ingest, binding[ops.IngestInput, ops.IngestOutput]{
		Use:  "ingest --url <url> [--title <title>] [--no-copy] < text",
		Args: cobra.NoArgs,
		Flags: func(fs *pflag.FlagSet, in *ops.IngestInput) {
			fs.StringVar(&in.URL, "url", "", "stable source URL (no signed query string)")
			fs.StringVar(&in.Title, "title", "", "title (default: first heading, else URL host and path)")
			fs.BoolVar(&in.NoCopy, "no-copy", false, "store summary + URL only (fidelity none); stdin is not read")
			fs.StringVar(&in.Backend, "backend", "", "backend name (default manual)")
			fs.StringVar(&in.ID, "id", "", "policy id (default generated)")
		},
		Stdin: func(r io.Reader, in *ops.IngestInput) (err error) {
			if in.NoCopy {
				return nil
			}
			in.Text, err = readStdin(r)
			return err
		},
		Text: func(w io.Writer, out ops.IngestOutput) error {
			_, err := fmt.Fprintln(w, out.ID)
			return err
		},
	})
}

func newPolicyCmd(l *loader) *cobra.Command {
	cmd := group("policy", "Policies: summary + transcript + rules")
	cmd.AddCommand(
		opCommand(l, ops.PolicyList, binding[ops.PolicyListInput, ops.PolicyListOutput]{
			Use: "list", Args: cobra.NoArgs,
			Text: func(w io.Writer, out ops.PolicyListOutput) error {
				for _, p := range out.Policies {
					if err := printPolicyLine(w, p); err != nil {
						return err
					}
				}
				return nil
			},
		}),
		opCommand(l, ops.PolicyShow, binding[ops.PolicyShowInput, ops.PolicyShowOutput]{
			Use: "show <id>", Args: cobra.ExactArgs(1),
			Bind: func(args []string, in *ops.PolicyShowInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.PolicyShowOutput) error {
				if err := printPolicyLine(w, out.PolicyView); err != nil {
					return err
				}
				for _, e := range out.Exceptions {
					if _, err := fmt.Fprintf(w, "exception\t%s\t%s\t%s\t%s\n", e.ID, e.Kind, e.Status, e.Rule); err != nil {
						return err
					}
				}
				_, err := fmt.Fprintf(w, "\n%s\n", out.Body)
				return err
			},
		}),
		opCommand(l, ops.PolicyReview, binding[ops.PolicyReviewInput, ops.PolicyReviewOutput]{
			Use: "review <id> --by <name>", Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.PolicyReviewInput) {
				fs.StringVar(&in.By, "by", "", "who reviewed the summary against the source")
			},
			Bind: func(args []string, in *ops.PolicyReviewInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.PolicyReviewOutput) error {
				_, err := fmt.Fprintf(w, "%s reviewed by %s\n", out.ID, out.ReviewedBy)
				return err
			},
		}),
		opCommand(l, ops.PolicyRefresh, binding[ops.PolicyRefreshInput, ops.PolicyRefreshOutput]{
			Use: "refresh <id> < text", Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.PolicyRefreshInput) {
				fs.StringVar(&in.Backend, "backend", "", "backend that read the text (default manual)")
			},
			Bind: func(args []string, in *ops.PolicyRefreshInput) error { in.ID = args[0]; return nil },
			Stdin: func(r io.Reader, in *ops.PolicyRefreshInput) (err error) {
				in.Text, err = readStdin(r)
				return err
			},
			Text: func(w io.Writer, out ops.PolicyRefreshOutput) error {
				if _, err := fmt.Fprintf(w, "%s\t%s\n", out.ID, out.Result); err != nil {
					return err
				}
				for _, r := range out.AffectedRules {
					if _, err := fmt.Fprintf(w, "rule\t%s\t%s\n", r.ID, r.Text); err != nil {
						return err
					}
				}
				for _, e := range out.AffectedExceptions {
					if _, err := fmt.Fprintf(w, "exception\t%s\t%s\t%s\n", e.ID, e.Kind, e.Status); err != nil {
						return err
					}
				}
				return nil
			},
		}),
	)
	return cmd
}

func printPolicyLine(w io.Writer, p ops.PolicyView) error {
	review := "unverified"
	if p.Verified {
		review = "reviewed by " + p.ReviewedBy
	}
	_, err := fmt.Fprintf(w, "%s\t%s\t%s\tsource %s\t%d rules\t%s\n", p.ID, p.Title, review, p.SourceStatus, p.Rules, p.URL)
	return err
}

func newRuleCmd(l *loader) *cobra.Command {
	cmd := group("rule", "Rules of a policy")
	cmd.AddCommand(
		opCommand(l, ops.RuleAdd, binding[ops.RuleAddInput, ops.RuleAddOutput]{
			Use: "add <policy> <text>", Args: cobra.MinimumNArgs(2),
			Bind: func(args []string, in *ops.RuleAddInput) error {
				in.Policy, in.Text = args[0], strings.Join(args[1:], " ")
				return nil
			},
			Text: func(w io.Writer, out ops.RuleAddOutput) error {
				_, err := fmt.Fprintf(w, "%s %s\n", out.ID, out.Link)
				return err
			},
		}),
		opCommand(l, ops.RuleList, binding[ops.RuleListInput, ops.RuleListOutput]{
			Use: "list [policy]", Args: cobra.MaximumNArgs(1),
			Bind: func(args []string, in *ops.RuleListInput) error {
				if len(args) == 1 {
					in.Policy = args[0]
				}
				return nil
			},
			Text: func(w io.Writer, out ops.RuleListOutput) error {
				for _, r := range out.Rules {
					if _, err := fmt.Fprintf(w, "%s\t%s\t%s\n", r.Policy, r.ID, r.Text); err != nil {
						return err
					}
				}
				return nil
			},
		}),
	)
	return cmd
}

func printException(w io.Writer, e ops.ExceptionView) error {
	_, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", e.ID, e.Policy, e.Rule, e.Kind, e.Status, e.ReviewBy, e.Title)
	return err
}

func newExceptionCmd(l *loader) *cobra.Command {
	cmd := group("exception", "Policy exceptions")
	cmd.AddCommand(
		opCommand(l, ops.ExceptionAdd, binding[ops.ExceptionAddInput, ops.ExceptionAddOutput]{
			Use: "add --policy <id> --kind <kind> --by <who> [--rule R4] [--with <id>] [--review-by YYYY-MM-DD] [--title ..] [--text ..]", Args: cobra.NoArgs,
			Flags: func(fs *pflag.FlagSet, in *ops.ExceptionAddInput) {
				fs.StringVar(&in.Policy, "policy", "", "policy id")
				fs.StringVar(&in.Rule, "rule", "", "rule id such as R4")
				fs.StringVar((*string)(&in.Kind), "kind", "", "underspecified|overly_broad|bug|unenforced|conflict_with|interpreted_as")
				fs.StringVar(&in.By, "by", "", "who raised or decided it")
				fs.StringVar(&in.With, "with", "", "document it conflicts with (conflict_with)")
				fs.StringVar(&in.ReviewBy, "review-by", "", "date to look again, YYYY-MM-DD")
				fs.StringVar(&in.Title, "title", "", "title (default from kind)")
				fs.StringVar(&in.Text, "text", "", "body text")
			},
			Text: func(w io.Writer, out ops.ExceptionAddOutput) error {
				_, err := fmt.Fprintln(w, out.ID)
				return err
			},
		}),
		opCommand(l, ops.ExceptionList, binding[ops.ExceptionListInput, ops.ExceptionListOutput]{
			Use: "list", Args: cobra.NoArgs,
			Flags: func(fs *pflag.FlagSet, in *ops.ExceptionListInput) {
				fs.StringVar(&in.Policy, "policy", "", "filter by policy id")
				fs.StringVar((*string)(&in.Status), "status", "", "filter by status")
				fs.StringVar((*string)(&in.Kind), "kind", "", "filter by kind")
			},
			Text: func(w io.Writer, out ops.ExceptionListOutput) error {
				for _, e := range out.Exceptions {
					if err := printException(w, e); err != nil {
						return err
					}
				}
				return nil
			},
		}),
		opCommand(l, ops.ExceptionShow, binding[ops.ExceptionShowInput, ops.ExceptionShowOutput]{
			Use: "show <id>", Args: cobra.ExactArgs(1),
			Bind: func(args []string, in *ops.ExceptionShowInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.ExceptionShowOutput) error {
				if err := printException(w, out.ExceptionView); err != nil {
					return err
				}
				_, err := fmt.Fprintf(w, "by\t%s\nwith\t%s\n\n%s\n", out.By, out.With, out.Body)
				return err
			},
		}),
		opCommand(l, ops.ExceptionExpiring, binding[ops.ExceptionExpiringInput, ops.ExceptionExpiringOutput]{
			Use: "expiring", Args: cobra.NoArgs,
			Flags: func(fs *pflag.FlagSet, in *ops.ExceptionExpiringInput) {
				fs.IntVar(&in.Days, "days", 30, "window in days from today; overdue is always listed")
			},
			Text: func(w io.Writer, out ops.ExceptionExpiringOutput) error {
				for _, e := range out.Exceptions {
					if _, err := fmt.Fprintf(w, "%d days\t", e.DaysLeft); err != nil {
						return err
					}
					if err := printException(w, e.ExceptionView); err != nil {
						return err
					}
				}
				return nil
			},
		}),
	)
	return cmd
}

func printConstraint(w io.Writer, c ops.ConstraintView) error {
	_, err := fmt.Fprintf(w, "%s\t%s\tby %s\treview %s\tin %s\tnot in %s\t%s\n", c.ID, c.Says, c.By, c.ReviewBy,
		strings.Join(c.AppliesIn, ","), strings.Join(c.NotIn, ","), c.SourceURL)
	return err
}

func newConstraintCmd(l *loader) *cobra.Command {
	cmd := group("constraint", "Justified refusals: says, why, by, source, scopes")
	cmd.AddCommand(
		opCommand(l, ops.ConstraintAdd, binding[ops.ConstraintAddInput, ops.ConstraintAddOutput]{
			Use: "add --says .. --why .. --by .. --source <url> --review-by YYYY-MM-DD [--applies-in ..] [--not-in ..]", Args: cobra.NoArgs,
			Flags: func(fs *pflag.FlagSet, in *ops.ConstraintAddInput) {
				fs.StringVar(&in.Says, "says", "", "the no, one line")
				fs.StringVar(&in.Why, "why", "", "the reason given")
				fs.StringVar(&in.By, "by", "", "who said it")
				fs.StringVar(&in.Source, "source", "", "stable URL of where it was said")
				fs.StringSliceVar(&in.AppliesIn, "applies-in", nil, "scope where it holds (repeat or comma-separate)")
				fs.StringSliceVar(&in.NotIn, "not-in", nil, "scope where it does not hold")
				fs.StringVar(&in.ReviewBy, "review-by", "", "date to ask again, YYYY-MM-DD")
				fs.StringVar(&in.Text, "text", "", "body text")
			},
			Text: func(w io.Writer, out ops.ConstraintAddOutput) error {
				_, err := fmt.Fprintln(w, out.ID)
				return err
			},
		}),
		opCommand(l, ops.ConstraintList, binding[ops.ConstraintListInput, ops.ConstraintListOutput]{
			Use: "list", Args: cobra.NoArgs,
			Text: func(w io.Writer, out ops.ConstraintListOutput) error {
				for _, c := range out.Constraints {
					if err := printConstraint(w, c); err != nil {
						return err
					}
				}
				return nil
			},
		}),
		opCommand(l, ops.ConstraintShow, binding[ops.ConstraintShowInput, ops.ConstraintShowOutput]{
			Use: "show <id>", Args: cobra.ExactArgs(1),
			Bind: func(args []string, in *ops.ConstraintShowInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.ConstraintShowOutput) error {
				if err := printConstraint(w, out.ConstraintView); err != nil {
					return err
				}
				_, err := fmt.Fprintf(w, "why\t%s\n\n%s\n", out.Why, out.Body)
				return err
			},
		}),
	)
	return cmd
}

func printConvention(w io.Writer, c ops.ConventionView) error {
	_, err := fmt.Fprintf(w, "%s\t%s\t%s\tin %s\n", c.ID, c.Title, c.Status, strings.Join(c.AppliesIn, ","))
	return err
}

func newConventionCmd(l *loader) *cobra.Command {
	cmd := group("convention", "Conventions")
	cmd.AddCommand(
		opCommand(l, ops.ConventionAdd, binding[ops.ConventionAddInput, ops.ConventionAddOutput]{
			Use: "add <title> [--status draft|adopted|retired] [--applies-in ..] [--text ..]", Args: cobra.MinimumNArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.ConventionAddInput) {
				fs.StringVar((*string)(&in.Status), "status", "", "draft (default), adopted, or retired")
				fs.StringSliceVar(&in.AppliesIn, "applies-in", nil, "scope (repeat or comma-separate)")
				fs.StringVar(&in.Text, "text", "", "body text")
			},
			Bind: func(args []string, in *ops.ConventionAddInput) error {
				in.Title = strings.Join(args, " ")
				return nil
			},
			Text: func(w io.Writer, out ops.ConventionAddOutput) error {
				_, err := fmt.Fprintln(w, out.ID)
				return err
			},
		}),
		opCommand(l, ops.ConventionList, binding[ops.ConventionListInput, ops.ConventionListOutput]{
			Use: "list", Args: cobra.NoArgs,
			Text: func(w io.Writer, out ops.ConventionListOutput) error {
				for _, c := range out.Conventions {
					if err := printConvention(w, c); err != nil {
						return err
					}
				}
				return nil
			},
		}),
		opCommand(l, ops.ConventionShow, binding[ops.ConventionShowInput, ops.ConventionShowOutput]{
			Use: "show <id>", Args: cobra.ExactArgs(1),
			Bind: func(args []string, in *ops.ConventionShowInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.ConventionShowOutput) error {
				if err := printConvention(w, out.ConventionView); err != nil {
					return err
				}
				_, err := fmt.Fprintf(w, "\n%s\n", out.Body)
				return err
			},
		}),
	)
	return cmd
}

func runClipboard(argv []string, stdin string) error {
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdin = strings.NewReader(stdin)
	return cmd.Run()
}

func newCopyCmd(l *loader) *cobra.Command {
	var clipboard bool
	cmd := opCommand(l, ops.Copy, binding[ops.CopyInput, ops.CopyOutput]{
		Use: "copy <id> [--as plain|email|slack|jira] [--clipboard]", Args: cobra.ExactArgs(1),
		Flags: func(fs *pflag.FlagSet, in *ops.CopyInput) {
			fs.StringVar((*string)(&in.As), "as", "plain", "plain, email, slack, or jira")
			fs.BoolVar(&clipboard, "clipboard", false, "put the text on the clipboard (pbcopy, wl-copy, xclip) instead of stdout")
		},
		Bind: func(args []string, in *ops.CopyInput) error {
			in.ID, in.Clipboard = args[0], clipboard
			return nil
		},
		Text: func(w io.Writer, out ops.CopyOutput) error {
			if !clipboard {
				_, err := io.WriteString(w, out.Text)
				return err
			}
			tool, err := export.ClipboardCopy(out.Text, exec.LookPath, runClipboard)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(w, "copied %d characters with %s\n", len(out.Text), tool)
			return err
		},
	})
	run := cmd.RunE
	cmd.RunE = func(c *cobra.Command, args []string) error {
		if j, _ := c.Flags().GetBool("json"); j && clipboard {
			return errors.New("--clipboard and --json cannot be combined: JSON output is not copied")
		}
		return run(c, args)
	}
	return cmd
}
