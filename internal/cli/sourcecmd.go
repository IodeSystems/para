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

// newSourceCmd is `para source`: where work comes in and goes out. Para
// fetches nothing; a source says how an agent reaches it.
func newSourceCmd(l *loader) *cobra.Command {
	cmd := group("source", "Sources: mailboxes, calendars, chats, trackers, and folders an agent sweeps with its own tools")
	var step struct{ fetch, scanIn, scanOut, state string }
	add := opCommand(l, ops.SourceAdd, binding[ops.SourceAddInput, ops.SourceAddOutput]{
		Use:  "add --title <t> [--example <system>] [flags]",
		Args: cobra.NoArgs,
		Flags: func(fs *pflag.FlagSet, in *ops.SourceAddInput) {
			fs.StringVar(&in.Title, "title", "", "name of the source, such as \"Work mailbox\"")
			fs.StringVar(&in.ID, "id", "", "document id (default the slug of the title)")
			fs.StringVar((*string)(&in.Example), "example", "", "start from the example of this system (para source example)")
			fs.StringVar((*string)(&in.System), "system", "", "gmail, gdocs, gcal, slack, github, jira, confluence, folder, or other")
			fs.StringVar((*string)(&in.Via), "via", "", "mcp, cli, or none (default none)")
			fs.StringVar(&in.Server, "server", "", "MCP server name (via mcp)")
			fs.StringArrayVar(&in.Tools, "tool", nil, "MCP tool name (via mcp); repeatable")
			fs.StringVar(&step.fetch, "fetch", "", "fetch step as JSON: {tool, args, command, query, how}")
			fs.StringVar(&step.scanIn, "scan-in", "", "scan_in step as JSON")
			fs.StringVar(&step.scanOut, "scan-out", "", "scan_out step as JSON")
			fs.StringVar(&step.state, "state", "", "state step as JSON")
			fs.StringArrayVar(&in.ReachedBy, "reached-by", nil, "agent identity that reaches it; repeatable")
			fs.StringVar(&in.Cadence, "cadence", "", "daily, weekly, monthly, quarterly, yearly, or every <n> days")
			fs.StringVar(&in.Note, "note", "", "what the source is to you")
		},
		Bind: func(_ []string, in *ops.SourceAddInput) error {
			for _, p := range []struct {
				flag, raw string
				out       **model.SourceStep
			}{{"fetch", step.fetch, &in.Fetch}, {"scan-in", step.scanIn, &in.ScanIn}, {"scan-out", step.scanOut, &in.ScanOut}, {"state", step.state, &in.State}} {
				if p.raw == "" {
					continue
				}
				var st model.SourceStep
				dec := json.NewDecoder(strings.NewReader(p.raw))
				dec.DisallowUnknownFields()
				if err := dec.Decode(&st); err != nil {
					return fmt.Errorf("--%s: want JSON {tool, args, command, query, how}: %w", p.flag, err)
				}
				*p.out = &st
			}
			return nil
		},
		Text: printID(func(o ops.SourceAddOutput) string { return o.ID }),
	})
	list := opCommand(l, ops.SourceList, binding[ops.SourceListInput, ops.SourceListOutput]{
		Use:  "list [--status active|paused]",
		Args: cobra.NoArgs,
		Flags: func(fs *pflag.FlagSet, in *ops.SourceListInput) {
			fs.StringVar((*string)(&in.Status), "status", "", "active or paused (default all)")
		},
		Text: func(w io.Writer, out ops.SourceListOutput) error {
			p := &printer{w: w}
			for _, s := range out.Sources {
				p.f("%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n", s.ID, s.Status, s.System, s.Via, firstNonEmpty(s.Cadence, "on demand"),
					"last "+firstNonEmpty(s.LastScan, "never"), reachWords(s), overdueWord(s))
			}
			return p.err
		},
	})
	show := opCommand(l, ops.SourceShow, binding[ops.SourceShowInput, ops.SourceShowOutput]{
		Use:  "show <id>",
		Args: cobra.ExactArgs(1),
		Bind: func(args []string, in *ops.SourceShowInput) error { in.ID = args[0]; return nil },
		Text: func(w io.Writer, out ops.SourceShowOutput) error {
			s := out.Source
			p := &printer{w: w}
			p.f("%s\t%s\t%s\t%s\n", s.ID, s.Status, s.System, s.Title)
			p.f("via\t%s %s %s\n", s.Via, s.Server, strings.Join(out.Tools, ", "))
			p.f("cadence\t%s\tdue %s\tlast %s\t%s\n", firstNonEmpty(s.Cadence, "on demand"), firstNonEmpty(s.Due, "-"), firstNonEmpty(s.LastScan, "never"), overdueWord(s))
			p.f("reach\t%s\n", reachWords(s))
			for _, st := range []struct {
				name string
				s    *model.SourceStep
			}{{"fetch", out.Fetch}, {"scan_in", out.ScanIn}, {"scan_out", out.ScanOut}, {"state", out.State}} {
				if st.s != nil {
					p.f("%s\t%s\n", st.name, oneLine(strings.Join([]string{st.s.Tool, st.s.Args, st.s.Command, st.s.Query, st.s.How}, " ")))
				}
			}
			for _, c := range out.Cites {
				p.f("cites\t%s\t%s\t%s\n", c.ID, c.Type, c.Title)
			}
			if out.Body != "" {
				p.f("\n%s\n", out.Body)
			}
			return p.err
		},
	})
	status := opCommand(l, ops.SourceStatus, binding[ops.SourceStatusInput, ops.SourceStatusOutput]{
		Use:  "status <id> active|paused",
		Args: cobra.ExactArgs(2),
		Bind: func(args []string, in *ops.SourceStatusInput) error {
			in.ID, in.Status = args[0], model.SourceStatus(args[1])
			return nil
		},
		Text: func(w io.Writer, out ops.SourceStatusOutput) error {
			_, err := fmt.Fprintf(w, "%s\t%s\n", out.ID, out.Status)
			return err
		},
	})
	example := opCommand(l, ops.SourceExample, binding[ops.SourceExampleInput, ops.SourceExampleOutput]{
		Use:  "example <system>",
		Args: cobra.ExactArgs(1),
		Bind: func(args []string, in *ops.SourceExampleInput) error {
			in.System = model.SourceSystem(args[0])
			return nil
		},
		Text: func(w io.Writer, out ops.SourceExampleOutput) error {
			_, err := io.WriteString(w, out.Text)
			return err
		},
	})
	cmd.AddCommand(add, list, show, status, example)
	return cmd
}

