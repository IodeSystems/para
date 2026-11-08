package cli

import (
	"errors"
	"io"
	neturl "net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/spf13/cobra"

	"github.com/iodesystems/para/internal/keys"
	"github.com/iodesystems/para/internal/netguard"
	"github.com/iodesystems/para/internal/ops"
	"github.com/iodesystems/para/internal/server"
)

// openURL opens a URL in a browser on this machine. Tests replace it, so no
// test opens anything.
var openURL = systemOpen

// systemOpen starts xdg-open on Linux and open on macOS. It does not wait for
// the browser. When it cannot open, the error is an *ops.OpenError that says
// why and what to do.
func systemOpen(url string) (string, error) {
	name, err := opener(runtime.GOOS, os.Getenv, exec.LookPath)
	if err != nil {
		return "", err
	}
	command := name + " " + url
	c := exec.Command(name, url)
	if err := c.Start(); err != nil {
		return command, &ops.OpenError{Reason: ops.OpenFailed, Detail: name + " failed (" + err.Error() + "): open the URL by hand"}
	}
	go func() { _ = c.Wait() }()
	return command, nil
}

// opener picks the command that opens a browser, or says why there is none.
func opener(goos string, getenv func(string) string, lookPath func(string) (string, error)) (string, error) {
	var name string
	switch goos {
	case "linux", "freebsd", "openbsd", "netbsd":
		name = "xdg-open"
		if getenv("DISPLAY") == "" && getenv("WAYLAND_DISPLAY") == "" {
			if getenv("SSH_CONNECTION") != "" || getenv("SSH_CLIENT") != "" {
				return "", &ops.OpenError{Reason: ops.OpenRemote, Detail: "para runs in an SSH session with no graphical display, not on the owner's desktop: give the owner the URL, or run para open on the owner's machine"}
			}
			return "", &ops.OpenError{Reason: ops.OpenNoDisplay, Detail: "no graphical session here (DISPLAY and WAYLAND_DISPLAY are not set): give the owner the URL, or run para open from a desktop terminal"}
		}
	case "darwin":
		name = "open"
	default:
		return "", &ops.OpenError{Reason: ops.OpenNoOpener, Detail: "para has no browser opener for " + goos + ": open the URL by hand"}
	}
	if _, err := lookPath(name); err != nil {
		return "", &ops.OpenError{Reason: ops.OpenNoOpener, Detail: name + " is not installed: install it (xdg-utils on Linux), or open the URL by hand"}
	}
	return name, nil
}

// loginScopes are the scopes of a browser login key.
var loginScopes = netguard.ScopeSets["web"]

// viewer returns the deps hooks that find this machine's para serve for vault
// id and open its pages. With signIn (the owner at the CLI), a page of this
// vault's local server opens signed in when no login key of the vault is
// active: a new login key goes in the URL fragment of the browser command
// line only. The command and error returned hold the plain URL. Without
// signIn (an agent), the page opens plain and signedIn says whether the owner
// holds an active login key.
func (l *loader) viewer(id string, signIn bool) (base func() (string, error), open func(string) (string, error), signedIn func() bool) {
	base = func() (string, error) {
		rec, ok := server.ReadRecord(l.install.Home)
		if !ok {
			return "", ops.ErrNoServer
		}
		return clientBase(id, serverAt{Addr: rec.Addr, Source: "record", Rec: &rec})
	}
	store := func() *keys.Store { return keys.New(server.ServerDir(l.install, id)) }
	if !signIn {
		signedIn = func() bool {
			act, err := store().ActiveLogins()
			return err == nil && len(act) > 0
		}
		return base, func(url string) (string, error) { return openURL(url) }, signedIn
	}
	open = func(url string) (string, error) {
		target := url
		if b, err := base(); err == nil && sameOriginURL(url, b) {
			if act, err := store().ActiveLogins(); err == nil && len(act) == 0 {
				if _, tok, err := store().CreateLogin(loginScopes, keys.LoginTTL); err == nil {
					target = withToken(url, tok)
				}
			}
		}
		cmd, err := openURL(target)
		if target != url {
			cmd = strings.ReplaceAll(cmd, target, url)
			if err != nil && strings.Contains(err.Error(), target) {
				err = errors.New(strings.ReplaceAll(err.Error(), target, url))
			}
		}
		return cmd, err
	}
	return base, open, nil
}

// withToken puts a sign-in token in the fragment of u.
func withToken(u, tok string) string {
	sep := "#"
	if strings.Contains(u, "#") {
		sep = "&"
	}
	return u + sep + "token=" + neturl.QueryEscape(tok)
}

// sameOriginURL reports whether u is on the scheme, host, and port of base.
func sameOriginURL(u, base string) bool {
	a, err := neturl.Parse(u)
	if err != nil {
		return false
	}
	b, err := neturl.Parse(base)
	if err != nil {
		return false
	}
	return a.Scheme == b.Scheme && strings.EqualFold(a.Host, b.Host)
}

// newOpenCmd is `para proposal open`, the command of op proposal_open.
func newOpenCmd(l *loader, use string) *cobra.Command {
	return opCommand(l, ops.ProposalOpen, binding[ops.ProposalOpenInput, ops.ProposalOpenOutput]{
		Use:  use,
		Args: cobra.ExactArgs(1),
		Bind: func(args []string, in *ops.ProposalOpenInput) error { in.ID = args[0]; return nil },
		Text: func(w io.Writer, out ops.ProposalOpenOutput) error {
			p := &printer{w: w}
			if out.URL != "" {
				p.f("%s\n", out.URL)
			}
			if out.Opened {
				p.f("opened in the browser\n")
			} else {
				p.f("not opened (%s)\n", out.Reason)
			}
			if out.Note != "" {
				p.f("%s\n", out.Note)
			}
			if out.URLNote != "" {
				p.f("%s\n", out.URLNote)
			}
			return p.err
		},
	})
}

// newTopOpenCmd is `para open <id|url>`: proposal_open with a short name. It
// carries no op annotation, so the op keeps one command at its own path.
func newTopOpenCmd(l *loader) *cobra.Command {
	cmd := newOpenCmd(l, "open <id|url>")
	delete(cmd.Annotations, opAnnotation)
	cmd.Short = "Open the web page of a proposal or a document in a browser on this machine when one is available, signed in; otherwise only prints the URL"
	return cmd
}
