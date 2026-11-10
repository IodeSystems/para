package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/iodesystems/para/internal/keys"
	"github.com/iodesystems/para/internal/netguard"
	"github.com/iodesystems/para/internal/server"
	"github.com/iodesystems/para/internal/service"
)

// serverAt is where a para serve listens and how that was found.
type serverAt struct {
	Addr   string
	Source string // "record", "service", or "default"
	Rec    *server.Record
}

// findServer looks in the runtime record of a live server, then the installed
// service unit, then the default. l.load must have run.
func (l *loader) findServer() serverAt {
	if rec, ok := server.ReadRecord(l.install.Home); ok {
		return serverAt{Addr: rec.Addr, Source: "record", Rec: &rec}
	}
	if args, installed := installedArgs(service.DefaultName); installed {
		if a := clientAddr(service.FlagValues(args, "--addr")); a != "" {
			return serverAt{Addr: a, Source: "service"}
		}
	}
	return serverAt{Addr: defaultAddr, Source: "default"}
}

// clientBase is the URL of vault id that passes the gate from this machine. A
// loopback bind is reached as <vault>.localhost:<port>, which names the vault
// whatever the bare host serves. Any other bind is reached at its own address,
// the bare host, which serves one vault. A bind on every address is reached
// through loopback.
func clientBase(id string, at serverAt) (string, error) {
	ap, err := netip.ParseAddrPort(at.Addr)
	if err != nil {
		return "", fmt.Errorf("listen address %q is not ip:port: %w", at.Addr, err)
	}
	port := strconv.Itoa(int(ap.Port()))
	a := ap.Addr().Unmap()
	if a.IsUnspecified() {
		a = netip.MustParseAddr("127.0.0.1")
	}
	if a.Is4() && a.IsLoopback() {
		return "http://" + id + ".localhost:" + port, nil
	}
	if at.Rec != nil && at.Rec.Vault != id {
		if at.Rec.Vault == "" {
			return "", fmt.Errorf("the server at %s serves no vault at its bare address: restart it with --vault %s", at.Addr, id)
		}
		return "", fmt.Errorf("the server at %s serves vault %s at its bare address, not %s: use --vault %s, or restart the server with --vault %s", at.Addr, at.Rec.Vault, id, at.Rec.Vault, id)
	}
	return "http://" + net.JoinHostPort(a.String(), port), nil
}

// Probe results where the server answered and said no.
var (
	errRefused     = errors.New("refused")     // 403: net.rules
	errMisdirected = errors.New("misdirected") // 421: the host names no vault
)

// probeHealth asks /healthz of base.
func probeHealth(base string) error {
	_, err := probeFrom(base)
	return err
}

// probeFrom is probeHealth that also returns the local address of the
// connection: the address the server sees as the client.
func probeFrom(base string) (string, error) {
	var from string
	c := localClientNoting(5*time.Second, func(a net.Addr) {
		if ta, ok := a.(*net.TCPAddr); ok {
			from = ta.IP.String()
		}
	})
	resp, err := c.Get(strings.TrimRight(base, "/") + "/healthz")
	if err != nil {
		return from, err
	}
	defer func() { _ = resp.Body.Close() }()
	switch resp.StatusCode {
	case http.StatusOK:
		return from, nil
	case http.StatusForbidden:
		return from, errRefused
	case http.StatusMisdirectedRequest:
		return from, errMisdirected
	}
	return from, errors.New(resp.Status)
}

// isLocalURL reports whether base names this machine. The admin token works
// only for a direct connection from this machine.
func isLocalURL(base string) bool {
	host, _, _ := strings.Cut(strings.TrimPrefix(strings.TrimPrefix(base, "http://"), "https://"), "/")
	h := host
	if hh, _, err := net.SplitHostPort(host); err == nil {
		h = hh
	}
	h = strings.Trim(h, "[]")
	if isLoopbackHost(h) {
		return true
	}
	if a, err := netip.ParseAddr(h); err == nil {
		return netguard.OwnAddr(a)
	}
	addrs, err := net.LookupHost(h)
	if err != nil || len(addrs) == 0 {
		return false
	}
	for _, s := range addrs {
		a, err := netip.ParseAddr(s)
		if err != nil || !netguard.OwnAddr(a) {
			return false
		}
	}
	return true
}

