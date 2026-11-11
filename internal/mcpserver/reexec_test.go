package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/iodesystems/para/internal/ops"
)

// memSource is an in-memory stdin that is safe as fd 0 is: wait does not
// take bytes, read does.
type memSource struct {
	mu     sync.Mutex
	cond   *sync.Cond
	buf    []byte
	closed bool
}

func newMemSource() *memSource {
	m := &memSource{}
	m.cond = sync.NewCond(&m.mu)
	return m
}

func (m *memSource) write(s string) {
	m.mu.Lock()
	m.buf = append(m.buf, s...)
	m.cond.Broadcast()
	m.mu.Unlock()
}

func (m *memSource) close() {
	m.mu.Lock()
	m.closed = true
	m.cond.Broadcast()
	m.mu.Unlock()
}

func (m *memSource) wait() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for len(m.buf) == 0 && !m.closed {
		m.cond.Wait()
	}
	return nil
}

func (m *memSource) read(p []byte) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.buf) == 0 {
		if m.closed {
			return 0, io.EOF
		}
		return 0, nil
	}
	n := copy(p, m.buf)
	m.buf = m.buf[n:]
	return n, nil
}

func (*memSource) safe() bool { return true }

// lines is stdout: it keeps each message line.
type lines struct {
	mu   sync.Mutex
	cond *sync.Cond
	all  []string
	part string
}

func newLines() *lines {
	l := &lines{}
	l.cond = sync.NewCond(&l.mu)
	return l
}

func (l *lines) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.part += string(p)
	for {
		i := strings.IndexByte(l.part, '\n')
		if i < 0 {
			break
		}
		l.all = append(l.all, l.part[:i])
		l.part = l.part[i+1:]
	}
	l.cond.Broadcast()
	return len(p), nil
}

// waitFor returns the index and text of the first line that has sub, or
// fails after a few seconds.
func (l *lines) waitFor(t *testing.T, sub string) (int, string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	go func() {
		time.Sleep(10 * time.Second)
		l.mu.Lock()
		l.cond.Broadcast()
		l.mu.Unlock()
	}()
	l.mu.Lock()
	defer l.mu.Unlock()
	for {
		for i, s := range l.all {
			if strings.Contains(s, sub) {
				return i, s
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no line with %s in:\n%s", sub, strings.Join(l.all, "\n"))
		}
		l.cond.Wait()
	}
}

func (l *lines) text() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.all, "\n")
}

func rpcCall(id int, tool string, args string) string {
	return `{"jsonrpc":"2.0","id":` + itoa(id) + `,"method":"tools/call","params":{"name":"` + tool + `","arguments":` + args + `}}` + "\n"
}

func itoa(i int) string { b, _ := json.Marshal(i); return string(b) }

const initLine = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"resume-client","version":"1"}}}` + "\n" +
	`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" +
	`{"jsonrpc":"2.0","id":2,"method":"logging/setLevel","params":{"level":"info"}}` + "\n"

