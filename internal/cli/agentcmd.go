package cli

import (
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/iodesystems/parasolve/internal/dispatch"
)

// agentStatusOutput is the output of para agent status.
type agentStatusOutput struct {
	Vault string `json:"vault"`
	dispatch.Status
	// Serving says whether a para serve runs on this install. Dispatch lives in
	// para serve: with none running, nothing is started.
	Serving bool   `json:"serving"`
	File    string `json:"file"` // where agent.json is read from
}

func writeAgentStatus(w io.Writer, o agentStatusOutput) error {
	p := &printer{w: w}
	p.f("vault\t%s\n", o.Vault)
	switch {
	case o.Error != "":
		p.f("config\terror: %s\n", o.Error)
	case !o.Configured:
		p.f("dispatch\tnot configured: para starts nothing. Put a command in %s to turn it on.\n", o.File)
	default:
		p.f("dispatch\tconfigured (%s)\n", o.File)
		p.f("command\t%s (%d arguments, not shown)\n", o.Program, o.Args)
		p.f("cwd\t%s\n", o.Cwd)
		p.f("limits\t%d at a time, %d starts a day\n", o.MaxConcurrent, o.DailyLimit)
		p.f("today\t%d of %d started (%s)\n", o.Started, o.DailyLimit, o.Day)
		if o.Serving {
			p.f("serve\trunning: it starts the command when a request can be claimed here\n")
		} else {
			p.f("serve\tnot running: nothing is started until para serve runs\n")
		}
		for _, r := range o.Running {
			p.f("running\t%s\tpid %d\tsince %s\n", r.ID, r.PID, r.Started.Local().Format("2006-01-02 15:04"))
		}
		for _, e := range o.Last {
			p.f("last\t%s\t%s\t%s\t%s\n", e.ID, e.Status, e.Ended.Local().Format("2006-01-02 15:04"), e.Log)
		}
		p.f("logs\t%s (mode 0600)\n", o.LogDir)
		p.f("note\ta run starts an agent: it spends tokens. The limits above bound it.\n")
	}
	return p.err
}

func newAgentCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Dispatch: the command para serve starts when agent work can be claimed on this machine",
		Long: "Dispatch is off unless $PARA_HOME/vaults/<id>/agent.json exists on this machine.\n" +
			"It is never in the vault. See para mcp --help-claude and para mcp --help-dun.",
		Args: cobra.NoArgs,
	}
	status := &cobra.Command{
		Use:   "status",
		Short: "Show the dispatch config, today's count, what runs, and how the last runs ended",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := l.load(); err != nil {
				return err
			}
			sel, err := l.selectVault()
			if err != nil {
				return err
			}
			state := l.stateDir(sel.ID)
			out := agentStatusOutput{
				Vault: sel.ID, Status: dispatch.ReadStatus(state, time.Now()),
				Serving: l.findServer().Rec != nil, File: dispatch.ConfigPath(state),
			}
			return emit(cmd, out, writeAgentStatus)
		},
	}
	addJSONFlag(status)
	cmd.AddCommand(status)
	return cmd
}
