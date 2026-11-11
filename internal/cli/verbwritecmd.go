package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/para/internal/ops"
)

// The generic write verbs (ops/verbwrite.go): add, edit, and link. tag is
// the tag command itself (tagcmd.go).

func newWriteVerbCmds(l *loader) []*cobra.Command {
	var addFields []string
	addCmd := opCommand(l, ops.Add, binding[ops.AddInput, ops.AddOutput]{
		Use: "add <type> <title> [--parent <id>] [--body <text>] [--tag <tag>]... [--field key=value]...", Args: cobra.MinimumNArgs(2),
		Flags: func(fs *pflag.FlagSet, in *ops.AddInput) {
			fs.StringVar(&in.Parent, "parent", "", "a goal, area, task, obligation, or incident it goes under, by type")
			fs.StringVar(&in.Body, "body", "", "the body")
			fs.StringArrayVar(&in.Tags, "tag", nil, "a topic; repeatable")
			fs.StringArrayVar(&addFields, "field", nil, "an input of the type's add op, such as done_when=text; repeatable")
		},
		Bind: func(args []string, in *ops.AddInput) error {
			in.Type, in.Title = ops.AddType(args[0]), strings.Join(args[1:], " ")
			var err error
			in.Fields, err = fieldFlags(addFields)
			return err
		},
		Text: func(w io.Writer, out ops.AddOutput) error {
			p := &printer{w: w}
			p.f("%s\t%s\t%s\n", out.Op, out.Type, firstNonEmpty(out.ID, "-"))
			if out.Tagged != nil {
				p.f("%s\t%s\n", out.Tagged.Op, out.ID)
			}
			return p.err
		},
	})
	var editFields []string
	var editTitle, editBody, editNotes string
	var editFlags *pflag.FlagSet
	editCmd := opCommand(l, ops.Edit, binding[ops.EditInput, ops.VerbOutput]{
		Use: "edit <id> [--title <title>] [--body <text>] [--field key=value]... | --notes <md> [--notes-hash <hash>]", Args: cobra.ExactArgs(1),
		Flags: func(fs *pflag.FlagSet, in *ops.EditInput) {
			editFlags = fs
			fs.StringVar(&editTitle, "title", "", "the new title")
			fs.StringVar(&editBody, "body", "", "the new body")
			fs.StringVar(&editNotes, "notes", "", "the new ## Notes (empty removes them); alone")
			fs.StringVar(&in.NotesHash, "notes-hash", "", "the notes hash show printed; leave out when the item has no notes")
			fs.StringArrayVar(&editFields, "field", nil, "an input of the type's edit op or of doc_edit, such as effort=s; repeatable")
		},
		Bind: func(args []string, in *ops.EditInput) error {
			in.ID = args[0]
			if editFlags.Changed("title") {
				in.Title = &editTitle
			}
			if editFlags.Changed("body") {
				in.Body = &editBody
			}
			if editFlags.Changed("notes") {
				in.Notes = &editNotes
			}
			var err error
			in.Fields, err = fieldFlags(editFields)
			return err
		},
		Text: verbText,
	})
	linkCmd := opCommand(l, ops.Link, binding[ops.LinkInput, ops.VerbOutput]{
		Use: "link <from> [to] [--rel depends_on|link|part_of|became] [--remove] [--label <label>] [--kind <kind>]", Args: cobra.RangeArgs(1, 2),
		Flags: func(fs *pflag.FlagSet, in *ops.LinkInput) {
			fs.StringVar((*string)(&in.Rel), "rel", "", "depends_on (default for a task, question, obligation, rfx), link (default for an area, goal, incident), part_of, or became (default for an io item: io_file existing)")
			fs.BoolVar(&in.Remove, "remove", false, "remove the link")
			fs.StringVar(&in.Label, "label", "", "rel link: its label (default the target)")
			fs.StringVar((*string)(&in.Kind), "kind", "", "rel link: repo, plan, docs, runbook, tracker, dashboard, or other (default)")
		},
		Bind: func(args []string, in *ops.LinkInput) error {
			in.From = args[0]
			if len(args) == 2 {
				in.To = args[1]
			}
			return nil
		},
		Text: verbText,
	})
	workCmd := opCommand(l, ops.Work, binding[ops.WorkInput, ops.VerbOutput]{
		Use: "work <next|note|done|fail|ask> [id] [--text ..] [--summary ..] [--resolution ..] [--reply ..] [--reason ..]", Args: cobra.RangeArgs(1, 2),
		Flags: func(fs *pflag.FlagSet, in *ops.WorkInput) {
			fs.StringVar(&in.Text, "text", "", "note: the note; ask: the ask")
			fs.IntVar(&in.LeaseSeconds, "lease-seconds", 0, "next, note: the lease")
			fs.StringVar((*string)(&in.Resolution), "resolution", "", "done: done, partial, stuck, blocked, or needs_clarification")
			fs.StringVar(&in.Summary, "summary", "", "done: what was done")
			fs.StringArrayVar(&in.Links, "link", nil, "done: an id made or changed; ask: an id it is about; repeatable")
			fs.StringArrayVar(&in.Followed, "followed", nil, "done: context followed; repeatable")
			fs.StringVar(&in.Reply, "reply", "", "done, fail: your answer to the owner")
			fs.StringVar(&in.Reason, "reason", "", "fail: why")
			fs.StringVar(&in.Title, "title", "", "ask: a short name")
			fs.StringArrayVar(&in.Context, "context", nil, "ask: what the work must follow; repeatable")
			fs.StringVar(&in.Kind, "kind", "", "ask: a kind, free text")
		},
		Bind: func(args []string, in *ops.WorkInput) error {
			in.Action = ops.WorkAction(args[0])
			if len(args) == 2 {
				in.ID = args[1]
			}
			return nil
		},
		Text: func(w io.Writer, out ops.VerbOutput) error {
			p := &printer{w: w}
			id, _ := out.Result["id"].(string)
			if r, ok := out.Result["request"].(map[string]any); ok && id == "" {
				id, _ = r["id"].(string)
			}
			p.f("%s\t%s\n", out.Op, firstNonEmpty(id, "-"))
			return p.err
		},
	})
	undoCmd := opCommand(l, ops.Undo, binding[ops.UndoInput, ops.VerbOutput]{
		Use: "undo <receipt>...", Args: cobra.MinimumNArgs(1),
		Bind: func(args []string, in *ops.UndoInput) error {
			in.Receipt, in.Receipts = args[0], args[1:]
			return nil
		},
		Text: verbText,
	})
	return []*cobra.Command{addCmd, editCmd, linkCmd, workCmd, undoCmd}
}

