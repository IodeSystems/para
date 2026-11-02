package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"

	"github.com/iodesystems/gwag/v2/gw/gat"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/netguard"
	"github.com/iodesystems/parasolve/internal/ops"
)

// TestNamingRule: the path and the GraphQL name come from the op name, so each
// op has one spelling on every surface. Non-op routes are listed here.
func TestNamingRule(t *testing.T) {
	nonOp := map[string]string{
		"authSessionGet":    "/auth/session",
		"authSessionCreate": "/auth/session",
		"authSessionDelete": "/auth/session/delete",
		"adminKeyList":      "/api/v1/admin/key/list",
		"adminKeyCreate":    "/api/v1/admin/key/create",
		"adminKeyRevoke":    "/api/v1/admin/key/revoke",
		"uiToday":           "/api/v1/ui/today",
		"uiReview":          "/api/v1/ui/review",
		"uiReviewAccept":    "/api/v1/ui/review/accept",
		"uiReviewReopen":    "/api/v1/ui/review/reopen",
		"uiDoc":             "/api/v1/ui/doc",
		"uiDocFields":       "/api/v1/ui/doc/fields",
		"uiLiveSubscribe":   "/api/v1/ui/live/subscribe",
	}
	camel := regexp.MustCompile(`^[a-z]+(?:[A-Z][a-z]+)*$`)
	s := schemaOnly(t)
	for _, r := range s.Routes() {
		if !camel.MatchString(r.ID) {
			t.Errorf("%s: operationId is not lowerCamel", r.ID)
		}
		if r.Op == "" {
			if nonOp[r.ID] != r.Path {
				t.Errorf("non-op route %s %s is not in the naming list", r.ID, r.Path)
			}
			continue
		}
		if want := "/api/v1/" + strings.ReplaceAll(r.Op, "_", "/"); r.Path != want {
			t.Errorf("%s: path %s, want %s", r.Op, r.Path, want)
		}
		if want := operationID(r.Op); r.ID != want {
			t.Errorf("%s: operationId %s, want %s", r.Op, r.ID, want)
		}
		if r.Method != get && r.Method != post {
			t.Errorf("%s: method %s: reads are GET, writes POST", r.Op, r.Method)
		}
	}
}

