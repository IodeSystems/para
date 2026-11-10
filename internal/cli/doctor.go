package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/iodesystems/para/internal/dispatch"
	"github.com/iodesystems/para/internal/export"
	"github.com/iodesystems/para/internal/keys"
	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/netguard"
	"github.com/iodesystems/para/internal/paramd"
	"github.com/iodesystems/para/internal/props"
	"github.com/iodesystems/para/internal/registry"
	"github.com/iodesystems/para/internal/server"
	"github.com/iodesystems/para/internal/service"
	"github.com/iodesystems/para/internal/store/local"
	"github.com/iodesystems/para/internal/upgrade"
	"github.com/iodesystems/para/internal/vault"
)

// report prints one line per finding (CLI-13).
type report struct {
	w     io.Writer
	fails int
	warns int
}

func (r *report) ok(what, detail string) {
	_, _ = fmt.Fprintf(r.w, "%s    %s: %s\n", paint(r.w, toneOK, "ok"), what, detail)
}

func (r *report) warn(what, detail string) {
	r.warns++
	_, _ = fmt.Fprintf(r.w, "%s  %s: %s\n", paint(r.w, toneWarn, "warn"), what, detail)
}

func (r *report) fail(what, detail string) {
	r.fails++
	_, _ = fmt.Fprintf(r.w, "%s  %s: %s\n", paint(r.w, toneErr, "fail"), what, detail)
}

// verdict is the last line of doctor.
func (r *report) verdict() {
	_ = verdict(r.w, countTone(r.fails, r.warns), "doctor: %d failed, %d warnings", r.fails, r.warns)
}

func newDoctorCmd(l *loader) *cobra.Command {
	var exposure string
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Check this install; read-only. --exposure runs live checks on a URL",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := l.load(); err != nil {
				return err
			}
			r := &report{w: cmd.OutOrStdout()}
			doctorInstall(l, r)
			if exposure != "" {
				doctorExposure(exposure, r)
			}
			r.verdict()
			if r.fails > 0 {
				return fmt.Errorf("doctor: %d failed", r.fails)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&exposure, "exposure", "", "base URL of a vault to probe, as a client would reach it")
	return cmd
}

func doctorInstall(l *loader, r *report) {
	r.ok("home", l.install.Home)
	vaults, err := l.install.List()
	if err != nil {
		r.fail("vaults", err.Error()+"; fix or remove the entry under "+filepath.Join(l.install.Home, "vaults"))
		return
	}
	if len(vaults) == 0 {
		r.warn("vaults", "none: run para setup")
	}
	doctorKinds(l, r, vaults)
	for _, v := range vaults {
		doctorVault(l, r, v.ID)
		doctorTick(l, r, v.ID, v.Path)
		doctorDispatch(l, r, v.ID)
		doctorSources(r, v.ID, v.Path)
	}
	if _, err := loadServeConfig(l, serveFlags{}); err != nil {
		r.fail("server config", err.Error()+"; fix "+filepath.Join(l.install.Home, serverConfig))
	}
	doctorTools(r)
	doctorService(r)
	doctorDaemon(l, r, vaults)
	doctorStaleMCP(r, "/proc")
	for _, v := range vaults {
		doctorUpgrade(l, r, v)
	}
}

// doctorKinds checks the machine kind and that each vault is of that kind. Work
// and personal never share a machine.
func doctorKinds(l *loader, r *report, vaults []registry.Vault) {
	mk, err := l.install.Kind()
	switch {
	case err != nil:
		r.fail("machine kind", err.Error())
	case mk == "":
		r.warn("machine kind", "not recorded: run para setup, or para init --kind work|personal")
	default:
		r.ok("machine kind", mk)
	}
	for _, v := range vaults {
		what := "vault " + v.ID + " kind"
		cfg, err := props.Load(filepath.Join(v.Path, ".para", "vault.properties"))
		if err != nil {
			continue // doctorVault and doctorUpgrade report an unreadable vault
		}
		switch vk := cfg[vault.KeyKind]; {
		case vk == "":
			r.warn(what, "not recorded in .para/vault.properties: run para setup (it marks the vault with the machine kind)")
		case !registry.ValidKind(vk):
			r.fail(what, fmt.Sprintf("%q is not work or personal", vk))
		case mk != "" && vk != mk:
			r.fail(what, fmt.Sprintf("%s on a %s machine: work and personal never share a machine; remove this vault from this machine", vk, mk))
		default:
			r.ok(what, vk)
		}
	}
}

