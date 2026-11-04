package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
)

func newRFXCmd(l *loader) *cobra.Command {
	var to, via []string
	cmd := &cobra.Command{Use: "rfx", Short: "Asks: requests for something, in or out", Args: cobra.NoArgs}
	cmd.AddCommand(
		opCommand(l, ops.RFXNudge, binding[ops.RFXNudgeInput, ops.RFXNudgeOutput]{
			Use: "nudge <id> [YYYY-MM-DD]", Args: cobra.RangeArgs(1, 2),
			Bind: func(args []string, in *ops.RFXNudgeInput) error {
				in.ID = args[0]
				if len(args) == 2 {
					in.Date = args[1]
				}
				return nil
			},
			Text: printID(func(o ops.RFXNudgeOutput) string { return o.ID + "\t" + o.NextNudge }),
		}),
		opCommand(l, ops.RFXAdd, binding[ops.RFXAddInput, ops.RFXAddOutput]{
			Use:  "add <ask>",
			Args: cobra.MinimumNArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.RFXAddInput) {
				fs.StringVar((*string)(&in.Direction), "direction", "out", "in (someone asks you) or out (you ask)")
				fs.StringArrayVar(&to, "to", nil, "person or group id asked (out) or who asked (in); pairs with --via, repeatable")
				fs.StringArrayVar(&via, "via", nil, "medium for the matching --to: "+mediumList)
				fs.StringVar(&in.Route, "route", "", "domain id (see `para route`)")
				fs.StringVar(&in.SelfServe, "self-serve", "", "what you checked before asking")
				fs.StringVar(&in.Project, "project", "", "project id")
				fs.StringSliceVar(&in.Blocks, "blocks", nil, "ids this blocks")
				fs.StringSliceVar(&in.DependsOn, "depends-on", nil, "RFX ids that must resolve first")
				fs.StringVar(&in.NextNudge, "next-nudge", "", "YYYY-MM-DD")
			},
			Bind: func(args []string, in *ops.RFXAddInput) error {
				in.Ask = strings.Join(args, " ")
				if len(to) != len(via) {
					return fmt.Errorf("%d --to and %d --via: give one --via for each --to", len(to), len(via))
				}
				in.Targets = nil
				for i := range to {
					in.Targets = append(in.Targets, ops.TargetInput{To: to[i], Medium: model.Medium(via[i])})
				}
				return nil
			},
			Text: func(w io.Writer, out ops.RFXAddOutput) error {
				// Warnings are part of the JSON; in text mode they go on stdout too, one per line.
				for _, wn := range out.Warnings {
					if _, err := fmt.Fprintln(w, "warning:", wn); err != nil {
						return err
					}
				}
				_, err := fmt.Fprintln(w, out.ID)
				return err
			},
		}),
		opCommand(l, ops.RFXList, binding[ops.RFXListInput, ops.RFXListOutput]{
			Use:  "list",
			Args: cobra.NoArgs,
			Flags: func(fs *pflag.FlagSet, in *ops.RFXListInput) {
				fs.StringVar((*string)(&in.Direction), "direction", "", "in or out")
				fs.StringVar((*string)(&in.Status), "status", "", "open, answered, closed, declined")
				fs.StringVar(&in.Route, "route", "", "domain id")
			},
			Text: func(w io.Writer, out ops.RFXListOutput) error { return writeRFX(w, out.Items) },
		}),
		opCommand(l, ops.RFXShow, binding[ops.RFXShowInput, ops.RFXShowOutput]{
			Use:  "show <id>",
			Args: cobra.ExactArgs(1),
			Bind: func(args []string, in *ops.RFXShowInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.RFXShowOutput) error {
				d := out.Doc
				m := d.Meta.(*model.RFX)
				p := &printer{w: w}
				p.f("%s\t%s\n", d.ID, d.Title)
				p.f("direction\t%s\nstatus\t%s\nroute\t%s\nnext_nudge\t%s\nplaybook\t%s\nself_serve_check\t%s\n",
					m.Direction, m.Status, m.Route, m.NextNudge, m.Playbook.Target(), m.SelfServeCheck)
				for _, t := range m.Targets {
					p.f("target\t%s\n", targetText(t))
				}
				for _, c := range out.Chain {
					p.f("%sdepends_on\t%s\t%s\t%s\n", strings.Repeat("  ", c.Depth-1), c.ID, c.Status, c.Title)
				}
				for _, id := range out.DependedOnBy {
					p.f("depended_on_by\t%s\n", id)
				}
				if out.Draft != "" {
					p.f("draft\n%s\n", out.Draft)
				}
				return p.err
			},
		}),
		opCommand(l, ops.RFXStatus, binding[ops.RFXStatusInput, ops.RFXStatusOutput]{
			Use:  "status <id> <open|answered|declined>",
			Args: cobra.ExactArgs(2),
			Bind: func(args []string, in *ops.RFXStatusInput) error {
				in.ID, in.Status = args[0], model.RFXStatus(args[1])
				return nil
			},
			Text: writeStatus,
		}),
		opCommand(l, ops.RFXClose, binding[ops.RFXCloseInput, ops.RFXStatusOutput]{
			Use:  "close <id>",
			Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.RFXCloseInput) {
				fs.StringVar(&in.Playbook, "playbook", "", "playbook id (required for an inbound request)")
			},
			Bind: func(args []string, in *ops.RFXCloseInput) error { in.ID = args[0]; return nil },
			Text: writeStatus,
		}),
		opCommand(l, ops.RFXWaiting, binding[ops.RFXWaitingInput, ops.RFXWaitingOutput]{
			Use:  "waiting",
			Args: cobra.NoArgs,
			Flags: func(fs *pflag.FlagSet, in *ops.RFXWaitingInput) {
				fs.StringVar((*string)(&in.Direction), "direction", "", "in or out; default both")
				fs.StringVar(&in.Today, "today", "", "YYYY-MM-DD; default the local date")
			},
			Text: func(w io.Writer, out ops.RFXWaitingOutput) error {
				for _, it := range out.Items {
					mark := ""
					switch {
					case len(it.BlockedOn) > 0:
						mark = "blocked on " + strings.Join(it.BlockedOn, ",")
					case it.Due:
						mark = "due"
					}
					if it.Stalled {
						mark = strings.TrimSpace("stalled " + mark)
					}
					if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", it.NextNudge, it.ID, it.Direction, it.Title, mark); err != nil {
						return err
					}
					for _, t := range it.Targets {
						if _, err := fmt.Fprintf(w, "\ttarget\t%s\n", targetText(t)); err != nil {
							return err
						}
					}
				}
				return nil
			},
		}),
		opCommand(l, ops.RFXReply, binding[ops.RFXReplyInput, ops.RFXReplyOutput]{
			Use:  "reply <id> --from <target> --outcome answered|no|partial [--text <text>] [--why <reason>]",
			Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.RFXReplyInput) {
				fs.StringVar(&in.From, "from", "", "person or group id that replied (required)")
				fs.StringVar((*string)(&in.Via), "via", "", "medium, when the target has several")
				fs.StringVar((*string)(&in.Outcome), "outcome", "", "answered, no, or partial (required)")
				fs.StringVar(&in.Text, "text", "", "the reply, or your summary of it; default: standard input")
				fs.StringVar(&in.URL, "url", "", "link to the original reply (required for no unless the target has a ref URL)")
				fs.StringVar(&in.Date, "date", "", "YYYY-MM-DD; default today")
				fs.StringVar(&in.Why, "why", "", "outcome no: the reason given (required)")
				fs.StringVar(&in.Says, "says", "", "outcome no: the no itself; default \"No: <request title>\"")
				fs.StringSliceVar(&in.AppliesIn, "applies-in", nil, "outcome no: scopes where it holds")
				fs.StringSliceVar(&in.NotIn, "not-in", nil, "outcome no: scopes where it does not")
				fs.StringVar(&in.ReviewBy, "review-by", "", "outcome no: YYYY-MM-DD; default 180 days out")
			},
			Bind: func(args []string, in *ops.RFXReplyInput) error { in.ID = args[0]; return nil },
			Stdin: func(r io.Reader, in *ops.RFXReplyInput) (err error) {
				if in.Text == "" {
					in.Text, err = readPiped(r)
				}
				return err
			},
			Text: func(w io.Writer, out ops.RFXReplyOutput) error {
				p := &printer{w: w}
				for _, wn := range out.Warnings {
					p.f("warning: %s\n", wn)
				}
				p.f("%s\t%s\t%s\tinteraction %s\tconstraint %s\n", out.ID, out.From, out.State, out.Interaction, out.Constraint)
				return p.err
			},
		}),
		opCommand(l, ops.RFXDraft, binding[ops.RFXDraftInput, ops.RFXDraftOutput]{
			Use:  "draft <id> [--text <text>]",
			Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.RFXDraftInput) {
				fs.StringVar(&in.Text, "text", "", "the draft message; default: standard input. Replaces the earlier draft; nothing is sent")
			},
			Bind: func(args []string, in *ops.RFXDraftInput) error { in.ID = args[0]; return nil },
			Stdin: func(r io.Reader, in *ops.RFXDraftInput) (err error) {
				if in.Text == "" {
					in.Text, err = readPiped(r)
				}
				return err
			},
			Text: func(w io.Writer, out ops.RFXDraftOutput) error {
				_, err := fmt.Fprintln(w, out.ID)
				return err
			},
		}),
		newRFXTargetCmd(l),
		opCommand(l, ops.RFXPaths, binding[ops.RFXPathsInput, ops.RFXPathsOutput]{
			Use:  "paths <id>",
			Args: cobra.ExactArgs(1),
			Bind: func(args []string, in *ops.RFXPathsInput) error { in.ID = args[0]; return nil },
			Text: writePaths,
		}),
		newRFXPathCmd(l),
	)
	return cmd
}

