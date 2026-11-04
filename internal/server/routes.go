package server

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"reflect"
	"strings"
	"unicode"

	"github.com/danielgtaylor/huma/v2"

	"github.com/iodesystems/gwag/v2/gw/gat"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/netguard"
	"github.com/iodesystems/para/internal/ops"
)

const (
	metaScope = "scope"
	metaOp    = "op"
)

// routeTable is every huma route and its scope, for the gate and the guards.
type routeTable struct {
	byKey map[string]string
	byOp  map[string]string // op name to scope
	list  []Route
}

// Route is one registered route.
type Route struct {
	Op     string // ops name; "" for a route with no op
	ID     string // GraphQL field name, the operationId
	Method string
	Path   string
	Scope  string
}

func newRouteTable() *routeTable {
	return &routeTable{byKey: map[string]string{}, byOp: map[string]string{}}
}

func (t *routeTable) add(method, path, scope string) {
	t.byKey[method+" "+path] = scope
}

func (t *routeTable) scope(method, path string) (string, bool) {
	s, ok := t.byKey[method+" "+path]
	return s, ok
}

// Routes returns every huma route in registration order.
func (s *Server) Routes() []Route { return s.routes.list }

type registrar struct {
	api huma.API
	g   *gat.Gateway
	s   *Server
}

// noInput is the huma input of a POST op whose Input has no fields.
type noInput struct{}

type bodyInput[I any] struct {
	Body I
}

// output is the huma output of an op: Body is what `--json` prints. Failed
// carries ops.Failing: the Output still returns with 200, as the CLI prints it
// before it exits non-zero.
type output[O any] struct {
	Failed string `header:"X-Para-Failed"`
	Body   O
}

// reg registers one op. Every op states its method and scope here. GET reads
// the Input from query parameters and POST from the JSON body.
func reg[I, O any](r *registrar, op *ops.Op[I, O], method, scope string) {
	name := op.Name()
	route := Route{Op: name, ID: operationID(name), Method: method, Path: "/api/v1/" + strings.ReplaceAll(name, "_", "/"), Scope: scope}
	hop := huma.Operation{
		OperationID:  route.ID,
		Method:       method,
		Path:         route.Path,
		Summary:      op.Description(),
		MaxBodyBytes: maxBody,
		Metadata:     map[string]any{metaScope: scope, metaOp: name},
	}
	r.s.routes.add(method, route.Path, scope)
	r.s.routes.byOp[name] = scope
	r.s.routes.list = append(r.s.routes.list, route)
	s := r.s
	run := func(ctx context.Context, in I) (*output[O], error) {
		b, err := s.bound(ctx)
		if err != nil {
			return nil, err
		}
		out, err := op.Run(ctx, b.deps(), in)
		if err != nil {
			return nil, b.t.apiError(err)
		}
		res := &output[O]{Body: out}
		if f, ok := any(out).(ops.Failing); ok {
			if e := f.Failed(); e != nil {
				res.Failed = b.t.public(e)
			}
		}
		return res, nil
	}
	switch {
	case method == http.MethodGet:
		gat.Register(r.api, r.g, hop, func(ctx context.Context, in *I) (*output[O], error) { return run(ctx, *in) })
	case reflect.TypeFor[I]().NumField() == 0:
		gat.Register(r.api, r.g, hop, func(ctx context.Context, _ *noInput) (*output[O], error) {
			var in I
			return run(ctx, in)
		})
	default:
		gat.Register(r.api, r.g, hop, func(ctx context.Context, in *bodyInput[I]) (*output[O], error) { return run(ctx, in.Body) })
	}
}

// raw registers a route that is not an op: session and admin routes.
func raw[I, O any](r *registrar, id, method, path, scope, summary string, h func(context.Context, *I) (*O, error)) {
	r.s.routes.add(method, path, scope)
	r.s.routes.list = append(r.s.routes.list, Route{ID: id, Method: method, Path: path, Scope: scope})
	gat.Register(r.api, r.g, huma.Operation{
		OperationID: id,
		Method:      method,
		Path:        path,
		Summary:     summary,
		Metadata:    map[string]any{metaScope: scope},
	}, h)
}

// operationID is lowerCamel of the op name: task_show is taskShow.
func operationID(name string) string {
	parts := strings.Split(name, "_")
	for i, p := range parts[1:] {
		parts[i+1] = upperFirst(p)
	}
	return strings.Join(parts, "")
}

func upperFirst(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = unicode.ToUpper(r[0])
	return string(r)
}

// boundVault is a vault bound to the calling principal.
type boundVault struct {
	t       *Tenant
	req     netguard.Request
	surface string
	routes  *routeTable
}

// deps binds the caller. Authorize holds an approval to the caller's scopes:
// proposal_decide cannot cause a write the caller could not make.
func (b *boundVault) deps() ops.Deps {
	return ops.Deps{Store: b.t.store.As(b.req.Principal.Name, b.surface), Authorize: func(op string) error {
		scope, ok := b.routes.byOp[op]
		if !ok || !b.req.Principal.Has(scope) {
			return fmt.Errorf("%w: approving %s needs scope %s", ops.ErrPersonOnly, op, scope)
		}
		return nil
	}}
}

// apiError maps an op error to a status. Text reaches the client through the
// same filter as MCP. An I/O error is logged and hidden.
func (t *Tenant) apiError(err error) error {
	var lint *model.LintError
	var path *fs.PathError
	if s, ok := ops.AsStaged(err); ok {
		return stagedStatus{s}
	}
	switch {
	case errors.Is(err, ops.ErrPersonOnly):
		return huma.Error403Forbidden(t.public(err))
	case errors.Is(err, model.ErrNotFound):
		return huma.Error404NotFound(t.public(err))
	case errors.Is(err, model.ErrConflict), errors.Is(err, model.ErrExists):
		return huma.Error409Conflict(t.public(err))
	case errors.As(err, &lint):
		return huma.Error422UnprocessableEntity(t.public(err))
	case errors.As(err, &path), errors.Is(err, context.DeadlineExceeded):
		t.log.Error("op failed", "vault", t.id, "error", err)
		return huma.Error500InternalServerError("internal error")
	}
	return huma.Error400BadRequest(t.public(err))
}

func (t *Tenant) public(err error) string { return ops.PublicError(err, t.hide) }

// stagedStatus answers a write the gate held back: 202 Accepted, with the
// staged item as the body. The write did not run; it waits for the owner.
type stagedStatus struct{ *ops.Staged }

func (s stagedStatus) GetStatus() int { return http.StatusAccepted }
