package upgrade

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/docs"
	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
	"github.com/iodesystems/para/internal/store/local"
	"github.com/iodesystems/para/internal/vault"
)

// dossierBody is a dossier with a narrative per section and the blocks given
// in order, as an agent wrote them.
func dossierBody(tldr, context string, sections map[string][]string, order []string) string {
	var b strings.Builder
	b.WriteString("````yaml para-dossier\ntldr: " + tldr + "\ncontext: " + context + "\n````\n")
	for _, sec := range order {
		b.WriteString("\n## " + sec + "\n\nWhy " + strings.ToLower(sec) + " matters, in the agent's words.\n\n")
		for _, blk := range sections[sec] {
			b.WriteString(blk + "\n\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

const ev = "evidence:\n  quote: \"we agreed\"\n  locator: https://chat.acme.example/m/1\n"

func blk(kind, id, yaml string) string {
	return "```yaml para-" + kind + " id=" + id + "\n" + yaml + ev + "```"
}

// heavyDossierFixture is a synthetic vault at format 7 shaped like heavy
// dossier use: many closed and superseded revisions (executed and revised
// proposals), and open dossiers that hold questions, messages with and
// without an io draft, actions, a doc edit, staged changes, items put off,
// an item after another, and an item held in an older file.
func heavyDossierFixture(t *testing.T) (*fixture, store.Store) {
	t.Helper()
	dir := t.TempDir()
	f := &fixture{t: t, root: filepath.Join(dir, "v"), home: filepath.Join(dir, "state")}
	tpl, err := fs.Sub(docs.Templates, "templates")
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Init(f.root, "acme", tpl, vault.InitOptions{NoGit: true}); err != nil {
		t.Fatal(err)
	}
	s := f.open("one")
	ctx := context.Background()
	if err := s.SetConfig(ctx, ConfigKey, "7"); err != nil {
		t.Fatal(err)
	}
	prop := func(st model.ProposalStatus, rev int) *model.Proposal {
		return &model.Proposal{Common: model.Common{Type: model.TypeProposal, Tags: []string{"launch"}}, Status: st, Revision: rev, ProposedBy: "agent:claude"}
	}
	create := func(d model.Document) {
		t.Helper()
		if _, err := s.Create(ctx, d); err != nil {
			t.Fatalf("create %s: %v", d.ID, err)
		}
	}
	create(model.Document{ID: "person-ada", Folder: model.FolderOrg, Title: "Ada", Meta: &model.Person{Common: model.Common{Type: model.TypePerson}}})
	create(model.Document{ID: "question-budget", Folder: model.FolderQuestions, Title: "Budget?", Body: "How much?",
		Meta: &model.Question{Common: model.Common{Type: model.TypeQuestion}, Status: model.QuestionOpen, Scope: model.ScopeRoot}})
	create(model.Document{ID: "io-hello", Folder: model.FolderIO, Title: "Hello Ada", Body: "Hello Ada, the launch is Friday.",
		Meta: &model.IO{Common: model.Common{Type: model.TypeIO}, Direction: model.IOOut, Medium: model.MediumSlack, State: model.IODraft,
			To: []string{"[[person-ada]]"}, Expects: model.ExpectsNone}})
	// Closed history: executed and revised dossiers, every item final.
	for i := range 12 {
		st, state := model.ProposalClosed, "done"
		if i%3 == 0 {
			st = model.ProposalSuperseded // a revised one: the later revision replaced it
		}
		body := dossierBody("Done "+fmt.Sprint(i), "history", map[string][]string{"Sent": {
			blk("message", "i1", "subtype: slack\npeople: [ada@acme.example]\nbody: Shipped.\nstate: "+state+"\n"),
			blk("change", "i2", "op: task_add\ninput: {title: Old}\nstate: approved\n"),
		}}, []string{"Sent"})
		create(model.Document{ID: fmt.Sprintf("proposal-old%d", i), Folder: model.FolderProposals, Title: fmt.Sprint("Old ", i), Body: body, Meta: prop(st, 1+i%3)})
	}
	// Open dossier one: every kind.
	create(model.Document{ID: "proposal-launch", Folder: model.FolderProposals, Title: "Launch plan", Meta: prop(model.ProposalOpen, 3),
		Body: dossierBody("Launch on Friday", "The team agreed in chat.", map[string][]string{
			"Decide": {
				blk("question", "q1", "title: Which day?\noptions: [Friday, Monday]\nrecommended: Friday\nbody: Pick the day.\nstate: pending\nanswer:\n  choices: [Monday]\n  via: pwa\n"),
				blk("question", "q2", "title: Budget?\nfor: \"[[question-budget]]\"\nstate: pending\n"),
				blk("question", "q3", "title: Later one\nstate: pending\ndefer_until: \"2026-12-01\"\ndeferred_reason: after the freeze\n"),
				blk("question", "q4", "title: Answered\nstate: answered\n"),
			},
			"Send": {
				blk("message", "m1", "subtype: slack\nio: \"[[io-hello]]\"\nstate: approved\napproved_hash: sha256:"+strings.Repeat("a", 64)+"\n"),
				blk("message", "m2", "subtype: email\npeople: [ada@acme.example]\nsubject: Launch\nbody: The launch is Friday.\nstate: skipped\n"),
				blk("action", "a1", "subtype: pr\ntarget: github.com/acme/site\nwhat: Open a PR\nhow: Bump the version\ndiff: \"-1\\n+2\"\nstate: claimed\nclaimed_by: agent:claude\n"),
				blk("doc-edit", "e1", "target: https://wiki.acme.example/launch\nold: Monday\nnew: Friday\nstate: pending\n"),
				blk("question", "q5", "title: After the PR?\nafter: a1\nstate: pending\n"),
				blk("message", "m3", "subtype: slack\nchannel: \"#launch\"\nbody: Denied text.\nstate: denied\nreason: no\n"),
			},
			"Change": {
				blk("change", "c1", "op: task_add\ninput: {title: Write notes}\nstate: pending\n"),
				blk("change", "c2", "op: task_add\ninput: {title: After notes}\nafter: c1\nstate: approved\nwaits: true\n"),
			},
		}, []string{"Decide", "Send", "Change"})})
	// Open dossier two: sent back, only staged and one message.
	create(model.Document{ID: "proposal-cleanup", Folder: model.FolderProposals, Title: "Cleanup", Meta: prop(model.ProposalSentBack, 2),
		Body: dossierBody("Clean up", "Old tasks.", map[string][]string{"Do": {
			blk("change", "c1", "op: task_resolve\ninput: {id: task-x, outcome: dropped}\nstate: sent_back\n"),
			blk("message", "m1", "subtype: slack\npeople: [\"[[person-ada]]\"]\nbody: Cleaning up.\nstate: pending\n"),
		}}, []string{"Do"})})
	// Open dossier three: status open, nothing open in it.
	create(model.Document{ID: "proposal-stale", Folder: model.FolderProposals, Title: "Stale", Meta: prop(model.ProposalOpen, 1),
		Body: dossierBody("All done", "", map[string][]string{"Done": {blk("message", "m1", "people: [a@acme.example]\nbody: Hi.\nstate: done\n")}}, []string{"Done"})})
	return f, s
}

func openCounts(t *testing.T, s store.Store) (map[string]int, int) {
	t.Helper()
	ctx := context.Background()
	refs, err := s.List(ctx, store.Filter{Type: model.TypeProposal, All: true})
	if err != nil {
		t.Fatal(err)
	}
	sum, n := map[string]int{}, 0
	for _, r := range refs {
		d, err := s.Get(ctx, r.ID)
		if err != nil {
			t.Fatal(err)
		}
		for k, v := range DossierCounts(d) {
			sum[k] += v
			n += v
		}
	}
	return sum, n
}

func TestDossiersStep(t *testing.T) {
	f, s := heavyDossierFixture(t)
	ctx := context.Background()
	before, total := openCounts(t, s)
	// q1 q2 q3 q5 m1 m2 a1 e1 c1 c2 in launch; c1 m1 in cleanup.
	if total != 12 || before[MoveStaged] != 3 || before[MoveQuestion] != 3 || before[MoveLinkQ] != 1 || before[MoveDraft] != 4 || before[MoveLinkIO] != 1 {
		t.Fatalf("fixture counts = %v (%d)", before, total)
	}
	files := func() map[string]string {
		out := map[string]string{}
		_ = filepath.WalkDir(f.root, func(p string, e fs.DirEntry, err error) error {
			if err == nil && !e.IsDir() && strings.HasSuffix(p, ".md") && !strings.Contains(p, "/.para/") {
				b, _ := os.ReadFile(p)
				out[strings.TrimPrefix(p, f.root+"/")] = string(b)
			}
			return nil
		})
		return out
	}
	orig := files()

	// Status: the automated part for the owner, the judgement part for an agent.
	st, err := StatusOf(ctx, s)
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Pending) != 2 || st.Pending[0].ID != "dossiers" || !st.Pending[0].Auto || st.Pending[0].Count != 3 ||
		st.Pending[1].ID != "dossiers" || st.Pending[1].Auto || st.Pending[1].Count != 2 {
		t.Fatalf("status = %+v", st.Pending)
	}

	// The dry run prints the plan per dossier and writes nothing.
	dry, err := Run(ctx, s, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(dry.Steps) != 1 || len(dry.Steps[0].Plan) != 3 {
		t.Fatalf("dry = %+v", dry)
	}
	plan := map[string]string{}
	for _, p := range dry.Steps[0].Plan {
		plan[p.Doc] = strings.Join(p.Lines, "\n")
	}
	for _, want := range []string{"q1 question: new question question-launch-q1", "q2 question: the vault question question-budget (for)",
		"m1 message: its io draft io-hello", "a1 action: new outgoing io draft io-launch-a1", "c1 change task_add: staged"} {
		if !strings.Contains(plan["proposal-launch"], want) {
			t.Fatalf("plan lacks %q:\n%s", want, plan["proposal-launch"])
		}
	}
	if !strings.Contains(plan["proposal-stale"], "closes as superseded") {
		t.Fatalf("stale plan = %q", plan["proposal-stale"])
	}
	if !maps(orig, files()) {
		t.Fatal("a dry run changed a file")
	}
	wantDocs := []string{"proposal-cleanup", "io-cleanup-m1", "proposal-launch", "question-launch-q1", "question-launch-q3", "io-launch-m2", "io-launch-a1", "io-launch-e1", "question-launch-q5", "proposal-stale"}
	if got := dry.Steps[0].Docs; !slices.Equal(sorted(got), sorted(wantDocs)) {
		t.Fatalf("dry docs = %v", got)
	}

	res, err := Run(ctx, s, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.FormatVersion != 7 || res.Steps[0].Batch == "" || !slices.Equal(sorted(res.Steps[0].Docs), sorted(wantDocs)) {
		t.Fatalf("run = %+v", res)
	}

	// The count test: every open item is a question, a draft, or a staged change.
	get := func(id string) model.Document {
		t.Helper()
		d, err := s.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	moved := map[string]int{}
	for _, id := range []string{"proposal-launch", "proposal-cleanup"} {
		dz, err := model.ParseDossier(get(id).Body)
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range dz.Items {
			if it.State != model.ItemMoved {
				continue
			}
			switch get(it.Result).Meta.(type) {
			case *model.Question:
				moved["question"]++
			case *model.IO:
				moved["io"]++
			default:
				t.Fatalf("%s %s went to %s", id, it.ID, it.Result)
			}
		}
	}
	_, staged := openCounts(t, s)
	if moved["question"] != before[MoveQuestion]+before[MoveLinkQ] || moved["io"] != before[MoveDraft]+before[MoveLinkIO] ||
		moved["question"]+moved["io"]+staged != total {
		t.Fatalf("after: moved %v, staged %d; before %v (%d)", moved, staged, before, total)
	}

	// D9: the dossier's words are on the question; the deferral and after carry.
	q1 := get("question-launch-q1")
	qm := q1.Meta.(*model.Question)
	if qm.Kind != model.KindDecision || qm.Recommended != "Friday" || len(qm.Options) != 2 || !strings.Contains(qm.Context, "Launch on Friday") ||
		!strings.Contains(qm.Context, "Why decide matters") || !strings.Contains(q1.Body, "[[proposal-launch]]") || !strings.Contains(q1.Body, "choices: Monday") ||
		!slices.Contains(qm.Tags, "launch") {
		t.Fatalf("q1 = %+v\n%s", qm, q1.Body)
	}
	if q3 := get("question-launch-q3").Meta.(*model.Question); q3.DeferUntil != "2026-12-01" || q3.DeferredReason != "after the freeze" {
		t.Fatalf("q3 = %+v", q3.Deferral)
	}
	if q5 := get("question-launch-q5").Meta.(*model.Question); !slices.Equal(q5.DeferredAfter, []string{"io-launch-a1"}) {
		t.Fatalf("q5 = %+v", q5.Deferral)
	}
	a1 := get("io-launch-a1")
	am := a1.Meta.(*model.IO)
	if am.Medium != model.MediumGithub || am.State != model.IODraft || am.ApprovedHash != "" || !slices.Equal(am.To, []string{"github.com/acme/site"}) ||
		!strings.Contains(a1.Body, "What: Open a PR") || !strings.Contains(a1.Body, "```diff") || am.Links[0] != model.LinkTo("proposal-launch") {
		t.Fatalf("a1 = %+v\n%s", am, a1.Body)
	}
	if m2 := get("io-launch-m2"); m2.Body != "The launch is Friday." || m2.Meta.(*model.IO).Subject != "Launch" || m2.Meta.(*model.IO).Medium != model.MediumEmail {
		t.Fatalf("m2 = %+v %q", m2.Meta, m2.Body)
	}
	if get("io-hello").Rev == "" || get("io-hello").Body != "Hello Ada, the launch is Friday." {
		t.Fatal("the linked draft changed")
	}
	launch := get("proposal-launch")
	if launch.Meta.(*model.Proposal).Status != model.ProposalOpen || !strings.Contains(launch.Body, whereHeading) || !strings.Contains(launch.Body, "- q1: [[question-launch-q1]]") {
		t.Fatalf("launch:\n%s", launch.Body)
	}
	if get("proposal-stale").Meta.(*model.Proposal).Status != model.ProposalSuperseded {
		t.Fatal("the empty dossier stayed open")
	}
	for i := range 12 {
		id := fmt.Sprintf("proposal-old%d", i)
		if orig["proposals/"+id+".md"] != raw(t, f, "proposals/"+id+".md") {
			t.Fatalf("closed dossier %s changed", id)
		}
	}
	findings, err := s.Lint(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, fd := range findings {
		if fd.Severity == model.SeverityError {
			t.Fatalf("lint after the step: %s", fd)
		}
	}

	// The judgement part is reported until it is done; a re-run and a second
	// machine do nothing.
	st, _ = StatusOf(ctx, s)
	if len(st.Pending) != 1 || st.Pending[0].Auto || st.Pending[0].Count != 2 {
		t.Fatalf("status after the run = %+v", st.Pending)
	}
	if again, err := Run(ctx, s, false); err != nil || again.Steps[0].Count != 0 || again.FormatVersion != 7 {
		t.Fatalf("re-run = %+v, %v", again, err)
	}
	two := f.open("two")
	if r, err := Run(ctx, two, false); err != nil || r.Steps[0].Count != 0 {
		t.Fatalf("second machine = %+v, %v", r, err)
	}

	// Undo restores every file byte for byte and removes what the step made.
	un, err := Undo(ctx, s, res.Steps[0].Batch)
	if err != nil || len(un.Skipped) != 0 {
		t.Fatalf("undo = %+v, %v", un, err)
	}
	if !maps(orig, files()) {
		t.Fatal("undo is not byte-exact")
	}
	if _, total2 := openCounts(t, s); total2 != total {
		t.Fatalf("after undo %d open items, want %d", total2, total)
	}

	// Run again, then the agent clears the staged changes; the format rises.
	res, err = Run(ctx, s, false)
	if err != nil {
		t.Fatal(err)
	}
	doc := get("proposal-launch")
	if doc, err = RefileItem(doc, "c1", "question-budget", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := RefileItem(doc, "q1", "", "x"); err == nil {
		t.Fatal("refiled a moved question")
	}
	if doc, err = RefileItem(doc, "c2", "", "done since"); err != nil {
		t.Fatal(err)
	}
	if doc.Meta.(*model.Proposal).Status != model.ProposalSuperseded || !strings.Contains(doc.Body, "- c2: dropped: done since") || !strings.Contains(doc.Body, "- c1: re-filed as [[question-budget]]") {
		t.Fatalf("launch after refile:\n%s", doc.Body)
	}
	if _, err := s.Update(ctx, doc); err != nil {
		t.Fatal(err)
	}
	if v, _ := Settle(ctx, s); v != 7 {
		t.Fatalf("format with cleanup left = %d", v)
	}
	clean := get("proposal-cleanup")
	if clean, err = RefileItem(clean, "c1", "", "the owner said no"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Update(ctx, clean); err != nil {
		t.Fatal(err)
	}
	if v, _ := Settle(ctx, s); v != DossierFormat {
		t.Fatalf("format after the judgement part = %d", v)
	}
	if st, _ := StatusOf(ctx, s); len(st.Pending) != 0 {
		t.Fatalf("pending after = %+v", st)
	}
}

func TestDossiersStepRefusesAnOlderPara(t *testing.T) {
	f, s := heavyDossierFixture(t)
	ctx := context.Background()
	old, err := local.Open(f.root, vault.Options{ID: "acme", Actor: "user", Surface: "cli", Install: "old",
		StateDir: filepath.Join(f.home, "old"), Now: func() time.Time { return time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	// A para of format 7 is older than this step.
	if err := old.Record(ctx, OpSession, "", "v0.3.0-ob", "7", ""); err != nil {
		t.Fatal(err)
	}
	before := raw(t, f, "proposals/proposal-launch.md")
	if _, err := Run(ctx, s, false); err == nil || !strings.Contains(err.Error(), "format 7") {
		t.Fatalf("run with format 7 seen = %v", err)
	}
	if raw(t, f, "proposals/proposal-launch.md") != before {
		t.Fatal("a refused run changed a file")
	}
}

func maps(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func sorted(s []string) []string {
	out := slices.Clone(s)
	slices.Sort(out)
	return out
}
