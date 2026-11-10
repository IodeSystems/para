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

func newIOCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "io", Short: "Messages not dealt with yet: the inbox (in) and the outbox (out)", Args: cobra.NoArgs}
	cmd.AddCommand(
		newIOAddCmd(l, "add <text>", ""),
		newIOListCmd(l, "list", "", nil),
		newIOListCmd(l, "inbox", "Incoming messages not filed: unread and read", &ops.IOListInput{Direction: model.IOIn}),
		newIOListCmd(l, "outbox", "Outgoing drafts not sent, by expects: draft and ready", &ops.IOListInput{Direction: model.IOOut}),
		opCommand(l, ops.IOShow, binding[ops.IOShowInput, ops.IOShowOutput]{
			Use:  "show <id>",
			Args: cobra.ExactArgs(1),
			Bind: func(args []string, in *ops.IOShowInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.IOShowOutput) error {
				p := &printer{w: w}
				ioRow(p, out.Item)
				p.f("%s\n", out.Doc.Body)
				return p.err
			},
		}),
		opCommand(l, ops.IORevisions, binding[ops.IORevisionsInput, ops.IORevisionsOutput]{
			Use:  "revisions <id> [--n <n>]",
			Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.IORevisionsInput) {
				fs.IntVar(&in.N, "n", 0, "show the text of this revision")
			},
			Bind: func(args []string, in *ops.IORevisionsInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.IORevisionsOutput) error {
				p := &printer{w: w}
				if out.N != 0 {
					p.f("%s\n", out.Text)
					return p.err
				}
				for _, r := range out.Revisions {
					p.f("r%d\t%s\t%s\t%s\n", r.N, r.At, r.By, r.Hash)
				}
				p.f("current r%d\n", out.Current)
				return p.err
			},
		}),
		opCommand(l, ops.IODiff, binding[ops.IODiffInput, ops.IODiffOutput]{
			Use:  "diff <id> [--from <n>] [--to <n>]",
			Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.IODiffInput) {
				fs.IntVar(&in.From, "from", 0, "older revision; default the one before --to")
				fs.IntVar(&in.To, "to", 0, "newer revision; default the current text")
				fs.StringVar(&in.Unit, "unit", "", "line (default) or paragraph")
				fs.IntVar(&in.Context, "context", 0, "unchanged units around a change; default 2")
			},
			Bind: func(args []string, in *ops.IODiffInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.IODiffOutput) error {
				p := &printer{w: w}
				p.f("%s\tr%d to r%d\t+%d -%d\n", out.ID, out.From, out.To, out.Added, out.Removed)
				p.f("%s", out.Text)
				return p.err
			},
		}),
		opCommand(l, ops.IOMade, binding[ops.IOMadeInput, ops.IOMadeOutput]{
			Use:  "made <id>",
			Args: cobra.ExactArgs(1),
			Bind: func(args []string, in *ops.IOMadeInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.IOMadeOutput) error {
				p := &printer{w: w}
				for _, r := range out.Rows {
					p.f("%s  %s  %s  %s\n", r.Doc, strings.Join(r.Actions, "+"), r.Type, r.Title)
				}
				p.f("%d documents\n", len(out.Rows))
				return p.err
			},
		}),
		opCommand(l, ops.IOEdit, binding[ops.IOEditInput, ops.IOEditOutput]{
			Use:  "edit <id>",
			Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.IOEditInput) {
				fs.StringVar((*string)(&in.State), "state", "", "in: read or unread; out: draft or ready")
				fs.StringVar(&in.Text, "text", "", "out: the new message text")
				fs.StringVar(&in.Subject, "subject", "", "out: the new subject")
				fs.StringArrayVar(&in.To, "to", nil, "out: a recipient (person or group id, or text); repeatable; replaces the list")
				fs.StringVar((*string)(&in.Medium), "medium", "", "out: "+mediumList)
				fs.StringVar((*string)(&in.Expects), "expects", "", "response or none; in: response when the sender waits for your reply")
				fs.StringVar(&in.Route, "route", "", "out with expects response: domain id")
				fs.StringVar((*string)(&in.Effort), "effort", "", effortHelp)
				fs.StringVar(&in.Ref, "ref", "", "the message permalink")
				fs.StringVar(&in.Verify, "verify", "", "with no ref: how the owner finds the message")
				fs.StringVar((*string)(&in.Voice), "voice", "", voiceHelp)
				fs.StringVar(&in.Kind, "kind", "", kindHelp)
				fs.StringVar(&in.Agent, "agent", "", "with --voice for_me: the agent that sends it for you")
			},
			Bind: func(args []string, in *ops.IOEditInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.IOEditOutput) error {
				p := &printer{w: w}
				for _, wn := range out.Warnings {
					p.f("note: %s\n", wn)
				}
				p.f("%s\t%s\n", out.ID, out.State)
				return p.err
			},
		}),
		newIOFileCmd(l),
		opCommand(l, ops.IODrop, binding[ops.IODropInput, ops.IODropOutput]{
			Use:  "drop <id> --reason <text>",
			Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.IODropInput) {
				fs.StringVar(&in.Reason, "reason", "", "why it needs nothing, or will not be sent (required)")
			},
			Bind: func(args []string, in *ops.IODropInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.IODropOutput) error {
				_, err := fmt.Fprintf(w, "%s\t%s\n", out.ID, out.State)
				return err
			},
		}),
		opCommand(l, ops.IOApprove, binding[ops.IOApproveInput, ops.IOApproveOutput]{
			Use:  "approve <id>",
			Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.IOApproveInput) {
				fs.StringVar(&in.OwnerSaid, "owner-said", "", "as an agent: the owner's words that approve this draft, verbatim")
			},
			Bind: func(args []string, in *ops.IOApproveInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.IOApproveOutput) error {
				_, err := fmt.Fprintf(w, "%s\t%s\t%s\n", out.ID, out.State, out.ApprovedHash)
				return err
			},
		}),
		opCommand(l, ops.IOSend, binding[ops.IOSendInput, ops.IOSendOutput]{
			Use:  "send <id> --url <url> | --locator <text>",
			Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.IOSendInput) {
				fs.StringVar(&in.URL, "url", "", "where the sent message is")
				fs.StringVar(&in.Locator, "locator", "", "where it is, when it has no URL")
				fs.StringVar(&in.At, "at", "", "RFC 3339 time it was sent; default now")
				fs.BoolVar(&in.Observed, "observed", false, "as an agent: the owner sent it themself and you only saw it")
			},
			Bind: func(args []string, in *ops.IOSendInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.IOSendOutput) error {
				p := &printer{w: w}
				for _, wn := range out.Warnings {
					p.f("warning: %s\n", wn)
				}
				p.f("%s\t%s\t%s %s\n", out.ID, out.State, out.BecameType, out.Became)
				return p.err
			},
		}),
		opCommand(l, ops.IOSendCheck, binding[ops.IOSendCheckInput, ops.IOSendCheckOutput]{
			Use:  "send-check <id>",
			Args: cobra.ExactArgs(1),
			Bind: func(args []string, in *ops.IOSendCheckInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.IOSendCheckOutput) error {
				p := &printer{w: w}
				if out.Held {
					p.f("%s\theld\t%s\n", out.ID, out.Reason)
					for _, n := range out.New {
						p.f("  %s\t%s\t%s\t%s\n", n.ID, n.At, n.From, n.Title)
					}
				} else {
					p.f("%s\tok\tsend by %s\n", out.ID, out.SendBy)
				}
				p.f("next\t%s\n", out.Next)
				return p.err
			},
		}),
		opCommand(l, ops.IOStats, binding[ops.IOStatsInput, ops.IOStatsOutput]{
			Use:  "stats [--days <n>]",
			Args: cobra.NoArgs,
			Flags: func(fs *pflag.FlagSet, in *ops.IOStatsInput) {
				fs.IntVar(&in.Days, "days", 0, "one window in days; default 7 and 30")
			},
			Text: func(w io.Writer, out ops.IOStatsOutput) error {
				_, err := fmt.Fprintln(w, firstNonEmpty(out.Text, "no sources"))
				return err
			},
		}),
		opCommand(l, ops.IODone, binding[ops.IODoneInput, ops.IOSendOutput]{
			Use:  "done <id> [--url <url>] [--locator <text>] [--text <what was sent>]",
			Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.IODoneInput) {
				fs.StringVar(&in.URL, "url", "", "where the sent message is")
				fs.StringVar(&in.Locator, "locator", "", "where it is, when it has no URL")
				fs.StringVar(&in.Text, "text", "", "what was sent; default the draft")
				fs.StringVar(&in.At, "at", "", "RFC 3339 time it was sent; default now")
			},
			Bind: func(args []string, in *ops.IODoneInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.IOSendOutput) error {
				p := &printer{w: w}
				p.f("%s\t%s\t%s %s\n", out.ID, out.State, out.BecameType, out.Became)
				return p.err
			},
		}),
	)
	return cmd
}

