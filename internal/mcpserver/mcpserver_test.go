package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/iodesystems/parasolve/docs"
	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/ops"
	"github.com/iodesystems/parasolve/internal/store"
	"github.com/iodesystems/parasolve/internal/store/local"
	"github.com/iodesystems/parasolve/internal/vault"
)

// sampleInputs holds a valid Input for each op that rejects the empty Input.
// An op added by another stream is called with {}; if that fails,
// TestParity fails and names the op: add its entry here.
var sampleInputs = map[string]string{
	"search":                    `{"query":"sample"}`,
	"constraint_add":            `{"says":"No prod access","why":"Audit scope","by":"Jordan Example","source":"https://wiki.acme.example/access","review_by":"2027-01-01"}`,
	"constraint_show":           `{"id":"constraint-sample"}`,
	"convention_add":            `{"title":"Branch names"}`,
	"convention_show":           `{"id":"convention-sample"}`,
	"copy":                      `{"id":"policy-sample","as":"slack"}`,
	"exception_add":             `{"policy":"policy-sample","kind":"bug","by":"Jordan Example"}`,
	"exception_show":            `{"id":"exception-sample"}`,
	"export":                    `{"id":"policy-sample","as":"slack","to":"ssh:desk.example:/tmp/sample.txt"}`,
	"inbox_add":                 `{"text":"buy milk\nand eggs"}`,
	"ingest":                    `{"url":"https://wiki.acme.example/policies/sample","text":"Sample policy text."}`,
	"inbox_show":                `{"id":"inbox-sample"}`,
	"inbox_triage":              `{"id":"inbox-sample","to":"task","project":"project-sample"}`,
	"interaction_add":           `{"summary":"Agreed to review on Friday","with":["person-sample"],"medium":"slack","direction":"in","date":"2026-11-01","url":"https://acme.example/m/1"}`,
	"interaction_show":          `{"id":"interaction-sample"}`,
	"org_domain_add":            `{"name":"Billing","owner":"role-sample"}`,
	"org_domain_show":           `{"id":"domain-sample"}`,
	"org_group_add":             `{"name":"Atlas","org":"acme","kind":"team"}`,
	"org_group_show":            `{"id":"group-sample"}`,
	"org_person_add":            `{"name":"Riley Example","org":"acme","aliases":["Ri"]}`,
	"org_person_alias_add":      `{"id":"person-sample","alias":"Ri"}`,
	"org_person_alias_remove":   `{"id":"person-sample","alias":"Sam"}`,
	"org_person_contact_add":    `{"id":"person-sample","method":"slack","value":"@sample","preferred":true}`,
	"org_person_contact_remove": `{"id":"person-sample","method":"email","value":"sample@acme.example"}`,
	"org_person_group_add":      `{"id":"person-sample","group":"group-sample"}`,
	"org_person_group_remove":   `{"id":"person-sample","group":"group-sample"}`,
	"org_person_note":           `{"id":"person-sample","text":"Prefers email for records.","date":"2026-11-01"}`,
	"org_person_show":           `{"id":"person-sample"}`,
	"org_role_add":              `{"name":"Auditor","org":"acme"}`,
	"org_role_show":             `{"id":"role-sample"}`,
	"org_team_add":              `{"name":"Platform","org":"acme"}`,
	"org_team_show":             `{"id":"team-sample"}`,
	"policy_refresh":            `{"id":"policy-sample","text":"Sample policy text."}`,
	"policy_review":             `{"id":"policy-sample","by":"Jordan Example"}`,
	"policy_show":               `{"id":"policy-sample"}`,
	"project_add":               `{"title":"Portal","owner":"Jordan Example"}`,
	"project_close":             `{"id":"project-sample","force":true}`,
	"project_show":              `{"id":"project-sample"}`,
	"rfx_add":                   `{"direction":"out","ask":"Can feature X launch?","route":"domain-sample","self_serve":"policy read"}`,
	"rfx_close":                 `{"id":"rfx-sample"}`,
	"rfx_draft":                 `{"id":"rfx-sample","text":"Hi, can we launch X?"}`,
	"rfx_reply":                 `{"id":"rfx-sample","from":"person-sample","outcome":"no","text":"Not now.","why":"Audit freeze","url":"https://acme.example/m/9"}`,
	"rfx_show":                  `{"id":"rfx-sample"}`,
	"rfx_status":                `{"id":"rfx-sample","status":"answered"}`,
	"rfx_target_add":            `{"id":"rfx-sample","to":"person-sample","medium":"slack"}`,
	"rfx_target_state":          `{"id":"rfx-sample","to":"person-sample","state":"sent","date":"2026-11-01"}`,
	"request_add":               `{"ask":"Summarize the sample project","links":["project-sample"]}`,
	"request_cancel":            `{"id":"request-sample"}`,
	"request_done":              `{"id":"request-sample","result":"Summary written.","links":["project-sample"]}`,
	"request_fail":              `{"id":"request-sample","reason":"No access."}`,
	"request_note":              `{"id":"request-sample","text":"Read the project."}`,
	"request_show":              `{"id":"request-sample"}`,
	"route":                     `{"query":"domain-sample"}`,
	"rule_add":                  `{"policy":"policy-sample","text":"Services log every failure."}`,
	"task_add":                  `{"project":"project-sample","title":"Write parser"}`,
	"task_block":                `{"id":"task-sample","reason":"waiting on legal"}`,
	"task_done":                 `{"id":"task-sample"}`,
	"task_show":                 `{"id":"task-sample"}`,
	"task_start":                `{"id":"task-sample"}`,
	"who":                       `{"query":"Sample person"}`,
}