// fakeBinary writes a binary file and returns its path and a func that
// replaces it by rename, as an install does.
func fakeBinary(t *testing.T) (string, func()) {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "para")
	if err := os.WriteFile(bin, []byte("v1"), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, func() {
		tmp := bin + ".new"
		if err := os.WriteFile(tmp, []byte("v2, a longer binary"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(tmp, bin); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGateGivesOneLinePerRead(t *testing.T) {
	src := newMemSource()
	g := newGate(src, time.Second)
	go g.pump()
	src.write("{\"a\":1}\n{\"b\":2}\npart")
	p := make([]byte, 4096)
	for _, want := range []string{"{\"a\":1}\n", "{\"b\":2}\n"} {
		n, err := g.Read(p)
		if err != nil || string(p[:n]) != want {
			t.Fatalf("read = %q, %v; want %q", p[:n], err, want)
		}
	}
	// A partial line goes in as it comes; the gate is then mid-line.
	n, err := g.Read(p)
	if err != nil || string(p[:n]) != "part" {
		t.Fatalf("read = %q, %v", p[:n], err)
	}
	g.mu.Lock()
	if len(g.line) == 0 || len(g.pending) != 0 {
		t.Errorf("line %q pending %q: want mid-line, nothing pending", g.line, g.pending)
	}
	g.mu.Unlock()
}

// TestReexecHandsOverAndResumes drives a stdio server on one pipe: the
// client initializes and calls; the binary is replaced; the next call is
// held, handed over as leftover bytes with the session, and answered by the
// new server, which never sees the client's initialize.
func TestReexecHandsOverAndResumes(t *testing.T) {
	s, _ := newVault(t)
	bin, install := fakeBinary(t)
	handed := make(chan []byte, 1)
	old, err := New(ops.Deps{Store: s}, Options{Version: "v1", Executable: bin, Reexec: &Reexec{
		Probe: func() error { return nil },
		Exec: func(h Handoff) error {
			b, err := EncodeHandoff(h)
			if err != nil {
				return err
			}
			handed <- b
			return ErrReplaced
		},
		Every:   time.Hour, // only a new line wakes the exec here
		HoldMax: 5 * time.Second,
	}})
	if err != nil {
		t.Fatal(err)
	}
	src, out := newMemSource(), newLines()
	ctx := t.Context()
	oldDone := make(chan error, 1)
	go func() { oldDone <- old.serveStdio(ctx, src, out) }()

	src.write(initLine)
	src.write(rpcCall(3, "lint", "{}"))
	out.waitFor(t, `"id":3`)

	install()
	src.write(rpcCall(4, "lint", "{}"))
	var raw []byte
	select {
	case raw = <-handed:
	case <-time.After(10 * time.Second):
		t.Fatalf("no exec; out:\n%s", out.text())
	}
	select {
	case err := <-oldDone:
		if err != nil {
			t.Fatalf("old serve: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the old server did not stop")
	}
	h, err := DecodeHandoff(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(h.Leftover), `"id":4`) {
		t.Errorf("leftover = %q: want the held call", h.Leftover)
	}
	if strings.Contains(out.text(), `"id":4`) {
		t.Errorf("the old server answered the held call:\n%s", out.text())
	}
	if !strings.Contains(string(h.Init), "resume-client") || !h.Initialized || h.LogLevel != "info" || h.Protocol == "" || h.From != "v1" {
		t.Errorf("handoff = %+v", h)
	}

	// The new process: same pipe, no initialize from the client.
	resumed, err := New(ops.Deps{Store: s}, Options{Version: "v2", Resume: &h})
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = resumed.serveStdio(ctx, src, out) }()
	i4, l4 := out.waitFor(t, `"id":4`)
	if strings.Contains(l4, "reconnect") || !strings.Contains(l4, "para upgraded v1 → v2: call changes") {
		t.Errorf("answer 4 = %s: want served, with the upgrade note", l4)
	}
	il, _ := out.waitFor(t, "notifications/tools/list_changed")
	if il > i4 {
		t.Errorf("list_changed (line %d) came after the answer to the leftover call (line %d)", il, i4)
	}
	out.waitFor(t, "para upgraded v1 → v2: call changes")
	if strings.Contains(out.text(), resumeID) {
		t.Errorf("an answer to the replayed initialize went out:\n%s", out.text())
	}
	src.write(rpcCall(5, "lint", "{}"))
	_, l5 := out.waitFor(t, `"id":5`)
	if strings.Contains(l5, "reconnect") || strings.Contains(l5, `"isError":true`) {
		t.Errorf("answer 5 = %s: want served", l5)
	}
	src.close()
}

// TestReexecFallsBack: a new binary that refuses the probe, or an exec that
// fails, leaves the old server serving with the reconnect message.
func TestReexecFallsBack(t *testing.T) {
	for _, tc := range []struct {
		name  string
		probe error
		exec  error
	}{
		{"probe refused", errors.New("unknown flag --handoff-probe"), nil},
		{"exec failed", nil, errors.New("exec format error")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newVault(t)
			bin, install := fakeBinary(t)
			var execs atomic.Int32
			srv, err := New(ops.Deps{Store: s}, Options{Version: "v1", Executable: bin, Reexec: &Reexec{
				Probe:   func() error { return tc.probe },
				Exec:    func(Handoff) error { execs.Add(1); return tc.exec },
				Every:   time.Hour,
				HoldMax: 5 * time.Second,
			}})
			if err != nil {
				t.Fatal(err)
			}
			src, out := newMemSource(), newLines()
			go func() { _ = srv.serveStdio(t.Context(), src, out) }()
			src.write(initLine)
			out.waitFor(t, `"id":2`)
			install()
			start := time.Now()
			src.write(rpcCall(3, "lint", "{}"))
			_, l3 := out.waitFor(t, `"id":3`)
			if !strings.Contains(l3, "reconnect") {
				t.Errorf("answer = %s: want the reconnect message", l3)
			}
			if d := time.Since(start); d > 4*time.Second {
				t.Errorf("the call waited %s: a refused handover must not wait for the hold", d)
			}
			if tc.exec != nil && execs.Load() != 1 {
				t.Errorf("execs = %d, want 1", execs.Load())
			}
			src.close()
		})
	}
}

func TestHandoffEncodeDecode(t *testing.T) {
	h := Handoff{From: "v1", Init: json.RawMessage(`{"protocolVersion":"2025-06-18"}`), Protocol: "2025-06-18", Initialized: true,
		LogLevel: "info", Tools: "core", Args: []string{"para", "mcp", "--vault", "v"}, Leftover: []byte("{\"id\":4}\n{\"id\"")}
	b, err := EncodeHandoff(h)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DecodeHandoff(b)
	if err != nil {
		t.Fatal(err)
	}
	h.V = HandoffVersion
	if string(got.Leftover) != string(h.Leftover) || string(got.Init) != string(h.Init) || got.From != "v1" || !got.Initialized || len(got.Args) != 4 {
		t.Errorf("round trip = %+v", got)
	}
	if _, err := DecodeHandoff([]byte(`{"v":99}`)); err == nil {
		t.Error("another version: want an error")
	}
	if _, err := DecodeHandoff([]byte(`not json`)); err == nil {
		t.Error("not JSON: want an error")
	}
}

func TestTakeHandoff(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "run")
	args := []string{"para", "mcp", "--vault", "v"}
	h := Handoff{From: "v1", Args: args, Leftover: []byte("x\n")}

	t.Setenv(HandoffEnv, "")
	if got, err := TakeHandoff(dir, args); got != nil || err != nil {
		t.Fatalf("none named: %v, %v", got, err)
	}

	path, err := writeHandoff(dir, h)
	if err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("handoff file mode: %v, %v", info, err)
	}
	t.Setenv(HandoffEnv, path)
	got, err := TakeHandoff(dir, args)
	if err != nil || got == nil || string(got.Leftover) != "x\n" {
		t.Fatalf("take = %+v, %v", got, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("the handoff file was not removed")
	}
	if os.Getenv(HandoffEnv) != "" {
		t.Error("the env var was not cleared")
	}

	// Other argv, a file outside dir, a file others can read: refused.
	path, _ = writeHandoff(dir, h)
	t.Setenv(HandoffEnv, path)
	if _, err := TakeHandoff(dir, []string{"para", "mcp", "--vault", "other"}); err == nil {
		t.Error("other argv: want an error")
	}
	outside := filepath.Join(t.TempDir(), "mcp-handoff-x.json")
	b, _ := EncodeHandoff(h)
	_ = os.WriteFile(outside, b, 0o600)
	t.Setenv(HandoffEnv, outside)
	if _, err := TakeHandoff(dir, args); err == nil {
		t.Error("a file outside the run dir: want an error")
	}
	loose := filepath.Join(dir, "mcp-handoff-loose.json")
	_ = os.WriteFile(loose, b, 0o644)
	_ = os.Chmod(loose, 0o644)
	t.Setenv(HandoffEnv, loose)
	if _, err := TakeHandoff(dir, args); err == nil {
		t.Error("mode 644: want an error")
	}
}

// TestPendingInstallNoticeAndClaimHold: while an install is pending, every
// answer carries the notice and a claim of new agent work is refused.
func TestPendingInstallNoticeAndClaimHold(t *testing.T) {
	s, _ := newVault(t)
	notice, hold := "para upgrades to v9.9.9 when idle", error(errors.New("para upgrades to v9.9.9 when idle, so no new agent work is claimed until then"))
	var mu sync.Mutex
	srv, err := New(ops.Deps{Store: s}, Options{Hold: func() (string, error) {
		mu.Lock()
		defer mu.Unlock()
		return notice, hold
	}})
	if err != nil {
		t.Fatal(err)
	}
	c := connectServer(t, srv)
	if res := call(t, c, "lint", "{}"); !strings.Contains(allText(res), notice) {
		t.Errorf("lint answer has no notice:\n%s", allText(res))
	}
	for _, tc := range []struct{ tool, args string }{
		{"request_next", `{}`},
		{"work", `{"action":"next"}`},
	} {
		res := call(t, c, tc.tool, tc.args)
		if !res.IsError || !strings.Contains(allText(res), "no new agent work is claimed") || !strings.Contains(allText(res), notice) {
			t.Errorf("%s while pending = %v:\n%s", tc.tool, res.IsError, allText(res))
		}
	}
	mu.Lock()
	notice, hold = "", nil
	mu.Unlock()
	res := call(t, c, "request_next", `{}`)
	if res.IsError || strings.Contains(allText(res), "when idle") {
		t.Errorf("request_next after the hold = %v:\n%s", res.IsError, allText(res))
	}
}

func connectServer(t *testing.T, srv *Server) *client.Client {
	t.Helper()
	c, err := client.NewInProcessClient(srv.MCP())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	if err := c.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Initialize(context.Background(), mcp.InitializeRequest{}); err != nil {
		t.Fatal(err)
	}
	return c
}

// TestReexecWithAnOpenListen: a client of protocol 2026-07-28 (Claude Code)
// keeps a subscriptions/listen open for the whole session and never sends
// initialize. The listen is not a request in flight: the first call after a
// swap is held and answered by the new server, which opens the listen again
// with the client's id and tells it on that stream.
func TestReexecWithAnOpenListen(t *testing.T) {
	s, _ := newVault(t)
	bin, install := fakeBinary(t)
	handed := make(chan []byte, 1)
	old, err := New(ops.Deps{Store: s}, Options{Version: "v1", Executable: bin, Reexec: &Reexec{
		Probe: func() error { return nil },
		Exec: func(h Handoff) error {
			b, err := EncodeHandoff(h)
			if err != nil {
				return err
			}
			handed <- b
			return ErrReplaced
		},
		Every:   time.Hour,
		HoldMax: 8 * time.Second,
	}})
	if err != nil {
		t.Fatal(err)
	}
	src, out := newMemSource(), newLines()
	ctx := t.Context()
	go func() { _ = old.serveStdio(ctx, src, out) }()
	meta := `"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"modern","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}`
	src.write(`{"jsonrpc":"2.0","id":"L1","method":"subscriptions/listen","params":{"notifications":{"toolsListChanged":true},` + meta + `}}` + "\n")
	out.waitFor(t, "notifications/subscriptions/acknowledged")
	call := func(id int) string {
		return `{"jsonrpc":"2.0","id":` + itoa(id) + `,"method":"tools/call","params":{"name":"lint","arguments":{},` + meta + `}}` + "\n"
	}
	src.write(call(3))
	out.waitFor(t, `"id":3`)

	install()
	start := time.Now()
	src.write(call(4))
	var raw []byte
	select {
	case raw = <-handed:
	case <-time.After(6 * time.Second):
		t.Fatalf("no exec while the listen is open; out:\n%s", out.text())
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Errorf("the exec waited %s", d)
	}
	h, err := DecodeHandoff(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.Listens) != 1 || !strings.Contains(string(h.Leftover), `"id":4`) {
		t.Fatalf("handoff listens %s leftover %q", h.Listens, h.Leftover)
	}
	resumed, err := New(ops.Deps{Store: s}, Options{Version: "v2", Resume: &h})
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = resumed.serveStdio(ctx, src, out) }()
	_, l4 := out.waitFor(t, `"id":4`)
	if strings.Contains(l4, "reconnect") || !strings.Contains(l4, "para upgraded v1 → v2") {
		t.Errorf("answer 4 = %s: want the new server's answer with the upgrade note", l4)
	}
	_, lc := out.waitFor(t, "notifications/tools/list_changed")
	if !strings.Contains(lc, `"io.modelcontextprotocol/subscriptionId":"L1"`) {
		t.Errorf("list_changed = %s: want it on the listen stream L1", lc)
	}
	if n := strings.Count(out.text(), "notifications/subscriptions/acknowledged"); n != 1 {
		t.Errorf("acknowledgements = %d, want 1:\n%s", n, out.text())
	}
	src.close()
}
