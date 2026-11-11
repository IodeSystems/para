package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/iodesystems/para/internal/keys"
	"github.com/iodesystems/para/internal/netguard"
	"github.com/iodesystems/para/internal/props"
	"github.com/iodesystems/para/internal/registry"
	"github.com/iodesystems/para/internal/server"
	"github.com/iodesystems/para/internal/service"
	"github.com/iodesystems/para/internal/vault"
)

// setupHooks replace the parts of para setup that touch this machine. Tests set
// them; nil fields mean the real thing.
type setupHooks struct {
	svcRun runner               // service manager commands
	wait   *time.Duration       // how long to wait for the server after a service start
	tty    func(io.Reader) bool // whether stdin takes prompts
}

func (l *loader) setupWait() time.Duration {
	if l.hooks != nil && l.hooks.wait != nil {
		return *l.hooks.wait
	}
	return 10 * time.Second
}

func (l *loader) stdinIsTTY(in io.Reader) bool {
	if l.hooks != nil && l.hooks.tty != nil {
		return l.hooks.tty(in)
	}
	return isTerminal(in)
}

type setupFlags struct {
	path, agent, agentName string
	service, noService     bool
	print, login, dryRun   bool
	domains                []string // --domain <vault>=<domain>
	allowDomains           []string // --allow-domain <domain>
	domainsDefault         bool
	domainOf               map[string]string
}

// prompter asks on a terminal.
type prompter struct {
	in  *bufio.Reader
	out io.Writer
}

func (p *prompter) ask(q, def string) (string, error) {
	if def != "" {
		_, _ = fmt.Fprintf(p.out, "%s [%s]: ", q, def)
	} else {
		_, _ = fmt.Fprintf(p.out, "%s: ", q)
	}
	line, err := p.in.ReadString('\n')
	if err != nil && (line == "" || !errors.Is(err, io.EOF)) {
		return "", fmt.Errorf("setup: no answer to %q: %w", q, err)
	}
	if line = strings.TrimSpace(line); line != "" {
		return line, nil
	}
	return def, nil
}

func (p *prompter) yesNo(q string, defYes bool) (bool, error) {
	def := "y/N"
	if defYes {
		def = "Y/n"
	}
	for {
		a, err := p.ask(q+" ["+def+"]", "")
		if err != nil {
			return false, err
		}
		switch strings.ToLower(a) {
		case "":
			return defYes, nil
		case "y", "yes":
			return true, nil
		case "n", "no":
			return false, nil
		}
	}
}

func newSetupCmd(l *loader) *cobra.Command {
	var f setupFlags
	cmd := &cobra.Command{
		Use:   "setup",
		Short: "Walk a new install through doctor, domains, first vault, service, sign-in, and agent hookup; a rerun skips what is done",
		Long: "Set up this machine in order: doctor checks, domains, first vault, the service\n" +
			"(para service install + start) and sign-in, then an optional agent hookup. A\n" +
			"step already done is skipped on a rerun.\n\n" +
			"Domains: a vault has a domain (vault.domain, one lowercase word such as life,\n" +
			"iode, or corp) and a machine holds a list of domains (machine.domains in\n" +
			"$PARA_HOME/install.properties, never in a vault). A vault opens only on a\n" +
			"machine that lists its domain; a corp vault stays on the corp machine. Setup\n" +
			"asks the domain of each registered vault that has none (default: its id),\n" +
			"then records the union as machine.domains.\n\n" +
			"On a terminal it asks each question. Every question has a flag. With no\n" +
			"terminal, a missing answer is listed with its flag and setup exits non-zero\n" +
			"before it changes anything. An external command (claude mcp add) runs only on\n" +
			"a yes: a prompt answer or --agent claude. --vault <id> names the first vault.\n" +
			"--agent none|claude|dun|generic: claude runs claude mcp add and para skill\n" +
			"install; dun prints para help dun; generic makes an agent key for any MCP\n" +
			"client over HTTP (--agent-name) and prints its name, URL, and token once.\n" +
			"A skill installed by another para version is reinstalled. Sign-in is skipped\n" +
			"while every served vault has a login key that has not expired (--login signs\n" +
			"in again). --dry-run prints every step and every write and changes nothing.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := l.load(); err != nil {
				return err
			}
			return runSetup(l, cmd, f)
		},
	}
	fl := cmd.Flags()
	fl.StringArrayVar(&f.domains, "domain", nil, "domain of a registered vault that has none: <vault>=<domain> (repeatable)")
	fl.BoolVar(&f.domainsDefault, "domains-default", false, "give each vault with no domain its id as domain")
	fl.StringArrayVar(&f.allowDomains, "allow-domain", nil, "add a domain to machine.domains (repeatable)")
	fl.StringVar(&f.path, "path", "", "directory of the first vault (with --vault <id>)")
	fl.BoolVar(&f.service, "service", false, "install and start the service")
	fl.BoolVar(&f.noService, "no-service", false, "do not install the service")
	fl.StringVar(&f.agent, "agent", "", "hook up an agent: none, claude, dun, or generic (default: ask; no terminal: none)")
	fl.StringVar(&f.agentName, "agent-name", "", "with --agent generic or claude: the key name and actor of the agent")
	fl.BoolVar(&f.print, "print", false, "print the sign-in URLs (they hold tokens) instead of opening a browser")
	fl.BoolVar(&f.login, "login", false, "sign in even when every served vault has a login key that has not expired")
	fl.BoolVar(&f.dryRun, "dry-run", false, "print what each step would do and each write, and change nothing")
	return cmd
}

