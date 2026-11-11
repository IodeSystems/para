package mcpserver

import (
	"context"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// Upgrades seen by a session, beyond the stale check of a stdio server
// (stale.go):
//   - Over HTTP (para serve), a service restart on a new binary ends every
//     session; the client reconnects to the same URL and gets the new tools.
//     A session that starts on a binary other than the one this key's last
//     session ran gets, on its first tool call, notifications/tools/list_changed
//     and a log message "para upgraded <old> → <new>: call changes", and the
//     same line as a note on that answer.
//   - On any transport, when a newer release is available (Options.Notice),
//     the instructions say so, and each session gets the line once per
//     version as a note on an answer. A running para serve also sends it to
//     every live session when its check finds the release (Announce).

// upgradedFor is how long after the server started a new session is still
// told about the upgrade: a session that starts days later is not news.
const upgradedFor = 24 * time.Hour

// noticeTTL is how long a Notice answer is reused.
const noticeTTL = time.Minute

// sessionNotes says each upgrade line once per session.
type sessionNotes struct {
	mu       sync.Mutex
	upgraded map[string]bool   // sessions told about the upgrade
	notice   map[string]string // session -> the notice it was told
	cached   string
	at       time.Time
}

func newSessionNotes() *sessionNotes {
	return &sessionNotes{upgraded: map[string]bool{}, notice: map[string]string{}}
}

// ForgetSession drops what this server keeps for a session that ended
// (DELETE, idle expiry, or eviction), so the maps do not grow.
func (s *Server) ForgetSession(id string) {
	s.sess.mu.Lock()
	delete(s.sess.upgraded, id)
	delete(s.sess.notice, id)
	s.sess.mu.Unlock()
}

func sessionID(ctx context.Context) string {
	if cs := server.ClientSessionFromContext(ctx); cs != nil {
		return cs.SessionID()
	}
	return ""
}

// noticeNow is Options.Notice, read at most once per noticeTTL.
func (s *Server) noticeNow() string {
	if s.notice == nil {
		return ""
	}
	n := s.sess
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.at.IsZero() || time.Since(n.at) > noticeTTL {
		n.cached, n.at = clean(s.notice()), time.Now()
	}
	return n.cached
}

// upgradeNotes returns the lines this session has not been told yet, and
// sends the upgrade's notifications to it.
func (s *Server) upgradeNotes(ctx context.Context) []string {
	id := sessionID(ctx)
	var out []string
	if s.upgradedFrom != "" && time.Since(s.started) < upgradedFor {
		s.sess.mu.Lock()
		first := !s.sess.upgraded[id]
		s.sess.upgraded[id] = true
		s.sess.mu.Unlock()
		if first {
			line := UpgradedLine(s.upgradedFrom, s.version)
			_ = s.mcp.SendNotificationToClient(ctx, string(mcp.MethodNotificationToolsListChanged), nil)
			_ = s.mcp.SendNotificationToClient(ctx, "notifications/message", map[string]any{"level": "info", "logger": notifyLogger, "data": line})
			out = append(out, "note: "+line+". The tool list may have changed: list the tools again.")
		}
	}
	if n := s.noticeNow(); n != "" {
		s.sess.mu.Lock()
		first := s.sess.notice[id] != n
		s.sess.notice[id] = n
		s.sess.mu.Unlock()
		if first {
			out = append(out, "note: "+n+". "+UpgradeHowTo)
		}
	}
	return out
}

// UpgradedLine is the line a session gets after para was upgraded under it.
func UpgradedLine(from, to string) string {
	return "para upgraded " + from + " → " + to + ": call changes"
}

// UpgradeHowTo says what an agent does about an available release.
const UpgradeHowTo = "Tell the owner; do not upgrade para yourself. The owner runs para upgrade (or the para service installs it at a quiet moment), then call changes; pending vault steps arrive as agent work titled \"Vault upgrade: ...\"."

// Announce sends text to every live session as a log message. It holds no
// vault text: para serve uses it for the update notice.
func (s *Server) Announce(text string) {
	if text = clean(text); text != "" {
		s.send(note{text: text, meta: map[string]string{"kind": "para_update"}})
	}
}
