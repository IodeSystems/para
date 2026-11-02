package dispatch

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/iodesystems/parasolve/internal/atomicfile"
)

// StateFile is the dispatch state in the vault's state directory. It holds the
// day's count, per-request attempts, and the runs in flight, so the limits hold
// across a restart of the server. It holds ids, times, and exit statuses only.
const StateFile = "agent-state.json"

// maxRunAge is how long a recorded run counts as in flight. A pid can be reused
// after a crash; an entry this old is dropped.
const maxRunAge = 24 * time.Hour

// keepLast is how many finished runs the state remembers, for para agent status.
const keepLast = 10

// Run is a command in flight.
type Run struct {
	ID      string    `json:"id"`
	PID     int       `json:"pid"`
	Started time.Time `json:"started"`
	Log     string    `json:"log"` // file name under agent/runs
}

// Exit is a command that ended. Status is exit:<n>, signal:<name>,
// start_failed, or wait_failed.
type Exit struct {
	ID     string    `json:"id"`
	Ended  time.Time `json:"ended"`
	Status string    `json:"status"`
	Log    string    `json:"log,omitempty"`
}

// State is the content of StateFile.
type State struct {
	Day      string         `json:"day"`      // local date the counts belong to
	Started  int            `json:"started"`  // runs started on Day
	Attempts map[string]int `json:"attempts"` // request id to starts on Day
	Running  []Run          `json:"running"`
	Last     []Exit         `json:"last"` // newest first
}

// StatePath is the state file of a vault whose state directory is stateDir.
func StatePath(stateDir string) string { return filepath.Join(stateDir, StateFile) }

// LoadState reads the state file. A missing file is an empty state. A file that
// does not parse is an error: the count is lost, and a dispatcher that guessed
// could spend past the limit, so it starts nothing until the file is fixed or
// removed.
func LoadState(stateDir string) (State, error) {
	data, err := os.ReadFile(StatePath(stateDir))
	if errors.Is(err, os.ErrNotExist) {
		return State{Attempts: map[string]int{}}, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("dispatch: state: %w", err)
	}
	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return State{}, fmt.Errorf("dispatch: state: %s does not parse (%w): fix or remove it; nothing starts until then", StateFile, err)
	}
	if s.Attempts == nil {
		s.Attempts = map[string]int{}
	}
	return s, nil
}

// save writes the state with mode 0600.
func (s *State) save(stateDir string) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("dispatch: state: %w", err)
	}
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return fmt.Errorf("dispatch: state: %w", err)
	}
	if err := atomicfile.Write(StatePath(stateDir), append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("dispatch: state: %w", err)
	}
	return nil
}

// rollover starts a new day: the counts reset, runs and history stay.
func (s *State) rollover(day string) {
	if s.Day == day {
		return
	}
	s.Day, s.Started, s.Attempts = day, 0, map[string]int{}
}

// live returns the recorded runs that still count: the process exists and the
// entry is not older than maxRunAge.
func (s *State) live(now time.Time) []Run {
	var out []Run
	for _, r := range s.Running {
		if r.PID > 0 && now.Sub(r.Started) < maxRunAge && pidAlive(r.PID) {
			out = append(out, r)
		}
	}
	return out
}

func (s *State) remember(e Exit) {
	s.Last = append([]Exit{e}, s.Last...)
	if len(s.Last) > keepLast {
		s.Last = s.Last[:keepLast]
	}
}

// lastEnded is when the newest remembered run of a request ended; the zero time
// when none is remembered. The cooldown reads it, so it holds across a restart.
func (s *State) lastEnded(id string) time.Time {
	for _, e := range s.Last {
		if e.ID == id {
			return e.Ended
		}
	}
	return time.Time{}
}
