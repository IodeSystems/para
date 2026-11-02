package importer

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iodesystems/parasolve/docs"
	"github.com/iodesystems/parasolve/internal/index"
	"github.com/iodesystems/parasolve/internal/store/local"
	"github.com/iodesystems/parasolve/internal/vault"
)

var ctx = context.Background()

// put writes a synthetic source file.
func put(t testing.TB, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// newStore makes an empty vault and opens it.
func newStore(t testing.TB) (*local.Store, string) {
	t.Helper()
	base := t.TempDir()
	root := filepath.Join(base, "vault")
	tpl, err := fs.Sub(docs.Templates, "templates")
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Init(root, "acme", tpl, vault.InitOptions{NoGit: true}); err != nil {
		t.Fatal(err)
	}
	s, err := local.Open(root, vault.Options{ID: "acme", Actor: "user", Surface: "cli", Install: "m1", StateDir: filepath.Join(base, "state")})
	if err != nil {
		t.Fatal(err)
	}
	return s, root
}

// legacyFixture writes an invented legacy vault: every name, date, and text is
// made up. It follows the contract of the legacy adapter.
func legacyFixture(t testing.TB) string {
	t.Helper()
	d := filepath.Join(t.TempDir(), "legacy")
	put(t, d, "00-Inbox/call-the-plumber.md", "Call the plumber about the leak.\n")
	put(t, d, "00-Inbox/Outbox/draft-to-ada.md", "---\nstatus: draft-not-sent\nto: ada@acme.example\n---\n\n# Draft to Ada\n\nCan the portal launch before the audit?\n")
	put(t, d, "01-Projects/Atlas/Atlas.md", "---\ntype: project\nstatus: active\nowner: Jordan Example\narea: \"[[Home Lab]]\"\n---\n\n# Atlas\n\nMove the server.\n")
	put(t, d, "01-Projects/Atlas/design-notes.md", "---\ncolour: teal\n---\n\n# Design notes\n\nSee [[Atlas]] and ![[Atlas]] and [[Missing Page]] and [[Atlas#Plan|the plan]].\n\n`[[not-a-link]]`\n")
	put(t, d, "01-Projects/Beacon/Beacon.md", "---\ntype: project\nstatus: paused\n---\n\n# Beacon\n\nRenew certificates.\n")
	put(t, d, "02-Areas/Home Lab.md", "---\ntype: area\n---\n\n# Home Lab\n\nThe lab at home.\n")
	put(t, d, "03-Resources/jordan-example.md", "---\ntype: person\norg: acme.example\naliases: [Jordie]\n---\n\n# Jordan Example\n\nPlatform engineer.\n")
	put(t, d, "03-Resources/logging-policy.md", "---\ntype: policy\nurl: https://wiki.acme.example/policies/logging\n---\n\n# Logging policy\n\nServices log failures.\n")
	put(t, d, "03-Resources/c-001.md", "---\ntype: clarification\nid: C-001\nkind: wide_enough_for\npolicy: logging-policy\nby: Jordan Example\n---\n\n# C-001 batch jobs\n\nBatch jobs count as services.\n")
	put(t, d, "03-Resources/c-002.md", "---\ntype: clarification\nid: C-002\nkind: made_up_kind\npolicy: logging-policy\n---\n\n# C-002 odd\n\nBody.\n")
	put(t, d, "03-Resources/outage.md", "---\ntype: incident\nseverity: sev2\nstatus: resolved\nstarted: 2025-03-01T09:00:00Z\n---\n\n# Portal outage\n\nRolled back.\n")
	put(t, d, "03-Resources/outage-two.md", "---\ntype: incident\n---\n\n# Second outage\n\nNo severity recorded.\n")
	put(t, d, "03-Resources/evidence.md", "---\ntype: evidence\n---\n\n# Audit evidence\n\nA screenshot list.\n")
	put(t, d, "03-Resources/cc-7.md", "---\ntype: change-case\n---\n\n# Change case 7\n\nMove the disk.\n")
	put(t, d, "04-Archive/Old/Old.md", "---\ntype: project\n---\n\n# Old\n\nDone long ago.\n")
	put(t, d, "Journal/Rollup/2025-03-01.md", "---\ntype: daily\n---\n\n# 2025-03-01\n\nWorked on [[Atlas]].\n")
	put(t, d, "Journal/Rollup/2025-W10.md", "---\ntype: weekly\n---\n\n# Week 10\n\nSummary.\n")
	put(t, d, "_Templates/daily.md", "# Template\n")
	put(t, d, "stray/readme.md", "# Stray\n")
	put(t, d, "03-Resources/diagram.png", "not really a png")
	put(t, d, "events.jsonl", strings.Join([]string{
		`{"ts":"2025-03-01T09:00:00Z","actor":"owner","action":"add","id":"T-250301-01","type":"task","text":"Order the disk","project":"Atlas","due":"2025-03-10"}`,
		`{"ts":"2025-03-01T09:01:00Z","actor":"owner","action":"status","id":"T-250301-01","status":"inflight"}`,
		`{"ts":"2025-03-01T09:02:00Z","actor":"owner","action":"add","id":"T-250301-02","type":"task","text":"Ask Ada about the window","project":"Atlas"}`,
		`{"ts":"2025-03-01T09:03:00Z","actor":"owner","action":"contact","id":"T-250301-02","who":"Ada Example","via":"email","state":"drafted"}`,
		`{"ts":"2025-03-01T09:04:00Z","actor":"owner","action":"contact","id":"T-250301-02","who":"Ada Example","state":"sent","reply_by":"2025-03-08","ref":"thread-17"}`,
		`{"ts":"2025-03-01T09:05:00Z","actor":"owner","action":"add","id":"T-250301-03","type":"task","text":"Mount the disk","project":"Atlas"}`,
		`{"ts":"2025-03-01T09:06:00Z","actor":"owner","action":"depends","id":"T-250301-03","depends_on":"T-250301-01"}`,
		`{"ts":"2025-03-01T09:06:30Z","actor":"owner","action":"depends","id":"T-250301-03","depends_on":["T-250301-02","N-250301-01","T-999999-99"]}`,
		`{"ts":"2025-03-01T09:07:00Z","actor":"owner","action":"check","id":"T-250301-03","check":"touch /tmp/PWNED-by-import"}`,
		`{"ts":"2025-03-01T09:08:00Z","actor":"owner","action":"check","id":"T-250301-03","check":"test -e /mnt/new-disk"}`,
		`{"ts":"2025-03-01T09:09:00Z","actor":"owner","action":"add","id":"T-250301-04","type":"task","text":"Pick the colour","project":"Nowhere"}`,
		`{"ts":"2025-03-01T09:10:00Z","actor":"owner","action":"status","id":"T-250301-04","status":"waiting","note":"wait for the paint order"}`,
		`{"ts":"2025-03-01T09:11:00Z","actor":"owner","action":"add","id":"T-250301-05","type":"task","text":"Water the plants"}`,
		`{"ts":"2025-03-01T09:12:00Z","actor":"owner","action":"status","id":"T-250301-05","status":"done"}`,
		`{"ts":"2025-03-01T09:13:00Z","actor":"owner","action":"add","id":"T-250301-06","type":"task","text":"Settle the invoice","project":"Beacon"}`,
		`{"ts":"2025-03-01T09:14:00Z","actor":"owner","action":"status","id":"T-250301-06","status":"waiting"}`,
		`{"ts":"2025-03-01T09:15:00Z","actor":"owner","action":"contact","id":"T-250301-06","who":"Ada Example","via":"slack","state":"sent"}`,
		`{"ts":"2025-03-01T09:16:00Z","actor":"owner","action":"contact","id":"T-250301-06","who":"Bo Example","via":"phone","state":"sent"}`,
		`{"ts":"2025-03-01T09:17:00Z","actor":"owner","action":"contact","id":"T-250301-06","who":"Ada Example","state":"replied"}`,
		`{"ts":"2025-03-01T09:18:00Z","actor":"owner","action":"contact","id":"T-250301-06","who":"Bo Example","state":"no-reply"}`,
		`{"ts":"2025-03-01T10:00:00Z","actor":"owner","action":"add","id":"Q-250301-01","type":"question","text":"Which config path does prod use?","project":"Atlas"}`,
		`{"ts":"2025-03-01T10:01:00Z","actor":"owner","action":"answer","id":"Q-250301-01","answer":"/etc/atlas"}`,
		`{"ts":"2025-03-01T10:02:00Z","actor":"owner","action":"add","id":"Q-250301-02","type":"question","text":"Who signs off on the window?"}`,
		`{"ts":"2025-03-01T10:03:00Z","actor":"owner","action":"contact","id":"Q-250301-02","who":"Bo Example","via":"email","state":"sent"}`,
		`{"ts":"2025-03-01T10:04:00Z","actor":"owner","action":"add","id":"Q-250301-03","type":"question","text":"Is the budget set?"}`,
		`{"ts":"2025-03-01T11:00:00Z","actor":"owner","action":"add","id":"D-250301-01","type":"deadline","text":"Renew the certificate","due":"2025-04-01","project":"Beacon"}`,
		`{"ts":"2025-03-01T11:01:00Z","actor":"owner","action":"add","id":"N-250301-01","type":"note","text":"Port 3 is the uplink\nCheck the switch label.","project":"Atlas"}`,
		`{"ts":"2025-03-01T11:02:00Z","actor":"owner","action":"note","id":"N-250301-01","text":"Label confirmed."}`,
		`{"ts":"2025-03-01T11:03:00Z","actor":"owner","action":"risk","id":"T-250301-01","text":"The disk may arrive late."}`,
		`{"ts":"2025-03-01T11:04:00Z","actor":"owner","action":"add","id":"X-250301-01","type":"decision","text":"Use SQLite for the cache","project":"Atlas"}`,
		`{"ts":"2025-03-01T11:05:00Z","actor":"owner","action":"add","id":"I-250301-01","type":"interaction","text":"Agreed to wait for January","who":"Ada Example","via":"slack","on":"2025-03-02","direction":"in","opinion":"sounded unsure"}`,
		`{"ts":"2025-03-01T11:06:00Z","actor":"owner","action":"add","id":"I-250301-02","type":"interaction","text":"Asked about the audit","who":"Bo Example","via":"carrier pigeon","direction":"out"}`,
		`{"ts":"2025-03-01T11:07:00Z","actor":"owner","action":"add","id":"I-250301-03","type":"interaction","text":"Heard in the hall"}`,
		`{"ts":"2025-03-01T11:08:00Z","actor":"owner","action":"mystery","id":"T-250301-01"}`,
		`not json`,
		`{"ts":"2025-03-01T11:09:00Z","actor":"owner","action":"status","id":"T-000000-00","status":"done"}`,
	}, "\n")+"\n")
	return d
}

// importAll scans dir and runs the unedited plan into a fresh vault.
func importAll(t testing.TB, dir string) (*local.Store, string, *Report, *RunResult) {
	t.Helper()
	s, root := newStore(t)
	rep, plan, err := Scan(dir, "")
	if err != nil {
		t.Fatal(err)
	}
	res, err := Run(ctx, s, plan, RunOptions{})
	if err != nil {
		t.Fatal(err)
	}
	return s, root, rep, res
}

func searchQuery(text string) index.Query { return index.Query{Text: text, Limit: 10} }