func schemaOnly(t *testing.T) *Server {
	t.Helper()
	s, err := New(Options{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestEveryRouteHasAScope reads huma's own table, so a route added with a bare
// gat.Register is caught even though it skipped reg.
func TestEveryRouteHasAScope(t *testing.T) {
	s := schemaOnly(t)
	valid := map[string]bool{netguard.ScopeAdmin: true, netguard.ScopeAnyKey: true}
	for _, v := range netguard.Scopes {
		valid[v] = true
	}
	n := 0
	for path, item := range s.api.OpenAPI().Paths {
		for _, op := range []*huma.Operation{item.Get, item.Post, item.Put, item.Patch, item.Delete, item.Head, item.Options} {
			if op == nil {
				continue
			}
			n++
			scope, _ := op.Metadata[metaScope].(string)
			if !valid[scope] {
				t.Errorf("%s %s: scope %q", op.Method, path, scope)
			}
			if got, ok := s.routes.scope(op.Method, path); !ok || got != scope {
				t.Errorf("%s %s: not in the route table", op.Method, path)
			}
		}
	}
	if n != len(s.Routes()) {
		t.Errorf("huma has %d routes, the table %d", n, len(s.Routes()))
	}
	if n < len(ops.All()) {
		t.Errorf("only %d routes for %d ops", n, len(ops.All()))
	}
}

// TestNewRouteWithNoScopeFails registers a route the way a mistake would: a
// bare gat.Register with no scope. It must be refused on REST and GraphQL.
func TestNewRouteWithNoScopeFails(t *testing.T) {
	s := &Server{log: schemaOnly(t).log, routes: newRouteTable(), mux: chi.NewRouter()}
	cfg := huma.DefaultConfig("t", "1")
	cfg.DocsPath, cfg.OpenAPIPath, cfg.SchemasPath = "", "", ""
	cfg.CreateHooks = nil
	s.api = humachi.New(s.mux, cfg)
	s.api.UseMiddleware(s.scopeMiddleware)
	g, err := gat.New()
	if err != nil {
		t.Fatal(err)
	}
	type out struct {
		Body struct {
			OK bool `json:"ok"`
		}
	}
	called := false
	gat.Register(s.api, g, huma.Operation{OperationID: "bare", Method: http.MethodGet, Path: "/api/v1/bare"},
		func(context.Context, *noInput) (*out, error) { called = true; return &out{}, nil })
	if err := gat.RegisterHuma(s.api, g, "/api"); err != nil {
		t.Fatal(err)
	}
	ctx := netguard.WithRequest(context.Background(), netguard.Request{Principal: netguard.Principal{Name: "admin", Admin: true}})

	r := httptest.NewRequest(get, "/api/v1/bare", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden || called {
		t.Fatalf("REST: %d called=%v %s", w.Code, called, w.Body)
	}
	r = httptest.NewRequest(post, "/api/graphql", strings.NewReader(`{"query":"{ t { v1 { bare { ok } } } }"}`)).WithContext(ctx)
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	s.mux.ServeHTTP(w, r)
	if called || !strings.Contains(w.Body.String(), "declares no scope") {
		t.Fatalf("GraphQL: called=%v %s", called, w.Body)
	}
}

// TestPointerOutputsAreNullable: a pointer with no omitempty is `required`, so
// GraphQL makes it non-null and a nil value fails the whole field.
func TestPointerOutputsAreNullable(t *testing.T) {
	seen := map[reflect.Type]bool{}
	var walk func(reflect.Type, string)
	walk = func(ty reflect.Type, at string) {
		for ty.Kind() == reflect.Pointer || ty.Kind() == reflect.Slice {
			ty = ty.Elem()
		}
		if ty.Kind() != reflect.Struct || seen[ty] || ty.PkgPath() == "time" {
			return
		}
		seen[ty] = true
		for i := range ty.NumField() {
			f := ty.Field(i)
			if !f.IsExported() {
				continue
			}
			tag := f.Tag.Get("json")
			if f.Type.Kind() == reflect.Pointer && !strings.Contains(tag, "omitempty") &&
				f.Tag.Get("nullable") != "true" && f.Tag.Get("required") != "false" {
				t.Errorf("%s.%s: pointer needs omitempty, nullable, or required:\"false\"", at, f.Name)
			}
			walk(f.Type, at+"."+f.Name)
		}
	}
	for _, e := range ops.All() {
		walk(e.OutputType(), e.Name())
	}
}

// TestGetInputsAreQueryParams: a GET Input field with no query tag is silently
// never bound, and required must match the CLI's required json fields.
func TestGetInputsAreQueryParams(t *testing.T) {
	byName := map[string]ops.Entry{}
	for _, e := range ops.All() {
		byName[e.Name()] = e
	}
	for _, r := range schemaOnly(t).Routes() {
		if r.Op == "" || r.Method != get {
			continue
		}
		ty := byName[r.Op].InputType()
		for i := range ty.NumField() {
			f := ty.Field(i)
			name, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
			if f.Tag.Get("query") != name {
				t.Errorf("%s.%s: query tag %q, want %q", r.Op, f.Name, f.Tag.Get("query"), name)
			}
			if want := !strings.Contains(opts, "omitempty"); want != (f.Tag.Get("required") == "true") {
				t.Errorf("%s.%s: required tag does not match json omitempty", r.Op, f.Name)
			}
		}
	}
}

// TestEnumsHaveValues: a named string type on an API struct is an enum or a
// listed free-text type. One values list serves schema and MCP.
func TestEnumsHaveValues(t *testing.T) {
	plain := map[reflect.Type]bool{
		reflect.TypeFor[model.Date](): true,
		reflect.TypeFor[model.Link](): true,
	}
	s := schemaOnly(t)
	_ = s
	seen := map[reflect.Type]bool{}
	var walk func(reflect.Type)
	walk = func(ty reflect.Type) {
		for ty.Kind() == reflect.Pointer || ty.Kind() == reflect.Slice {
			ty = ty.Elem()
		}
		if seen[ty] {
			return
		}
		seen[ty] = true
		switch ty.Kind() {
		case reflect.String:
			if _, ok := enumValues.Load(ty); ty.PkgPath() != "" && !ok && !plain[ty] {
				t.Errorf("%s has no enum values: add addEnum in schema.go, or list it as free text", ty)
			}
		case reflect.Struct:
			for i := range ty.NumField() {
				if ty.Field(i).IsExported() {
					walk(ty.Field(i).Type)
				}
			}
		}
	}
	for _, e := range ops.All() {
		walk(e.InputType())
		walk(e.OutputType())
	}
}

// TestMetaIsFlatUnion: the flat Meta holds every field of every document type,
// and a typed Meta field must keep its JSON type.
func TestMetaIsFlatUnion(t *testing.T) {
	flat := map[string]reflect.Type{}
	mt := reflect.TypeFor[Meta]()
	for i := range mt.NumField() {
		name, _, _ := strings.Cut(mt.Field(i).Tag.Get("json"), ",")
		flat[name] = mt.Field(i).Type
	}
	var walk func(reflect.Type, string)
	walk = func(ty reflect.Type, doc string) {
		for i := range ty.NumField() {
			f := ty.Field(i)
			if f.Anonymous {
				walk(f.Type, doc)
				continue
			}
			name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
			if name == "" || name == "-" {
				continue
			}
			got, ok := flat[name]
			if !ok {
				t.Errorf("%s.%s is not in Meta: add %q", doc, f.Name, name)
				continue
			}
			if got == reflect.TypeFor[*jsonValue]() {
				continue
			}
			if base(got) != base(f.Type) {
				t.Errorf("%s.%s: %s against Meta %s", doc, name, f.Type, got)
			}
		}
	}
	for _, ty := range model.Types {
		m, ok := model.NewMeta(ty)
		if !ok {
			t.Fatalf("no meta for %s", ty)
		}
		walk(reflect.TypeOf(m).Elem(), string(ty))
	}
}

// base is the JSON shape of a type: list of scalar, or the type name for a
// struct.
func base(ty reflect.Type) string {
	for ty.Kind() == reflect.Pointer {
		ty = ty.Elem()
	}
	switch ty.Kind() {
	case reflect.Slice:
		return "[]" + base(ty.Elem())
	case reflect.Struct:
		return ty.Name()
	}
	return ty.Kind().String()
}

// TestServerHoldsNoTransportCode: export stays render-only on the server. The
// op renders and logs; only the CLI runs a transport.
func TestServerHoldsNoTransportCode(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, bad := range []string{`"os/exec"`, "export.Send", "export.ClipboardCopy", `"net/smtp"`} {
			if strings.Contains(string(raw), bad) {
				t.Errorf("%s holds %s", f, bad)
			}
		}
	}
}

func TestExportOverAPIRendersAndMovesNothing(t *testing.T) {
	f := newFixture(t)
	tok := f.key(f.keyA, "k", netguard.ScopeVaultRead)
	target := filepath.Join(t.TempDir(), "out.txt")
	w := f.do(call{method: post, target: "/api/v1/export", token: tok, body: `{"id":"policy-sample","to":"file:` + target + `"}`})
	wantStatus(t, w, 200)
	if !strings.Contains(w.Body.String(), `"text"`) {
		t.Errorf("no rendered text: %s", w.Body)
	}
	if _, err := os.Stat(target); err == nil {
		t.Error("the server ran a transport")
	}
}

const proposalBody = `{"title":"From the key","source":"note-sample","items":[{"kind":"external","medium":"email","target":"sample@acme.example","draft":"Hi.","evidence":{"quote":"Sample text.","locator":"line 1"}}]}`

// A key with vault:read and proposal:add can suggest and change nothing.
func TestProposeOnlyKey(t *testing.T) {
	f := newFixture(t)
	tok := f.key(f.keyA, "proposer", netguard.ScopeVaultRead, netguard.ScopeProposalAdd)
	wantStatus(t, f.do(call{method: post, target: "/api/v1/proposal/add", token: tok, body: proposalBody}), 200)
	wantStatus(t, f.do(call{target: "/api/v1/proposal/list", token: tok}), 200)
	wantStatus(t, f.do(call{method: post, target: "/api/v1/task/add", token: tok, body: `{"project":"project-sample","title":"Direct"}`}), 403)
	wantStatus(t, f.do(call{method: post, target: "/api/v1/inbox/add", token: tok, body: `{"text":"Direct"}`}), 403)
	for _, r := range f.srv.Routes() {
		if r.Scope == netguard.ScopeProposalReview {
			wantStatus(t, f.do(call{method: r.Method, target: r.Path, token: tok, body: sample(r.Op)}), 403)
		}
	}
	// proposal:add alone reads nothing: the agent's key also needs vault:read.
	only := f.key(f.keyA, "bare", netguard.ScopeProposalAdd)
	wantStatus(t, f.do(call{target: "/api/v1/proposal/list", token: only}), 403)
}

// TestReviewNeedsASession: a bearer key is an agent, whatever its scopes. The op
// refuses it, on REST and on GraphQL, and nothing changes.
func TestReviewNeedsASession(t *testing.T) {
	f := newFixture(t)
	tok := f.key(f.keyA, "reviewer", allScopes...)
	before := f.do(call{target: "/api/v1/proposal/show?id=proposal-sample", token: tok}).Body.String()
	for _, r := range f.srv.Routes() {
		if r.Scope != netguard.ScopeProposalReview {
			continue
		}
		w := f.do(call{method: post, target: r.Path, token: tok, body: sample(r.Op)})
		wantStatus(t, w, 403)
		if !strings.Contains(w.Body.String(), "only a person") {
			t.Errorf("%s: %s", r.Op, w.Body)
		}
	}
	types := f.gqlTypes(tok)
	fields := fieldsOf(types)
	got := fields["proposalApprove"]
	doc, vars := gqlDoc(types, got.Kind, got.Field, sample("proposal_approve"), t)
	_, res := f.graphql(tok, doc, vars)
	if res["errors"] == nil || !strings.Contains(fmt.Sprint(res["errors"]), "only a person") {
		t.Fatalf("GraphQL approve with a bearer key: %v", res)
	}
	if after := f.do(call{target: "/api/v1/proposal/show?id=proposal-sample", token: tok}).Body.String(); after != before {
		t.Error("a refused review changed the proposal")
	}
}

func session(f *fixture, name string, scopes ...string) map[string]string {
	f.t.Helper()
	return map[string]string{"Cookie": netguard.CookieName + "=" + f.session(f.key(f.keyA, name, scopes...)), "Origin": "http://" + hostA}
}

// TestApprovalKeepsTheApproversScopes: proposal:review alone cannot cause a
// write the session could not make, though an external item needs no more.
func TestApprovalKeepsTheApproversScopes(t *testing.T) {
	f := newFixture(t)
	h := session(f, "narrow", netguard.ScopeVaultRead, netguard.ScopeProposalReview)
	w := f.do(call{method: post, target: "/api/v1/proposal/approve", header: h, body: `{"id":"proposal-sample","items":["i1"]}`})
	wantStatus(t, w, 403)
	if !strings.Contains(w.Body.String(), netguard.ScopeVaultWrite) {
		t.Errorf("no scope named: %s", w.Body)
	}
	show := f.do(call{target: "/api/v1/proposal/show?id=proposal-sample", header: h})
	wantStatus(t, show, 200)
	if strings.Contains(show.Body.String(), `"state": "running"`) || strings.Contains(show.Body.String(), `"state": "approved"`) {
		t.Fatalf("a refused approval changed the item: %s", show.Body)
	}
	wantStatus(t, f.do(call{method: post, target: "/api/v1/proposal/approve", header: h, body: `{"id":"proposal-sample","items":["i2"]}`}), 200)

	wide := session(f, "wide", netguard.ScopeVaultRead, netguard.ScopeVaultWrite, netguard.ScopeProposalReview)
	wantStatus(t, f.do(call{method: post, target: "/api/v1/proposal/approve", header: wide, body: `{"id":"proposal-sample","items":["i1"]}`}), 200)
	// The approved op ran under the proposing agent, not the approver.
	st := f.store(t)
	evs, err := st.Events(contextBackground(), 50)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range evs {
		found = found || (e.Op == "create" && e.Actor == "claude-agent" && e.Surface == "proposal")
	}
	if !found {
		t.Errorf("no create by the proposer: %+v", evs)
	}
}

// TestSessionCannotReviewItsOwnProposal: an agent key that also holds
// proposal:review, turned into a cookie, is still the actor that filed it.
func TestSessionCannotReviewItsOwnProposal(t *testing.T) {
	f := newFixture(t)
	tok := f.key(f.keyA, "both", netguard.ScopeVaultRead, netguard.ScopeProposalAdd, netguard.ScopeProposalReview)
	w := f.do(call{method: post, target: "/api/v1/proposal/add", token: tok, body: proposalBody})
	wantStatus(t, w, 200)
	var out ops.ProposalFiled
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	h := map[string]string{"Cookie": netguard.CookieName + "=" + f.session(tok), "Origin": "http://" + hostA}
	wantStatus(t, f.do(call{method: post, target: "/api/v1/proposal/approve", header: h, body: `{"id":"` + out.ID + `","items":["i1"]}`}), 403)
}
