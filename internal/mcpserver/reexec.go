package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// A stdio para mcp whose binary was replaced execs the new binary on the
// same stdin and stdout, when no request is in flight, and hands it the
// session (Handoff). The new process replays the client's initialize to its
// own server, drops that answer, and sends notifications/tools/list_changed;
// the client sees one server that changed its tools.
//
// No byte from the client is lost. This package owns the stdin reader (gate):
//   - it reads stdin only under its lock, and only after poll said bytes
//     wait (exec_unix.go), so an exec under the lock never drops bytes the
//     kernel already gave this process;
//   - it gives mcp-go's bufio reader at most one line per Read, so that
//     buffer never holds a byte past the line mcp-go is handling;
//   - it execs only while mcp-go waits in Read (it handled every line before),
//     at a line boundary, and with every request it was given answered (the
//     answer written out: the exec also takes the write lock).
//
// What the gate read and did not give mcp-go goes in Handoff.Leftover, and
// the new process handles it first. When the new binary does not answer the
// probe, or the exec fails, the old server keeps serving as before: the stale
// check (stale.go) refuses what it cannot serve, with the reconnect message.

// Reexec configures the exec into a replacing binary.
type Reexec struct {
	// Dir is where the handoff file goes: $PARA_HOME/run, never a vault.
	Dir string
	// Probe asks the binary now on disk whether it takes the session: it
	// runs it with the same argv and --handoff-probe. Nil: none does.
	Probe func() error
	// Exec replaces this process with the binary now on disk, handing it h.
	// It returns only on failure. Nil is the real exec (exec_unix.go).
	Exec func(h Handoff) error
	// Every is how often an idle server looks at its binary; zero is 2s.
	Every time.Duration
	// HoldMax is how long a line that arrived after a replacement waits for
	// the exec before the old server takes it; zero is 10s.
	HoldMax time.Duration
	// Log receives a line on each decision. Nil discards.
	Log io.Writer
}

// errReplaced is what a test's Exec returns for an exec that happened: the
// old server stops reading and returns.
var errReplaced = errors.New("mcpserver: replaced")

// ErrReplaced is errReplaced, for a test outside this package.
var ErrReplaced = errReplaced

// source is stdin as the gate reads it.
type source interface {
	// wait blocks until read has bytes, or the end, to return.
	wait() error
	// read returns bytes that wait said are there.
	read(p []byte) (int, error)
	// safe reports that read never blocks after wait, so the gate reads
	// under its lock and an exec under that lock loses no byte. A source
	// that is not safe never execs.
	safe() bool
}

// readerSource is any reader: it blocks in read, so it never execs.
type readerSource struct{ r io.Reader }

func (readerSource) wait() error                  { return nil }
func (s readerSource) read(p []byte) (int, error) { return s.r.Read(p) }
func (readerSource) safe() bool                   { return false }

// stdinSource is the fd source of the process's stdin (exec_unix.go), or a
// readerSource for any other reader.
func stdinSource(in io.Reader) source {
	if f, ok := in.(*os.File); ok {
		if s := fdSource(f); s != nil {
			return s
		}
	}
	return readerSource{r: in}
}

// resumeID prefixes the ids of the requests a resumed process sends its own
// server; their answers are not written out.
const resumeID = "para-resume-"

// gate sits between stdin and mcp-go's stdio server.
type gate struct {
	mu   sync.Mutex
	cond *sync.Cond
	src  source

	pending []byte // read from stdin, not given to the server yet
	srcErr  error
	closed  bool   // the process was replaced (a test's exec): Read ends
	line    []byte // the line being given to the server, up to its '\n'
	waiting bool   // the server waits in Read: it handled every line before
	open    map[string]int
	// after is the id of a request whose answer must be out before the next
	// line goes in (the resumed initialize); barrier is it once given.
	after, barrier string
	drop           map[string]bool // ids whose answers are not written
	want           bool            // a replacing binary may take over: hold lines
	holdUntil      time.Time
	holdSpent      bool // a hold ran out: later lines go in at once
	holdMax        time.Duration
	candidate      func() bool // a replacement not yet tried is on disk
	kick           chan struct{}

	// The session, for the handoff.
	init        json.RawMessage
	initID      string
	protocol    string
	initialized bool
	logLevel    string
}