func sampleSource() model.Source {
	return model.Source{URL: "https://wiki.acme.example/policies/sample", Retrieved: "2026-11-01T09:00:00Z",
		Hash: "sha256:" + strings.Repeat("ab", 32), Fidelity: model.FidelityExact}
}

// seedSamples creates the documents that sampleInputs name.
func seedSamples(t *testing.T, s store.Store) {
	t.Helper()
	for _, d := range []model.Document{
		{ID: "project-sample", Folder: model.FolderProjects, Title: "Sample project",
			Meta: &model.Project{Common: model.Common{Type: model.TypeProject}, Status: model.ProjectActive, Owner: "Jordan Example"}},
		{ID: "task-sample", Folder: model.FolderProjects, Title: "Sample task",
			Meta: &model.Task{Common: model.Common{Type: model.TypeTask}, Status: model.TaskTodo, Project: model.LinkTo("project-sample")}},
		{ID: "inbox-sample", Folder: model.FolderInbox, Title: "Sample capture",
			Meta: &model.Inbox{Common: model.Common{Type: model.TypeInbox}}},
		{ID: "role-sample", Folder: model.FolderOrg, Title: "Sample role",
			Meta: &model.Role{Common: model.Common{Type: model.TypeRole}, Org: "acme"}},
		{ID: "team-sample", Folder: model.FolderOrg, Title: "Sample team",
			Meta: &model.Team{Common: model.Common{Type: model.TypeTeam}, Org: "acme"}},
		{ID: "person-sample", Folder: model.FolderOrg, Title: "Sample person",
			Meta: &model.Person{Common: model.Common{Type: model.TypePerson}, Org: "acme", Aliases: []string{"Sam"},
				Contacts: []model.Contact{{Method: model.MediumEmail, Value: "sample@acme.example", Preferred: true}},
				Groups:   []model.Link{model.LinkTo("group-sample")}}},
		{ID: "group-sample", Folder: model.FolderOrg, Title: "Sample group",
			Meta: &model.Group{Common: model.Common{Type: model.TypeGroup}, Org: "acme", Kind: model.GroupTeam}},
		{ID: "interaction-sample", Folder: model.FolderInteractions, Title: "Sample interaction",
			Meta: &model.Interaction{Common: model.Common{Type: model.TypeInteraction}, Date: "2026-11-01", With: []model.Link{model.LinkTo("person-sample")},
				Medium: model.MediumSlack, Direction: model.InteractionIn, Visibility: model.VisibilityPrivate}},
		{ID: "request-sample", Folder: model.FolderRequests, Title: "Sample request",
			Meta: &model.Request{Common: model.Common{Type: model.TypeRequest}, Status: model.RequestClaimed, Runner: "install-test",
				Queued: "2026-11-01T09:00:00Z", Claimed: "agent", Lease: "2099-01-01T00:00:00Z"}},
		{ID: "request-queued", Folder: model.FolderRequests, Title: "Queued sample request",
			Meta: &model.Request{Common: model.Common{Type: model.TypeRequest}, Status: model.RequestQueued, Runner: "install-test",
				Queued: "2026-11-01T09:00:00Z"}},
		{ID: "domain-sample", Folder: model.FolderOrg, Title: "Sample domain",
			Meta: &model.Domain{Common: model.Common{Type: model.TypeDomain}, Owner: model.LinkTo("role-sample")}},
		{ID: "rfx-sample", Folder: model.FolderRFX, Title: "Sample request",
			Meta: &model.RFX{Common: model.Common{Type: model.TypeRFX}, Direction: model.RFXOut, Status: model.RFXOpen, Route: "domain-sample", SelfServeCheck: "policy read",
				Targets: []model.Target{{To: model.LinkTo("person-sample"), Medium: model.MediumEmail, Value: "sample@acme.example", State: model.TargetDrafted}}}},
		{ID: "policy-sample", Folder: model.FolderPolicies, Title: "Sample policy",
			Meta: &model.Policy{Common: model.Common{Type: model.TypePolicy}, Source: sampleSource()}},
		{ID: "policy-sample.source", Folder: model.FolderPolicies, Title: "Sample policy (transcript)",
			Body: "Source: https://wiki.acme.example/policies/sample\n\nSample policy text.",
			Meta: &model.Transcript{Common: model.Common{Type: model.TypeTranscript}, Of: model.LinkTo("policy-sample"), Source: sampleSource()}},
		{ID: "exception-sample", Folder: model.FolderPolicies, Title: "Sample exception",
			Meta: &model.PolicyException{Common: model.Common{Type: model.TypePolicyException}, Policy: model.LinkTo("policy-sample"),
				Kind: model.ExceptionBug, Status: model.ExceptionOpen, By: "Jordan Example"}},
		{ID: "constraint-sample", Folder: model.FolderConstraints, Title: "Sample no",
			Meta: &model.Constraint{Common: model.Common{Type: model.TypeConstraint}, Why: "w", By: "Jordan Example",
				Source: model.Source{URL: "https://wiki.acme.example/a", Retrieved: "2026-11-01", Fidelity: model.FidelityNone}, ReviewBy: "2027-01-01"}},
		{ID: "convention-sample", Folder: model.FolderConventions, Title: "Sample convention",
			Meta: &model.Convention{Common: model.Common{Type: model.TypeConvention}, Status: model.ConventionDraft}},
	} {
		if _, err := s.Create(t.Context(), d); err != nil {
			t.Fatal(err)
		}
	}
}