// vaultsWithoutDomain lists the registered vaults with no vault.domain.
func vaultsWithoutDomain(vaults []registry.Vault) []string {
	var ids []string
	for _, v := range vaults {
		cfg, err := props.Load(filepath.Join(v.Path, ".para", "vault.properties"))
		if err == nil && cfg[vault.KeyDomain] == "" {
			ids = append(ids, v.ID)
		}
	}
	return ids
}

// setupMissing lists what a run with no terminal would have to ask.
func setupMissing(firstID string, f setupFlags, vaults []registry.Vault, noDomain []string) []string {
	var m []string
	for _, id := range noDomain {
		if f.domainOf[id] == "" && !f.domainsDefault {
			m = append(m, fmt.Sprintf("--domain %s=<domain>  domain of vault %s (or --domains-default for every vault: its id)", id, id))
		}
	}
	if len(vaults) == 0 && (firstID == "" || f.path == "") {
		m = append(m, "--vault <id> --path <dir>  first vault")
	}
	if _, installed := installedArgs(service.DefaultName); !installed && !f.service && !f.noService {
		m = append(m, "--service or --no-service  install the service")
	}
	if f.agent == "generic" && f.agentName == "" {
		m = append(m, "--agent-name <name>        name of the generic agent")
	}
	return m
}

// would prints one planned action of a dry run.
func would(w io.Writer, format string, a ...any) {
	_, _ = fmt.Fprintf(w, "    would %s\n", fmt.Sprintf(format, a...))
}

func runSetup(l *loader, cmd *cobra.Command, f setupFlags) error {
	w := cmd.OutOrStdout()
	// --vault names the first vault: it is not registered yet, so doctor and the
	// kind step run without it.
	firstID := l.vaultFlag
	l.vaultFlag = ""
	f.domainOf = map[string]string{}
	for _, kv := range f.domains {
		id, d, ok := strings.Cut(kv, "=")
		if !ok || id == "" || !registry.ValidDomain(d) {
			return fmt.Errorf("setup: --domain %q must be <vault>=<domain>, the domain one lowercase word", kv)
		}
		f.domainOf[id] = d
	}
	for _, d := range f.allowDomains {
		if !registry.ValidDomain(d) {
			return fmt.Errorf("setup: --allow-domain %q must be one lowercase word", d)
		}
	}
	if f.service && f.noService {
		return errors.New("setup: --service and --no-service exclude each other")
	}
	switch f.agent {
	case "", "none", "claude", "dun", "generic":
	default:
		return fmt.Errorf("setup: --agent %q must be none, claude, dun, or generic", f.agent)
	}
	in := cmd.InOrStdin()
	tty := l.stdinIsTTY(in)
	p := &prompter{in: bufio.NewReader(in), out: w}

	vaults, err := l.install.List()
	if err != nil {
		return err
	}
	for id := range f.domainOf {
		if id != firstID && !slices.ContainsFunc(vaults, func(v registry.Vault) bool { return v.ID == id }) {
			return fmt.Errorf("setup: --domain %s=...: no vault %s is registered", id, id)
		}
	}
	if f.dryRun {
		_, _ = fmt.Fprintln(w, "dry run: nothing is written, installed, started, or run")
	}
	if !tty {
		if m := setupMissing(firstID, f, vaults, vaultsWithoutDomain(vaults)); len(m) > 0 {
			if !f.dryRun {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "setup: no terminal to ask on; pass:")
				for _, x := range m {
					_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "  "+x)
				}
				return errors.New("setup: missing answers")
			}
			_, _ = fmt.Fprintln(w, "no terminal to ask on; a real run needs:")
			for _, x := range m {
				_, _ = fmt.Fprintln(w, "  "+x)
			}
		}
	}

	if err := setupDoctor(l, cmd, p, tty); err != nil {
		return err
	}
	if err := setupDomains(l, cmd, p, tty, f, vaults); err != nil {
		return err
	}
	if err := setupVault(l, cmd, p, tty, f, firstID); err != nil {
		return err
	}
	l.vaultFlag = firstID
	if err := setupService(l, cmd, p, tty, f); err != nil {
		return err
	}
	if err := setupAgent(l, cmd, p, tty, f, firstID); err != nil {
		return err
	}
	_, _ = fmt.Fprintln(w)
	if f.dryRun {
		_ = verdict(w, toneWarn, "setup: dry run, nothing changed")
		return nil
	}
	_ = verdict(w, toneOK, "setup: done")
	_, err = fmt.Fprintln(w, "Last step: open your agent and run the `bootstrap` prompt (MCP prompt \"bootstrap\"; para bootstrap prints it) for the first interview.")
	return err
}