func reachWords(s ops.SourceRow) string {
	var parts []string
	for _, d := range s.Declared {
		w := d.Actor
		if !d.Fresh {
			w += " (old)"
		}
		parts = append(parts, w)
	}
	if len(parts) == 0 {
		parts = append(parts, "no agent declared it")
	}
	if len(s.ReachedBy) > 0 {
		parts = append(parts, "reached_by "+strings.Join(s.ReachedBy, ","))
	}
	return strings.Join(parts, "; ")
}

func overdueWord(s ops.SourceRow) string {
	if s.Overdue {
		return "overdue"
	}
	return ""
}

// newSweepCmd is `para sweep`: the plan an agent follows and the record it
// leaves. Para reads nothing itself.
func newSweepCmd(l *loader) *cobra.Command {
	cmd := group("sweep", "Sweeps: an agent reads its sources with its own tools; para plans and records")
	plan := opCommand(l, ops.SweepPlan, binding[ops.SweepPlanInput, ops.SweepPlanOutput]{
		Use:  "plan [source...]",
		Args: cobra.ArbitraryArgs,
		Bind: func(args []string, in *ops.SweepPlanInput) error { in.Sources = args; return nil },
		Text: func(w io.Writer, out ops.SweepPlanOutput) error {
			p := &printer{w: w}
			for _, s := range out.Sources {
				p.f("%s\t%s\tsince %s\tknown %d\tdrafts %d\tasks %d\ttracked %d\n", s.ID, s.Via, s.Since, len(s.Known), len(s.Drafts), len(s.Asks), len(s.Tracked))
				for _, st := range []struct {
					name string
					s    *ops.SweepStep
				}{{"scan_in", s.ScanIn}, {"scan_out", s.ScanOut}} {
					if st.s != nil {
						p.f("  %s\t%s\n", st.name, oneLine(firstNonEmpty(st.s.Rendered, st.s.Query, st.s.How)))
					}
				}
			}
			for _, sk := range out.Skipped {
				p.f("skipped\t%s\n", sk)
			}
			p.f("next\t%s\n", out.Next)
			return p.err
		},
	})
	var counts []string
	record := opCommand(l, ops.SweepRecord, binding[ops.SweepRecordInput, ops.SweepRecordOutput]{
		Use:  "record <source> [--cursor <c>] [--count in=3]...",
		Args: cobra.ExactArgs(1),
		Flags: func(fs *pflag.FlagSet, in *ops.SweepRecordInput) {
			fs.Var(optString{&in.Cursor}, "cursor", "the cursor your tool returned (unset keeps the stored one)")
			fs.StringArrayVar(&counts, "count", nil, "name=n of what the sweep wrote; repeatable")
			fs.StringArrayVar(&in.Errors, "error", nil, "what went wrong, one line; repeatable")
			fs.BoolVar(&in.Failed, "failed", false, "the scan did not finish: keep last_scan and cursor")
			fs.StringVar(&in.ScannedAt, "scanned-at", "", "RFC 3339 time the scan ran; default now")
			fs.StringVar(&in.Request, "request", "", "the sweep request you claimed: it is ended")
		},
		Bind: func(args []string, in *ops.SweepRecordInput) error {
			in.Source = args[0]
			for _, c := range counts {
				k, v, ok := strings.Cut(c, "=")
				var n int
				if _, err := fmt.Sscanf(v, "%d", &n); !ok || err != nil || k == "" {
					return fmt.Errorf("--count %q: want name=n", c)
				}
				if in.Counts == nil {
					in.Counts = map[string]int{}
				}
				in.Counts[k] = n
			}
			return nil
		},
		Text: func(w io.Writer, out ops.SweepRecordOutput) error {
			_, err := fmt.Fprintf(w, "%s\tlast %s\tnext due %s\n", out.Source, firstNonEmpty(out.LastScan, "never"), firstNonEmpty(out.Due, "-"))
			return err
		},
	})
	cmd.AddCommand(plan, record)
	return cmd
}