func newVault(t *testing.T) (store.Store, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "vault")
	templates, err := fs.Sub(docs.Templates, "templates")
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Init(root, "test", templates, vault.InitOptions{}); err != nil {
		t.Fatal(err)
	}
	s, err := local.Open(root, vault.Options{ID: "test", Actor: "agent", Surface: "mcp", Install: "install-test", StateDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return s, root
}

// seededTemplate builds one vault with seedSamples applied. Each parity case
// copies it: building a vault per op costs a few seconds of fsync, a copy
// costs milliseconds.
func seededTemplate(t *testing.T) string {
	t.Helper()
	s, root := newVault(t)
	seedSamples(t, s)
	return root
}

// vaultFromTemplate copies a template vault and opens the copy.
func vaultFromTemplate(t *testing.T, template string) store.Store {
	t.Helper()
	root := filepath.Join(t.TempDir(), "vault")
	err := filepath.WalkDir(template, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(template, path)
		if err != nil {
			return err
		}
		dst := filepath.Join(root, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(dst, info.Mode().Perm())
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, b, info.Mode().Perm())
	})
	if err != nil {
		t.Fatal(err)
	}
	s, err := local.Open(root, vault.Options{ID: "test", Actor: "agent", Surface: "mcp", Install: "install-test", StateDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func connect(t *testing.T, deps ops.Deps, hide ...string) *client.Client {
	t.Helper()
	srv, err := New(deps, Options{Version: "test", Hide: hide})
	if err != nil {
		t.Fatal(err)
	}
	c, err := client.NewInProcessClient(srv.MCP())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	ctx := context.Background()
	if err := c.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Initialize(ctx, mcp.InitializeRequest{}); err != nil {
		t.Fatal(err)
	}
	return c
}

func call(t *testing.T, c *client.Client, name, args string) *mcp.CallToolResult {
	t.Helper()
	var a any
	if args != "" {
		if err := json.Unmarshal([]byte(args), &a); err != nil {
			t.Fatal(err)
		}
	}
	res, err := c.CallTool(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Name: name, Arguments: a}})
	if err != nil {
		t.Fatalf("%s: protocol error: %v", name, err)
	}
	return res
}

func textOf(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if len(res.Content) != 1 {
		t.Fatalf("content parts = %d, want 1", len(res.Content))
	}
	tc, ok := res.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("content is %T, want text", res.Content[0])
	}
	return tc.Text
}

