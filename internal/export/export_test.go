package export

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iodesystems/paradize/internal/model"
)

func testDoc() model.Document {
	return model.Document{ID: "policy-x", Title: "Logging", Body: "## Scope\nSee [[policy-y#^R2]] and [[policy-z|Z]]. ^b1\n\n```\n[[keep]] ^b2\n```",
		Meta: &model.Policy{Source: model.Source{URL: "https://wiki.acme.example/p"}}}
}

func TestParseTarget(t *testing.T) {
	good := map[string]Target{
		"":                             {Kind: Stdout},
		"stdout":                       {Kind: Stdout},
		"clipboard":                    {Kind: Clipboard},
		"osc52":                        {Kind: OSC52},
		"tmux":                         {Kind: Tmux},
		"wormhole":                     {Kind: Wormhole},
		"qr":                           {Kind: QR},
		"file:/tmp/a b.txt":            {Kind: File, Path: "/tmp/a b.txt"},
		"file:rel:colon":               {Kind: File, Path: "rel:colon"},
		"ssh:desk:/srv/in.txt":         {Kind: SSH, Host: "desk", Path: "/srv/in.txt"},
		"ssh:me@desk.acme.example:~/x": {Kind: SSH, Host: "me@desk.acme.example", Path: "~/x"},
	}
	for in, want := range good {
		got, err := ParseTarget(in)
		if err != nil || got != want {
			t.Errorf("%q: got %+v err %v, want %+v", in, got, err, want)
		}
	}
	bad := []string{
		"fax", "stdout:x", "clipboard:x", "file:", "file", "ssh:", "ssh:host", "ssh:host:",
		"ssh:-oProxyCommand=evil:/x", "ssh:-p:/x", "ssh:host name:/x", "ssh:host;rm:/x", "ssh:$(id):/x",
		"ssh:`id`:/x", "ssh:a@-b:/x", "ssh:host:/x\nid", "ssh:ho st:/x", "ssh:[::1]:/x", "ssh:@h:/x",
	}
	for _, in := range bad {
		if got, err := ParseTarget(in); err == nil {
			t.Errorf("%q accepted as %+v", in, got)
		}
	}
}

func TestShellQuote(t *testing.T) {
	cases := map[string]string{
		"/srv/in.txt":   `'/srv/in.txt'`,
		"it's":          `'it'\''s'`,
		"a b; rm -rf /": `'a b; rm -rf /'`,
		"$(id)":         `'$(id)'`,
		"~/notes.txt":   `"$HOME"/'notes.txt'`,
		"~root/x":       `'~root/x'`,
	}
	for in, want := range cases {
		if got := ShellQuote(in); got != want {
			t.Errorf("%q: got %s want %s", in, got, want)
		}
	}
}

func TestSSHArgvStaysOneWord(t *testing.T) {
	argv := SSHArgv(Target{Kind: SSH, Host: "me@desk", Path: "/x'; id; '"})
	if len(argv) != 4 || argv[0] != "ssh" || argv[1] != "--" || argv[2] != "me@desk" || !strings.HasPrefix(argv[3], "cat > '") {
		t.Fatalf("argv = %q", argv)
	}
}

func TestRenderTextFormats(t *testing.T) {
	ctx := context.Background()
	for _, f := range []Format{Plain, Email, Slack, Jira, MD} {
		r, err := Render(ctx, f, testDoc(), "ACME")
		if err != nil || r.Binary || r.Format != f || len(r.Data) == 0 {
			t.Fatalf("%s: %+v %v", f, r, err)
		}
		if strings.Contains(string(r.Data), "type:") {
			t.Errorf("%s: frontmatter leaked", f)
		}
	}
	r, _ := Render(ctx, MD, testDoc(), "ACME")
	got := string(r.Data)
	contains(t, got, "**[ACME]**", "# Logging", "## Scope", "See policy-y R2 and Z.", "```\n[[keep]] ^b2\n```", "Source: https://wiki.acme.example/p")
	if strings.Contains(got, " ^b1") {
		t.Errorf("block id kept:\n%s", got)
	}
	if _, err := Render(ctx, "fax", testDoc(), "ACME"); err == nil {
		t.Error("unknown format accepted")
	}
}