// newTagVerbCmd is tag: tag items with --add and untag them with --remove.
// Its subcommands are tag_add, tag_remove, and tag_list.
func newTagVerbCmd(l *loader) *cobra.Command {
	return opCommand(l, ops.Tag, binding[ops.TagVerbInput, ops.TagVerbOutput]{
		Use: "tag <id>... [--add <tag>]... [--remove <tag>]...", Args: cobra.MinimumNArgs(1),
		Flags: func(fs *pflag.FlagSet, in *ops.TagVerbInput) {
			fs.StringArrayVar(&in.Add, "add", nil, "a topic to add; repeatable")
			fs.StringArrayVar(&in.Remove, "remove", nil, "a topic to remove; repeatable")
		},
		Bind: func(args []string, in *ops.TagVerbInput) error { in.IDs = args; return nil },
		Text: func(w io.Writer, out ops.TagVerbOutput) error {
			p := &printer{w: w}
			for _, r := range out.Ran {
				var t ops.TagOutput
				b, err := json.Marshal(r.Result)
				if err == nil {
					err = json.Unmarshal(b, &t)
				}
				if err != nil {
					return fmt.Errorf("cli: tag: %w", err)
				}
				for _, c := range t.Changed {
					p.f("%s\t%s\n", r.Op, c)
				}
				for _, s := range t.Skipped {
					p.f("skipped\t%s\n", s)
				}
			}
			return p.err
		},
	})
}
