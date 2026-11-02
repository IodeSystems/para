// Package ops holds every operation para offers. An op is a typed function.
// Each surface is a thin wrapper over it: the CLI binds flags to Input and
// prints Output, and a later MCP surface iterates All. To add an op, add a
// file with Input and Output structs (JSON tags on every field), a function,
// and one `var X = define(...)`.
package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/iodesystems/parasolve/internal/store"
)

// Deps is what an op may use. A surface builds it per call or per session.
type Deps struct {
	Store store.Store
	// Authorize says whether the caller may cause op to run. An approved
	// proposal item runs under the proposing agent, so the server uses this to
	// keep the approver's own scopes in force. Nil allows every op: the CLI and
	// the TUI are the owner on this machine.
	Authorize func(op string) error
	// Changed, when set, returns a channel that closes at the next change of
	// the vault, from any process. A waiting op blocks on it instead of
	// polling files. Take it before reading state, so no change is missed.
	Changed func() <-chan struct{}
}

// Func is the shape of every op.
type Func[I, O any] func(ctx context.Context, d Deps, in I) (O, error)

// Op is a typed, named op. Surfaces with a compile-time reference use Run.
type Op[I, O any] struct {
	name   string
	desc   string
	fn     Func[I, O]
	person bool
}

// Entry is the untyped view of an Op, for surfaces that iterate All.
type Entry interface {
	// Name is the CLI path joined by '_': `para inbox add` is inbox_add.
	Name() string
	Description() string
	// PersonOnly marks an op no agent may call. It is refused inside the op, and
	// MCP does not list it (ForAgents).
	PersonOnly() bool
	InputType() reflect.Type
	OutputType() reflect.Type
	// Invoke decodes a JSON Input and returns the Output. An unknown field is
	// an error. Empty input means the zero Input.
	Invoke(ctx context.Context, d Deps, input json.RawMessage) (any, error)
}

// Failing is implemented by an Output that can report a failed result with
// useful data, such as lint findings. The surface emits the Output, then
// exits non-zero or marks the result as an error.
type Failing interface {
	Failed() error
}

func (o *Op[I, O]) Name() string        { return o.name }
func (o *Op[I, O]) Description() string { return o.desc }
func (o *Op[I, O]) PersonOnly() bool    { return o.person }

func (o *Op[I, O]) InputType() reflect.Type  { return reflect.TypeFor[I]() }
func (o *Op[I, O]) OutputType() reflect.Type { return reflect.TypeFor[O]() }

// Run calls the op.
func (o *Op[I, O]) Run(ctx context.Context, d Deps, in I) (O, error) {
	out, err := o.fn(ctx, d, in)
	if err != nil {
		return out, fmt.Errorf("%s: %w", o.name, err)
	}
	return out, nil
}

// quotedPath finds a quoted absolute path, which may hold spaces.
var quotedPath = regexp.MustCompile(`(["'])/[^"'\n]*(["'])`)

// absPath finds absolute filesystem paths in error text.
var absPath = regexp.MustCompile(`(^|[\s"'(=:])(/[^\s"'):,;]+)`)

// PublicError returns error text for a client of a surface: one line, with
// the prefixes in hide and every other absolute path removed.
func PublicError(err error, hide []string) string {
	msg := err.Error()
	for _, h := range hide {
		if h != "" && h != "/" {
			msg = strings.ReplaceAll(msg, h, "<vault>")
		}
	}
	msg = quotedPath.ReplaceAllString(msg, "${1}<path>${2}")
	msg = absPath.ReplaceAllString(msg, "${1}<path>")
	if i := strings.IndexAny(msg, "\r\n"); i >= 0 {
		msg = msg[:i]
	}
	return msg
}

// Invoke implements Entry.
func (o *Op[I, O]) Invoke(ctx context.Context, d Deps, input json.RawMessage) (any, error) {
	var in I
	if len(bytes.TrimSpace(input)) > 0 {
		dec := json.NewDecoder(bytes.NewReader(input))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&in); err != nil {
			return nil, fmt.Errorf("%s: input: %w", o.name, err)
		}
	}
	return o.Run(ctx, d, in)
}

var (
	registry = map[string]Entry{}
	nameRe   = regexp.MustCompile(`^[a-z]+(?:_[a-z]+)*$`)
)

// define registers an op at package initialization. It panics on a bad or
// repeated name: that is a programming error.
func define[I, O any](name, desc string, fn Func[I, O]) *Op[I, O] {
	if !nameRe.MatchString(name) {
		panic("ops: bad op name " + name)
	}
	if _, dup := registry[name]; dup {
		panic("ops: duplicate op " + name)
	}
	op := &Op[I, O]{name: name, desc: desc, fn: fn}
	registry[name] = op
	return op
}

// ErrPersonOnly is a person-only op called from an agent surface.
var ErrPersonOnly = errors.New("only a person can do this: use the terminal or a browser session")

// PersonSurfaces can call a person-only op. A bearer key is surface api and an
// MCP server is surface mcp: both are agents here, whatever their scopes.
var PersonSurfaces = []string{"cli", "tui", "pwa"}

// definePerson registers an op that refuses every surface but a person's. The
// check sits in the op, so no surface can forget it.
func definePerson[I, O any](name, desc string, fn Func[I, O]) *Op[I, O] {
	op := define(name, desc, func(ctx context.Context, d Deps, in I) (O, error) {
		if !slices.Contains(PersonSurfaces, d.Store.Info().Surface) {
			var zero O
			return zero, ErrPersonOnly
		}
		return fn(ctx, d, in)
	})
	op.person = true
	return op
}

// ForAgents returns the ops an agent surface lists, sorted by name.
func ForAgents() []Entry {
	return slices.DeleteFunc(All(), Entry.PersonOnly)
}

// All returns every op, sorted by name.
func All() []Entry {
	out := make([]Entry, 0, len(registry))
	for _, e := range registry {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out
}
