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

// parseFields reads --field key=value flags. A value is JSON when it parses
// as JSON (a list, a map, a number, true, null), else a plain string.
func parseFields(pairs []string) (map[string]any, error) {
	if len(pairs) == 0 {
		return nil, nil
	}
	out := map[string]any{}
	for _, p := range pairs {
		k, v, ok := strings.Cut(p, "=")
		if !ok || strings.TrimSpace(k) == "" {
			return nil, fmt.Errorf("--field %q: want key=value", p)
		}
		var j any
		if err := json.Unmarshal([]byte(v), &j); err == nil {
			switch j.(type) {
			case []any, map[string]any, nil, bool:
				out[strings.TrimSpace(k)] = j
				continue
			}
		}
		out[strings.TrimSpace(k)] = v
	}
	return out, nil
}

func newDocCmd(l *loader) *cobra.Command {
	cmd := group("doc", "Documents: repair and edit any document through the store")
	var fields []string
	var title, body, sectionBody string
	var retypeTo string
	cmd.AddCommand(
		opCommand(l, ops.DocRename, binding[ops.DocRenameInput, ops.DocRenameOutput]{
			Use: "rename <id> <new-id>", Args: cobra.ExactArgs(2),
			Bind: func(args []string, in *ops.DocRenameInput) error { in.ID, in.NewID = args[0], args[1]; return nil },
			Text: func(w io.Writer, out ops.DocRenameOutput) error {
				_, err := fmt.Fprintf(w, "%s\tlinks rewritten in %d\n", out.ID, len(out.Rewritten))
				return err
			},
		}),
		opCommand(l, ops.DocRetype, binding[ops.DocRetypeInput, ops.DocRetypeOutput]{
			Use: "retype <id> --to <type> [--field key=value]...", Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, _ *ops.DocRetypeInput) {
				fs.StringVar(&retypeTo, "to", "", "the new type, such as policy_exception, team, or role")
				fs.StringArrayVar(&fields, "field", nil, "a field the new type requires: key=value (JSON for lists); repeatable")
			},
			Bind: func(args []string, in *ops.DocRetypeInput) (err error) {
				in.ID, in.Type = args[0], model.Type(retypeTo)
				in.Fields, err = parseFields(fields)
				return err
			},
			Text: func(w io.Writer, out ops.DocRetypeOutput) error {
				_, err := fmt.Fprintf(w, "%s\t%s\t%s\tkept: %s\n", out.ID, out.Type, out.Folder, strings.Join(out.Kept, ","))
				return err
			},
		}),
		opCommand(l, ops.DocEdit, binding[ops.DocEditInput, ops.DocEditOutput]{
			Use: "edit <id> [--title <t>] [--body <text> | --append <text> [--dated] | --section <heading> --section-body <text>] [--field key=value]... (or the new body on stdin)", Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.DocEditInput) {
				fs.StringVar(&title, "title", "", "the new title")
				fs.StringVar(&body, "body", "", "the new body: replaces the whole body")
				fs.StringVar(&in.Append, "append", "", "add text at the end of the body, or of --section (made when missing)")
				fs.BoolVar(&in.Dated, "dated", false, "with --append: add it as a line \"- YYYY-MM-DD: text\" dated today")
				fs.StringVar(&in.Section, "section", "", "a \"## \" section of the body, by heading: for --append or --section-body")
				fs.StringVar(&sectionBody, "section-body", "", "replace the text of --section (added when missing); the rest of the body stays")
				fs.StringArrayVar(&fields, "field", nil, "a declared field: key=value (JSON for lists, null removes); repeatable")
				fs.StringVar(&in.Rev, "rev", "", "refuse when the document is no longer at this revision")
			},
			Bind: func(args []string, in *ops.DocEditInput) (err error) {
				in.ID = args[0]
				if title != "" {
					in.Title = &title
				}
				if body != "" {
					in.Body = &body
				}
				if sectionBody != "" {
					in.SectionBody = &sectionBody
				}
				in.Fields, err = parseFields(fields)
				return err
			},
			Stdin: func(r io.Reader, in *ops.DocEditInput) error {
				if in.Title != nil || in.Body != nil || len(in.Fields) > 0 || in.Append != "" || in.SectionBody != nil {
					return nil
				}
				text, err := readStdin(r)
				if err != nil {
					return err
				}
				if in.Section != "" {
					in.SectionBody = &text // the new text of the section
				} else {
					in.Body = &text
				}
				return nil
			},
			Text: printID(func(o ops.DocEditOutput) string { return o.ID }),
		}),
		opCommand(l, ops.DocMerge, binding[ops.DocMergeInput, ops.DocMergeOutput]{
			Use: "merge <keep-id> <alias-id> [--note <text>]", Args: cobra.ExactArgs(2),
			Flags: func(fs *pflag.FlagSet, in *ops.DocMergeInput) {
				fs.StringVar(&in.Note, "note", "", "an open task alias: the note of its duplicate close")
			},
			Bind: func(args []string, in *ops.DocMergeInput) error { in.Keep, in.Alias = args[0], args[1]; return nil },
			Text: func(w io.Writer, out ops.DocMergeOutput) error {
				_, err := fmt.Fprintf(w, "%s\tmerged into %s\tlinks rewritten in %d\tjoined: %s\n", out.Alias, out.Keep, len(out.Rewritten), strings.Join(out.Joined, ","))
				return err
			},
		}),
		opCommand(l, ops.DocArchive, binding[ops.DocArchiveInput, ops.DocArchiveOutput]{
			Use: "archive <id> [--reason <text>]", Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.DocArchiveInput) {
				fs.StringVar(&in.Reason, "reason", "", "why it is archived, under ## Archived")
			},
			Bind: func(args []string, in *ops.DocArchiveInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.DocArchiveOutput) error {
				p := &printer{w: w}
				p.f("%s\t%s\n", out.ID, out.Folder)
				for _, x := range out.Warnings {
					p.f("warning: %s\n", x)
				}
				return p.err
			},
		}),
		opCommand(l, ops.DocMerges, binding[ops.DocMergesInput, ops.DocMergesOutput]{
			Use: "merges <id>", Args: cobra.ExactArgs(1),
			Bind: func(args []string, in *ops.DocMergesInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.DocMergesOutput) error {
				p := &printer{w: w}
				if out.DuplicateOf != "" {
					p.f("merged into\t%s\n", out.DuplicateOf)
				}
				for _, m := range out.MergedFrom {
					p.f("merged from\t%s\t%s\n", m.ID, m.Title)
				}
				return p.err
			},
		}),
	)
	cmd.Short = "Documents: tracked remote documents (track, versions, diff, seen, comments), and repair and edit of any document through the store"
	cmd.AddCommand(trackedDocCmds(l)...)
	return cmd
}

func newAttemptCmd(l *loader) *cobra.Command {
	cmd := group("attempt", "Attempts: what was tried on a task or an Ask")
	cmd.AddCommand(
		opCommand(l, ops.AttemptAdd, binding[ops.AttemptAddInput, ops.AttemptAddOutput]{
			Use: "add <task-or-rfx-id> --what <text> --result <text> [--source <ref>]...", Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.AttemptAddInput) {
				fs.StringVar(&in.What, "what", "", "what was tried")
				fs.StringVar(&in.Result, "result", "", "what came of it")
				fs.StringArrayVar(&in.Sources, "source", nil, "a source: URL or path with a locator; repeatable")
			},
			Bind: func(args []string, in *ops.AttemptAddInput) error { in.Target = args[0]; return nil },
			Text: printID(func(o ops.AttemptAddOutput) string { return o.ID }),
		}),
	)
	return cmd
}
