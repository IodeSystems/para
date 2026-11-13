package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/para/internal/ops"
)

// newAttachmentCmd: files attached to an item, stored in the vault under
// attachments/<id>/.
func newAttachmentCmd(l *loader) *cobra.Command {
	cmd := group("attachment", "Files attached to an item: images, audio, video, PDF, and text, stored in the vault under attachments/<id>/")
	cmd.AddCommand(
		opCommand(l, ops.AttachmentAdd, binding[ops.AttachmentAddInput, ops.AttachmentAddOutput]{
			Use: "add <id> <file>", Args: cobra.ExactArgs(2),
			Bind: func(args []string, in *ops.AttachmentAddInput) error {
				data, err := os.ReadFile(args[1])
				if err != nil {
					return fmt.Errorf("cli: attachment add: %w", err)
				}
				in.ID, in.Name, in.Data = args[0], filepath.Base(args[1]), data
				return nil
			},
			Text: func(w io.Writer, out ops.AttachmentAddOutput) error {
				p := &printer{w: w}
				p.f("%s\t%s\t%d bytes\n%s\n", out.Path, out.Mime, out.Size, out.Embed)
				return p.err
			},
		}),
		opCommand(l, ops.AttachmentRead, binding[ops.AttachmentReadInput, ops.AttachmentReadOutput]{
			Use: "read <file>", Args: cobra.ExactArgs(1),
			Bind: func(args []string, in *ops.AttachmentReadInput) error { in.File = args[0]; return nil },
			Text: func(w io.Writer, out ops.AttachmentReadOutput) error {
				p := &printer{w: w}
				p.f("%s\t%s\t%d bytes\t%s\n", out.Path, out.Mime, out.Size, out.SHA256)
				if out.Note != "" {
					p.f("note: %s\n", out.Note)
				}
				if out.Text != "" {
					p.f("\n%s\n", out.Text)
				}
				return p.err
			},
		}),
	)
	return cmd
}

// newItemCmd: what the composer at the foot of any item's page does from a
// terminal. agent send is under para agent.
func newItemCmd(l *loader) *cobra.Command {
	cmd := group("item", "Any item, by id: close a task or a meeting from its page, or record that an agent prepared it")
	cmd.AddCommand(opCommand(l, ops.ItemClose, binding[ops.ItemCloseInput, ops.ItemCloseOutput]{
		Use: "close <id>", Args: cobra.ExactArgs(1),
		Bind: func(args []string, in *ops.ItemCloseInput) error { in.ID = args[0]; return nil },
		Text: printID(func(o ops.ItemCloseOutput) string { return o.ID + "\t" + o.By }),
	}))
	cmd.AddCommand(opCommand(l, ops.ItemPrepared, binding[ops.ItemPreparedInput, ops.ItemPreparedOutput]{
		Use: "prepared <id> --kind draft|answer|fields|brief|plan|none [--ref <id>]", Args: cobra.ExactArgs(1),
		Flags: func(fs *pflag.FlagSet, in *ops.ItemPreparedInput) {
			fs.StringVar((*string)(&in.Kind), "kind", "", "what was prepared: draft, answer, fields, brief, plan, or none (a proposed close takes close: use op_run)")
			fs.StringVar(&in.Ref, "ref", "", "the document that holds it, such as an io draft; empty when it is on the item")
		},
		Bind: func(args []string, in *ops.ItemPreparedInput) error { in.ID = args[0]; return nil },
		Text: printID(func(o ops.ItemPreparedOutput) string { return o.ID }),
	}))
	return cmd
}

// newPreparedCmd is `para prepared`: what an agent prepared for an item that
// the owner approves, such as a proposed close of a task.
func newPreparedCmd(l *loader) *cobra.Command {
	cmd := group("prepared", "What an agent prepared for the owner to approve: a proposed close of a task")
	cmd.AddCommand(opCommand(l, ops.PreparedClose, binding[ops.PreparedCloseInput, ops.PreparedCloseOutput]{
		Use: "close <id>", Args: cobra.ExactArgs(1),
		Bind: func(args []string, in *ops.PreparedCloseInput) error { in.ID = args[0]; return nil },
		Text: func(w io.Writer, o ops.PreparedCloseOutput) error {
			if !o.Proposed {
				_, err := fmt.Fprintf(w, "%s\tno proposed close\n", o.ID)
				return err
			}
			state := "ready"
			if !o.Ready {
				state = "stale"
			}
			loc := "-"
			if o.Close.Evidence != nil {
				loc = o.Close.Evidence.Locator
			}
			_, err := fmt.Fprintf(w, "%s\t%s\tlikely %s, %d%%\t%s\t%s\tby %s\n", o.ID, state, o.Close.Outcome, o.Close.Confidence, o.Close.Reason, loc, o.By)
			return err
		},
	}))
	cmd.AddCommand(opCommand(l, ops.PreparedCloseApprove, binding[ops.PreparedCloseInput, ops.PreparedCloseApproveOutput]{
		Use: "close-approve <id>", Args: cobra.ExactArgs(1),
		Bind: func(args []string, in *ops.PreparedCloseInput) error { in.ID = args[0]; return nil },
		Text: printID(func(o ops.PreparedCloseApproveOutput) string { return o.ID + "\t" + o.Outcome + "\t" + o.Receipt }),
	}))
	return cmd
}

// agentSendCmds: what the owner writes about an item, sent to an agent, and
// taking it back.
func agentSendCmds(l *loader) []*cobra.Command {
	var atts []string
	var done bool
	send := opCommand(l, ops.AgentSend, binding[ops.AgentSendInput, ops.AgentSendOutput]{
		Use: "send <id> [text] [--attach <path>]... [--done] (or the text on stdin)", Args: cobra.MinimumNArgs(1),
		Flags: func(fs *pflag.FlagSet, in *ops.AgentSendInput) {
			fs.StringArrayVar(&atts, "attach", nil, "vault path of an attachment (para attachment add); repeat for more")
			fs.BoolVar(&done, "done", false, "ask the agent to close the item when it is done")
		},
		Bind: func(args []string, in *ops.AgentSendInput) error {
			in.ID, in.Text, in.Attachments, in.Done = args[0], strings.Join(args[1:], " "), atts, done
			return nil
		},
		Stdin: func(r io.Reader, in *ops.AgentSendInput) error {
			if in.Text != "" || len(in.Attachments) > 0 {
				return nil
			}
			text, err := readStdin(r)
			in.Text = text
			return err
		},
		Text: func(w io.Writer, out ops.AgentSendOutput) error {
			p := &printer{w: w}
			p.f("%s\t%s\t%s\n", out.Request, out.Status, out.Title)
			if out.Note != "" {
				p.f("note: %s\n", out.Note)
			}
			return p.err
		},
	})
	return []*cobra.Command{send}
}