// newInboxCmd keeps `para inbox add` for one release: it is `para io add
// --direction in`.
func newInboxCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "inbox", Short: "Old name of `para io`: only `inbox add` is left", Args: cobra.NoArgs}
	add := newIOAddCmd(l, "add <text>", model.IOIn)
	add.Short = "Same as `para io add --direction in` (this name goes in a later release)"
	delete(add.Annotations, opAnnotation)
	cmd.AddCommand(add)
	return cmd
}

func newIOAddCmd(l *loader, use string, dir model.IODirection) *cobra.Command {
	var via ioViaFlags
	return opCommand(l, ops.IOAdd, binding[ops.IOAddInput, ops.IOAddOutput]{
		Use:  use,
		Args: cobra.MinimumNArgs(1),
		Flags: func(fs *pflag.FlagSet, in *ops.IOAddInput) {
			if dir == "" {
				fs.StringVar((*string)(&in.Direction), "direction", "", "in (it came to you) or out (a draft to send) (required)")
			}
			fs.StringVar((*string)(&in.Medium), "medium", "", mediumList+" (default other)")
			fs.StringVar(&in.Subject, "subject", "", "subject; default the first line of the text")
			fs.StringVar(&in.From, "from", "", "sender: person or group id, or text")
			fs.StringArrayVar(&in.To, "to", nil, "recipient: person or group id, or text; repeatable")
			fs.StringVar(&in.At, "at", "", "RFC 3339 time it came; default now")
			fs.StringVar(&in.Ref, "ref", "", "public link to the message")
			fs.StringVar(&via.source, "via-source", "", "private source: connector or source name")
			fs.StringVar(&via.ref, "via-ref", "", "private source: its id of the message")
			fs.StringVar(&via.thread, "via-thread", "", "private source: its id of the conversation (thread, channel thread, ticket)")
			fs.StringVar(&in.Verify, "verify", "", "with via and no ref: how the owner finds the message")
			fs.StringVar((*string)(&in.Expects), "expects", "", "out: response (sending makes an Ask) or none (sending logs an interaction); in: response when the sender waits for your reply")
			fs.StringVar(&in.Route, "route", "", "out with expects response: domain id")
			fs.StringVar(&in.Thread, "thread", "", "id of the io item, interaction, Ask, or question this answers or acts on")
			fs.StringVar((*string)(&in.Effort), "effort", "", effortHelp)
			fs.StringVar((*string)(&in.Voice), "voice", "", voiceHelp)
			fs.StringVar(&in.Kind, "kind", "", kindHelp)
			fs.StringVar(&in.Agent, "agent", "", "with --voice for_me: the agent that sends it for you")
		},
		Bind: func(args []string, in *ops.IOAddInput) error {
			in.Text = strings.Join(args, " ")
			if dir != "" {
				in.Direction = dir
			}
			if via.source != "" || via.ref != "" {
				in.Via = &model.IOVia{Source: via.source, Ref: via.ref, Thread: via.thread}
			}
			return nil
		},
		Text: func(w io.Writer, out ops.IOAddOutput) error {
			p := &printer{w: w}
			for _, wn := range out.Warnings {
				p.f("warning: %s\n", wn)
			}
			p.f("%s\n", out.ID)
			return p.err
		},
	})
}

