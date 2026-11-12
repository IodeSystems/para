package paramd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const hash = "sha256:abababababababababababababababababababababababababababababababab"

// doc builds a file from frontmatter lines and a body.
func doc(fm, body string) string {
	return "---\n" + strings.TrimLeft(fm, "\n") + "---\n\n" + strings.TrimLeft(body, "\n")
}

const policyFM = `
type: policy
created: 2026-11-01
verified: false
source:
  url: https://wiki.acme.example/policies/logging
  retrieved: 2026-11-01T09:00:00Z
  hash: ` + hash + `
  fidelity: exact
`

const personFM = `
type: person
created: 2026-11-01
org: acme.example
`

const rfxFM = `
type: rfx
created: 2026-11-01
direction: out
status: open
route: domain-a
self_serve_check: searched
`

const interactionFM = `
type: interaction
created: 2026-11-01
date: 2026-11-01
with:
  - "[[jo]]"
medium: slack
direction: in
visibility: private
url: https://acme.example/m/1
`

const requestFM = `
type: request
created: 2026-11-01
status: claimed
runner: inst-a
queued_at: 2026-11-01T09:00:00.123456789Z
claimed_by: agent
lease_until: 2026-11-01T09:30:00Z
links:
  - "[[project-a]]"
`

func TestParse(t *testing.T) {
	src := doc(policyFM, "# Logging\n\nSee [[policy-logging.source]] and [[acme-portal#Goals|goals]] and [[policy-logging#^R1]].\n\n```\n[[in-code]] ^R9\n```\n\nUse `[[inline]]` here.\n\nR1 policy-logging: Log failures. ^R1\n\n## Page 2\n")
	f := Parse("vault/policy-logging.md", []byte(src))
	if f.Name != "policy-logging" || f.Type() != "policy" || f.Title != "Logging" {
		t.Fatalf("name/type/title = %q %q %q", f.Name, f.Type(), f.Title)
	}
	var got []string
	for _, l := range f.Links {
		got = append(got, l.Target+"|"+l.Heading+"|"+l.Block+"|"+l.Alias)
	}
	want := []string{"policy-logging.source|||", "acme-portal|Goals||goals", "policy-logging||R1|"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("links = %v, want %v", got, want)
	}
	if len(f.Blocks) != 1 || f.Blocks[0].ID != "R1" {
		t.Errorf("blocks = %+v", f.Blocks)
	}
	if len(f.Rules) != 1 || f.Rules[0].Policy != "policy-logging" || f.Rules[0].Text != "Log failures." {
		t.Errorf("rules = %+v", f.Rules)
	}
	if len(f.PageMarks) != 1 || f.PageMarks[0].N != 2 {
		t.Errorf("page marks = %+v", f.PageMarks)
	}
	if f.Frontmatter["verified"] != false {
		t.Errorf("frontmatter = %v", f.Frontmatter)
	}
}

