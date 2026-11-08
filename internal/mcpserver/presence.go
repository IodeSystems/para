package mcpserver

import (
	"context"
	"os"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/iodesystems/para/internal/presence"
)

// presenceEvery is how often a tool call may rewrite the presence file.
const presenceEvery = 10 * time.Second

// presenceFile is this session's record in the vault's state directory on
// this machine (internal/presence). A nil presenceFile does nothing.
type presenceFile struct {
	dir   string
	warn  func(string)
	mu    sync.Mutex
	rec   presence.Record
	wrote time.Time
	live  bool
}

func newPresence(dir, actor string, channel bool, warn func(string)) *presenceFile {
	if dir == "" {
		return nil
	}
	push := []string{presence.PushMessage}
	if channel {
		push = append(push, presence.PushChannel)
	}
	return &presenceFile{dir: dir, warn: warn, rec: presence.Record{PID: os.Getpid(), Actor: actor, Push: push}}
}

// start writes the record. The session is connected from here to stop.
func (p *presenceFile) start() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.live = true
	p.rec.StartedAt = time.Now().UTC().Format(time.RFC3339)
	p.writeLocked(time.Now())
}

// stop removes the record.
func (p *presenceFile) stop() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.live {
		return
	}
	p.live = false
	if err := presence.Remove(p.dir, p.rec.PID); err != nil {
		p.warn(err.Error())
	}
}

// client records the client's name and version from initialize.
func (p *presenceFile) client(info mcp.Implementation) {
	if p == nil || info.Name == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.rec.Client == info.Name && p.rec.ClientVersion == info.Version {
		return
	}
	p.rec.Client, p.rec.ClientVersion = info.Name, info.Version
	if p.live {
		p.writeLocked(time.Now())
	}
}

// call records a tool call. The first call is written at once; later ones
// rewrite the file at most every presenceEvery.
func (p *presenceFile) call(ctx context.Context) {
	if p == nil {
		return
	}
	if s, ok := server.ClientSessionFromContext(ctx).(server.SessionWithClientInfo); ok {
		p.client(s.GetClientInfo())
	}
	now := time.Now()
	p.mu.Lock()
	defer p.mu.Unlock()
	first := p.rec.LastCallAt == ""
	p.rec.LastCallAt = now.UTC().Format(time.RFC3339)
	if p.live && (first || now.Sub(p.wrote) >= presenceEvery) {
		p.writeLocked(now)
	}
}

func (p *presenceFile) writeLocked(now time.Time) {
	if err := presence.Write(p.dir, p.rec); err != nil {
		p.warn(err.Error())
		return
	}
	p.wrote = now
}