// newLoginCmd opens the UI of every vault the server serves, signed in. The
// web app reads and decides review items, so each key is a new login key with
// vault:read and review:decide that ends in 30 days (--days) and that the web
// app renews near its end. --admin signs in with the admin token instead.
// `para serve login` and `para service login-admin` are this one command.
func newLoginCmd(l *loader, use string) *cobra.Command {
	var (
		base    string
		printIt bool
		admin   bool
		days    int
	)
	cmd := &cobra.Command{
		Use:   use,
		Short: "Sign the browser in to every vault the server serves (--vault: one), with a 30-day login key that renews",
		Long: "Open the UI of each vault the server serves, signed in with a new login key:\n" +
			"vault:read and review:decide, ending in 30 days (--days, at most 90). The web\n" +
			"app renews the key when it is within 7 days of its end, so a browser that\n" +
			"opens para at least once a month stays signed in. Each vault is its own\n" +
			"origin, so each opens in its own tab. --vault signs in to one vault only.\n" +
			"The server is found from $PARA_HOME/var/serve.json, then the installed\n" +
			"service, then 127.0.0.1:7700. The token goes in the URL fragment, which a\n" +
			"browser never sends. This works from this machine only; for another\n" +
			"device use para key create <device> --url <its address> --scope web, and\n" +
			"save the key in a password manager (it does not\n" +
			"renew, so it stays the same). --admin signs in with the admin token instead. para serve\n" +
			"sessions lists the login keys; para serve logout revokes them.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if days < 1 || days > int(keys.LoginMaxTTL/(24*time.Hour)) {
				return fmt.Errorf("serve: login: --days must be 1 to %d", int(keys.LoginMaxTTL/(24*time.Hour)))
			}
			return runLogin(l, cmd, base, printIt, admin, time.Duration(days)*24*time.Hour)
		},
	}
	f := cmd.Flags()
	f.StringVar(&base, "url", "", "base URL (default: found from the running server); needs one vault")
	f.BoolVar(&printIt, "print", false, "print the sign-in URLs (they hold the tokens) instead of opening them")
	f.BoolVar(&admin, "admin", false, "sign in with the admin token instead of a new key")
	f.IntVar(&days, "days", int(keys.LoginTTL/(24*time.Hour)), "life of the login key in days (at most 90)")
	return cmd
}

// loginVaults are the vaults `para serve login` signs in to: --vault, else every
// vault of the running server, else the selected vault.
func (l *loader) loginVaults(at serverAt) ([]string, error) {
	if l.vaultFlag == "" && at.Rec != nil && len(at.Rec.Vaults) > 0 {
		return at.Rec.Vaults, nil
	}
	sel, err := l.selectVault()
	if err != nil {
		return nil, err
	}
	return []string{sel.ID}, nil
}

func runLogin(l *loader, cmd *cobra.Command, base string, printIt, admin bool, ttl time.Duration) error {
	if err := l.load(); err != nil {
		return err
	}
	at := l.findServer()
	if base == "" && at.Rec == nil {
		// No live server record: a key minted now would sign in to nothing.
		return fmt.Errorf("serve: login: no server is running (no live record in %s). Start it: para service start, or para serve", server.RecordPath(l.install.Home))
	}
	ids, err := l.loginVaults(at)
	if err != nil {
		return err
	}
	if base != "" && len(ids) > 1 {
		return fmt.Errorf("serve: login: --url names one server address; add --vault <id>")
	}
	w := cmd.OutOrStdout()
	signed := 0
	var lastErr error
	for _, id := range ids {
		u, vb, err := l.loginOne(id, at, base, admin, ttl)
		if err != nil {
			// One vault the bare address cannot reach does not stop the others.
			if len(ids) > 1 {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "%s: not signed in: %v\n", id, err)
				lastErr = err
				continue
			}
			return err
		}
		if printIt {
			if _, err := fmt.Fprintln(w, u); err != nil {
				return err
			}
			signed++
			continue
		}
		if err := openBrowser(l, u); err != nil {
			if _, err := fmt.Fprintf(w, "%s: could not open a browser (%v); open this:\n%s\n", id, err, u); err != nil {
				return err
			}
			continue
		}
		if _, err := fmt.Fprintf(w, "%s: signed in, opened %s\n", id, vb); err != nil {
			return err
		}
		signed++
	}
	if signed == 0 && lastErr != nil {
		return lastErr
	}
	return nil
}

