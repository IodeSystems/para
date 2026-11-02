package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/iodesystems/parasolve/internal/ops"
)

type env struct {
	t    *testing.T
	home string
	dir  string
	vars map[string]string
	// stdin is read by the next run, then cleared.
	stdin string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	// A test must not see or probe the service installed on this machine.
	t.Setenv("HOME", dir)
	return &env{t: t, home: filepath.Join(dir, "home"), dir: dir, vars: map[string]string{}}
}

// run executes one para command in-process with a fresh loader.
func (e *env) run(args ...string) (stdout, stderr string, err error) {
	e.t.Helper()
	var out, errb bytes.Buffer
	l := &loader{version: "test", env: func(k string) string {
		if k == "PARA_HOME" {
			return e.home
		}
		return e.vars[k]
	}}
	root := newRoot(l)
	root.SetOut(&out)
	root.SetErr(&errb)
	root.SetArgs(args)
	root.SetIn(strings.NewReader(e.stdin))
	e.stdin = ""
	err = root.Execute()
	return out.String(), errb.String(), err
}

func (e *env) ok(args ...string) string {
	e.t.Helper()
	out, _, err := e.run(args...)
	if err != nil {
		e.t.Fatalf("para %v: %v", args, err)
	}
	return out
}

func (e *env) json(v any, args ...string) {
	e.t.Helper()
	out := e.ok(append(args, "--json")...)
	if err := json.Unmarshal([]byte(out), v); err != nil {
		e.t.Fatalf("para %v --json: stdout is not JSON: %v\n%s", args, err, out)
	}
}

func (e *env) init(id string) string {
	e.t.Helper()
	path := filepath.Join(e.dir, id)
	e.ok("init", id, path)
	return path
}

func TestEndToEnd(t *testing.T) {
	const capture = "zebrafish synthetic capture"
	e := newEnv(t)
	path := e.init("acme")

	var added ops.InboxAddOutput
	e.json(&added, "inbox", "add", capture)
	if !strings.HasPrefix(added.ID, "inbox-") || added.Folder != "inbox" {
		t.Fatalf("added = %+v", added)
	}
	data, err := os.ReadFile(filepath.Join(path, "inbox", added.ID+".md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "# "+capture) {
		t.Fatalf("file:\n%s", data)
	}

	var lint ops.LintOutput
	e.json(&lint, "lint")
	if lint.Errors != 0 || lint.Warnings != 0 || len(lint.Findings) != 0 {
		t.Fatalf("lint = %+v", lint)
	}

	var tail ops.EventsTailOutput
	e.json(&tail, "events", "tail")
	if len(tail.Events) != 1 {
		t.Fatalf("events = %+v", tail.Events)
	}
	r := tail.Events[0]
	if r.Op != "create" || r.Doc != added.ID || r.Vault != "acme" || r.Actor != "user" || r.Surface != "cli" || r.To != "inbox" {
		t.Fatalf("event = %+v", r)
	}
	if raw := e.ok("events", "tail", "--json"); strings.Contains(raw, "zebrafish") {
		t.Fatalf("content in events:\n%s", raw)
	}
}

func TestStdoutIsOnlyJSON(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	cases := [][]string{
		{"inbox", "add", "x", "--json"},
		{"lint", "--json"},
		{"reconcile", "--json"},
		{"events", "tail", "--json"},
		{"vault", "list", "--json"},
		{"vault", "use-check", "--json"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			out, errOut, err := e.run(args...)
			if err != nil {
				t.Fatal(err)
			}
			if !json.Valid([]byte(out)) {
				t.Fatalf("stdout = %q", out)
			}
			if errOut != "" {
				t.Fatalf("stderr = %q", errOut)
			}
		})
	}
}

func TestInitFailures(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"id already registered", []string{"init", "acme", filepath.Join(e.dir, "other")}, "already registered"},
		{"bad id", []string{"init", "Not_OK", filepath.Join(e.dir, "bad")}, "DNS label"},
		{"path in use", []string{"init", "second", filepath.Join(e.dir, "acme")}, "not empty"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, _, err := e.run(c.args...); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("err = %v", err)
			}
		})
	}
	var list vaultListOutput
	e.json(&list, "vault", "list")
	if len(list.Vaults) != 1 {
		t.Fatalf("a failed init registered a vault: %+v", list)
	}
}

