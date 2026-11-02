package ops

import (
	"regexp"
	"strings"

	"github.com/iodesystems/paradize/internal/model"
)

// Rendering of para-md for paste. Line based and small: it covers what
// docs/format.md allows (headings, lists, tables, fences, links, wikilinks,
// bold, code spans).

var (
	blockIDRe  = regexp.MustCompile(` \^[A-Za-z0-9-]+$`)
	wikiRe     = regexp.MustCompile(`\[\[([^\]|#]+)(?:#\^?([^\]|]*))?(?:\|([^\]]*))?\]\]`)
	mdLinkRe   = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)\)`)
	boldRe     = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	codeRe     = regexp.MustCompile("`([^`]+)`")
	headingRe  = regexp.MustCompile(`^(#{1,6}) +(.*)$`)
	bulletRe   = regexp.MustCompile(`^(\s*)[-*] +(.*)$`)
	numberedRe = regexp.MustCompile(`^(\s*)\d+\. +(.*)$`)
	sepCellRe  = regexp.MustCompile(`^:?-+:?$`)
)

// renderCopy renders doc for format. Frontmatter is never included.
func renderCopy(f CopyFormat, doc model.Document, marking string) string {
	var b strings.Builder
	banner := "[" + marking + "]"
	if f == CopySlack || f == CopyJira {
		banner = "*[" + marking + "]*"
	}
	b.WriteString(banner + "\n\n")
	switch f {
	case CopyEmail:
		b.WriteString("Subject: [" + marking + "] " + doc.Title + "\n\n")
	case CopySlack:
		b.WriteString("*" + doc.Title + "*\n\n")
	case CopyJira:
		b.WriteString("h1. " + doc.Title + "\n\n")
	default:
		b.WriteString(doc.Title + "\n\n")
	}
	if body := renderBody(f, doc.Body); body != "" {
		b.WriteString(body + "\n\n")
	}
	if u := sourceURLOf(doc.Meta); u != "" {
		b.WriteString("Source: " + u + "\n\n")
	}
	b.WriteString(banner + "\n")
	return b.String()
}

func renderBody(f CopyFormat, body string) string {
	var out []string
	lines := strings.Split(strings.Trim(body, "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trim := strings.TrimLeft(line, " ")
		switch {
		case isFence(trim):
			var code []string
			for i++; i < len(lines) && !isFence(lines[i]); i++ {
				code = append(code, lines[i])
			}
			out = append(out, fence(f, code)...)
		case strings.HasPrefix(trim, "|"):
			var rows [][]string
			for ; i < len(lines) && strings.HasPrefix(strings.TrimLeft(lines[i], " "), "|"); i++ {
				rows = append(rows, tableCells(lines[i]))
			}
			i--
			out = append(out, table(f, rows)...)
		default:
			out = append(out, renderLine(f, line))
		}
	}
	return strings.Trim(strings.Join(out, "\n"), "\n")
}

func isFence(line string) bool {
	t := strings.TrimLeft(line, " ")
	return strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~")
}

func fence(f CopyFormat, code []string) []string {
	if f == CopyJira {
		return append(append([]string{"{code}"}, code...), "{code}")
	}
	return append(append([]string{"```"}, code...), "```")
}

func tableCells(line string) []string {
	line = strings.TrimSpace(line)
	line = strings.TrimSuffix(strings.TrimPrefix(line, "|"), "|")
	var cells []string
	var cur strings.Builder
	for i := 0; i < len(line); i++ {
		switch {
		case line[i] == '\\' && i+1 < len(line) && line[i+1] == '|':
			cur.WriteByte('|')
			i++
		case line[i] == '|':
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(line[i])
		}
	}
	return append(cells, strings.TrimSpace(cur.String()))
}

func isSeparator(cells []string) bool {
	for _, c := range cells {
		if !sepCellRe.MatchString(c) {
			return false
		}
	}
	return len(cells) > 0
}

func table(f CopyFormat, rows [][]string) []string {
	var out []string
	for n, cells := range rows {
		if n == 1 && isSeparator(cells) {
			continue
		}
		for i := range cells {
			cells[i] = inline(f, cells[i])
		}
		switch {
		case f == CopyJira && n == 0:
			out = append(out, "||"+strings.Join(cells, "||")+"||")
		case f == CopyJira:
			out = append(out, "|"+strings.Join(cells, "|")+"|")
		default:
			out = append(out, strings.Join(cells, " | "))
		}
	}
	if f == CopySlack {
		return fence(f, out)
	}
	return out
}

func renderLine(f CopyFormat, line string) string {
	line = blockIDRe.ReplaceAllString(line, "")
	if m := headingRe.FindStringSubmatch(line); m != nil {
		text := inline(f, m[2])
		switch f {
		case CopyJira:
			return "h" + string(rune('0'+len(m[1]))) + ". " + text
		case CopySlack:
			return "*" + text + "*"
		}
		return text
	}
	if m := bulletRe.FindStringSubmatch(line); m != nil {
		depth := len(m[1]) / 2
		switch f {
		case CopyJira:
			return strings.Repeat("*", depth+1) + " " + inline(f, m[2])
		case CopySlack:
			return m[1] + "• " + inline(f, m[2])
		}
		return m[1] + "- " + inline(f, m[2])
	}
	if m := numberedRe.FindStringSubmatch(line); m != nil && f == CopyJira {
		return strings.Repeat("#", len(m[1])/2+1) + " " + inline(f, m[2])
	}
	return inline(f, line)
}

// inline converts links, wikilinks, bold, and code spans.
func inline(f CopyFormat, s string) string {
	s = wikiRe.ReplaceAllStringFunc(s, func(m string) string {
		p := wikiRe.FindStringSubmatch(m)
		switch {
		case p[3] != "":
			return p[3]
		case p[2] != "":
			return p[1] + " " + p[2]
		}
		return p[1]
	})
	s = mdLinkRe.ReplaceAllStringFunc(s, func(m string) string {
		p := mdLinkRe.FindStringSubmatch(m)
		switch f {
		case CopySlack:
			return "<" + p[2] + "|" + p[1] + ">"
		case CopyJira:
			return "[" + p[1] + "|" + p[2] + "]"
		}
		return p[1] + " (" + p[2] + ")"
	})
	switch f {
	case CopySlack:
		s = boldRe.ReplaceAllString(s, "*$1*")
	case CopyJira:
		s = boldRe.ReplaceAllString(s, "*$1*")
		s = codeRe.ReplaceAllString(s, "{{$1}}")
	default:
		s = boldRe.ReplaceAllString(s, "$1")
		s = codeRe.ReplaceAllString(s, "$1")
	}
	return s
}
