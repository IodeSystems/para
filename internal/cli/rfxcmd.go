package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/paradize/internal/model"
	"github.com/iodesystems/paradize/internal/ops"
)

func newRFXCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "rfx", Short: "Requests for something, in or out", Args: cobra.NoArgs}
	cmd.AddCommand(
		opCommand(l, ops.RFXAdd, binding[ops.RFXAddInput, ops.RFXAddOutput]{
			Use:  "add <ask>",
			Args: cobra.MinimumNArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.RFXAddInput) {
				fs.StringVar((*string)(&in.Direction), "direction", "out", "in (someone asks you) or out (you ask)")
				fs.StringVar(&in.To, "to", "", "who is asked (out) or who asked (in)")
				fs.StringVar(&in.Route, "route", "", "domain id (see `para route`)")
				fs.StringVar(&in.Channel, "channel", "", "where the ask goes")
				fs.StringVar(&in.SelfServe, "self-serve", "", "what you checked before asking")
				fs.StringVar(&in.Project, "project", "", "project id")
				fs.StringSliceVar(&in.Blocks, "blocks", nil, "ids this blocks")
				fs.StringSliceVar(&in.DependsOn, "depends-on", nil, "RFX ids that must resolve first")
				fs.StringVar(&in.NextNudge, "next-nudge", "", "YYYY-MM-DD")
			},
			Bind: func(args []string, in *ops.RFXAddInput) error {
				in.Ask = strings.Join(args, " ")
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
				p.f("direction\t%s\nstatus\t%s\nroute\t%s\nto\t%s\nchannel\t%s\nnext_nudge\t%s\nplaybook\t%s\nself_serve_check\t%s\n",
					m.Direction, m.Status, m.Route, m.To, m.Channel, m.NextNudge, m.Playbook.Target(), m.SelfServeCheck)
				for _, c := range out.Chain {
					p.f("%sdepends_on\t%s\t%s\t%s\n", strings.Repeat("  ", c.Depth-1), c.ID, c.Status, c.Title)
				}
				for _, id := range out.DependedOnBy {
					p.f("depended_on_by\t%s\n", id)
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
					if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", it.NextNudge, it.ID, it.Direction, it.To, it.Title, mark); err != nil {
						return err
					}
				}
				return nil
			},
		}),
	)
	return cmd
}

func writeStatus(w io.Writer, out ops.RFXStatusOutput) error {
	_, err := fmt.Fprintf(w, "%s\t%s -> %s\n", out.ID, out.From, out.Status)
	return err
}

func writeRFX(w io.Writer, items []ops.RFXSummary) error {
	for _, s := range items {
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", s.ID, s.Direction, s.Status, s.Route, s.To, s.NextNudge, s.Title); err != nil {
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
		cmd.AddCommand(sub)
	}
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