func TestVaultSelection(t *testing.T) {
	e := newEnv(t)
	if _, _, err := e.run("inbox", "add", "x"); err == nil || !strings.Contains(err.Error(), "para init") {
		t.Fatalf("no vaults: err = %v", err)
	}
	e.init("acme")
	e.ok("inbox", "add", "goes to the only vault")
	e.init("personal")

	cases := []struct {
		name  string
		args  []string
		envV  string
		want  string // selected vault, or empty for an error
		inErr []string
	}{
		{"ambiguous", nil, "", "", []string{"acme", "personal", "--vault"}},
		{"flag", []string{"--vault", "personal"}, "", "personal", nil},
		{"env", nil, "acme", "acme", nil},
		{"flag beats env", []string{"--vault", "personal"}, "acme", "personal", nil},
		{"unknown", []string{"--vault", "nope"}, "", "", []string{"nope", "acme", "personal"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e.vars["PARA_VAULT"] = c.envV
			defer delete(e.vars, "PARA_VAULT")
			args := append([]string{"vault", "use-check", "--json"}, c.args...)
			out, _, err := e.run(args...)
			if c.want == "" {
				if err == nil {
					t.Fatalf("selected: %s", out)
				}
				for _, w := range c.inErr {
					if !strings.Contains(err.Error(), w) {
						t.Errorf("error %q lacks %q", err, w)
					}
				}
				if out != "" {
					t.Errorf("stdout on error: %q", out)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var sel struct {
				ID string `json:"id"`
			}
			json.Unmarshal([]byte(out), &sel)
			if sel.ID != c.want {
				t.Fatalf("selected %s", out)
			}
		})
	}
	// An op honors the same rule.
	if _, _, err := e.run("inbox", "add", "x"); err == nil {
		t.Fatal("op ran with two vaults and no choice")
	}
}

func TestVaultRefMustMatchVault(t *testing.T) {
	e := newEnv(t)
	acme := e.init("acme")
	e.init("personal")
	ref := filepath.Join(e.home, "vaults", "personal", "vault.ref")
	os.WriteFile(ref, []byte("path="+acme+"\n"), 0o600)
	if _, _, err := e.run("--vault", "personal", "inbox", "add", "x"); err == nil {
		t.Fatal("wrote through a ref that points at another vault")
	}
}

func TestLintFailsWithFindings(t *testing.T) {
	e := newEnv(t)
	path := e.init("acme")
	os.WriteFile(filepath.Join(path, "inbox", "hand-made.md"), []byte("no frontmatter\n"), 0o600)
	cases := []struct {
		name string
		args []string
	}{
		{"vault", []string{"lint", "--json"}},
		{"path", []string{"lint", path, "--json"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, _, err := e.run(c.args...)
			if err == nil {
				t.Fatal("want non-zero exit")
			}
			var got ops.LintOutput
			if jerr := json.Unmarshal([]byte(out), &got); jerr != nil {
				t.Fatalf("stdout %q: %v", out, jerr)
			}
			if got.Errors == 0 || got.Findings[0].Path != "inbox/hand-made.md" {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestLintPathNeedsNoVault(t *testing.T) {
	e := newEnv(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "x-doc.md"), []byte("---\ntype: inbox\ncreated: 2026-11-01\n---\n\n# X\n"), 0o600)
	if out := e.ok("lint", dir); !strings.Contains(out, "0 errors") {
		t.Fatalf("out = %q", out)
	}
}

func TestInboxAddRefusesBadText(t *testing.T) {
	e := newEnv(t)
	path := e.init("acme")
	if _, _, err := e.run("inbox", "add", "see <b>this</b>"); err == nil || !strings.Contains(err.Error(), "raw-html") {
		t.Fatalf("err = %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(path, "inbox")); len(entries) != 0 {
		t.Fatalf("wrote %v", entries)
	}
	var tail ops.EventsTailOutput
	e.json(&tail, "events", "tail")
	if len(tail.Events) != 0 {
		t.Fatalf("events = %+v", tail.Events)
	}
}

func TestReconcileCommand(t *testing.T) {
	e := newEnv(t)
	path := e.init("acme")
	var added ops.InboxAddOutput
	e.json(&added, "inbox", "add", "synthetic")
	file := filepath.Join(path, "inbox", added.ID+".md")
	raw, _ := os.ReadFile(file)
	os.WriteFile(file, append(raw, []byte("By hand.\n")...), 0o600)
	// One-machine vault: log a hand edit at once.
	propsFile := filepath.Join(path, ".para", "vault.properties")
	cfg, _ := os.ReadFile(propsFile)
	os.WriteFile(propsFile, append(cfg, []byte("reconcile.grace=0s\n")...), 0o600)

	if out := e.ok("reconcile"); !strings.Contains(out, "changed\tinbox/"+added.ID+".md") {
		t.Fatalf("out = %q", out)
	}
	var tail ops.EventsTailOutput
	e.json(&tail, "events", "tail", "-n", "1")
	if len(tail.Events) != 1 || tail.Events[0].Op != "external_edit" {
		t.Fatalf("events = %+v", tail.Events)
	}
}

// walk visits every command under c.
func walk(c *cobra.Command, fn func(*cobra.Command)) {
	fn(c)
	for _, sub := range c.Commands() {
		walk(sub, fn)
	}
}

// Parity rule 1: each op has one command, at the path its name spells.
func TestEveryOpHasACommand(t *testing.T) {
	got := map[string]string{}
	root := newRoot(&loader{})
	walk(root, func(c *cobra.Command) {
		if name := c.Annotations[opAnnotation]; name != "" {
			path := strings.TrimPrefix(c.CommandPath(), root.Name()+" ")
			got[name] = strings.NewReplacer(" ", "_", "-", "_").Replace(path) // task to-question is op task_to_question
		}
	})
	for _, e := range ops.All() {
		if got[e.Name()] != e.Name() {
			t.Errorf("op %s: command path %q", e.Name(), got[e.Name()])
		}
	}
}

// parityCase is one op of TestInvokeMatchesCLIJSON.
type parityCase struct {
	input    string
	args     []string
	sameData bool
}

func jsonKeys(t *testing.T, raw []byte) []string {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("%v: %s", err, raw)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Parity rule 2: Entry.Invoke, the path a registry surface uses, returns the
// shape of `para <cmd> --json`. Ids differ per run, so keys are compared; for
// read-only ops the whole value is compared.
func TestInvokeMatchesCLIJSON(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	l := &loader{env: func(k string) string {
		if k == "PARA_HOME" {
			return e.home
		}
		return ""
	}}
	if err := l.load(); err != nil {
		t.Fatal(err)
	}
	deps, err := l.deps()
	if err != nil {
		t.Fatal(err)
	}
	seedWork(t, deps)
	cases := map[string]parityCase{
		"inbox_add":       {`{"text":"synthetic"}`, []string{"inbox", "add", "synthetic"}, false},
		"lint":            {``, []string{"lint"}, true},
		"reconcile":       {`{}`, []string{"reconcile"}, true},
		"events_tail":     {`{"limit":20}`, []string{"events", "tail"}, true},
		"project_add":     {`{"title":"One","owner":"Jordan Example"}`, []string{"project", "add", "One", "--owner", "Jordan Example"}, false},
		"project_list":    {`{}`, []string{"project", "list"}, true},
		"project_show":    {`{"id":"project-a"}`, []string{"project", "show", "project-a"}, true},
		"project_close":   {`{"id":"project-c1"}`, []string{"project", "close", "project-c2"}, false},
		"task_add":        {`{"project":"project-a","title":"T","depends_on":["task-d1"]}`, []string{"task", "add", "T", "-p", "project-a", "--depends-on", "task-d1"}, false},
		"task_list":       {`{"project":"project-a"}`, []string{"task", "list", "-p", "project-a"}, false},
		"task_show":       {`{"id":"task-d1"}`, []string{"task", "show", "task-d1"}, true},
		"task_start":      {`{"id":"task-s1"}`, []string{"task", "start", "task-s2"}, false},
		"task_done":       {`{"id":"task-d2"}`, []string{"task", "done", "task-d3"}, false},
		"task_block":      {`{"id":"task-b1","reason":"waiting"}`, []string{"task", "block", "task-b2", "--reason", "waiting"}, false},
		"graph":           {`{"project":"project-a"}`, []string{"graph", "project-a"}, false},
		"next":            {`{"project":"project-a"}`, []string{"next", "project-a"}, false},
		"journal_daily":   {`{"date":"2026-11-01"}`, []string{"journal", "daily", "2026-11-01"}, false},
		"journal_weekly":  {`{"date":"2026-11-01"}`, []string{"journal", "weekly", "2026-11-01"}, false},
		"journal_monthly": {`{"date":"2026-11-01"}`, []string{"journal", "monthly", "2026-11-01"}, false},
		"index_rebuild":   {`{}`, []string{"index", "rebuild"}, true},
		"search":          {`{"query":"synthetic"}`, []string{"search", "synthetic"}, true},
	}
	seedRFXParity(t, deps)
	maps.Copy(cases, rfxParityCases)
	govCases, stdins := governanceParityCases(e)
	maps.Copy(cases, govCases)
	seedDirectoryParity(t, deps)
	maps.Copy(cases, directoryParityCases)
	seedRequestParity(t, deps)
	maps.Copy(cases, requestParityCases)
	seedStreamPParity(t, deps)
	pCases, pStdins := streamPParityCases()
	maps.Copy(cases, pCases)
	maps.Copy(stdins, pStdins)
	seedExportParity(t, deps)
	maps.Copy(cases, exportParityCases)
	seedStreamSParity(t, deps)
	maps.Copy(cases, streamSParityCases())
	seedStreamUParity(t, deps)
	maps.Copy(cases, streamUParityCases())
	seedQuestionParity(t, deps)
	maps.Copy(cases, questionParityCases())
	seedStreamVParity(t, deps)
	maps.Copy(cases, streamVParityCases())
	seedProposalParity(t, deps)
	proposalCases, proposalStdins := proposalParityCases()
	maps.Copy(cases, proposalCases)
	maps.Copy(stdins, proposalStdins)
	all := ops.All()
	if len(all) != len(cases) {
		t.Fatalf("%d ops, %d cases: add a case for each new op", len(all), len(cases))
	}
	for _, entry := range all {
		t.Run(entry.Name(), func(t *testing.T) {
			c, ok := cases[entry.Name()]
			if !ok {
				t.Fatal("no case")
			}
			got, err := entry.Invoke(context.Background(), deps, json.RawMessage(c.input))
			if err != nil {
				t.Fatal(err)
			}
			viaInvoke, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			e.stdin = stdins[entry.Name()]
			viaCLI := e.ok(append(c.args, "--json")...)
			if a, b := jsonKeys(t, viaInvoke), jsonKeys(t, []byte(viaCLI)); !reflect.DeepEqual(a, b) {
				t.Fatalf("keys: invoke %v, cli %v", a, b)
			}
			if !c.sameData {
				return
			}
			var a, b any
			json.Unmarshal(viaInvoke, &a)
			json.Unmarshal([]byte(viaCLI), &b)
			if !reflect.DeepEqual(a, b) {
				t.Fatalf("invoke %s\ncli %s", viaInvoke, viaCLI)
			}
		})
	}
}

func TestInitNoGit(t *testing.T) {
	e := newEnv(t)
	path := filepath.Join(e.dir, "ng")
	e.ok("init", "ng", path, "--no-git")
	if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
		t.Fatal("git init ran")
	}
	ign, _ := os.ReadFile(filepath.Join(path, ".gitignore"))
	if !strings.Contains(string(ign), ".para/*") || !strings.Contains(string(ign), "!.para/vault.properties") {
		t.Fatalf(".gitignore = %q", ign)
	}
	if left, _ := os.ReadDir(filepath.Join(path, ".para")); len(left) != 1 {
		t.Fatalf(".para holds %v", left)
	}
	if _, err := os.Stat(filepath.Join(e.home, "vaults", "ng", "manifest.json")); err != nil {
		t.Fatalf("manifest not in state dir: %v", err)
	}
}

func TestTodayShowsSyncConflicts(t *testing.T) {
	e := newEnv(t)
	path := e.init("acme")
	os.WriteFile(filepath.Join(path, "inbox", "x (1).md"), []byte("c"), 0o600)
	if out := e.ok("today"); !strings.Contains(out, "sync-conflict\tinbox/x (1).md") {
		t.Fatalf("today = %q", out)
	}
	if out := e.ok("lint"); !strings.Contains(out, "sync-conflict") {
		t.Fatalf("lint = %q", out)
	}
}
