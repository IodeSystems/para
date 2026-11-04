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
		if a := service.FlagValue(args, "--addr"); a != "" {
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

// newLoginCmd opens the vault's UI signed in. The web app reads and decides
// review items, so the key is a new vault:read and review:decide key that ends
// in a day; --admin signs
// in with the admin token instead. `para serve login` and `para service
// login-admin` are this one command.
func newLoginCmd(l *loader, use string) *cobra.Command {
	var (
		base    string
		printIt bool
		admin   bool
	)
	cmd := &cobra.Command{
		Use:   use,
		Short: "Open the UI signed in with a new vault:read, review:decide key (--admin: the admin token)",
		Long: "Open the UI signed in with a new vault:read and review:decide key that ends in\n" +
			"24 hours. The web app reads, and decides what agents put in the review stack. The server is found from $PARA_HOME/var/serve.json, then\n" +
			"the installed service, then 127.0.0.1:7700. The token goes in the URL\n" +
			"fragment, which a browser never sends. This works from this machine only;\n" +
			"for another machine use para key create <name> --scope vault:read --print-url.\n" +
			"--admin signs in with the admin token instead.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLogin(l, cmd, base, printIt, admin)
		},
	}
	f := cmd.Flags()
	f.StringVar(&base, "url", "", "base URL (default: found from the running server)")
	f.BoolVar(&printIt, "print", false, "print the sign-in URL (it holds the token) instead of opening it")
	f.BoolVar(&admin, "admin", false, "sign in with the admin token instead of a new key")
	return cmd
}

func runLogin(l *loader, cmd *cobra.Command, base string, printIt, admin bool) error {
	store, id, err := l.keyStore()
	if err != nil {
		return err
	}
	tok, err := store.AdminToken()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("serve: login: vault %s has no admin token: para serve creates it on first start", id)
		}
		return err
	}
	var mint func() (string, error)
	if !admin {
		mint = func() (string, error) {
			name := "viewer-" + time.Now().UTC().Format("20060102-150405")
			_, t, err := store.Create(name, []string{netguard.ScopeVaultRead, netguard.ScopeReviewDecide}, nil, 24*time.Hour)
			return t, err
		}
	}
	at := l.findServer()
	if base == "" {
		if base, err = clientBase(id, at); err != nil {
			return fmt.Errorf("serve: login: %w", err)
		}
	}
	if !isLocalURL(base) {
		return fmt.Errorf("serve: login: %s is not this machine, and the admin token works from this machine only. For another machine run: para key create <name> --scope vault:read --scope vault:write --print-url --url %s", base, base)
	}
	from, err := probeFrom(base)
	switch {
	case errors.Is(err, errRefused):
		rules := filepath.Join(l.install.Home, "vaults", id, "server", "net.rules")
		line := "allow / <address>"
		if from != "" {
			line = "allow / " + from
		}
		return fmt.Errorf("serve: login: %s refused this machine (403): net.rules does not allow it. Add this line to %s:\n  %s", base, rules, line)
	case errors.Is(err, errMisdirected):
		return fmt.Errorf("serve: login: %s answered 421: its host does not name a vault. Use --vault <id> with a vault the server has, or restart the server with --vault <id>", base)
	case err != nil:
		return fmt.Errorf("serve: login: nothing answers at %s (%v). No running server was found in %s (looked at: %s). Start it: para service start, or para serve", base, err, server.RecordPath(l.install.Home), at.Source)
	}
	if mint != nil {
		if tok, err = mint(); err != nil {
			return fmt.Errorf("serve: login: %w", err)
		}
	}
	target := signInURL(base, tok)
	w := cmd.OutOrStdout()
	if printIt {
		_, err := fmt.Fprintln(w, target)
		return err
	}
	if err := openBrowser(l, target); err != nil {
		_, err = fmt.Fprintf(w, "could not open a browser (%v); open this:\n%s\n", err, target)
		return err
	}
	_, err = fmt.Fprintf(w, "opened %s in the browser\n", base)
	return err
}

// serveStatus is the output of `para serve status`.
type serveStatus struct {
	Running   bool      `json:"running"`
	PID       int       `json:"pid,omitempty"`
	Addr      string    `json:"addr,omitempty"`
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
			st := serveStatus{Running: true, PID: rec.PID, Addr: rec.Addr, Version: rec.Version, Vault: rec.Vault,
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
