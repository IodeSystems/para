package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/iodesystems/parasolve/internal/netguard"
	"github.com/iodesystems/parasolve/internal/props"
	"github.com/iodesystems/parasolve/internal/server"
	"github.com/iodesystems/parasolve/internal/webui"
	"github.com/iodesystems/parasolve/ui"
)

const (
	defaultAddr     = "127.0.0.1:7700"
	shutdownTimeout = 10 * time.Second
	readHeaderLimit = 10 * time.Second
	idleLimit       = 2 * time.Minute

	serverConfig = "server.properties" // in $PARA_HOME: per machine, never synced
	keyProxies   = "net.trusted_proxies"
	keyBases     = "net.base_hosts"
)

type serveFlags struct {
	addr    string
	proxies []string
	bases   []string
	webRoot string
	check   bool
}

// serveConfig is the checked configuration of a server.
type serveConfig struct {
	proxies netguard.Proxies
	bases   []string
	bare    string
	web     http.Handler
}

func newServeCmd(l *loader) *cobra.Command {
	var f serveFlags
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve the API and UI for every vault, routed by host name",
		Long: "Serve the API and UI. One process serves every vault of this install:\n" +
			"<vault>.localhost:<port> and <vault>.<base> for each --base. The vault named\n" +
			"by --vault, PARA_VAULT, the default vault, or the only vault is also served\n" +
			"at the bare host: the listen address, localhost, 127.0.0.1, ::1, and each\n" +
			"--base. Any other Host gets 421. Token auth is always on.\n\n" +
			"$PARA_HOME/server.properties can set net.trusted_proxies and net.base_hosts\n" +
			"(comma separated). Each vault's net.rules is in\n" +
			"$PARA_HOME/vaults/<id>/server/ and starts with loopback only.\n\n" +
			"To stop it, send SIGTERM or press Ctrl-C.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := l.load(); err != nil {
				return err
			}
			return runServe(cmd.Context(), l, f, cmd.ErrOrStderr())
		},
	}
	cmd.AddCommand(newLoginCmd(l, "login"), newServeStatusCmd(l))
	fs := cmd.Flags()
	fs.StringVar(&f.addr, "addr", defaultAddr, "listen address")
	fs.StringArrayVar(&f.proxies, "trusted-proxy", nil, "exact IP of a proxy whose X-Forwarded-For is read (repeatable)")
	fs.StringArrayVar(&f.bases, "base", nil, "base host: serves <vault>.<base> (repeatable)")
	fs.StringVar(&f.webRoot, "web-root", "", "serve the UI from this directory, not the embedded one")
	fs.BoolVar(&f.check, "check", false, "prove the server can start, then exit")
	return cmd
}

// logger follows LOG-1: JSON unless this is a dev build, level PARA_LOG_LEVEL.
// Output is stderr only (LOG-2).
func (l *loader) logger(w io.Writer) *slog.Logger {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(l.env("PARA_LOG_LEVEL"))); err != nil {
		lvl = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: lvl}
	if l.version == "dev" {
		return slog.New(slog.NewTextHandler(w, opts))
	}
	return slog.New(slog.NewJSONHandler(w, opts))
}

// loadServeConfig merges flags with server.properties and checks every value.
func loadServeConfig(l *loader, f serveFlags) (serveConfig, error) {
	file := filepath.Join(l.install.Home, serverConfig)
	cfg := map[string]string{}
	if _, err := os.Stat(file); err == nil {
		if cfg, err = props.Load(file); err != nil {
			return serveConfig{}, fmt.Errorf("serve: %w", err)
		}
	}
	proxies, err := netguard.ParseProxies(append(splitList(cfg[keyProxies]), f.proxies...))
	if err != nil {
		return serveConfig{}, fmt.Errorf("serve: %w", err)
	}
	out := serveConfig{proxies: proxies, bases: append(splitList(cfg[keyBases]), f.bases...), bare: l.vaultFlag}
	if out.bare != "" {
		if _, err := l.install.Select(out.bare, ""); err != nil {
			return serveConfig{}, fmt.Errorf("serve: --vault: %w", err)
		}
	} else if sel, err := l.selectVault(); err == nil {
		// PARA_VAULT, the default vault, or the only vault. With no choice,
		// a bare address answers 421, and runServe says how to fix that.
		out.bare = sel.ID
	}
	webFS := ui.Dist()
	if f.webRoot != "" {
		info, err := os.Stat(f.webRoot)
		if err != nil {
			return serveConfig{}, fmt.Errorf("serve: --web-root: %w", err)
		}
		if !info.IsDir() {
			return serveConfig{}, fmt.Errorf("serve: --web-root: %s is not a directory", f.webRoot)
		}
		webFS = os.DirFS(f.webRoot)
	}
	out.web = webui.Handler(webFS)
	return out, nil
}

