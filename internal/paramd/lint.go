package paramd

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// Severity of a finding.
type Severity string

const (
	Error Severity = "error"
	Warn  Severity = "warn"
)

// Lint codes. Each is documented in docs/format.md.
const (
	CodeEncoding     = "encoding"
	CodeLineEnding   = "line-ending"
	CodeFrontmatter  = "frontmatter"
	CodeType         = "type"
	CodeField        = "field"
	CodeFieldValue   = "field-value"
	CodeSource       = "source"
	CodeTitle        = "title"
	CodeHTML         = "raw-html"
	CodeFilename     = "filename"
	CodeTable        = "table"
	CodeImage        = "image"
	CodeURL          = "url"
	CodeRule         = "rule"
	CodeBlockID      = "block-id"
	CodeWikilink     = "wikilink"
	CodeSourceLine   = "source-line"
	CodePageMarker   = "page-marker"
	CodeJournalMark  = "journal-marker"
	CodeDupFilename  = "filename-duplicate"
	CodeLinkBroken   = "link-unresolved"
	CodeBlockBroken  = "block-unresolved"
	CodeNoTranscript = "transcript-missing"
	CodeSummaryLong  = "summary-long"
)

// MaxRuleLen is the longest allowed rule line, in characters. raglit
// fragments are 1000-character windows that ignore headings, so a rule must
// be understood inside one window.
const MaxRuleLen = 500

// MaxSummaryLen is the longest interaction body that lints clean, in
// characters. An interaction holds a summary in the user's words and a link,
// not a copy of the message.
const MaxSummaryLen = 500

// Finding is one lint result.
type Finding struct {
	Path     string
	Line     int // 1-based; 0 when the finding is about the whole file
	Code     string
	Severity Severity
	Msg      string
}

func (f Finding) String() string {
	loc := f.Path
	if f.Line > 0 {
		loc = fmt.Sprintf("%s:%d", f.Path, f.Line)
	}
	return fmt.Sprintf("%s: %s [%s] %s", loc, f.Severity, f.Code, f.Msg)
}

