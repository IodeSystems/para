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

// trackedDocCmds are the tracked document commands under `para doc`. Para
// fetches nothing: the text comes on stdin, as the agent or the owner read it.
func trackedDocCmds(l *loader) []*cobra.Command {
	var viaSource, viaRef string
	track := opCommand(l, ops.DocTrack, binding[ops.DocTrackInput, ops.DocTrackOutput]{
		Use:  "track --url <url> [--system <s>] [--title <t>] < text",
		Args: cobra.NoArgs,
		Flags: func(fs *pflag.FlagSet, in *ops.DocTrackInput) {
			fs.StringVar(&in.URL, "url", "", "public link a person can open")
			fs.StringVar((*string)(&in.System), "system", "", "confluence, jira, github, gdoc, or other (default other)")
			fs.StringVar(&in.Title, "title", "", "title (default: first heading, else URL host and path)")
			fs.StringVar(&viaSource, "via-source", "", "private connector that reads it again")
			fs.StringVar(&viaRef, "via-ref", "", "the connector's own id of the document")
			fs.StringVar(&in.Owner, "owner", "", "person, group, or team id that owns the document")
			fs.StringArrayVar(&in.Watchers, "watcher", nil, "person, group, or team id; repeatable")
			fs.StringVar(&in.Area, "area", "", "area id")
			fs.StringVar(&in.Goal, "goal", "", "goal id")
			fs.StringVar(&in.Project, "project", "", "ongoing project id")
			fs.StringVar(&in.Route, "route", "", "domain an Ask from a comment goes to")
			fs.StringVar(&in.Note, "note", "", "what the document is to you")
			fs.StringVar((*string)(&in.Fidelity), "fidelity", "", "exact (default), partial, or ocr")
			fs.StringVar(&in.FetchedAt, "fetched-at", "", "RFC 3339 time it was read; default now")
			fs.StringVar(&in.ID, "id", "", "document id (default generated)")
		},
		Bind: func(_ []string, in *ops.DocTrackInput) error {
			if viaSource != "" || viaRef != "" {
				in.Via = &model.IOVia{Source: viaSource, Ref: viaRef}
			}
			return nil
		},
		Stdin: func(r io.Reader, in *ops.DocTrackInput) (err error) {
			in.Text, err = readStdin(r)
			return err
		},
		Text: func(w io.Writer, out ops.DocTrackOutput) error {
			_, err := fmt.Fprintf(w, "%s\tversion %d\t%s\n", out.ID, out.Version, out.Hash)
			return err
		},
	})
	version := group("version", "Versions of a tracked document")
	version.AddCommand(opCommand(l, ops.DocVersionAdd, binding[ops.DocVersionAddInput, ops.DocVersionAddOutput]{
		Use:  "add <id> < text",
		Args: cobra.ExactArgs(1),
		Flags: func(fs *pflag.FlagSet, in *ops.DocVersionAddInput) {
			fs.StringVar((*string)(&in.Fidelity), "fidelity", "", "exact (default), partial, or ocr")
			fs.StringVar(&in.FetchedAt, "fetched-at", "", "RFC 3339 time it was read; default now")
			fs.StringVar(&in.Proposal, "proposal", "", "the dossier of the doc edit this text is the result of")
			fs.StringVar(&in.Item, "item", "", "the doc edit item")
		},
		Bind: func(args []string, in *ops.DocVersionAddInput) error { in.ID = args[0]; return nil },
		Stdin: func(r io.Reader, in *ops.DocVersionAddInput) (err error) {
			in.Text, err = readStdin(r)
			return err
		},
		Text: func(w io.Writer, out ops.DocVersionAddOutput) error {
			p := &printer{w: w}
			p.f("%s\t%s\tversion %d\tunseen %d\t+%d -%d\n", out.ID, out.Result, out.Version, out.Unseen, out.Added, out.Removed)
			if len(out.AnchorsLost) > 0 {
				p.f("quote not found: %s\n", strings.Join(out.AnchorsLost, ", "))
			}
			return p.err
		},
	}))
	tracked := opCommand(l, ops.DocTracked, binding[ops.DocTrackedInput, ops.DocTrackedOutput]{
		Use:  "tracked [--status <s>] [--unseen]",
		Args: cobra.NoArgs,
		Flags: func(fs *pflag.FlagSet, in *ops.DocTrackedInput) {
			fs.StringVar((*string)(&in.Status), "status", "", "watching, paused, or archived (default: all but archived)")
			fs.BoolVar(&in.Unseen, "unseen", false, "only documents with versions not seen")
		},
		Text: func(w io.Writer, out ops.DocTrackedOutput) error {
			p := &printer{w: w}
			p.f("unseen %d\topen comments %d\n", out.Unseen, out.OpenComments)
			for _, d := range out.Docs {
				p.f("%s\t%s\t%s\tv%d seen v%d\tcomments %d\t%s\n", d.ID, d.Status, d.System, d.Latest, d.LastSeen, d.OpenComments, d.Title)
			}
			return p.err
		},
	})
	tracked.AddCommand(
		opCommand(l, ops.DocTrackedShow, binding[ops.DocTrackedShowInput, ops.DocTrackedShowOutput]{
			Use:  "show <id> [--version <n>]",
			Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.DocTrackedShowInput) {
				fs.IntVar(&in.Version, "version", 0, "the version whose text to print; default the latest")
			},
			Bind: func(args []string, in *ops.DocTrackedShowInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.DocTrackedShowOutput) error {
				p := &printer{w: w}
				p.f("%s\t%s\t%s\t%s\n", out.Doc.ID, out.Doc.Status, out.Doc.URL, out.Doc.Title)
				for _, v := range out.Versions {
					seen := ""
					if v.Seen {
						seen = "\tseen"
					}
					p.f("v%d\t%s\t%s\t%s%s\n", v.Version, v.FetchedAt, v.FetchedBy, v.Hash, seen)
				}
				for _, c := range out.Comments {
					p.f("%s\t%s\t%s\t%s\n", c.ID, firstNonEmpty(string(c.Resolution), "open"), c.By, oneLine(c.Text))
				}
				p.f("\n%s\n", out.Text)
				return p.err
			},
		}),
		opCommand(l, ops.DocTrackedStatus, binding[ops.DocTrackedStatusInput, ops.DocTrackedStatusOutput]{
			Use:  "status <id> watching|paused|archived",
			Args: cobra.ExactArgs(2),
			Bind: func(args []string, in *ops.DocTrackedStatusInput) error {
				in.ID, in.Status = args[0], model.DocStatus(args[1])
				return nil
			},
			Text: func(w io.Writer, out ops.DocTrackedStatusOutput) error {
				_, err := fmt.Fprintf(w, "%s\t%s\n", out.ID, out.Status)
				return err
			},
		}),
	)
	diff := opCommand(l, ops.DocDiff, binding[ops.DocDiffInput, ops.DocDiffOutput]{
		Use:  "diff <id> [--from <n>] [--to <n>] [--unit line|paragraph]",
		Args: cobra.ExactArgs(1),
		Flags: func(fs *pflag.FlagSet, in *ops.DocDiffInput) {
			fs.IntVar(&in.From, "from", 0, "older version; default the last version seen")
			fs.IntVar(&in.To, "to", 0, "newer version; default the latest")
			fs.BoolVar(&in.Whole, "whole", false, "compare with the empty text: all of version to")
			fs.StringVar(&in.Unit, "unit", "", "line (default) or paragraph")
			fs.IntVar(&in.Context, "context", 0, "unchanged units around a change; default 2")
		},
		Bind: func(args []string, in *ops.DocDiffInput) error { in.ID = args[0]; return nil },
		Text: func(w io.Writer, out ops.DocDiffOutput) error {
			p := &printer{w: w}
			p.f("%s\tv%d to v%d\t+%d -%d\n", out.ID, out.From, out.To, out.Added, out.Removed)
			p.f("%s", out.Text)
			return p.err
		},
	})
	seen := opCommand(l, ops.DocSeen, binding[ops.DocSeenInput, ops.DocSeenOutput]{
		Use:  "seen <id> [--version <n>]",
		Args: cobra.ExactArgs(1),
		Flags: func(fs *pflag.FlagSet, in *ops.DocSeenInput) {
			fs.IntVar(&in.Version, "version", 0, "seen up to this version; default the latest")
		},
		Bind: func(args []string, in *ops.DocSeenInput) error { in.ID = args[0]; return nil },
		Text: func(w io.Writer, out ops.DocSeenOutput) error {
			_, err := fmt.Fprintf(w, "%s\tseen v%d\tunseen %d\n", out.ID, out.LastSeen, out.Unseen)
			return err
		},
	})
	var start, end int
	comment := group("comment", "Comments people made on a tracked document")
	comment.AddCommand(
		opCommand(l, ops.DocCommentAdd, binding[ops.DocCommentAddInput, ops.DocCommentAddOutput]{
			Use:  "add <id> --by <who> --text <comment> [--quote <text>] [--version <n>]",
			Args: cobra.ExactArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.DocCommentAddInput) {
				fs.StringVar(&in.By, "by", "", "person id, or a name")
				fs.StringVar(&in.Text, "text", "", "the comment")
				fs.StringVar(&in.Quote, "quote", "", "the text of the document it is about")
				fs.IntVar(&in.Version, "version", 0, "the version quoted; default the latest")
				fs.IntVar(&start, "start", -1, "where the quote starts in that version, in code points")
				fs.IntVar(&end, "end", -1, "where the quote ends")
				fs.StringVar(&in.URL, "url", "", "link to the comment")
				fs.StringVar(&in.At, "at", "", "RFC 3339 time it was made; default now")
				fs.StringVar(&in.Became, "became", "", "id of an Ask, question, or task that already holds it")
			},
			Bind: func(args []string, in *ops.DocCommentAddInput) error {
				in.ID = args[0]
				if start >= 0 {
					in.Start = &start
				}
				if end >= 0 {
					in.End = &end
				}
				return nil
			},
			Text: func(w io.Writer, out ops.DocCommentAddOutput) error {
				_, err := fmt.Fprintf(w, "%s\t%s\n", out.ID, out.Comment)
				return err
			},
		}),
		opCommand(l, ops.DocCommentResolve, binding[ops.DocCommentResolveInput, ops.DocCommentResolveOutput]{
			Use:  "resolve <id> <comment> resolved|dropped|became [--became <id>] [--note <text>]",
			Args: cobra.ExactArgs(3),
			Flags: func(fs *pflag.FlagSet, in *ops.DocCommentResolveInput) {
				fs.StringVar(&in.Became, "became", "", "with became: the Ask, question, or task id")
				fs.StringVar(&in.Note, "note", "", "why; required for dropped")
			},
			Bind: func(args []string, in *ops.DocCommentResolveInput) error {
				in.ID, in.Comment, in.Resolution = args[0], args[1], model.CommentResolution(args[2])
				return nil
			},
			Text: func(w io.Writer, out ops.DocCommentResolveOutput) error {
				_, err := fmt.Fprintf(w, "%s\t%s\t%s\n", out.ID, out.Comment, out.Resolution)
				return err
			},
		}),
		opCommand(l, ops.DocCommentFile, binding[ops.DocCommentFileInput, ops.DocCommentFileOutput]{
			Use:  "file <id> <comment> ask|question|commitment",
			Args: cobra.ExactArgs(3),
			Flags: func(fs *pflag.FlagSet, in *ops.DocCommentFileInput) {
				fs.StringVar(&in.Goal, "goal", "", "goal id; default the document's")
				fs.StringVar(&in.Area, "area", "", "area id; default the document's")
				fs.StringVar(&in.Task, "task", "", "question: scope a task")
				fs.BoolVar(&in.Root, "root", false, "question: scope the whole vault")
				fs.StringVar(&in.Route, "route", "", "Ask: the domain; default the document's route, else its system")
				fs.StringVar(&in.Due, "due", "", "commitment: YYYY-MM-DD; default a week on")
				fs.StringVar(&in.DoneWhen, "done-when", "", "commitment: how we know it is done")
			},
			Bind: func(args []string, in *ops.DocCommentFileInput) error {
				in.ID, in.Comment, in.To = args[0], args[1], args[2]
				return nil
			},
			Text: func(w io.Writer, out ops.DocCommentFileOutput) error {
				p := &printer{w: w}
				for _, wn := range out.Warnings {
					p.f("warning: %s\n", wn)
				}
				p.f("%s\t%s\t%s %s\n", out.ID, out.Comment, out.Type, out.Became)
				return p.err
			},
		}),
	)
	comments := opCommand(l, ops.DocComments, binding[ops.DocCommentsInput, ops.DocCommentsOutput]{
		Use:  "comments <id> [--open]",
		Args: cobra.ExactArgs(1),
		Flags: func(fs *pflag.FlagSet, in *ops.DocCommentsInput) {
			fs.BoolVar(&in.Open, "open", false, "only comments with no resolution")
		},
		Bind: func(args []string, in *ops.DocCommentsInput) error { in.ID = args[0]; return nil },
		Text: func(w io.Writer, out ops.DocCommentsOutput) error {
			p := &printer{w: w}
			for _, c := range out.Comments {
				state := firstNonEmpty(string(c.Resolution), "open")
				if c.Lost {
					state += " (quote gone)"
				}
				p.f("%s\t%s\t%s\t%s\t%s\n", c.ID, state, c.By, oneLine(c.Text), c.Became.Target())
			}
			return p.err
		},
	})
	return []*cobra.Command{track, version, tracked, diff, seen, comment, comments}
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