func TestLint(t *testing.T) {
	cases := []struct {
		name string
		path string
		src  string
		want []string // "code" or "code:line"; empty means no findings
	}{
		{"valid policy", "policy-logging.md", doc(policyFM, "# Logging\n\nR1 policy-logging: Log failures. ^R1\n"), nil},
		{"crlf", "convention-a.md", strings.ReplaceAll(doc("type: convention\ncreated: 2026-11-01\nstatus: draft\napplies_in: [acme-prod]\n", "# A\n"), "\n", "\r\n"), []string{"line-ending"}},
		{"no frontmatter", "a.md", "# A\n", []string{"frontmatter:1", "title"}},
		{"unterminated frontmatter", "a.md", "---\ntype: person\n# A\n", []string{"frontmatter:1"}},
		{"bad yaml", "a.md", "---\ntype: [\n---\n\n# A\n", []string{"frontmatter:1"}},
		{"missing type", "a.md", doc("created: 2026-11-01\n", "# A\n"), []string{"type:1"}},
		{"unknown type", "a.md", doc("type: widget\n", "# A\n"), []string{"type:1"}},
		{"io in", "io-a.md", doc("type: io\ncreated: 2026-11-01\ndirection: in\nmedium: email\nstate: unread\nfrom: \"[[jo]]\"\nto: [me@acme.example]\nat: 2026-11-01T09:00:00Z\nvia: {source: gmail, ref: x1}\n", "# A\n"), nil},
		{"io in takes no out state", "io-a.md", doc("type: io\ncreated: 2026-11-01\ndirection: in\nmedium: email\nstate: draft\n", "# A\n"), []string{"field-value:1"}},
		{"io in takes no route", "io-a.md", doc("type: io\ncreated: 2026-11-01\ndirection: in\nmedium: email\nstate: read\nroute: logging\n", "# A\n"), []string{"field-value:1"}},
		{"io in may expect a response", "io-a.md", doc("type: io\ncreated: 2026-11-01\ndirection: in\nmedium: email\nstate: read\nexpects: response\neffort: xs\n", "# A\n"), nil},
		{"effort is xs, s, m, or l", "io-a.md", doc("type: io\ncreated: 2026-11-01\ndirection: in\nmedium: email\nstate: read\neffort: huge\n", "# A\n"), []string{"field-value:1"}},
		{"io out needs expects", "io-a.md", doc("type: io\ncreated: 2026-11-01\ndirection: out\nmedium: email\nstate: draft\n", "# A\n"), []string{"field:1"}},
		{"io response needs route", "io-a.md", doc("type: io\ncreated: 2026-11-01\ndirection: out\nmedium: email\nstate: draft\nexpects: response\n", "# A\n"), []string{"field:1"}},
		{"io sent needs became", "io-a.md", doc("type: io\ncreated: 2026-11-01\ndirection: out\nmedium: email\nstate: sent\nexpects: none\n", "# A\n"), []string{"field:1"}},
		{"io bad via", "io-a.md", doc("type: io\ncreated: 2026-11-01\ndirection: in\nmedium: email\nstate: unread\nvia: {source: gmail, id: x}\n", "# A\n"), []string{"field-value:1", "field-value:1"}},
		{"io bad at", "io-a.md", doc("type: io\ncreated: 2026-11-01\ndirection: in\nmedium: email\nstate: unread\nat: yesterday\n", "# A\n"), []string{"field-value:1"}},
		{"io proposal all fields", "io-a.md", doc("type: io\ncreated: 2026-11-01\ndirection: out\nmedium: email\nstate: draft\nexpects: none\nreviewed: 2026-11-02T09:00:00Z\nwhy: Unblocks release\nimpact: Ships v2\nassessments:\n  - {name: confidence, percent: 80, basis: tests pass}\n  - {name: custom, percent: 0}\ngroup: batch-1\nvariant_of: io-b\nrevision: 2\nrevisions:\n  - {n: 1, hash: abc, by: agent, at: 2026-11-01T09:00:00Z}\nattempts:\n  - {at: 2026-11-02T10:00:00Z, actor: me, tool: gh, medium: github, result: failed, error: boom}\n  - {at: 2026-11-02T11:00:00Z, result: sent, url: \"https://x.example/1\"}\nunknown_key: kept\n", "# A\n"), nil},
		{"io assessment percent over 100", "io-a.md", doc("type: io\ncreated: 2026-11-01\ndirection: out\nmedium: email\nstate: draft\nexpects: none\nassessments:\n  - {name: risk, percent: 101}\n", "# A\n"), []string{"field-value:1"}},
		{"io assessment percent negative", "io-a.md", doc("type: io\ncreated: 2026-11-01\ndirection: out\nmedium: email\nstate: draft\nexpects: none\nassessments:\n  - {name: risk, percent: -1}\n", "# A\n"), []string{"field-value:1"}},
		{"io attempt bad result", "io-a.md", doc("type: io\ncreated: 2026-11-01\ndirection: out\nmedium: email\nstate: draft\nexpects: none\nattempts:\n  - {at: 2026-11-02T10:00:00Z, result: done}\n", "# A\n"), []string{"field-value:1"}},
		{"io group empty", "io-a.md", doc("type: io\ncreated: 2026-11-01\ndirection: out\nmedium: email\nstate: draft\nexpects: none\ngroup: \"\"\n", "# A\n"), []string{"field-value:1"}},
		{"tracked doc", "runbook.md", doc("type: tracked_doc\ncreated: 2026-11-01\nurl: https://wiki.acme.example/a\nsystem: confluence\nstatus: watching\nlatest: 2\nlast_seen: 1\nvia: {source: confluence, ref: \"1\"}\ncomments:\n  - {id: c1, by: Ada, at: 2026-11-01T09:00:00Z, text: Why, quote: x, version: 1, resolution: became, became: \"[[rfx-a]]\"}\n", "# A\n"), nil},
		{"tracked doc seen after latest", "runbook.md", doc("type: tracked_doc\ncreated: 2026-11-01\nurl: https://wiki.acme.example/a\nsystem: confluence\nstatus: watching\nlatest: 2\nlast_seen: 3\n", "# A\n"), []string{"field-value:1"}},
		{"tracked doc bad comment", "runbook.md", doc("type: tracked_doc\ncreated: 2026-11-01\nurl: https://wiki.acme.example/a\nsystem: confluence\nstatus: watching\nlatest: 2\ncomments:\n  - {id: x1, by: Ada, at: soon, text: t, version: 9, resolution: done, color: red}\n", "# A\n"),
			[]string{"field-value:1", "field-value:1", "field-value:1", "field-value:1", "field-value:1"}},
		{"tracked doc became needs link", "runbook.md", doc("type: tracked_doc\ncreated: 2026-11-01\nurl: https://wiki.acme.example/a\nsystem: confluence\nstatus: watching\nlatest: 2\ncomments:\n  - {id: c1, by: Ada, at: 2026-11-01T09:00:00Z, text: t, version: 1, resolution: became}\n", "# A\n"), []string{"field-value:1"}},
		{"version transcript", "runbook-v2.source.md", doc("type: transcript\ncreated: 2026-11-01\nof: \"[[runbook]]\"\nversion: 2\nsource: {url: https://wiki.acme.example/a, retrieved: 2026-11-01T09:00:00Z, hash: \"sha256:abababababababababababababababababababababababababababababababab\", fidelity: exact}\n", "# A\n\nSource: https://wiki.acme.example/a\n"), nil},
		{"version transcript wrong name", "runbook-v3.source.md", doc("type: transcript\ncreated: 2026-11-01\nof: \"[[runbook]]\"\nversion: 2\nsource: {url: https://wiki.acme.example/a, retrieved: 2026-11-01T09:00:00Z, hash: \"sha256:abababababababababababababababababababababababababababababababab\", fidelity: exact}\n", "# A\n\nSource: https://wiki.acme.example/a\n"), []string{"field-value:1"}},
		{"person without org", "jo.md", doc("type: person\ncreated: 2026-11-01\n", "# Jo\n"), nil},
		{"team missing org", "jo.md", doc("type: team\ncreated: 2026-11-01\n", "# Jo\n"), []string{"field:1"}},
		{"bad date", "jo.md", doc("type: person\ncreated: yesterday\norg: acme.example\n", "# Jo\n"), []string{"field-value:1"}},
		{"timestamp not date", "jo.md", doc("type: person\ncreated: 2026-11-01T10:00:00Z\norg: acme.example\n", "# Jo\n"), []string{"field-value:1"}},
		{"bad enum", "p.md", doc("type: goal\ncreated: 2026-11-01\nstatus: weird\nowner: Jo\n", "# P\n"), []string{"field-value:1"}},
		{"task with neither", "t.md", doc("type: task\ncreated: 2026-11-01\nstatus: todo\n", "# T\n"), []string{"field:1"}},
		{"task with area only", "t.md", doc("type: task\ncreated: 2026-11-01\nstatus: todo\narea: \"[[health]]\"\n", "# T\n"), nil},
		{"unquoted link", "t.md", doc("type: task\ncreated: 2026-11-01\nstatus: todo\nproject: [[acme-portal]]\n", "# T\n"), []string{"field-value:1"}},
		{"exception conflict needs with", "e.md", doc("type: policy_exception\ncreated: 2026-11-01\npolicy: \"[[policy-a]]\"\nkind: conflict_with\nstatus: open\nby: Jo\n", "# E\n"), []string{"field:1"}},
		{"exception rule must be block", "e.md", doc("type: policy_exception\ncreated: 2026-11-01\npolicy: \"[[policy-a]]\"\nrule: \"[[policy-a]]\"\nkind: bug\nstatus: open\nby: Jo\n", "# E\n"), []string{"field-value:1"}},
		{"source missing hash", "policy-a.md", doc(strings.Replace(policyFM, "  hash: "+hash+"\n", "", 1), "# A\n\nR1 policy-a: x. ^R1\n"), []string{"source:1"}},
		{"source none needs no hash", "policy-a.md", doc(strings.Replace(strings.Replace(policyFM, "  hash: "+hash+"\n", "", 1), "fidelity: exact", "fidelity: none", 1), "# A\n\nR1 policy-a: x. ^R1\n"), nil},
		{"source bad url", "policy-a.md", doc(strings.Replace(policyFM, "https://wiki", "ftp://wiki", 1), "# A\n\nR1 policy-a: x. ^R1\n"), []string{"source:1"}},
		{"filename uppercase", "Jo.md", doc("type: person\ncreated: 2026-11-01\norg: acme.example\n", "# Jo\n"), []string{"filename"}},
		{"source suffix on non-transcript", "jo.source.md", doc("type: person\ncreated: 2026-11-01\norg: acme.example\n", "# Jo\n"), []string{"filename"}},
		{"no title", "jo.md", doc("type: person\ncreated: 2026-11-01\norg: acme.example\n", "Text first.\n"), []string{"title:6"}},
		{"two titles", "jo.md", doc("type: person\ncreated: 2026-11-01\norg: acme.example\n", "# Jo\n\n# Again\n"), []string{"title:9"}},
		{"h1 in code is fine", "jo.md", doc("type: person\ncreated: 2026-11-01\norg: acme.example\n", "# Jo\n\n```\n# comment\n```\n"), nil},
		{"raw html", "jo.md", doc("type: person\ncreated: 2026-11-01\norg: acme.example\n", "# Jo\n\nHello <b>x</b>\n"), []string{"raw-html:9"}},
		{"html comment", "jo.md", doc("type: person\ncreated: 2026-11-01\norg: acme.example\n", "# Jo\n\n<!-- x -->\n"), []string{"raw-html:9"}},
		{"html in code span and autolink ok", "jo.md", doc("type: person\ncreated: 2026-11-01\norg: acme.example\n", "# Jo\n\nUse `<b>` or <https://acme.example/a>.\n"), nil},
		{"table ok", "jo.md", doc("type: person\ncreated: 2026-11-01\norg: acme.example\n", "# Jo\n\n| a | b |\n|---|:-:|\n| 1 | 2 |\n"), nil},
		{"table ragged", "jo.md", doc("type: person\ncreated: 2026-11-01\norg: acme.example\n", "# Jo\n\n| a | b |\n|---|---|\n| 1 |\n"), []string{"table:11"}},
		{"table second separator", "jo.md", doc("type: person\ncreated: 2026-11-01\norg: acme.example\n", "# Jo\n\n| a | b |\n|---|---|\n|---|---|\n"), []string{"table:11"}},
		{"table separator width", "jo.md", doc("type: person\ncreated: 2026-11-01\norg: acme.example\n", "# Jo\n\n| a | b |\n|---|\n"), []string{"table:10"}},
		{"table escaped pipe", "jo.md", doc("type: person\ncreated: 2026-11-01\norg: acme.example\n", "# Jo\n\n| a | b |\n|---|---|\n| x \\| y | z |\n"), nil},
		{"image ok", "jo.md", doc("type: person\ncreated: 2026-11-01\norg: acme.example\n", "# Jo\n\n![Org chart with three teams](img/org.png)\n"), nil},
		{"image no alt", "jo.md", doc("type: person\ncreated: 2026-11-01\norg: acme.example\n", "# Jo\n\n![](img/org.png)\n"), []string{"image:9"}},
		{"image alt is filename", "jo.md", doc("type: person\ncreated: 2026-11-01\norg: acme.example\n", "# Jo\n\n![org.png](img/org.png)\n"), []string{"image:9"}},
		{"image embed", "jo.md", doc("type: person\ncreated: 2026-11-01\norg: acme.example\n", "# Jo\n\n![[org.png]]\n"), []string{"image:9", "wikilink:9"}},
		{"question scoped to a project", "question-a.md", doc("type: question\ncreated: 2026-11-01\nstatus: open\nscope: \"[[acme-portal]]\"\n", "# Q\n"), nil},
		{"question scoped to root", "question-a.md", doc("type: question\ncreated: 2026-11-01\nstatus: open\nscope: root\n", "# Q\n"), nil},
		{"question with no scope", "question-a.md", doc("type: question\ncreated: 2026-11-01\nstatus: open\n", "# Q\n"), []string{"field:1"}},
		{"question with an unquoted scope", "question-a.md", doc("type: question\ncreated: 2026-11-01\nstatus: open\nscope: [[acme-portal]]\n", "# Q\n"), []string{"field-value:1"}},
		{"question with two scopes", "question-a.md", doc("type: question\ncreated: 2026-11-01\nstatus: open\nscope:\n  - \"[[a]]\"\n  - \"[[b]]\"\n", "# Q\n"), []string{"field-value:1"}},
		{"question with a scope that is not root", "question-a.md", doc("type: question\ncreated: 2026-11-01\nstatus: open\nscope: everywhere\n", "# Q\n"), []string{"field-value:1"}},
		{"question recommended is an option", "question-a.md", doc("type: question\ncreated: 2026-11-01\nstatus: open\nscope: root\noptions: [a, b]\nrecommended: b\nkind: decision\n", "# Q\n"), nil},
		{"question recommended is not an option", "question-a.md", doc("type: question\ncreated: 2026-11-01\nstatus: open\nscope: root\noptions: [a, b]\nrecommended: c\n", "# Q\n"), []string{"field-value:1"}},
		{"meeting requested without a time", "meeting-a.md", doc("type: meeting\ncreated: 2026-11-01\nstatus: requested\nwith:\n  - \"[[ada]]\"\n", "# M\n"), nil},
		{"meeting scheduled with zone", "meeting-a.md", doc("type: meeting\ncreated: 2026-11-01\nstatus: scheduled\nstart: 2026-11-04T10:00:00-05:00\nend: 2026-11-04T10:30:00-05:00\nwith:\n  - \"[[ada]]\"\n", "# M\n"), nil},
		{"meeting scheduled with no time", "meeting-a.md", doc("type: meeting\ncreated: 2026-11-01\nstatus: scheduled\nwith:\n  - \"[[ada]]\"\n", "# M\n"), []string{"field:1", "field:1"}},
		{"meeting start without end", "meeting-a.md", doc("type: meeting\ncreated: 2026-11-01\nstatus: requested\nstart: 2026-11-04T10:00:00Z\nwith:\n  - \"[[ada]]\"\n", "# M\n"), []string{"field-value:1"}},
		{"meeting end before start", "meeting-a.md", doc("type: meeting\ncreated: 2026-11-01\nstatus: held\nstart: 2026-11-04T10:00:00Z\nend: 2026-11-04T09:00:00Z\nwith:\n  - \"[[ada]]\"\n", "# M\n"), []string{"field-value:1"}},
		{"meeting time without zone", "meeting-a.md", doc("type: meeting\ncreated: 2026-11-01\nstatus: scheduled\nstart: \"2026-11-04T10:00:00\"\nend: \"2026-11-04T11:00:00\"\nwith:\n  - \"[[ada]]\"\n", "# M\n"), []string{"field-value:1", "field-value:1"}},
		{"meeting with nobody", "meeting-a.md", doc("type: meeting\ncreated: 2026-11-01\nstatus: requested\n", "# M\n"), []string{"field:1"}},
		{"meeting bad status", "meeting-a.md", doc("type: meeting\ncreated: 2026-11-01\nstatus: soon\nwith:\n  - \"[[ada]]\"\n", "# M\n"), []string{"field-value:1"}},
		{"meeting target on a meeting medium", "rfx-a.md", doc("type: rfx\ncreated: 2026-11-01\ndirection: out\nstatus: open\nroute: r\nself_serve_check: x\ntargets:\n  - to: \"[[ada]]\"\n    medium: meeting\n    state: scheduled\n", "# R\n"), nil},
		{"requested state on email", "rfx-a.md", doc("type: rfx\ncreated: 2026-11-01\ndirection: out\nstatus: open\nroute: r\nself_serve_check: x\ntargets:\n  - to: \"[[ada]]\"\n    medium: email\n    state: requested\n", "# R\n"), []string{"field-value:1"}},
		{"question bad kind", "question-a.md", doc("type: question\ncreated: 2026-11-01\nstatus: open\nscope: root\nkind: wish\n", "# Q\n"), []string{"field-value:1"}},
		{"signed url", "jo.md", doc("type: person\ncreated: 2026-11-01\norg: acme.example\n", "# Jo\n\n[f](https://files.acme.example/a.pdf?X-Amz-Signature=abc&x=1)\n"), []string{"url:9"}},
		{"url with userinfo", "jo.md", doc("type: person\ncreated: 2026-11-01\norg: acme.example\n", "# Jo\n\nhttps://bob:pw@acme.example/a\n"), []string{"url:9"}},
		{"plain url ok", "jo.md", doc("type: person\ncreated: 2026-11-01\norg: acme.example\n", "# Jo\n\nhttps://acme.example/a?page=2.\n"), nil},
		{"wikilink with folder", "jo.md", doc("type: person\ncreated: 2026-11-01\norg: acme.example\n", "# Jo\n\n[[people/jo]] [[jo.md]] [[]]\n"), []string{"wikilink:9", "wikilink:9", "wikilink:9"}},
		{"duplicate block id", "jo.md", doc("type: person\ncreated: 2026-11-01\norg: acme.example\n", "# Jo\n\nA ^x1\n\nB ^x1\n"), []string{"block-id:11"}},
		{"rule wrong policy id", "policy-a.md", doc(policyFM, "# A\n\nR1 policy-b: x. ^R1\n"), []string{"rule:14"}},
		{"rule id mismatch", "policy-a.md", doc(policyFM, "# A\n\nR1 policy-a: x. ^R2\n"), []string{"rule:14", "rule"}},
		{"rule without block id", "policy-a.md", doc(policyFM, "# A\n\nR1 policy-a: x.\n\nR2 policy-a: y. ^R2\n"), []string{"rule:14"}},
		{"rule without policy id", "policy-a.md", doc(policyFM, "# A\n\nR1: x. ^R1\n"), []string{"rule:14", "rule"}},
		{"rule too long", "policy-a.md", doc(policyFM, "# A\n\nR1 policy-a: "+strings.Repeat("word ", 110)+" ^R1\n"), []string{"rule:14"}},
		{"rule in list item", "policy-a.md", doc(policyFM, "# A\n\n- R1 policy-a: x. ^R1\n"), nil},
		{"policy without rules warns", "policy-a.md", doc(policyFM, "# A\n\nSummary only.\n"), []string{"rule"}},
		{"rule outside policy", "jo.md", doc("type: person\ncreated: 2026-11-01\norg: acme.example\n", "# Jo\n\nR1 jo: x. ^R1\n"), []string{"rule:9"}},
		{"transcript ok", "policy-a.source.md", doc(strings.Replace(policyFM, "type: policy\n", "type: transcript\nof: \"[[policy-a]]\"\n", 1), "# A (transcript)\n\nSource: https://wiki.acme.example/policies/logging\n\n## Page 1\n\nx\n\n## Page 3\n\ny\n"), nil},
		{"transcript wrong name", "policy-a.md", doc(strings.Replace(policyFM, "type: policy\n", "type: transcript\nof: \"[[policy-a]]\"\n", 1), "# A\n\nSource: https://wiki.acme.example/policies/logging\n"), []string{"filename"}},
		{"transcript of mismatch", "policy-a.source.md", doc(strings.Replace(policyFM, "type: policy\n", "type: transcript\nof: \"[[policy-b]]\"\n", 1), "# A\n\nSource: https://wiki.acme.example/policies/logging\n"), []string{"field-value:1"}},
		{"transcript no source line", "policy-a.source.md", doc(strings.Replace(policyFM, "type: policy\n", "type: transcript\nof: \"[[policy-a]]\"\n", 1), "# A\n\nText.\n"), []string{"source-line:13"}},
		{"transcript source mismatch", "policy-a.source.md", doc(strings.Replace(policyFM, "type: policy\n", "type: transcript\nof: \"[[policy-a]]\"\n", 1), "# A\n\nSource: https://wiki.acme.example/other\n"), []string{"source-line:15"}},
		{"transcript page order", "policy-a.source.md", doc(strings.Replace(policyFM, "type: policy\n", "type: transcript\nof: \"[[policy-a]]\"\n", 1), "# A\n\nSource: https://wiki.acme.example/policies/logging\n\n## Page 2\n\n## Page 2\n"), []string{"page-marker:19"}},
		{"transcript page form", "policy-a.source.md", doc(strings.Replace(policyFM, "type: policy\n", "type: transcript\nof: \"[[policy-a]]\"\n", 1), "# A\n\nSource: https://wiki.acme.example/policies/logging\n\n### Page  1\n"), []string{"page-marker:17"}},
		{"journal ok", "2026-11-01.md", doc("type: journal\ncreated: 2026-11-01\ndate: 2026-11-01\nscope: day\n", "# Day\n\n%% para:begin tasks %%\nx\n%% para:end tasks %%\n"), nil},
		{"journal summary-agent ok", "2026-11-01.md", doc("type: journal\ncreated: 2026-11-01\ndate: 2026-11-01\nscope: day\n", "# Day\n\nMine.\n\n%% para:begin summary-agent %%\nMoved [[x]].\n%% para:end summary-agent %%\n"), nil},
		{"journal summary-agent unclosed", "2026-11-01.md", doc("type: journal\ncreated: 2026-11-01\ndate: 2026-11-01\nscope: day\n", "# Day\n\n%% para:begin summary-agent %%\nMoved.\n"), []string{"journal-marker:10"}},
		{"journal unclosed", "2026-11-01.md", doc("type: journal\ncreated: 2026-11-01\ndate: 2026-11-01\nscope: day\n", "# Day\n\n%% para:begin tasks %%\nx\n"), []string{"journal-marker:10"}},
		{"journal mismatched end", "2026-11-01.md", doc("type: journal\ncreated: 2026-11-01\ndate: 2026-11-01\nscope: day\n", "# Day\n\n%% para:begin a %%\n%% para:end b %%\n"), []string{"journal-marker:11"}},
		{"marker outside journal", "jo.md", doc("type: person\ncreated: 2026-11-01\norg: acme.example\n", "# Jo\n\n%% para:begin a %%\n%% para:end a %%\n"), []string{"journal-marker:9", "journal-marker:10"}},
		{"person directory ok", "jo.md", doc(personFM, "# Jo\n\n## Notes\n\n- 2026-11-01: Prefers Slack.\n"), nil},
		{"contact bad method", "jo.md", doc(personFM+"contacts:\n  - method: fax\n    value: x\n", "# Jo\n"), []string{"field-value:1"}},
		{"contact missing value", "jo.md", doc(personFM+"contacts:\n  - method: email\n", "# Jo\n"), []string{"field-value:1"}},
		{"contact preferred not bool", "jo.md", doc(personFM+"contacts:\n  - method: email\n    value: a@acme.example\n    preferred: yes please\n", "# Jo\n"), []string{"field-value:1"}},
		{"contacts not a list", "jo.md", doc(personFM+"contacts: email\n", "# Jo\n"), []string{"field-value:1"}},
		{"groups unquoted", "jo.md", doc(personFM+"groups:\n  - [[group-a]]\n", "# Jo\n"), []string{"field-value:1"}},
		{"group ok", "group-a.md", doc("type: group\ncreated: 2026-11-01\norg: Any text\nkind: channel\n", "# A\n"), nil},
		{"group bad kind", "group-a.md", doc("type: group\ncreated: 2026-11-01\norg: o\nkind: club\n", "# A\n"), []string{"field-value:1"}},
		{"group missing kind", "group-a.md", doc("type: group\ncreated: 2026-11-01\norg: o\n", "# A\n"), []string{"field:1"}},
		{"rfx targets ok", "rfx-a.md", doc(rfxFM+"targets:\n  - to: \"[[jo]]\"\n    medium: slack\n    state: sent\n    sent: 2026-11-01\n", "# A\n"), nil},
		{"rfx target bad state", "rfx-a.md", doc(rfxFM+"targets:\n  - to: \"[[jo]]\"\n    medium: slack\n    state: waiting\n", "# A\n"), []string{"field-value:1"}},
		{"rfx target missing medium", "rfx-a.md", doc(rfxFM+"targets:\n  - to: \"[[jo]]\"\n    state: sent\n", "# A\n"), []string{"field-value:1"}},
		{"rfx target unquoted to", "rfx-a.md", doc(rfxFM+"targets:\n  - to: [[jo]]\n    medium: slack\n    state: sent\n", "# A\n"), []string{"field-value:1"}},
		{"rfx target bad sent date", "rfx-a.md", doc(rfxFM+"targets:\n  - to: \"[[jo]]\"\n    medium: slack\n    state: sent\n    sent: today\n", "# A\n"), []string{"field-value:1"}},
		{"request ok", "request-a.md", doc(requestFM, "# A\n"), nil},
		{"request bad status", "request-a.md", doc(strings.Replace(requestFM, "status: claimed", "status: running", 1), "# A\n"), []string{"field-value:1"}},
		{"request no runner", "request-a.md", doc(strings.Replace(requestFM, "runner: inst-a\n", "", 1), "# A\n"), []string{"field:1"}},
		{"request bad time", "request-a.md", doc(strings.Replace(requestFM, "lease_until: 2026-11-01T09:30:00Z", "lease_until: soon", 1), "# A\n"), []string{"field-value:1"}},
		{"interaction ok", "interaction-a.md", doc(interactionFM, "# A\n\nShort summary.\n"), nil},
		{"interaction no with", "interaction-a.md", doc(strings.Replace(interactionFM, "with:\n  - \"[[jo]]\"\n", "with: []\n", 1), "# A\n"), []string{"field-value:1"}},
		{"interaction missing with", "interaction-a.md", doc(strings.Replace(interactionFM, "with:\n  - \"[[jo]]\"\n", "", 1), "# A\n"), []string{"field:1"}},
		{"interaction bad direction", "interaction-a.md", doc(strings.Replace(interactionFM, "direction: in", "direction: sideways", 1), "# A\n"), []string{"field-value:1"}},
		{"interaction bad visibility", "interaction-a.md", doc(strings.Replace(interactionFM, "visibility: private", "visibility: secret", 1), "# A\n"), []string{"field-value:1"}},
		{"interaction summary at limit", "interaction-a.md", doc(interactionFM, "# A\n\n"+strings.Repeat("x", MaxSummaryLen)+"\n"), nil},
		{"interaction summary over limit", "interaction-a.md", doc(interactionFM, "# A\n\n"+strings.Repeat("x", MaxSummaryLen+1)+"\n"), []string{"summary-long:13"}},
		{"long body on other types is fine", "jo.md", doc(personFM, "# Jo\n\n"+strings.Repeat("x", 2*MaxSummaryLen)+"\n"), nil},
		{"area kinds ok", "area-a.md", doc("type: area\ncreated: 2026-11-01\nstatus: active\nkind: project\ncadence: quarterly\nlast_reviewed: 2026-10-01\nconcepts:\n  - \"[[area-c]]\"\n", "# A\n"), nil},
		{"area bad kind", "area-a.md", doc("type: area\ncreated: 2026-11-01\nstatus: active\nkind: hobby\n", "# A\n"), []string{"field-value:1"}},
		{"area bad cadence warns", "area-a.md", doc("type: area\ncreated: 2026-11-01\nstatus: active\ncadence: sometimes\n", "# A\n"), []string{"recur:1"}},
		{"concept area with concepts", "area-c.md", doc("type: area\ncreated: 2026-11-01\nstatus: active\nkind: concept\nconcepts:\n  - \"[[area-d]]\"\n", "# C\n"), []string{"field-value:1"}},
		{"goal risks ok", "p.md", doc("type: goal\ncreated: 2026-11-01\nstatus: active\nowner: Jo\nrisks:\n  - text: Vendor slips\n    likelihood: medium\n    impact: high\n    owner: \"[[jo]]\"\n    mitigation: Second vendor\n    source: chat\n", "# P\n"), nil},
		{"goal risk bad impact", "p.md", doc("type: goal\ncreated: 2026-11-01\nstatus: active\nowner: Jo\nrisks:\n  - text: Vendor slips\n    impact: huge\n", "# P\n"), []string{"field-value:1"}},
		{"goal risk no text", "p.md", doc("type: goal\ncreated: 2026-11-01\nstatus: active\nowner: Jo\nrisks:\n  - impact: low\n", "# P\n"), []string{"field-value:1"}},
		{"task assumptions ok", "t.md", doc("type: task\ncreated: 2026-11-01\nstatus: todo\nproject: \"[[p]]\"\nassumptions:\n  - text: The API is stable\n    source: chat\n    checked: 2026-11-02\n    by: jo\n    status: holds\n", "# T\n"), nil},
		{"assumption bad status", "t.md", doc("type: task\ncreated: 2026-11-01\nstatus: todo\nproject: \"[[p]]\"\nassumptions:\n  - text: The API is stable\n    status: maybe\n", "# T\n"), []string{"field-value:1"}},
		{"imported_from ok", "jo.md", doc("type: person\ncreated: 2026-11-01\nimported_from:\n  path: /old/people/jo.md\n  hash: "+hash+"\n", "# Jo\n"), nil},
		{"imported_from bad hash", "n.md", doc("type: note\ncreated: 2026-11-01\nimported_from:\n  path: /old/n.md\n  hash: abc\n", "# N\n"), []string{"field-value:1"}},
		{"imported_from no path", "n.md", doc("type: note\ncreated: 2026-11-01\nimported_from:\n  hash: "+hash+"\n", "# N\n"), []string{"field-value:1"}},
		{"imported_from not a map", "n.md", doc("type: note\ncreated: 2026-11-01\nimported_from: /old/n.md\n", "# N\n"), []string{"field-value:1"}},
		{"calendar_uid on a meeting", "t.md", doc("type: task\ncreated: 2026-11-01\nstatus: todo\nkind: meeting\ncalendar_uid: sync@acme.example\n", "# T\n"), nil},
		{"calendar_uid on a plain task", "t.md", doc("type: task\ncreated: 2026-11-01\nstatus: todo\nproject: \"[[p]]\"\ncalendar_uid: sync@acme.example\n", "# T\n"), []string{"field-value:1"}},
		{"calendar_uids on a meeting", "t.md", doc("type: task\ncreated: 2026-11-01\nstatus: todo\nkind: meeting\ncalendar_uids:\n  - uid: sync@acme.example\n    role: primary\n  - uid: room@acme.example\n", "# T\n"), nil},
		{"calendar_uids repeated uid", "t.md", doc("type: task\ncreated: 2026-11-01\nstatus: todo\nkind: meeting\ncalendar_uids:\n  - uid: a@x\n  - uid: a@x\n", "# T\n"), []string{"field-value"}},
		{"calendar_uids unknown key", "t.md", doc("type: task\ncreated: 2026-11-01\nstatus: todo\nkind: meeting\ncalendar_uids:\n  - uid: a@x\n    room: 4\n", "# T\n"), []string{"field-value"}},
		{"calendar_uids on a plain task", "t.md", doc("type: task\ncreated: 2026-11-01\nstatus: todo\nproject: \"[[p]]\"\ncalendar_uids:\n  - uid: a@x\n", "# T\n"), []string{"field-value:1"}},
		{"assumption bad checked", "p.md", doc("type: goal\ncreated: 2026-11-01\nstatus: active\nowner: Jo\nassumptions:\n  - text: Budget holds\n    checked: last week\n", "# P\n"), []string{"field-value:1"}},
		{"interaction signed url", "interaction-a.md", doc(strings.Replace(interactionFM, "url: https://acme.example/m/1", "url: https://acme.example/m/1?token=abc", 1), "# A\n"), []string{"url:10"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Lint(Parse(c.path, []byte(c.src)))
			if !sameCodes(got, c.want) {
				t.Errorf("findings:\n  %s\nwant %v", joinFindings(got), c.want)
			}
		})
	}
}