// readPiped reads r unless it is a terminal, which would wait for the user.
// An empty result makes the op say what is missing.
func readPiped(r io.Reader) (string, error) {
	if f, ok := r.(*os.File); ok {
		if st, err := f.Stat(); err == nil && st.Mode()&os.ModeCharDevice != 0 {
			return "", nil
		}
	}
	return readStdin(r)
}

func newRFXTargetCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "target", Short: "Who an ask goes to, and by what medium", Args: cobra.NoArgs}
	cmd.AddCommand(
		opCommand(l, ops.RFXTargetAdd, binding[ops.RFXTargetAddInput, ops.RFXTargetOutput]{
			Use:  "add <rfx-id> <to> <medium>",
			Args: cobra.ExactArgs(3),
			Flags: func(fs *pflag.FlagSet, in *ops.RFXTargetAddInput) {
				fs.StringVar(&in.Value, "value", "", "address or handle; default the person's preferred contact for the medium")
			},
			Bind: func(args []string, in *ops.RFXTargetAddInput) error {
				in.ID, in.To, in.Medium = args[0], args[1], model.Medium(args[2])
				return nil
			},
			Text: writeTargets,
		}),
		opCommand(l, ops.RFXTargetState, binding[ops.RFXTargetStateInput, ops.RFXTargetOutput]{
			Use:  "state <rfx-id> <to> <drafted|sent|replied|no_reply>",
			Args: cobra.ExactArgs(3),
			Flags: func(fs *pflag.FlagSet, in *ops.RFXTargetStateInput) {
				fs.StringVar((*string)(&in.Medium), "via", "", "medium, when the target has several")
				fs.StringVar(&in.Ref, "ref", "", "thread or ticket URL or id")
				fs.StringVar(&in.Date, "date", "", "YYYY-MM-DD it was sent; default today")
				fs.StringVar((*string)(&in.Reason), "reason", "", "friction code of a stall: "+reasonList+" (no_reply records no_response)")
				fs.StringVar(&in.Why, "why", "", "the reason in words")
				fs.StringVar(&in.About, "about", "", "policy, exception, or domain that applies")
			},
			Bind: func(args []string, in *ops.RFXTargetStateInput) error {
				in.ID, in.To, in.State = args[0], args[1], model.TargetState(args[2])
				return nil
			},
			Text: writeTargets,
		}),
	)
	return cmd
}

func newRFXPathCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "path", Short: "Next moves on a stalled ask", Args: cobra.NoArgs}
	cmd.AddCommand(opCommand(l, ops.RFXPathAdd, binding[ops.RFXPathAddInput, ops.RFXPathAddOutput]{
		Use:  "add <rfx-id> <to>",
		Args: cobra.ExactArgs(2),
		Flags: func(fs *pflag.FlagSet, in *ops.RFXPathAddInput) {
			fs.StringVar((*string)(&in.Medium), "medium", "", "how it goes: "+mediumList)
			fs.StringVar(&in.Value, "value", "", "address or handle; default the person's preferred contact")
			fs.StringVar((*string)(&in.Reason), "reason", "", "why it moves: "+reasonList)
			fs.StringVar(&in.Why, "why", "", "the reason in one line")
			fs.StringVar(&in.About, "about", "", "policy, exception, or domain that applies")
			fs.StringVar(&in.Nudge, "nudge", "", "new follow-up date YYYY-MM-DD")
			fs.BoolVar(&in.Override, "override", false, "repeat inside the person's away_until or quiet_days (a person only)")
		},
		Bind: func(args []string, in *ops.RFXPathAddInput) error { in.ID, in.To = args[0], args[1]; return nil },
		Text: func(w io.Writer, out ops.RFXPathAddOutput) error {
			if out.Held != "" {
				if _, err := fmt.Fprintf(w, "override: %s\n", out.Held); err != nil {
					return err
				}
			}
			return writeTargets(w, ops.RFXTargetOutput{Targets: out.Targets})
		},
	}))
	return cmd
}