func TestToolsMatchOps(t *testing.T) {
	s, _ := newVault(t)
	c := connect(t, ops.Deps{Store: s})
	list, err := c.ListTools(context.Background(), mcp.ListToolsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]mcp.Tool{}
	for _, tl := range list.Tools {
		got[tl.Name] = tl
	}
	all := ops.All()
	if len(got) != len(all) {
		t.Errorf("tools = %d, ops = %d", len(got), len(all))
	}
	for _, e := range all {
		tl, ok := got[e.Name()]
		if !ok {
			t.Errorf("op %s has no tool", e.Name())
			continue
		}
		if tl.Description != e.Description() {
			t.Errorf("%s: description %q, want %q", e.Name(), tl.Description, e.Description())
		}
		raw, err := json.Marshal(tl.InputSchema)
		if err != nil {
			t.Fatal(err)
		}
		var sc map[string]any
		if err := json.Unmarshal(raw, &sc); err != nil {
			t.Fatal(err)
		}
		if sc["type"] != "object" {
			t.Errorf("%s: schema type = %v", e.Name(), sc["type"])
		}
		props, _ := sc["properties"].(map[string]any)
		for _, bad := range []string{"vault", "home", "path"} {
			if _, ok := props[bad]; ok {
				t.Errorf("%s: input has %q: a tool must not reach another vault or path", e.Name(), bad)
			}
		}
	}
	// Admin commands are not ops.
	for _, admin := range []string{"init", "vault", "mcp", "version", "vault_list", "vault_add"} {
		if _, ok := got[admin]; ok {
			t.Errorf("admin command %q is exposed as a tool", admin)
		}
	}
}

func TestInputSchema(t *testing.T) {
	type nested struct {
		N int `json:"n"`
	}
	type embedded struct {
		E string `json:"e"`
	}
	type in struct {
		embedded
		Name    string            `json:"name"`
		Folder  string            `json:"folder,omitempty"`
		Count   *int              `json:"count,omitempty"`
		Tags    []string          `json:"tags"`
		Meta    map[string]string `json:"meta,omitempty"`
		Nest    nested            `json:"nest,omitempty"`
		When    time.Time         `json:"when,omitempty"`
		Skipped string            `json:"-"`
		hidden  string
	}
	raw, err := inputSchema(reflect.TypeFor[in]())
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	props := got["properties"].(map[string]any)
	var names []string
	for k := range props {
		names = append(names, k)
	}
	for _, want := range []string{"e", "name", "folder", "count", "tags", "meta", "nest", "when"} {
		if _, ok := props[want]; !ok {
			t.Errorf("missing property %s (have %v)", want, names)
		}
	}
	if len(props) != 8 {
		t.Errorf("properties = %v", names)
	}
	if got["additionalProperties"] != false {
		t.Errorf("additionalProperties = %v", got["additionalProperties"])
	}
	req := map[string]bool{}
	for _, r := range got["required"].([]any) {
		req[r.(string)] = true
	}
	for _, c := range []struct {
		field string
		want  bool
	}{{"e", true}, {"name", true}, {"tags", true}, {"folder", false}, {"count", false}, {"when", false}} {
		if req[c.field] != c.want {
			t.Errorf("required[%s] = %v, want %v", c.field, req[c.field], c.want)
		}
	}
	if p := props["count"].(map[string]any); p["type"] != "integer" {
		t.Errorf("count = %v", p)
	}
	if p := props["tags"].(map[string]any); p["type"] != "array" {
		t.Errorf("tags = %v", p)
	}
	if p := props["when"].(map[string]any); p["format"] != "date-time" {
		t.Errorf("when = %v", p)
	}
	_ = in{}.hidden
	_ = in{}.Skipped
}

