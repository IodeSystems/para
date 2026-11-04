package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/iodesystems/para/internal/service"
)

// runner runs a service manager command with its output on w. Tests replace it:
// no test may touch this machine's service manager.
type runner func(w io.Writer, name string, args ...string) error

func execRunner(w io.Writer, name string, args ...string) error {
	c := exec.Command(name, args...)
	c.Stdout, c.Stderr = w, w
	return c.Run()
}

func useLaunchd() bool { return runtime.GOOS == "darwin" }

func unitPath(name string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("service: home: %w", err)
	}
	u := service.Unit{Name: name}
	if useLaunchd() {
		return filepath.Join(service.LaunchAgentsDir(home), u.PlistFileName()), nil
	}
	return filepath.Join(service.UserUnitDir(home), u.UnitFileName()), nil
}

// installedArgs returns the serve arguments of the installed service.
func installedArgs(name string) ([]string, bool) {
	p, err := unitPath(name)
	if err != nil {
		return nil, false
	}
	return service.ExecArgs(p)
}

// localBase is the vault URL on this machine for a listen address.
func localBase(id, addr string) string {
	if addr == "" {
		addr = defaultAddr
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		port = "7700"
	}
	return "http://" + id + ".localhost:" + port
}

// vaultBase is the local URL of a vault, from the address the server listens on.
func (l *loader) vaultBase(id string) string {
	return localBase(id, l.findServer().Addr)
}

// signInURL puts the token in the fragment, which a browser never sends. The
// UI reads it, removes it, and exchanges it at POST /auth/session.
func signInURL(base, token string) string {
	return strings.TrimRight(base, "/") + "/#token=" + url.QueryEscape(token)
}

// localClient reaches *.localhost names at loopback, whatever the resolver does.
func localClient(timeout time.Duration) *http.Client { return localClientNoting(timeout, nil) }

// localClientNoting also calls note with the local address of each connection,
// which is the address the server sees as the client.
func localClientNoting(timeout time.Duration, note func(net.Addr)) *http.Client {
	d := &net.Dialer{Timeout: timeout}
	return &http.Client{Timeout: timeout, Transport: &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err == nil && (host == "localhost" || strings.HasSuffix(host, ".localhost")) {
				addr = net.JoinHostPort("127.0.0.1", port)
			}
			c, err := d.DialContext(ctx, network, addr)
			if err == nil && note != nil {
				note(c.LocalAddr())
			}
			return c, err
		},
	}}
}

func newServiceCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "service",
		Short: "Install and control para as a per-user service",
		Long: "Install and control `para serve` as a per-user service. Building is not part\n" +
			"of this: run `make install`, then `para service restart`.",
		Args: cobra.NoArgs,
	}
	run := runner(execRunner)
	cmd.AddCommand(newServiceInstallCmd(l), newServiceUninstallCmd(&run))
	for _, verb := range []string{"start", "stop", "restart", "status", "logs"} {
		cmd.AddCommand(newServiceCtlCmd(verb, &run))
	}
	cmd.AddCommand(newLoginCmd(l, "login-admin"))
	return cmd
}

func newServiceInstallCmd(l *loader) *cobra.Command {
	var (
		name, execBin string
		printIt       bool
	)
	cmd := &cobra.Command{
		Use:   "install [-- <serve flags>...]",
		Short: "Write the service definition (flags after -- go to `para serve`)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := l.load(); err != nil {
				return err
			}
			// Parse through serve's own flag set, then prove it can run: under
			// Restart=on-failure a bad flag repeats for ever, reported to nobody.
			inner := *l
			check := newRoot(&inner)
			check.SetArgs(append([]string{"--home", l.install.Home, "serve", "--check"}, serveArgs(l, args)...))
			check.SetOut(cmd.ErrOrStderr())
			check.SetErr(cmd.ErrOrStderr())
			if err := check.ExecuteContext(cmd.Context()); err != nil {
				return fmt.Errorf("service: install: serve flags: %w", err)
			}
			if execBin == "" {
				p, err := os.Executable()
				if err != nil {
					return fmt.Errorf("service: install: locate this binary (pass --exec): %w", err)
				}
				if r, err := filepath.EvalSymlinks(p); err == nil {
					p = r
				}
				execBin = p
			}
			if !filepath.IsAbs(execBin) {
				return fmt.Errorf("service: install: --exec %q must be absolute", execBin)
			}
			wd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("service: install: %w", err)
			}
			env := []string{"PARA_HOME=" + l.install.Home}
			if l.env("PATH") != "" {
				env = append(env, "PATH="+l.env("PATH"))
			}
			u := service.Unit{Name: name, Exec: execBin, Args: serveArgs(l, args), WorkingDir: wd, Env: env}
			home, err := os.UserHomeDir()
			if err != nil {
				return fmt.Errorf("service: install: %w", err)
			}
			text, dir, file := u.Render(), service.UserUnitDir(home), u.UnitFileName()
			if useLaunchd() {
				text, dir, file = u.RenderPlist(service.LogPath(home, name)), service.LaunchAgentsDir(home), u.PlistFileName()
			}
			if printIt {
				_, err := fmt.Fprint(cmd.OutOrStdout(), text)
				return err
			}
			p, err := service.Install(dir, file, text)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\nnext: para service start\n", p)
			return err
		},
	}
	f := cmd.Flags()
	f.StringVar(&name, "name", service.DefaultName, "service name")
	f.StringVar(&execBin, "exec", "", "para binary (default: this binary)")
	f.BoolVar(&printIt, "print", false, "print the definition instead of writing it")
	return cmd
}