func TestRenderPandoc(t *testing.T) {
	// The fake pandoc records its argv and stdin, and writes the -o file.
	dir := fakeTools(t, map[string]string{
		"pandoc": `out=""; prev=""
for a in "$@"; do
  if [ "$prev" = "-o" ]; then out="$a"; fi
  prev="$a"
done
printf '%s\n' "$@" > "$FAKE_LOG.argv"
cat > "$FAKE_LOG.stdin"
printf 'FAKE-OUTPUT' > "$out"`,
		"xelatex": `exit 0`,
	})
	log := filepath.Join(dir, "log")
	t.Setenv("FAKE_LOG", log)
	ctx := context.Background()
	for _, f := range []Format{HTML, DOCX, PDF} {
		r, err := Render(ctx, f, testDoc(), "ACME")
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if string(r.Data) != "FAKE-OUTPUT" || r.Binary != f.Binary() || r.Ext != string(f) {
			t.Errorf("%s: %+v", f, r)
		}
		argv := mustRead(t, log+".argv")
		contains(t, argv, "--sandbox", "gfm", "pagetitle=Logging")
		contains(t, mustRead(t, log+".stdin"), "# Logging", "**[ACME]**")
		if f == PDF {
			contains(t, argv, "--pdf-engine=xelatex")
		}
		if f == HTML {
			contains(t, argv, "html5")
		}
	}
}

func TestRenderPandocMissing(t *testing.T) {
	fakeTools(t, map[string]string{})
	for _, f := range []Format{HTML, PDF, DOCX} {
		if _, err := Render(context.Background(), f, testDoc(), "ACME"); err == nil || !strings.Contains(err.Error(), "pandoc") {
			t.Errorf("%s: err = %v", f, err)
		}
	}
	// pandoc without a pdf engine: html works in principle, pdf says why not.
	fakeTools(t, map[string]string{"pandoc": "exit 0"})
	if _, err := Render(context.Background(), PDF, testDoc(), "ACME"); err == nil || !strings.Contains(err.Error(), "pdf engine") {
		t.Errorf("pdf err = %v", err)
	}
}

func TestPandocFailureSurfaces(t *testing.T) {
	fakeTools(t, map[string]string{"pandoc": "echo bad input >&2; exit 3"})
	_, err := Render(context.Background(), HTML, testDoc(), "ACME")
	if err == nil || !strings.Contains(err.Error(), "bad input") {
		t.Fatalf("err = %v", err)
	}
}

func TestAvailability(t *testing.T) {
	fakeTools(t, map[string]string{"pandoc": "exit 0", "wl-copy": "exit 0", "xelatex": "exit 0"})
	by := func(items []Item) map[string]Item {
		m := map[string]Item{}
		for _, it := range items {
			m[it.Name] = it
		}
		return m
	}
	f := by(AvailableFormats())
	for _, n := range []string{"plain", "md", "email", "slack", "jira", "html", "docx", "pdf"} {
		if !f[n].Available {
			t.Errorf("format %s unavailable: %+v", n, f[n])
		}
	}
	if f["pdf"].Found != "xelatex" {
		t.Errorf("pdf = %+v", f["pdf"])
	}
	tr := by(AvailableTransports())
	if !tr["clipboard"].Available || tr["clipboard"].Found != "wl-copy" {
		t.Errorf("clipboard = %+v", tr["clipboard"])
	}
	for _, n := range []string{"stdout", "file", "osc52", "qr"} {
		if !tr[n].Available {
			t.Errorf("transport %s must always be available", n)
		}
	}
	for _, n := range []string{"tmux", "wormhole", "ssh"} {
		if tr[n].Available || len(tr[n].Needs) == 0 {
			t.Errorf("transport %s = %+v, want missing", n, tr[n])
		}
	}
	fakeTools(t, map[string]string{})
	if f := by(AvailableFormats()); f["html"].Available || f["pdf"].Available || f["docx"].Available || !f["plain"].Available {
		t.Errorf("formats without pandoc = %+v", f)
	}
}

