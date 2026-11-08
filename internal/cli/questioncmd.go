package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
)

// optionDetailFlags reads a decision's option weighing: whole options as JSON
// (--option-detail), and percents and a basis per option name.
type optionDetailFlags struct {
	json, rec, conf, basis []string
}

// details merges the flags into one OptionDetail per option, in the order
// the options were first named.
func (f *optionDetailFlags) details() ([]model.OptionDetail, error) {
	var out []model.OptionDetail
	at := func(name string) *model.OptionDetail {
		for i := range out {
			if out[i].Name == name {
				return &out[i]
			}
		}
		out = append(out, model.OptionDetail{Name: name})
		return &out[len(out)-1]
	}
	for _, s := range f.json {
		var o model.OptionDetail
		if err := json.Unmarshal([]byte(s), &o); err != nil {
			return nil, fmt.Errorf("--option-detail: %w", err)
		}
		if strings.TrimSpace(o.Name) == "" {
			return nil, fmt.Errorf("--option-detail: name is required")
		}
		if i := slices.IndexFunc(out, func(x model.OptionDetail) bool { return x.Name == o.Name }); i >= 0 {
			return nil, fmt.Errorf("--option-detail: %q is given twice", o.Name)
		}
		out = append(out, o)
	}
	pair := func(flag, s string) (string, string, error) {
		k, v, ok := strings.Cut(s, "=")
		if !ok || strings.TrimSpace(k) == "" || strings.TrimSpace(v) == "" {
			return "", "", fmt.Errorf("--%s %q: want <option>=<value>", flag, s)
		}
		return strings.TrimSpace(k), strings.TrimSpace(v), nil
	}
	percents := func(flag string, list []string, set func(*model.OptionDetail, int)) error {
		for _, s := range list {
			k, v, err := pair(flag, s)
			if err != nil {
				return err
			}
			n, err := strconv.Atoi(strings.TrimSuffix(v, "%"))
			if err != nil {
				return fmt.Errorf("--%s %q: want a percent from 0 to 100", flag, s)
			}
			set(at(k), n)
		}
		return nil
	}
	if err := percents("recommendation", f.rec, func(o *model.OptionDetail, n int) { o.Recommendation = n }); err != nil {
		return nil, err
	}
	if err := percents("option-confidence", f.conf, func(o *model.OptionDetail, n int) { o.Confidence = n }); err != nil {
		return nil, err
	}
	for _, s := range f.basis {
		k, v, err := pair("option-basis", s)
		if err != nil {
			return nil, err
		}
		at(k).ConfidenceBasis = v
	}
	return out, nil
}

func writeQuestions(p *printer, indent string, list []ops.QuestionView) {
	for _, v := range list {
		p.f("%s%s\t%s\t%s\t%s\tblocks %d\t%s\n", indent, v.ID, v.Kind, stateText(v), scopeText(v), v.Blocking, v.Title)
	}
}

// stateText is the status, or "proposed answer" for an old agent answer that
// still waits for a person, with "waiting" when the question waits on other work.
func stateText(v ops.QuestionView) string {
	s := string(v.Status)
	if v.HasProposal {
		s = "proposed answer"
	}
	if v.Deferred {
		if len(v.WaitsOn) > 0 {
			s += " (later, waits on: " + strings.Join(namesOrIDs(v.WaitsOnNames, v.WaitsOn), ", ") + ")"
		} else {
			s += " (later: " + v.DeferUntil + ")"
		}
	}
	if v.AgentLooking {
		s += " (agent looking)"
	}
	if len(v.WaitingOn) > 0 {
		s += " (waiting)"
	}
	return s
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
	fs.BoolVar(&in.Deferred, "deferred", false, "include the questions put off with defer (Later); they are marked")
}

func listText(w io.Writer, out ops.QuestionListOutput) error {
	p := &printer{w: w}
	writeQuestions(p, "", out.Questions)
	return p.err
}