func doctorVault(l *loader, r *report, id string) {
	dir := server.ServerDir(l.install, id)
	what := "vault " + id
	store := keys.New(dir)
	if _, err := os.Stat(store.AdminPath()); errors.Is(err, os.ErrNotExist) {
		r.warn(what, "no server state yet: para serve creates admin.token and net.rules")
		return
	}
	if _, err := store.AdminToken(); err != nil {
		r.fail(what+" admin token", err.Error()+"; fix: chmod 600 "+store.AdminPath())
	} else {
		r.ok(what+" admin token", "mode 600")
	}
	rules := filepath.Join(dir, "net.rules")
	if _, err := netguard.LoadRules(rules); err != nil {
		r.fail(what+" net.rules", err.Error()+"; fix "+rules+": without it every request is denied")
	} else {
		r.ok(what+" net.rules", "parsed")
	}
	list, err := store.List()
	if err != nil {
		r.fail(what+" keys", err.Error())
		return
	}
	r.ok(what+" keys", fmt.Sprintf("%d", len(list)))
}

// doctorTick reports whether tick runs on this install for the vault: it does
// only where request.runner names this install.
func doctorTick(l *loader, r *report, id, path string) {
	what := "vault " + id + " tick"
	cfg, err := props.Load(filepath.Join(path, ".para", "vault.properties"))
	if err != nil {
		r.fail(what, err.Error())
		return
	}
	install, err := l.install.ID()
	if err != nil {
		r.fail(what, err.Error())
		return
	}
	switch runner := strings.TrimSpace(cfg["request.runner"]); runner {
	case "":
		r.warn(what, "no runner named: para tick refuses. To tick here, run: para vault config set request.runner "+install)
	case install:
		r.ok(what, "this install is the runner")
	default:
		r.ok(what, "the runner is "+runner+", not this install: tick skips here")
	}
}

// doctorDispatch reports the per-machine dispatch config of a vault. Dispatch
// is off without agent.json, and a run of it spends tokens.
func doctorDispatch(l *loader, r *report, id string) {
	what := "vault " + id + " dispatch"
	state := l.stateDir(id)
	cfg, err := dispatch.LoadConfig(state)
	if err != nil {
		r.fail(what, err.Error()+"; fix "+dispatch.ConfigPath(state))
		return
	}
	if cfg == nil {
		r.ok(what, "off: no "+dispatch.FileName+", para starts nothing")
		return
	}
	r.ok(what, fmt.Sprintf("on: %s (%d arguments), %d at a time, %d starts a day. A run starts an agent and spends tokens", cfg.Command[0], len(cfg.Command)-1, cfg.MaxConcurrent, cfg.DailyLimit))
	if _, err := cfg.Program(); err != nil {
		r.warn(what+" command", err.Error()+"; nothing can start until it is on PATH")
	}
	if err := cfg.CheckCwd(state); err != nil {
		r.warn(what+" cwd", err.Error())
	}
	if p, err := unitPath(service.DefaultName); err == nil {
		if _, installed := installedArgs(service.DefaultName); installed && !filepath.IsAbs(cfg.Command[0]) {
			if !onPath(cfg.Command[0], service.EnvValue(p, "PATH")) {
				r.warn(what+" service PATH", cfg.Command[0]+" is not on the service's PATH: reinstall from a shell that has it (para service install), or use an absolute path")
			}
		}
	}
	if s, err := dispatch.LoadState(state); err != nil {
		r.fail(what+" state", err.Error())
	} else {
		r.ok(what+" state", fmt.Sprintf("%d runs in flight recorded, %d started on %s", len(s.Running), s.Started, s.Day))
	}
}