func text(s string) Rendered { return Rendered{Format: Plain, Data: []byte(s), Ext: "txt"} }
func binary(s string) Rendered {
	return Rendered{Format: PDF, Data: []byte(s), Binary: true, Ext: "pdf"}
}

func send(t *testing.T, to string, r Rendered) (stdout, stderr, detail string, err error) {
	t.Helper()
	tg, err := ParseTarget(to)
	if err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	detail, err = Send(context.Background(), tg, r, "policy-x", IO{Stdout: &out, Stderr: &errb,
		Getenv: func(string) string { return "" }})
	return out.String(), errb.String(), detail, err
}

func TestSendStdout(t *testing.T) {
	out, _, _, err := send(t, "stdout", text("hello\n"))
	if err != nil || out != "hello\n" {
		t.Fatalf("out %q err %v", out, err)
	}
	// Binary to stdout is allowed: `> file.pdf`.
	if out, _, _, err = send(t, "", binary("%PDF")); err != nil || out != "%PDF" {
		t.Fatalf("out %q err %v", out, err)
	}
}

func TestSendFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out file.txt")
	if _, _, detail, err := send(t, "file:"+path, text("hello")); err != nil || !strings.Contains(detail, "5 bytes") {
		t.Fatalf("detail %q err %v", detail, err)
	}
	if mustRead(t, path) != "hello" {
		t.Fatal("content")
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v", st.Mode())
	}
	if _, _, _, err := send(t, "file:"+path, binary("PDF2")); err != nil || mustRead(t, path) != "PDF2" {
		t.Fatalf("overwrite: %v", err)
	}
	if _, _, _, err := send(t, "file:"+filepath.Join(dir, "missing", "x"), text("a")); err == nil {
		t.Error("missing directory accepted")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("temp files left behind: %v", entries)
	}
}

func TestSendClipboard(t *testing.T) {
	dir := fakeTools(t, map[string]string{"xclip": `printf '%s\n' "$@" > "$FAKE_LOG.argv"; cat > "$FAKE_LOG.stdin"`})
	t.Setenv("FAKE_LOG", filepath.Join(dir, "log"))
	_, _, detail, err := send(t, "clipboard", text("secret words"))
	if err != nil || !strings.Contains(detail, "xclip") {
		t.Fatalf("detail %q err %v", detail, err)
	}
	if mustRead(t, dir+"/log.stdin") != "secret words" {
		t.Error("stdin")
	}
	contains(t, mustRead(t, dir+"/log.argv"), "-selection", "clipboard")
	fakeTools(t, map[string]string{})
	if _, _, _, err := send(t, "clipboard", text("x")); err == nil || !strings.Contains(err.Error(), "none of") {
		t.Errorf("no tool: %v", err)
	}
}

func TestClipboardToolOrder(t *testing.T) {
	var ran []string
	look := func(n string) (string, error) {
		if n == "clip.exe" || n == "xclip" {
			return "/x/" + n, nil
		}
		return "", os.ErrNotExist
	}
	tool, err := ClipboardCopy("a", look, func(argv []string, in string) error { ran = argv; return nil })
	if err != nil || tool != "xclip" || strings.Join(ran, " ") != "xclip -selection clipboard" {
		t.Fatalf("tool %q ran %v err %v", tool, ran, err)
	}
	only := func(n string) (string, error) {
		if n == "clip.exe" {
			return "/x/clip.exe", nil
		}
		return "", os.ErrNotExist
	}
	if tool, err = ClipboardCopy("a", only, func([]string, string) error { return nil }); err != nil || tool != "clip.exe" {
		t.Fatalf("tool %q err %v", tool, err)
	}
}

func TestSendTmux(t *testing.T) {
	dir := fakeTools(t, map[string]string{"tmux": `printf '%s\n' "$@" > "$FAKE_LOG.argv"; cat > "$FAKE_LOG.stdin"`})
	t.Setenv("FAKE_LOG", filepath.Join(dir, "log"))
	if _, _, _, err := send(t, "tmux", text("buffer text")); err != nil {
		t.Fatal(err)
	}
	if got := mustRead(t, dir+"/log.argv"); got != "load-buffer\n-\n" {
		t.Errorf("argv = %q", got)
	}
	if mustRead(t, dir+"/log.stdin") != "buffer text" {
		t.Error("stdin")
	}
	fakeTools(t, map[string]string{"tmux": "echo no server >&2; exit 1"})
	if _, _, _, err := send(t, "tmux", text("x")); err == nil || !strings.Contains(err.Error(), "no server") {
		t.Errorf("err = %v", err)
	}
}

