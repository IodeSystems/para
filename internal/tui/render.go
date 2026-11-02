package tui

import (
	"os"
	"strconv"
	"strings"
	"sync"

	"charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
)

// styleConfig is the glamour style by name, with the heading prefixes of
// levels 2 to 6 removed: they print "## " in front of a heading, which is
// markdown source, not a heading.
func styleConfig(name string) ansi.StyleConfig {
	var c ansi.StyleConfig
	switch name {
	case "light":
		c = styles.LightStyleConfig
	case "notty":
		c = styles.NoTTYStyleConfig
	default:
		c = styles.DarkStyleConfig
	}
	for _, h := range []*ansi.StyleBlock{&c.H2, &c.H3, &c.H4, &c.H5, &c.H6} {
		h.Prefix = ""
		h.Bold = boolPtr(true)
	}
	if name == "notty" {
		c.H1.Prefix = ""
	}
	return c
}

func boolPtr(b bool) *bool { return &b }

// mdStyle is the glamour style, resolved once at startup. glamour's
// WithAutoStyle asks the terminal for its background with an OSC query and
// waits up to five seconds; terminals that never answer (tmux without
// passthrough) would stall every render. PARA_MD_STYLE (dark, light, notty)
// wins, then COLORFGBG, then dark.
var (
	mdStyleOnce sync.Once
	mdStyle     = "dark"
)

func resolveStyle(env func(string) string) string {
	switch strings.ToLower(strings.TrimSpace(env("PARA_MD_STYLE"))) {
	case "dark":
		return "dark"
	case "light":
		return "light"
	case "notty", "none":
		return "notty"
	}
	parts := strings.Split(strings.TrimSpace(env("COLORFGBG")), ";")
	if len(parts) >= 2 {
		if bg, err := strconv.Atoi(parts[len(parts)-1]); err == nil && bg >= 7 && bg != 8 {
			return "light"
		}
	}
	return "dark"
}

// InitStyle resolves the glamour style. Call it before the program starts.
func InitStyle() {
	mdStyleOnce.Do(func() { mdStyle = resolveStyle(os.Getenv) })
}

// renderers caches one glamour renderer per width: building one costs more
// than rendering a page.
type renderers struct {
	mu sync.Mutex
	by map[int]*glamour.TermRenderer
}

func (r *renderers) get(width int) *glamour.TermRenderer {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.by == nil {
		r.by = map[int]*glamour.TermRenderer{}
	}
	if tr, ok := r.by[width]; ok {
		return tr
	}
	InitStyle()
	tr, err := glamour.NewTermRenderer(glamour.WithStyles(styleConfig(mdStyle)), glamour.WithWordWrap(width))
	if err != nil {
		tr = nil
	}
	r.by[width] = tr
	return tr
}

// RenderedDoc is a prepared document after glamour, ready to show.
type RenderedDoc struct {
	Lines  []string
	Links  []Link
	Keys   map[string]Key
	Blocks []string
}

// render runs glamour over p at width and swaps in the key letters.
func (r *renderers) render(p Prepared, width int) RenderedDoc {
	width = max(width, 20)
	text := p.Markdown
	if tr := r.get(width); tr != nil {
		if out, err := tr.Render(p.Markdown); err == nil {
			text = out
		}
	}
	text = Finish(text)
	lines := strings.Split(text, "\n")
	for len(lines) > 0 && strings.TrimSpace(stripSGR(lines[0])) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(stripSGR(lines[len(lines)-1])) == "" {
		lines = lines[:len(lines)-1]
	}
	return RenderedDoc{Lines: lines, Links: p.Links, Keys: p.Keys, Blocks: p.Blocks}
}
