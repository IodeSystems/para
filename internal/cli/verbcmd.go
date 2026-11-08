package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
)

// The generic verbs (ops/verbs.go): close, later, reopen, show, list, and
// send-to-agent take any id; brief takes any item with people.

// verbText prints what a write verb ran and the receipt that undoes it.
func verbText(w io.Writer, out ops.VerbOutput) error {
	p := &printer{w: w}
	id, _ := out.Result["id"].(string)
	p.f("%s\t%s\n", out.Op, firstNonEmpty(id, "-"))
	if out.Receipt != "" {
		p.f("receipt\t%s\n", out.Receipt)
	}
	return p.err
}

// laterText prints what later put off and until when: a date, an item it
// waits on, or someday. Later runs defer; its result is defer's output.
func laterText(w io.Writer, out ops.VerbOutput) error {
	var d ops.DeferOutput
	b, err := json.Marshal(out.Result)
	if err == nil {
		err = json.Unmarshal(b, &d)
	}
	if err != nil {
		return fmt.Errorf("cli: later: %w", err)
	}
	p := &printer{w: w}
	if d.Created != "" {
		p.f("created\t%s\n", d.Created)
	}
	for _, x := range d.Similar {
		p.f("similar\t%s\t%s\t(use --after %s, or --new)\n", x.ID, x.Title, x.ID)
	}
	when := model.SomedayWord
	until := d.Until
	if until == model.SomedayWord {
		until = ""
	}
	switch {
	case d.Follows != "":
		when = d.Follows
	case d.After != "" && until != "":
		when = "after " + d.After + ", until " + until
	case d.After != "":
		when = "after " + d.After
	case until != "":
		when = "until " + until
	}
	for _, id := range d.Changed {
		p.f("put off\t%s\t%s\n", id, when)
	}
	for _, s := range d.Skipped {
		p.f("skipped\t%s\n", s)
	}
	if len(d.Changed) == 0 && len(d.Similar) == 0 {
		p.f("nothing put off\n")
	}
	if out.Receipt != "" {
		p.f("receipt\t%s\n", out.Receipt)
	}
	return p.err
}

// fieldFlags reads --field key=value; a value that is JSON (a number, a
// list, true) is taken as JSON, any other as text.
func fieldFlags(list []string) (map[string]any, error) {
	if len(list) == 0 {
		return nil, nil
	}
	out := map[string]any{}
	for _, f := range list {
		k, v, ok := strings.Cut(f, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("--field %q: want key=value", f)
		}
		var x any
		if json.Unmarshal([]byte(v), &x) == nil {
			out[k] = x
		} else {
			out[k] = v
		}
	}
	return out, nil
}

