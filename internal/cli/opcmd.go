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
		l.ratingsNote(cmd)
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

// writeContext carries --why, --source, and --request onto the context of a
// write made as an agent, the way the MCP server does. An op with a why input
// of its own keeps that field's meaning, and its value serves the receipt; so
// does a request input, as over MCP. A command with its own --source or
// --request flag hides the global one. As the owner no receipt is filed, so a
// write refuses --source and --request.
func (l *loader) writeContext(ctx context.Context, e ops.Entry, in any) (context.Context, error) {
	if !ops.NeedsWhy(e) {
		return ctx, nil
	}
	if !l.agent() {
		if len(l.sourceFlags) > 0 || strings.TrimSpace(l.requestFlag) != "" {
			return ctx, fmt.Errorf("%s: --source and --request need --actor: an owner write files no receipt", e.Name())
		}
		return ctx, nil
	}
	own := inputFields(in)
	why := strings.TrimSpace(l.whyFlag)
	if why == "" {
		why = strings.TrimSpace(fieldString(own, "why"))
	}
	if why == "" {
		return ctx, fmt.Errorf("%s: --why is required: say why you make this write as actor %s", e.Name(), l.actor())
	}
	m := ops.WriteMeta{Why: why, Request: strings.TrimSpace(l.requestFlag)}
	if m.Request == "" {
		m.Request = strings.TrimSpace(fieldString(own, "request"))
	}
	for _, s := range l.sourceFlags {
		if s = strings.TrimSpace(s); s != "" {
			m.Sources = append(m.Sources, s)
		}
	}
	return ops.WithWrite(ctx, m), nil
}

// inputFields is an op input as its JSON fields; nil when it is not an object.
func inputFields(in any) map[string]json.RawMessage {
	b, err := json.Marshal(in)
	if err != nil {
		return nil
	}
	var f map[string]json.RawMessage
	_ = json.Unmarshal(b, &f)
	return f
}

// fieldString reads a string field; a missing or non-string field reads as "".
func fieldString(f map[string]json.RawMessage, name string) string {
	var s string
	_ = json.Unmarshal(f[name], &s)
	return s
}

// ratingsNote says on stderr that --minor, --obvious, and --dossier decide
// nothing since 0.3.0. They are accepted for one release.
func (l *loader) ratingsNote(cmd *cobra.Command) {
	var names []string
	for _, f := range []struct {
		name string
		set  bool
	}{{"minor", l.minorFlag}, {"obvious", l.obviousFlag}, {"dossier", strings.TrimSpace(l.dossierFlag) != ""}} {
		if f.set {
			names = append(names, f.name)
		}
	}
	if len(names) > 0 {
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "note: %s\n", ops.RatingsNote(names))
	}
}