// serveArgs carries a root --vault into the service, where it picks the bare
// host's vault.
func serveArgs(l *loader, args []string) []string {
	if l.vaultFlag == "" || service.FlagValue(args, "--vault") != "" {
		return args
	}
	return append(slices.Clone(args), "--vault", l.vaultFlag)
}

func newServiceUninstallCmd(run *runner) *cobra.Command {
	var name string
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Stop the service and remove its definition",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := unitPath(name)
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			if useLaunchd() {
				_ = (*run)(w, "launchctl", "bootout", launchdTarget(name))
			} else {
				_ = (*run)(w, "systemctl", "--user", "disable", "--now", name+".service")
			}
			if err := os.Remove(p); err != nil {
				return fmt.Errorf("service: uninstall: %w", err)
			}
			_, err = fmt.Fprintln(w, "removed", p)
			return err
		},
	}
	cmd.Flags().StringVar(&name, "name", service.DefaultName, "service name")
	return cmd
}

func launchdTarget(name string) string {
	return "gui/" + strconv.Itoa(os.Getuid()) + "/" + service.Unit{Name: name}.Label()
}

func newServiceCtlCmd(verb string, run *runner) *cobra.Command {
	short := map[string]string{
		"start": "Start the service", "stop": "Stop the service", "restart": "Restart the service",
		"status": "Show whether the service runs", "logs": "Show the recent log",
	}[verb]
	var name string
	cmd := &cobra.Command{
		Use:   verb,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			p, err := unitPath(name)
			if err != nil {
				return err
			}
			if _, err := os.Stat(p); err != nil {
				return fmt.Errorf("service: %s: not installed: run para service install", verb)
			}
			return serviceCtl(cmd.OutOrStdout(), *run, verb, name, p)
		},
	}
	cmd.Flags().StringVar(&name, "name", service.DefaultName, "service name")
	return cmd
}

func serviceCtl(w io.Writer, run runner, verb, name, file string) error {
	if !useLaunchd() {
		if verb == "logs" {
			return run(w, "journalctl", "--user", "-u", name+".service", "-n", "200", "--no-pager")
		}
		if verb == "start" {
			// A service that does not come back after a reboot is not a service.
			return run(w, "systemctl", "--user", "enable", "--now", name+".service")
		}
		return run(w, "systemctl", "--user", verb, name+".service")
	}
	target := launchdTarget(name)
	switch verb {
	case "start":
		return run(w, "launchctl", "bootstrap", "gui/"+strconv.Itoa(os.Getuid()), file)
	case "stop":
		return run(w, "launchctl", "bootout", target)
	case "restart":
		return run(w, "launchctl", "kickstart", "-k", target)
	case "status":
		return run(w, "launchctl", "print", target)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("service: logs: %w", err)
	}
	return run(w, "tail", "-n", "200", service.LogPath(home, name))
}

func openBrowser(l *loader, u string) error {
	if runtime.GOOS == "darwin" {
		return exec.Command("open", u).Run()
	}
	if l.env("DISPLAY") == "" && l.env("WAYLAND_DISPLAY") == "" {
		return errors.New("no display")
	}
	return exec.Command("xdg-open", u).Run()
}
