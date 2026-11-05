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

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// Deps is what an op may use. A surface builds it per call or per session.
type Deps struct {
	Store store.Store
	// Authorize says whether the caller may cause op to run. An approved
	// proposal item runs under the proposing agent, so the server uses this to
	// keep the caller's own scopes in force. Nil allows every op: the CLI is
	// the owner on this machine.
	Authorize func(op string) error
	// ViewerBase returns the base URL of the para serve that serves this vault
	// on this machine, such as http://acme.localhost:7700, or an error that says
	// how to start one. Nil: no server is known here.
	ViewerBase func() (string, error)
	// OpenURL opens a URL in a browser on the machine para runs on and returns
	// the command it ran. Nil: nothing can be opened here; the URL is still
	// returned.
	OpenURL func(url string) (command string, err error)
	// SignedIn reports whether the owner's browser holds a login key for this
	// vault. Nil: not known, or the opener signs in itself.
	SignedIn func() bool
	// Changed, when set, returns a channel that closes at the next change of
	// the vault, from any process. A waiting op blocks on it instead of
	// polling files. Take it before reading state, so no change is missed.
	Changed func() <-chan struct{}
	// ChangesSince is the release this MCP session's agent last saw; the
	// changes op uses it when it gets no since. Empty means the newest only.
	ChangesSince string
}

// Func is the shape of every op.
type Func[I, O any] func(ctx context.Context, d Deps, in I) (O, error)

// Op is a typed, named op. Surfaces with a compile-time reference use Run.
type Op[I, O any] struct {
	name   string
	desc   string
	fn     Func[I, O]
	person bool
	risk   Risk
	noUndo bool
	// oneReceipt: a write files one receipt for every document it changed,
	// on every surface, so one undo reverses all of it (merge, archive).
	oneReceipt bool
	// proposalReceipts: a dossier the write changes gets a receipt too, so
	// receipt_undo restores it (tags, deferral). Other writes leave a dossier
	// out: it is its own record.
	proposalReceipts bool
	// gateRule, when set, can let a write through the gate or hold it back
	// whatever gate.require says (withGateRule).
	gateRule func(I, WriteMeta) (gateVerdict, string)
}

// Entry is the untyped view of an Op, for surfaces that iterate All.
type Entry interface {
	// Name is the CLI path joined by '_': `para inbox add` is inbox_add.
	Name() string
	Description() string
	// PersonOnly marks an op no agent may call. It is refused inside the op, and
	// MCP does not list it (ForAgents).
	PersonOnly() bool
	// Risk is the op's class: read, safe, or review.
	Risk() Risk
	// Undoable reports whether receipt_undo can reverse a write of the op. The
	// gate (gate.go) lets only undoable writes apply directly.
	Undoable() bool
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
func (o *Op[I, O]) Risk() Risk          { return o.risk }
func (o *Op[I, O]) Undoable() bool      { return !o.noUndo }

// withNoUndo marks an op whose write receipt_undo cannot reverse: settings,
// keys, and vault upgrades.
func withNoUndo[I, O any](o *Op[I, O]) *Op[I, O] {
	o.noUndo = true
	return o
}

// withProposalReceipts files a receipt for each dossier the op changes.
func withProposalReceipts[I, O any](o *Op[I, O]) *Op[I, O] {
	o.proposalReceipts = true
	return o
}

// withOneReceipt marks an op whose write changes several documents as one
// act: it leaves one receipt on every surface, a person's too, and
// receipt_undo restores every document of it.
func withOneReceipt[I, O any](o *Op[I, O]) *Op[I, O] {
	o.oneReceipt = true
	return o
}

func (o *Op[I, O]) InputType() reflect.Type  { return reflect.TypeFor[I]() }
func (o *Op[I, O]) OutputType() reflect.Type { return reflect.TypeFor[O]() }

// Run calls the op.
func (o *Op[I, O]) Run(ctx context.Context, d Deps, in I) (O, error) {
	if o.risk != RiskRead && (needsRecord(ctx, d) || (o.oneReceipt && ctx.Value(recordingKey{}) == nil)) {
		return runRecorded(ctx, d, o, in)
	}
	return o.run(ctx, d, in)
}

func (o *Op[I, O]) run(ctx context.Context, d Deps, in I) (O, error) {
	d, err := withImport(d, in)
	if err != nil {
		var zero O
		return zero, fmt.Errorf("%s: %w", o.name, err)
	}
	out, err := o.fn(ctx, d, in)
	if err != nil {
		return out, fmt.Errorf("%s: %w", o.name, err)
	}
	if needsNames(reflect.TypeFor[O]()) {
		fillNames(ctx, d.Store, &out)
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
	var lint *model.LintError
	if errors.As(err, &lint) {
		// The multi-line text would be cut to its first line, which drops
		// every finding. Join the findings on one line.
		msg = strings.ReplaceAll(msg, "\n  ", "; ")
		msg = strings.Replace(msg, "write; ", "write: ", 1)
	}
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
			return nil, fmt.Errorf("%s: input: %w", o.name, inputError(o, err))
		}
	}
	return o.Run(ctx, d, in)
}

var (
	registry = map[string]Entry{}
	nameRe   = regexp.MustCompile(`^[a-z]+(?:_[a-z]+)*$`)
)

// define registers an op of risk review: the class that needs no thought to be
// safe. defineSafe and defineRead state the lower classes, in the definition.
// It panics on a bad or repeated name: that is a programming error.
func define[I, O any](name, desc string, fn Func[I, O]) *Op[I, O] {
	if !nameRe.MatchString(name) {
		panic("ops: bad op name " + name)
	}
	if _, dup := registry[name]; dup {
		panic("ops: duplicate op " + name)
	}
	op := &Op[I, O]{name: name, desc: desc, fn: fn, risk: RiskReview}
	registry[name] = op
	return op
}

// defineRead registers an op that changes no document.
func defineRead[I, O any](name, desc string, fn Func[I, O]) *Op[I, O] {
	op := define(name, desc, fn)
	op.risk = RiskRead
	return op
}

// defineSafe registers an additive, reversible op: capture, note, draft.
func defineSafe[I, O any](name, desc string, fn Func[I, O]) *Op[I, O] {
	op := define(name, desc, fn)
	op.risk = RiskSafe
	return op
}

// ErrPersonOnly is a person-only op called from an agent surface.
var ErrPersonOnly = errors.New("only a person can do this: use the terminal or a browser session")

// PersonSurfaces can call a person-only op. A bearer key is surface api and an
// MCP server is surface mcp: both are agents here, whatever their scopes.
var PersonSurfaces = []string{"cli", "pwa"}

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