func writePaths(w io.Writer, out ops.RFXPathsOutput) error {
	p := &printer{w: w}
	p.f("%s\t%s\t%s\n", out.ID, out.Status, out.Title)
	if out.Stalled {
		p.f("stalled\t%s\n", out.StalledWhy)
	}
	for _, e := range out.Log {
		p.f("tried\t%s\t%s\t%s\tadded %s\tsent %s\t%s\tasked %d times, last %s\n", e.To, e.Medium, e.State, dash(e.Added), dash(e.Sent), dash(string(e.Reason)), e.Contacts, dash(e.Last))
	}
	for _, m := range out.Moves {
		hold := ""
		if m.Hold != "" {
			hold = "\tHELD: " + m.Hold
		}
		p.f("move\t%s\t%s%s\n", m.Kind, m.Label, hold)
		p.f("\t%s\n", m.Why)
		if m.Command != "" {
			p.f("\t$ %s\n", m.Command)
		}
	}
	return p.err
}

// reasonList is the list of friction codes for flag help.
var reasonList = strings.Join(strs(model.FrictionReasons), ", ")

// mediumList is the list of media for flag help.
var mediumList = strings.Join(strs(model.Mediums), ", ")

func strs[T ~string](in []T) []string {
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = string(v)
	}
	return out
}

func targetText(t model.Target) string {
	return fmt.Sprintf("%s\t%s\t%s\t%s\t%s\t%s", t.To.Target(), t.Medium, t.State, t.Value, t.Sent, t.Ref)
}

func writeTargets(w io.Writer, out ops.RFXTargetOutput) error {
	p := &printer{w: w}
	for _, t := range out.Targets {
		p.f("%s\n", targetText(t))
	}
	return p.err
}

func writeStatus(w io.Writer, out ops.RFXStatusOutput) error {
	_, err := fmt.Fprintf(w, "%s\t%s -> %s\n", out.ID, out.From, out.Status)
	return err
}

func writeRFX(w io.Writer, items []ops.RFXSummary) error {
	for _, s := range items {
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", s.ID, s.Direction, s.Status, s.Route, s.NextNudge, s.Title); err != nil {
			return err
		}
	}
	return nil
}

func newOrgCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "org", Short: "Who owns what: facts only", Args: cobra.NoArgs}
	kind := func(name string, add *ops.Op[ops.OrgAddInput, ops.OrgAddOutput], list *ops.Op[ops.OrgListInput, ops.OrgListOutput], show *ops.Op[ops.OrgShowInput, ops.OrgShowOutput]) {
		sub := &cobra.Command{Use: name, Short: "Manage " + name + " records", Args: cobra.NoArgs}
		sub.AddCommand(
			opCommand(l, add, binding[ops.OrgAddInput, ops.OrgAddOutput]{
				Use:  "add <name>",
				Args: cobra.MinimumNArgs(1),
				Flags: func(fs *pflag.FlagSet, in *ops.OrgAddInput) {
					fs.StringVar(&in.Org, "org", "", "organization (person, team, role)")
					fs.StringVar(&in.ID, "id", "", "document id; default from the name")
					if name == "domain" {
						fs.StringVar(&in.Owner, "owner", "", "id of the owning role, team, or person")
					}
					if name == "person" {
						fs.StringVar(&in.Role, "role", "", "id of the role they hold")
						fs.StringVar(&in.Team, "team", "", "id of their team")
						fs.StringArrayVar(&in.Aliases, "alias", nil, "another name for the person (repeatable)")
					}
					fs.StringArrayVar(&in.Said, "said", nil, "what was said, as stated (repeatable); needs --source")
					fs.StringVar(&in.Source, "source", "", "who said it, where, when, or a URL")
				},
				Bind: func(args []string, in *ops.OrgAddInput) error {
					in.Name = strings.Join(args, " ")
					return nil
				},
				Text: func(w io.Writer, out ops.OrgAddOutput) error {
					_, err := fmt.Fprintln(w, out.ID)
					return err
				},
			}),
			opCommand(l, list, binding[ops.OrgListInput, ops.OrgListOutput]{
				Use:  "list",
				Args: cobra.NoArgs,
				Text: func(w io.Writer, out ops.OrgListOutput) error {
					for _, r := range out.Items {
						if _, err := fmt.Fprintf(w, "%s\t%s\n", r.ID, r.Title); err != nil {
							return err
						}
					}
					return nil
				},
			}),
			opCommand(l, show, binding[ops.OrgShowInput, ops.OrgShowOutput]{
				Use:  "show <id>",
				Args: cobra.ExactArgs(1),
				Bind: func(args []string, in *ops.OrgShowInput) error { in.ID = args[0]; return nil },
				Text: func(w io.Writer, out ops.OrgShowOutput) error {
					_, err := fmt.Fprintf(w, "%s\t%s\n%s\n", out.Doc.ID, out.Doc.Title, out.Doc.Body)
					return err
				},
			}),
		)
		if name == "person" {
			sub.AddCommand(newPersonAliasCmd(l), newPersonContactCmd(l), newPersonGroupCmd(l), newPersonNoteCmd(l))
		}
		cmd.AddCommand(sub)
	}
	cmd.AddCommand(newOrgGroupCmd(l))
	kind("person", ops.OrgPersonAdd, ops.OrgPersonList, ops.OrgPersonShow)
	kind("team", ops.OrgTeamAdd, ops.OrgTeamList, ops.OrgTeamShow)
	kind("role", ops.OrgRoleAdd, ops.OrgRoleList, ops.OrgRoleShow)
	kind("domain", ops.OrgDomainAdd, ops.OrgDomainList, ops.OrgDomainShow)
	return cmd
}

func newRouteCmd(l *loader) *cobra.Command {
	return opCommand(l, ops.Route, binding[ops.RouteInput, ops.RouteOutput]{
		Use:  "route <domain-or-text>",
		Args: cobra.MinimumNArgs(1),
		Bind: func(args []string, in *ops.RouteInput) error {
			in.Query = strings.Join(args, " ")
			return nil
		},
		Text: func(w io.Writer, out ops.RouteOutput) error {
			p := &printer{w: w}
			ref := func(label string, rs []model.Ref) {
				for _, r := range rs {
					p.f("%s\t%s\t%s\n", label, r.ID, r.Title)
				}
			}
			p.f("# Check first: existing answers\n")
			ref("policy", out.SelfServe.Policies)
			ref("constraint", out.SelfServe.Constraints)
			ref("convention", out.SelfServe.Conventions)
			for _, s := range out.SelfServe.PriorRFX {
				p.f("rfx\t%s\t%s\t%s\n", s.ID, s.Status, s.Title)
				for _, t := range s.Targets {
					p.f("\ttarget\t%s\n", targetText(t))
				}
			}
			p.f("# Then ask\n")
			if out.Domain == nil {
				p.f("no single domain matched\n")
				ref("candidate", out.Candidates)
				return p.err
			}
			ref("domain", []model.Ref{*out.Domain})
			if out.Owner != nil {
				ref("owner", []model.Ref{*out.Owner})
			}
			ref("person", out.People)
			return p.err
		},
	})
}

// printer keeps the first write error, so a Text func checks it once.
type printer struct {
	w   io.Writer
	err error
}

func (p *printer) f(format string, a ...any) {
	if p.err == nil {
		_, p.err = fmt.Fprintf(p.w, format, a...)
	}
}
