package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
	"github.com/iodesystems/para/internal/publish"
)

// pbDeps opens the env's vault as the owner, to seed documents directly.
func pbDeps(t *testing.T, e *env) ops.Deps {
	t.Helper()
	l := &loader{env: func(k string) string {
		if k == "PARA_HOME" {
			return e.home
		}
		return ""
	}}
	if err := l.load(); err != nil {
		t.Fatal(err)
	}
	deps, err := l.deps()
	if err != nil {
		t.Fatal(err)
	}
	return deps
}

func pbArea(id, title, body string, sets ...string) model.Document {
	return model.Document{ID: id, Folder: model.FolderAreas, Title: title, Body: body,
		Meta: &model.Area{Common: model.Common{Type: model.TypeArea, Publish: sets}, Status: model.AreaActive}}
}

func pbFiles(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	return names
}

// pbEgress returns the egress events of publish runs.
func pbEgress(e *env) []model.Event {
	var tail ops.EventsTailOutput
	e.json(&tail, "events", "tail", "-n", "500")
	var out []model.Event
	for _, ev := range tail.Events {
		if ev.Op == "egress" && strings.HasPrefix(ev.To, "publish:") {
			out = append(out, model.Event{Doc: ev.Doc, To: ev.To, From: ev.From, Hash: ev.Hash})
		}
	}
	return out
}