// sameCodes compares findings to "code" or "code:line" wants, ignoring order.
func sameCodes(got []Finding, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	used := make([]bool, len(got))
outer:
	for _, w := range want {
		code, line, hasLine := strings.Cut(w, ":")
		for i, g := range got {
			if used[i] || g.Code != code {
				continue
			}
			if hasLine && itoa(g.Line) != line {
				continue
			}
			used[i] = true
			continue outer
		}
		return false
	}
	return true
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

func joinFindings(fs []Finding) string {
	var s []string
	for _, f := range fs {
		s = append(s, f.String())
	}
	return strings.Join(s, "\n  ")
}

func TestLintInvalidUTF8(t *testing.T) {
	src := doc("type: person\ncreated: 2026-11-01\norg: acme.example\n", "# Jo\n\nbad \xff byte\n")
	got := Lint(Parse("jo.md", []byte(src)))
	if !sameCodes(got, []string{"encoding"}) {
		t.Errorf("findings: %s", joinFindings(got))
	}
}

func TestLintBOM(t *testing.T) {
	src := "\xef\xbb\xbf" + doc("type: person\ncreated: 2026-11-01\norg: acme.example\n", "# Jo\n")
	got := Lint(Parse("jo.md", []byte(src)))
	if !sameCodes(got, []string{"encoding"}) {
		t.Errorf("findings: %s", joinFindings(got))
	}
}

// Every shipped template must lint clean under a valid file name.
func TestTemplatesLintClean(t *testing.T) {
	names := map[string]string{
		"policy.md":           "policy-example.md",
		"transcript.md":       "policy-example.source.md",
		"policy_exception.md": "exception-example.md",
		"tracked_doc.md":      "portal-runbook.md",
		"source.md":           "work-mail.md",
	}
	dir := filepath.Join("..", "..", "docs", "templates")
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
		name := e.Name()
		if n, ok := names[name]; ok {
			name = n
		}
		f := Parse(name, data)
		seen[f.Type()] = true
		if got := Lint(f); len(got) != 0 {
			t.Errorf("%s:\n  %s", e.Name(), joinFindings(got))
		}
	}
	for _, typ := range Types {
		if !seen[typ] && typ != "inbox" { // inbox is read until upgrade step io-folder; nothing makes one
			t.Errorf("no template for type %s", typ)
		}
	}
}