func TestSendWormholeText(t *testing.T) {
	dir := fakeTools(t, map[string]string{"wormhole": `printf '%s\n' "$@" > "$FAKE_LOG.argv"; cat > "$FAKE_LOG.stdin"; echo "Wormhole code is: 7-crossover-clockwork" >&2`})
	t.Setenv("FAKE_LOG", filepath.Join(dir, "log"))
	_, stderr, _, err := send(t, "wormhole", text("hello"))
	if err != nil {
		t.Fatal(err)
	}
	contains(t, stderr, "7-crossover-clockwork")
	if got := mustRead(t, dir+"/log.argv"); got != "send\n--text\n-\n" {
		t.Errorf("argv = %q (text must not appear in argv)", got)
	}
	if mustRead(t, dir+"/log.stdin") != "hello" {
		t.Error("stdin")
	}
}

func TestSendWormholeBinaryUsesPrivateTempFile(t *testing.T) {
	dir := fakeTools(t, map[string]string{"wormhole": `file="$2"
printf '%s\n' "$@" > "$FAKE_LOG.argv"
cp "$file" "$FAKE_LOG.copy"
stat -c %a "$file" > "$FAKE_LOG.mode"
echo "Wormhole code is: 9-x" >&2`})
	t.Setenv("FAKE_LOG", filepath.Join(dir, "log"))
	_, stderr, _, err := send(t, "wormhole", binary("PDFBYTES"))
	if err != nil {
		t.Fatal(err)
	}
	contains(t, stderr, "9-x")
	argv := strings.Fields(mustRead(t, dir+"/log.argv"))
	if len(argv) != 2 || argv[0] != "send" || !strings.HasSuffix(argv[1], "policy-x.pdf") {
		t.Fatalf("argv = %v", argv)
	}
	if mustRead(t, dir+"/log.copy") != "PDFBYTES" || strings.TrimSpace(mustRead(t, dir+"/log.mode")) != "600" {
		t.Error("temp file content or mode")
	}
	if _, err := os.Stat(argv[1]); !os.IsNotExist(err) {
		t.Errorf("temp file kept: %v", err)
	}
	fakeTools(t, map[string]string{})
	if _, _, _, err := send(t, "wormhole", text("x")); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Errorf("err = %v", err)
	}
}

func TestSendSSH(t *testing.T) {
	// The fake ssh behaves like the real one: it drops the options and the
	// host, then hands the remaining word to a shell in a scratch directory.
	work := t.TempDir()
	dir := fakeTools(t, map[string]string{"ssh": `printf '%s\n' "$@" > "$FAKE_LOG.argv"
[ "$1" = "--" ] || exit 9
shift; shift
cd "$FAKE_WORK" && exec sh -c "$*"`})
	t.Setenv("FAKE_LOG", filepath.Join(dir, "log"))
	t.Setenv("FAKE_WORK", work)
	t.Setenv("HOME", work)

	if _, _, _, err := send(t, "ssh:desk:out.txt", text("remote text")); err != nil {
		t.Fatal(err)
	}
	if mustRead(t, filepath.Join(work, "out.txt")) != "remote text" {
		t.Error("remote content")
	}
	argv := strings.Split(strings.TrimSpace(mustRead(t, dir+"/log.argv")), "\n")
	if len(argv) != 3 || argv[0] != "--" || argv[1] != "desk" {
		t.Errorf("argv = %q", argv)
	}

	// Hostile paths become file names, never commands.
	for _, name := range []string{"a b", "x'; touch pwned; '", "$(touch pwned2)", "`touch pwned3`", "q\"x", "-rf"} {
		if _, _, _, err := send(t, "ssh:desk:"+name, binary("B"+name)); err != nil {
			t.Fatalf("%q: %v", name, err)
		}
		if got := mustRead(t, filepath.Join(work, name)); got != "B"+name {
			t.Errorf("%q: content %q", name, got)
		}
	}
	for _, p := range []string{"pwned", "pwned2", "pwned3"} {
		if _, err := os.Stat(filepath.Join(work, p)); err == nil {
			t.Errorf("injection ran: %s exists", p)
		}
	}
	// ~/ expands on the remote side.
	if _, _, _, err := send(t, "ssh:desk:~/home.txt", text("h")); err != nil || mustRead(t, filepath.Join(work, "home.txt")) != "h" {
		t.Errorf("~ path: %v", err)
	}
	// A host that looks like an option never reaches ssh.
	if _, err := ParseTarget("ssh:-oProxyCommand=touch pwned4:/x"); err == nil {
		t.Error("option host accepted")
	}
	// ssh failure is reported.
	fakeTools(t, map[string]string{"ssh": "echo denied >&2; exit 255"})
	if _, stderr, _, err := send(t, "ssh:desk:x", text("x")); err == nil || !strings.Contains(stderr, "denied") {
		t.Errorf("err %v stderr %q", err, stderr)
	}
}

