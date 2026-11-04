package export

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	qrcode "github.com/skip2/go-qrcode"

	"github.com/iodesystems/para/internal/atomicfile"
)

// Kind is a transport name.
type Kind string

// Transports.
const (
	Stdout    Kind = "stdout"
	File      Kind = "file"
	Clipboard Kind = "clipboard"
	OSC52     Kind = "osc52"
	Tmux      Kind = "tmux"
	Wormhole  Kind = "wormhole"
	QR        Kind = "qr"
	SSH       Kind = "ssh"
)

// Kinds lists every transport kind.
var Kinds = []Kind{Stdout, File, Clipboard, OSC52, Tmux, Wormhole, QR, SSH}

// DestPrefix starts the egress destination of every export.
const DestPrefix = "export:"

// Limits.
const (
	maxOSC52 = 100000 // bytes of base64; many terminals drop more
	maxQR    = 2000   // bytes; a terminal QR above this is unreadable
)

// Target is a parsed --to value.
type Target struct {
	Kind Kind
	Path string // file path, or the remote path for ssh
	Host string // ssh only
}

// String is the --to form.
func (t Target) String() string {
	switch t.Kind {
	case File:
		return "file:" + t.Path
	case SSH:
		return "ssh:" + t.Host + ":" + t.Path
	}
	return string(t.Kind)
}

// Dest is the egress destination. It names the transport only: the event log
// holds ids and enums, never paths or hosts.
func (t Target) Dest() string { return DestPrefix + string(t.Kind) }

var sshHostRe = regexp.MustCompile(`^(?:[A-Za-z0-9._-]+@)?[A-Za-z0-9][A-Za-z0-9._-]*$`)

// ParseTarget parses a --to value. An empty value means stdout.
func ParseTarget(s string) (Target, error) {
	if s == "" {
		return Target{Kind: Stdout}, nil
	}
	kind, rest, hasRest := strings.Cut(s, ":")
	t := Target{Kind: Kind(kind)}
	switch t.Kind {
	case Stdout, Clipboard, OSC52, Tmux, Wormhole, QR:
		if hasRest {
			return Target{}, fmt.Errorf("transport %s takes no argument", kind)
		}
		return t, nil
	case File:
		if rest == "" || strings.ContainsRune(rest, 0) {
			return Target{}, errors.New("transport file needs a path: file:<path>")
		}
		t.Path = rest
		return t, nil
	case SSH:
		host, path, ok := strings.Cut(rest, ":")
		if !ok || path == "" {
			return Target{}, errors.New("transport ssh needs ssh:<host>:<path>")
		}
		if !sshHostRe.MatchString(host) {
			return Target{}, fmt.Errorf("ssh host %q must be [user@]name of letters, digits, dot, dash, underscore", host)
		}
		if strings.ContainsAny(path, "\x00\n\r") {
			return Target{}, errors.New("ssh path holds a control character")
		}
		t.Host, t.Path = host, path
		return t, nil
	}
	return Target{}, fmt.Errorf("transport %q must be one of %v (file:<path>, ssh:<host>:<path>)", kind, Kinds)
}

// textOnly transports cannot carry pdf or docx.
func (k Kind) textOnly() bool {
	return k == Clipboard || k == OSC52 || k == Tmux || k == QR
}

// Check reports whether the target can carry the rendered content, before
// anything is sent.
func (t Target) Check(r Rendered) error {
	if r.Binary && t.Kind.textOnly() {
		return fmt.Errorf("transport %s carries text only: %s is binary", t.Kind, r.Format)
	}
	return nil
}

// IO is the process context a transport uses.
type IO struct {
	Stdout io.Writer
	Stderr io.Writer
	Getenv func(string) string
	// Terminal opens the controlling terminal for osc52; nil or an error
	// falls back to Stdout.
	Terminal func() (io.WriteCloser, error)
}

func (o IO) getenv(k string) string {
	if o.Getenv == nil {
		return os.Getenv(k)
	}
	return o.Getenv(k)
}

