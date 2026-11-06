package paramd

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// Annotations are Obsidian callouts with a para header, inline in an area
// body under the section they apply to (docs/format.md):
//
//	> [!exception] ann-ab12 · review_by 2027-01-31 · by [[person-x]] · kind underspecified
//	> The reason, one or more lines.
//
// The header is the callout title: an id, then " · "-separated fields, each
// a key, one space, and a value. Para reads only callouts of AnnotationKinds.
// A note callout without an ann- id is a plain Obsidian note and is left
// alone; an exception, convention, or constraint callout without a valid
// header is Broken: lint warns, the text stays.

// AnnotationKinds are the callout kinds para reads as annotations.
var AnnotationKinds = []string{"note", "exception", "convention", "constraint"}

// FieldSep separates the id and the fields of a callout header.
const FieldSep = " · "

// CalloutField is one key and value of a callout header.
type CalloutField struct {
	Key   string
	Value string
}

// Callout is one annotation callout. Lines are 1-based in the text parsed.
type Callout struct {
	Kind   string
	ID     string // ann-<id>; empty when Broken
	Fields []CalloutField
	Text   string // the lines after the header, without "> "
	Line   int    // header line
	End    int    // last line of the callout
	// Heading is the text of the nearest heading above, without '#'; empty at
	// the top of the body. HeadingLine is its line, 0 at the top.
	Heading     string
	HeadingLine int
	// Broken says why a header para should read is not one; the callout is
	// still listed, with no id and no fields.
	Broken string
}

// Field returns the value of key, or "".
func (c Callout) Field(key string) string {
	for _, f := range c.Fields {
		if f.Key == key {
			return f.Value
		}
	}
	return ""
}

// Set returns c with key set to value: replaced in place, or added last. An
// empty value removes the key.
func (c Callout) Set(key, value string) Callout {
	fs := slices.Clone(c.Fields)
	i := slices.IndexFunc(fs, func(f CalloutField) bool { return f.Key == key })
	switch {
	case value == "" && i >= 0:
		fs = slices.Delete(fs, i, i+1)
	case value == "":
	case i >= 0:
		fs[i].Value = value
	default:
		fs = append(fs, CalloutField{Key: key, Value: value})
	}
	c.Fields = fs
	return c
}

var (
	calloutHeadRe = regexp.MustCompile(`^>\s?\[!([A-Za-z][A-Za-z0-9-]*)\][+-]?(?:\s+(.*))?$`)
	annIDRe       = regexp.MustCompile(`^ann-[a-z0-9]+(?:-[a-z0-9]+)*$`)
	fieldKeyRe    = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	headingRe     = regexp.MustCompile(`^(#{1,6})\s+(.*?)\s*#*\s*$`)
)

// isAnnotationHeader reports whether line is the header of a para annotation.
func isAnnotationHeader(line string) bool {
	m := calloutHeadRe.FindStringSubmatch(line)
	return m != nil && slices.Contains(AnnotationKinds, strings.ToLower(m[1])) && strings.HasPrefix(strings.TrimSpace(m[2]), "ann-")
}

// ValidAnnotationID reports whether s is an annotation id: ann-<lowercase words>.
func ValidAnnotationID(s string) bool { return annIDRe.MatchString(s) }

// CheckCallout reports what would make c print a header para cannot read.
func CheckCallout(c Callout) error {
	if !slices.Contains(AnnotationKinds, c.Kind) {
		return fmt.Errorf("kind %q must be one of %s", c.Kind, strings.Join(AnnotationKinds, ", "))
	}
	if !annIDRe.MatchString(c.ID) {
		return fmt.Errorf("id %q must read ann-<lowercase letters and digits>", c.ID)
	}
	seen := map[string]bool{}
	for _, f := range c.Fields {
		switch {
		case !fieldKeyRe.MatchString(f.Key):
			return fmt.Errorf("field key %q must be lowercase letters, digits, and _", f.Key)
		case seen[f.Key]:
			return fmt.Errorf("field %s given twice", f.Key)
		case strings.TrimSpace(f.Value) != f.Value || f.Value == "":
			return fmt.Errorf("field %s: value %q is empty or has space at an end", f.Key, f.Value)
		case strings.Contains(f.Value, strings.TrimSpace(FieldSep)) || strings.ContainsAny(f.Value, "\r\n"):
			return fmt.Errorf("field %s: value must be one line without %q", f.Key, strings.TrimSpace(FieldSep))
		}
		seen[f.Key] = true
	}
	if strings.Contains(c.Text, "\r") {
		return fmt.Errorf("text has a carriage return")
	}
	return nil
}

