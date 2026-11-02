package export

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/iodesystems/parasolve/internal/model"
)

// Format is an output format.
type Format string

// Formats. plain through jira are built in. html, pdf, and docx need pandoc.
const (
	Plain Format = "plain"
	MD    Format = "md"
	Email Format = "email"
	Slack Format = "slack"
	Jira  Format = "jira"
	HTML  Format = "html"
	PDF   Format = "pdf"
	DOCX  Format = "docx"
)

// Formats lists every Format.
var Formats = []Format{Plain, MD, Email, Slack, Jira, HTML, PDF, DOCX}

// Enum implements the MCP enum hook.
func (Format) Enum() []string {
	out := make([]string, len(Formats))
	for i, f := range Formats {
		out[i] = string(f)
	}
	return out
}

// Binary reports whether f produces bytes that are not text.
func (f Format) Binary() bool { return f == PDF || f == DOCX }

func (f Format) ext() string {
	switch f {
	case MD:
		return "md"
	case HTML:
		return "html"
	case PDF:
		return "pdf"
	case DOCX:
		return "docx"
	}
	return "txt"
}

// Rendered is a document in one format.
type Rendered struct {
	Format Format
	Data   []byte
	Binary bool
	Ext    string // file extension without the dot
}

// pdfEngines are tried in order for pandoc --pdf-engine.
var pdfEngines = []string{"pdflatex", "xelatex", "lualatex", "tectonic", "typst", "wkhtmltopdf", "weasyprint"}

// Pandoc is the pandoc executable name.
const Pandoc = "pandoc"

// pdfEngine returns the first PDF engine on PATH, or "".
func pdfEngine() string {
	for _, e := range pdfEngines {
		if _, err := exec.LookPath(e); err == nil {
			return e
		}
	}
	return ""
}

// Render renders doc in f. Text formats are built in; html, pdf, and docx
// run pandoc on the md rendering. Content never leaves the process except to
// pandoc, through stdin and a private temp directory.
func Render(ctx context.Context, f Format, doc model.Document, marking string) (Rendered, error) {
	switch f {
	case Plain, Email, Slack, Jira:
		return Rendered{Format: f, Data: []byte(Text(f, doc, marking)), Ext: f.ext()}, nil
	case MD:
		return Rendered{Format: f, Data: []byte(markdown(doc, marking)), Ext: f.ext()}, nil
	case HTML, PDF, DOCX:
		return pandoc(ctx, f, doc, marking)
	}
	return Rendered{}, fmt.Errorf("format %q must be one of %v", f, Formats)
}

// markdown is the md format: marking, title heading, body without block ids
// and wikilink syntax, source line. Frontmatter is never included.
func markdown(doc model.Document, marking string) string {
	var b strings.Builder
	banner := "**[" + marking + "]**"
	b.WriteString(banner + "\n\n# " + doc.Title + "\n\n")
	var body []string
	fenced := false
	for _, line := range strings.Split(strings.Trim(doc.Body, "\n"), "\n") {
		if isFence(line) {
			fenced = !fenced
		}
		if !fenced {
			line = blockIDRe.ReplaceAllString(line, "")
			line = wikiRe.ReplaceAllStringFunc(line, func(m string) string {
				p := wikiRe.FindStringSubmatch(m)
				switch {
				case p[3] != "":
					return p[3]
				case p[2] != "":
					return p[1] + " " + p[2]
				}
				return p[1]
			})
		}
		body = append(body, line)
	}
	if s := strings.Trim(strings.Join(body, "\n"), "\n"); s != "" {
		b.WriteString(s + "\n\n")
	}
	if u := SourceURL(doc.Meta); u != "" {
		b.WriteString("Source: " + u + "\n\n")
	}
	b.WriteString(banner + "\n")
	return b.String()
}

var titleSafeRe = regexp.MustCompile(`[\x00-\x1f]`)

func pandoc(ctx context.Context, f Format, doc model.Document, marking string) (Rendered, error) {
	if _, err := exec.LookPath(Pandoc); err != nil {
		return Rendered{}, fmt.Errorf("format %s needs pandoc on PATH", f)
	}
	args := []string{"--sandbox", "-f", "gfm", "--metadata", "pagetitle=" + titleSafeRe.ReplaceAllString(doc.Title, " ")}
	switch f {
	case HTML:
		args = append(args, "-t", "html5", "-s")
	case DOCX:
		args = append(args, "-t", "docx")
	case PDF:
		eng := pdfEngine()
		if eng == "" {
			return Rendered{}, fmt.Errorf("format pdf needs a pdf engine on PATH (one of %s)", strings.Join(pdfEngines, ", "))
		}
		args = append(args, "--pdf-engine="+eng)
	}
	dir, err := os.MkdirTemp("", "para-export-*")
	if err != nil {
		return Rendered{}, fmt.Errorf("export: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	out := filepath.Join(dir, "out."+f.ext())
	args = append(args, "-o", out)
	cmd := exec.CommandContext(ctx, Pandoc, args...)
	cmd.Stdin = strings.NewReader(markdown(doc, marking))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return Rendered{}, fmt.Errorf("pandoc: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	data, err := os.ReadFile(out)
	if err != nil {
		return Rendered{}, fmt.Errorf("pandoc: %w", err)
	}
	if len(data) == 0 {
		return Rendered{}, errors.New("pandoc: produced no output")
	}
	return Rendered{Format: f, Data: data, Binary: f.Binary(), Ext: f.ext()}, nil
}