var (
	stemRe     = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*(?:\.source)?$`)
	blockIDRe  = regexp.MustCompile(`^[A-Za-z0-9-]+$`)
	ruleIDRe   = regexp.MustCompile(`^R\d+$`)
	dateRe     = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	linkValRe  = regexp.MustCompile(`^\[\[([^\[\]|#]+)(?:#\^([A-Za-z0-9-]+))?\]\]$`)
	hashRe     = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	htmlTagRe  = regexp.MustCompile(`</?[A-Za-z][A-Za-z0-9-]*(?:\s[^<>]*)?/?>|<!--|<![A-Za-z]|<\?`)
	imageRe    = regexp.MustCompile(`!\[([^\]]*)\]\(([^)\s]*)`)
	urlRe      = regexp.MustCompile(`https?://[^\s)>\]"'<]+`)
	sepRowRe   = regexp.MustCompile(`^\s*\|?\s*:?-+:?\s*(?:\|\s*:?-+:?\s*)*\|?\s*$`)
	mdSuffixRe = regexp.MustCompile(`(?i)\.md$`)
	journalRe  = regexp.MustCompile(`^%%\s*para:(begin|end)\s+([a-z0-9][a-z0-9-]*)\s*%%$`)
)

// signedQueryKeys are query parameters that carry a credential or signature.
var signedQueryKeys = map[string]bool{
	"x-amz-signature": true, "x-amz-credential": true, "x-amz-security-token": true,
	"x-goog-signature": true, "x-goog-credential": true, "awsaccesskeyid": true,
	"signature": true, "sig": true, "token": true, "access_token": true,
	"id_token": true, "api_key": true, "apikey": true, "key": true,
	"auth": true, "password": true, "secret": true, "sas": true,
}

// Lint checks one file against the para-md rules. Cross-file rules (duplicate
// names, unresolved links) are in CheckVault.
func Lint(f *File) []Finding {
	l := &linter{f: f}
	l.encoding()
	l.frontmatter()
	l.filename()
	l.title()
	l.html()
	l.tables()
	l.images()
	l.urls()
	l.links()
	l.blocksAndRules()
	l.transcript()
	l.journal()
	l.interaction()
	sort.SliceStable(l.out, func(i, j int) bool { return l.out[i].Line < l.out[j].Line })
	return l.out
}

type linter struct {
	f   *File
	out []Finding
}

func (l *linter) add(line int, code string, sev Severity, format string, a ...any) {
	l.out = append(l.out, Finding{Path: l.f.Path, Line: line, Code: code, Severity: sev, Msg: fmt.Sprintf(format, a...)})
}

func (l *linter) errf(line int, code, format string, a ...any) {
	l.add(line, code, Error, format, a...)
}

func (l *linter) encoding() {
	if !l.f.ValidUTF8 {
		l.errf(0, CodeEncoding, "file is not valid UTF-8")
	}
	if l.f.HasBOM {
		l.errf(1, CodeEncoding, "file starts with a byte order mark")
	}
	if l.f.HasCR {
		l.errf(0, CodeLineEnding, "file contains CR characters; use LF line endings")
	}
}

func (l *linter) frontmatter() {
	f := l.f
	if f.FMErr != "" {
		l.errf(1, CodeFrontmatter, "%s", f.FMErr)
		return
	}
	typ := f.Type()
	if _, ok := f.Frontmatter["type"]; !ok {
		l.errf(1, CodeType, "missing required field %q", "type")
		return
	}
	known := false
	for _, t := range Types {
		if t == typ {
			known = true
		}
	}
	if !known {
		l.errf(1, CodeType, "unknown type %q", fmt.Sprint(f.Frontmatter["type"]))
		return
	}
	fields := append(append([]field(nil), commonFields...), typeFields[typ]...)
	for _, fd := range fields {
		v, present := f.Frontmatter[fd.Name]
		if !present || v == nil {
			if fd.Required {
				l.errf(1, CodeField, "type %s: missing required field %q", typ, fd.Name)
			}
			continue
		}
		l.checkValue(fd, v)
	}
	if typ == "policy_exception" && f.Frontmatter["kind"] == "conflict_with" {
		if _, ok := f.Frontmatter["with"]; !ok {
			l.errf(1, CodeField, "type policy_exception: kind conflict_with requires field %q", "with")
		}
	}
	if typ == "transcript" && strings.HasSuffix(f.Name, ".source") {
		if m := linkValRe.FindStringSubmatch(str(f.Frontmatter["of"])); m != nil && m[1]+".source" != f.Name {
			l.errf(1, CodeFieldValue, "field %q: %q does not match file name %q (expected %q)", "of", m[1], f.Name, m[1]+".source")
		}
	}
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

func isDate(v any) bool {
	switch x := v.(type) {
	case time.Time:
		return x.Hour() == 0 && x.Minute() == 0 && x.Second() == 0
	case string:
		return dateRe.MatchString(x)
	}
	return false
}

func (l *linter) checkValue(fd field, v any) {
	bad := func(format string, a ...any) {
		l.errf(1, CodeFieldValue, "field %q: %s", fd.Name, fmt.Sprintf(format, a...))
	}
	switch fd.Kind {
	case kString:
		if s, ok := v.(string); !ok || strings.TrimSpace(s) == "" {
			bad("must be a non-empty string")
		}
	case kDate:
		if !isDate(v) {
			bad("must be a date YYYY-MM-DD, got %v", v)
		}
	case kBool:
		if _, ok := v.(bool); !ok {
			bad("must be true or false, got %v", v)
		}
	case kEnum:
		s, _ := v.(string)
		for _, e := range fd.Enum {
			if s == e {
				return
			}
		}
		bad("%v is not one of %s", v, strings.Join(fd.Enum, ", "))
	case kList:
		list, ok := v.([]any)
		if !ok {
			bad("must be a list")
			return
		}
		for _, e := range list {
			if _, ok := e.(string); !ok {
				bad("list items must be strings, got %v", e)
				return
			}
		}
	case kLink, kRuleLink:
		s, ok := v.(string)
		m := linkValRe.FindStringSubmatch(s)
		switch {
		case !ok:
			bad("must be a quoted wikilink such as \"[[name]]\" (an unquoted [[name]] is parsed as a YAML list)")
		case m == nil:
			bad("%q is not a wikilink like \"[[name]]\"", s)
		case fd.Kind == kRuleLink && m[2] == "":
			bad("%q must point at a rule block, like \"[[policy-name#^R4]]\"", s)
		case fd.Kind == kLink && m[2] != "":
			bad("%q must not carry a block id", s)
		}
	case kSourceMap:
		l.checkSource(fd.Name, v)
	case kLinkList:
		list, ok := v.([]any)
		if !ok || (fd.Required && len(list) == 0) {
			bad("must be a non-empty list of quoted wikilinks")
			return
		}
		for _, e := range list {
			s, _ := e.(string)
			if m := linkValRe.FindStringSubmatch(s); m == nil || m[2] != "" {
				bad("%v is not a wikilink like \"[[name]]\"", e)
				return
			}
		}
	case kContacts, kTargets:
		items := contactFields
		if fd.Kind == kTargets {
			items = targetFields
		}
		list, ok := v.([]any)
		if !ok {
			bad("must be a list of mappings")
			return
		}
		for i, e := range list {
			m, ok := e.(map[string]any)
			if !ok {
				bad("item %d must be a mapping", i+1)
				continue
			}
			for _, sub := range items {
				sv, present := m[sub.Name]
				if !present || sv == nil {
					if sub.Required {
						bad("item %d: missing %q", i+1, sub.Name)
					}
					continue
				}
				l.checkValue(field{Name: fmt.Sprintf("%s[%d].%s", fd.Name, i+1, sub.Name), Kind: sub.Kind, Enum: sub.Enum, Required: sub.Required}, sv)
			}
		}
	}
}

func (l *linter) checkSource(name string, v any) {
	bad := func(format string, a ...any) {
		l.errf(1, CodeSource, "field %q: %s", name, fmt.Sprintf(format, a...))
	}
	m, ok := v.(map[string]any)
	if !ok {
		bad("must be a mapping with url, retrieved, hash, fidelity")
		return
	}
	u := str(m["url"])
	if pu, err := url.Parse(u); err != nil || (pu.Scheme != "http" && pu.Scheme != "https") || pu.Host == "" {
		bad("url %q must be an absolute http(s) URL", u)
	}
	switch r := m["retrieved"].(type) {
	case time.Time:
	case string:
		if _, err := time.Parse(time.RFC3339, r); err != nil && !dateRe.MatchString(r) {
			bad("retrieved %q must be an RFC 3339 timestamp or YYYY-MM-DD", r)
		}
	default:
		bad("missing retrieved (RFC 3339 timestamp)")
	}
	fid, _ := m["fidelity"].(string)
	okFid := false
	for _, e := range fidelities {
		if fid == e {
			okFid = true
		}
	}
	if !okFid {
		bad("fidelity %v is not one of %s", m["fidelity"], strings.Join(fidelities, ", "))
	}
	h, hasHash := m["hash"].(string)
	switch {
	case hasHash && !hashRe.MatchString(h):
		bad("hash must be sha256:<64 hex characters>")
	case !hasHash && fid != "none":
		bad("missing hash (optional only when fidelity is none)")
	}
	for _, k := range []string{"complete", "untrusted"} {
		if x, ok := m[k]; ok {
			if _, isBool := x.(bool); !isBool {
				bad("%s must be true or false", k)
			}
		}
	}
}

func (l *linter) filename() {
	f := l.f
	if !stemRe.MatchString(f.Name) {
		l.errf(0, CodeFilename, "file name %q must be lowercase kebab-case ([a-z0-9] groups joined by '-')", f.Name)
		return
	}
	isSrc := strings.HasSuffix(f.Name, ".source")
	switch typ := f.Type(); {
	case typ == "transcript" && !isSrc:
		l.errf(0, CodeFilename, "transcript file name must end in .source.md")
	case typ != "transcript" && isSrc && typ != "":
		l.errf(0, CodeFilename, "only transcripts may end in .source.md")
	}
}

func (l *linter) title() {
	f := l.f
	if f.FMErr != "" && !f.HasFM {
		// Frontmatter problems are already reported; a title check would only add noise.
		if len(f.Lines) > 0 && f.Lines[0].Text != "---" {
			l.errf(f.BodyLine, CodeTitle, "first non-blank line after frontmatter must be '# Title'")
		}
		return
	}
	if f.Title == "" {
		l.errf(f.BodyLine, CodeTitle, "first non-blank line after frontmatter must be '# Title'")
	}
	for _, ln := range f.Lines {
		if ln.Code || ln.FM || ln.N == f.TitleLine {
			continue
		}
		if titleRe.MatchString(ln.Text) {
			l.errf(ln.N, CodeTitle, "second level-1 heading; a file has one '# Title'")
		}
	}
}

func (l *linter) html() {
	for _, ln := range l.f.Lines {
		if ln.Code || ln.FM {
			continue
		}
		if m := htmlTagRe.FindString(ln.Prose); m != "" {
			l.errf(ln.N, CodeHTML, "raw HTML %q is not allowed", m)
		}
	}
}

func splitCells(s string) []string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "|")
	if strings.HasSuffix(s, "|") && !strings.HasSuffix(s, `\|`) {
		s = s[:len(s)-1]
	}
	var cells []string
	var cur strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) && s[i+1] == '|' {
			cur.WriteString(`\|`)
			i++
			continue
		}
		if s[i] == '|' {
			cells = append(cells, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(s[i])
	}
	return append(cells, cur.String())
}

func (l *linter) tables() {
	lines := l.f.Lines
	isRow := func(i int) bool {
		return i < len(lines) && !lines[i].Code && !lines[i].FM && strings.Contains(lines[i].Text, "|")
	}
	for i := 0; i < len(lines); i++ {
		if !isRow(i) || !isRow(i+1) || !sepRowRe.MatchString(lines[i+1].Text) || sepRowRe.MatchString(lines[i].Text) {
			continue
		}
		n := len(splitCells(lines[i].Text))
		if sn := len(splitCells(lines[i+1].Text)); sn != n {
			l.errf(lines[i+1].N, CodeTable, "separator row has %d cells, header has %d", sn, n)
		}
		j := i + 2
		for ; isRow(j); j++ {
			if sepRowRe.MatchString(lines[j].Text) {
				l.errf(lines[j].N, CodeTable, "second header or separator row; a table has one header row")
				continue
			}
			if c := len(splitCells(lines[j].Text)); c != n {
				l.errf(lines[j].N, CodeTable, "row has %d cells, header has %d (merged or missing cell)", c, n)
			}
		}
		i = j - 1
	}
}

func (l *linter) images() {
	for _, ln := range l.f.Lines {
		if ln.Code || ln.FM {
			continue
		}
		for _, m := range imageRe.FindAllStringSubmatch(ln.Prose, -1) {
			alt := strings.TrimSpace(m[1])
			base := m[2][strings.LastIndex(m[2], "/")+1:]
			if alt == "" || strings.EqualFold(alt, base) {
				l.errf(ln.N, CodeImage, "image %q needs alt text that describes it", m[2])
			}
		}
		if strings.Contains(ln.Prose, "![[") {
			l.errf(ln.N, CodeImage, "wikilink embed ![[...]] has no description; use ![description](path)")
		}
	}
}

func (l *linter) urls() {
	for _, ln := range l.f.Lines {
		for _, raw := range urlRe.FindAllString(ln.Text, -1) {
			raw = strings.TrimRight(raw, ".,;:!?")
			u, err := url.Parse(raw)
			if err != nil {
				continue
			}
			if u.User != nil {
				l.errf(ln.N, CodeURL, "URL %q carries credentials", redact(raw))
			}
			keys := make([]string, 0)
			for k := range u.Query() {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if signedQueryKeys[strings.ToLower(k)] {
					l.errf(ln.N, CodeURL, "URL has signed or credential query parameter %q; store the stable URL without it", k)
				}
			}
		}
	}
}

func redact(raw string) string {
	if i := strings.Index(raw, "@"); i >= 0 {
		if j := strings.Index(raw, "://"); j >= 0 && j < i {
			return raw[:j+3] + "***" + raw[i:]
		}
	}
	return raw
}

func (l *linter) links() {
	for _, lk := range l.f.Links {
		switch {
		case lk.Target == "":
			l.errf(lk.Line, CodeWikilink, "%s has an empty target", lk.Raw)
		case strings.Contains(lk.Target, "/") || mdSuffixRe.MatchString(lk.Target):
			l.errf(lk.Line, CodeWikilink, "%s: target must be a bare file name without folder or .md", lk.Raw)
		case !stemRe.MatchString(lk.Target):
			l.errf(lk.Line, CodeWikilink, "%s: target %q is not a valid file name", lk.Raw, lk.Target)
		}
		if lk.Block != "" && !blockIDRe.MatchString(lk.Block) {
			l.errf(lk.Line, CodeWikilink, "%s: bad block id", lk.Raw)
		}
		if strings.Contains(lk.Raw, "#^") && lk.Block == "" {
			l.errf(lk.Line, CodeWikilink, "%s: empty block id", lk.Raw)
		}
	}
}

func (l *linter) blocksAndRules() {
	f := l.f
	seen := map[string]int{}
	for _, b := range f.Blocks {
		if first, dup := seen[b.ID]; dup {
			l.errf(b.Line, CodeBlockID, "block id ^%s already used on line %d", b.ID, first)
		}
		seen[b.ID] = b.Line
	}
	isPolicy := f.Type() == "policy"
	ruleAt := map[int]bool{}
	for _, r := range f.Rules {
		ruleAt[r.Line] = true
	}
	for _, b := range f.Blocks {
		if !ruleIDRe.MatchString(b.ID) {
			continue
		}
		switch {
		case !isPolicy:
			l.errf(b.Line, CodeRule, "rule block ^%s is only allowed in a policy file", b.ID)
		case !ruleAt[b.Line]:
			l.errf(b.Line, CodeRule, "rule line must read 'R<n> <policy-id>: <text> ^R<n>' with matching ids")
		}
	}
	for _, r := range f.Rules {
		if isPolicy && r.Policy != f.Name {
			l.errf(r.Line, CodeRule, "rule %s names policy %q, file is %q", r.ID, r.Policy, f.Name)
		}
		if n := utf8.RuneCountInString(f.Lines[r.Line-1].Text); n > MaxRuleLen {
			l.errf(r.Line, CodeRule, "rule %s is %d characters; limit is %d. Split it", r.ID, n, MaxRuleLen)
		}
	}
	if isPolicy {
		for _, ln := range f.Lines {
			if ln.Code || ln.FM || ruleAt[ln.N] {
				continue
			}
			if ruleHeadRe.MatchString(ln.Text) && !blockRe.MatchString(ln.Text) {
				l.errf(ln.N, CodeRule, "rule line has no ^R<n> block id")
			}
		}
		if len(f.Rules) == 0 {
			l.add(0, CodeRule, Warn, "policy has no rules")
		}
	}
}

func (l *linter) transcript() {
	f := l.f
	if f.Type() != "transcript" {
		return
	}
	if f.TitleLine > 0 {
		var got string
		var at int
		for i := f.TitleLine; i < len(f.Lines); i++ {
			if strings.TrimSpace(f.Lines[i].Text) != "" {
				got, at = f.Lines[i].Text, f.Lines[i].N
				break
			}
		}
		want := ""
		if m, ok := f.Frontmatter["source"].(map[string]any); ok {
			want = str(m["url"])
		}
		u, ok := strings.CutPrefix(got, "Source: ")
		switch {
		case !ok:
			l.errf(f.TitleLine, CodeSourceLine, "transcript needs a 'Source: <url>' line right after the title")
		case want != "" && strings.TrimSpace(u) != want:
			l.errf(at, CodeSourceLine, "Source line %q differs from frontmatter source.url %q", strings.TrimSpace(u), want)
		}
	}
	prev := 0
	for _, pm := range f.PageMarks {
		if pm.Raw != fmt.Sprintf("## Page %d", pm.N) {
			l.errf(pm.Line, CodePageMarker, "page marker %q must be exactly '## Page %d'", pm.Raw, pm.N)
		}
		if pm.N < 1 || pm.N <= prev {
			l.errf(pm.Line, CodePageMarker, "page %d does not increase (previous %d)", pm.N, prev)
		}
		prev = pm.N
	}
}

func (l *linter) journal() {
	f := l.f
	open, openLine := "", 0
	names := map[string]bool{}
	for _, ln := range f.Lines {
		if ln.Code || ln.FM {
			continue
		}
		t := strings.TrimSpace(ln.Text)
		m := journalRe.FindStringSubmatch(t)
		if m == nil {
			if strings.HasPrefix(t, "%%") && (strings.Contains(t, "para:begin") || strings.Contains(t, "para:end")) {
				l.errf(ln.N, CodeJournalMark, "malformed marker; use '%%%% para:begin <name> %%%%'")
			}
			continue
		}
		if f.Type() != "journal" {
			l.errf(ln.N, CodeJournalMark, "generated-section markers are only allowed in journal files")
			continue
		}
		switch m[1] {
		case "begin":
			if open != "" {
				l.errf(ln.N, CodeJournalMark, "marker %q opened inside %q (line %d)", m[2], open, openLine)
			}
			if names[m[2]] {
				l.errf(ln.N, CodeJournalMark, "marker name %q used twice", m[2])
			}
			names[m[2]] = true
			open, openLine = m[2], ln.N
		case "end":
			if open != m[2] {
				l.errf(ln.N, CodeJournalMark, "end marker %q does not match open marker %q", m[2], open)
			}
			open = ""
		}
	}
	if open != "" {
		l.errf(openLine, CodeJournalMark, "marker %q is never closed", open)
	}
}

// interaction warns when the summary is long: it should be the user's words
// and a link, not a copy of the message.
func (l *linter) interaction() {
	if l.f.Type() != "interaction" {
		return
	}
	var b []string
	for _, ln := range l.f.Lines {
		if !ln.FM && l.f.TitleLine > 0 && ln.N > l.f.TitleLine {
			b = append(b, ln.Text)
		}
	}
	if n := utf8.RuneCountInString(strings.TrimSpace(strings.Join(b, "\n"))); n > MaxSummaryLen {
		l.add(l.f.TitleLine, CodeSummaryLong, Warn, "summary is %d characters (over %d): write it in your own words and link the original", n, MaxSummaryLen)
	}
}
