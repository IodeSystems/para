// Package paramd parses and lints para-md files (see docs/format.md).
//
// Parse never fails on content: a malformed file still yields a File, and
// Lint reports what is wrong. Only reading the file can fail (ParseFile).
package paramd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

// Line is one physical line of the file.
type Line struct {
	N     int    // 1-based
	Text  string // without trailing CR
	Code  bool   // inside a fenced code block, fence lines included
	FM    bool   // frontmatter, delimiters included
	Prose string // Text with inline code spans blanked; empty for Code and FM lines
}

// Link is a wikilink: [[target#heading|alias]] or [[target#^R4]].
type Link struct {
	Target  string
	Heading string // text after '#', when not a block ref
	Block   string // block id without '^', e.g. "R4"
	Alias   string
	Embed   bool // ![[...]]
	Line    int
	Raw     string
}

// Block is a block id (^R4) at the end of a line.
type Block struct {
	ID   string
	Line int
	Text string // the line without the block id
}

// Rule is a well-formed policy rule line: "R4 policy-logging: text ^R4".
type Rule struct {
	ID     string // "R4"
	Policy string // policy id, the file stem
	Text   string // text after the colon
	Line   int
}

// PageMark is a transcript page marker, "## Page N".
type PageMark struct {
	N    int
	Line int
	Raw  string
}

// File is a parsed para-md file.
type File struct {
	Path string
	Name string // file name without ".md"
	Raw  []byte

	ValidUTF8 bool
	HasBOM    bool
	HasCR     bool

	Lines []Line

	HasFM       bool
	FMErr       string         // set when frontmatter is missing, unterminated, or invalid YAML
	Frontmatter map[string]any // empty when absent
	FMNode      *yaml.Node     // the frontmatter mapping, parsed once; nil when absent or invalid

	Title     string // text of the first "# " heading, when it is the first body line
	TitleLine int
	Body      string // everything after the frontmatter
	BodyLine  int    // 1-based line number where Body starts

	Links     []Link
	Blocks    []Block
	Rules     []Rule
	PageMarks []PageMark
}

// Type is the frontmatter "type" value, or "".
func (f *File) Type() string {
	s, _ := f.Frontmatter["type"].(string)
	return s
}

// FrontmatterYAML returns the raw YAML between the '---' lines, or nil.
func (f *File) FrontmatterYAML() []byte {
	if !f.HasFM {
		return nil
	}
	var b bytes.Buffer
	for i := 1; i < len(f.Lines) && f.Lines[i].FM && f.Lines[i].Text != "---"; i++ {
		b.WriteString(f.Lines[i].Text)
		b.WriteByte('\n')
	}
	return b.Bytes()
}

// ValidStem reports whether s is a legal file name without ".md".
func ValidStem(s string) bool { return stemRe.MatchString(s) }

// ParseFile reads and parses a file from disk.
func ParseFile(path string) (*File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("parse file: %w", err)
	}
	return Parse(path, data), nil
}

var (
	titleRe    = regexp.MustCompile(`^# +(\S.*)$`)
	wikilinkRe = regexp.MustCompile(`(!?)\[\[([^\[\]\n]*)\]\]`)
	blockRe    = regexp.MustCompile(`(?:^|\s)\^([A-Za-z0-9-]+)\s*$`)
	ruleLineRe = regexp.MustCompile(`^(?:[-*]\s+)?(R\d+)\s+([a-z0-9][a-z0-9-]*):\s+(\S.*)$`)
	ruleHeadRe = regexp.MustCompile(`^(?:[-*]\s+)?R\d+\s+[a-z0-9][a-z0-9-]*:`)
	// pageMarkRe is raglit's transcriptionPageMark (reread.go:105) applied per line.
	pageMarkRe = regexp.MustCompile(`^##+\s*Page\s+(\d+)\s*$`)
)

// Parse parses data as a para-md file. path is used for the file name only.
func Parse(path string, data []byte) *File {
	f := &File{
		Path:        path,
		Name:        strings.TrimSuffix(filepath.Base(path), ".md"),
		Raw:         data,
		ValidUTF8:   utf8.Valid(data),
		HasBOM:      bytes.HasPrefix(data, []byte("\xef\xbb\xbf")),
		HasCR:       bytes.Contains(data, []byte("\r")),
		Frontmatter: map[string]any{},
	}
	text := strings.ToValidUTF8(string(data), "�")
	text = strings.TrimPrefix(text, "\uFEFF")
	for i, s := range strings.Split(text, "\n") {
		f.Lines = append(f.Lines, Line{N: i + 1, Text: strings.TrimSuffix(s, "\r")})
	}

	bodyIdx := f.parseFrontmatter()
	f.markCode(bodyIdx)

	var body []string
	for _, l := range f.Lines[bodyIdx:] {
		body = append(body, l.Text)
	}
	f.Body = strings.Join(body, "\n")
	f.BodyLine = bodyIdx + 1

	for i := bodyIdx; i < len(f.Lines); i++ {
		if strings.TrimSpace(f.Lines[i].Text) == "" {
			continue
		}
		if m := titleRe.FindStringSubmatch(f.Lines[i].Text); m != nil {
			f.Title = strings.TrimSpace(m[1])
			f.TitleLine = f.Lines[i].N
		}
		break
	}

	f.scan()
	return f
}

