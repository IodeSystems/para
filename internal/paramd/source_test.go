package paramd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sourceFM = "type: source\ncreated: 2026-11-01\nsystem: github\nstatus: active\nvia: cli\n"

func TestLintSource(t *testing.T) {
	for _, c := range []struct {
		name, fm string
		want     []string
	}{
		{"cli clean", sourceFM + "fetch: {command: \"gh issue view {url}\"}\ncadence: daily\nreached_by: [claude-work]\n", nil},
		{"cli needs a command", sourceFM + "scan_in: {how: read it}\n", []string{"field:1"}},
		{"mcp needs server", "type: source\ncreated: 2026-11-01\nsystem: gmail\nstatus: active\nvia: mcp\nfetch: {tool: get_thread, args: '{\"id\": \"{ref}\"}'}\n", []string{"field:1"}},
		{"mcp no command", "type: source\ncreated: 2026-11-01\nsystem: gmail\nstatus: active\nvia: mcp\nserver: gmail\nscan_in: {command: ls}\n", []string{"field-value:1"}},
		{"cli no tool", sourceFM + "fetch: {command: \"gh {ref}\", tool: x}\n", []string{"field-value:1"}},
		{"none no command", "type: source\ncreated: 2026-11-01\nsystem: other\nstatus: active\nvia: none\nscan_in: {command: ls}\n", []string{"field-value:1"}},
		{"server on cli", sourceFM + "server: x\nfetch: {command: \"gh {ref}\"}\n", []string{"field-value:1"}},
		{"unknown step key", sourceFM + "fetch: {command: \"gh {ref}\", color: red}\n", []string{"field-value:1"}},
		{"unknown placeholder", sourceFM + "fetch: {command: \"gh {ref} {who}\"}\n", []string{"field-value:1"}},
		{"fetch needs ref", sourceFM + "fetch: {command: \"gh issue list\"}\n", []string{"field-value:1"}},
		{"bad cadence", sourceFM + "fetch: {command: \"gh {ref}\"}\ncadence: hourly\n", []string{"recur:1"}},
		{"bad reached_by", sourceFM + "fetch: {command: \"gh {ref}\"}\nreached_by: [\"two words\"]\n", []string{"field-value:1"}},
		{"bad via", "type: source\ncreated: 2026-11-01\nsystem: github\nstatus: active\nvia: ftp\n", []string{"field-value:1"}},
		{"github token", sourceFM + "fetch: {command: \"gh {ref} ghp_abcdefghijklmnopqrstuvwxyz0123\"}\n", []string{"secret:1"}},
		{"slack token", sourceFM + "fetch: {command: \"gh {ref}\", how: \"use xoxb-1234567890-abcdefghij\"}\n", []string{"secret:1"}},
		{"bearer", sourceFM + "fetch: {command: \"curl -H 'Authorization: Bearer abcdefghijklmnop0123' {url}\"}\n", []string{"secret:1"}},
		{"key name", sourceFM + "fetch: {command: \"gh {ref}\"}\napi_key: x\n", []string{"secret:1"}},
		{"long opaque", sourceFM + "fetch: {command: \"gh {ref}\", how: \"Zm9vYmFyYmF6cXV4Q29ycmVjdEhvcnNlQmF0dGVyeVN0YXBsZQ1\"}\n", []string{"secret:1"}},
		{"password in url", sourceFM + "fetch: {command: \"curl https://jo:hunter22@host.example/{ref}\"}\n", []string{"secret:1", "url"}},
		{"cursor may be opaque", sourceFM + "fetch: {command: \"gh {ref}\"}\ncursor: Zm9vYmFyYmF6cXV4Q29ycmVjdEhvcnNlQmF0dGVyeVN0YXBsZQ1\nlast_scan: 2026-11-02T09:00:00Z\n", nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := Lint(Parse("src.md", []byte(doc(c.fm, "# Source\n"))))
			if !sameCodes(got, c.want) {
				t.Fatalf("got\n  %s\nwant %v", joinFindings(got), c.want)
			}
		})
	}
}

// Every shipped example source lints clean, and there is one per system but
// other.
func TestSourceExamplesLintClean(t *testing.T) {
	dir := filepath.Join("..", "..", "docs", "sources")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		f := Parse(e.Name(), data)
		if f.Type() != "source" {
			t.Errorf("%s: type %s", e.Name(), f.Type())
		}
		// An example is written as class and vendor; an older system value
		// is the vendor of one.
		seen[str(f.Frontmatter["system"])] = true
		seen[str(f.Frontmatter["vendor"])] = true
		if got := Lint(f); len(got) != 0 {
			t.Errorf("%s:\n  %s", e.Name(), joinFindings(got))
		}
	}
	for _, s := range sourceSystems {
		if !seen[s] && s != "other" {
			t.Errorf("no example for system %s", s)
		}
	}
}

// A via.source that names no source is a warning, and only in a vault that
// has a source document.
func TestCheckViaSources(t *testing.T) {
	mk := func(path, src string) *File { return Parse(path, []byte(src)) }
	io := func(name, via string) *File {
		return mk("io/"+name+".md", doc("type: io\ncreated: 2026-11-01\ndirection: in\nmedium: email\nstate: unread\nvia: {source: "+via+", ref: x}\n", "# A\n"))
	}
	src := mk("sources/work-mail.md", doc("type: source\ncreated: 2026-11-01\nsystem: gmail\nstatus: active\nvia: none\n", "# Mail\n"))
	if f := CheckVault([]*File{io("io-a", "nowhere")}); len(f) != 0 {
		t.Fatalf("no sources: %v", f)
	}
	f := CheckVault([]*File{src, io("io-a", "work-mail"), io("io-b", "gmail"), io("io-c", "\"[[work-mail]]\""), io("io-d", "nowhere")})
	if len(f) != 1 || f[0].Code != CodeSourceUnresolved || f[0].Severity != Warn || !strings.Contains(f[0].Path, "io-d") {
		t.Fatalf("findings = %v", f)
	}
}
