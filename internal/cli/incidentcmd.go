package cli

import (
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/ops"
)

func writeIncidents(p *printer, indent string, list []ops.IncidentView) {
	for _, v := range list {
		p.f("%s%s\t%s\t%s\t%s\t%s\t%s\n", indent, v.ID, v.Severity, v.Status, v.Started, dash(v.Area), v.Title)
	}
}

func changeIncidentText(w io.Writer, out ops.IncidentChangeOutput) error {
	p := &printer{w: w}
	p.f("%s\t%s\t%s\n", out.ID, out.Status, dash(out.Ended))
	for _, wn := range out.Warnings {
		p.f("warning: %s\n", wn)
	}
	return p.err
}

func newIncidentCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "incident", Short: "Incidents: the record of a fault in a system you run (not a pager)", Args: cobra.NoArgs}
	cmd.AddCommand(
		newLinkCmd(l, ops.IncidentLinkAdd, ops.IncidentLinkRemove),
		opCommand(l, ops.IncidentAdd, binding[ops.IncidentAddInput, ops.IncidentAddOutput]{
			Use:  "add <title>",
			Args: cobra.MinimumNArgs(1),
			Flags: func(fs *pflag.FlagSet, in *ops.IncidentAddInput) {
				fs.StringVar((*string)(&in.Severity), "severity", "", "sev1, sev2, or sev3 (required)")
				fs.StringVar(&in.Area, "area", "", "area id of the system")
				fs.StringVar(&in.Started, "started", "", "RFC 3339 start time; default now")
				fs.StringVar(&in.Body, "body", "", "what is known, before the timeline")
			},
			Bind: func(args []string, in *ops.IncidentAddInput) error {
				in.Title = strings.Join(args, " ")
				return nil
			},
			Text: printID(func(o ops.IncidentAddOutput) string { return o.ID }),
		}),
		opCommand(l, ops.IncidentList, binding[ops.IncidentListInput, ops.IncidentListOutput]{
			Use:  "list",
			Args: cobra.NoArgs,
			Flags: func(fs *pflag.FlagSet, in *ops.IncidentListInput) {
				fs.StringVar((*string)(&in.Status), "status", "", "only open, mitigated, or resolved")
				fs.StringVar(&in.Area, "area", "", "only this area")
				fs.BoolVar(&in.All, "all", false, "include resolved incidents")
			},
			Text: func(w io.Writer, out ops.IncidentListOutput) error {
				p := &printer{w: w}
				writeIncidents(p, "", out.Incidents)
				return p.err
			},
		}),
		opCommand(l, ops.IncidentShow, binding[ops.IncidentShowInput, ops.IncidentShowOutput]{
			Use:  "show <id>",
			Args: cobra.ExactArgs(1),
			Bind: func(args []string, in *ops.IncidentShowInput) error { in.ID = args[0]; return nil },
			Text: func(w io.Writer, out ops.IncidentShowOutput) error {
				p := &printer{w: w}
				v := out.Incident
				p.f("%s\t%s\t%s\t%s\t%s\t%s\n", v.ID, v.Severity, v.Status, v.Started, dash(v.Ended), v.Title)
				p.f("area\t%s\n", dash(v.Area))
				writeLinks(p, v.Links)
				p.f("%s\n", out.Body)
				p.f("follow-ups\n")
				if p.err == nil {
					p.err = writeTasks(w, out.FollowUps)
				}
				return p.err
			},
		}),
		opCommand(l, ops.IncidentNote, binding[ops.IncidentNoteInput, ops.IncidentChangeOutput]{
			Use:  "note <id> <text>",
			Args: cobra.MinimumNArgs(2),
			Flags: func(fs *pflag.FlagSet, in *ops.IncidentNoteInput) {
				fs.StringVar(&in.At, "at", "", "RFC 3339 time of the event; default now")
			},
			Bind: func(args []string, in *ops.IncidentNoteInput) error {
				in.ID, in.Text = args[0], strings.Join(args[1:], " ")
				return nil
			},
			Text: changeIncidentText,
		}),
		opCommand(l, ops.IncidentStatus, binding[ops.IncidentStatusInput, ops.IncidentChangeOutput]{
			Use:  "status <id> <open|mitigated|resolved>",
			Args: cobra.ExactArgs(2),
			Flags: func(fs *pflag.FlagSet, in *ops.IncidentStatusInput) {
				fs.StringVar(&in.Text, "note", "", "text for the timeline line")
			},
			Bind: func(args []string, in *ops.IncidentStatusInput) error {
				in.ID, in.Status = args[0], model.IncidentStatus(args[1])
				return nil
			},
			Text: changeIncidentText,
		}),
	)
	return cmd
}
