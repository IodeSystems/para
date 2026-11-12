package paramd

import (
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/iodesystems/para/internal/recur"
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
	CodeLinkFormer   = "link-former"     // a link names a former id; it resolves, relink fixes it
	CodeFormerTaken  = "former-id-taken" // a file is named by an id another document lists in former_ids
	CodeBlockBroken  = "block-unresolved"
	CodeNoTranscript = "transcript-missing"
	CodeSummaryLong  = "summary-long"
	CodeRecur        = "recur"
	CodeScope        = "scope"
	CodeAnnotation   = "annotation"
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
	stemRe      = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*(?:\.source)?$`)
	blockIDRe   = regexp.MustCompile(`^[A-Za-z0-9-]+$`)
	ruleIDRe    = regexp.MustCompile(`^R\d+$`)
	dateRe      = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	agentOwerRe = regexp.MustCompile(`^agent:[a-z0-9]+(?:[-_.][a-z0-9]+)*$`)
	linkValRe   = regexp.MustCompile(`^\[\[([^\[\]|#]+)(?:#\^([A-Za-z0-9-]+))?\]\]$`)
	hashRe      = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	// pointerTargetRe accepts a URL with any scheme, or a path. ~/ is kept as written.
	pointerTargetRe = regexp.MustCompile(`^(?:[a-z][a-z0-9+.-]*://\S+|(?:/|~/|\./|\.\./)\S*)$`)
	htmlTagRe       = regexp.MustCompile(`</?[A-Za-z][A-Za-z0-9-]*(?:\s[^<>]*)?/?>|<!--|<![A-Za-z]|<\?`)
	imageRe         = regexp.MustCompile(`!\[([^\]]*)\]\(([^)\s]*)`)
	urlRe           = regexp.MustCompile(`https?://[^\s)>\]"'<]+`)
	sepRowRe        = regexp.MustCompile(`^\s*\|?\s*:?-+:?\s*(?:\|\s*:?-+:?\s*)*\|?\s*$`)
	mdSuffixRe      = regexp.MustCompile(`(?i)\.md$`)
	journalRe       = regexp.MustCompile(`^%%\s*para:(begin|end)\s+([a-z0-9][a-z0-9-]*)\s*%%$`)
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
	l.annotations()
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
	if typ == "policy_exception" && str(f.Frontmatter["policy"]) == "" && str(f.Frontmatter["policy_text"]) == "" {
		l.errf(1, CodeField, "type policy_exception: needs field %q or %q", "policy", "policy_text")
	}
	if typ == "task" {
		l.task()
	}
	if typ == "group" {
		switch kind := str(f.Frontmatter["kind"]); {
		case kind != "domain" && f.Frontmatter["org"] == nil:
			l.errf(1, CodeField, "type group: missing required field %q", "org")
		case kind == "domain" && f.Frontmatter["owner"] == nil:
			l.errf(1, CodeField, "type group: kind domain requires field %q", "owner")
		}
	}
	if typ == "task" {
		if r := str(f.Frontmatter["recur"]); r != "" {
			if _, err := recur.Parse(r); err != nil {
				l.add(1, CodeRecur, Warn, "%v: para will not create the next task from it", err)
			}
		}
	}
	if typ == "area" {
		l.area()
	}
	if typ == "goal" && str(f.Frontmatter["status"]) == "parked" && strings.TrimSpace(str(f.Frontmatter["resume_when"])) == "" {
		l.add(1, CodeField, Warn, "type goal: status parked needs field %q: what it waits on, a condition or a date", "resume_when")
	}
	if typ == "project" {
		if c := str(f.Frontmatter["cadence"]); c != "" {
			if _, err := recur.Parse(c); err != nil {
				l.add(1, CodeRecur, Warn, "cadence: %v: health reads the state line as stale after 14 days", err)
			}
		}
	}
	if typ == "question" {
		l.question()
	}
	if typ == "meeting" {
		l.meeting()
	}
	if typ == "obligation" {
		l.obligation()
	}
	if typ == "io" {
		l.io()
	}
	if typ == "tracked_doc" {
		l.trackedDoc()
	}
	if typ == "source" {
		l.source()
	}
	if typ == "transcript" && strings.HasSuffix(f.Name, ".source") {
		m := linkValRe.FindStringSubmatch(str(f.Frontmatter["of"]))
		v, versioned := f.Frontmatter["version"].(int)
		switch {
		case m == nil:
		case versioned:
			if want := fmt.Sprintf("%s-v%d.source", m[1], v); want != f.Name {
				l.errf(1, CodeFieldValue, "field %q: version %d of %q is file %q, not %q", "version", v, m[1], want, f.Name)
			}
		case m[1]+".source" != f.Name:
			l.errf(1, CodeFieldValue, "field %q: %q does not match file name %q (expected %q)", "of", m[1], f.Name, m[1]+".source")
		}
	}
}

// checkVia checks a {source, ref} map: a private connector and its own id.
func (l *linter) checkVia() {
	v, ok := l.f.Frontmatter["via"]
	if !ok || v == nil {
		return
	}
	m, _ := v.(map[string]any)
	if strings.TrimSpace(str(m["source"])) == "" || strings.TrimSpace(str(m["ref"])) == "" {
		l.errf(1, CodeFieldValue, "field %q: want {source, ref}, both non-empty", "via")
	}
	for k := range m {
		if k != "source" && k != "ref" && k != "thread" {
			l.errf(1, CodeFieldValue, "field %q: unknown key %q; want source, ref, and optional thread", "via", k)
		}
	}
}

// commentKeys are the keys of a tracked_doc comment.
var commentKeys = map[string]bool{"id": true, "by": true, "at": true, "url": true, "text": true, "quote": true, "version": true,
	"start": true, "end": true, "anchor": true, "became": true, "resolution": true, "resolved_at": true, "note": true}

var commentIDRe = regexp.MustCompile(`^c[1-9][0-9]*$`)

// trackedDoc checks what the field kinds cannot: via, last_seen within the
// versions, and each comment.
func (l *linter) trackedDoc() {
	fm := l.f.Frontmatter
	l.checkVia()
	latest, _ := fm["latest"].(int)
	if seen, ok := fm["last_seen"].(int); ok && latest > 0 && seen > latest {
		l.errf(1, CodeFieldValue, "field %q: %d is after latest %d", "last_seen", seen, latest)
	}
	list, _ := fm["comments"].([]any)
	ids := map[string]bool{}
	for i, e := range list {
		c, ok := e.(map[string]any)
		if !ok {
			continue // kMaps reports it
		}
		bad := func(format string, a ...any) {
			l.errf(1, CodeFieldValue, "field %q: comment %d: %s", "comments", i+1, fmt.Sprintf(format, a...))
		}
		id := str(c["id"])
		switch {
		case !commentIDRe.MatchString(id):
			bad("id %q: want c<n>", id)
		case ids[id]:
			bad("id %q repeats", id)
		}
		ids[id] = true
		for k := range c {
			if !commentKeys[k] {
				bad("unknown key %q", k)
			}
		}
		if strings.TrimSpace(str(c["by"])) == "" || strings.TrimSpace(str(c["text"])) == "" {
			bad("needs by and text")
		}
		switch at := c["at"].(type) {
		case time.Time:
		case string:
			if _, err := time.Parse(time.RFC3339Nano, at); err != nil {
				bad("at %q: want an RFC 3339 time", at)
			}
		default:
			bad("needs at, an RFC 3339 time")
		}
		if v, ok := c["version"].(int); !ok || v < 1 || (latest > 0 && v > latest) {
			bad("version %v: want a version from 1 to latest", c["version"])
		}
		res := str(c["resolution"])
		switch res {
		case "", "resolved", "dropped":
		case "became":
			if !linkValRe.MatchString(str(c["became"])) {
				bad("resolution became needs became, a quoted wikilink")
			}
		default:
			bad("resolution %q: want became, resolved, or dropped", res)
		}
		if b := str(c["became"]); b != "" && !linkValRe.MatchString(b) {
			bad("became %q is not a wikilink like \"[[name]]\"", b)
		}
	}
}

// task checks what the field kinds cannot: a task has a goal or an area,
// except a meeting or a call; scheduled is a date or an RFC 3339 time; end
// needs a timed scheduled and follows it.
func (l *linter) task() {
	fm := l.f.Frontmatter
	kind := str(fm["kind"])
	if fm["project"] == nil && fm["goal"] == nil && fm["area"] == nil && kind != "meeting" && kind != "call" && kind != "incident" {
		l.errf(1, CodeField, "type task: needs field %q or %q", "goal", "area")
	}
	// A task reaches a project only through a goal, except a task a cron
	// made (cron:). Below format 11 the vault reads a task's project: as
	// goal: before lint (vault legacy.go).
	if fm["project"] != nil && fm["cron"] == nil {
		l.errf(1, CodeFieldValue, "field %q: a task reaches a project through a goal: link the goal (goal:); only a task a cron made (cron:) links a project", "project")
	}
	if kind == "incident" {
		for _, k := range []string{"severity", "started"} {
			if fm[k] == nil {
				l.errf(1, CodeField, "type task: kind incident requires field %q", k)
			}
		}
	}
	for _, k := range []string{"calendar_uid", "calendar_uids"} {
		if fm[k] != nil && kind != "meeting" && kind != "call" {
			l.errf(1, CodeFieldValue, "field %q: only a task of kind meeting or call has a calendar event", k)
		}
	}
	l.slot()
}

// obligation checks what the field kinds cannot: a series has recur, ongoing
// has cadence, a closed one has an outcome, and the slot reads as a task's.
func (l *linter) obligation() {
	fm := l.f.Frontmatter
	switch str(fm["schedule"]) {
	case "series":
		if str(fm["recur"]) == "" {
			l.errf(1, CodeField, "type obligation: schedule series requires field %q", "recur")
		}
	case "ongoing":
		if str(fm["cadence"]) == "" {
			l.errf(1, CodeField, "type obligation: schedule ongoing requires field %q", "cadence")
		}
	}
	// A cron is a series on a project: makes needs both.
	if fm["makes"] != nil && (str(fm["schedule"]) != "series" || fm["project"] == nil) {
		l.errf(1, CodeFieldValue, "field %q: a cron is a series on a project: needs schedule series and field %q", "makes", "project")
	}
	for _, k := range []string{"recur", "cadence"} {
		if r := str(fm[k]); r != "" {
			if _, err := recur.Parse(r); err != nil {
				l.add(1, CodeRecur, Warn, "%s: %v: para will not make the next occurrence or review from it", k, err)
			}
		}
	}
	if str(fm["state"]) == "closed" && str(fm["outcome"]) == "" {
		l.errf(1, CodeField, "type obligation: state closed requires field %q", "outcome")
	}
	l.slot()
}

// slot checks scheduled and end: scheduled is a date or an RFC 3339 time; end
// needs a timed scheduled and follows it.
func (l *linter) slot() {
	fm := l.f.Frontmatter
	if fm["scheduled"] == nil {
		if fm["end"] != nil {
			l.errf(1, CodeFieldValue, "field %q: needs scheduled", "end")
		}
		return
	}
	switch x := fm["scheduled"].(type) {
	case string:
		if t, err := time.Parse(time.RFC3339Nano, x); err == nil {
			if end := timestampOf(fm["end"]); !end.IsZero() && !end.After(t) {
				l.errf(1, CodeFieldValue, "field %q: must be after scheduled", "end")
			}
			return
		}
	case time.Time:
		if !isDate(x) {
			return
		}
	}
	if fm["end"] != nil {
		l.errf(1, CodeFieldValue, "field %q: a date has no end; give scheduled a start time", "end")
	}
}

// area checks what the field kinds cannot: a cadence para can read, and no
// concepts on a concept area.
func (l *linter) area() {
	fm := l.f.Frontmatter
	if c := str(fm["cadence"]); c != "" {
		if _, err := recur.Parse(c); err != nil {
			l.add(1, CodeRecur, Warn, "cadence: %v: health will not report the area as neglected", err)
		}
	}
	if str(fm["kind"]) == "concept" && fm["concepts"] != nil {
		l.errf(1, CodeFieldValue, "field %q: a concept area draws on no concepts; only a project area has them", "concepts")
	}
}

// meeting checks what the field kinds cannot: a booked or held meeting has a
// start and an end, and the end is after the start.
func (l *linter) meeting() {
	fm := l.f.Frontmatter
	status := str(fm["status"])
	start, end := timestampOf(fm["start"]), timestampOf(fm["end"])
	if status == "scheduled" || status == "held" {
		for _, k := range []string{"start", "end"} {
			if fm[k] == nil {
				l.errf(1, CodeField, "type meeting: status %s requires field %q", status, k)
			}
		}
	}
	if (fm["start"] == nil) != (fm["end"] == nil) {
		l.errf(1, CodeFieldValue, "fields %q and %q: give both or neither", "start", "end")
	}
	if !start.IsZero() && !end.IsZero() && !end.After(start) {
		l.errf(1, CodeFieldValue, "field %q: must be after start", "end")
	}
}

// timestampOf reads an RFC 3339 value that YAML gave as a time or a string;
// the zero time when it is neither.
func timestampOf(v any) time.Time {
	switch x := v.(type) {
	case time.Time:
		return x
	case string:
		t, _ := time.Parse(time.RFC3339Nano, x)
		return t
	}
	return time.Time{}
}

// question checks what the field kinds cannot: recommended names one option.
func (l *linter) question() {
	fm := l.f.Frontmatter
	rec := str(fm["recommended"])
	opts, _ := fm["options"].([]any)
	if list, ok := fm["options"].([]any); ok && len(list) == 0 {
		l.errf(1, CodeFieldValue, "field %q: omit it or list the options", "options")
	}
	if rec == "" {
		return
	}
	for _, o := range opts {
		if o == rec {
			return
		}
	}
	l.errf(1, CodeFieldValue, "field %q: %q is not one of the options", "recommended", rec)
}

// io checks what the field kinds cannot: the state belongs to the direction,
// an outgoing message says whether it expects a response, an incoming one has
// no route, and via names a
// source and a reference.
func (l *linter) io() {
	fm := l.f.Frontmatter
	dir, state := str(fm["direction"]), str(fm["state"])
	in := map[string]bool{"unread": true, "read": true, "filed": true, "dropped": true}
	out := map[string]bool{"draft": true, "ready": true, "sent": true, "dropped": true, "superseded": true}
	switch {
	case dir == "in" && state != "" && !in[state]:
		l.errf(1, CodeFieldValue, "field %q: %q is for an outgoing message; incoming takes unread, read, filed, dropped", "state", state)
	case dir == "out" && state != "" && !out[state]:
		l.errf(1, CodeFieldValue, "field %q: %q is for an incoming message; outgoing takes draft, ready, sent, dropped, superseded", "state", state)
	}
	// On an incoming message expects response means the sender waits for a
	// reply; route is only for the Ask an outgoing one makes.
	_, hasExp := fm["expects"]
	switch {
	case dir == "out" && !hasExp:
		l.errf(1, CodeField, "type io: an outgoing message needs field %q (response or none)", "expects")
	case dir == "in" && str(fm["route"]) != "":
		l.errf(1, CodeFieldValue, "field %q: only an outgoing message has a route", "route")
	}
	if dir == "out" && str(fm["expects"]) == "response" && str(fm["route"]) == "" {
		l.errf(1, CodeField, "type io: expects response needs field %q, the domain the Ask goes to", "route")
	}
	// A sent action (to no person) becomes nothing: its ref says where it is.
	sentAction := state == "sent" && str(fm["ref"]) != "" && str(fm["expects"]) == "none"
	// A logged contact (date set: an interaction before format 10) is the record itself.
	logged := fm["date"] != nil
	if logged && state != "filed" && state != "sent" {
		l.errf(1, CodeFieldValue, "field %q: a logged contact (with date) is filed or sent, not %q", "state", state)
	}
	if (state == "filed" || state == "sent") && str(fm["became"]) == "" && !sentAction && !logged {
		l.errf(1, CodeField, "type io: state %s needs field %q", state, "became")
	}
	l.checkVia()
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
	case kTimestamp:
		switch x := v.(type) {
		case time.Time:
		case string:
			if _, err := time.Parse(time.RFC3339Nano, x); err != nil {
				bad("must be an RFC 3339 time such as 2026-11-01T09:00:00Z, got %q", x)
			}
		default:
			bad("must be an RFC 3339 time, got %v", v)
		}
	case kWhen:
		if _, err := time.Parse(time.RFC3339Nano, str(v)); err != nil && !isDate(v) {
			bad("must be a date YYYY-MM-DD or an RFC 3339 time such as 2026-11-01T09:00:00Z, got %v", v)
		}
	case kDate:
		if !isDate(v) {
			bad("must be a date YYYY-MM-DD, got %v", v)
		}
	case kBool:
		if _, ok := v.(bool); !ok {
			bad("must be true or false, got %v", v)
		}
	case kPercent:
		if n, ok := v.(int); !ok || n < 0 || n > 100 {
			bad("must be a whole number from 0 to 100, got %v", v)
		}
	case kCount:
		if n, ok := v.(int); !ok || n < 1 {
			bad("must be a whole number, 1 or more, got %v", v)
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
	case kOwner:
		s, ok := v.(string)
		m := linkValRe.FindStringSubmatch(s)
		switch {
		case ok && s == "self":
		case !ok:
			bad("must be one quoted wikilink such as \"[[person-name]]\", or the word self")
		case m == nil:
			bad("%q is not a wikilink like \"[[name]]\", and is not self", s)
		case m[2] != "":
			bad("%q must not carry a block id", s)
		}
	case kScope:
		s, ok := v.(string)
		m := linkValRe.FindStringSubmatch(s)
		switch {
		case ok && s == "root":
		case !ok:
			bad("must be one quoted wikilink such as \"[[acme-portal]]\", or the word root (an unquoted [[name]] is parsed as a YAML list, and two scopes are not allowed)")
		case m == nil:
			bad("%q is not a wikilink like \"[[name]]\", and is not root", s)
		case m[2] != "":
			bad("%q must not carry a block id", s)
		}
	case kMap:
		if _, ok := v.(map[string]any); !ok {
			bad("must be a mapping")
		}
	case kMaps:
		list, ok := v.([]any)
		if !ok {
			bad("must be a list of mappings")
			return
		}
		for i, e := range list {
			if _, ok := e.(map[string]any); !ok {
				bad("item %d must be a mapping", i+1)
			}
		}
	case kSourceMap:
		l.checkSource(fd.Name, v)
	case kThreads, kThreads1:
		keys := []any{v}
		if fd.Kind == kThreads {
			list, ok := v.([]any)
			if !ok {
				bad("must be a list of thread keys")
				return
			}
			keys = list
		}
		for _, e := range keys {
			s, _ := e.(string)
			if _, _, err := ParseThreadKey(s); err != nil {
				bad("%v is not a thread key: an item id, #tag, or channel:<source>/<thread>", e)
				return
			}
		}
	case kFollows:
		m, ok := v.(map[string]any)
		if !ok {
			bad("must be a mapping with source and verified")
			return
		}
		l.checkSource(fd.Name+".source", m["source"])
		if _, ok := m["verified"].(bool); !ok {
			bad("verified must be true or false")
		}
	case kCalendarUIDs:
		list, ok := v.([]any)
		if !ok {
			bad("must be a list of {uid, role} mappings")
			return
		}
		seen := map[string]bool{}
		for i, e := range list {
			m, ok := e.(map[string]any)
			if !ok {
				bad("item %d must be a mapping with uid and an optional role", i+1)
				continue
			}
			for k := range m {
				if k != "uid" && k != "role" {
					bad("item %d: unknown key %q (uid, role)", i+1, k)
				}
			}
			uid, _ := m["uid"].(string)
			switch {
			case strings.TrimSpace(uid) == "" || strings.ContainsAny(uid, "\r\n"):
				bad("item %d: uid must be one non-empty line", i+1)
			case seen[uid]:
				bad("item %d: uid %q is listed twice", i+1, uid)
			}
			seen[uid] = true
			if r, ok := m["role"]; ok {
				if s, ok := r.(string); !ok || strings.ContainsAny(s, "\r\n") {
					bad("item %d: role must be one line of text", i+1)
				}
			}
		}
	case kImported:
		m, ok := v.(map[string]any)
		if !ok {
			bad("must be a mapping with path and hash")
			return
		}
		if s, ok := m["path"].(string); !ok || strings.TrimSpace(s) == "" {
			bad("path must be a non-empty string")
		}
		if h, _ := m["hash"].(string); !hashRe.MatchString(h) {
			bad("hash must be sha256:<64 lowercase hex characters>")
		}
	case kOwers:
		list, ok := v.([]any)
		if !ok {
			bad("must be a list of quoted wikilinks, or the word agent or agent:<name>")
			return
		}
		for _, e := range list {
			s, _ := e.(string)
			if s == "agent" || agentOwerRe.MatchString(s) {
				continue
			}
			if m := linkValRe.FindStringSubmatch(s); m == nil || m[2] != "" {
				bad("%v is not a wikilink like \"[[name]]\", agent, or agent:<name>", e)
				return
			}
		}
	case kProduce:
		m, ok := v.(map[string]any)
		if !ok {
			bad("must be a mapping with kind, and optional template, to, medium, guide")
			return
		}
		for k := range m {
			if !slices.Contains([]string{"kind", "template", "to", "medium", "guide", "builtin"}, k) {
				bad("unknown key %q (kind, template, to, medium, guide, builtin)", k)
			}
		}
		if k, _ := m["kind"].(string); !slices.Contains(produceKinds, k) {
			bad("kind must be one of %s", strings.Join(produceKinds, ", "))
		}
		for _, k := range []string{"template", "guide", "builtin"} {
			if x, present := m[k]; present {
				if _, ok := x.(string); !ok {
					bad("%s must be text", k)
				}
			}
		}
		if x, present := m["medium"]; present {
			if s, _ := x.(string); !slices.Contains(mediums, s) {
				bad("medium must be one of %s", strings.Join(mediums, ", "))
			}
		}
		if x, present := m["to"]; present {
			list, ok := x.([]any)
			if !ok {
				bad("to must be a list")
			}
			for _, e := range list {
				if _, ok := e.(string); !ok {
					bad("to: items must be strings, got %v", e)
				}
			}
		}
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
	case kContacts, kTargets, kPointers, kSources, kRisks, kAssumptions, kAssessments, kRevisions, kAttempts:
		items := contactFields
		switch fd.Kind {
		case kRisks:
			items = riskFields
		case kAssumptions:
			items = assumptionFields
		case kAssessments:
			items = assessmentFields
		case kRevisions:
			items = revisionFields
		case kAttempts:
			items = attemptFields
		case kTargets:
			items = targetFields
		case kPointers:
			items = pointerFields
		case kSources:
			items = sourceFields
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
				if fd.Kind == kTargets && sub.Name == "state" && (sv == "requested" || sv == "scheduled") && m["medium"] != "meeting" {
					bad("item %d: state %v applies to medium meeting only", i+1, sv)
				}
				if fd.Kind == kPointers && sub.Name == "target" {
					if s, ok := sv.(string); ok && !pointerTargetRe.MatchString(s) {
						bad("item %d: target %q must be a URL (scheme://...) or a path starting with /, ~/, ./ or ../", i+1, s)
					}
				}
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
	if pu, err := url.Parse(u); u == "" && str(m["verify"]) != "" {
		// A no the owner heard with no link cites where and when instead.
	} else if err != nil || (pu.Scheme != "http" && pu.Scheme != "https") || pu.Host == "" {
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
	// An area that follows a source keeps the rules of the policy it was.
	isPolicy := f.Type() == "policy" || f.Type() == "area" && f.Frontmatter["follows"] != nil
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
			l.errf(b.Line, CodeRule, "rule block ^%s is only allowed in a policy file or an area that follows a source", b.ID)
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
		if len(f.Rules) == 0 && f.Type() == "policy" {
			l.add(0, CodeRule, Warn, "policy has no rules")
		}
	}
}

// annotations warns on a callout header para cannot read and on an id used
// twice. The text stays as written either way.
func (l *linter) annotations() {
	seen := map[string]int{}
	for _, c := range l.f.Callouts {
		if c.Broken != "" {
			l.add(c.Line, CodeAnnotation, Warn, "%s callout: %s; para does not read it as an annotation", c.Kind, c.Broken)
			continue
		}
		if first, dup := seen[c.ID]; dup {
			l.add(c.Line, CodeAnnotation, Warn, "annotation id %s already used on line %d", c.ID, first)
		}
		seen[c.ID] = c.Line
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
