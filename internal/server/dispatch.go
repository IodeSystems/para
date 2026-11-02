package server

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/iodesystems/parasolve/internal/dispatch"
	"github.com/iodesystems/parasolve/internal/events"
	"github.com/iodesystems/parasolve/internal/model"
)

// dispatchRefresh is how often RunDispatch looks for a vault that has gained an
// agent.json.
const dispatchRefresh = 15 * time.Second

// DispatchActor is the actor and surface of dispatch events.
const DispatchActor = "dispatch"

// CheckDispatch checks the agent.json of every registered vault. A file that
// does not parse is an error, so `serve --check` stops a service unit once with
// the reason. A command that is not on PATH, or a missing working directory, is
// a note: the machine may mount it later, or PATH may differ from this shell's.
func (h *Hub) CheckDispatch() (notes []string, err error) {
	vaults, err := h.install.List()
	if err != nil {
		return nil, err
	}
	for _, v := range vaults {
		state := filepath.Join(h.install.Home, "vaults", v.ID)
		cfg, err := dispatch.LoadConfig(state)
		if err != nil {
			return notes, err
		}
		if cfg == nil {
			continue
		}
		if _, err := cfg.Program(); err != nil {
			notes = append(notes, "vault "+v.ID+": "+err.Error())
		}
		if err := cfg.CheckCwd(state); err != nil {
			notes = append(notes, "vault "+v.ID+": "+err.Error())
		}
	}
	return notes, nil
}

// RunDispatch starts a dispatcher for each vault that has an agent.json on this
// machine, and starts one later for a vault that gains one. A vault without the
// file starts nothing and watches nothing. It returns when ctx ends and every
// command it started has ended: ending ctx sends each a SIGTERM. home is the
// install's $PARA_HOME, handed to the commands.
func (h *Hub) RunDispatch(ctx context.Context, home string) {
	var wg sync.WaitGroup
	running := map[string]bool{}
	start := func(id string) {
		t, err := h.tenant(id)
		if err != nil || t == nil {
			return
		}
		d := dispatch.New(dispatch.Options{
			Vault: id, Home: home, StateDir: t.state, Store: t.store.As(DispatchActor, DispatchActor),
			Log: h.log, Context: ctx,
		})
		stopWatch := events.Watch(ctx, filepath.Join(t.path, "events"), 0, func([]model.Event) { d.Kick() },
			func(m string) { h.log.Warn("dispatch watch", "vault", id, "note", m) })
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer stopWatch()
			d.Run(ctx)
		}()
		h.log.Info("dispatch on", "vault", id)
	}
	refresh := func() {
		vaults, err := h.install.List()
		if err != nil {
			return
		}
		for _, v := range vaults {
			if running[v.ID] {
				continue
			}
			if _, err := os.Stat(dispatch.ConfigPath(filepath.Join(home, "vaults", v.ID))); err != nil {
				continue
			}
			running[v.ID] = true
			start(v.ID)
		}
	}
	refresh()
	t := time.NewTicker(dispatchRefresh)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		case <-t.C:
			refresh()
		}
	}
}
