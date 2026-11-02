package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/iodesystems/paradize/internal/atomicfile"
	"github.com/iodesystems/paradize/internal/model"
)

// Position is this machine's review position for one vault: events at or
// before At are reviewed. Previous is the position before the last accept,
// so a reopen can undo it. It is stored in the vault's state dir, which is
// per machine and never synced.
type Position struct {
	At       time.Time `json:"at"`
	Previous time.Time `json:"previous"`
}

const positionFile = "review.json"

// LoadPosition reads the position; a missing file is the zero position, which
// reviews everything.
func LoadPosition(stateDir string) (Position, error) {
	b, err := os.ReadFile(filepath.Join(stateDir, positionFile))
	if errors.Is(err, fs.ErrNotExist) {
		return Position{}, nil
	}
	if err != nil {
		return Position{}, fmt.Errorf("tui: review position: %w", err)
	}
	var p Position
	if err := json.Unmarshal(b, &p); err != nil {
		return Position{}, fmt.Errorf("tui: review position: %w", err)
	}
	return p, nil
}

// SavePosition writes the position.
func SavePosition(stateDir string, p Position) error {
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return fmt.Errorf("tui: review position: %w", err)
	}
	b, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("tui: review position: %w", err)
	}
	if err := atomicfile.Write(filepath.Join(stateDir, positionFile), stateDir, b, 0o600); err != nil {
		return fmt.Errorf("tui: review position: %w", err)
	}
	return nil
}

// Accept moves the position to at.
func (p Position) Accept(at time.Time) Position {
	if !at.After(p.At) {
		return p
	}
	return Position{At: at, Previous: p.At}
}

// Reopen undoes the last accept.
func (p Position) Reopen() Position {
	return Position{At: p.Previous, Previous: p.Previous}
}

// Need kinds: things that wait for a person before anything else.
const (
	NeedDraft      = "draft RFX"
	NeedUnverified = "unverified summary"
	NeedProposed   = "proposed request"
)

// Need is an item that needs the owner.
type Need struct {
	Kind  string
	ID    string
	Title string
}

// Group is the events of one actor.
type Group struct {
	Actor  string
	Events []model.Event
}

// ReviewData is the Review screen: needs first, then what others changed
// since the position, grouped by actor.
type ReviewData struct {
	Needs  []Need
	Groups []Group
	Count  int
}

// ownSurface reports whether an event is the owner's own action. The owner's
// CLI and TUI work is not review material.
func ownSurface(e model.Event) bool { return e.Surface == "cli" || e.Surface == Surface }

// BuildReview derives the Review screen from a snapshot and a position.
func BuildReview(s Snapshot, pos Position) ReviewData {
	var r ReviewData
	for _, x := range s.RFX {
		if _, ok := s.Drafts[x.ID]; ok {
			r.Needs = append(r.Needs, Need{NeedDraft, x.ID, x.Title})
		}
	}
	for _, p := range s.Policies {
		if !p.Verified {
			r.Needs = append(r.Needs, Need{NeedUnverified, p.ID, p.Title})
		}
	}
	for _, q := range s.Requests {
		if q.Status == model.RequestProposed {
			r.Needs = append(r.Needs, Need{NeedProposed, q.ID, q.Title})
		}
	}
	by := map[string][]model.Event{}
	for _, e := range s.Events {
		if !e.TS.After(pos.At) || ownSurface(e) {
			continue
		}
		by[e.Actor] = append(by[e.Actor], e)
		r.Count++
	}
	for actor, evs := range by {
		sort.SliceStable(evs, func(i, j int) bool { return evs[i].TS.Before(evs[j].TS) })
		r.Groups = append(r.Groups, Group{Actor: actor, Events: evs})
	}
	sort.Slice(r.Groups, func(i, j int) bool { return r.Groups[i].Actor < r.Groups[j].Actor })
	return r
}
