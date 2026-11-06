package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/iodesystems/para/internal/ops"
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
	// Stdin reads the command's standard input into the Input; optional.
	Stdin func(r io.Reader, in *I) error
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
		if b.Stdin != nil {
			if err := b.Stdin(cmd.InOrStdin(), &in); err != nil {
				return err
			}
		}
		ctx, err := l.writeContext(cmd.Context(), op, in)
		if err != nil {
			return err
		}
		deps, err := l.deps()
		if err != nil {
			return err
		}
		for _, s := range ops.SimilarFor(ctx, deps.Store, op.Name(), in) {
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "similar: %s\n", s.Title)
		}
		if a := ops.Alias(op.Name()); a != nil {
			return runAlias(cmd, a, ctx, deps, in, b.Text)
		}
		out, err := op.Run(ctx, deps, in)
		if s, ok := ops.AsStaged(err); ok {
			return emitStaged(cmd, s)
		}
		if err != nil {
			return err
		}
		return emit(cmd, out, b.Text)
	}
	return cmd
}

// runAlias runs an old op name through its alias (ops.Alias), as MCP does.
// --json prints the output with alias_of; the text form is the old op's, with
// a note on stderr that names the op to use.
func runAlias[I, O any](cmd *cobra.Command, a ops.Entry, ctx context.Context, deps ops.Deps, in I, text func(io.Writer, O) error) error {
	raw, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("cli: %s: %w", a.Name(), err)
	}
	res, err := a.Invoke(ctx, deps, raw)
	if s, ok := ops.AsStaged(err); ok {
		return emitStaged(cmd, s)
	}
	if err != nil {
		return err
	}
	ao, _ := res.(ops.AliasOutput)
	if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
		return emit(cmd, ao, func(io.Writer, ops.AliasOutput) error { return nil })
	}
	_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "note: %s is an alias of %s; use %s\n", a.Name(), ao.Of(), ao.Of())
	out, ok := ao.Value().(O)
	if !ok {
		b, err := json.Marshal(ao.Value())
		if err == nil {
			err = json.Unmarshal(b, &out)
		}
		if err != nil {
			return fmt.Errorf("cli: %s: output: %w", a.Name(), err)
		}
	}
	return emit(cmd, out, text)
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

// writeContext carries --why onto the context of a write made as an agent, the
// way the MCP server does. An op with a why input of its own keeps that field's
// meaning, and its value serves the receipt. As the owner it changes nothing.
func (l *loader) writeContext(ctx context.Context, e ops.Entry, in any) (context.Context, error) {
	if !l.agent() || !ops.NeedsWhy(e) {
		return ctx, nil
	}
	why := strings.TrimSpace(l.whyFlag)
	if why == "" {
		if b, err := json.Marshal(in); err == nil {
			var m struct {
				Why string `json:"why"`
			}
			if json.Unmarshal(b, &m) == nil {
				why = strings.TrimSpace(m.Why)
			}
		}
	}
	if why == "" {
		return ctx, fmt.Errorf("%s: --why is required: say why you make this write as actor %s", e.Name(), l.actor())
	}
	return ops.WithWrite(ctx, ops.WriteMeta{Why: why, Minor: l.minorFlag, Obvious: l.obviousFlag, Dossier: strings.TrimSpace(l.dossierFlag)}), nil
}

// emitStaged prints a write the gate held back. It is a success: the write
// waits in the review stack.
func emitStaged(cmd *cobra.Command, s *ops.Staged) error {
	return emit(cmd, *s, func(w io.Writer, s ops.Staged) error {
		p := &printer{w: w}
		p.f("staged\t%s\t%s\t%s\n%s\n", s.Proposal, s.Item, strings.Join(s.Missing, ","), s.Message)
		if s.URL != "" {
			p.f("url: %s\n", s.URL)
		}
		return p.err
	})
}
