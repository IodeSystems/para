package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
)

func newPersonContactCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "contact", Short: "How to reach a person", Args: cobra.NoArgs}
	bind := func(args []string, in *ops.PersonContactInput) error {
		in.ID, in.Method, in.Value = args[0], model.Medium(args[1]), strings.Join(args[2:], " ")
		return nil
	}
	cmd.AddCommand(
		opCommand(l, ops.OrgPersonContactAdd, binding[ops.PersonContactInput, ops.PersonContactOutput]{
			Use:  "add <person-id> <method> <value>",
			Args: cobra.MinimumNArgs(3),
			Flags: func(fs *pflag.FlagSet, in *ops.PersonContactInput) {
				fs.StringVar(&in.Label, "label", "", "what this contact is for, such as work or on-call")
				fs.BoolVar(&in.Preferred, "preferred", false, "prefer this contact for its method")
			},
			Bind: bind,
			Text: writeContacts,
		}),
		opCommand(l, ops.OrgPersonContactRemove, binding[ops.PersonContactInput, ops.PersonContactOutput]{
			Use:  "remove <person-id> <method> <value>",
			Args: cobra.MinimumNArgs(3),
			Bind: bind,
			Text: writeContacts,
		}),
	)
	return cmd
}

func newPersonAliasCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "alias", Short: "Other names for a person", Args: cobra.NoArgs}
	bind := func(args []string, in *ops.PersonAliasInput) error {
		in.ID, in.Alias = args[0], strings.Join(args[1:], " ")
		return nil
	}
	text := func(w io.Writer, out ops.PersonAliasOutput) error {
		p := &printer{w: w}
		for _, a := range out.Aliases {
			p.f("%s\n", a)
		}
		return p.err
	}
	cmd.AddCommand(
		opCommand(l, ops.OrgPersonAliasAdd, binding[ops.PersonAliasInput, ops.PersonAliasOutput]{
			Use: "add <person-id> <alias>", Args: cobra.MinimumNArgs(2), Bind: bind, Text: text,
		}),
		opCommand(l, ops.OrgPersonAliasRemove, binding[ops.PersonAliasInput, ops.PersonAliasOutput]{
			Use: "remove <person-id> <alias>", Args: cobra.MinimumNArgs(2), Bind: bind, Text: text,
		}),
	)
	return cmd
}

func writeContacts(w io.Writer, out ops.PersonContactOutput) error {
	p := &printer{w: w}
	for _, c := range out.Contacts {
		p.f("%s\n", contactText(c))
	}
	return p.err
}

func contactText(c model.Contact) string {
	mark := ""
	if c.Preferred {
		mark = "*"
	}
	return fmt.Sprintf("%s%s\t%s\t%s", mark, c.Method, c.Value, c.Label)
}

func newPersonGroupCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "group", Short: "Group membership of a person", Args: cobra.NoArgs}
	bind := func(args []string, in *ops.PersonGroupInput) error {
		in.ID, in.Group = args[0], args[1]
		return nil
	}
	text := func(w io.Writer, out ops.PersonGroupOutput) error {
		p := &printer{w: w}
		for _, g := range out.Groups {
			p.f("%s\n", g)
		}
		return p.err
	}
	cmd.AddCommand(
		opCommand(l, ops.OrgPersonGroupAdd, binding[ops.PersonGroupInput, ops.PersonGroupOutput]{
			Use: "add <person-id> <group-id>", Args: cobra.ExactArgs(2), Bind: bind, Text: text,
		}),
		opCommand(l, ops.OrgPersonGroupRemove, binding[ops.PersonGroupInput, ops.PersonGroupOutput]{
			Use: "remove <person-id> <group-id>", Args: cobra.ExactArgs(2), Bind: bind, Text: text,
		}),
	)
	return cmd
}