func setupDoctor(l *loader, cmd *cobra.Command, p *prompter, tty bool) error {
	w := cmd.OutOrStdout()
	for {
		var buf bytes.Buffer
		r := &report{w: &buf}
		doctorInstall(l, r)
		for _, line := range strings.Split(buf.String(), "\n") {
			if strings.HasPrefix(line, "fail") {
				_, _ = fmt.Fprintln(w, paint(w, toneErr, "fail")+strings.TrimPrefix(line, "fail"))
			}
		}
		if r.fails == 0 {
			return verdict(w, toneOK, "doctor: 0 failed, %d warnings (warnings are expected before setup)", r.warns)
		}
		_ = verdict(w, toneErr, "doctor: %d failed", r.fails)
		if !tty {
			return fmt.Errorf("setup: doctor: %d failed: fix them, then run para setup again", r.fails)
		}
		again, err := p.yesNo("Fix the lines above, then check again?", true)
		if err != nil {
			return err
		}
		if !again {
			return fmt.Errorf("setup: doctor: %d failed", r.fails)
		}
	}
}

// setupDomains gives each registered vault with no domain one (through
// vault.SetConfig), then records the union of the domains as machine.domains.
// Only the owner changes the machine list, and only here and in para init.
func setupDomains(l *loader, cmd *cobra.Command, p *prompter, tty bool, f setupFlags, vaults []registry.Vault) error {
	w := cmd.OutOrStdout()
	have, err := l.install.Domains()
	if err != nil {
		return err
	}
	install := ""
	if !f.dryRun {
		if install, err = l.install.ID(); err != nil {
			return err
		}
	}
	var chosen, existing []string // chosen: stamped or named now; existing: read from vaults
	for _, v := range vaults {
		cfg, err := props.Load(filepath.Join(v.Path, ".para", "vault.properties"))
		if err != nil {
			_ = verdict(w, toneWarn, "vault domain: %s not readable: %v", v.ID, err)
			continue
		}
		file := filepath.Join(v.Path, ".para", "vault.properties")
		if d := cfg[vault.KeyDomain]; d != "" {
			if fd := f.domainOf[v.ID]; fd != "" && fd != d {
				return fmt.Errorf("setup: vault %s is domain %s; --domain %s=%s would change it: edit %s yourself if you mean it", v.ID, d, v.ID, fd, file)
			}
			existing = append(existing, d)
			continue
		}
		d := f.domainOf[v.ID]
		switch {
		case d != "":
		case f.domainsDefault || (f.dryRun && !tty):
			d = v.ID
		default:
			for d == "" {
				a, err := p.ask("Domain of vault "+v.ID+" (one lowercase word: who controls its data, such as life, iode, corp)", v.ID)
				if err != nil {
					return err
				}
				if registry.ValidDomain(a) {
					d = a
				}
			}
		}
		if !registry.ValidDomain(d) {
			return fmt.Errorf("setup: vault %s: domain %q must be one lowercase word", v.ID, d)
		}
		if f.dryRun {
			_ = verdict(w, toneWarn, "domain: %s would be marked %s", v.ID, d)
			would(w, "write %s: %s=%s", file, vault.KeyDomain, d)
		} else {
			vv, err := vault.Open(v.Path, vault.Options{ID: v.ID, Actor: cliActor, Surface: cliSurface, Install: install, StateDir: l.stateDir(v.ID)})
			if err != nil {
				return fmt.Errorf("setup: vault domain: %w", err)
			}
			if err := vv.SetConfig(cmd.Context(), vault.KeyDomain, d); err != nil {
				return fmt.Errorf("setup: vault domain: %s: %w", v.ID, err)
			}
			_ = verdict(w, toneOK, "domain: %s marked %s", v.ID, d)
		}
		chosen = append(chosen, d)
	}
	chosen = append(chosen, f.allowDomains...)
	// A vault already marked with a domain the machine list lacks: the first
	// record takes it; a list that exists needs a yes.
	for _, d := range existing {
		switch {
		case slices.Contains(have, d) || slices.Contains(chosen, d):
		case len(have) == 0:
			chosen = append(chosen, d)
		case tty && !f.dryRun:
			yes, err := p.yesNo("A vault here is domain "+d+", which machine.domains lacks. Add it?", false)
			if err != nil {
				return err
			}
			if yes {
				chosen = append(chosen, d)
			}
		default:
			_ = verdict(w, toneWarn, "domain: a vault here is %s, not in machine.domains; para setup --allow-domain %s adds it", d, d)
		}
	}
	union := slices.Clone(have)
	for _, d := range chosen {
		if !slices.Contains(union, d) {
			union = append(union, d)
		}
	}
	file := filepath.Join(l.install.Home, "install.properties")
	if len(union) == 0 {
		return verdict(w, toneWarn, "domains: none yet; the first para init or para setup vault records one")
	}
	_, _ = fmt.Fprintf(w, "This machine holds domains: %s\n", strings.Join(union, ", "))
	if len(union) == len(have) {
		return verdict(w, toneOK, "domains: %s (recorded)", strings.Join(have, ", "))
	}
	if f.dryRun {
		_ = verdict(w, toneWarn, "domains: would be recorded")
		would(w, "write %s: machine.domains=%s", file, strings.Join(union, ","))
		return nil
	}
	if tty {
		yes, err := p.yesNo("Record these as machine.domains?", true)
		if err != nil {
			return err
		}
		if !yes {
			return verdict(w, toneWarn, "domains: not recorded; vaults open only while machine.domains is empty or lists their domain")
		}
	}
	if err := l.install.SetDomains(union); err != nil {
		return err
	}
	return verdict(w, toneOK, "domains: %s (recorded in %s)", strings.Join(union, ", "), file)
}