// doctorSources checks the sources of a vault as far as this machine can: a
// via cli source's commands must be on PATH here. A via mcp source is reached
// only through an agent's own connector, which may live inside its harness:
// para cannot check it.
func doctorSources(r *report, id, path string) {
	files, _ := filepath.Glob(filepath.Join(path, string(model.FolderSources), "*.md"))
	for _, file := range files {
		f, err := paramd.ParseFile(file)
		if err != nil || f.Type() != "source" {
			continue
		}
		what := "vault " + id + " source " + f.Name
		fm := f.Frontmatter
		if s, _ := fm["status"].(string); s == string(model.SourcePaused) {
			r.ok(what, "paused")
			continue
		}
		switch via, _ := fm["via"].(string); via {
		case string(model.SourceViaMCP):
			server, _ := fm["server"].(string)
			r.ok(what, "via mcp (server "+server+"): agent-only, not checked here")
		case string(model.SourceViaCLI):
			var missing, found []string
			for _, prog := range sourcePrograms(fm) {
				if p, err := exec.LookPath(prog); err == nil {
					found = append(found, p)
				} else if !slices.Contains(missing, prog) {
					missing = append(missing, prog)
				}
			}
			switch {
			case len(missing) > 0:
				r.warn(what, "via cli: not on PATH here: "+strings.Join(missing, ", ")+"; install it, or sweep this source from a machine that has it")
			case len(found) == 0:
				r.warn(what, "via cli but no command in its steps")
			default:
				r.ok(what, "via cli: "+strings.Join(found, ", "))
			}
		default:
			r.ok(what, "via none: the agent reads it as the source says; not checked here")
		}
	}
}

// sourcePrograms lists the program of each command template of a source: the
// first word, with ~/ expanded.
func sourcePrograms(fm map[string]any) []string {
	var out []string
	for _, step := range paramd.SourceSteps {
		m, _ := fm[step].(map[string]any)
		c, _ := m["command"].(string)
		words := strings.Fields(c)
		if len(words) == 0 {
			continue
		}
		prog := strings.Trim(words[0], `"'`)
		if rest, ok := strings.CutPrefix(prog, "~/"); ok {
			if home, err := os.UserHomeDir(); err == nil {
				prog = filepath.Join(home, rest)
			}
		}
		if !slices.Contains(out, prog) {
			out = append(out, prog)
		}
	}
	return out
}

func doctorTools(r *report) {
	if p, err := exec.LookPath("git"); err == nil {
		r.ok("git", p)
	} else {
		r.warn("git", "not on PATH: vaults made with git cannot commit; install git")
	}
	for _, it := range export.AvailableFormats() {
		line(r, "export format "+it.Name, it)
	}
	for _, it := range export.AvailableTransports() {
		line(r, "export transport "+it.Name, it)
	}
}

func line(r *report, what string, it export.Item) {
	if it.Available {
		r.ok(what, "available")
		return
	}
	r.warn(what, "needs "+strings.Join(it.Needs, " or ")+" on PATH")
}

func doctorService(r *report) {
	args, installed := installedArgs(service.DefaultName)
	if !installed {
		r.warn("service", "not installed: para service install")
		return
	}
	r.ok("service", "installed")
	addrs := service.FlagValues(args, "--addr")
	if len(addrs) == 0 {
		addrs = []string{defaultAddr}
	}
	for _, addr := range addrs {
		if ap, err := netip.ParseAddrPort(addr); err == nil && !ap.Addr().IsLoopback() {
			r.warn("service address", addr+" is plain HTTP on a non-loopback address: tokens and documents cross that network in the clear; put hz in front or bind 127.0.0.1")
		}
	}
	if p, err := unitPath(service.DefaultName); err == nil {
		path := service.EnvValue(p, "PATH")
		if onPath("git", path) {
			r.ok("service PATH", "git found")
		} else {
			r.warn("service PATH", "no git on the service's PATH: reinstall from a shell that has it: para service install")
		}
	}
}

func onPath(tool, path string) bool {
	for _, d := range filepath.SplitList(path) {
		if info, err := os.Stat(filepath.Join(d, tool)); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return true
		}
	}
	return false
}