type colour string

func (colour) Enum() []string { return []string{"red", "blue"} }

func TestEnums(t *testing.T) {
	type in struct {
		Folder colour `json:"colour"`
		Status []colour
	}
	raw, err := inputSchema(reflect.TypeFor[in]())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(raw, []byte(`"enum":["red","blue"]`)) {
		t.Errorf("schema lacks Enumer values: %s", raw)
	}
	for typ, want := range enums {
		if len(want) == 0 {
			t.Errorf("%s: empty enum", typ)
		}
		s, err := typeSchema(typ, 0)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(s["enum"], want) {
			t.Errorf("%s: enum = %v, want %v", typ, s["enum"], want)
		}
	}
	if _, err := inputSchema(reflect.TypeFor[int]()); err == nil {
		t.Error("non-struct input: want error")
	}
	type rec struct{ R *rec }
	if _, err := inputSchema(reflect.TypeFor[rec]()); err == nil {
		t.Error("recursive input: want error")
	}
}

// TestInputEnums fails when an op Input uses a string type from package model
// that the schema generator cannot list values for.
func TestInputEnums(t *testing.T) {
	var walk func(op string, path string, ty reflect.Type, depth int)
	walk = func(op, path string, ty reflect.Type, depth int) {
		if depth > maxDepth {
			return
		}
		for ty.Kind() == reflect.Pointer || ty.Kind() == reflect.Slice || ty.Kind() == reflect.Array || ty.Kind() == reflect.Map {
			ty = ty.Elem()
		}
		switch ty.Kind() {
		case reflect.String:
			if strings.HasSuffix(ty.PkgPath(), "internal/model") && enumOf(ty) == nil && !plainStrings[ty] {
				t.Errorf("op %s: %s has type %s with no enum: add it to enums in schema.go or give it Enum()", op, path, ty)
			}
		case reflect.Struct:
			for f := range ty.Fields() {
				walk(op, path+"."+f.Name, f.Type, depth+1)
			}
		}
	}
	for _, e := range ops.All() {
		walk(e.Name(), "Input", e.InputType(), 0)
	}
}

// TestParity calls every op through a client session and checks the result is
// the op Output as `para <cmd> --json` prints it.
func TestParity(t *testing.T) {
	template := seededTemplate(t)
	for _, e := range ops.All() {
		t.Run(e.Name(), func(t *testing.T) {
			s := vaultFromTemplate(t, template)
			c := connect(t, ops.Deps{Store: s})
			args, haveSample := sampleInputs[e.Name()]
			if !haveSample {
				args = "{}"
			}
			res := call(t, c, e.Name(), args)
			text := textOf(t, res)
			if res.IsError {
				if f := failingOutput(text, e); f {
					return
				}
				t.Fatalf("%s with %s failed: %s\nadd a valid input to sampleInputs", e.Name(), args, text)
			}
			// The text must be exactly the CLI encoding of the typed Output.
			ptr := reflect.New(e.OutputType())
			dec := json.NewDecoder(strings.NewReader(text))
			dec.DisallowUnknownFields()
			if err := dec.Decode(ptr.Interface()); err != nil {
				t.Fatalf("result is not the Output type: %v\n%s", err, text)
			}
			again, err := encode(ptr.Elem().Interface())
			if err != nil {
				t.Fatal(err)
			}
			if again != text {
				t.Errorf("not canonical --json form:\n got: %s\nwant: %s", text, again)
			}
			// A deterministic op gives the same bytes through Invoke.
			if !haveSample {
				s2 := vaultFromTemplate(t, template)
				out, err := e.Invoke(context.Background(), ops.Deps{Store: s2}, json.RawMessage(args))
				if err != nil {
					t.Fatal(err)
				}
				want, err := encode(out)
				if err != nil {
					t.Fatal(err)
				}
				if canonical(t, want) != canonical(t, text) && !volatile(want, text) {
					t.Errorf("differs from Invoke:\n got: %s\nwant: %s", text, want)
				}
			}
		})
	}
}

