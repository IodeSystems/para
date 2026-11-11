package cli

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/iodesystems/para/changes"
	"github.com/iodesystems/para/internal/server"
	"github.com/iodesystems/para/internal/update"
)

// The update check and auto-install of para serve (internal/update.Auto).
// Switches are per machine, in $PARA_HOME/install.properties:
// upgrade.check=false stops the check (and a vault with egress.update=deny
// stops it), upgrade.auto=false only notifies,
// upgrade.vault_steps=auto runs the automated vault steps after an
// auto-install (otherwise they are filed as agent work).

// autoUpdateEvery is how often para serve runs a pass; autoUpdateFirst is the
// wait before the first one, so a restart settles first.
var (
	autoUpdateEvery = 10 * time.Minute
	autoUpdateFirst = time.Minute
)

// updateSettings reads the machine's switches; an unreadable file keeps the
// defaults (on).
func (l *loader) updateSettings() update.Settings {
	cfg, err := l.install.Props()
	if err != nil {
		cfg = nil
	}
	return update.SettingsFrom(cfg)
}

// updateCurrent is the release this build counts as for the check: its
// version when it is vX.Y.Z, else v<newest release notes in it>.
func (l *loader) updateCurrent() string {
	if update.IsRelease(l.version, "") {
		return l.version
	}
	return "v" + changes.Latest()
}

func (l *loader) updateRelease() bool { return update.IsRelease(l.version, l.srcDir) }

// updateStatus is the web banner's read and doctor's.
func (l *loader) updateStatus() server.UpdateStatus {
	st := update.LoadState(update.StateFile(l.install.Home))
	set := l.updateSettings()
	cur := l.updateCurrent()
	out := server.UpdateStatus{
		Running: l.version, Release: l.updateRelease(), Check: set.Check, Auto: set.Auto,
		Checked: st.Checked, CheckError: st.CheckError, CheckKind: st.CheckKind,
		Notice: update.Notice(st, cur, l.updateRelease(), set),
	}
	if st.Newer(cur) {
		out.Available, out.Summary = st.Version, st.Summary
	}
	if st.InstalledTo != "" && st.InstalledTo == l.version {
		out.InstalledFrom, out.InstalledTo, out.InstalledAt = st.InstalledFrom, st.InstalledTo, st.InstalledAt
	}
	return out
}

func (l *loader) updateNotice() string { return l.updateStatus().Notice }

// newAutoUpdate builds the pass para serve runs. quiet and egress come from
// the running server.
func newAutoUpdate(l *loader, log *slog.Logger, srv *server.Server, hub *server.Hub) (*update.Auto, error) {
	u, err := releaseUpdater(l)
	if err != nil {
		return nil, err
	}
	u.Current = l.updateCurrent()
	return &update.Auto{
		StatePath:    u.StatePath,
		Current:      u.Current,
		Release:      l.updateRelease(),
		Settings:     l.updateSettings,
		Latest:       u.Latest,
		Egress:       hub.LogEgress,
		UpdateDenied: hub.UpdateDenied,
		Quiet: func(now time.Time) (bool, string) {
			if why := srv.Busy(now, update.QuietFor); why != "" {
				return false, why
			}
			if why := hub.AgentsBusy(context.Background(), now); why != "" {
				return false, why
			}
			return true, ""
		},
		Install: func(ctx context.Context) error { return autoInstall(ctx, l, log, u) },
		Log:     log,
	}, nil
}

// autoInstall is `para upgrade --release` without a terminal: verify and
// swap, record it, bring each vault along (its automated steps filed as
// agent work unless upgrade.vault_steps=auto), then restart the service,
// which ends this process.
func autoInstall(ctx context.Context, l *loader, log *slog.Logger, u update.Upgrader) error {
	w := &logLines{log: log}
	defer w.flush()
	set := l.updateSettings()
	o := upgradeOptions{Version: l.version, OldRelease: changes.Latest(), Home: l.install.Home, Updater: u,
		NoVaultSteps: set.VaultSteps != "auto", Yes: set.VaultSteps == "auto"}
	o.VaultIDs, o.Skipped = machineVaults(l)
	res, err := installRelease(ctx, w, o)
	if err != nil || !res.Installed {
		return err
	}
	if err := update.MarkInstalled(u.StatePath, l.version, res.To, time.Now()); err != nil {
		log.Warn("update: record the install", "error", err)
	}
	for _, id := range o.VaultIDs {
		_ = vaultSteps(ctx, w, o, u.Exe, id)
	}
	w.flush()
	restartService(ctx, w, u.Exe, l.install.Home)
	return nil
}

// runAutoUpdate runs a pass after autoUpdateFirst, then every
// autoUpdateEvery, until ctx ends. A new notice goes to every MCP session.
func runAutoUpdate(ctx context.Context, a *update.Auto, l *loader, srv *server.Server) {
	last := l.updateNotice()
	t := time.NewTimer(autoUpdateFirst)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		a.Pass(ctx)
		if n := l.updateNotice(); n != last {
			last = n
			if n != "" {
				srv.AnnounceUpdate("para: " + n + ". Tell the owner; call changes after the upgrade.")
			}
		}
		t.Reset(autoUpdateEvery)
	}
}

// logLines is an io.Writer that logs each line it gets at info.
type logLines struct {
	log *slog.Logger
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *logLines) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf.Write(p)
	for {
		line, err := w.buf.ReadString('\n')
		if err != nil {
			w.buf.WriteString(line)
			return len(p), nil
		}
		if line = strings.TrimSpace(line); line != "" {
			w.log.Info("update", "line", line)
		}
	}
}

func (w *logLines) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if line := strings.TrimSpace(w.buf.String()); line != "" {
		w.log.Info("update", "line", line)
	}
	w.buf.Reset()
}