func TestSendSSHRejectsUnparsedTarget(t *testing.T) {
	fakeTools(t, map[string]string{"ssh": "exit 0"})
	_, err := Send(context.Background(), Target{Kind: SSH, Host: "-oProxyCommand=x", Path: "/x"}, text("a"), "d", IO{Stdout: &bytes.Buffer{}})
	if err == nil {
		t.Fatal("hand-built hostile target accepted")
	}
}

type closeBuf struct{ bytes.Buffer }

func (*closeBuf) Close() error { return nil }

func TestSendOSC52(t *testing.T) {
	tg, _ := ParseTarget("osc52")
	run := func(env string, data string) (stdout, tty string, err error) {
		var out bytes.Buffer
		var term closeBuf
		_, err = Send(context.Background(), tg, text(data), "d", IO{Stdout: &out,
			Getenv:   func(k string) string { return map[string]string{"TMUX": env}[k] },
			Terminal: func() (io.WriteCloser, error) { return &term, nil }})
		return out.String(), term.String(), err
	}
	b64 := base64.StdEncoding.EncodeToString([]byte("hi"))
	stdout, tty, err := run("", "hi")
	if err != nil || stdout != "" || tty != "\x1b]52;c;"+b64+"\x07" {
		t.Fatalf("stdout %q tty %q err %v", stdout, tty, err)
	}
	_, tty, err = run("/tmp/tmux-1000/default,1,0", "hi")
	if err != nil || tty != "\x1bPtmux;\x1b\x1b]52;c;"+b64+"\x07\x1b\\" {
		t.Fatalf("tmux tty %q err %v", tty, err)
	}
	// No terminal: the sequence goes to stdout.
	var out bytes.Buffer
	if _, err := Send(context.Background(), tg, text("hi"), "d", IO{Stdout: &out, Getenv: func(string) string { return "" },
		Terminal: func() (io.WriteCloser, error) { return nil, os.ErrNotExist }}); err != nil || !strings.Contains(out.String(), b64) {
		t.Fatalf("fallback %q err %v", out.String(), err)
	}
	if _, _, err := run("", strings.Repeat("x", 80000)); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Errorf("oversize err = %v", err)
	}
}

func TestSendQR(t *testing.T) {
	out, _, _, err := send(t, "qr", text("https://wiki.acme.example/p"))
	if err != nil || strings.Count(out, "\n") < 10 || !strings.ContainsAny(out, "\u2580\u2584\u2588") {
		t.Fatalf("qr output %q err %v", out, err)
	}
	if _, _, _, err := send(t, "qr", text(strings.Repeat("x", 5000))); err == nil || !strings.Contains(err.Error(), "limit") {
		t.Errorf("oversize err = %v", err)
	}
}

func TestBinaryOnlyToCapableTransports(t *testing.T) {
	for _, to := range []string{"clipboard", "osc52", "tmux", "qr"} {
		if _, _, _, err := send(t, to, binary("PDF")); err == nil || !strings.Contains(err.Error(), "text only") {
			t.Errorf("%s: err = %v", to, err)
		}
	}
}