type ioViaFlags struct{ source, ref, thread string }

const (
	voiceHelp = "out: as_me (default: you approve it and it goes out as yours) or for_me (an agent sends it for you, stamped, when a rule of .para/delegation.yaml allows it)"
	kindHelp  = "out: a free label delegation rules match, such as scheduling or acknowledgement"
)

// newIOListCmd lists io items; fixed holds the filter of `io inbox` and
// `io outbox`, which are views of io_list and not ops of their own.
func newIOListCmd(l *loader, use, short string, fixed *ops.IOListInput) *cobra.Command {
	cmd := opCommand(l, ops.IOList, binding[ops.IOListInput, ops.IOListOutput]{
		Use:  use,
		Args: cobra.NoArgs,
		Flags: func(fs *pflag.FlagSet, in *ops.IOListInput) {
			if fixed == nil {
				fs.StringVar((*string)(&in.Direction), "direction", "", "in or out")
				fs.StringVar((*string)(&in.State), "state", "", "unread, read, filed, draft, ready, sent, or dropped")
			}
			fs.StringVar((*string)(&in.Medium), "medium", "", mediumList)
			fs.StringVar((*string)(&in.Expects), "expects", "", "response or none")
			fs.BoolVar(&in.All, "all", false, "include filed, sent, and dropped items")
			fs.StringVar((*string)(&in.Voice), "voice", "", "out: as_me or for_me; for_me --all lists what agents sent for you, stamped")
		},
		Bind: func(_ []string, in *ops.IOListInput) error {
			if fixed != nil {
				in.Direction = fixed.Direction
			}
			return nil
		},
		Text: func(w io.Writer, out ops.IOListOutput) error {
			p := &printer{w: w}
			if fixed != nil && fixed.Direction == model.IOOut {
				for _, e := range model.IOExpectsAll {
					head := false
					for _, it := range out.Items {
						if it.Expects == e {
							if !head {
								p.f("expects %s:\n", e)
								head = true
							}
							ioRow(p, it)
						}
					}
				}
				return p.err
			}
			for _, it := range out.Items {
				ioRow(p, it)
			}
			return p.err
		},
	})
	if fixed != nil {
		cmd.Short = short
		delete(cmd.Annotations, opAnnotation)
	}
	return cmd
}

