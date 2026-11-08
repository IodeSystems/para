package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/keys"
	"github.com/iodesystems/para/internal/ops"
	"github.com/iodesystems/para/internal/registry"
	"github.com/iodesystems/para/internal/server"
)

func TestSchemaDumpNeedsNoVaultAndMatchesCommitted(t *testing.T) {
	e := newEnv(t)
	out := filepath.Join(e.dir, "gen", "schema.graphql")
	e.ok("schema", "dump", out)
	if _, err := os.Stat(e.home); err == nil {
		t.Error("schema dump touched the home")
	}
	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("../../ui/gen/schema.graphql")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Error("ui/gen/schema.graphql is stale: run make schema")
	}
}

func TestKeyCommands(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	tok := strings.TrimSpace(e.ok("key", "create", "agent", "--scope", "vault:read", "--scope", "rfx:draft", "--allow-ip", "10.0.0.5", "--ttl", "30d"))
	if !strings.HasPrefix(tok, "para_agent_") {
		t.Fatalf("token %q", tok)
	}
	list := e.ok("key", "list")
	if !strings.Contains(list, "agent\tvault:read,rfx:draft\t10.0.0.5\texpires") || strings.Contains(list, tok) {
		t.Errorf("list: %s", list)
	}
	var keysOut []map[string]any
	e.json(&keysOut, "key", "list")
	if keysOut[0]["hash"] != "" {
		t.Errorf("hash shown: %v", keysOut[0])
	}
	url := e.ok("key", "create", "web", "--scope", "vault:read", "--print-url", "--url", "http://acme.localhost:7700/")
	if !strings.HasPrefix(url, "http://acme.localhost:7700/#token=para_web_") {
		t.Errorf("url: %s", url)
	}
	e.ok("key", "revoke", "agent")
	if !strings.Contains(e.ok("key", "list"), "revoked") {
		t.Error("not revoked")
	}
	for _, bad := range [][]string{
		{"key", "create", "x"},
		{"key", "create", "x", "--scope", "admin"},
		{"key", "create", "x", "--scope", "nope"},
		{"key", "create", "admin", "--scope", "vault:read"},
		{"key", "create", "x", "--scope", "vault:read", "--ttl", "soon"},
		{"key", "revoke", "missing"},
	} {
		if _, _, err := e.run(bad...); err == nil {
			t.Errorf("%v was accepted", bad)
		}
	}
	info, err := os.Stat(filepath.Join(server.ServerDir(registry.Install{Home: e.home}, "acme"), "keys", "web.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("key file mode %o", info.Mode().Perm())
	}
}

func TestKeyCreateURLNamesTheVault(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	e.init("beta")
	if err := os.WriteFile(filepath.Join(e.home, "server.properties"), []byte("net.base_hosts=example.test\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e.ok("key", "create", "phone", "--scope", "vault:read", "--url", "https://beta.example.test")
	if _, err := os.Stat(filepath.Join(server.ServerDir(registry.Install{Home: e.home}, "beta"), "keys", "phone.json")); err != nil {
		t.Errorf("key not in vault beta: %v", err)
	}
	if _, err := os.Stat(filepath.Join(server.ServerDir(registry.Install{Home: e.home}, "acme"), "keys", "phone.json")); err == nil {
		t.Error("key made in vault acme too")
	}
	for _, bad := range [][]string{
		{"key", "create", "x", "--scope", "vault:read", "--url", "https://beta.example.test", "--vault", "acme"},
		{"key", "create", "x", "--scope", "vault:read", "--url", "https://beta.other.test"},
		{"key", "create", "x", "--scope", "vault:read", "--url", "https://gamma.example.test"},
		{"key", "create", "x", "--scope", "vault:read", "--url", "not a url"},
	} {
		if _, _, err := e.run(bad...); err == nil {
			t.Errorf("%v was accepted", bad)
		}
	}
}

func TestServeCheckProvesFlags(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	e.ok("serve", "--check", "--trusted-proxy", "10.0.0.1", "--vault", "acme")
	e.ok("serve", "--check", "--addr", "127.0.0.1:7701", "--addr", "[::1]:7701", "--vault", "acme")
	for _, bad := range [][]string{
		{"serve", "--check", "--trusted-proxy", "10.0.0.0/8"},
		{"serve", "--check", "--addr", "nonsense"},
		{"serve", "--check", "--addr", "127.0.0.1:7701", "--addr", "nonsense"},
		{"serve", "--check", "--addr", "127.0.0.1:7701", "--addr", "127.0.0.1:7701"},
		{"serve", "--check", "--vault", "missing"},
		{"serve", "--check", "--web-root", "/nonexistent/web"},
		{"serve", "--check", "--insecure"},
	} {
		if _, _, err := e.run(bad...); err == nil {
			t.Errorf("%v was accepted", bad)
		}
	}
	dir := server.ServerDir(registry.Install{Home: e.home}, "acme")
	info, err := os.Stat(filepath.Join(dir, "admin.token"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("admin.token mode %o", info.Mode().Perm())
	}
}

func TestServiceInstallPrint(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	e.vars["PATH"] = "/usr/bin:/opt/tools"
	got := e.ok("--vault", "acme", "service", "install", "--print", "--exec", "/opt/para", "--", "--addr", "127.0.0.1:7701")
	for _, want := range []string{"/opt/para serve", "--addr 127.0.0.1:7701", "--vault acme"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in:\n%s", want, got)
		}
	}
	if useLaunchd() == strings.Contains(got, "ExecStartPre=/opt/para serve --check") {
		t.Errorf("check line:\n%s", got)
	}
	if !strings.Contains(got, "/usr/bin:/opt/tools") || !strings.Contains(got, e.home) {
		t.Errorf("PATH or PARA_HOME not recorded:\n%s", got)
	}
	if _, _, err := e.run("service", "install", "--print", "--", "--no-such-flag"); err == nil {
		t.Error("a bad serve flag must stop the install")
	}
	if _, _, err := e.run("service", "install", "--print", "--exec", "relative/para"); err == nil {
		t.Error("a relative --exec must be refused")
	}
}

func TestServiceVerbsUseTheServiceManager(t *testing.T) {
	var got []string
	fake := func(_ io.Writer, name string, args ...string) error {
		got = append(got, name+" "+strings.Join(args, " "))
		return nil
	}
	for _, v := range []string{"start", "stop", "restart", "status", "logs"} {
		if err := serviceCtl(io.Discard, fake, v, "para", "/x/para.service"); err != nil {
			t.Fatal(err)
		}
	}
	if len(got) != 5 {
		t.Fatalf("%v", got)
	}
	if !useLaunchd() && got[0] != "systemctl --user enable --now para.service" {
		t.Errorf("%v", got)
	}
}

func TestSignInURLUsesFragment(t *testing.T) {
	got := signInURL("http://acme.localhost:7700/", "para_x_a+b")
	if got != "http://acme.localhost:7700/#token=para_x_a%2Bb" {
		t.Error(got)
	}
	if localBase("acme", "0.0.0.0:7800") != "http://acme.localhost:7800" {
		t.Error(localBase("acme", "0.0.0.0:7800"))
	}
}

// runningServer starts the real binary on a free loopback port and returns its
// port. The test stops it with SIGTERM to its own pid and proves a clean exit.
func runningServer(t *testing.T, e *env, args ...string) (string, func() error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, os.Args[0], append([]string{"serve", "--addr", "127.0.0.1:0"}, args...)...)
	cmd.Env = append(os.Environ(), "PARA_TEST_BINARY=1", "PARA_HOME="+e.home, "PARA_VAULT=")
	stderr, err := cmd.StderrPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	addrc := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			var rec struct {
				Msg  string `json:"msg"`
				Addr string `json:"addr"`
			}
			if json.Unmarshal(sc.Bytes(), &rec) == nil && rec.Msg == "para serving" {
				addrc <- rec.Addr
			}
		}
	}()
	var addr string
	select {
	case addr = <-addrc:
	case <-time.After(30 * time.Second):
		t.Fatal("server did not start")
	}
	stopped := false
	stop := func() error {
		if stopped {
			return nil
		}
		stopped = true
		if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
			return err
		}
		return cmd.Wait()
	}
	t.Cleanup(func() { _ = stop() })
	first, _, _ := strings.Cut(addr, ",")
	_, port, _ := strings.Cut(first, ":")
	return port, stop
}

