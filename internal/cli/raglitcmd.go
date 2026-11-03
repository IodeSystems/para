package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/iodesystems/parasolve/internal/raglit"
)

// ignorePatterns keep para's own state, events and legacy proposals out of the index.
// They are set before the watch starts, so nothing in them is ever read.
var ignorePatterns = []string{"**/.para/**", "**/events/**", "**/proposals/**", "**/.git/**"}

// minRaglitStamp is the commit time of the raglit build that serves
// manage_project and manage_index over /mcp (44b40ad), as in a pseudo-version.
const minRaglitStamp = "20261102052510"

var versionRe = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)(?:-0\.(\d{14})-)?`)

// raglitVersionOK reports whether a `raglit --version` line names a build new
// enough. A line it cannot read passes: the tool check after connecting is the
// authority.
func raglitVersionOK(line string) bool {
	m := versionRe.FindStringSubmatch(line)
	if m == nil {
		return true
	}
	if m[4] != "" { // a build after a release
		return m[4] >= minRaglitStamp
	}
	// A release: v0.4.0 and older lack the lifecycle tools.
	var maj, min, patch int
	_, _ = fmt.Sscan(m[1], &maj)
	_, _ = fmt.Sscan(m[2], &min)
	_, _ = fmt.Sscan(m[3], &patch)
	return maj > 0 || min > 4 || (min == 4 && patch >= 1)
}

var projectNameBad = regexp.MustCompile(`[^a-z0-9_-]+`)

// raglitProject maps a vault id to a raglit project name: lower case,
// [a-z0-9_-] only, no "__", and not "root".
func raglitProject(id string) string {
	n := projectNameBad.ReplaceAllString(strings.ToLower(id), "-")
	for strings.Contains(n, "__") {
		n = strings.ReplaceAll(n, "__", "_")
	}
	if n == "" || n == "root" {
		n = "para-" + n
	}
	return strings.TrimSuffix(n, "-")
}

// raglitBinary finds the raglit executable.
func (l *loader) raglitBinary() (string, error) {
	if l.raglitExe != "" {
		return l.raglitExe, nil
	}
	p, err := exec.LookPath("raglit")
	if err != nil {
		return "", errors.New("raglit is not installed or not on PATH: install raglit (main 44b40ad or later) and run `para raglit connect` again")
	}
	return p, nil
}

func runRaglit(ctx context.Context, bin string, args ...string) (string, error) {
	var out, errb bytes.Buffer
	c := exec.CommandContext(ctx, bin, args...)
	c.Stdout, c.Stderr = &out, &errb
	if err := c.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return out.String(), errors.New(firstLine(msg))
	}
	return out.String(), nil
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	return s
}

// raglitVault is the selected vault with its per-machine paths.
type raglitVault struct {
	id, path, state, project string
}

func (l *loader) raglitVault() (raglitVault, error) {
	if err := l.load(); err != nil {
		return raglitVault{}, err
	}
	sel, err := l.selectVault()
	if err != nil {
		return raglitVault{}, err
	}
	abs, err := filepath.Abs(sel.Path)
	if err != nil {
		return raglitVault{}, err
	}
	return raglitVault{id: sel.ID, path: abs, state: l.stateDir(sel.ID), project: raglitProject(sel.ID)}, nil
}

// newRaglitCmd connects a vault to a raglit daemon for meaning search. These
// are admin commands, not ops: no agent surface can connect, or point a vault's
// queries at another daemon.
func newRaglitCmd(l *loader) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "raglit",
		Short: "Connect this vault to a raglit daemon for meaning search (this machine only)",
		Long: `Search always finds exact words. Connected to a raglit daemon, it also finds
documents by meaning. The connection is per machine and per vault, in
$PARA_HOME/vaults/<id>/raglit.json (mode 0600), never in the vault.
One daemon per trust domain: a work vault uses a work daemon.`,
		Args: cobra.NoArgs,
	}
	cmd.AddCommand(newRaglitConnectCmd(l), newRaglitStatusCmd(l), newRaglitDisconnectCmd(l))
	return cmd
}

func newRaglitConnectCmd(l *loader) *cobra.Command {
	var daemon, token string
	cmd := &cobra.Command{
		Use:   "connect",
		Short: "Create the raglit project for the selected vault and start indexing it",
		Long: `Create a raglit project for the selected vault on the daemon (raglit project
create), keep its URL and key on this machine, set the ignore patterns, watch
the vault folder, and run a test search. Safe to run again: with a stored key
it checks and repairs, and does not create anything twice.