// loginOne mints the key of one vault and returns its sign-in URL and base.
func (l *loader) loginOne(id string, at serverAt, base string, admin bool, ttl time.Duration) (string, string, error) {
	store := keys.New(server.ServerDir(l.install, id))
	tok, err := store.AdminToken()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", "", fmt.Errorf("serve: login: vault %s has no admin token: para serve creates it on first start", id)
		}
		return "", "", err
	}
	if base == "" {
		if base, err = clientBase(id, at); err != nil {
			return "", "", fmt.Errorf("serve: login: %w", err)
		}
	}
	if !isLocalURL(base) {
		return "", "", fmt.Errorf("serve: login: %s is not this machine, and the admin token works from this machine only. For another device run: para key create <device> --url %s --scope web, and paste the key it prints into the sign-in page", base, base)
	}
	from, err := probeFrom(base)
	switch {
	case errors.Is(err, errRefused):
		rules := filepath.Join(l.install.Home, "vaults", id, "server", "net.rules")
		line := "allow / <address>"
		if from != "" {
			line = "allow / " + from
		}
		return "", "", fmt.Errorf("serve: login: %s refused this machine (403): net.rules does not allow it. Add this line to %s:\n  %s", base, rules, line)
	case errors.Is(err, errMisdirected):
		return "", "", fmt.Errorf("serve: login: %s answered 421: its host does not name a vault. Use --vault <id> with a vault the server has, or restart the server with --vault <id>", base)
	case err != nil:
		return "", "", fmt.Errorf("serve: login: nothing answers at %s (%v). No running server was found in %s (looked at: %s). Start it: para service start, or para serve", base, err, server.RecordPath(l.install.Home), at.Source)
	}
	if !admin {
		if _, tok, err = store.CreateLogin(loginScopes, ttl); err != nil {
			return "", "", fmt.Errorf("serve: login: %w", err)
		}
	}
	return signInURL(base, tok), base, nil
}

// loginStores are the key stores of --vault, or of every vault on this install.
func (l *loader) loginStores() ([]string, error) {
	if err := l.load(); err != nil {
		return nil, err
	}
	if l.vaultFlag != "" {
		sel, err := l.selectVault()
		if err != nil {
			return nil, err
		}
		return []string{sel.ID}, nil
	}
	list, err := l.install.List()
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(list))
	for _, v := range list {
		ids = append(ids, v.ID)
	}
	return ids, nil
}

// loginSession is one active login key, for `para serve sessions`.
type loginSession struct {
	ID       string     `json:"id"`
	Vault    string     `json:"vault"`
	Created  time.Time  `json:"created"`
	Expires  *time.Time `json:"expires,omitempty"`
	LastUsed *time.Time `json:"last_used,omitempty"`
}

func newServeSessionsCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sessions",
		Short: "List the active login keys of para serve login (--vault: one vault)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ids, err := l.loginStores()
			if err != nil {
				return err
			}
			out := []loginSession{}
			for _, id := range ids {
				st := keys.New(server.ServerDir(l.install, id))
				act, err := st.ActiveLogins()
				if err != nil {
					return err
				}
				for _, k := range act {
					s := loginSession{ID: k.Name, Vault: id, Created: k.Created, Expires: k.Expires}
					if u := st.LastUsed(k.Name); !u.IsZero() {
						s.LastUsed = &u
					}
					out = append(out, s)
				}
			}
			w := cmd.OutOrStdout()
			if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
				enc := json.NewEncoder(w)
				enc.SetIndent("", "  ")
				return enc.Encode(out)
			}
			p := &printer{w: w}
			if len(out) == 0 {
				p.f("no active login keys: run para serve login\n")
			}
			day := func(t *time.Time) string {
				if t == nil {
					return "-"
				}
				return t.Local().Format("2006-01-02 15:04")
			}
			for _, s := range out {
				p.f("%s  %s  created %s  expires %s  last used %s\n", s.ID, s.Vault, day(&s.Created), day(s.Expires), day(s.LastUsed))
			}
			return p.err
		},
	}
	addJSONFlag(cmd)
	return cmd
}

