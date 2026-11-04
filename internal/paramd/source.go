package paramd

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/iodesystems/para/internal/recur"
)

// CodeSecret is a source field that looks like a token, key, or password. A
// vault holds no secret: a connector keeps its own credentials.
const CodeSecret = "secret"

// CodeSourceUnresolved is a via.source that names no source document, in a
// vault that has source documents. It is a warning: sources are recommended,
// never required.
const CodeSourceUnresolved = "source-unresolved"

// sourceStepKeys are the keys of a source step (fetch, scan_in, scan_out,
// state).
var sourceStepKeys = []string{"tool", "args", "command", "query", "how"}

// SourceSteps are the step fields of a source, in order.
var SourceSteps = []string{"fetch", "scan_in", "scan_out", "state"}

// SourcePlaceholders are the names a step template may hold in braces: {ref}
// and {url} name one item; {cursor} is the cursor of the last sweep; {since}
// is the last scan as RFC 3339 and {since_date} its date; {query} is the
// step's query.
var SourcePlaceholders = []string{"ref", "url", "cursor", "since", "since_date", "query"}

var placeholderRe = regexp.MustCompile(`\{([a-z_]+)\}`)

// secretValueRes match values that look like a credential: well-known token
// prefixes, a private key, a bearer header, a JWT, or a basic-auth URL.
var secretValueRes = []*regexp.Regexp{
	regexp.MustCompile(`\b(?:ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{20,}`),
	regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}`),
	regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}`),
	regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{20,}`),
	regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`),
	regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{30,}`),
	regexp.MustCompile(`\bya29\.[0-9A-Za-z_-]{20,}`),
	regexp.MustCompile(`\bglpat-[0-9A-Za-z_-]{20,}`),
	regexp.MustCompile(`\bATATT[0-9A-Za-z_=-]{20,}`),
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`),
	regexp.MustCompile(`(?i)\b(?:bearer|basic)\s+[A-Za-z0-9._~+/=-]{16,}`),
	regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.`),
	regexp.MustCompile(`(?i)\b(?:token|secret|password|passwd|api[_-]?key|client[_-]?secret)\s*[:=]\s*\S{6,}`),
	regexp.MustCompile(`[a-z][a-z0-9+.-]*://[^/\s:@]+:[^/\s@]+@`),
}

// secretKeyRe matches a key name that would hold a credential.
var secretKeyRe = regexp.MustCompile(`(?i)token|secret|password|passwd|api[_-]?key|private[_-]?key|credential|cookie|auth`)

// longOpaqueRe is a long run with no space, letters and digits mixed: what a
// pasted key looks like. A cursor is opaque by design and skips this check.
var longOpaqueRe = regexp.MustCompile(`[A-Za-z0-9_+/=-]{40,}`)

// LooksSecret reports why s looks like a credential, or "".
func LooksSecret(s string, opaqueOK bool) string {
	for _, re := range secretValueRes {
		if re.MatchString(s) {
			return "it matches " + re.String()
		}
	}
	if !opaqueOK {
		for _, m := range longOpaqueRe.FindAllString(s, -1) {
			if strings.ContainsAny(m, "0123456789") && strings.ContainsAny(m, "abcdefghijklmnopqrstuvwxyz") && strings.ContainsAny(m, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") {
				return "it holds a long opaque string, like a key"
			}
		}
	}
	return ""
}