func TestServeListensOnEveryAddr(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.2:0")
	if err != nil {
		t.Skip("127.0.0.2 is not a loopback address here:", err)
	}
	_ = probe.Close()
	e := newEnv(t)
	e.init("acme")
	_, stop := runningServer(t, e, "--addr", "127.0.0.2:0", "--vault", "acme")
	rec, ok := server.ReadRecord(e.home)
	if !ok {
		t.Fatal("no server record")
	}
	if len(rec.Addrs) != 2 || !strings.HasPrefix(rec.Addrs[0], "127.0.0.1:") || !strings.HasPrefix(rec.Addrs[1], "127.0.0.2:") {
		t.Fatalf("addrs %v", rec.Addrs)
	}
	if rec.Addr != rec.Addrs[0] {
		t.Errorf("client addr %s, want the loopback listener %s", rec.Addr, rec.Addrs[0])
	}
	for _, a := range rec.Addrs {
		resp, err := http.Get("http://" + a + "/healthz")
		if err != nil {
			t.Fatalf("%s: %v", a, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("%s: /healthz %d", a, resp.StatusCode)
		}
	}
	if err := stop(); err != nil {
		t.Fatalf("stop: %v", err)
	}
}

func TestClientAddrPrefersLoopback(t *testing.T) {
	for _, c := range []struct {
		in   []string
		want string
	}{
		{nil, ""},
		{[]string{"10.100.0.8:7700"}, "10.100.0.8:7700"},
		{[]string{"10.100.0.8:7700", "127.0.0.1:7700"}, "127.0.0.1:7700"},
		{[]string{"[::1]:7700", "127.0.0.1:7700"}, "[::1]:7700"},
	} {
		if got := clientAddr(c.in); got != c.want {
			t.Errorf("clientAddr(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func fetch(t *testing.T, port, host, path, token string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, "http://127.0.0.1:"+port+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = host
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(b)
}

// aliasOfLine is the alias_of key an alias call adds as the last key.
var aliasOfLine = regexp.MustCompile(`,\n  "alias_of": "[a-z_]+"`)

// TestAPIEqualsCLIJSON runs the real binary: the API body is the bytes of
// `para <cmd> --json`, and a key's writes land under its name.
func TestAPIEqualsCLIJSON(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	var project struct {
		ID string `json:"id"`
	}
	e.json(&project, "project", "add", "--done-when", "Done.", "Portal", "--owner", "Jordan Example")
	e.ok("task", "add", "Write parser", "--project", project.ID)
	e.ok("inbox", "add", "sample capture")
	tok := strings.TrimSpace(e.ok("key", "create", "agent", "--scope", "vault:read", "--scope", "vault:write"))
	port, stop := runningServer(t, e)
	host := "acme.localhost:" + port
	for _, c := range []struct {
		path string
		args []string
	}{
		{"/api/v1/project/list", []string{"project", "list"}},
		{"/api/v1/project/show?id=" + project.ID, []string{"project", "show", project.ID}},
		{"/api/v1/task/list", []string{"task", "list"}},
		{"/api/v1/io/list", []string{"io", "list"}},
		{"/api/v1/next", []string{"next"}},
		{"/api/v1/graph", []string{"graph"}},
		{"/api/v1/rfx/list", []string{"rfx", "list"}},
		{"/api/v1/org/person/list", []string{"org", "person", "list"}},
		{"/api/v1/lint", []string{"lint"}},
	} {
		code, body := fetch(t, port, host, c.path, tok)
		if code != 200 {
			t.Fatalf("%s: %d %s", c.path, code, body)
		}
		// project show is an alias of show: the CLI adds alias_of, the route
		// serves the old op.
		if want := aliasOfLine.ReplaceAllString(e.ok(append(c.args, "--json")...), ""); want != body {
			t.Errorf("%s differs from para %v --json:\n got: %s\nwant: %s", c.path, c.args, body, want)
		}
	}
	if code, _ := fetch(t, port, host, "/api/v1/task/list", ""); code != 401 {
		t.Errorf("no token: %d", code)
	}
	if code, _ := fetch(t, port, "evil.example", "/healthz", ""); code != http.StatusMisdirectedRequest {
		t.Errorf("rebinding: %d", code)
	}

	// CLI-8: a second server on this home is refused.
	if out, _, err := e.run("serve", "--addr", "127.0.0.1:0"); err == nil || !strings.Contains(fmt.Sprint(err), "another para serve") {
		t.Errorf("second server: %v %s", err, out)
	}
	if err := stop(); err != nil {
		t.Errorf("SIGTERM did not stop cleanly: %v", err)
	}
	for _, f := range []string{"serve.json", "serve.lock"} {
		if _, err := os.Stat(filepath.Join(e.home, "var", f)); err == nil {
			t.Errorf("var/%s left behind", f)
		}
	}
}

func TestBareHostNeedsAChosenVault(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	e.init("globex")
	port, _ := runningServer(t, e)
	if code, _ := fetch(t, port, "127.0.0.1:"+port, "/healthz", ""); code != http.StatusMisdirectedRequest {
		t.Errorf("two vaults, no choice: %d", code)
	}
	one := newEnv(t)
	one.init("acme")
	port, _ = runningServer(t, one)
	if code, _ := fetch(t, port, "127.0.0.1:"+port, "/healthz", ""); code != 200 {
		t.Errorf("only vault: %d", code)
	}
	def := newEnv(t)
	def.init("acme")
	def.init("globex")
	def.ok("vault", "set-default", "globex")
	port, _ = runningServer(t, def)
	if code, _ := fetch(t, port, "127.0.0.1:"+port, "/healthz", ""); code != 200 {
		t.Errorf("default vault: %d", code)
	}
	e2 := newEnv(t)
	e2.init("acme")
	port, _ = runningServer(t, e2, "--vault", "acme")
	if code, _ := fetch(t, port, "127.0.0.1:"+port, "/healthz", ""); code != 200 {
		t.Errorf("--vault: %d", code)
	}
}

func TestDoctor(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	got := e.ok("doctor")
	if !strings.Contains(got, "warn  vault acme: no server state yet") || !strings.Contains(got, "export format plain") {
		t.Errorf("%s", got)
	}
	port, stop := runningServer(t, e)
	base := "http://acme.localhost:" + port
	got = e.ok("doctor", "--exposure", base)
	for _, want := range []string{
		"ok    vault acme admin token", "ok    vault acme net.rules",
		"ok    exposure health", "ok    exposure headers", "ok    exposure no token read",
		"ok    exposure no token graphql", "ok    exposure rebinding host", "ok    exposure path trick", "ok    exposure forged client",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "fail ") {
		t.Errorf("a failure:\n%s", got)
	}
	if err := stop(); err != nil {
		t.Fatal(err)
	}

	// A bad state fails with its fix, and the exit is non-zero.
	dir := server.ServerDir(registry.Install{Home: e.home}, "acme")
	if err := os.Chmod(keys.New(dir).AdminPath(), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "net.rules"), []byte("permit / any\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, _, err := e.run("doctor")
	if err == nil {
		t.Error("doctor must exit non-zero")
	}
	if !strings.Contains(out, "fail  vault acme admin token") || !strings.Contains(out, "chmod 600") || !strings.Contains(out, "fail  vault acme net.rules") {
		t.Errorf("%s", out)
	}
}

func TestLoginAdminPrint(t *testing.T) {
	e := newEnv(t)
	e.init("acme")
	if _, _, err := e.run("service", "login-admin", "--print"); err == nil {
		t.Error("no token yet must be an error")
	}
	port, _ := runningServer(t, e)
	got := e.ok("service", "login-admin", "--print", "--url", "http://acme.localhost:"+port)
	if !strings.HasPrefix(got, "http://acme.localhost:"+port+"/#token=") {
		t.Errorf("%s", got)
	}
	if _, _, err := e.run("service", "login-admin", "--print", "--url", "http://acme.localhost:1"); err == nil {
		t.Error("a dead server must be an error")
	}
}

// A real `para serve` logs a hand edit by itself, and stops cleanly.
func TestServeLogsHandEdits(t *testing.T) {
	e := newEnv(t)
	path := e.init("acme")
	props := filepath.Join(path, ".para", "vault.properties")
	raw, err := os.ReadFile(props)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(props, append(raw, []byte("\nreconcile.grace=0s\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	_, stop := runningServer(t, e)
	time.Sleep(500 * time.Millisecond)
	start := time.Now()
	if err := os.WriteFile(filepath.Join(path, "io", "inbox-hand.md"), []byte("---\ntype: inbox\n---\n# By hand\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for {
		var tail ops.EventsTailOutput
		e.json(&tail, "events", "tail")
		found := false
		for _, ev := range tail.Events {
			found = found || (ev.Op == "external_edit" && ev.Doc == "inbox-hand")
		}
		if found {
			t.Logf("external_edit logged %v after the write", time.Since(start))
			break
		}
		if time.Since(start) > 10*time.Second {
			t.Fatalf("no external_edit: %+v", tail.Events)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := stop(); err != nil {
		t.Fatalf("serve did not stop cleanly: %v", err)
	}
}