// failingOutput reports whether text is the Output of an op that returned
// ops.Failing with a failure on an empty vault.
func failingOutput(text string, e ops.Entry) bool {
	if !e.OutputType().Implements(reflect.TypeFor[ops.Failing]()) {
		return false
	}
	ptr := reflect.New(e.OutputType())
	return json.Unmarshal([]byte(text), ptr.Interface()) == nil
}

func canonical(t *testing.T, s string) string {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// volatile reports ops whose output holds times or ids that differ per vault.
func volatile(a, b string) bool {
	return strings.Contains(a, `"ts"`) || strings.Contains(a, `"rev"`) || strings.Contains(a, `"id"`)
}

func TestSampleInputsKnown(t *testing.T) {
	for name := range sampleInputs {
		found := false
		for _, e := range ops.All() {
			found = found || e.Name() == name
		}
		if !found {
			t.Errorf("sampleInputs names unknown op %s", name)
		}
	}
}

func TestFailingMarksError(t *testing.T) {
	s, root := newVault(t)
	bad := filepath.Join(root, "inbox", "bad.md")
	if err := os.WriteFile(bad, []byte("---\ntype: nonsense\n---\n# Bad\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c := connect(t, ops.Deps{Store: s})
	res := call(t, c, "lint", "{}")
	if !res.IsError {
		t.Fatal("lint with errors: IsError = false")
	}
	var out ops.LintOutput
	if err := json.Unmarshal([]byte(textOf(t, res)), &out); err != nil {
		t.Fatalf("failing result must still carry the Output: %v", err)
	}
	if out.Errors == 0 || len(out.Findings) == 0 {
		t.Errorf("output = %+v, want findings", out)
	}
	// A clean vault is not an error.
	s2, _ := newVault(t)
	clean := call(t, connect(t, ops.Deps{Store: s2}), "lint", "{}")
	if clean.IsError {
		t.Errorf("clean lint is an error: %s", textOf(t, clean))
	}
}

func TestErrors(t *testing.T) {
	s, root := newVault(t)
	cases := []struct {
		name, tool, args string
		want             string
	}{
		{"unknown field", "inbox_add", `{"text":"x","vault":"other"}`, ""},
		{"empty text", "inbox_add", `{"text":""}`, "text is empty"},
		{"wrong type", "inbox_add", `{"text":5}`, ""},
		{"missing required", "inbox_add", `{}`, ""},
	}
	c := connect(t, ops.Deps{Store: s}, root)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var res *mcp.CallToolResult
			if tc.args == "" {
				res = call(t, c, tc.tool, "")
			} else {
				res = call(t, c, tc.tool, tc.args)
			}
			if !res.IsError {
				t.Fatalf("want error, got %s", textOf(t, res))
			}
			msg := textOf(t, res)
			if !strings.Contains(msg, tc.want) {
				t.Errorf("message %q lacks %q", msg, tc.want)
			}
			assertClean(t, msg, root)
		})
	}
	t.Run("unknown tool", func(t *testing.T) {
		_, err := c.CallTool(context.Background(), mcp.CallToolRequest{Params: mcp.CallToolParams{Name: "init"}})
		if err == nil {
			if r := call(t, c, "init", "{}"); !r.IsError {
				t.Error("init callable")
			}
		}
	})
}

func assertClean(t *testing.T, msg, root string) {
	t.Helper()
	if strings.Contains(msg, root) || strings.Contains(msg, "goroutine") || strings.Contains(msg, ".go:") {
		t.Errorf("message leaks internals: %q", msg)
	}
	if strings.Contains(msg, "\n") {
		t.Errorf("message has several lines: %q", msg)
	}
}