func TestCheckVault(t *testing.T) {
	mk := func(path, src string) *File { return Parse(path, []byte(src)) }
	person := "type: person\ncreated: 2026-11-01\norg: acme.example\n"
	pol := mk("policies/policy-a.md", doc(policyFM, "# A\n\nR1 policy-a: x. ^R1\n"))
	goalFM := "type: goal\ncreated: 2026-11-01\nstatus: active\nowner: Jo\n"
	areaFM := "type: area\ncreated: 2026-11-01\nstatus: active\n"
	areaP := mk("areas/area-p.md", doc(areaFM, "# P\n")) // no kind: reads as project
	areaC := mk("areas/area-c.md", doc(areaFM+"kind: concept\n", "# C\n"))
	conv := func(id, refines string) *File {
		fm := "type: convention\ncreated: 2026-11-01\nstatus: adopted\napplies_in: [x]\n"
		if refines != "" {
			fm += "refines: \"[[" + refines + "]]\"\n"
		}
		return mk("conventions/"+id+".md", doc(fm, "# "+id+"\n"))
	}
	tr := mk("policies/policy-a.source.md", doc(strings.Replace(policyFM, "type: policy\n", "type: transcript\nof: \"[[policy-a]]\"\n", 1), "# A\n\nSource: https://wiki.acme.example/policies/logging\n"))
	cases := []struct {
		name  string
		files []*File
		want  []string
	}{
		{"clean", []*File{pol, tr, mk("org/jo.md", doc(person, "# Jo\n\n[[policy-a#^R1]]\n"))}, nil},
		{"duplicate name across folders", []*File{mk("org/jo.md", doc(person, "# Jo\n")), mk("archive/jo.md", doc(person, "# Jo\n"))}, []string{"filename-duplicate"}},
		{"case-insensitive duplicate", []*File{mk("org/jo.md", doc(person, "# Jo\n")), mk("archive/JO.md", doc(person, "# Jo\n"))}, []string{"filename-duplicate"}},
		{"broken link", []*File{mk("org/jo.md", doc(person, "# Jo\n\n[[nobody]]\n"))}, []string{"link-unresolved:9"}},
		{"broken block", []*File{pol, tr, mk("org/jo.md", doc(person, "# Jo\n\n[[policy-a#^R9]]\n"))}, []string{"block-unresolved:9"}},
		{"question scope is a project", []*File{mk("questions/question-a.md", doc("type: question\ncreated: 2026-11-01\nstatus: open\nscope: \"[[acme-portal]]\"\n", "# Q\n")), mk("projects/acme-portal.md", doc("type: goal\ncreated: 2026-11-01\nstatus: active\nowner: Jo\n", "# P\n"))}, nil},
		{"question scope is missing", []*File{mk("questions/question-a.md", doc("type: question\ncreated: 2026-11-01\nstatus: open\nscope: \"[[acme-portal]]\"\n", "# Q\n"))}, []string{"link-unresolved:5"}},
		{"question scope is a person", []*File{mk("questions/question-a.md", doc("type: question\ncreated: 2026-11-01\nstatus: open\nscope: \"[[jo]]\"\n", "# Q\n")), mk("org/jo.md", doc(person, "# Jo\n"))}, []string{"scope"}},
		{"goal home is a project area", []*File{mk("projects/goal-a.md", doc(goalFM+"area: \"[[area-p]]\"\nconcepts:\n  - \"[[area-c]]\"\n", "# G\n")), areaP, areaC}, nil},
		{"goal home is a concept area", []*File{mk("projects/goal-a.md", doc(goalFM+"area: \"[[area-c]]\"\n", "# G\n")), areaC}, []string{"field-value"}},
		{"goal concept is a project area", []*File{mk("projects/goal-a.md", doc(goalFM+"concepts:\n  - \"[[area-p]]\"\n", "# G\n")), areaP}, []string{"field-value"}},
		{"project area concept is a person", []*File{mk("areas/area-q.md", doc(areaFM+"concepts:\n  - \"[[jo]]\"\n", "# Q\n")), mk("org/jo.md", doc(person, "# Jo\n"))}, []string{"field-value"}},
		{"refines same type", []*File{conv("conv-a", ""), conv("conv-b", "conv-a")}, nil},
		{"refines other type", []*File{conv("conv-b", "jo"), mk("org/jo.md", doc(person, "# Jo\n"))}, []string{"field-value"}},
		{"refines cycle", []*File{conv("conv-a", "conv-b"), conv("conv-b", "conv-a")}, []string{"field-value", "field-value"}},
		{"policy without transcript", []*File{pol}, []string{"transcript-missing"}},
		{"fidelity none needs no transcript", []*File{mk("policies/policy-a.md", doc(strings.Replace(strings.Replace(policyFM, "  hash: "+hash+"\n", "", 1), "fidelity: exact", "fidelity: none", 1), "# A\n\nR1 policy-a: x. ^R1\n"))}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := CheckVault(c.files)
			if !sameCodes(got, c.want) {
				t.Errorf("findings:\n  %s\nwant %v", joinFindings(got), c.want)
			}
		})
	}
}