func newPersonNoteCmd(l *loader) *cobra.Command {
	return opCommand(l, ops.OrgPersonNote, binding[ops.PersonNoteInput, ops.PersonNoteOutput]{
		Use:  "note <person-id> <text>",
		Args: cobra.MinimumNArgs(2),
		Flags: func(fs *pflag.FlagSet, in *ops.PersonNoteInput) {
			fs.StringVar(&in.Date, "date", "", "YYYY-MM-DD; default today")
		},
		Bind: func(args []string, in *ops.PersonNoteInput) error {
			in.ID, in.Text = args[0], strings.Join(args[1:], " ")
			return nil
		},
		Text: func(w io.Writer, out ops.PersonNoteOutput) error {
			_, err := fmt.Fprintln(w, out.Line)
			return err
		},
	})
}

func newOrgGroupCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "group", Short: "Manage group records", Args: cobra.NoArgs}
	cmd.AddCommand(
		opCommand(l, ops.OrgGroupAdd, binding[ops.GroupAddInput, ops.OrgAddOutput]{
			Use:  "add <name>",
			Args: cobra.MinimumNArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.GroupAddInput) {
				fs.StringVar(&in.Org, "org", "", "organization or other free text")
				fs.StringVar((*string)(&in.Kind), "kind", "team", strings.Join(strs(model.GroupKinds), ", "))
				fs.StringVar(&in.ID, "id", "", "document id; default from the name")
			},
			Bind: func(args []string, in *ops.GroupAddInput) error {
				in.Name = strings.Join(args, " ")
				return nil
			},
			Text: func(w io.Writer, out ops.OrgAddOutput) error {
				_, err := fmt.Fprintln(w, out.ID)
				return err
			},
		}),
		opCommand(l, ops.OrgGroupList, binding[ops.OrgListInput, ops.OrgListOutput]{
			Use:  "list",
			Args: cobra.NoArgs,
			Text: func(w io.Writer, out ops.OrgListOutput) error {
				p := &printer{w: w}
				for _, r := range out.Items {
					p.f("%s\t%s\n", r.ID, r.Title)
				}
				return p.err
			},
		}),
		opCommand(l, ops.OrgGroupShow, binding[ops.OrgShowInput, ops.GroupShowOutput]{
			Use:  "show <id>",
			Args: cobra.ExactArgs(1),
			Bind: func(args []string, in *ops.OrgShowInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.GroupShowOutput) error {
				g := out.Doc.Meta.(*model.Group)
				p := &printer{w: w}
				p.f("%s\t%s\nkind\t%s\norg\t%s\n", out.Doc.ID, out.Doc.Title, g.Kind, g.Org)
				for _, m := range out.Members {
					p.f("member\t%s\t%s\n", m.ID, m.Title)
				}
				return p.err
			},
		}),
	)
	return cmd
}

func newWhoCmd(l *loader) *cobra.Command {
	return opCommand(l, ops.Who, binding[ops.WhoInput, ops.WhoOutput]{
		Use:  "who <name-alias-or-contact>",
		Args: cobra.MinimumNArgs(1),
		Flags: func(fs *pflag.FlagSet, in *ops.WhoInput) {
			fs.IntVar(&in.Recent, "recent", 0, "interactions to list; default 5")
		},
		Bind: func(args []string, in *ops.WhoInput) error {
			in.Query = strings.Join(args, " ")
			return nil
		},
		Text: func(w io.Writer, out ops.WhoOutput) error {
			p := &printer{w: w}
			if out.Person == nil {
				if len(out.Candidates) == 0 {
					p.f("no one matches %q\n", out.Query)
					return p.err
				}
				p.f("%d people match %q; pick one by id\n", len(out.Candidates), out.Query)
				for _, c := range out.Candidates {
					p.f("candidate\t%s\t%s\n", c.ID, c.Title)
				}
				return p.err
			}
			wp := out.Person
			p.f("%s\t%s\norg\t%s\n", wp.ID, wp.Name, wp.Org)
			for _, a := range wp.Aliases {
				p.f("alias\t%s\n", a)
			}
			for _, r := range []struct {
				label string
				ref   *model.Ref
			}{{"role", wp.Role}, {"team", wp.Team}} {
				if r.ref != nil {
					p.f("%s\t%s\t%s\n", r.label, r.ref.ID, r.ref.Title)
				}
			}
			for _, g := range wp.Groups {
				p.f("group\t%s\t%s\n", g.ID, g.Title)
			}
			for _, c := range wp.Contacts {
				p.f("contact\t%s\n", contactText(c))
			}
			for _, n := range wp.Notes {
				p.f("note\t%s\n", n)
			}
			for _, i := range wp.Interactions {
				p.f("interaction\t%s\t%s\t%s\t%s\t%s\n", i.Date, i.ID, i.Medium, i.Direction, i.Title)
			}
			for _, r := range wp.OpenRFX {
				p.f("rfx\t%s\t%s\n", r.ID, r.Title)
				for _, t := range r.Targets {
					p.f("\ttarget\t%s\n", targetText(t))
				}
			}
			for _, t := range wp.OpenTasks {
				p.f("task\t%s\t%s\t%s\t%s\n", t.ID, t.Status, dash(string(t.Due)), t.Title)
			}
			return p.err
		},
	})
}

func newInteractionCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "interaction", Short: "Log of contact with people", Args: cobra.NoArgs}
	cmd.AddCommand(
		opCommand(l, ops.InteractionAdd, binding[ops.InteractionAddInput, ops.InteractionAddOutput]{
			Use:  "add <summary>",
			Args: cobra.MinimumNArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.InteractionAddInput) {
				fs.StringArrayVar(&in.With, "with", nil, "person or group id (repeatable)")
				fs.StringVar((*string)(&in.Medium), "medium", "", mediumList)
				fs.StringVar((*string)(&in.Direction), "direction", "", "in, out, or observed")
				fs.StringVar((*string)(&in.Visibility), "visibility", "private", "public or private")
				fs.StringVar(&in.URL, "url", "", "link to the original")
				fs.StringVar(&in.Date, "date", "", "YYYY-MM-DD; default today")
			},
			Bind: func(args []string, in *ops.InteractionAddInput) error {
				in.Summary = strings.Join(args, " ")
				return nil
			},
			Text: func(w io.Writer, out ops.InteractionAddOutput) error {
				p := &printer{w: w}
				for _, wn := range out.Warnings {
					p.f("warning: %s\n", wn)
				}
				p.f("%s\n", out.ID)
				return p.err
			},
		}),
		opCommand(l, ops.InteractionList, binding[ops.InteractionListInput, ops.InteractionListOutput]{
			Use:  "list",
			Args: cobra.NoArgs,
			Flags: func(fs *pflag.FlagSet, in *ops.InteractionListInput) {
				fs.StringVar(&in.With, "with", "", "person or group id")
				fs.StringVar((*string)(&in.Medium), "medium", "", "only this medium")
				fs.StringVar((*string)(&in.Direction), "direction", "", "in, out, or observed")
				fs.StringVar(&in.Since, "since", "", "YYYY-MM-DD, inclusive")
				fs.IntVar(&in.Limit, "limit", 0, "most to list; 0 means all")
			},
			Text: func(w io.Writer, out ops.InteractionListOutput) error {
				p := &printer{w: w}
				for _, i := range out.Items {
					p.f("%s\t%s\t%s\t%s\t%s\t%s\n", i.Date, i.ID, i.Medium, i.Direction, strings.Join(i.With, ","), i.Title)
				}
				return p.err
			},
		}),
		opCommand(l, ops.InteractionShow, binding[ops.InteractionShowInput, ops.InteractionShowOutput]{
			Use:  "show <id>",
			Args: cobra.ExactArgs(1),
			Bind: func(args []string, in *ops.InteractionShowInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.InteractionShowOutput) error {
				m := out.Doc.Meta.(*model.Interaction)
				p := &printer{w: w}
				p.f("%s\t%s\ndate\t%s\nwith\t%s\nmedium\t%s\ndirection\t%s\nvisibility\t%s\nurl\t%s\n%s\n",
					out.Doc.ID, out.Doc.Title, m.Date, strings.Join(linkIDs(m.With), ","), m.Medium, m.Direction, m.Visibility, m.URL, out.Doc.Body)
				return p.err
			},
		}),
	)
	return cmd
}

func linkIDs(ls []model.Link) []string {
	out := make([]string, len(ls))
	for i, l := range ls {
		out[i] = l.Target()
	}
	return out
}
