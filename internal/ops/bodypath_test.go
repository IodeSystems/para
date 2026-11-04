package ops

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

const bodyEv = "evidence: {quote: \"Ship it.\", locator: \"note line 1\"}\n"

func dossierWith(blocks ...string) string {
	return "```yaml para-dossier\ntldr: Check the path bodies\n```\n\n## Items\n\n" + strings.Join(blocks, "\n")
}

func docPathOf(t *testing.T, d Deps, id string) string {
	t.Helper()
	doc, err := d.Store.Get(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return string(doc.Folder) + "/" + id + ".md"
}

// proposal_add: source may be empty when source_path is set, and a
// source_path inside the vault becomes the source.
func TestProposalAddSourcePath(t *testing.T) {
	f := ioFix(t)
	io1 := outDraft(t, f, model.ExpectsNone, "")
	md := dossierWith("```yaml para-action id=a1\nwhat: Book the room\nhow: calendar\n" + bodyEv + "```\n")
	out := mustRun(t, ProposalAdd, f.agent, ProposalAddInput{Title: "Room", Markdown: md, SourcePath: "/srv/exports/thread.txt"})
	if show := mustRun(t, ProposalShow, f.owner, ProposalIDInput{ID: out.ID}); show.Proposal.Source != "" || show.Proposal.SourcePath != "/srv/exports/thread.txt" {
		t.Fatalf("outside path = %+v", show.Proposal)
	}
	out = mustRun(t, ProposalAdd, f.agent, ProposalAddInput{Title: "Room 2", Markdown: md, SourcePath: "io/" + io1 + ".md"})
	if show := mustRun(t, ProposalShow, f.owner, ProposalIDInput{ID: out.ID}); show.Proposal.Source != io1 {
		t.Fatalf("io path = %+v", show.Proposal)
	}
	abs := filepath.Join(f.root, docPathOf(t, f.owner, f.source))
	out = mustRun(t, ProposalAdd, f.agent, ProposalAddInput{Title: "Room 3", Markdown: md, SourcePath: abs})
	if show := mustRun(t, ProposalShow, f.owner, ProposalIDInput{ID: out.ID}); show.Proposal.Source != f.source {
		t.Fatalf("absolute path = %+v", show.Proposal)
	}
	// The items form: source_path alone is enough.
	items := []ItemInput{{Kind: model.ItemMessage, Body: "Hi.", Evidence: model.Evidence{Quote: "Ship it.", Locator: "note"}, People: []string{"ada@acme.example"}, Reversible: model.ReversibleNo}}
	if _, err := ProposalAdd.Run(writeCtx(t), f.agent, ProposalAddInput{Title: "No source", TLDR: "x", Context: "y", Items: items}); err == nil {
		t.Fatal("items with no source and no source_path were filed")
	}
	if _, err := ProposalAdd.Run(writeCtx(t), f.agent, ProposalAddInput{Title: "Path only", TLDR: "x", Context: "y", Items: items, SourcePath: "/srv/exports/thread.txt"}); err != nil {
		t.Fatalf("items with source_path only: %v", err)
	}
}

// A message body that is only the path of an io draft becomes its io
// reference; the owner's card edit (subject, body, to) is written to the
// draft and approval binds to the edited text. A path to any other file, or
// outside the vault, is refused.
func TestMessageBodyPath(t *testing.T) {
	f := ioFix(t)
	io1 := outDraft(t, f, model.ExpectsNone, "")
	msg := func(body string) string {
		return dossierWith("```yaml para-message id=m1\nbody: " + body + "\n" + bodyEv + "```\n")
	}
	if _, err := ProposalAdd.Run(writeCtx(t), f.agent, ProposalAddInput{Title: "Note", Markdown: msg(docPathOf(t, f.owner, f.source))}); err == nil || !strings.Contains(err.Error(), "put the message in an io draft") {
		t.Fatalf("message body naming a note: %v", err)
	}
	if _, err := ProposalAdd.Run(writeCtx(t), f.agent, ProposalAddInput{Title: "Out", Markdown: msg("/etc/hosts.md")}); err == nil || !strings.Contains(err.Error(), "outside the vault") {
		t.Fatalf("message body outside the vault: %v", err)
	}
	id := fileDossierMD(t, f, msg("io/"+io1+".md"))
	raw, _ := f.owner.Store.Get(t.Context(), id)
	if !strings.Contains(raw.Body, "[["+io1+"]]") || strings.Contains(raw.Body, "body:") {
		t.Fatalf("dossier body = %s", raw.Body)
	}
	if _, err := draft(t, f.owner, id, 1, DraftAnswer{Item: "m1", Answer: &model.ItemAnswer{Decision: model.AnswerEdit,
		Subject: "Quote, please", Body: "Hi Ada,\n\nthe quote by Friday, please.", People: []string{"person-ada-example"}}}); err != nil {
		t.Fatal(err)
	}
	if sub := mustRun(t, ProposalSubmit, f.owner, ProposalSubmitInput{ID: id, Revision: 1}); len(sub.Failed) != 0 {
		t.Fatalf("submit failed: %v", sub.Failed)
	}
	doc, m, _ := getIO(t.Context(), f.owner, io1)
	if m.State != model.IOReady || m.Subject != "Quote, please" || doc.Body != "Hi Ada,\n\nthe quote by Friday, please." || len(m.To) != 1 || m.To[0] != "[[person-ada-example]]" {
		t.Fatalf("draft after the edit = %s %q %q %v", m.State, m.Subject, doc.Body, m.To)
	}
	show := mustRun(t, ProposalShow, f.owner, ProposalIDInput{ID: id})
	it := show.Items[0]
	if it.State != model.ItemApproved || it.ApprovedHash != it.Hash || it.Body != doc.Body {
		t.Fatalf("approval is not bound to the edited text: %+v", it)
	}
	ex := mustRun(t, ProposalExecuted, f.agent, ProposalExecutedInput{ID: id, Item: "m1", Result: ExecDone, URL: "https://mail.acme.example/1"})
	if ex.IO == nil || ex.IO.State != model.IOSent {
		t.Fatalf("executed = %+v", ex)
	}
}

// Another kind keeps a path body: the page reads the file through the store,
// and an owner's edit puts the text in the item.
func TestActionBodyPath(t *testing.T) {
	f := ioFix(t)
	p := docPathOf(t, f.owner, f.source)
	act := func(body string) string {
		return dossierWith("```yaml para-action id=a1\nwhat: Post the notes\nhow: wiki\nbody: " + body + "\n" + bodyEv + "```\n")
	}
	if _, err := ProposalAdd.Run(writeCtx(t), f.agent, ProposalAddInput{Title: "Out", Markdown: act(filepath.Join(t.TempDir(), "notes.md"))}); err == nil || !strings.Contains(err.Error(), "outside the vault") {
		t.Fatalf("action body outside the vault: %v", err)
	}
	if _, err := ProposalAdd.Run(writeCtx(t), f.agent, ProposalAddInput{Title: "Missing", Markdown: act("resources/nothing-here.md")}); err == nil || !strings.Contains(err.Error(), "no vault document") {
		t.Fatalf("action body naming nothing: %v", err)
	}
	id := fileDossierMD(t, f, act(p))
	show := mustRun(t, ProposalShow, f.owner, ProposalIDInput{ID: id})
	if bf := show.Items[0].BodyFile; bf == nil || bf.Doc != f.source || bf.Text != "Ship it." {
		t.Fatalf("body_file = %+v", show.Items[0].BodyFile)
	}
	if _, err := draft(t, f.owner, id, 1, DraftAnswer{Item: "a1", Answer: &model.ItemAnswer{Decision: model.AnswerEdit, Subject: "Notes"}}); err != nil {
		t.Fatal(err)
	}
	mustRun(t, ProposalSubmit, f.owner, ProposalSubmitInput{ID: id, Revision: 1})
	show = mustRun(t, ProposalShow, f.owner, ProposalIDInput{ID: id})
	if it := show.Items[0]; it.Body != "Ship it." || it.BodyFile != nil || it.ApprovedHash != it.Hash {
		t.Fatalf("edited item = %+v", it)
	}
}

// Page URLs say they work on this machine only while server.base_url is unset.
func TestURLNote(t *testing.T) {
	f := ioFix(t)
	agent := f.agent
	agent.ViewerBase = func() (string, error) { return "http://acme.localhost:7700", nil }
	md := dossierWith("```yaml para-action id=a1\nwhat: Book the room\nhow: calendar\n" + bodyEv + "```\n")
	out := mustRun(t, ProposalAdd, agent, ProposalAddInput{Title: "Room", Markdown: md, SourcePath: "x"})
	if out.ReviewURL == "" || out.URLNote != urlNoteText {
		t.Fatalf("filed = %+v", out)
	}
	if show := mustRun(t, ProposalShow, agent, ProposalIDInput{ID: out.ID}); show.URLNote != urlNoteText {
		t.Fatalf("show url_note = %q", show.URLNote)
	}
	if err := f.owner.Store.SetConfig(t.Context(), reviewBaseConfig, "https://para.acme.example"); err != nil {
		t.Fatal(err)
	}
	out = mustRun(t, ProposalAdd, agent, ProposalAddInput{Title: "Room 2", Markdown: md, SourcePath: "x"})
	if !strings.HasPrefix(out.ReviewURL, "https://para.acme.example/") || out.URLNote != "" {
		t.Fatalf("with base_url = %+v", out)
	}
}
