package ops

import (
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

func evidenceMD(ev string) string {
	return "```yaml para-dossier\ntldr: Pick a vendor\n```\n\n## Q\n\n" +
		"```yaml para-question id=q1\ntitle: Which vendor?\noptions: [A, B]\nevidence: " + ev + "\n```\n"
}

func TestEvidenceMustBeVerifiable(t *testing.T) {
	f := newAgentFix(t)
	refused := []string{
		`{quote: "Roll back.", locator: "slack #ops"}`,
		`{quote: "Roll back.", locator: "slack #ops", via: {source: slack, ref: "C01/p17"}}`, // via without verify
		`{quote: "Roll back.", locator: "task-none-such"}`,
	}
	for _, ev := range refused {
		_, err := ProposalAdd.Run(writeCtx(t), f.agent, ProposalAddInput{Title: "Pick", Markdown: evidenceMD(ev)})
		if err == nil {
			t.Fatalf("evidence %s was accepted", ev)
		}
		if strings.Contains(ev, "#ops\"}") && !strings.Contains(err.Error(), "permalink: use it") {
			t.Errorf("refusal = %v", err)
		}
	}
	note, err := f.owner.Store.Get(ctx, f.source)
	if err != nil {
		t.Fatal(err)
	}
	accepted := []string{
		`{quote: "Roll back.", locator: "https://chat.example/archives/C01/p17"}`,
		`{quote: "Ship it.", locator: "[[` + f.source + `]]"}`,
		`{quote: "Ship it.", locator: "` + string(note.Folder) + `/` + f.source + `.md:3"}`,
		`{quote: "Roll back.", locator: "slack #ops", via: {source: slack, ref: "C01/p17"}, verify: "Slack #ops, Dana on 2026-11-05 17:43: search rollback"}`,
	}
	for _, ev := range accepted {
		if _, err := ProposalAdd.Run(writeCtx(t), f.agent, ProposalAddInput{Title: "Pick", Markdown: evidenceMD(ev)}); err != nil {
			t.Errorf("evidence %s: %v", ev, err)
		}
	}
}

func TestOldEvidenceStillReads(t *testing.T) {
	f := newAgentFix(t)
	body := evidenceMD(`{quote: "Roll back.", locator: "slack #ops"}`)
	if _, err := model.ParseDossier(body); err != nil {
		t.Fatalf("an old dossier does not read: %v", err)
	}
	found := model.LintDossier(body)
	if len(found) != 1 || found[0].Code != model.CodeDossierEvidence {
		t.Fatalf("lint = %v, want one evidence warning", found)
	}
	// Filed before the check: it reads, and revises while the item keeps its evidence.
	doc, err := f.agent.Store.Create(ctx, model.Document{Folder: model.FolderProposals, Title: "Old", Body: body,
		Meta: &model.Proposal{Common: model.Common{Type: model.TypeProposal}, Status: model.ProposalOpen, Revision: 1, ProposedBy: "claude"}})
	if err != nil {
		t.Fatalf("a dossier with an old locator is refused: %v", err)
	}
	if _, err := ProposalRevise.Run(writeCtx(t), f.agent, ProposalReviseInput{ID: doc.ID, Markdown: strings.Replace(body, "Pick a vendor", "Pick a vendor now", 1)}); err != nil {
		t.Fatalf("revise with the old evidence kept: %v", err)
	}
}
