package dispatch

import (
	"path/filepath"
	"time"
)

// Status is what `para agent status` shows for one vault. It reads the config
// and the state file and starts nothing. It holds the program name and the
// number of arguments, never the arguments: they may hold a key.
type Status struct {
	Configured    bool   `json:"configured"`
	Error         string `json:"error,omitempty"` // the config or state file does not load
	Program       string `json:"program,omitempty"`
	Args          int    `json:"args"`
	Cwd           string `json:"cwd,omitempty"`
	MaxConcurrent int    `json:"max_concurrent"`
	DailyLimit    int    `json:"daily_limit"`
	Day           string `json:"day"`
	Started       int    `json:"started"` // starts today
	Running       []Run  `json:"running"`
	Last          []Exit `json:"last"`
	LogDir        string `json:"log_dir,omitempty"`
}

// ReadStatus reads the dispatch status of the vault whose state directory is
// stateDir. A run whose process is gone is not listed as running.
func ReadStatus(stateDir string, now time.Time) Status {
	st := Status{Running: []Run{}, Last: []Exit{}, Day: now.Local().Format("2006-01-02")}
	cfg, err := LoadConfig(stateDir)
	if err != nil {
		st.Error = err.Error()
		return st
	}
	if cfg == nil {
		return st
	}
	st.Configured = true
	st.Program, st.Args = cfg.Command[0], len(cfg.Command)-1
	st.Cwd, st.MaxConcurrent, st.DailyLimit = cfg.Cwd, cfg.MaxConcurrent, cfg.DailyLimit
	st.LogDir = filepath.Join(stateDir, "agent", "runs")
	s, err := LoadState(stateDir)
	if err != nil {
		st.Error = err.Error()
		return st
	}
	if s.Day == st.Day {
		st.Started = s.Started
	}
	if live := s.live(now); live != nil {
		st.Running = live
	}
	if s.Last != nil {
		st.Last = s.Last
	}
	return st
}