func ioRow(p *printer, it ops.IOItem) {
	who := it.From
	if it.Direction == model.IOOut {
		who = strings.Join(it.To, ", ")
	}
	extra := ""
	switch {
	case it.Legacy:
		extra = "\t(inbox capture: run para vault upgrade)"
	case it.Became != "":
		extra = "\t-> " + it.Became
	}
	if it.Voice == model.VoiceForMe {
		extra += "\t(for you, stamped: " + firstNonEmpty(it.AllowedBy, "no rule") + ")"
	}
	p.f("%s\t%s\t%s\t%s\t%s\t%s\t%s%s\n", it.ID, it.Direction, it.State, it.Medium, firstNonEmpty(it.At, string(it.Created)), who, it.Title, extra)
}

func firstNonEmpty(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}

func newIOFileCmd(l *loader) *cobra.Command {
	var to, via []string
	return opCommand(l, ops.IOFile, binding[ops.IOFileInput, ops.IOFileOutput]{
		Use:  "file <id> --to task|rfx|project|note|question|interaction | --existing <id>",
		Args: cobra.ExactArgs(1),
		Flags: func(fs *pflag.FlagSet, in *ops.IOFileInput) {
			fs.StringVar((*string)(&in.To), "to", "", "task, rfx, project, note, question, or interaction (required unless --existing); with --existing also obligation or area")
			fs.StringVar(&in.Existing, "existing", "", "id of a document that already holds it (also an obligation or an area); para links it and makes nothing")
			fs.StringVar(&in.Project, "project", "", "task: project id (or --area); rfx, note: optional")
			fs.StringVar(&in.RequestedBy, "requested-by", "", "task: who it is for (default the sender)")
			fs.StringVar(&in.Title, "title", "", "task: its title (default the first line of the message)")
			fs.StringVar(&in.Due, "due", "", "task: due date YYYY-MM-DD")
			fs.StringVar(&in.Owner, "owner", "", "project: owner (default the vault owner, else self)")
			fs.StringVar((*string)(&in.Direction), "direction", "", "rfx: in or out (default in)")
			fs.StringVar(&in.Route, "route", "", "rfx: domain id (required)")
			fs.StringArrayVar(&to, "target", nil, "rfx: person or group id; pairs with --via, repeatable")
			fs.StringArrayVar(&via, "via", nil, "rfx: medium for the matching --target: "+mediumList)
			fs.StringVar(&in.SelfServe, "self-serve", "", "rfx: what you checked before asking")
			fs.StringVar(&in.NextNudge, "next-nudge", "", "rfx: YYYY-MM-DD")
			fs.StringVar(&in.Area, "area", "", "task, project, or note: area id")
			fs.StringVar(&in.Person, "person", "", "note: person id; makes a dated line in their notes, not a note document")
			fs.StringVar(&in.Date, "date", "", "note: YYYY-MM-DD; default today")
			fs.StringVar(&in.DoneWhen, "done-when", "", "task, project: how we know it is done")
			fs.StringVar(&in.Task, "task", "", "question: scope to this task (or --project, --area, --root: exactly one)")
			fs.BoolVar(&in.Root, "root", false, "question: scope to the whole vault")
			fs.StringVar((*string)(&in.Kind), "kind", "", "question: question, decision, or input")
			fs.StringArrayVar(&in.Options, "option", nil, "question: a choice; repeatable")
			fs.StringVar(&in.Recommended, "recommended", "", "question: the option you suggest")
			fs.StringArrayVar(&in.With, "with", nil, "interaction: person or group id; repeatable; default the sender")
			fs.StringVar((*string)(&in.Visibility), "visibility", "", "interaction: public or private (default private)")
		},
		Bind: func(args []string, in *ops.IOFileInput) error {
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
		Text: func(w io.Writer, out ops.IOFileOutput) error {
			p := &printer{w: w}
			for _, wn := range out.Warnings {
				p.f("warning: %s\n", wn)
			}
			p.f("%s\t%s\t%s\t%s\n", out.ID, out.State, out.To, out.Became)
			return p.err
		},
	})
}
