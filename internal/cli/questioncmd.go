package cli

import (
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/parasolve/internal/ops"
)

func writeQuestions(p *printer, indent string, list []ops.QuestionView) {
	for _, v := range list {
		p.f("%s%s\t%s\t%s\t%s\tblocks %d\t%s\n", indent, v.ID, v.Kind, v.Status, scopeText(v), v.Blocking, v.Title)
	}
}

func scopeText(v ops.QuestionView) string {
	if v.ScopeType == ops.ScopeRoot {
		return "root"
	}
	return v.ScopeType + ":" + v.Scope
}

func answerText(w io.Writer, out ops.QuestionAnswerOutput) error {
	p := &printer{w: w}
	p.f("%s\t%s\t%s\n", out.ID, out.Status, dash(out.Choice))
	for _, id := range out.Unblocked {
		p.f("unblocked\t%s\n", id)
	}
	return p.err
}

func listFlags(fs *pflag.FlagSet, in *ops.QuestionListInput) {
	fs.StringVarP(&in.Project, "project", "p", "", "only questions in this project (its own, or its tasks')")
	fs.StringVar(&in.Task, "task", "", "only questions scoped to this task")
	fs.StringVar(&in.Area, "area", "", "only questions in this area (its own, its projects', or their tasks')")
	fs.BoolVar(&in.Root, "root", false, "only vault-level questions")
	fs.StringVar((*string)(&in.Status), "status", "", "only open, answered, or dropped")
	fs.BoolVar(&in.All, "all", false, "include answered and dropped questions")
}

func listText(w io.Writer, out ops.QuestionListOutput) error {
	p := &printer{w: w}
	writeQuestions(p, "", out.Questions)
	return p.err
}

func answerBinding() binding[ops.QuestionAnswerInput, ops.QuestionAnswerOutput] {
	return binding[ops.QuestionAnswerInput, ops.QuestionAnswerOutput]{
		Use:  "answer <id>",
		Args: cobra.ExactArgs(1),
		Flags: func(fs *pflag.FlagSet, in *ops.QuestionAnswerInput) {
			fs.StringVar(&in.Choice, "choice", "", "one of the question's options, or its number")
			fs.StringVar(&in.Text, "text", "", "the answer in words; default: standard input. Never a secret: say where it is kept")
			fs.StringVar(&in.Why, "why", "", "the reason; required for a decision")
			fs.StringVar(&in.Source, "source", "", "where the answer came from: a URL, a meeting, a person")
			fs.StringVar(&in.By, "by", "", "who answered; default you")
		},
		Bind: func(args []string, in *ops.QuestionAnswerInput) error { in.ID = args[0]; return nil },
		Stdin: func(r io.Reader, in *ops.QuestionAnswerInput) (err error) {
			if in.Text == "" && in.Choice == "" {
				in.Text, err = readPiped(r)
			}
			return err
		},
		Text: answerText,
	}
}

// withoutOp makes a shortcut: the command runs its op but is not the one
// command that spells the op's name.
func withoutOp(cmd *cobra.Command, use string) *cobra.Command {
	cmd.Annotations = nil
	cmd.Use = use
	return cmd
}

func newQuestionCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "question", Short: "Open questions: unknowns, decisions, and inputs to answer (a task is work to do)", Args: cobra.NoArgs}
	cmd.AddCommand(
		opCommand(l, ops.QuestionAdd, binding[ops.QuestionAddInput, ops.QuestionAddOutput]{
			Use:  "add <text>",
			Args: cobra.MinimumNArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.QuestionAddInput) {
				fs.StringVarP(&in.Project, "project", "p", "", "scope: a project id")
				fs.StringVar(&in.Task, "task", "", "scope: a task id")
				fs.StringVar(&in.Area, "area", "", "scope: an area id")
				fs.BoolVar(&in.Root, "root", false, "scope: the whole vault")
				fs.StringVar((*string)(&in.Kind), "kind", "", "question (default), decision (needs a reason to answer), or input")
				fs.StringArrayVar(&in.Options, "option", nil, "a choice; repeatable")
				fs.StringVar(&in.Recommended, "recommended", "", "the option you suggest")
			},
			Bind: func(args []string, in *ops.QuestionAddInput) error {
				in.Text = strings.Join(args, " ")
				return nil
			},
			Text: printID(func(o ops.QuestionAddOutput) string { return o.ID }),
		}),
		opCommand(l, ops.QuestionList, binding[ops.QuestionListInput, ops.QuestionListOutput]{
			Use: "list", Args: cobra.NoArgs, Flags: listFlags, Text: listText,
		}),
		opCommand(l, ops.QuestionShow, binding[ops.QuestionShowInput, ops.QuestionShowOutput]{
			Use:  "show <id>",
			Args: cobra.ExactArgs(1),
			Bind: func(args []string, in *ops.QuestionShowInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.QuestionShowOutput) error {
				p := &printer{w: w}
				v := out.Question
				p.f("%s\t%s\t%s\t%s\n", v.ID, v.Kind, v.Status, v.Title)
				p.f("scope\t%s\n", scopeText(v))
				if len(v.Options) > 0 {
					for i, o := range v.Options {
						mark := ""
						if o == v.Recommended {
							mark = "\t(recommended)"
						}
						p.f("option %d\t%s%s\n", i+1, o, mark)
					}
				}
				p.f("rfx\t%s\nblocks\t%s\n%s\n", dash(v.RFX), joinOrDash(v.Blocks), out.Body)
				return p.err
			},
		}),
		opCommand(l, ops.QuestionAnswer, answerBinding()),
		opCommand(l, ops.QuestionDrop, binding[ops.QuestionDropInput, ops.QuestionAnswerOutput]{
			Use:  "drop <id>",
			Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.QuestionDropInput) {
				fs.StringVar(&in.Reason, "reason", "", "why nobody needs the answer now")
			},
			Bind: func(args []string, in *ops.QuestionDropInput) error { in.ID = args[0]; return nil },
			Text: answerText,
		}),
		opCommand(l, ops.QuestionAsk, binding[ops.QuestionAskInput, ops.QuestionAskOutput]{
			Use:  "ask <id> --to <person> --via <medium> --route <domain>",
			Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.QuestionAskInput) {
				fs.StringVar(&in.To, "to", "", "person or group id to ask (required)")
				fs.StringVar((*string)(&in.Via), "via", "", "medium: "+mediumList+" (required)")
				fs.StringVar(&in.Route, "route", "", "domain id (required)")
				fs.StringVar(&in.Value, "value", "", "address or handle; default the person's preferred contact")
				fs.StringVar(&in.SelfServe, "self-serve", "", "what you checked before asking")
				fs.StringVar(&in.NextNudge, "next-nudge", "", "YYYY-MM-DD")
			},
			Bind: func(args []string, in *ops.QuestionAskInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.QuestionAskOutput) error {
				p := &printer{w: w}
				for _, wn := range out.Warnings {
					p.f("warning: %s\n", wn)
				}
				p.f("%s\t%s\n", out.ID, out.RFX)
				return p.err
			},
		}),
	)
	return cmd
}

// newQuestionsCmd is `para questions`: the open questions, the ones that block
// the most first. It runs question_list.
func newQuestionsCmd(l *loader) *cobra.Command {
	return withoutOp(opCommand(l, ops.QuestionList, binding[ops.QuestionListInput, ops.QuestionListOutput]{
		Args: cobra.NoArgs, Flags: listFlags, Text: listText,
	}), "questions")
}

// newAnswerCmd is `para answer <id>`: question_answer.
func newAnswerCmd(l *loader) *cobra.Command {
	return withoutOp(opCommand(l, ops.QuestionAnswer, answerBinding()), "answer <id>")
}
