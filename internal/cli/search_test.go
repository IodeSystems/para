package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/index"
	"github.com/iodesystems/para/internal/ops"
)

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

const policyFile = `---
type: policy
created: 2026-11-01
verified: false
source:
  url: https://wiki.acme.example/policies/logging
  retrieved: 2026-11-01T09:00:00Z
  hash: sha256:abababababababababababababababababababababababababababababababab
  fidelity: exact
---

# Logging policy

Summary: services record every authentication failure.
`

const transcriptFile = `---
type: transcript
created: 2026-11-01
of: "[[policy-logging]]"
source:
  url: https://wiki.acme.example/policies/logging
  retrieved: 2026-11-01T09:00:00Z
  hash: sha256:abababababababababababababababababababababababababababababababab
  fidelity: exact
---

# Logging policy (transcript)

Source: https://wiki.acme.example/policies/logging

## Page 1

Verbatim: authentication failures must carry a request id.
`

func TestSearchEndToEnd(t *testing.T) {
	e := newEnv(t)
	root := e.init("acme")
	write(t, root, "policies/policy-logging.md", policyFile)
	write(t, root, "policies/policy-logging.source.md", transcriptFile)

	var st index.Stats
	e.json(&st, "index", "rebuild")
	if st.Documents != 2 {
		t.Fatalf("stats = %+v", st)
	}
	var out ops.SearchOutput
	e.json(&out, "search", "authentication")
	kinds := map[string]index.Hit{}
	for _, h := range out.Hits {
		kinds[h.Kind] = h
	}
	s, tr := kinds[index.KindSummary], kinds[index.KindTranscript]
	if s.ID != "policy-logging" || tr.ID != "policy-logging.source" || !s.Unverified || tr.Unverified {
		t.Fatalf("hits = %+v", out.Hits)
	}
	for _, h := range []index.Hit{s, tr} {
		if h.SourceURL != "https://wiki.acme.example/policies/logging" || h.Path == "" || h.Title == "" || h.Passage == "" {
			t.Errorf("hit = %+v", h)
		}
	}
	if txt := e.ok("search", "authentication"); !strings.Contains(txt, "[summary, unverified, exact]") || !strings.Contains(txt, "[transcript, exact]") {
		t.Errorf("text output:\n%s", txt)
	}

	e.json(&out, "search", "authentication", "--type", "policy")
	if len(out.Hits) != 1 || out.Hits[0].ID != "policy-logging" {
		t.Errorf("--type policy: %+v", out.Hits)
	}
	e.json(&out, "search", "authentication", "--type", "task")
	if len(out.Hits) != 0 {
		t.Errorf("--type task: %+v", out.Hits)
	}
	e.json(&out, "search", "authentication", "--since", "2099-01-01")
	if len(out.Hits) != 0 {
		t.Errorf("--since 2099-01-01: %+v", out.Hits)
	}
	if _, _, err := e.run("search", "x", "--type", "bogus"); err == nil {
		t.Error("--type bogus accepted")
	}

	if _, err := os.Stat(filepath.Join(root, ".para", "index.sqlite")); err == nil {
		t.Error("index is inside the vault tree")
	}
	// A hand edit shows without a rebuild; a deleted index is rebuilt silently.
	write(t, root, "policies/policy-logging.md", strings.ReplaceAll(policyFile, "authentication", "pelican"))
	e.json(&out, "search", "pelican")
	if len(out.Hits) != 1 || out.Hits[0].ID != "policy-logging" {
		t.Fatalf("after edit: %+v", out.Hits)
	}
	if err := os.Remove(filepath.Join(e.home, "vaults", "acme", "index.sqlite")); err != nil {
		t.Fatal(err)
	}
	e.json(&out, "search", "request id")
	if len(out.Hits) != 1 || out.Hits[0].Kind != index.KindTranscript {
		t.Fatalf("after delete: %+v", out.Hits)
	}
}
