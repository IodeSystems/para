package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/netguard"
	"github.com/iodesystems/para/internal/ops"
	"github.com/iodesystems/para/internal/opsample"
	"github.com/iodesystems/para/internal/store/local"
)

func contextBackground() context.Context { return context.Background() }

func (f *fixture) store(t *testing.T) *local.Store {
	t.Helper()
	tn, ok := f.hub.Lookup("acme")
	if !ok {
		t.Fatal("no acme")
	}
	return tn.(*Tenant).store.As("key", surfaceAPI)
}

func encodeJSON(t *testing.T, v any) string {
	t.Helper()
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// sample returns the op's sample Input.
func sample(name string) string {
	if s, ok := opsample.Inputs[name]; ok {
		return s
	}
	return "{}"
}

// restTarget turns a sample Input into a REST request.
func restTarget(t *testing.T, r Route, input string) call {
	t.Helper()
	if r.Method == post {
		return call{method: post, target: r.Path, body: input}
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(input), &m); err != nil {
		t.Fatal(err)
	}
	q := url.Values{}
	for k, v := range m {
		q.Set(k, fmt.Sprint(v))
	}
	target := r.Path
	if len(q) > 0 {
		target += "?" + q.Encode()
	}
	return call{method: get, target: target}
}

// sampleToken makes a key with every scope. The sample requests are claimed by
// an actor named agent.
func (f *fixture) sampleToken() string { return f.key(f.keyA, "agent", allScopes...) }

// sign sends c as the caller an op needs: a person-only op refuses a bearer key,
// so it goes through a browser session of the same key.
func (f *fixture) sign(c *call, e ops.Entry, tok string) {
	f.t.Helper()
	if !e.PersonOnly() && e.Risk() != ops.RiskReview {
		c.token = tok
		return
	}
	c.header = map[string]string{"Cookie": netguard.CookieName + "=" + f.session(tok), "Origin": "http://" + hostA}
}

// TestRESTEqualsInvoke: for every op the REST body is the canonical --json form
// of the Output type, and a read gives the exact bytes of Invoke, which
// TestInvokeMatchesCLIJSON ties to the CLI.
func TestRESTEqualsInvoke(t *testing.T) {
	f0 := newFixture(t)
	for _, r := range f0.srv.Routes() {
		if r.Op == "" {
			continue
		}
		e := entry(t, r.Op)
		t.Run(r.Op, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			c := restTarget(t, r, sample(r.Op))
			f.sign(&c, e, f.sampleToken())
			w := f.do(c)
			wantStatus(t, w, 200)
			text := w.Body.String()
			ptr := reflect.New(e.OutputType())
			dec := json.NewDecoder(strings.NewReader(text))
			dec.DisallowUnknownFields()
			if err := dec.Decode(ptr.Interface()); err != nil {
				t.Fatalf("not the Output type: %v\n%s", err, text)
			}
			if again := encodeJSON(t, ptr.Elem().Interface()); again != text {
				t.Errorf("not the --json form:\n got: %s\nwant: %s", text, again)
			}
			if r.Method != get {
				return
			}
			out, err := e.Invoke(contextBackground(), ops.Deps{Store: f.store(t)}, json.RawMessage(sample(r.Op)))
			if err != nil {
				t.Fatal(err)
			}
			if want := encodeJSON(t, out); want != text {
				t.Errorf("differs from Invoke:\n got: %s\nwant: %s", text, want)
			}
			if fl, ok := out.(ops.Failing); ok && fl.Failed() != nil && w.Header().Get("X-Para-Failed") == "" {
				t.Error("a failed Output must set X-Para-Failed")
			}
		})
	}
}

func entry(t *testing.T, name string) ops.Entry {
	t.Helper()
	e, ok := ops.Lookup(name)
	if !ok {
		t.Fatalf("no op %s", name)
	}
	return e
}

// session exchanges a bearer key for a cookie value.
func (f *fixture) session(tok string) string {
	f.t.Helper()
	w := f.do(call{method: post, target: "/auth/session", token: tok})
	wantStatus(f.t, w, 200)
	for _, c := range w.Result().Cookies() {
		if c.Name == netguard.CookieName {
			return c.Value
		}
	}
	f.t.Fatalf("no cookie: %v", w.Result().Header)
	return ""
}

// introspection below drives GraphQL from the schema itself, so a new op is
// exercised with no change here.