func splitList(s string) []string {
	var out []string
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}

func runServe(ctx context.Context, l *loader, f serveFlags, w io.Writer) error {
	log := l.logger(w)
	cfg, err := loadServeConfig(l, f)
	if err != nil {
		return err
	}
	bind, err := netip.ParseAddrPort(f.addr)
	if err != nil {
		return fmt.Errorf("serve: --addr: want ip:port: %w", err)
	}
	live, err := server.NewLive()
	if err != nil {
		return err
	}
	hub := server.NewHub(l.install, log)
	hub.SetLive(live)
	defer hub.Close()
	ids, err := hub.Prepare()
	if err != nil {
		return err
	}
	if f.check {
		log.Info("server can start", "addr", f.addr, "vaults", len(ids))
		return nil
	}
	if len(ids) == 0 {
		log.Warn("no vaults: every host gets 421 until para init")
	}
	if err := server.EnsureVarDir(l.install.Home); err != nil {
		return err
	}
	release, err := server.LockPID(server.LockPath(l.install.Home))
	if err != nil {
		return err
	}
	defer release()

	srv, err := server.New(server.Options{Version: l.version, Web: cfg.web, Log: log, Live: live})
	if err != nil {
		return err
	}
	bare := []string{"127.0.0.1", "::1"}
	if !bind.Addr().IsUnspecified() {
		bare = append(bare, bind.Addr().String())
	}
	if cfg.bare == "" {
		log.Warn("no vault for a bare address: it answers 421. Use --vault <id>, run para vault set-default <id>, or open <vault>.localhost", "addr", f.addr)
	}
	gate := netguard.New(netguard.Config{
		Proxies: cfg.proxies,
		Router:  netguard.NewRouter(cfg.bases, cfg.bare, bare),
		Lookup:  hub.Lookup,
		Policy:  srv.Policy,
		Log:     log,
	})
	ln, err := net.Listen("tcp", f.addr)
	if err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	defer func() { _ = ln.Close() }()
	rec := server.Record{PID: os.Getpid(), Addr: ln.Addr().String(), Version: l.version, Vault: cfg.bare,
		Vaults: ids, BaseHosts: cfg.bases, Started: time.Now().UTC()}
	if err := server.WriteRecord(l.install.Home, rec); err != nil {
		return err
	}
	defer server.RemoveRecord(l.install.Home)
	if !bind.Addr().IsLoopback() {
		log.Warn("plain HTTP on a non-loopback address: tokens and documents cross the network in the clear; token auth stays on", "addr", f.addr)
	}
	hs := &http.Server{
		Handler:           srv.Handler(gate),
		ReadHeaderTimeout: readHeaderLimit,
		IdleTimeout:       idleLimit,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
	// http.Server.Shutdown leaves hijacked sockets alone; the relay closes them.
	relayDone := live.RegisterShutdown(hs, shutdownTimeout, log)
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	done := make(chan error, 1)
	go func() { done <- hs.Serve(ln) }()
	log.Info("para serving", "addr", ln.Addr().String(), "vaults", strings.Join(ids, ","), "version", l.version)
	log.Info("to sign in as admin, run: para serve login" + loginVaultHint(cfg.bare, ids) + " (the token is never logged)")
	select {
	case err := <-done:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}
	log.Info("para stopping")
	sctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := hs.Shutdown(sctx); err != nil {
		return fmt.Errorf("serve: shutdown: %w", err)
	}
	<-relayDone
	if err := <-done; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}

// loginVaultHint adds --vault when a bare `para serve login` could not pick.
func loginVaultHint(bare string, ids []string) string {
	if bare != "" || len(ids) < 2 {
		return ""
	}
	return " --vault <id>"
}