// source checks what the field kinds cannot: the steps, the via of each step,
// placeholders, cadence, and that no field looks like a secret.
func (l *linter) source() {
	fm := l.f.Frontmatter
	via := str(fm["via"])
	if via == "mcp" && strings.TrimSpace(str(fm["server"])) == "" {
		l.errf(1, CodeField, "type source: via mcp needs field %q, the MCP server name", "server")
	}
	if via != "mcp" && (str(fm["server"]) != "" || fm["tools"] != nil) {
		l.errf(1, CodeFieldValue, "type source: server and tools are for via mcp, not %q", via)
	}
	commands := 0
	for _, name := range SourceSteps {
		v, ok := fm[name]
		if !ok || v == nil {
			continue
		}
		m, ok := v.(map[string]any)
		if !ok {
			continue // kMap reports it
		}
		bad := func(format string, a ...any) {
			l.errf(1, CodeFieldValue, "field %q: %s", name, fmt.Sprintf(format, a...))
		}
		for k, val := range m {
			if !slices.Contains(sourceStepKeys, k) {
				bad("unknown key %q; want %s", k, strings.Join(sourceStepKeys, ", "))
				continue
			}
			if _, ok := val.(string); !ok && val != nil {
				bad("%s must be text", k)
			}
		}
		if len(m) == 0 {
			bad("empty: give tool, command, query, or how")
		}
		switch via {
		case "mcp":
			if str(m["command"]) != "" {
				bad("command is for via cli; via mcp uses tool and args")
			}
		case "cli":
			if str(m["tool"]) != "" || str(m["args"]) != "" {
				bad("tool and args are for via mcp; via cli uses command")
			}
		case "none":
			if str(m["tool"]) != "" || str(m["args"]) != "" || str(m["command"]) != "" {
				bad("via none has no tool, args, or command: say how in words")
			}
		}
		if str(m["args"]) != "" && str(m["tool"]) == "" {
			bad("args needs tool")
		}
		if str(m["command"]) != "" {
			commands++
		}
		for _, k := range []string{"args", "command", "query"} {
			for _, p := range placeholderRe.FindAllStringSubmatch(str(m[k]), -1) {
				if !slices.Contains(SourcePlaceholders, p[1]) {
					bad("%s: unknown placeholder {%s}; want one of {%s}", k, p[1], strings.Join(SourcePlaceholders, "} {"))
				}
			}
		}
		if name == "fetch" {
			t := str(m["args"]) + str(m["command"])
			if t != "" && !strings.Contains(t, "{ref}") && !strings.Contains(t, "{url}") {
				bad("a fetch template needs {ref} or {url}: it reads one item")
			}
		}
	}
	if via == "cli" && commands == 0 {
		l.errf(1, CodeField, "type source: via cli needs a command in fetch, scan_in, scan_out, or state")
	}
	if c := str(fm["cadence"]); c != "" {
		if _, err := recur.Parse(c); err != nil {
			l.add(1, CodeRecur, Warn, "cadence: %v: para will not schedule a sweep of it", err)
		}
	}
	for _, r := range listStrings(fm["reached_by"]) {
		if strings.TrimSpace(r) == "" || strings.ContainsAny(r, " \t\n") {
			l.errf(1, CodeFieldValue, "field %q: %q: want an agent identity, one word", "reached_by", r)
		}
	}
	l.secrets(fm)
}

// secrets refuses any key or value of a source that looks like a credential.
func (l *linter) secrets(fm map[string]any) {
	var walk func(path string, v any)
	walk = func(path string, v any) {
		switch x := v.(type) {
		case map[string]any:
			for k, e := range x {
				p := k
				if path != "" {
					p = path + "." + k
				}
				if secretKeyRe.MatchString(k) {
					l.errf(1, CodeSecret, "field %q: the key names a credential; a vault holds no secret, the connector keeps its own", p)
					continue
				}
				walk(p, e)
			}
		case []any:
			for _, e := range x {
				walk(path, e)
			}
		case string:
			if why := LooksSecret(x, path == "cursor"); why != "" {
				l.errf(1, CodeSecret, "field %q looks like a token or key (%s); a vault holds no secret, the connector keeps its own", path, why)
			}
		}
	}
	walk("", fm)
}

func listStrings(v any) []string {
	list, _ := v.([]any)
	out := make([]string, 0, len(list))
	for _, e := range list {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// checkViaSources warns, in a vault with source documents, about each io or
// tracked_doc whose via.source names none. Sources are recommended, never
// required: a vault with no source document gets no warning.
func checkViaSources(files []*File, byName map[string]*File) []Finding {
	bySystem := map[string]int{}
	has := false
	for _, f := range files {
		if f.Type() == "source" {
			has = true
			bySystem[strings.ToLower(str(f.Frontmatter["system"]))]++
		}
	}
	if !has {
		return nil
	}
	var out []Finding
	for _, f := range files {
		if t := f.Type(); t != "io" && t != "tracked_doc" {
			continue
		}
		m, _ := f.Frontmatter["via"].(map[string]any)
		name := strings.TrimSpace(str(m["source"]))
		if name == "" {
			continue
		}
		key := strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(name, "[["), "]]"))
		if dst, ok := byName[key]; ok && dst.Type() == "source" {
			continue
		}
		if bySystem[key] == 1 {
			continue
		}
		out = append(out, Finding{Path: f.Path, Line: 1, Code: CodeSourceUnresolved, Severity: Warn,
			Msg: fmt.Sprintf("via.source %q names no source document; add one in sources/ (para source example) or name an existing one", name)})
	}
	return out
}