// Send moves r through the target and returns a one-line description of what
// happened. The caller checks egress first. doc names temp files.
func Send(ctx context.Context, t Target, r Rendered, doc string, o IO) (string, error) {
	if err := t.Check(r); err != nil {
		return "", err
	}
	switch t.Kind {
	case Stdout:
		_, err := o.Stdout.Write(r.Data)
		return "", err
	case File:
		return sendFile(t, r)
	case Clipboard:
		tool, err := ClipboardCopy(string(r.Data), exec.LookPath, func(argv []string, stdin string) error {
			return runStdin(ctx, argv, []byte(stdin), nil, nil)
		})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("copied %d bytes with %s", len(r.Data), tool), nil
	case OSC52:
		return sendOSC52(r, o)
	case Tmux:
		var stderr bytes.Buffer
		if err := runStdin(ctx, []string{"tmux", "load-buffer", "-"}, r.Data, nil, &stderr); err != nil {
			return "", fmt.Errorf("tmux: %w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return fmt.Sprintf("loaded %d bytes into the tmux buffer", len(r.Data)), nil
	case Wormhole:
		return sendWormhole(ctx, r, doc, o)
	case QR:
		if len(r.Data) > maxQR {
			return "", fmt.Errorf("qr: %d bytes exceeds the %d byte limit for a terminal code", len(r.Data), maxQR)
		}
		q, err := qrcode.New(string(r.Data), qrcode.Medium)
		if err != nil {
			return "", fmt.Errorf("qr: %w", err)
		}
		_, err = io.WriteString(o.Stdout, q.ToSmallString(false))
		return "", err
	case SSH:
		return sendSSH(ctx, t, r, o)
	}
	return "", fmt.Errorf("unknown transport %q", t.Kind)
}

func sendFile(t Target, r Rendered) (string, error) {
	dir := filepath.Dir(t.Path)
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return "", fmt.Errorf("file: directory %s does not exist", dir)
	}
	if err := atomicfile.Write(t.Path, r.Data, 0o600); err != nil {
		return "", fmt.Errorf("file: %w", err)
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(r.Data), t.Path), nil
}

// clipboardTools are tried in order; the first on PATH wins.
var clipboardTools = [][]string{
	{"pbcopy"},
	{"wl-copy"},
	{"xclip", "-selection", "clipboard"},
	{"clip.exe"},
}

// ClipboardCopy pipes text into the first available clipboard tool and
// returns its name.
func ClipboardCopy(text string, lookPath func(string) (string, error), run func(argv []string, stdin string) error) (string, error) {
	for _, argv := range clipboardTools {
		if _, err := lookPath(argv[0]); err == nil {
			if err := run(argv, text); err != nil {
				return "", fmt.Errorf("clipboard: %s: %w", argv[0], err)
			}
			return argv[0], nil
		}
	}
	return "", errors.New("clipboard: none of pbcopy, wl-copy, xclip, clip.exe found on PATH")
}

// runStdin runs argv with stdin. Nil writers discard output.
func runStdin(ctx context.Context, argv []string, stdin []byte, stdout, stderr io.Writer) error {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.Stdout, cmd.Stderr = stdout, stderr
	return cmd.Run()
}

// OSC52Sequence is the escape sequence that sets the terminal clipboard.
// Inside tmux it is wrapped in the passthrough form.
func OSC52Sequence(data []byte, inTmux bool) string {
	seq := "\x1b]52;c;" + base64.StdEncoding.EncodeToString(data) + "\x07"
	if inTmux {
		return "\x1bPtmux;" + strings.ReplaceAll(seq, "\x1b", "\x1b\x1b") + "\x1b\\"
	}
	return seq
}

func sendOSC52(r Rendered, o IO) (string, error) {
	if n := base64.StdEncoding.EncodedLen(len(r.Data)); n > maxOSC52 {
		return "", fmt.Errorf("osc52: %d bytes of base64 exceeds the %d limit most terminals accept; use another transport", n, maxOSC52)
	}
	seq := OSC52Sequence(r.Data, o.getenv("TMUX") != "")
	w := o.Stdout
	if o.Terminal != nil {
		if tty, err := o.Terminal(); err == nil {
			defer func() { _ = tty.Close() }()
			w = tty
		}
	}
	if _, err := io.WriteString(w, seq); err != nil {
		return "", fmt.Errorf("osc52: %w", err)
	}
	return fmt.Sprintf("sent %d bytes to the terminal clipboard", len(r.Data)), nil
}

var unsafeNameRe = regexp.MustCompile(`[^A-Za-z0-9._-]`)

// sendWormhole sends text on stdin (--text -) and binary as a file in a
// private temp directory. wormhole prints the code on its stderr; it goes to
// o.Stderr as it appears. The command returns when the receiver is done.
func sendWormhole(ctx context.Context, r Rendered, doc string, o IO) (string, error) {
	argv := []string{"wormhole", "send", "--text", "-"}
	stdin := r.Data
	if r.Binary {
		dir, err := os.MkdirTemp("", "para-wormhole-*")
		if err != nil {
			return "", fmt.Errorf("wormhole: %w", err)
		}
		defer func() { _ = os.RemoveAll(dir) }()
		name := unsafeNameRe.ReplaceAllString(doc, "_") + "." + r.Ext
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, r.Data, 0o600); err != nil {
			return "", fmt.Errorf("wormhole: %w", err)
		}
		argv, stdin = []string{"wormhole", "send", path}, nil
	}
	if _, err := exec.LookPath("wormhole"); err != nil {
		return "", errors.New("wormhole: not found on PATH")
	}
	if err := runStdin(ctx, argv, stdin, o.Stderr, o.Stderr); err != nil {
		return "", fmt.Errorf("wormhole: %w", err)
	}
	return "wormhole transfer finished", nil
}

// ShellQuote quotes s for a POSIX shell as one word. A leading "~/" keeps
// the remote home expansion.
func ShellQuote(s string) string {
	prefix := ""
	if strings.HasPrefix(s, "~/") {
		prefix, s = `"$HOME"/`, s[2:]
	}
	return prefix + "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// SSHArgv is the ssh command line for a target. The host is validated by
// ParseTarget and follows "--", so it cannot be read as an option. The path
// is one quoted word.
func SSHArgv(t Target) []string {
	return []string{"ssh", "--", t.Host, "cat > " + ShellQuote(t.Path)}
}

func sendSSH(ctx context.Context, t Target, r Rendered, o IO) (string, error) {
	if !sshHostRe.MatchString(t.Host) || t.Path == "" {
		return "", errors.New("ssh: target not parsed by ParseTarget")
	}
	if err := runStdin(ctx, SSHArgv(t), r.Data, o.Stderr, o.Stderr); err != nil {
		return "", fmt.Errorf("ssh: %w", err)
	}
	return fmt.Sprintf("sent %d bytes to %s:%s", len(r.Data), t.Host, t.Path), nil
}