func setupVault(l *loader, cmd *cobra.Command, p *prompter, tty bool, f setupFlags, firstID string) error {
	w := cmd.OutOrStdout()
	vaults, err := l.install.List()
	if err != nil {
		return err
	}
	if len(vaults) > 0 {
		ids := make([]string, len(vaults))
		for i, v := range vaults {
			ids[i] = v.ID
		}
		return verdict(w, toneOK, "vault: %s (registered)", strings.Join(ids, ", "))
	}
	id, path := firstID, f.path
	if f.dryRun && !tty && (id == "" || path == "") {
		return verdict(w, toneWarn, "vault: none registered; a real run needs --vault <id> --path <dir>")
	}
	if id == "" {
		if id, err = p.ask("Id of the first vault (one lowercase word, such as life)", ""); err != nil {
			return err
		}
	}
	if path == "" {
		def := "~/para/" + id
		if home, err := os.UserHomeDir(); err == nil {
			def = filepath.Join(home, "para", id)
		}
		if path, err = p.ask("Directory of the vault", def); err != nil {
			return err
		}
	}
	domain := f.domainOf[id]
	if domain == "" {
		domain = id
	}
	if f.dryRun {
		_ = verdict(w, toneWarn, "vault: %s at %s would be created (default)", id, path)
		would(w, "create the vault folders and %s with vault.id=%s, %s=%s", filepath.Join(path, ".para", "vault.properties"), id, vault.KeyDomain, domain)
		would(w, "register it in %s and make it the default vault", l.install.Home)
		would(w, "write %s: machine.domains gains %s", filepath.Join(l.install.Home, "install.properties"), domain)
		return nil
	}
	v, err := createVault(l, id, path, domain, false, true)
	if err != nil {
		return err
	}
	if len(v.Added) > 0 {
		_ = verdict(w, toneOK, "domains: %s added to machine.domains", v.Domain)
	}
	return verdict(w, toneOK, "vault: %s at %s (default, domain %s)", v.ID, v.Path, v.Domain)
}

// waitServer waits up to d for a live server record.
func waitServer(home string, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for {
		if _, ok := server.ReadRecord(home); ok {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// subCommand runs another para command in-process on a copy of l.
func subCommand(l *loader, cmd *cobra.Command, args ...string) (string, error) {
	inner := *l
	root := newRoot(&inner)
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"--home", l.install.Home}, args...))
	err := root.ExecuteContext(cmd.Context())
	return out.String(), err
}

