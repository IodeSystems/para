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

// newRetryCmd builds `para retry`: the work on a write that lint refused.
func newRetryCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{Use: "retry", Short: "Agent writes that lint refused: propose a fix, decide on it", Args: cobra.NoArgs}
	text := func(w io.Writer, id string, status any) error {
		_, err := fmt.Fprintf(w, "%s\t%v\n", id, status)
		return err
	}
	// input reads a JSON object flag into m.
	input := func(fs *pflag.FlagSet, raw *string) {
		fs.StringVar(raw, "input", "", "the op input, as a JSON object")
	}
	parse := func(raw string, m *map[string]any) error {
		if strings.TrimSpace(raw) == "" {
			return nil
		}
		if err := json.Unmarshal([]byte(raw), m); err != nil {
			return fmt.Errorf("--input: %w", err)
		}
		return nil
	}
	var proposeRaw, decideRaw string
	cmd.AddCommand(
		opCommand(l, ops.RetryPropose, binding[ops.RetryProposeInput, ops.RetryProposeOutput]{
			Use:  "propose <request> <why>",
			Args: cobra.MinimumNArgs(2),
			Flags: func(fs *pflag.FlagSet, in *ops.RetryProposeInput) {
				input(fs, &proposeRaw)
				fs.StringSliceVar(&in.Links, "link", nil, "id the proposal rests on (repeatable)")
			},
			Bind: func(args []string, in *ops.RetryProposeInput) error {
				in.Request, in.Why = args[0], strings.Join(args[1:], " ")
				return parse(proposeRaw, &in.Input)
			},
			Text: func(w io.Writer, out ops.RetryProposeOutput) error { return text(w, out.ID, out.Status) },
		}),
		opCommand(l, ops.RetryDecide, binding[ops.RetryDecideInput, ops.RetryDecideOutput]{
			Use:  "decide <request> accept|edit|drop|reject",
			Args: cobra.ExactArgs(2),
			Flags: func(fs *pflag.FlagSet, in *ops.RetryDecideInput) {
				input(fs, &decideRaw)
				fs.StringVar(&in.Notes, "notes", "", "reject: what the agent should change; drop: why")
				fs.BoolVar(&in.Approve, "approve", false, "with edit: also run the op with the input")
			},
			Bind: func(args []string, in *ops.RetryDecideInput) error {
				in.Request, in.Action = args[0], ops.RetryAction(args[1])
				return parse(decideRaw, &in.Input)
			},
			Text: func(w io.Writer, out ops.RetryDecideOutput) error { return text(w, out.ID, out.Status) },
		}),
	)
	return cmd
}