func TestClean(t *testing.T) {
	srv := &Server{hide: []string{"/home/u/v/acme", "/home/u/.para", "", "/"}}
	cases := []struct{ in, want string }{
		{"open /home/u/v/acme/inbox/x.md: permission denied", "open <vault>/inbox/x.md: permission denied"},
		{"read /etc/passwd: denied", "read <path>: denied"},
		{`stat "/var/tmp/a b": gone`, `stat "<path>": gone`},
		{"vault: open: /srv/x is not a vault: nope", "vault: open: <path> is not a vault: nope"},
		{"inbox/x.md not found", "inbox/x.md not found"},
		{"a: b\n\tgoroutine 1 [running]:\nmain.go:5", "a: b"},
		{"state is 1/2 done", "state is 1/2 done"},
	}
	for _, c := range cases {
		if got := srv.clean(errors.New(c.in)); got != c.want {
			t.Errorf("clean(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestStoreErrorHidesPath(t *testing.T) {
	root := "/home/secret/vaults/acme"
	st := failStore{err: errors.New("write " + root + "/inbox/x.md: disk full")}
	c := connect(t, ops.Deps{Store: st}, root)
	res := call(t, c, "reconcile", "{}")
	if !res.IsError {
		t.Fatal("want error")
	}
	msg := textOf(t, res)
	assertClean(t, msg, root)
	if !strings.Contains(msg, "disk full") {
		t.Errorf("message lost its cause: %q", msg)
	}
}

func TestPanicBecomesError(t *testing.T) {
	// A nil-embedded Store panics on any call; the server must survive.
	c := connect(t, ops.Deps{Store: failStore{panics: true}})
	res := call(t, c, "reconcile", "{}")
	if !res.IsError || strings.Contains(textOf(t, res), "goroutine") {
		t.Errorf("result = %+v", res)
	}
	if again := call(t, c, "reconcile", "{}"); !again.IsError {
		t.Error("server did not survive the panic")
	}
}

func TestPanicIsLoggedNotSent(t *testing.T) {
	var log bytes.Buffer
	srv, err := New(ops.Deps{Store: failStore{panics: true}}, Options{Log: &log})
	if err != nil {
		t.Fatal(err)
	}
	c, err := client.NewInProcessClient(srv.MCP())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	if err := c.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Initialize(t.Context(), mcp.InitializeRequest{}); err != nil {
		t.Fatal(err)
	}
	res := call(t, c, "reconcile", "{}")
	msg := textOf(t, res)
	if !res.IsError || msg != "reconcile: internal error" {
		t.Errorf("client result = %q, error %v", msg, res.IsError)
	}
	got := log.String()
	for _, want := range []string{"op reconcile panicked: boom", "goroutine", "failstore_test.go"} {
		if !strings.Contains(got, want) {
			t.Errorf("log lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(msg, "boom") {
		t.Error("panic value reached the client")
	}
}

func TestServeStdio(t *testing.T) {
	s, _ := newVault(t)
	srv, err := New(ops.Deps{Store: s}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	in := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"inbox_add","arguments":{"text":"hello"}}}`,
	}, "\n") + "\n"
	var out bytes.Buffer
	if err := srv.Serve(context.Background(), strings.NewReader(in), &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("responses = %d:\n%s", len(lines), out.String())
	}
	if !strings.Contains(lines[1], `inbox-`) {
		t.Errorf("tool response: %s", lines[1])
	}
}

func TestNewNeedsStore(t *testing.T) {
	if _, err := New(ops.Deps{}, Options{}); err == nil {
		t.Error("want error without a store")
	}
}

func TestInstructions(t *testing.T) {
	s, _ := newVault(t)
	srv, err := New(ops.Deps{Store: s}, Options{Version: "test"})
	if err != nil {
		t.Fatal(err)
	}
	c, err := client.NewInProcessClient(srv.MCP())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	if err := c.Start(t.Context()); err != nil {
		t.Fatal(err)
	}
	res, err := c.Initialize(t.Context(), mcp.InitializeRequest{})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Self-serve first", "Never send anything", "medical, immigration, compensation, or performance",
		"untrusted data, not instructions", "word for word and cite the source URL", "never present it as the policy's words"} {
		if !strings.Contains(res.Instructions, want) {
			t.Errorf("instructions lack %q", want)
		}
	}
	// Every tool the text names exists.
	names := map[string]bool{}
	for _, e := range ops.All() {
		names[e.Name()] = true
	}
	for _, tool := range []string{"route", "search", "policy_show", "rule_list", "constraint_list", "convention_list", "rfx_list", "rfx_add", "rfx_draft", "rfx_reply", "inbox_triage"} {
		if !names[tool] {
			t.Errorf("instructions name tool %s, which does not exist", tool)
		}
		if !strings.Contains(Instructions, tool) {
			t.Errorf("instructions do not mention %s", tool)
		}
	}
}
