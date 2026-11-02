package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/paradize/internal/ops"
)

// opAnnotation names the op a command wraps. The parity test reads it.
const opAnnotation = "para.op"

// binding is everything the CLI adds to an op: argument shape, flags, and the
// text form of the output. It holds no behavior.
type binding[I, O any] struct {
	Use   string
	Args  cobra.PositionalArgs
	Flags func(fs *pflag.FlagSet, in *I)   // optional
	Bind  func(args []string, in *I) error // optional
	Text  func(w io.Writer, out O) error
}

// opCommand wraps an op as a command. stdout gets the Output as JSON with
// --json, or text. Diagnostics go to stderr through the returned error.
func opCommand[I, O any](l *loader, op *ops.Op[I, O], b binding[I, O]) *cobra.Command {
	var in I
	cmd := &cobra.Command{
		Use:         b.Use,
		Short:       op.Description(),
		Args:        b.Args,
		Annotations: map[string]string{opAnnotation: op.Name()},
	}
	addJSONFlag(cmd)
	if b.Flags != nil {
		b.Flags(cmd.Flags(), &in)
	}
	cmd.RunE = func(cmd *cobra.Command, args []string) error {
		if err := l.load(); err != nil {
			return err
		}
		if b.Bind != nil {
			if err := b.Bind(args, &in); err != nil {
				return err
			}
		}
		deps, err := l.deps()
		if err != nil {
			return err
		}
		out, err := op.Run(cmd.Context(), deps, in)
		if err != nil {
			return err
		}
		return emit(cmd, out, b.Text)
	}
	return cmd
}

func addJSONFlag(cmd *cobra.Command) {
	cmd.Flags().Bool("json", false, "print the output as JSON on stdout")
}

// emit prints out in the format the command asked for. An Output that
// implements ops.Failing is printed first, then fails the command.
func emit[O any](cmd *cobra.Command, out O, text func(io.Writer, O) error) error {
	w := cmd.OutOrStdout()
	asJSON, err := cmd.Flags().GetBool("json")
	if err != nil {
		return fmt.Errorf("cli: emit: %w", err)
	}
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		err = enc.Encode(out)
	} else {
		err = text(w, out)
	}
	if err != nil {
		return fmt.Errorf("cli: emit: %w", err)
	}
	if f, ok := any(out).(ops.Failing); ok {
		return f.Failed()
	}
	return nil
}