func newGate(src source, holdMax time.Duration) *gate {
	g := &gate{src: src, open: map[string]int{}, drop: map[string]bool{}, holdMax: holdMax, kick: make(chan struct{}, 1)}
	g.cond = sync.NewCond(&g.mu)
	return g
}

// poke wakes the upgrader without blocking.
func (g *gate) poke() {
	select {
	case g.kick <- struct{}{}:
	default:
	}
}

// pump reads stdin into pending until it ends or the gate closes.
func (g *gate) pump() {
	buf := make([]byte, 32<<10)
	for {
		err := g.src.wait()
		var n int
		if err == nil {
			if g.src.safe() {
				g.mu.Lock()
				if g.closed {
					g.mu.Unlock()
					return
				}
				n, err = g.src.read(buf)
			} else {
				n, err = g.src.read(buf)
				g.mu.Lock()
			}
		} else {
			g.mu.Lock()
		}
		if g.closed {
			g.mu.Unlock()
			return
		}
		g.pending = append(g.pending, buf[:n]...)
		if err != nil {
			g.srcErr = err
		}
		g.cond.Broadcast()
		g.mu.Unlock()
		if n > 0 {
			g.poke()
		}
		if err != nil {
			return
		}
	}
}

// Read gives the server at most one line, up to its '\n', per call.
func (g *gate) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	for {
		if g.closed {
			return 0, io.EOF
		}
		if len(g.pending) > 0 && g.barrier == "" {
			if len(g.line) == 0 && g.hold() {
				g.waiting = true
				g.poke()
				g.cond.Wait()
				continue
			}
			g.waiting, g.holdUntil = false, time.Time{}
			n := len(g.pending)
			if i := bytes.IndexByte(g.pending, '\n'); i >= 0 {
				n = i + 1
			}
			n = copy(p, g.pending[:n])
			g.pending = g.pending[n:]
			g.line = append(g.line, p[:n]...)
			if p[n-1] == '\n' {
				g.given(g.line)
				g.line = g.line[:0]
			}
			return n, nil
		}
		if len(g.pending) == 0 && g.srcErr != nil {
			if len(g.line) > 0 {
				g.given(g.line)
				g.line = g.line[:0]
			}
			return 0, g.srcErr
		}
		g.waiting = true
		g.poke()
		g.cond.Wait()
	}
}

// hold reports whether a new line waits for an exec. It holds a line at
// most holdMax, then the old server takes it. Called under mu.
func (g *gate) hold() bool {
	if g.holdSpent {
		return false
	}
	if !g.want && g.candidate != nil && g.candidate() {
		g.want = true
	}
	if !g.want {
		return false
	}
	now := time.Now()
	if g.holdUntil.IsZero() {
		g.holdUntil = now.Add(g.holdMax)
		time.AfterFunc(g.holdMax, func() {
			g.mu.Lock()
			g.cond.Broadcast()
			g.mu.Unlock()
		})
	}
	if now.Before(g.holdUntil) {
		return true
	}
	// The exec waits for a quiet moment still; lines no longer wait for it.
	g.holdSpent = true
	return false
}

func (g *gate) setWant(v bool) {
	g.mu.Lock()
	g.want = v
	g.cond.Broadcast()
	g.mu.Unlock()
}

// idKey is a JSON-RPC id in a form both directions share; "" for none.
func idKey(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var b bytes.Buffer
	if json.Compact(&b, raw) != nil {
		return string(raw)
	}
	return b.String()
}

// given records a line the server got. Called under mu.
func (g *gate) given(line []byte) {
	var m struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if json.Unmarshal(line, &m) != nil {
		return
	}
	id := idKey(m.ID)
	switch {
	case m.Method != "" && id != "":
		g.open[id]++
		switch m.Method {
		case string(mcp.MethodInitialize):
			g.init, g.initID = append(json.RawMessage(nil), m.Params...), id
		case string(mcp.MethodSetLogLevel):
			var p struct {
				Level string `json:"level"`
			}
			if json.Unmarshal(m.Params, &p) == nil {
				g.logLevel = p.Level
			}
		}
		if id == g.after {
			g.barrier, g.after = id, ""
		}
	case m.Method == string(mcp.MethodNotificationInitialized):
		g.initialized = true
	}
}

