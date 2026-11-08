package mcpserver

import (
	"context"
	"encoding/json"
	"reflect"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/iodesystems/para/internal/events"
	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
)

const requestWaitTool = "request_wait"

// RequestWaitInput is the input of request_wait.
type RequestWaitInput struct {
	TimeoutSeconds int `json:"timeout_seconds,omitempty"` // default 15, at most 60
}

// RequestWaitOutput says whether this install can claim a request. It claims
// nothing: call request_next when Claimable is true.
type RequestWaitOutput struct {
	Claimable bool `json:"claimable"`
}

// change is a broadcast to every waiter: Wait takes the channel before it reads
// state, and bump closes it after the vault changed.
type change struct {
	mu sync.Mutex
	ch chan struct{}
}

func (c *change) wait() <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ch == nil {
		c.ch = make(chan struct{})
	}
	return c.ch
}

func (c *change) bump() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ch != nil {
		close(c.ch)
		c.ch = nil
	}
}

// Start runs one events.Watch over the vault for the life of the session,
// and writes the session's presence file (Options.PresenceDir). Without
// Options.EventsDir there is no watcher. The returned func stops the watcher,
// waits for its goroutine, and removes the presence file; it is safe to call
// more than once. Serve calls
// Start, so only an in-process client calls it directly.
func (s *Server) Start(ctx context.Context) (stop func()) {
	s.presence.start()
	if s.eventsDir == "" {
		return s.presence.stop
	}
	s.runCtx.Store(&ctx)
	stopWatch := events.Watch(ctx, s.eventsDir, s.poll, s.onBatch, s.warn)
	// Note what is claimable now, so a start announces nothing.
	s.check(ctx)
	tctx, cancel := context.WithCancel(ctx)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		t := time.NewTicker(fallbackCheck)
		defer t.Stop()
		for {
			select {
			case <-tctx.Done():
				return
			case <-t.C:
				s.check(tctx)
			}
		}
	}()
	var once sync.Once
	return func() {
		once.Do(func() {
			cancel()
			wg.Wait()
			stopWatch()
			s.presence.stop()
		})
	}
}

// warn reports a watcher note to the operator log.
func (s *Server) warn(m string) {
	if s.log != nil {
		_, _ = s.log.Write([]byte("para mcp: " + m + "\n"))
	}
}

func (s *Server) onBatch([]model.Event) {
	s.changed.bump()
	if p := s.runCtx.Load(); p != nil {
		s.check(*p)
	}
}

// addRequestWait registers request_wait. It is not an op: it blocks on the
// session watcher, which only this surface has. The CLI equivalent is
// `para request wait`.
func (s *Server) addRequestWait() error {
	raw, err := inputSchema(reflect.TypeFor[RequestWaitInput]())
	if err != nil {
		return err
	}
	desc := "Wait a short time until this install can claim a request; returns claimable true as soon as one can be, else false. Claims nothing: call request_next when claimable"
	s.mcp.AddTool(mcp.NewToolWithRawSchema(requestWaitTool, desc, raw), s.requestWait)
	return nil
}

func (s *Server) requestWait(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	s.presence.call(ctx)
	var in RequestWaitInput
	if args := req.GetRawArguments(); args != nil {
		b, err := json.Marshal(args)
		if err == nil {
			err = json.Unmarshal(b, &in)
		}
		if err != nil {
			return mcp.NewToolResultError(requestWaitTool + ": input: " + err.Error()), nil
		}
	}
	limit := ops.DefaultWait
	switch {
	case in.TimeoutSeconds < 0:
		return mcp.NewToolResultError(requestWaitTool + ": timeout_seconds is negative"), nil
	case in.TimeoutSeconds > 0:
		limit = time.Duration(in.TimeoutSeconds) * time.Second
	}
	if limit > ops.MaxWait {
		return mcp.NewToolResultError(requestWaitTool + ": timeout_seconds is over 60"), nil
	}
	out, err := s.waitClaimable(ctx, limit)
	if err != nil {
		return mcp.NewToolResultError(s.clean(err)), nil
	}
	text, err := encode(out)
	if err != nil {
		return mcp.NewToolResultError(requestWaitTool + ": output: encode failed"), nil
	}
	return mcp.NewToolResultText(text), nil
}

// waitClaimable blocks until a request is claimable, the limit passes, or ctx
// ends. With no watcher it checks once a second.
func (s *Server) waitClaimable(ctx context.Context, limit time.Duration) (RequestWaitOutput, error) {
	deadline := time.After(limit)
	var tick <-chan time.Time
	if s.eventsDir == "" {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		tick = t.C
	}
	for {
		changed := s.changed.wait()
		s.reqMu.Lock()
		ok, err := s.reqWatch.Claimable(ctx)
		s.reqMu.Unlock()
		if err != nil || ok {
			return RequestWaitOutput{Claimable: ok}, err
		}
		select {
		case <-ctx.Done():
			return RequestWaitOutput{}, ctx.Err()
		case <-deadline:
			return RequestWaitOutput{}, nil
		case <-tick:
		case <-changed:
		}
	}
}