func newServeLogoutCmd(l *loader) *cobra.Command {
	return &cobra.Command{
		Use:   "logout",
		Short: "Revoke the login keys of para serve login, in every vault (--vault: one)",
		Long: "Revoke every active login key made by para serve login or para open, in\n" +
			"every vault on this install, or in --vault only. Browsers signed in with\n" +
			"them are signed out at their next request. Other keys are not touched.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ids, err := l.loginStores()
			if err != nil {
				return err
			}
			p := &printer{w: cmd.OutOrStdout()}
			n := 0
			for _, id := range ids {
				st := keys.New(server.ServerDir(l.install, id))
				act, err := st.ActiveLogins()
				if err != nil {
					return err
				}
				for _, k := range act {
					if _, err := st.Revoke(k.Name); err != nil {
						return err
					}
					p.f("revoked %s (%s)\n", k.Name, id)
					n++
				}
			}
			if n == 0 {
				p.f("no active login keys\n")
			}
			return p.err
		},
	}
}

// serveStatus is the output of `para serve status`.
type serveStatus struct {
	Running   bool      `json:"running"`
	PID       int       `json:"pid,omitempty"`
	Addr      string    `json:"addr,omitempty"`
	Addrs     []string  `json:"addrs,omitempty"`
	Version   string    `json:"version,omitempty"`
	Vault     string    `json:"vault,omitempty"`
	Vaults    []string  `json:"vaults,omitempty"`
	BaseHosts []string  `json:"base_hosts,omitempty"`
	Started   time.Time `json:"started,omitzero"`
	URL       string    `json:"url,omitempty"`
	Health    string    `json:"health,omitempty"`
}

func newServeStatusCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the running server: pid, address, vaults, version, URL, health",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := l.load(); err != nil {
				return err
			}
			asJSON, _ := cmd.Flags().GetBool("json")
			w := cmd.OutOrStdout()
			rec, ok := server.ReadRecord(l.install.Home)
			if !ok {
				if asJSON {
					_ = json.NewEncoder(w).Encode(serveStatus{})
				}
				return fmt.Errorf("serve: status: no server is running (no live record in %s). Start it: para service start, or para serve", server.RecordPath(l.install.Home))
			}
			st := serveStatus{Running: true, PID: rec.PID, Addr: rec.Addr, Addrs: rec.Addrs, Version: rec.Version, Vault: rec.Vault,
				Vaults: rec.Vaults, BaseHosts: rec.BaseHosts, Started: rec.Started}
			id := l.vaultFlag
			if id == "" {
				id = rec.Vault
			}
			if id == "" && len(rec.Vaults) == 1 {
				id = rec.Vaults[0]
			}
			if id == "" {
				st.Health = "unknown: no vault for a bare address; pass --vault <id>"
			} else if base, err := clientBase(id, serverAt{Addr: rec.Addr, Source: "record", Rec: &rec}); err != nil {
				st.Health = "unknown: " + err.Error()
			} else {
				st.URL = base
				switch err := probeHealth(base); {
				case err == nil:
					st.Health = "ok"
				case errors.Is(err, errRefused):
					st.Health = "refused (403): net.rules does not allow this machine; see para serve login"
				default:
					st.Health = err.Error()
				}
			}
			if asJSON {
				enc := json.NewEncoder(w)
				enc.SetIndent("", "  ")
				return enc.Encode(st)
			}
			p := &printer{w: w}
			p.f("pid      %d\n", st.PID)
			p.f("address  %s\n", st.Addr)
			if len(st.Addrs) > 1 {
				p.f("listens  %s\n", strings.Join(st.Addrs, ", "))
			}
			p.f("version  %s\n", st.Version)
			p.f("started  %s\n", st.Started.Local().Format(time.RFC3339))
			p.f("vaults   %s\n", strings.Join(st.Vaults, ", "))
			if st.Vault != "" {
				p.f("bare     %s\n", st.Vault)
			}
			if len(st.BaseHosts) > 0 {
				p.f("bases    %s\n", strings.Join(st.BaseHosts, ", "))
			}
			if st.URL != "" {
				p.f("url      %s\n", st.URL)
			}
			p.f("health   %s\n", st.Health)
			return p.err
		},
	}
	addJSONFlag(cmd)
	return cmd
}
