package ops

import (
	"errors"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

// oldStaged files a pre-0.3.0 dossier of staged writes, as the gate wrote
// them: kind para, pending, evidence "agent claude" (not checkable).
func oldStaged(t *testing.T, d Deps, id string, items ...model.ProposalItem) {
	t.Helper()
	for i := range items {
		items[i].Kind, items[i].State = model.ItemPara, model.ItemPending
		items[i].How, items[i].Evidence = "staged", model.Evidence{Quote: "staged", Locator: "agent claude"}
	}
	body, err := model.NewDossier(model.DossierHeader{TLDR: "Staged writes"}, "Items", items).Render()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Store.Create(t.Context(), model.Document{ID: id, Folder: model.FolderProposals, Title: "Staged writes", Body: body,
		Meta: &model.Proposal{Common: model.Common{Type: model.TypeProposal, Created: "2026-10-01"}, Status: model.ProposalOpen, Revision: 1, ProposedBy: "claude"}}); err != nil {
		t.Fatal(err)
	}
}

func TestProposalRetire(t *testing.T) {
	f := newAgentFix(t)
	task := mustRun(t, TaskAdd, f.owner, TaskAddInput{Goal: f.proj, Title: "Renew cert", DoneWhen: "Renewed."}).ID
	oldStaged(t, f.owner, "proposal-applied",
		model.ProposalItem{ID: "s1", Op: "task_edit", Input: map[string]any{"id": task, "effort": "s"}},
		model.ProposalItem{ID: "s2", Op: "task_add", Input: map[string]any{"goal": f.proj, "title": "Rotate keys", "done_when": "Rotated."}})
	oldStaged(t, f.owner, "proposal-unapplied",
		model.ProposalItem{ID: "s1", Op: "task_add", Input: map[string]any{"goal": f.proj, "title": "Never made", "done_when": "Made."}})
	lint, err := f.owner.Store.Lint(t.Context())
	if err != nil || len(lint) != 3 {
		t.Fatalf("lint before = %v %v", lint, err)
	}
	// The agent made the staged writes directly.
	mustRun(t, TaskEdit, f.agent, TaskEditInput{ID: task, Effort: model.EffortS})
	mustRun(t, TaskAdd, f.agent, TaskAddInput{Goal: f.proj, Title: "Rotate keys", DoneWhen: "Rotated."})

	if _, err := ProposalRetire.Run(t.Context(), f.agent, ProposalRetireInput{IDs: []string{"proposal-applied"}}); !errors.Is(err, ErrPersonOnly) {
		t.Fatalf("agent retire = %v", err)
	}
	if _, err := ProposalRetire.Run(t.Context(), f.owner, ProposalRetireInput{}); err == nil {
		t.Fatal("no ids and no all_applied accepted")
	}

	dry := mustRun(t, ProposalRetire, f.owner, ProposalRetireInput{AllApplied: true, DryRun: true})
	if len(dry.Rows) != 2 || !dry.DryRun {
		t.Fatalf("dry = %+v", dry)
	}
	byID := map[string]RetireRow{}
	for _, r := range dry.Rows {
		byID[r.ID] = r
	}
	if a := byID["proposal-applied"]; !a.Retired || len(a.Open) != 2 || a.Open[0].Applied == "" || a.Open[1].Applied == "" {
		t.Fatalf("applied row = %+v", a)
	}
	if u := byID["proposal-unapplied"]; u.Retired || !strings.Contains(u.Skipped, "s1") {
		t.Fatalf("unapplied row = %+v", u)
	}
	if v := mustRun(t, ProposalList, f.owner, ProposalListInput{Status: model.ProposalOpen}); len(v.Items) != 2 {
		t.Fatalf("a dry run changed something: %+v", v.Items)
	}

	mustRun(t, ProposalRetire, f.owner, ProposalRetireInput{AllApplied: true})
	doc, err := f.owner.Store.Get(t.Context(), "proposal-applied")
	if err != nil {
		t.Fatal(err)
	}
	if doc.Meta.(*model.Proposal).Status != model.ProposalClosed || !strings.Contains(doc.Body, "## Retired\n\nRetired ") ||
		!strings.Contains(doc.Body, "- s1 task_edit: applied directly, [[receipt-") {
		t.Fatalf("retired:\n%s", doc.Body)
	}
	// The unapplied one is retired by id, with the owner's note.
	mustRun(t, ProposalRetire, f.owner, ProposalRetireInput{IDs: []string{"proposal-unapplied"}, Note: "Not needed any more"})
	doc, _ = f.owner.Store.Get(t.Context(), "proposal-unapplied")
	if !strings.Contains(doc.Body, ": Not needed any more.") || !strings.Contains(doc.Body, "- s1 task_add: not applied") {
		t.Fatalf("by id:\n%s", doc.Body)
	}
	if v := mustRun(t, ProposalList, f.owner, ProposalListInput{Status: model.ProposalOpen}); len(v.Items) != 0 {
		t.Fatalf("still open: %+v", v.Items)
	}
	again := mustRun(t, ProposalRetire, f.owner, ProposalRetireInput{IDs: []string{"proposal-unapplied"}})
	if again.Rows[0].Retired || again.Rows[0].Skipped != "already closed" {
		t.Fatalf("again = %+v", again.Rows)
	}
	mustLintClean(t, f.owner)
}