// Print is the callout as body lines, without a final newline.
func (c Callout) Print() string {
	var b strings.Builder
	b.WriteString("> [!" + c.Kind + "] " + c.ID)
	for _, f := range c.Fields {
		b.WriteString(FieldSep + f.Key + " " + f.Value)
	}
	if c.Text != "" {
		for _, l := range strings.Split(c.Text, "\n") {
			b.WriteString("\n>")
			if l != "" {
				b.WriteString(" " + l)
			}
		}
	}
	return b.String()
}

// parseHeader reads the title of a callout of an annotation kind. ours is
// false for a plain note; broken says why a header is not para's.
func parseHeader(kind, title string) (id string, fields []CalloutField, ours bool, broken string) {
	title = strings.TrimSpace(title)
	if !strings.HasPrefix(title, "ann-") {
		if kind == "note" {
			return "", nil, false, ""
		}
		return "", nil, true, "header has no annotation id: want > [!" + kind + "] ann-<id> · key value · ..."
	}
	parts := strings.Split(title, FieldSep)
	id = strings.TrimSpace(parts[0])
	if !annIDRe.MatchString(id) {
		return "", nil, true, fmt.Sprintf("annotation id %q must read ann-<lowercase letters and digits>", id)
	}
	seen := map[string]bool{}
	for _, p := range parts[1:] {
		k, v, _ := strings.Cut(strings.TrimSpace(p), " ")
		v = strings.TrimSpace(v)
		switch {
		case !fieldKeyRe.MatchString(k) || v == "":
			return "", nil, true, fmt.Sprintf("field %q must read 'key value'", strings.TrimSpace(p))
		case seen[k]:
			return "", nil, true, fmt.Sprintf("field %s given twice", k)
		}
		seen[k] = true
		fields = append(fields, CalloutField{Key: k, Value: v})
	}
	return id, fields, true, ""
}

// Callouts lists the annotation callouts of a markdown body, in order.
// Fenced code is skipped. A heading inside a callout does not count.
func Callouts(body string) []Callout {
	lines := strings.Split(body, "\n")
	return calloutsOf(lines, codeLines(lines), 1)
}

// codeLines flags the lines of fenced code blocks, fences included.
func codeLines(lines []string) []bool {
	f := &File{}
	for i, s := range lines {
		f.Lines = append(f.Lines, Line{N: i + 1, Text: strings.TrimSuffix(s, "\r")})
	}
	f.markCode(0)
	out := make([]bool, len(lines))
	for i, l := range f.Lines {
		out[i] = l.Code
	}
	return out
}

// calloutsOf scans lines; base is the line number of lines[0].
func calloutsOf(lines []string, code []bool, base int) []Callout {
	var out []Callout
	heading, headingLine := "", 0
	for i := 0; i < len(lines); i++ {
		if code[i] {
			continue
		}
		t := strings.TrimSuffix(lines[i], "\r")
		if m := headingRe.FindStringSubmatch(t); m != nil {
			heading, headingLine = m[2], base+i
			continue
		}
		m := calloutHeadRe.FindStringSubmatch(t)
		if m == nil {
			continue
		}
		end := i
		for end+1 < len(lines) && !code[end+1] && strings.HasPrefix(lines[end+1], ">") {
			end++
		}
		kind := strings.ToLower(m[1])
		if !slices.Contains(AnnotationKinds, kind) {
			i = end
			continue
		}
		id, fields, ours, broken := parseHeader(kind, m[2])
		if !ours {
			i = end
			continue
		}
		var text []string
		for _, l := range lines[i+1 : end+1] {
			l = strings.TrimSuffix(strings.TrimPrefix(l, ">"), "\r")
			text = append(text, strings.TrimPrefix(l, " "))
		}
		out = append(out, Callout{Kind: kind, ID: id, Fields: fields, Text: strings.Join(text, "\n"),
			Line: base + i, End: base + end, Heading: heading, HeadingLine: headingLine, Broken: broken})
		i = end
	}
	return out
}

// Section is a heading of a body and the lines under it, up to the next
// heading of any level. Lines are 0-based indexes of the body lines; the top
// of the body is the section with Level 0 and Line -1.
type Section struct {
	Heading string
	Level   int
	Line    int // the heading line; -1 for the top
	End     int // the first line after the section
}

// Sections lists the sections of a body, the top first.
func Sections(body string) []Section {
	lines := strings.Split(body, "\n")
	code := codeLines(lines)
	out := []Section{{Line: -1}}
	for i, l := range lines {
		if code[i] {
			continue
		}
		if m := headingRe.FindStringSubmatch(l); m != nil {
			out[len(out)-1].End = i
			out = append(out, Section{Heading: m[2], Level: len(m[1]), Line: i})
		}
	}
	out[len(out)-1].End = len(lines)
	return out
}

// Where an annotation goes in a body.
type Anchor struct {
	Heading string // a section heading, without '#'; "" with Block "" is the top
	Block   string // a block id without '^', such as R4
}