func setupService(l *loader, cmd *cobra.Command, p *prompter, tty bool, f setupFlags) error {
	w := cmd.OutOrStdout()
	_, installed := installedArgs(service.DefaultName)
	started := false
	switch {
	case installed:
		_ = verdict(w, toneOK, "service: installed (already)")
	default:
		want := f.service
		if !f.service && !f.noService {
			if f.dryRun && !tty {
				_ = verdict(w, toneWarn, "service: not chosen; a real run needs --service or --no-service")
				return setupSignIn(l, cmd, f, false)
			}
			var err error
			if want, err = p.yesNo("Install the service (starts the web UI at login, port 7700)?", true); err != nil {
				return err
			}
		}
		if !want {
			_ = verdict(w, toneWarn, "service: skipped; start the web UI with para serve, or para service install")
			break
		}
		exe, err := l.executable()
		if err != nil {
			return fmt.Errorf("setup: service: %w", err)
		}
		if f.dryRun {
			_ = verdict(w, toneWarn, "service: would be installed and started")
			would(w, "run: para service install --exec %s", exe)
			would(w, "run: para service start")
			return setupSignIn(l, cmd, f, true)
		}
		if out, err := subCommand(l, cmd, "service", "install", "--exec", exe); err != nil {
			return fmt.Errorf("setup: service install: %w\n%s", err, out)
		}
		if out, err := subCommand(l, cmd, "service", "start"); err != nil {
			return fmt.Errorf("setup: service start: %w\n%s", err, out)
		}
		started = true
		_ = verdict(w, toneOK, "service: installed and started")
	}
	if started {
		waitServer(l.install.Home, l.setupWait())
	}
	return setupSignIn(l, cmd, f, false)
}

// loginFresh reports whether every vault the server serves has a login key
// that is not revoked or expired.
func loginFresh(l *loader, at serverAt) (bool, error) {
	var ids []string
	if at.Rec != nil {
		ids = at.Rec.Vaults
	}
	if len(ids) == 0 {
		vs, err := l.install.List()
		if err != nil {
			return false, err
		}
		for _, v := range vs {
			ids = append(ids, v.ID)
		}
	}
	if len(ids) == 0 {
		return false, nil
	}
	for _, id := range ids {
		act, err := keys.New(server.ServerDir(l.install, id)).ActiveLogins()
		if err != nil {
			return false, err
		}
		if len(act) == 0 {
			return false, nil
		}
	}
	return true, nil
}

// setupSignIn signs the browser in, unless every served vault already has a
// login key that has not expired and --login is not given. serverComing is a
// dry run's service start.
func setupSignIn(l *loader, cmd *cobra.Command, f setupFlags, serverComing bool) error {
	w := cmd.OutOrStdout()
	rec, up := server.ReadRecord(l.install.Home)
	if f.dryRun && serverComing && !up {
		_ = verdict(w, toneWarn, "sign-in: would run para login once the server is up")
		would(w, "mint a login key per vault (vault:read, vault:write, rfx:draft, review:decide, 30 days) and open the browser")
		return nil
	}
	if !up {
		return verdict(w, toneWarn, "sign-in: no server is running. Start it: para service start, or para serve; then para login")
	}
	if !f.login {
		fresh, err := loginFresh(l, serverAt{Addr: rec.Addr, Source: "record", Rec: &rec})
		if err != nil {
			return err
		}
		if fresh {
			return verdict(w, toneOK, "sign-in: already (para login to sign in again)")
		}
	}
	if f.dryRun {
		_ = verdict(w, toneWarn, "sign-in: would run para login")
		would(w, "mint a login key per served vault (vault:read, vault:write, rfx:draft, review:decide, 30 days) and open the browser")
		return nil
	}
	if err := runLogin(l, cmd, "", f.print, false, keys.LoginTTL); err != nil {
		return verdict(w, toneWarn, "sign-in: %v", err)
	}
	return verdict(w, toneOK, "sign-in: done")
}

// claudeHooked reports whether Claude Code already has server name at user
// scope (~/.claude.json). It reads a file; it runs nothing.
func claudeHooked(name string) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	b, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		return false
	}
	var doc struct {
		MCPServers map[string]json.RawMessage `json:"mcpServers"`
	}
	if json.Unmarshal(b, &doc) != nil {
		return false
	}
	_, ok := doc.MCPServers[name]
	return ok
}