// answered records an answer the server wrote out. Called under mu.
func (g *gate) answered(id string, msg []byte) {
	if g.open[id] > 1 {
		g.open[id]--
	} else {
		delete(g.open, id)
	}
	if id == g.initID {
		var r struct {
			Result struct {
				ProtocolVersion string `json:"protocolVersion"`
			} `json:"result"`
		}
		if json.Unmarshal(msg, &r) == nil {
			g.protocol = r.Result.ProtocolVersion
		}
	}
	if id == g.barrier {
		g.barrier = ""
	}
	g.cond.Broadcast()
	g.poke()
}

// quiet reports whether an exec now loses nothing: the server waits in Read
// at a line boundary and every request it got is answered. Called under mu.
func (g *gate) quiet() bool {
	return g.waiting && len(g.line) == 0 && len(g.open) == 0 && g.barrier == "" && g.srcErr == nil && !g.closed
}

// outWriter is the server's stdout. Each Write is one message (mcp-go
// writes a message and its '\n' in one call). An exec takes mu, so no
// message is cut.
type outWriter struct {
	mu sync.Mutex
	g  *gate
	w  io.Writer
	// dropped runs, under mu, after the answer to a resume request was
	// dropped; it may write to w.
	dropped func(id string, msg []byte)
}

func (o *outWriter) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	var m struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Result json.RawMessage `json:"result"`
		Error  json.RawMessage `json:"error"`
	}
	id, answer := "", false
	if json.Unmarshal(p, &m) == nil && m.Method == "" {
		id = idKey(m.ID)
		answer = id != "" && (m.Result != nil || m.Error != nil)
	}
	o.g.mu.Lock()
	drop := answer && o.g.drop[id]
	o.g.mu.Unlock()
	var err error
	if !drop {
		_, err = o.w.Write(p)
	}
	if answer {
		o.g.mu.Lock()
		o.g.answered(id, p)
		o.g.mu.Unlock()
	}
	if drop && o.dropped != nil {
		o.dropped(id, p)
	}
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

// serveStdio serves the session on src and out through the gate.
func (s *Server) serveStdio(ctx context.Context, src source, out io.Writer) error {
	r := s.reexec
	holdMax := 10 * time.Second
	if r != nil && r.HoldMax > 0 {
		holdMax = r.HoldMax
	}
	g := newGate(src, holdMax)
	o := &outWriter{g: g, w: out}
	if h := s.resume; h != nil {
		s.resumeInto(g, o, h)
	}
	var up *upgrader
	if r != nil && src.safe() && s.upgraded != nil && s.upgraded.binary != nil {
		up = &upgrader{s: s, r: r, g: g, o: o, stamp: s.upgraded.binary}
		g.candidate = up.candidate
	}
	go g.pump()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if up != nil {
		go up.run(ctx)
	}
	return server.NewStdioServer(s.mcp).Listen(ctx, g, o)
}

// resumeInto queues the replay of h's session ahead of its leftover bytes:
// initialize (its answer dropped, and nothing else goes in before it),
// notifications/initialized, and the log level. After the initialize answer
// the client gets notifications/tools/list_changed and the upgrade line.
func (s *Server) resumeInto(g *gate, o *outWriter, h *Handoff) {
	var pre bytes.Buffer
	line := func(v any) {
		b, _ := json.Marshal(v)
		pre.Write(b)
		pre.WriteByte('\n')
	}
	initID := ""
	if len(h.Init) > 0 {
		initID = resumeID + "init"
		line(map[string]any{"jsonrpc": mcp.JSONRPC_VERSION, "id": initID, "method": mcp.MethodInitialize, "params": h.Init})
		if h.Initialized {
			line(map[string]any{"jsonrpc": mcp.JSONRPC_VERSION, "method": mcp.MethodNotificationInitialized})
		}
		if h.LogLevel != "" {
			line(map[string]any{"jsonrpc": mcp.JSONRPC_VERSION, "id": resumeID + "log", "method": mcp.MethodSetLogLevel, "params": map[string]string{"level": h.LogLevel}})
		}
	}
	g.pending = append(pre.Bytes(), h.Leftover...)
	key := func(id string) string { b, _ := json.Marshal(id); return string(b) }
	if initID != "" {
		g.after = key(initID)
		g.drop[key(initID)] = true
		g.drop[key(resumeID+"log")] = true
	}
	o.dropped = func(id string, msg []byte) {
		if id != key(initID) {
			return
		}
		var r struct {
			Result struct {
				ProtocolVersion string `json:"protocolVersion"`
			} `json:"result"`
		}
		if json.Unmarshal(msg, &r) == nil && h.Protocol != "" && r.Result.ProtocolVersion != h.Protocol {
			s.warn(fmt.Sprintf("resumed session: protocol %s, the old server had %s", r.Result.ProtocolVersion, h.Protocol))
		}
		if h.Initialized {
			for _, n := range []map[string]any{
				{"jsonrpc": mcp.JSONRPC_VERSION, "method": mcp.MethodNotificationToolsListChanged},
				{"jsonrpc": mcp.JSONRPC_VERSION, "method": "notifications/message", "params": map[string]any{"level": "info", "logger": notifyLogger, "data": UpgradedLine(h.From, s.version)}},
			} {
				b, _ := json.Marshal(n)
				_, _ = o.w.Write(append(b, '\n'))
			}
		}
	}
	s.warn("resumed the session of para " + h.From)
}

