package upgrade

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/atomicfile"
	"github.com/iodesystems/para/internal/store"
)

// OpSession is the event an install logs when a para opens the vault for
// writing: From is the binary's version, To its format (Latest). It is logged
// once a day per install, and again when either changes. A vault upgrade reads
// it to find installs that run an older para (Stale).
const OpSession = "session"

// StaleDays is how far back Stale looks for installs.
const StaleDays = 14

// sessionFile is this machine's record of the last session event it logged.
const sessionFile = "session.json"

type sessionMark struct {
	Version string `json:"version"`
	Format  int    `json:"format"`
	Day     string `json:"day"`
}

// Recorder is the part of a store that logs an event.
type Recorder interface {
	Record(ctx context.Context, op, doc, from, to, hash string) error
	Now() time.Time
}

// RecordSession logs a session event for this install, unless stateDir says
// one with the same version and format was logged today. An empty stateDir
// logs every time.
func RecordSession(ctx context.Context, r Recorder, stateDir, version string) error {
	mark := sessionMark{Version: version, Format: Latest(), Day: r.Now().Format("2006-01-02")}
	p := filepath.Join(stateDir, sessionFile)
	if stateDir != "" {
		var old sessionMark
		if b, err := os.ReadFile(p); err == nil && json.Unmarshal(b, &old) == nil && old == mark {
			return nil
		}
	}
	if err := r.Record(ctx, OpSession, "", version, strconv.Itoa(mark.Format), ""); err != nil {
		return fmt.Errorf("upgrade: record session: %w", err)
	}
	if stateDir == "" {
		return nil
	}
	b, _ := json.Marshal(mark)
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return fmt.Errorf("upgrade: record session: %w", err)
	}
	if err := atomicfile.Write(p, b, 0o600); err != nil {
		return fmt.Errorf("upgrade: record session: %w", err)
	}
	return nil
}

// StaleInstall is an install seen in StaleDays that runs a para older than the
// format a run needs. Format 0 and an empty Version mean it logged no session
// event: a para from before they were logged.
type StaleInstall struct {
	Install  string `json:"install"`
	LastSeen string `json:"last_seen"` // RFC 3339
	Version  string `json:"version,omitempty"`
	Format   int    `json:"format"`
}

// Stale lists the other installs seen in the last StaleDays whose latest
// session event names a format below want, or that logged none. This install
// runs this para, so it is never stale.
func Stale(ctx context.Context, s store.Store, want int) ([]StaleInstall, error) {
	self := s.Info().Install
	evs, err := s.Events(ctx, math.MaxInt32)
	if err != nil {
		return nil, fmt.Errorf("upgrade: stale: %w", err)
	}
	cutoff := s.Now().AddDate(0, 0, -StaleDays)
	seen := map[string]*StaleInstall{}
	for _, e := range evs {
		if e.Install == "" || e.Install == self || e.TS.Before(cutoff) {
			continue
		}
		in := seen[e.Install]
		if in == nil {
			in = &StaleInstall{Install: e.Install}
			seen[e.Install] = in
		}
		in.LastSeen = e.TS.UTC().Format(time.RFC3339)
		if e.Op == OpSession {
			in.Version = e.From
			in.Format, _ = strconv.Atoi(e.To)
		}
	}
	var out []StaleInstall
	for _, in := range seen {
		if in.Format < want {
			out = append(out, *in)
		}
	}
	slices.SortFunc(out, func(a, b StaleInstall) int { return strings.Compare(a.Install, b.Install) })
	return out, nil
}

func staleError(st []StaleInstall) error {
	var parts []string
	for _, s := range st {
		what := "a para that logs no version (older than this one)"
		if s.Format > 0 {
			what = fmt.Sprintf("para %s (format %d)", s.Version, s.Format)
		}
		parts = append(parts, fmt.Sprintf("install %s, seen %s, runs %s", s.Install, s.LastSeen, what))
	}
	return errors.New("upgrade: refused: " + strings.Join(parts, "; ") +
		". Upgrade para on that machine first (para upgrade), open the vault there once, then run this again; --force runs it anyway, and that machine refuses the vault until it is upgraded")
}