// setupSkill brings the installed para skill to this binary's version. A skill
// that is installed and of another version is updated whatever agent is chosen;
// a missing one is installed only when install is true.
func setupSkill(l *loader, cmd *cobra.Command, f setupFlags, install bool) error {
	w := cmd.OutOrStdout()
	have, ok := installedSkill("")
	switch {
	case ok && have == l.version:
		return nil
	case !ok && !install:
		return nil
	}
	dir, err := defaultSkillsDir()
	if err != nil {
		return err
	}
	if f.dryRun {
		if ok {
			_ = verdict(w, toneWarn, "agent: would update para skill (%s → %s)", skillVersionLabel(have), l.version)
		} else {
			_ = verdict(w, toneWarn, "agent: would install para skill (%s)", l.version)
		}
		would(w, "write %s and its reference files, %s=%s", filepath.Join(dir, "para", "SKILL.md"), skillVersionFile, l.version)
		return nil
	}
	if _, err := installSkill("", l.version); err != nil {
		return fmt.Errorf("setup: agent: skill install: %w", err)
	}
	if ok {
		return verdict(w, toneOK, "agent: para skill updated (%s → %s)", skillVersionLabel(have), l.version)
	}
	return verdict(w, toneOK, "agent: para skill installed")
}

func setupAgent(l *loader, cmd *cobra.Command, p *prompter, tty bool, f setupFlags, firstID string) error {
	w := cmd.OutOrStdout()
	agent := f.agent
	if agent == "" {
		agent = "none"
		if tty {
			yes, err := p.yesNo("Hook up an agent now? (optional)", false)
			if err != nil {
				return err
			}
			if yes {
				if agent, err = p.ask("Which: claude, dun, or generic (any MCP client over HTTP)", "claude"); err != nil {
					return err
				}
			}
		}
	}
	if agent != "claude" {
		if err := setupSkill(l, cmd, f, false); err != nil {
			return err
		}
	}
	if agent == "none" {
		_ = verdict(w, toneWarn, "agent: skipped; later: para setup --agent claude|dun|generic")
		return writeWorkerNote(cmd, l)
	}
	sel, err := l.selectVault()
	if err != nil {
		if f.dryRun && firstID != "" {
			_ = verdict(w, toneWarn, "agent: %s would be hooked up once vault %s exists", agent, firstID)
			return writeWorkerNote(cmd, l)
		}
		return fmt.Errorf("setup: agent: %w", err)
	}
	switch agent {
	case "dun":
		if err := writeHarnessHelp(cmd, l, "dun"); err != nil {
			return err
		}
		_ = verdict(w, toneWarn, "agent: dun has no add command; edit its config as printed above")
	case "claude":
		if err := setupClaude(l, cmd, p, tty, f, sel.ID); err != nil {
			return err
		}
	case "generic":
		if err := setupGeneric(l, cmd, p, f, sel.ID); err != nil {
			return err
		}
	default:
		return fmt.Errorf("setup: --agent %q must be none, claude, dun, or generic", agent)
	}
	return writeWorkerNote(cmd, l)
}

func setupClaude(l *loader, cmd *cobra.Command, p *prompter, tty bool, f setupFlags, id string) error {
	w := cmd.OutOrStdout()
	if rec, ok := server.ReadRecord(l.install.Home); ok && serviceInstalled() {
		base, err := clientBase(id, serverAt{Addr: rec.Addr, Source: "record", Rec: &rec})
		switch {
		case err != nil:
			_ = verdict(w, toneWarn, "agent: HTTP: %v; using stdio", err)
		case !loopbackBase(base):
			_ = verdict(w, toneWarn, "agent: the para service is at %s, not a loopback address: the token would cross the network; using stdio", base)
		default:
			return setupClaudeHTTP(l, cmd, p, tty, f, id, base)
		}
	}
	e, err := vaultMCPEntry(cmd, l, id)
	if err != nil {
		return err
	}
	line, err := claudeLine(e)
	if err != nil {
		return err
	}
	hooked := claudeHooked(e.Name)
	have, skillThere := installedSkill("")
	skillCurrent := skillThere && have == l.version
	if hooked && skillCurrent {
		return verdict(w, toneOK, "agent: Claude Code already has %s and the para skill", e.Name)
	}
	if _, err := exec.LookPath("claude"); err != nil {
		_ = verdict(w, toneWarn, "agent: claude is not on PATH; run these yourself, then restart the session:")
		if !hooked {
			_, _ = fmt.Fprintln(w, "  "+line)
		}
		if !skillCurrent {
			_, _ = fmt.Fprintln(w, "  para skill install")
		}
		return nil
	}
	if !hooked {
		_, _ = fmt.Fprintf(w, "  %s\n", line)
		yes := f.agent == "claude"
		if !yes && (!f.dryRun || tty) {
			if yes, err = p.yesNo("Run it?", false); err != nil {
				return err
			}
		}
		if f.dryRun {
			_ = verdict(w, toneWarn, "agent: would run claude mcp add for %s", e.Name)
			would(w, "run: %s", strings.Join(claudeAddArgs(withActor(e, "claude")), " "))
		} else {
			if !yes {
				return verdict(w, toneWarn, "agent: not run")
			}
			parts := claudeAddArgs(withActor(e, "claude"))
			c := exec.Command(parts[0], parts[1:]...)
			c.Stdout, c.Stderr = w, cmd.ErrOrStderr()
			if err := c.Run(); err != nil {
				return fmt.Errorf("setup: agent: claude mcp add: %w", err)
			}
			_ = verdict(w, toneOK, "agent: Claude Code has %s; restart the session and check /mcp", e.Name)
		}
	}
	if err := setupSkill(l, cmd, f, true); err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, "  Other harnesses: para help dun.")
	return err
}