// parseFrontmatter fills the frontmatter fields and returns the index of the
// first body line.
func (f *File) parseFrontmatter() int {
	if len(f.Lines) == 0 || f.Lines[0].Text != "---" {
		f.FMErr = "file does not start with a '---' line"
		return 0
	}
	end := -1
	for i := 1; i < len(f.Lines); i++ {
		if f.Lines[i].Text == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		f.FMErr = "frontmatter has no closing '---' line"
		for i := range f.Lines {
			f.Lines[i].FM = true
		}
		return len(f.Lines)
	}
	f.HasFM = true
	var src []string
	for i := 0; i <= end; i++ {
		f.Lines[i].FM = true
		if i > 0 && i < end {
			src = append(src, f.Lines[i].Text)
		}
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(strings.Join(src, "\n")), &doc); err != nil {
		f.FMErr = "invalid YAML: " + strings.TrimPrefix(err.Error(), "yaml: ")
		return end + 1
	}
	if len(doc.Content) == 0 {
		return end + 1
	}
	var m map[string]any
	if err := doc.Content[0].Decode(&m); err != nil {
		f.FMErr = "invalid YAML: " + strings.TrimPrefix(err.Error(), "yaml: ")
		return end + 1
	}
	f.FMNode = doc.Content[0]
	if m != nil {
		f.Frontmatter = m
	}
	return end + 1
}

// markCode flags lines inside fenced code blocks and builds Prose.
func (f *File) markCode(from int) {
	var fence byte
	var fenceLen int
	for i := from; i < len(f.Lines); i++ {
		l := &f.Lines[i]
		t := strings.TrimLeft(l.Text, " ")
		if len(l.Text)-len(t) <= 3 && len(t) >= 3 && (t[0] == '`' || t[0] == '~') {
			n := 0
			for n < len(t) && t[n] == t[0] {
				n++
			}
			if n >= 3 {
				switch {
				case fence == 0:
					fence, fenceLen = t[0], n
					l.Code = true
					continue
				case t[0] == fence && n >= fenceLen && strings.TrimSpace(t[n:]) == "":
					fence = 0
					l.Code = true
					continue
				}
			}
		}
		if fence != 0 {
			l.Code = true
			continue
		}
		l.Prose = maskInlineCode(l.Text)
	}
}

// maskInlineCode blanks the contents of inline code spans, delimiters included.
func maskInlineCode(s string) string {
	b := []byte(s)
	for i := 0; i < len(b); {
		if b[i] != '`' {
			i++
			continue
		}
		n := 0
		for i+n < len(b) && b[i+n] == '`' {
			n++
		}
		j := i + n
		closed := -1
		for j < len(b) {
			if b[j] != '`' {
				j++
				continue
			}
			m := 0
			for j+m < len(b) && b[j+m] == '`' {
				m++
			}
			if m == n {
				closed = j + m
				break
			}
			j += m
		}
		if closed < 0 {
			i += n
			continue
		}
		for k := i; k < closed; k++ {
			b[k] = ' '
		}
		i = closed
	}
	return string(b)
}

// scan extracts links, block ids, rules, and page markers.
func (f *File) scan() {
	for _, l := range f.Lines {
		var text string
		switch {
		case l.FM:
			text = l.Text
		case l.Code:
			continue
		default:
			text = l.Prose
		}
		for _, m := range wikilinkRe.FindAllStringSubmatch(text, -1) {
			f.Links = append(f.Links, parseLink(m[1] == "!", m[2], l.N, m[0]))
		}
		if l.FM {
			continue
		}
		if m := pageMarkRe.FindStringSubmatch(l.Text); m != nil {
			n, _ := strconv.Atoi(m[1])
			f.PageMarks = append(f.PageMarks, PageMark{N: n, Line: l.N, Raw: l.Text})
		}
		if loc := blockRe.FindStringSubmatchIndex(l.Text); loc != nil {
			id := l.Text[loc[2]:loc[3]]
			txt := strings.TrimSpace(l.Text[:loc[0]])
			f.Blocks = append(f.Blocks, Block{ID: id, Line: l.N, Text: txt})
			if m := ruleLineRe.FindStringSubmatch(txt); m != nil && m[1] == id {
				f.Rules = append(f.Rules, Rule{ID: m[1], Policy: m[2], Text: m[3], Line: l.N})
			}
		}
	}
}

func parseLink(embed bool, inner string, line int, raw string) Link {
	lk := Link{Embed: embed, Line: line, Raw: raw}
	if i := strings.Index(inner, "|"); i >= 0 {
		lk.Alias = inner[i+1:]
		inner = inner[:i]
	}
	if i := strings.Index(inner, "#"); i >= 0 {
		frag := inner[i+1:]
		inner = inner[:i]
		if b, ok := strings.CutPrefix(frag, "^"); ok {
			lk.Block = b
		} else {
			lk.Heading = frag
		}
	}
	lk.Target = inner
	return lk
}