// upgrader watches the binary and execs into its replacement.
type upgrader struct {
	s     *Server
	r     *Reexec
	g     *gate
	o     *outWriter
	stamp *binaryStamp

	mu    sync.Mutex
	tried os.FileInfo // the replacement probed, refused, or failed: not again
}

// candidate reports whether a replacement not yet tried is on disk.
func (u *upgrader) candidate() bool {
	cur, err := os.Stat(u.stamp.path)
	if err != nil || !u.stamp.differs(cur) {
		return false
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.tried == nil || !os.SameFile(u.tried, cur) || cur.ModTime() != u.tried.ModTime()
}

func (u *upgrader) logf(format string, a ...any) {
	if u.r.Log != nil {
		_, _ = fmt.Fprintf(u.r.Log, "para mcp: "+format+"\n", a...)
	}
}

func (u *upgrader) run(ctx context.Context) {
	every := u.r.Every
	if every <= 0 {
		every = 2 * time.Second
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-u.g.kick:
		}
		if !u.candidate() {
			continue
		}
		cur, _ := os.Stat(u.stamp.path)
		u.g.setWant(true)
		if err := u.probe(); err != nil {
			u.logf("the new binary does not take the session (%v); this server keeps serving", err)
			u.fail(cur)
			continue
		}
		if done := u.execWhenQuiet(ctx, cur, t.C); done {
			return
		}
	}
}

func (u *upgrader) probe() error {
	if u.r.Probe == nil {
		return errors.New("no probe")
	}
	return u.r.Probe()
}

func (u *upgrader) fail(cur os.FileInfo) {
	u.mu.Lock()
	u.tried = cur
	u.mu.Unlock()
	u.g.setWant(false)
}

// execWhenQuiet execs at the first quiet moment. It returns true when the
// process was replaced (a test's exec), false when the exec failed or ctx
// ended.
func (u *upgrader) execWhenQuiet(ctx context.Context, cur os.FileInfo, tick <-chan time.Time) bool {
	for {
		done, err := u.tryExec()
		if done {
			return true
		}
		if err != nil {
			u.logf("exec of the new binary failed (%v); this server keeps serving", err)
			u.fail(cur)
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-u.g.kick:
		case <-tick:
		}
	}
}

// tryExec execs when the session is quiet. It holds the write lock and the
// gate's lock across the exec, so no byte comes in or goes out meanwhile.
func (u *upgrader) tryExec() (done bool, err error) {
	u.o.mu.Lock()
	defer u.o.mu.Unlock()
	g := u.g
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.quiet() {
		return false, nil
	}
	h := Handoff{
		V: HandoffVersion, From: u.s.version, Init: g.init, Protocol: g.protocol,
		Initialized: g.initialized, LogLevel: g.logLevel, Tools: u.s.toolsMode, Args: os.Args,
		Leftover: append([]byte(nil), g.pending...),
	}
	ex := u.r.Exec
	if ex == nil {
		ex = func(h Handoff) error { return execBinary(u.r.Dir, u.stamp.path, h) }
	}
	u.logf("exec %s: the binary was replaced; handing over the session", u.stamp.path)
	err = ex(h)
	if errors.Is(err, errReplaced) {
		g.closed = true
		g.cond.Broadcast()
		return true, nil
	}
	if err == nil {
		err = errors.New("exec returned")
	}
	return false, err
}
