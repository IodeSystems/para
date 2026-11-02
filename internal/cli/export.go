package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/paradize/internal/export"
	"github.com/iodesystems/paradize/internal/ops"
)

// newExportCmd is `para export <id> --as <format> --to <transport>`, plus
// `para export list` (also `--list`). The export op renders and logs; this
// command runs the transport. That split keeps every transport that acts on
// the machine out of the op, so no MCP caller can reach one.
func newExportCmd(l *loader) *cobra.Command {
	var list bool
	var to string
	var run func(*cobra.Command, []string) error
	var cmdRef *cobra.Command
	cmd := opCommand(l, ops.Export, binding[ops.ExportInput, ops.ExportOutput]{
		Use:  "export <id> [--as plain|md|email|slack|jira|html|pdf|docx] [--to stdout|file:<path>|clipboard|osc52|tmux|wormhole|qr|ssh:<host>:<path>]",
		Args: cobra.ArbitraryArgs,
		Flags: func(fs *pflag.FlagSet, in *ops.ExportInput) {
			fs.StringVar((*string)(&in.As), "as", "plain", "plain, md, email, slack, jira, html, pdf, or docx (html, pdf, docx need pandoc)")
			fs.StringVar(&to, "to", "stdout", "stdout, file:<path>, clipboard, osc52, tmux, wormhole, qr, or ssh:<host>:<path>")
			fs.BoolVar(&list, "list", false, "list the formats and transports available here")
		},
		Bind: func(args []string, in *ops.ExportInput) error {
			if len(args) != 1 {
				return errors.New("export needs one document id (or --list)")
			}
			in.ID, in.To = args[0], to
			return nil
		},
		Text: func(w io.Writer, out ops.ExportOutput) error {
			r, err := out.Rendered()
			if err != nil {
				return err
			}
			target, err := export.ParseTarget(out.To)
			if err != nil {
				return err
			}
			errw := cmdRef.ErrOrStderr()
			detail, err := export.Send(cmdRef.Context(), target, r, out.ID, export.IO{
				Stdout: w, Stderr: errw, Terminal: openTerminal,
			})
			if err != nil {
				return err
			}
			if detail != "" {
				_, err = fmt.Fprintln(errw, detail)
			}
			return err
		},
	})
	listCmd := opCommand(l, ops.ExportList, binding[ops.ExportListInput, ops.ExportListOutput]{
		Use: "list", Args: cobra.NoArgs,
		Text: func(w io.Writer, out ops.ExportListOutput) error {
			for _, group := range []struct {
				name  string
				items []export.Item
			}{{"formats", out.Formats}, {"transports", out.Transports}} {
				if _, err := fmt.Fprintln(w, group.name+":"); err != nil {
					return err
				}
				for _, it := range group.items {
					if _, err := fmt.Fprintln(w, "  "+itemLine(it)); err != nil {
						return err
					}
				}
			}
			return nil
		},
	})
	cmd.AddCommand(listCmd)
	run = cmd.RunE
	cmd.RunE = func(c *cobra.Command, args []string) error {
		cmdRef = c
		if list {
			if len(args) != 0 {
				return errors.New("--list takes no document id")
			}
			listCmd.SetOut(c.OutOrStdout())
			listCmd.SetErr(c.ErrOrStderr())
			if j, _ := c.Flags().GetBool("json"); j {
				_ = listCmd.Flags().Set("json", "true")
			}
			listCmd.SetContext(c.Context())
			return listCmd.RunE(listCmd, nil)
		}
		if j, _ := c.Flags().GetBool("json"); j && to != "stdout" && to != "" {
			return errors.New("--to and --json cannot be combined: JSON output is not sent to a transport")
		}
		return run(c, args)
	}
	return cmd
}

func itemLine(it export.Item) string {
	status := "missing"
	if it.Available {
		status = "ok"
	}
	line := fmt.Sprintf("%-10s %s", it.Name, status)
	switch {
	case it.Found != "":
		line += "  (" + it.Found + ")"
	case !it.Available && len(it.Needs) > 0:
		line += "  needs " + strings.Join(it.Needs, " or ")
	}
	if it.Arg != "" {
		line += "  " + it.Arg
	}
	return line
}

// openTerminal opens the controlling terminal for osc52, so the sequence
// reaches the terminal even when stdout is a pipe.
func openTerminal() (io.WriteCloser, error) {
	return os.OpenFile("/dev/tty", os.O_WRONLY, 0)
}