// serviceInstalled reports whether the para service's unit file exists.
func serviceInstalled() bool {
	p, err := unitPath(service.DefaultName)
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

// claudeKeyName is the key, and so the actor, of Claude Code over HTTP. A
// key name is never reused, so a later key is claude-2, claude-3, and so on.
const claudeKeyName = "claude"

// loopbackKey is the allow list of the Claude Code key: it works from this
// machine only.
var loopbackKey = []string{"127.0.0.0/8", "::1"}

// loopbackBase reports whether base is a URL on this machine only:
// <vault>.localhost or a loopback address.
func loopbackBase(base string) bool {
	u, err := url.Parse(base)
	if err != nil {
		return false
	}
	h := u.Hostname()
	if h == "localhost" || strings.HasSuffix(h, ".localhost") {
		return true
	}
	a, err := netip.ParseAddr(h)
	return err == nil && a.Unmap().IsLoopback()
}

// claudeHTTPArgs is the claude mcp add command of the HTTP transport. The
// token goes in the header; Claude Code keeps it in its config, para keeps
// only its hash. claude mcp add reads the header from its arguments only, so
// the token is in this process's command line (/proc/<pid>/cmdline) while it
// runs: visible to local users of this machine for that moment. The key works
// from loopback only.
func claudeHTTPArgs(name, base, token string) []string {
	return []string{"claude", "mcp", "add", "--transport", "http", "--scope", "user", name,
		strings.TrimRight(base, "/") + "/mcp", "--header", "Authorization: Bearer " + token}
}

// claudeKeyFor picks the key name of a new Claude Code key: want when given,
// else the first of claude, claude-2, ... that no key has (a revoked key
// keeps its name). live is a key of that family that still works, if any.
func claudeKeyFor(existing []keys.Key, want string, now time.Time) (name, live string) {
	taken := map[string]bool{}
	for _, k := range existing {
		taken[k.Name] = true
		family := k.Name == claudeKeyName || strings.HasPrefix(k.Name, claudeKeyName+"-")
		if want == "" && family && live == "" && k.Revoked == nil && (k.Expires == nil || k.Expires.After(now)) {
			live = k.Name
		}
	}
	if want != "" {
		return want, ""
	}
	name = claudeKeyName
	for n := 2; taken[name]; n++ {
		name = claudeKeyName + "-" + strconv.Itoa(n)
	}
	return name, live
}

// setupClaudeHTTP hooks Claude Code to the running para service over HTTP
// with a new agent key: a service restart on an upgrade needs no reconnect.
// base is a loopback URL (setupClaude checked it).
func setupClaudeHTTP(l *loader, cmd *cobra.Command, p *prompter, tty bool, f setupFlags, id, base string) error {
	w := cmd.OutOrStdout()
	name := "para-" + id
	have, skillThere := installedSkill("")
	skillCurrent := skillThere && have == l.version
	if claudeHooked(name) {
		if skillCurrent {
			return verdict(w, toneOK, "agent: Claude Code already has %s and the para skill", name)
		}
		return setupSkill(l, cmd, f, true)
	}
	shown := strings.Join(claudeHTTPArgs(name, base, "<token>"), " ")
	ks := keys.New(server.ServerDir(l.install, id))
	existing, err := ks.List()
	if err != nil {
		return err
	}
	keyName, live := claudeKeyFor(existing, f.agentName, time.Now())
	if _, err := exec.LookPath("claude"); err != nil {
		_ = verdict(w, toneWarn, "agent: claude is not on PATH; make a key and run the line yourself, then restart the session:")
		_, _ = fmt.Fprintf(w, "  para --vault %s key create %s --scope agent --allow-ip 127.0.0.0/8 --allow-ip ::1\n  %s\n", id, keyName, shown)
		if !skillCurrent {
			_, _ = fmt.Fprintln(w, "  para skill install")
		}
		return nil
	}
	if live != "" {
		return verdict(w, toneWarn, "agent: key %s works and its token is not kept; para --vault %s key revoke %s, then run this again (or pass --agent-name <name>)", live, id, live)
	}
	_, _ = fmt.Fprintf(w, "  %s (HTTP: the para service runs; a new agent key %s, loopback only)\n", shown, keyName)
	yes := f.agent == "claude"
	if !yes && (!f.dryRun || tty) {
		if yes, err = p.yesNo("Run it?", false); err != nil {
			return err
		}
	}
	if f.dryRun {
		_ = verdict(w, toneWarn, "agent: would create key %s (scope agent, loopback only) and run claude mcp add --transport http for %s", keyName, name)
		would(w, "run: %s", shown)
		return nil
	}
	if !yes {
		return verdict(w, toneWarn, "agent: not run")
	}
	scopes, err := netguard.ExpandScopes([]string{"agent"})
	if err != nil {
		return err
	}
	_, tok, err := ks.Create(keyName, scopes, loopbackKey, 0)
	if err != nil {
		return fmt.Errorf("setup: agent: %w", err)
	}
	parts := claudeHTTPArgs(name, base, tok)
	c := exec.Command(parts[0], parts[1:]...)
	c.Stdout, c.Stderr = w, cmd.ErrOrStderr()
	if err := c.Run(); err != nil {
		// The token is lost with the failed run: revoke the key. A run again
		// makes the next free name.
		if _, rerr := ks.Revoke(keyName); rerr != nil {
			_, _ = fmt.Fprintf(w, "could not revoke key %s: %v\n", keyName, rerr)
		}
		return fmt.Errorf("setup: agent: claude mcp add: %w (key %s is revoked; run setup again)", err, keyName)
	}
	_ = verdict(w, toneOK, "agent: Claude Code has %s over HTTP (%s/mcp, key %s); restart the session and check /mcp", name, strings.TrimRight(base, "/"), keyName)
	if err := setupSkill(l, cmd, f, true); err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, "  Other harnesses: para help dun.")
	return err
}