// ParseAnchor reads "", "top", "## Heading", "Heading", or "^R4".
func ParseAnchor(s string) Anchor {
	s = strings.TrimSpace(s)
	switch {
	case s == "" || strings.EqualFold(s, "top"):
		return Anchor{}
	case strings.HasPrefix(s, "^"):
		return Anchor{Block: strings.TrimPrefix(s, "^")}
	}
	return Anchor{Heading: strings.TrimSpace(strings.TrimLeft(s, "#"))}
}

func (a Anchor) String() string {
	switch {
	case a.Block != "":
		return "^" + a.Block
	case a.Heading != "":
		return a.Heading
	}
	return "top"
}

// InsertCallout puts c into body at the anchor: after the last line of the
// heading's own text (before a subsection), or after the line with the block
// id and the callouts that follow it, or at the end of the text before the
// first heading. A blank line goes before the callout when the next line is
// blank or the end; when the next line is text (a list item after a rule), the
// blank line goes after it instead, and a callout at line 1 always has one
// after. RemoveCallout reads that shape back, so it gives the body back.
func InsertCallout(body string, a Anchor, c Callout) (string, error) {
	if err := CheckCallout(c); err != nil {
		return body, err
	}
	for _, x := range Callouts(body) {
		if x.ID == c.ID {
			return body, fmt.Errorf("annotation %s is already in the body", c.ID)
		}
	}
	lines := strings.Split(body, "\n")
	if body == "" {
		lines = nil
	}
	at, err := insertAt(lines, a)
	if err != nil {
		return body, err
	}
	ins := strings.Split(c.Print(), "\n")
	switch {
	case len(lines) == 0:
	case at == 0 || at < len(lines) && lines[at] != "":
		ins = append(ins, "")
	default:
		ins = append([]string{""}, ins...)
	}
	out := append(append(slices.Clone(lines[:at]), ins...), lines[at:]...)
	return strings.Join(out, "\n"), nil
}

// insertAt is the line index the callout goes before.
func insertAt(lines []string, a Anchor) (int, error) {
	code := codeLines(lines)
	// after skips back over blank lines from end to the last line in use.
	after := func(start, end int) int {
		for end > start && strings.TrimSpace(lines[end-1]) == "" {
			end--
		}
		return end
	}
	if a.Block != "" {
		for i, l := range lines {
			if code[i] {
				continue
			}
			if m := blockRe.FindStringSubmatch(l); m != nil && m[1] == a.Block && !strings.HasPrefix(l, ">") {
				// Past the callouts that already follow the block.
				j := i + 1
				for {
					k := j
					for k < len(lines) && lines[k] == "" {
						k++
					}
					if k >= len(lines) || !calloutHeadRe.MatchString(lines[k]) {
						break
					}
					for k < len(lines) && strings.HasPrefix(lines[k], ">") {
						k++
					}
					j = k
				}
				return j, nil
			}
		}
		return 0, fmt.Errorf("no block ^%s in the body", a.Block)
	}
	secs := Sections(strings.Join(lines, "\n"))
	if len(lines) == 0 {
		return 0, nil
	}
	if a.Heading == "" {
		return after(0, secs[0].End), nil
	}
	for _, s := range secs[1:] {
		if strings.EqualFold(s.Heading, a.Heading) {
			return after(s.Line+1, s.End), nil
		}
	}
	return 0, fmt.Errorf("no heading %q in the body", a.Heading)
}

// RemoveCallout takes the callout with id out of body with the blank line
// InsertCallout put with it: the one before it, or else the one after.
func RemoveCallout(body, id string) (string, error) {
	i := slices.IndexFunc(Callouts(body), func(c Callout) bool { return c.ID == id })
	if i < 0 {
		return body, fmt.Errorf("no annotation %s in the body", id)
	}
	c := Callouts(body)[i]
	lines := strings.Split(body, "\n")
	s, e := c.Line-1, c.End
	switch {
	case s > 0 && lines[s-1] == "":
		s--
	case e < len(lines) && lines[e] == "":
		e++
	}
	return strings.Join(append(slices.Clone(lines[:s]), lines[e:]...), "\n"), nil
}

// ReplaceCallout puts c in place of the callout with the same id, keeping
// every other line.
func ReplaceCallout(body string, c Callout) (string, error) {
	if err := CheckCallout(c); err != nil {
		return body, err
	}
	cs := Callouts(body)
	i := slices.IndexFunc(cs, func(x Callout) bool { return x.ID == c.ID })
	if i < 0 {
		return body, fmt.Errorf("no annotation %s in the body", c.ID)
	}
	lines := strings.Split(body, "\n")
	out := append(slices.Clone(lines[:cs[i].Line-1]), strings.Split(c.Print(), "\n")...)
	return strings.Join(append(out, lines[cs[i].End:]...), "\n"), nil
}