type typeRef struct {
	Kind   string   `json:"kind"`
	Name   string   `json:"name"`
	OfType *typeRef `json:"ofType"`
}

type gqlField struct {
	Name string `json:"name"`
	Args []struct {
		Name string  `json:"name"`
		Type typeRef `json:"type"`
	} `json:"args"`
	Type typeRef `json:"type"`
}

type gqlType struct {
	Kind   string     `json:"kind"`
	Name   string     `json:"name"`
	Fields []gqlField `json:"fields"`
}

func (r typeRef) leaf() typeRef {
	for r.OfType != nil {
		r = *r.OfType
	}
	return r
}

func (r typeRef) String() string {
	switch r.Kind {
	case "NON_NULL":
		return r.OfType.String() + "!"
	case "LIST":
		return "[" + r.OfType.String() + "]"
	}
	return r.Name
}

func (f *fixture) gqlTypes(token string) map[string]gqlType {
	f.t.Helper()
	w := f.do(call{target: "/api/schema/graphql?format=json", token: token})
	wantStatus(f.t, w, 200)
	var res struct {
		Data struct {
			Schema struct {
				Types []gqlType `json:"types"`
			} `json:"__schema"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		f.t.Fatal(err)
	}
	out := map[string]gqlType{}
	for _, ty := range res.Data.Schema.Types {
		out[ty.Name] = ty
	}
	return out
}

func selection(types map[string]gqlType, r typeRef, depth int) string {
	leaf := r.leaf()
	if leaf.Kind != "OBJECT" {
		return ""
	}
	if depth > 6 {
		return "{ __typename }"
	}
	var b strings.Builder
	b.WriteString("{ ")
	for _, fd := range types[leaf.Name].Fields {
		b.WriteString(fd.Name + " " + selection(types, fd.Type, depth+1) + " ")
	}
	b.WriteString("}")
	return b.String()
}

// fieldsOf returns the op fields of the query and mutation namespaces.
func fieldsOf(types map[string]gqlType) map[string]struct {
	Field gqlField
	Kind  string
} {
	out := map[string]struct {
		Field gqlField
		Kind  string
	}{}
	for _, fd := range types["ParaV1QueryNamespace"].Fields {
		out[fd.Name] = struct {
			Field gqlField
			Kind  string
		}{fd, "query"}
	}
	for _, fd := range types["ParaV1MutationNamespace"].Fields {
		out[fd.Name] = struct {
			Field gqlField
			Kind  string
		}{fd, "mutation"}
	}
	return out
}

func gqlDoc(types map[string]gqlType, kind string, fd gqlField, input string, t *testing.T) (string, map[string]any) {
	t.Helper()
	vars := map[string]any{}
	var decl, args []string
	var in map[string]any
	dec := json.NewDecoder(strings.NewReader(input))
	dec.UseNumber()
	if err := dec.Decode(&in); err != nil {
		t.Fatal(err)
	}
	for _, a := range fd.Args {
		if a.Name == "body" {
			vars["body"] = camelInput(in)
		} else if v, ok := in[a.Name]; ok {
			vars[a.Name] = v
		} else {
			continue
		}
		decl = append(decl, "$"+a.Name+": "+a.Type.String())
		args = append(args, a.Name+": $"+a.Name)
	}
	head, call := kind, fd.Name
	if len(decl) > 0 {
		head += "(" + strings.Join(decl, ", ") + ")"
		call += "(" + strings.Join(args, ", ") + ")"
	}
	return fmt.Sprintf("%s { para { v1 { %s %s } } }", head, call, selection(types, fd.Type, 0)), vars
}

// same reports whether a GraphQL value equals a REST value. Absent, null, and
// [] are one value: a client of either surface reads them as nothing.
func same(g, r any) bool {
	if empty(g) && empty(r) {
		return true
	}
	switch gv := g.(type) {
	case map[string]any:
		rv, ok := r.(map[string]any)
		if !ok {
			return false
		}
		for k, v := range gv {
			if !same(v, camelKeys(rv)[k]) {
				return false
			}
		}
		return true
	case []any:
		rv, ok := r.([]any)
		if !ok || len(gv) != len(rv) {
			return false
		}
		for i := range gv {
			if !same(gv[i], rv[i]) {
				return false
			}
		}
		return true
	}
	return fmt.Sprint(g) == fmt.Sprint(r)
}

// camelKeys maps REST field names to GraphQL names: gat camelCases them.
func camelKeys(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v // a JSON scalar keeps its keys as written
	}
	for k, v := range m {
		out[operationID(k)] = v
	}
	return out
}

// camelInput renames the keys of an input for GraphQL, at every level.
func camelInput(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			if k == "input" { // a JSON scalar: its keys pass through as written
				out[k] = e
				continue
			}
			out[operationID(k)] = camelInput(e)
		}
		return out
	case []any:
		for i := range x {
			x[i] = camelInput(x[i])
		}
	}
	return v
}

func empty(v any) bool {
	if v == nil {
		return true
	}
	if l, ok := v.([]any); ok {
		return len(l) == 0
	}
	return v == ""
}

// TestGraphQLEveryOp runs every op over GraphQL, with a selection of every
// field. REST and GraphQL bind input separately and encode output separately, so
// a passing REST test proves nothing here. GET must be a query and POST a
// mutation, and reads must equal REST.
func TestGraphQLEveryOp(t *testing.T) {
	f0 := newFixture(t)
	tok := f0.sampleToken()
	types := f0.gqlTypes(tok)
	fields := fieldsOf(types)
	for _, r := range f0.srv.Routes() {
		if r.Op == "" {
			continue
		}
		t.Run(r.Op, func(t *testing.T) {
			t.Parallel()
			got, ok := fields[r.ID]
			if !ok {
				t.Fatalf("no GraphQL field %s", r.ID)
			}
			if (r.Method == get) != (got.Kind == "query") {
				t.Fatalf("%s %s is a %s", r.Method, r.Path, got.Kind)
			}
			f := newFixture(t)
			key := f.sampleToken()
			doc, vars := gqlDoc(types, got.Kind, got.Field, sample(r.Op), t)
			c := call{}
			f.sign(&c, entry(t, r.Op), key)
			w, res := f.graphqlAs(c, doc, vars)
			wantStatus(t, w, 200)
			if res["errors"] != nil {
				t.Fatalf("errors: %v\n%s\nvars %v", res["errors"], doc, vars)
			}
			if r.Method != get {
				return
			}
			// ops_stats counts its own call: the REST call after this one sees
			// one more, so its numbers cannot match. The GraphQL call above
			// still proves the field resolves.
			if r.Op == "ops_stats" {
				return
			}
			data := res["data"].(map[string]any)["para"].(map[string]any)["v1"].(map[string]any)[r.ID]
			c = restTarget(t, r, sample(r.Op))
			c.token = key
			rest := f.do(c)
			wantStatus(t, rest, 200)
			var want any
			dec := json.NewDecoder(bytes.NewReader(rest.Body.Bytes()))
			dec.UseNumber()
			if err := dec.Decode(&want); err != nil {
				t.Fatal(err)
			}
			if !same(data, want) {
				t.Errorf("GraphQL differs from REST:\n got: %v\nwant: %v", data, want)
			}
		})
	}
}

// TestEveryOpIsRegistered: an op with no route fails here.
func TestEveryOpIsRegistered(t *testing.T) {
	// ops that are deliberately not served. Keep it short and give the reason.
	excluded := map[string]string{
		"proposal_approve": "a person decides at a terminal or in the chat; the web app only reads proposals",
		"proposal_deny":    "a person decides at a terminal or in the chat; the web app only reads proposals",
		"proposal_open":    "opens a browser on the machine para runs on: CLI and MCP only",
	}
	f := newFixture(t)
	seen := map[string]bool{}
	for _, r := range f.srv.Routes() {
		seen[r.Op] = true
	}
	for _, e := range ops.All() {
		if _, ok := excluded[e.Name()]; !ok && !seen[e.Name()] {
			t.Errorf("op %s has no route: add reg(...) in routes_ops.go or name it in excluded", e.Name())
		}
	}
	for name := range excluded {
		if seen[name] {
			t.Errorf("excluded op %s has a route", name)
		}
	}
}

func TestSDLUsesNoLong(t *testing.T) {
	f := newFixture(t)
	if strings.Contains(f.srv.SDL(), "Long") {
		t.Error("SDL has Long: counts are int32, and para has no integer ids")
	}
	if !strings.Contains(f.srv.SDL(), "taskShow(id: String!)") {
		t.Error("SDL lost the flat field taskShow(id: String!)")
	}
}