--url is the daemon, such as http://127.0.0.1:7420. Without it raglit uses its
default daemon. The vault folder must be readable on the daemon's host.

If the project already exists and this machine has no key, run
  raglit project key NAME --root VAULT --json
and give the token to this command with --token (and --url).

Indexing runs on the daemon and lags the files by seconds plus queue time. A
daemon that captions documents queues a model call per document, so the first
index can take a while.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return raglitConnect(cmd, l, daemon, token)
		},
	}
	cmd.Flags().StringVar(&daemon, "url", "", "the raglit daemon, such as http://127.0.0.1:7420")
	cmd.Flags().StringVar(&token, "token", "", "a project key made with raglit project key")
	return cmd
}

func raglitConnect(cmd *cobra.Command, l *loader, daemon, token string) error {
	v, err := l.raglitVault()
	if err != nil {
		return err
	}
	w := cmd.OutOrStdout()
	say := func(format string, a ...any) { _, _ = fmt.Fprintf(w, format+"\n", a...) }
	ctx := cmd.Context()
	bin, err := l.raglitBinary()
	if err != nil {
		return err
	}
	ver, _ := runRaglit(ctx, bin, "--version")
	if !raglitVersionOK(ver) {
		return fmt.Errorf("raglit connect: %s is too old: it lacks manage_project over MCP; install raglit main 44b40ad or later", strings.TrimSpace(ver))
	}
	cfg, have, err := raglit.Load(v.state)
	if err != nil {
		return err
	}
	daemon = strings.TrimRight(daemon, "/")
	switch {
	case token != "":
		if daemon == "" {
			return errors.New("raglit connect: --token needs --url")
		}
		cfg = raglit.Config{URL: daemon, Project: v.project, Token: token}
		if err := raglit.Save(v.state, cfg); err != nil {
			return err
		}
		say("stored the key you gave for project %s", cfg.Project)
	case have && (daemon == "" || daemon == strings.TrimRight(cfg.URL, "/")):
		say("using the stored key for project %s on %s", cfg.Project, cfg.URL)
	default:
		args := []string{"project", "create", v.project, "--root", v.path, "--json"}
		if daemon != "" {
			args = append(args, "--daemon", daemon)
		}
		out, err := runRaglit(ctx, bin, args...)
		if err != nil {
			if strings.Contains(err.Error(), "already has a key") {
				return fmt.Errorf("raglit connect: project %s exists on the daemon and this machine has no key for it.\n"+
					"Run: raglit project key %s --root %s --json%s\n"+
					"Then: para raglit connect --url <daemon> --token <token>\n"+
					"A new key keeps the same indexes", v.project, v.project, v.path, daemonArg(daemon))
			}
			return fmt.Errorf("raglit connect: raglit project create: %w", err)
		}
		var made struct {
			Project string `json:"project"`
			URL     string `json:"url"`
			Token   string `json:"token"`
		}
		if err := json.Unmarshal([]byte(out), &made); err != nil || made.Token == "" || made.URL == "" {
			return errors.New("raglit connect: raglit project create printed no key; the token prints once, so run raglit project key and use --token")
		}
		base := strings.TrimSuffix(strings.TrimRight(made.URL, "/"), "/mcp/"+made.Project)
		cfg = raglit.Config{URL: base, Project: made.Project, Token: made.Token}
		// The token prints once: keep it before anything else can fail.
		if err := raglit.Save(v.state, cfg); err != nil {
			return err
		}
		say("created project %s on %s; the key is stored in %s", cfg.Project, cfg.URL, raglit.Path(v.state))
	}

	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	s, err := cfg.Open(cctx)
	if err != nil {
		return fmt.Errorf("raglit connect: %s (the key stays stored; fix the daemon and run connect again)", raglit.Reason(err))
	}
	defer s.Close()
	tools, err := s.Tools(cctx)
	if err != nil {
		return err
	}
	for _, want := range []string{"manage_project", "manage_index", "search"} {
		if !contains(tools, want) {
			return fmt.Errorf("raglit connect: the daemon does not serve %s: it is too old; update raglit", want)
		}
	}
	ix, err := s.Indexes(cctx)
	if err != nil {
		return fmt.Errorf("raglit connect: %s", raglit.Reason(err))
	}
	if len(ix) == 0 {
		if _, err := s.Call(cctx, "manage_project", map[string]any{"action": "create", "caption_profile": "docs"}); err != nil {
			return fmt.Errorf("raglit connect: create the index: %w", err)
		}
		say("created the index (caption profile docs)")
	}
	for _, p := range ignorePatterns {
		if _, err := s.Call(cctx, "manage_index", map[string]any{"action": "ignore", "pattern": p}); err != nil {
			return fmt.Errorf("raglit connect: ignore %s: %w", p, err)
		}
	}
	say("ignoring %s", strings.Join(ignorePatterns, " "))
	watches, err := s.Call(cctx, "manage_project", map[string]any{"action": "watches"})
	if err != nil {
		return fmt.Errorf("raglit connect: list watches: %w", err)
	}
	if strings.Contains(watches, v.path) {
		say("already watching %s", v.path)
	} else {
		if _, err := s.Call(cctx, "manage_project", map[string]any{"action": "watch", "dir": v.path}); err != nil {
			return fmt.Errorf("raglit connect: watch %s: %w (the folder must be under the key's root on the daemon's host)", v.path, err)
		}
		say("watching %s (the daemon polls every 5 s)", v.path)
	}
	if _, meaning, err := cfg.Search(cctx, "para", 1); err != nil {
		return fmt.Errorf("raglit connect: test search: %s", raglit.Reason(err))
	} else if meaning {
		say("test search ok; the daemon can search by meaning")
	} else {
		say("test search ok; the daemon has no embedding model, so search stays exact-only until it has one")
	}
	say("indexing lags the files by seconds plus queue time. If the daemon captions documents, the first index queues a model call per document: expect a backlog")
	return nil
}