func TestParseFileMissing(t *testing.T) {
	if _, err := ParseFile("does-not-exist.md"); err == nil || !strings.HasPrefix(err.Error(), "parse file: ") {
		t.Errorf("err = %v", err)
	}
}

func TestLintExceptionPolicyOptionalAndLegacyKind(t *testing.T) {
	base := "type: policy_exception\ncreated: 2026-11-01\nstatus: open\nby: unknown\n"
	cases := []struct {
		name, fm string
		bad      bool
	}{
		{"policy_text only, new kinds", base + "kind: established_example\npolicy_text: Logging standard\n", false},
		{"wide_enough_for with a link", base + "kind: wide_enough_for\npolicy: \"[[policy-logging]]\"\n", false},
		{"neither policy nor policy_text", base + "kind: bug\n", true},
		{"an old vault with legacy_kind", base + "kind: interpreted_as\npolicy_text: Logging standard\nlegacy_kind: established example\n", false},
	}
	for _, c := range cases {
		var errs []string
		for _, f := range Lint(Parse("policies/exception-x.md", []byte(doc(c.fm, "# X\n")))) {
			if f.Severity == Error {
				errs = append(errs, f.Msg)
			}
		}
		if (len(errs) > 0) != c.bad {
			t.Errorf("%s: errors = %v", c.name, errs)
		}
	}
}