func TestPublishWritesTheSet(t *testing.T) {
	e := newEnv(t)
	vault := e.init("acme")
	deps := pbDeps(t, e)
	s := deps.Store
	rulesBody := "Ask [[person-pb-jo|Jo]] or [[group-pb-ops]]. Steps: [[area-pb-runbook]] and [[area-pb-runbook|the runbook]].\n\nPrivate: [[area-pb-private]]. Mail: [[io-pb-mail]].\n\n```\n[[area-pb-private]] stays in code\n```"
	for _, d := range []model.Document{
		{ID: "person-pb-jo", Folder: model.FolderOrg, Title: "Jo Example",
			Meta: &model.Person{Common: model.Common{Type: model.TypePerson}, Org: "acme.example"}},
		{ID: "group-pb-ops", Folder: model.FolderOrg, Title: "Ops Example",
			Meta: &model.Group{Common: model.Common{Type: model.TypeGroup}, Org: "acme.example", Kind: model.GroupTeam}},
		pbArea("area-pb-private", "Private notes", "PRIVATE-WORDS here. ^b1"),
		pbArea("area-pb-runbook", "Runbook", "RUNBOOK-STEPS.", "design"),
		pbArea("area-pb-rules", "Rules", rulesBody, "design"),
	} {
		if _, err := s.Create(t.Context(), d); err != nil {
			t.Fatalf("seed %s: %v", d.ID, err)
		}
	}
	mail := model.Document{ID: "io-pb-mail", Folder: model.FolderIO, Title: "Mail from Jo",
		Meta: &model.IO{Common: model.Common{Type: model.TypeIO}, Direction: model.IOIn, State: model.IOUnread, Medium: model.MediumEmail}}
	if _, err := s.Create(t.Context(), mail); err != nil {
		t.Fatalf("seed io: %v", err)
	}

	// No folder yet: refused, with the command that sets one.
	if _, _, err := e.run("publish", "design"); err == nil || !strings.Contains(err.Error(), "para publish dir design") {
		t.Fatalf("no dir: %v", err)
	}
	out := filepath.Join(e.dir, "shared", "design")
	e.ok("publish", "dir", "design", out)
	if got := strings.TrimSpace(e.ok("publish", "dir", "design")); got != out {
		t.Fatalf("dir = %q", got)
	}
	if _, err := os.Stat(filepath.Join(e.home, "vaults", "acme", "publish.json")); err != nil {
		t.Fatalf("the folder is not kept in the state dir: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(vault, ".para", "vault.properties")); strings.Contains(string(b), out) {
		t.Fatal("the folder went into the vault")
	}

	// An agent may not publish, nor set a folder.
	if _, _, err := e.run("--actor", "claude", "publish", "design"); err == nil {
		t.Fatal("an agent published")
	}
	if _, _, err := e.run("--actor", "claude", "publish", "dir", "design", filepath.Join(e.dir, "x")); err == nil {
		t.Fatal("an agent set a folder")
	}

	// The test vault denies egress by default: publish runs only once the owner opts in.
	e.ok("vault", "config", "set", "egress.publish", "allow")
	stdout := e.ok("publish", "design")
	for _, want := range []string{"person-pb-jo", "group-pb-ops", "io-pb-mail", "from area-pb-rules"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("review list lacks %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "area-pb-private") {
		t.Errorf("review list names a non-person: %s", stdout)
	}
	rules, runbook := publish.FileName("area-pb-rules", "Rules"), publish.FileName("area-pb-runbook", "Runbook")
	if got, want := pbFiles(t, out), []string{publish.IndexName, rules, runbook}; !slices.Equal(got, sortCopy(want)) {
		t.Fatalf("files = %v, want %v", got, want)
	}
	text := mustReadFile(t, filepath.Join(out, rules))
	for _, want := range []string{
		publish.HeaderPrefix + "design from vault acme on ",
		"Edit the source in the vault, not this file.",
		"# Rules",
		"Ask Jo or group-pb-ops.",
		"Steps: [Runbook](" + runbook + ") and [the runbook](" + runbook + ").",
		"Private: area-pb-private.",
		"[[area-pb-private]] stays in code",
		"**[UNMARKED]**",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("rules lacks %q:\n%s", want, text)
		}
	}
	if !strings.HasPrefix(text, publish.HeaderPrefix) {
		t.Errorf("header is not the first line:\n%s", text)
	}
	index := mustReadFile(t, filepath.Join(out, publish.IndexName))
	if !strings.Contains(index, "- [Rules]("+rules+")") || !strings.Contains(index, "- [Runbook]("+runbook+")") {
		t.Errorf("index:\n%s", index)
	}
	for _, name := range pbFiles(t, out) {
		if strings.Contains(mustReadFile(t, filepath.Join(out, name)), "PRIVATE-WORDS") {
			t.Fatalf("%s holds a document outside the set", name)
		}
	}

	// One egress event per run, with ids and a hash, never content.
	evs := pbEgress(e)
	if len(evs) != 1 || evs[0].Doc != "design" || evs[0].To != "publish:design" || !strings.HasPrefix(evs[0].Hash, "sha256:") {
		t.Fatalf("egress events = %+v", evs)
	}
	logs, _ := filepath.Glob(filepath.Join(vault, ".para", "events", "*.jsonl"))
	for _, p := range logs {
		if b := mustReadFile(t, p); strings.Contains(b, "RUNBOOK-STEPS") || strings.Contains(b, "Runbook") || strings.Contains(b, out) {
			t.Fatalf("the event log holds content or the folder: %s", p)
		}
	}

	// A marking for the set.
	e.ok("vault", "config", "set", "publish.design.marking", "ACME SHARED")
	e.ok("publish", "design")
	if text := mustReadFile(t, filepath.Join(out, rules)); !strings.Contains(text, "**[ACME SHARED]**") {
		t.Fatalf("marking:\n%s", text)
	}
	if got := pbFiles(t, out); !slices.Equal(got, sortCopy([]string{publish.IndexName, rules, runbook})) {
		t.Fatalf("names changed across runs: %v", got)
	}
	if n := len(pbEgress(e)); n != 2 {
		t.Fatalf("%d egress events after two runs", n)
	}

	// A document taken out of the set disappears, and links to it go plain.
	e.ok("publish", "set", "area-pb-runbook")
	e.ok("publish", "design")
	if got := pbFiles(t, out); !slices.Equal(got, sortCopy([]string{publish.IndexName, rules})) {
		t.Fatalf("after drop: %v", got)
	}
	if text := mustReadFile(t, filepath.Join(out, rules)); !strings.Contains(text, "Steps: area-pb-runbook and the runbook.") {
		t.Fatalf("link to a dropped document:\n%s", text)
	}
	e.ok("publish", "set", "area-pb-runbook", "design")
	e.ok("publish", "design")
	before := mustReadFile(t, filepath.Join(out, rules))

	// An embed or a block reference of a document outside the set refuses the
	// run, and nothing is written. Lint refuses an embed in a para write, so
	// it comes from a hand edit (Obsidian).
	events := len(pbEgress(e))
	rulesFile := filepath.Join(vault, "areas", "area-pb-rules.md")
	orig := mustReadFile(t, rulesFile)
	handEdit := func(extra string) {
		t.Helper()
		if err := os.WriteFile(rulesFile, []byte(orig+"\nQuoted: "+extra+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, link := range []string{"![[area-pb-private]]", "[[area-pb-private#^b1]]"} {
		handEdit(link)
		_, _, err := e.run("publish", "design")
		if err == nil || !strings.Contains(err.Error(), "area-pb-rules") || !strings.Contains(err.Error(), "area-pb-private") || !strings.Contains(err.Error(), "nothing written") {
			t.Fatalf("%s: err = %v", link, err)
		}
		if got := mustReadFile(t, filepath.Join(out, rules)); got != before {
			t.Fatalf("%s: the folder changed", link)
		}
		var pv ops.PublishPreviewOutput
		e.json(&pv, "publish", "preview", "design")
		if len(pv.Refused) != 1 || pv.Refused[0].Target != "area-pb-private" {
			t.Fatalf("%s: preview refused = %+v", link, pv.Refused)
		}
	}
	if n := len(pbEgress(e)); n != events {
		t.Fatalf("a refused run logged egress: %d, was %d", n, events)
	}
	// An embed of a document in the set is a link.
	handEdit("![[area-pb-runbook]] and [[area-pb-runbook#^x|step x]]")
	e.ok("publish", "design")
	if text := mustReadFile(t, filepath.Join(out, rules)); !strings.Contains(text, "Quoted: [Runbook]("+runbook+") and [step x]("+runbook+")") {
		t.Fatalf("in-set embed:\n%s", text)
	}

	// A folder with a file para did not write is refused, and the file stays.
	foreign := filepath.Join(out, "notes.txt")
	if err := os.WriteFile(foreign, []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.run("publish", "design"); err == nil || !strings.Contains(err.Error(), "notes.txt") {
		t.Fatalf("foreign file: %v", err)
	}
	if mustReadFile(t, foreign) != "mine" {
		t.Fatal("the foreign file changed")
	}
	other := filepath.Join(e.dir, "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "README.md"), []byte("# Mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	e.ok("publish", "dir", "design", other)
	if _, _, err := e.run("publish", "design"); err == nil || !strings.Contains(err.Error(), "README.md") {
		t.Fatalf("foreign md: %v", err)
	}

	// A folder inside the vault is refused.
	e.ok("publish", "dir", "design", filepath.Join(vault, "shared"))
	if _, _, err := e.run("publish", "design"); err == nil || !strings.Contains(err.Error(), "inside the vault") {
		t.Fatalf("folder in the vault: %v", err)
	}
}

func sortCopy(s []string) []string {
	out := slices.Clone(s)
	slices.Sort(out)
	return out
}

func mustReadFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