func daemonArg(d string) string {
	if d == "" {
		return ""
	}
	return " --daemon " + d
}

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func newRaglitStatusCmd(l *loader) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show whether this vault is connected, whether the daemon has models, and the index size",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			v, err := l.raglitVault()
			if err != nil {
				return err
			}
			w := cmd.OutOrStdout()
			cfg, ok, err := raglit.Load(v.state)
			if err != nil {
				return err
			}
			if !ok {
				_, err := fmt.Fprintf(w, "connected: no\nsearch is exact-only. Run: para raglit connect\n")
				return err
			}
			p := &printer{w: w}
			p.f("connected: yes\ndaemon:    %s\nproject:   %s\n", cfg.URL, cfg.Project)
			ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			s, err := cfg.Open(ctx)
			var ix []raglit.Index
			if err == nil {
				defer s.Close()
				ix, err = s.Indexes(ctx)
			}
			if err != nil {
				p.f("daemon:    unreachable (%s); search falls back to exact-only\n", raglit.Reason(err))
				return p.err
			}
			docs := 0
			for _, i := range ix {
				docs += i.Documents
			}
			models := "off (exact-only)"
			if raglit.HasMeaning(ix) {
				models = "on"
			}
			p.f("models:    %s\nindexed:   %d documents\n", models, docs)
			return p.err
		},
	}
}

func newRaglitDisconnectCmd(l *loader) *cobra.Command {
	return &cobra.Command{
		Use:   "disconnect",
		Short: "Remove the local raglit config; the index stays on the daemon",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			v, err := l.raglitVault()
			if err != nil {
				return err
			}
			cfg, ok, _ := raglit.Load(v.state)
			if err := raglit.Remove(v.state); err != nil {
				return err
			}
			return writeDisconnected(cmd.OutOrStdout(), v, cfg, ok)
		},
	}
}

func writeDisconnected(w io.Writer, v raglitVault, cfg raglit.Config, was bool) error {
	if !was {
		_, err := fmt.Fprintln(w, "this vault was not connected")
		return err
	}
	_, err := fmt.Fprintf(w, "removed %s. Search is exact-only again.\n"+
		"The index, its text, and the watch remain on the daemon at %s. Para cannot purge them.\n"+
		"To delete the project there, on the daemon's host: raglit project delete %s --yes\n"+
		"(not in raglit releases yet; on older builds the key can be revoked with raglit key revoke)\n",
		raglit.FileName, cfg.URL, cfg.Project)
	return err
}