func answerBinding() binding[ops.QuestionAnswerInput, ops.QuestionAnswerOutput] {
	var refs, quotes []string
	return binding[ops.QuestionAnswerInput, ops.QuestionAnswerOutput]{
		Use:  "answer <id>",
		Args: cobra.ExactArgs(1),
		Flags: func(fs *pflag.FlagSet, in *ops.QuestionAnswerInput) {
			fs.StringVar(&in.Choice, "choice", "", "one of the question's options, or its number")
			fs.StringVar(&in.Text, "text", "", "the answer in words; default: standard input. Never a secret: say where it is kept")
			fs.StringVar(&in.Why, "why", "", "the reason; required for a decision")
			fs.StringVar(&in.Source, "source", "", "where the answer came from: a URL, a meeting, a person")
			fs.StringVar(&in.By, "by", "", "who answered; default you")
			fs.StringArrayVar(&refs, "ref", nil, "a source: a URL, a document id, or free text such as a meeting; repeatable (an agent must give one)")
			fs.StringArrayVar(&quotes, "quote", nil, "the quote for the ref at the same position; repeatable")
		},
		Bind: func(args []string, in *ops.QuestionAnswerInput) error {
			in.ID = args[0]
			if len(quotes) > len(refs) {
				return fmt.Errorf("--quote given %d times, --ref %d: each quote goes with a ref", len(quotes), len(refs))
			}
			in.Sources = nil
			for i, r := range refs {
				s := model.AnswerSource{Ref: r}
				if i < len(quotes) {
					s.Quote = quotes[i]
				}
				in.Sources = append(in.Sources, s)
			}
			return nil
		},
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
	var od optionDetailFlags
	cmd := &cobra.Command{Use: "question", Short: "Open questions: unknowns, decisions, and inputs to answer (a task is work to do)", Args: cobra.NoArgs}
	cmd.AddCommand(
		opCommand(l, ops.QuestionReview, binding[ops.QuestionReviewInput, ops.QuestionReviewOutput]{
			Use:  "review <id> answer|decline|skip|ask",
			Args: cobra.ExactArgs(2),
			Flags: func(fs *pflag.FlagSet, in *ops.QuestionReviewInput) {
				fs.StringArrayVar(&in.Choices, "choice", nil, "answer: an option picked (repeatable)")
				fs.StringArrayVar(&in.AddedOptions, "add-option", nil, "answer: an option to add (repeatable)")
				fs.StringVar(&in.OwnWords, "own-words", "", "answer: your own words")
				fs.StringVar(&in.Note, "note", "", "required to decline or ask")
				fs.StringArrayVar(&in.Refs, "ref", nil, "ask: a vault id the agent should read (repeatable)")
			},
			Bind: func(args []string, in *ops.QuestionReviewInput) error {
				in.ID, in.Decision = args[0], args[1]
				return nil
			},
			Text: func(w io.Writer, out ops.QuestionReviewOutput) error {
				p := &printer{w: w}
				p.f("%s\t%s\t%s\t%s\n", out.ID, out.Status, dash(out.Review), dash(out.Request))
				return p.err
			},
		}),
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
				fs.StringVar(&in.Context, "context", "", "what is needed to decide")
				fs.StringVar((*string)(&in.Effort), "effort", "", effortHelp)
				fs.StringArrayVar(&od.json, "option-detail", nil, "one option weighed, as JSON: {name, pros, cons, cost, benefit, confidence, confidence_basis, recommendation, examples}; repeatable")
				fs.StringArrayVar(&od.rec, "recommendation", nil, "<option>=<percent>: how much you recommend the option; repeatable")
				fs.StringArrayVar(&od.conf, "option-confidence", nil, "<option>=<percent>: how sure you are of the option's weighing; repeatable")
				fs.StringArrayVar(&od.basis, "option-basis", nil, "<option>=<text>: what the confidence rests on; repeatable")
			},
			Bind: func(args []string, in *ops.QuestionAddInput) error {
				in.Text = strings.Join(args, " ")
				var err error
				in.OptionDetails, err = od.details()
				return err
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
				p.f("%s\t%s\t%s\t%s\n", v.ID, v.Kind, stateText(v), v.Title)
				p.f("scope\t%s\n", scopeText(v))
				p.f("depends on\t%s\nwaiting on\t%s\n", joinOrDash(v.DependsOn), joinOrDash(v.WaitingOn))
				if v.HasProposal {
					p.f("proposed by\t%s\nproposed\t%s %s\n", v.ProposedBy, v.ProposedChoice, v.ProposedAnswer)
					if v.ProposedWhy != "" {
						p.f("why\t%s\n", v.ProposedWhy)
					}
					for _, s := range v.ProposedSources {
						p.f("source\t%s\t%s\n", s.Ref, s.Quote)
					}
				}
				if v.RejectedReason != "" {
					p.f("rejected\t%s: %s\n", v.RejectedBy, v.RejectedReason)
				}
				if v.Context != "" {
					p.f("context\t%s\n", v.Context)
				}
				for i, o := range v.Options {
					mark := ""
					if o == v.Recommended {
						mark = "\t(recommended)"
					}
					p.f("option %d\t%s%s\n", i+1, o, mark)
					for _, od := range v.OptionDetails {
						if od.Name == o {
							writeOptionDetail(p, od)
						}
					}
				}
				p.f("rfx\t%s\nblocks\t%s\n%s\n", dash(v.RFX), joinOrDash(v.Blocks), out.Body)
				return p.err
			},
		}),
		opCommand(l, ops.QuestionAnswer, answerBinding()),
		opCommand(l, ops.QuestionEdit, binding[ops.QuestionEditInput, ops.QuestionEditOutput]{
			Use:  "edit <id>",
			Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.QuestionEditInput) {
				fs.StringVar(&in.Title, "title", "", "new title")
				fs.StringArrayVar(&in.Options, "option", nil, "replace the options; repeatable")
				fs.BoolVar(&in.ClearOptions, "clear-options", false, "remove all options")
				fs.StringVar(&in.Recommended, "recommended", "", "the option you suggest")
				fs.StringVar(&in.Context, "context", "", "what is needed to decide")
				fs.BoolVar(&in.ClearRecommended, "clear-recommended", false, "remove the recommendation")
				fs.StringVar((*string)(&in.Effort), "effort", "", effortHelp)
			},
			Bind: func(args []string, in *ops.QuestionEditInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.QuestionEditOutput) error {
				p := &printer{w: w}
				p.f("%s\t%s\t%s\n", out.ID, out.Title, joinOrDash(out.Options))
				return p.err
			},
		}),
		newQuestionDependsCmd(l),
		opCommand(l, ops.QuestionDrop, binding[ops.QuestionDropInput, ops.QuestionAnswerOutput]{
			Use:  "drop <id>",
			Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.QuestionDropInput) {
				fs.StringVar(&in.Reason, "reason", "", "why nobody needs the answer now")
			},
			Bind: func(args []string, in *ops.QuestionDropInput) error { in.ID = args[0]; return nil },
			Text: answerText,
		}),
		questionInvestigateCmd(l),
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

func newQuestionDependsCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "depends", Short: "Edit what a question waits on", Args: cobra.NoArgs}
	text := func(w io.Writer, out ops.QuestionDependsOutput) error {
		_, err := fmt.Fprintf(w, "%s\t%s\n", out.ID, joinOrDash(out.DependsOn))
		return err
	}
	bind := func(args []string, in *ops.QuestionDependsInput) error {
		in.ID, in.DependsOn = args[0], args[1:]
		return nil
	}
	cmd.AddCommand(
		opCommand(l, ops.QuestionDependsAdd, binding[ops.QuestionDependsInput, ops.QuestionDependsOutput]{
			Use: "add <id> <depends-on>...", Args: cobra.MinimumNArgs(2), Bind: bind, Text: text,
		}),
		opCommand(l, ops.QuestionDependsRemove, binding[ops.QuestionDependsInput, ops.QuestionDependsOutput]{
			Use: "remove <id> <depends-on>...", Args: cobra.MinimumNArgs(2), Bind: bind, Text: text,
		}),
	)
	return cmd
}

func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// investigateText prints the request an investigate queued or added to.
func investigateText(w io.Writer, out ops.InvestigateOutput) error {
	how := "queued"
	if out.Appended {
		how = "added to"
	}
	_, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\trunner %s\n", out.ID, out.Request, how, out.Status, out.Runner)
	return err
}

// questionInvestigateCmd is `para question investigate <id> --how <text>`, a
// second spelling of the investigate op. It carries no op annotation: the op's
// one command is `para investigate`.
func questionInvestigateCmd(l *loader) *cobra.Command {
	cmd := opCommand(l, ops.Investigate, binding[ops.InvestigateInput, ops.InvestigateOutput]{
		Use:  "investigate <id> [--how <text>]",
		Args: cobra.ExactArgs(1),
		Flags: func(fs *pflag.FlagSet, in *ops.InvestigateInput) {
			fs.StringVar(&in.Hint, "how", "", "ask the agent about the question, or where to look (optional)")
			fs.StringVar(&in.Runner, "runner", "", "install id that may claim the work; default as request add")
		},
		Bind: func(args []string, in *ops.InvestigateInput) error { in.ID = args[0]; return nil },
		Text: investigateText,
	})
	cmd.Annotations = nil
	return cmd
}

func writeOptionDetail(p *printer, o model.OptionDetail) {
	for _, c := range o.Pros {
		p.f("  pro\t%s [%s]\n", c.Text, c.Source)
	}
	for _, c := range o.Cons {
		p.f("  con\t%s [%s]\n", c.Text, c.Source)
	}
	if o.Cost != nil {
		p.f("  cost\t%s [%s]\n", o.Cost.Text, o.Cost.Source)
	}
	if o.Benefit != nil {
		p.f("  benefit\t%s [%s]\n", o.Benefit.Text, o.Benefit.Source)
	}
	for _, e := range o.Examples {
		p.f("  %s\t%s [%s]\n", e.Kind, e.Text, e.Source)
	}
	if o.Confidence > 0 {
		p.f("  confidence\t%d%% %s\n", o.Confidence, o.ConfidenceBasis)
	}
	if o.Recommendation > 0 {
		p.f("  recommendation\t%d%%\n", o.Recommendation)
	}
}
