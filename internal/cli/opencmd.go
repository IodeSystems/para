package cli

import (
	"errors"
	"io"
	"os/exec"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/iodesystems/para/internal/ops"
	"github.com/iodesystems/para/internal/server"
)

// openURL opens a URL in a browser on this machine. Tests replace it, so no
// test opens anything.
var openURL = systemOpen

// systemOpen starts xdg-open on Linux and open on macOS. It does not wait for
// the browser.
func systemOpen(url string) (string, error) {
	var name string
	switch runtime.GOOS {
	case "linux", "freebsd", "openbsd", "netbsd":
		name = "xdg-open"
	case "darwin":
		name = "open"
	default:
		return "", errors.New("no browser opener for " + runtime.GOOS)
	}
	command := name + " " + url
	c := exec.Command(name, url)
	if err := c.Start(); err != nil {
		return command, err
	}
	go func() { _ = c.Wait() }()
	return command, nil
}

// viewer returns the deps hooks that find this machine's para serve for vault
// id and open its pages.
func (l *loader) viewer(id string) (func() (string, error), func(string) (string, error)) {
	base := func() (string, error) {
		rec, ok := server.ReadRecord(l.install.Home)
		if !ok {
			return "", ops.ErrNoServer
		}
		return clientBase(id, serverAt{Addr: rec.Addr, Source: "record", Rec: &rec})
	}
	return base, func(url string) (string, error) { return openURL(url) }
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
			if out.Note != "" {
				p.f("%s\n", out.Note)
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
	cmd.Short = "Open the web page of a proposal or a document in a browser on this machine; prints the URL"
	return cmd
}