// doctorExposure probes base as a client would. It sends no token and writes
// nothing. A probe that gets through a control is a failure with the fix.
func doctorExposure(base string, r *report) {
	base = strings.TrimRight(base, "/")
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		r.fail("exposure", "bad URL "+base)
		return
	}
	hc := localClient(10 * time.Second)
	hc.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	probe := func(method, path string, hdr map[string]string, host string) (*http.Response, string, error) {
		req, err := http.NewRequest(method, base+path, nil)
		if err != nil {
			return nil, "", err
		}
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		if host != "" {
			req.Host = host
		}
		resp, err := hc.Do(req)
		if err != nil {
			return nil, "", err
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		return resp, string(b), nil
	}

	if u.Scheme == "http" && !isLoopbackHost(u.Hostname()) {
		r.warn("exposure scheme", "plain HTTP: tokens and documents are readable on this network; no service worker or clipboard in the UI")
	}
	resp, body, err := probe(http.MethodGet, "/healthz", nil, "")
	if err != nil {
		r.fail("exposure health", err.Error()+": check the URL, the proxy, and net.rules")
		return
	}
	switch {
	case resp.StatusCode != http.StatusOK:
		r.fail("exposure health", resp.Status+" "+strings.TrimSpace(body)+": 403 means net.rules denies this client; 421 means the host does not name a vault")
		return
	case strings.Contains(body, "/"):
		r.fail("exposure health", "/healthz names a path: upgrade para")
	default:
		r.ok("exposure health", "status only")
	}
	if !strings.Contains(resp.Header.Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		r.fail("exposure headers", "no strict CSP: a proxy strips it; pass response headers through")
	} else {
		r.ok("exposure headers", "CSP present")
	}
	check := func(what, method, path string, want int, hdr map[string]string, host, fix string) {
		resp, _, err := probe(method, path, hdr, host)
		if err != nil {
			r.fail(what, err.Error())
			return
		}
		if resp.StatusCode != want {
			r.fail(what, fmt.Sprintf("got %s, want %d: %s", resp.Status, want, fix))
			return
		}
		r.ok(what, resp.Status)
	}
	check("exposure no token read", http.MethodGet, "/api/v1/task/list", http.StatusUnauthorized, nil, "", "a route is open; do not expose this server")
	check("exposure no token write", http.MethodPost, "/api/v1/io/add", http.StatusUnauthorized, nil, "", "a route is open; do not expose this server")
	check("exposure no token graphql", http.MethodPost, "/api/graphql", http.StatusUnauthorized, nil, "", "a route is open; do not expose this server")
	check("exposure rebinding host", http.MethodGet, "/healthz", http.StatusMisdirectedRequest, nil, "rebinding.invalid", "the proxy rewrites Host; pass it through unchanged")
	check("exposure path trick", http.MethodGet, "/api/v1/..%2fv1/task/list", http.StatusBadRequest, nil, "", "the proxy rewrites paths before para sees them; keep the raw path")
	check("exposure forged client", http.MethodGet, "/api/v1/task/list", http.StatusUnauthorized, map[string]string{"X-Forwarded-For": "127.0.0.1"}, "", "a forged address changed the answer; check net.trusted_proxies")
}

func isLoopbackHost(h string) bool {
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	a, err := netip.ParseAddr(h)
	return err == nil && a.IsLoopback()
}

// doctorDaemon checks the running server, found from its runtime record, then
// the installed service, then the default.
func doctorDaemon(l *loader, r *report, vaults []registry.Vault) {
	at := l.findServer()
	if at.Source == "default" {
		return
	}
	if at.Rec != nil {
		r.ok("server", fmt.Sprintf("pid %d at %s, version %s", at.Rec.PID, at.Rec.Addr, at.Rec.Version))
		if note := staleServerNote(at.Rec.Version, l.version); note != "" {
			r.warn("server version", note)
		}
	}
	id := ""
	switch {
	case at.Rec != nil && at.Rec.Vault != "":
		id = at.Rec.Vault
	case len(vaults) > 0:
		id = vaults[0].ID
	default:
		return
	}
	base, err := clientBase(id, at)
	if err != nil {
		r.warn("daemon", err.Error())
		return
	}
	switch err := probeHealth(base); {
	case err == nil:
		r.ok("daemon", "answers at "+base)
	case errors.Is(err, errRefused):
		r.warn("daemon", base+" refused this machine (403): para serve login says which net.rules line to add")
	default:
		r.warn("daemon", "nothing answers at "+base+" ("+err.Error()+"): para service start")
	}
}

// doctorUpgrade reports pending vault upgrade steps. It reads only.
func doctorUpgrade(l *loader, r *report, v registry.Vault) {
	what := "vault " + v.ID + " format"
	st, err := local.Open(v.Path, vault.Options{ID: v.ID, ReadOnly: true})
	if err != nil {
		return
	}
	s, err := upgrade.StatusOf(context.Background(), st)
	if err != nil {
		r.warn(what, err.Error())
		return
	}
	if len(s.Pending) == 0 {
		r.ok(what, fmt.Sprintf("%d of %d", s.FormatVersion, s.Latest))
		return
	}
	r.warn(what, fmt.Sprintf("%d upgrade steps pending: para --vault %s vault upgrade status", len(s.Pending), v.ID))
}