func withActor(e mcpServerEntry, actor string) mcpServerEntry {
	e.Args = append(append([]string{}, e.Args...), "--actor", actor)
	return e
}

// setupGeneric makes an agent key for any MCP client over HTTP and prints its
// name, URL, and token. The token is shown once and written nowhere.
func setupGeneric(l *loader, cmd *cobra.Command, p *prompter, f setupFlags, id string) error {
	w := cmd.OutOrStdout()
	rec, ok := server.ReadRecord(l.install.Home)
	if !ok {
		return verdict(w, toneWarn, "agent: no server is running. Start it: para service start, or para serve; then para setup --agent generic --agent-name <name>")
	}
	name := f.agentName
	if name == "" && f.dryRun {
		return verdict(w, toneWarn, "agent: a real run needs --agent-name <name> for the generic agent")
	}
	if name == "" {
		var err error
		if name, err = p.ask("Name of the agent (also its actor)", ""); err != nil {
			return err
		}
	}
	base, err := clientBase(id, serverAt{Addr: rec.Addr, Source: "record", Rec: &rec})
	if err != nil {
		return fmt.Errorf("setup: agent: %w", err)
	}
	store := keys.New(server.ServerDir(l.install, id))
	existing, err := store.List()
	if err != nil {
		return err
	}
	for _, k := range existing {
		if k.Name == name {
			return verdict(w, toneWarn, "agent: key %s exists and its token is not kept; para key revoke %s, then run this again", name, name)
		}
	}
	scopes, err := netguard.ExpandScopes([]string{"agent"})
	if err != nil {
		return err
	}
	if f.dryRun {
		_ = verdict(w, toneWarn, "agent: would create key %s (scope agent)", name)
		would(w, "write the key store of vault %s under %s and print its name, URL, and token once", id, server.ServerDir(l.install, id))
		return nil
	}
	_, tok, err := store.Create(name, scopes, nil, 0)
	if err != nil {
		return fmt.Errorf("setup: agent: %w", err)
	}
	_ = verdict(w, toneOK, "agent: key %s created (scope agent)", name)
	return writeGenericBlock(w, name, base, tok)
}

func writeGenericBlock(w io.Writer, name, base, tok string) error {
	_, err := fmt.Fprintf(w, "  name:  %s\n  url:   %s/mcp\n  token: %s\n  The token is shown once and saved nowhere: put it in the client's secret store now.\n", name, strings.TrimRight(base, "/"), tok)
	return err
}

// writeWorkerNote says how an agent in another project reaches para.
func writeWorkerNote(cmd *cobra.Command, l *loader) error {
	_, err := fmt.Fprint(cmd.OutOrStdout(), "Agents in other projects: para mcp --read-only --vault <id> --actor <project>.\n"+
		"An agent with only a shell: para --actor <name> --why \"...\" <command>.\n")
	return err
}