func newVerbCmds(l *loader) []*cobra.Command {
	var fields []string
	var evURL, evVerify, evSource, evRef string
	closeCmd := opCommand(l, ops.Close, binding[ops.CloseInput, ops.VerbOutput]{
		Use: "close <id> [--outcome <outcome>] [--reason <text>] [--field key=value]...", Args: cobra.ExactArgs(1),
		Flags: func(fs *pflag.FlagSet, in *ops.CloseInput) {
			fs.StringVar(&in.Outcome, "outcome", "", "how it ended, by type: done, partial, dropped, duplicate, superseded, declined, failed, rerouted, skipped, cancelled, sent, resolved; empty is the type's plain close")
			fs.StringVar(&in.Reason, "reason", "", "why, kept where the type's close op keeps one (note, notes, reason, text)")
			fs.StringArrayVar(&fields, "field", nil, "an input of the type's close op, such as link=task-x or force=true; repeatable")
			fs.StringVar(&evURL, "evidence", "", "evidence the owner can open: an http(s) URL, a vault id or path, or an absolute path (or ~/path) to a file on this machine")
			fs.StringVar(&evSource, "evidence-source", "", "evidence in a private connector: its source (with --evidence-ref and --evidence-verify)")
			fs.StringVar(&evRef, "evidence-ref", "", "the item's ref in that source")
			fs.StringVar(&evVerify, "evidence-verify", "", "how the owner finds the item themself")
		},
		Bind: func(args []string, in *ops.CloseInput) error {
			in.ID = args[0]
			var err error
			if in.Fields, err = fieldFlags(fields); err != nil {
				return err
			}
			if evURL != "" || evSource != "" || evRef != "" || evVerify != "" {
				ev := &model.Evidence{Locator: evURL, Verify: evVerify}
				if evSource != "" || evRef != "" {
					ev.Via = &model.IOVia{Source: evSource, Ref: evRef}
				}
				in.Evidence = ev
			}
			return nil
		},
		Text: verbText,
	})
	var items []string
	laterCmd := opCommand(l, ops.Later, binding[ops.LaterInput, ops.VerbOutput]{
		Use: "later [id]... [--until YYYY-MM-DD | --until-start <obligation> | --until-end <obligation> | --after <id> | --when <text>]", Args: cobra.ArbitraryArgs,
		Flags: func(fs *pflag.FlagSet, in *ops.LaterInput) {
			fs.StringVar(&in.Until, "until", "", "YYYY-MM-DD it comes back; leave out for someday")
			fs.StringVar(&in.After, "after", "", "id of an item to wait for")
			fs.StringVar(&in.UntilStart, "until-start", "", "id of an obligation: back on its start (or due), following it when it moves")
			fs.StringVar(&in.UntilEnd, "until-end", "", "id of an obligation: back the day after it ends, following it when it moves")
			fs.StringVar(&in.When, "when", "", "a condition in words: makes a task that the items wait on")
			fs.StringVar(&in.Place, "place", "", "goal or area of the task --when makes")
			fs.BoolVar(&in.New, "new", false, "with --when: always make a new task, even when an open task has the same or a similar condition")
			fs.StringVar(&in.Reason, "reason", "", "why it can wait")
			fs.StringVar(&in.Scope, "scope", "", "a goal or area: every open question in it that blocks nothing")
			fs.StringVar(&in.Tag, "tag", "", "a topic: every tagged open item")
			fs.StringArrayVar(&items, "item", nil, "a dossier item as <proposal>#<item>; repeatable")
		},
		Bind: func(args []string, in *ops.LaterInput) error {
			in.IDs = args
			var err error
			in.Items, err = itemRefs(items)
			return err
		},
		Text: laterText,
	})
	var reItems []string
	reopenCmd := opCommand(l, ops.Reopen, binding[ops.ReopenInput, ops.VerbOutput]{
		Use: "reopen <id>... [--reason <text>]", Args: cobra.ArbitraryArgs,
		Flags: func(fs *pflag.FlagSet, in *ops.ReopenInput) {
			fs.StringVar(&in.Reason, "reason", "", "why it is open again")
			fs.StringVar(&in.Tag, "tag", "", "a topic: bring back every tagged item put off")
			fs.StringArrayVar(&reItems, "item", nil, "a dossier item put off, as <proposal>#<item>; repeatable")
		},
		Bind: func(args []string, in *ops.ReopenInput) error {
			in.IDs = args
			var err error
			in.Items, err = itemRefs(reItems)
			return err
		},
		Text: verbText,
	})
	var atts []string
	sendCmd := opCommand(l, ops.SendToAgent, binding[ops.SendToAgentInput, ops.VerbOutput]{
		Use: "send-to-agent <id> [note]", Args: cobra.MinimumNArgs(1),
		Flags: func(fs *pflag.FlagSet, in *ops.SendToAgentInput) {
			fs.StringVar(&in.Title, "title", "", "a short name for the work (an agent must give one)")
			fs.StringArrayVar(&atts, "attach", nil, "vault path of an attachment; repeatable")
			fs.BoolVar(&in.Done, "done", false, "ask the agent to close the item after")
			fs.BoolVar(&in.FileNotes, "file-notes", false, "a meeting: ask the agent to file its notes and close it as held")
		},
		Bind: func(args []string, in *ops.SendToAgentInput) error {
			in.ID, in.Note, in.Attachments = args[0], strings.Join(args[1:], " "), atts
			return nil
		},
		Text: func(w io.Writer, out ops.VerbOutput) error {
			p := &printer{w: w}
			id, _ := out.Result["request"].(string)
			if id == "" {
				id, _ = out.Result["id"].(string)
			}
			p.f("%s\t%s\n", out.Op, firstNonEmpty(id, "-"))
			return p.err
		},
	})
	showCmd := opCommand(l, ops.Show, binding[ops.ShowInput, ops.ShowOutput]{
		Use: "show <id>", Args: cobra.ExactArgs(1),
		Bind: func(args []string, in *ops.ShowInput) error { in.ID = args[0]; return nil },
		Text: func(w io.Writer, out ops.ShowOutput) error {
			p := &printer{w: w}
			p.f("%s\t%s\t%s\t%s\t%s\n", out.ID, out.Type, firstNonEmpty(out.Status, "-"), out.Life, out.Title)
			need := "no"
			if out.NeedsYou {
				need = "yes"
			}
			p.f("needs you: %s (%s)\n", need, out.Reason)
			if out.Op != "" {
				p.f("detail: %s (--json prints it)\n", out.Op)
			}
			return p.err
		},
	})
	listCmd := opCommand(l, ops.List, binding[ops.ListInput, ops.ListOutput]{
		Use: "list [type]", Args: cobra.MaximumNArgs(1),
		Flags: func(fs *pflag.FlagSet, in *ops.ListInput) {
			fs.StringVar(&in.State, "state", "", "open, later, closed, or all; default open and later")
			fs.StringVar(&in.Tag, "tag", "", "a topic")
			fs.BoolVar(&in.NeedsYou, "needs-you", false, "only what needs the owner now")
			fs.IntVar(&in.Limit, "limit", 0, "at most this many rows")
		},
		Bind: func(args []string, in *ops.ListInput) error {
			if len(args) == 1 {
				in.Type = model.Type(args[0])
			}
			return nil
		},
		Text: func(w io.Writer, out ops.ListOutput) error {
			p := &printer{w: w}
			for _, r := range out.Rows {
				mark := ""
				if r.NeedsYou {
					mark = "*"
				}
				p.f("%s%s\t%s\t%s\t%s\t%s\n", mark, r.ID, r.Type, r.Life, r.Reason, r.Title)
			}
			if len(out.Rows) < out.Count {
				p.f("%d of %d\n", len(out.Rows), out.Count)
			}
			return p.err
		},
	})
	briefCmd := opCommand(l, ops.Brief, binding[ops.BriefInput, ops.BriefOutput]{
		Use: "brief <id>", Args: cobra.ExactArgs(1),
		Bind: func(args []string, in *ops.BriefInput) error { in.ID = args[0]; return nil },
		Text: func(w io.Writer, out ops.BriefOutput) error {
			_, err := io.WriteString(w, out.Markdown)
			return err
		},
	})
	return []*cobra.Command{closeCmd, laterCmd, reopenCmd, sendCmd, showCmd, listCmd, briefCmd}
}