// agentSourcesCmd is `para agent sources`: declare the sources this actor
// reaches, on this machine.
func agentSourcesCmd(l *loader) *cobra.Command {
	return opCommand(l, ops.AgentSources, binding[ops.AgentSourcesInput, ops.AgentSourcesOutput]{
		Use:  "sources [source...]",
		Args: cobra.ArbitraryArgs,
		Bind: func(args []string, in *ops.AgentSourcesInput) error {
			in.Sources = append([]string{}, args...)
			return nil
		},
		Text: func(w io.Writer, out ops.AgentSourcesOutput) error {
			p := &printer{w: w}
			p.f("%s\t%s\tdeclared %s\n", out.Actor, out.At, firstNonEmpty(strings.Join(out.Sources, ", "), "none"))
			if len(out.Unknown) > 0 {
				p.f("unknown\t%s\n", strings.Join(out.Unknown, ", "))
			}
			if len(out.Due) > 0 {
				p.f("due\t%s: para sweep plan\n", strings.Join(out.Due, ", "))
			}
			return p.err
		},
	})
}

// optString is a flag that sets *p only when given, so empty is a value.
type optString struct{ p **string }

func (o optString) String() string {
	if o.p == nil || *o.p == nil {
		return ""
	}
	return **o.p
}
func (o optString) Set(v string) error { *o.p = &v; return nil }
func (o optString) Type() string       { return "string" }
