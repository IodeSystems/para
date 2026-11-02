package tui

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/iodesystems/parasolve/internal/export"
	"github.com/iodesystems/parasolve/internal/index"
	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/ops"
)

// Messages. Every one is the result of a command run off the update loop.
type (
	snapMsg struct {
		snap Snapshot
		fp   string
	}
	pollMsg struct{}
	fpMsg   struct{ fp string }
	docMsg  struct {
		id        string
		doc       model.Document
		err       error
		reload    bool // an in-place refresh of the page already shown
		untrusted bool // a proposal's source: the page says so
		ov        overview
	}
	whoMsg struct {
		query string
		out   ops.WhoOutput
		err   error
	}
	searchMsg struct {
		query    string
		hits     []index.Hit
		degraded string
		err      error
	}
	exportListMsg struct {
		out ops.ExportListOutput
		err error
	}
	// doneMsg reports a finished action. Text is shown on the status line;
	// Output is shown in an overlay when not empty (a QR code).
	doneMsg struct {
		text   string
		output string
		err    error
		reload bool
		// advance is the item a decision settled: the cursor moves on.
		advance string
	}
	envMsg struct {
		env Env
		err error
	}
)

const defaultPoll = time.Second

func (m *Model) pollEvery() time.Duration {
	if v := m.env.getenv("PARA_TUI_POLL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return defaultPoll
}

func (m *Model) tick() tea.Cmd {
	return tea.Tick(m.pollEvery(), func(time.Time) tea.Msg { return pollMsg{} })
}

func (m *Model) loadSnap() tea.Cmd {
	env := m.env
	return func() tea.Msg {
		return snapMsg{snap: Load(context.Background(), env), fp: Fingerprint(env.Vault.Path)}
	}
}

func (m *Model) checkFP() tea.Cmd {
	root := m.env.Vault.Path
	return func() tea.Msg { return fpMsg{Fingerprint(root)} }
}

func (m *Model) loadDoc(id string, reload bool) tea.Cmd {
	env := m.env
	return func() tea.Msg {
		d, err := env.Deps.Store.Get(context.Background(), id)
		var ov overview
		if err == nil {
			ov = buildOverview(context.Background(), env, d)
		}
		return docMsg{id: id, doc: d, err: err, reload: reload, ov: ov}
	}
}

func (m *Model) loadWho(q string) tea.Cmd {
	env := m.env
	return func() tea.Msg {
		o, err := ops.Who.Run(context.Background(), env.Deps, ops.WhoInput{Query: q, Recent: 10})
		return whoMsg{query: q, out: o, err: err}
	}
}

func (m *Model) loadSearch(q string) tea.Cmd {
	env := m.env
	return func() tea.Msg {
		in := ops.ParseQuery(q)
		in.Limit, in.Prefix = 30, true
		o, err := ops.Search.Run(context.Background(), env.Deps, in)
		return searchMsg{query: q, hits: o.Hits, degraded: o.Degraded, err: err}
	}
}

func (m *Model) loadExportList() tea.Cmd {
	env := m.env
	return func() tea.Msg {
		o, err := ops.ExportList.Run(context.Background(), env.Deps, ops.ExportListInput{})
		return exportListMsg{out: o, err: err}
	}
}

func (m *Model) capture(text string) tea.Cmd {
	env := m.env
	return func() tea.Msg {
		hint := ops.SimilarFor(context.Background(), env.Deps.Store, "task_add", ops.TaskAddInput{Title: text})
		_, err := ops.InboxAdd.Run(context.Background(), env.Deps, ops.InboxAddInput{Text: text})
		if err != nil {
			return doneMsg{err: err}
		}
		said := "Captured: " + truncate(oneLine(text), 60)
		if len(hint) > 0 {
			said += ". similar: " + truncate(hint[0].Title, 40)
		}
		return doneMsg{text: said, reload: true}
	}
}

func (m *Model) openVault(id string) tea.Cmd {
	in := m.env.Install
	return func() tea.Msg {
		vs, err := in.List()
		if err != nil {
			return envMsg{err: err}
		}
		for _, v := range vs {
			if v.ID == id {
				e, err := OpenEnv(in, v)
				return envMsg{env: e, err: err}
			}
		}
		return envMsg{err: fmt.Errorf("vault %q not found", id)}
	}
}

// runExport renders a document through ops.Export and sends it with the
// transport, as the CLI does. Stdout and QR output come back as Output for an
// overlay, because the terminal belongs to the program.
func (m *Model) runExport(id string, f export.Format, to string) tea.Cmd {
	env := m.env
	return func() tea.Msg {
		ctx := context.Background()
		out, err := ops.Export.Run(ctx, env.Deps, ops.ExportInput{ID: id, As: f, To: to})
		if err != nil {
			return doneMsg{err: err}
		}
		r, err := out.Rendered()
		if err != nil {
			return doneMsg{err: err}
		}
		target, err := export.ParseTarget(out.To)
		if err != nil {
			return doneMsg{err: err}
		}
		var stdout, stderr bytes.Buffer
		detail, err := export.Send(ctx, target, r, out.ID, export.IO{Stdout: &stdout, Stderr: &stderr, Terminal: openTTY})
		if err != nil {
			return doneMsg{err: err}
		}
		if detail == "" {
			detail = fmt.Sprintf("exported %s as %s to %s", id, f, out.To)
		}
		return doneMsg{text: detail, output: stdout.String()}
	}
}

// copyText sends text to the terminal clipboard (OSC 52) for a document
// block. The vault's egress check runs first and logs the doc id.
func (m *Model) copyText(doc, text string) tea.Cmd {
	env := m.env
	return func() tea.Msg {
		ctx := context.Background()
		target, _ := export.ParseTarget("osc52")
		dec, err := env.Deps.Store.Egress(ctx, doc, target.Dest())
		if err != nil {
			return doneMsg{err: err}
		}
		if !dec.Allowed {
			return doneMsg{err: fmt.Errorf("osc52 is not in this vault's egress.export")}
		}
		r := export.Rendered{Format: export.Plain, Ext: "txt", Data: []byte(text)}
		detail, err := export.Send(ctx, target, r, doc, export.IO{Stdout: io.Discard, Stderr: io.Discard, Terminal: openTTY})
		if err != nil {
			return doneMsg{err: err}
		}
		return doneMsg{text: detail}
	}
}

// openTTY opens the controlling terminal so OSC 52 reaches it.
func openTTY() (io.WriteCloser, error) { return os.OpenFile("/dev/tty", os.O_WRONLY, 0) }

// openURL opens a URL with the desktop opener when there is a display, else
// copies it. It reports which.
func (m *Model) openURL(url string) tea.Cmd {
	env := m.env
	return func() tea.Msg {
		opener := ""
		switch runtime.GOOS {
		case "darwin":
			opener = "open"
		default:
			if env.getenv("DISPLAY") != "" || env.getenv("WAYLAND_DISPLAY") != "" {
				opener = "xdg-open"
			}
		}
		if opener != "" {
			if p, err := exec.LookPath(opener); err == nil {
				c := exec.Command(p, url)
				if err := c.Start(); err == nil {
					go func() { _ = c.Wait() }()
					return doneMsg{text: "opened " + url}
				}
			}
		}
		target, _ := export.ParseTarget("osc52")
		r := export.Rendered{Format: export.Plain, Ext: "txt", Data: []byte(url)}
		if _, err := export.Send(context.Background(), target, r, "url", export.IO{Stdout: io.Discard, Stderr: io.Discard, Terminal: openTTY}); err != nil {
			return doneMsg{err: err}
		}
		return doneMsg{text: "no opener here; copied " + url}
	}
}

func (m *Model) savePosition(p Position) tea.Cmd {
	dir := m.env.StateDir
	return func() tea.Msg {
		if err := SavePosition(dir, p); err != nil {
			return doneMsg{err: err}
		}
		return doneMsg{reload: false}
	}
}

// acceptNeed accepts one item that needs the owner.
func (m *Model) acceptNeed(n Need) tea.Cmd {
	env := m.env
	return func() tea.Msg {
		ctx := context.Background()
		switch n.Kind {
		case NeedUnverified:
			if _, err := ops.PolicyReview.Run(ctx, env.Deps, ops.PolicyReviewInput{ID: n.ID, By: Actor}); err != nil {
				return doneMsg{err: err}
			}
			return doneMsg{text: "verified " + n.ID, reload: true}
		case NeedProposed:
			if _, err := ops.RequestAccept(ctx, env.Deps, ops.RequestAcceptInput{ID: n.ID}); err != nil {
				return doneMsg{err: err}
			}
			return doneMsg{text: "queued " + n.ID, reload: true}
		}
		return doneMsg{text: "a draft has nothing to accept: open it, send it yourself, then record the reply"}
	}
}

// reopenRFX sets a request back to open.
func (m *Model) reopenRFX(id string) tea.Cmd {
	env := m.env
	return func() tea.Msg {
		if _, err := ops.RFXStatus.Run(context.Background(), env.Deps, ops.RFXStatusInput{ID: id, Status: model.RFXOpen}); err != nil {
			return doneMsg{err: err}
		}
		return doneMsg{text: "reopened " + id, reload: true}
	}
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	return s
}
