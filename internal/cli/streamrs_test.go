package cli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
)

// seedStreamRSParity makes a dossier with an approved action to claim and
// pending messages to draft, reply to, and reject. Each case runs twice.
func seedStreamRSParity(t *testing.T, deps ops.Deps) {
	t.Helper()
	ev := model.Evidence{Quote: "RS source", Locator: "title"}
	msg := func(id string) model.ProposalItem {
		return model.ProposalItem{ID: id, Kind: model.ItemMessage, Channel: "#rs", Body: "Note " + id, Evidence: ev, State: model.ItemPending, Rev: 1}
	}
	act := model.ProposalItem{ID: "a1", Kind: model.ItemAction, What: "Open the PR", How: "gh", Evidence: ev, State: model.ItemApproved, Rev: 1}
	act.ApprovedHash, _ = act.Hash()
	body, err := model.NewDossier(model.DossierHeader{TLDR: "RS parity"}, "Items", []model.ProposalItem{act, msg("m1"), msg("m2"), msg("m3")}).Render()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := deps.Store.Create(context.Background(), model.Document{ID: "proposal-rs", Folder: model.FolderProposals, Title: "RS parity", Body: body,
		Meta: &model.Proposal{Common: model.Common{Type: model.TypeProposal}, Status: model.ProposalOpen, Revision: 1, ProposedBy: "user"}}); err != nil {
		t.Fatal(err)
	}
}

func streamRSParityCases() (map[string]parityCase, map[string]string) {
	draft := `{"id":"proposal-rs","revision":1,"items":[{"item":"m1","answer":{"decision":"approve"}}]}`
	return map[string]parityCase{
		"proposal_response": {`{"id":"proposal-rs"}`, []string{"proposal", "response", "proposal-rs"}, false},
		"proposal_claim":    {`{"id":"proposal-rs","items":["a1"]}`, []string{"proposal", "claim", "proposal-rs", "a1"}, false},
		"proposal_reply":    {`{"id":"proposal-rs","section":"items","text":"Noted."}`, []string{"proposal", "reply", "proposal-rs", "Noted.", "--section", "items"}, false},
		"proposal_stack":    {`{"proposal":"proposal-rs"}`, []string{"proposal", "stack", "--proposal", "proposal-rs"}, false},
		"proposal_draft":    {draft, []string{"proposal", "draft", "proposal-rs"}, false},
		"proposal_submit":   {`{"id":"proposal-rs","revision":1}`, []string{"proposal", "submit", "proposal-rs"}, false},
		"stack_decide":      {`{"items":[{"proposal":"proposal-rs","item":"m2"}],"decision":"reject"}`, []string{"stack", "decide", "reject", "proposal-rs:m3"}, false},
	}, map[string]string{
		"proposal_draft": draft,
	}
}

// TestSubmitAnswersTypedInTheFile: the owner types answers into the dossier
// in an editor; nothing is decided until para proposal submit.
func TestSubmitAnswersTypedInTheFile(t *testing.T) {
	e := newEnv(t)
	path := e.init("acme")
	md := "```yaml para-dossier\ntldr: Tell Ada\n```\n\n## Mail\n\n```yaml para-message id=m1\npeople: [ada@acme.example]\nbody: Hi Ada.\nevidence: {quote: q, locator: l}\n```\n"
	in, _ := json.Marshal(ops.ProposalAddInput{Title: "Tell Ada", Markdown: md})
	e.stdin = string(in)
	var filed ops.ProposalFiled
	e.json(&filed, "--actor", "claude", "--why", "The owner asked.", "proposal", "add")
	file := filepath.Join(path, "proposals", filed.ID+".md")
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	typed := strings.Replace(string(raw), "body: Hi Ada.\n", "body: Hi Ada.\nanswer: {decision: approve, comment: Send it today.}\n", 1)
	if err := os.WriteFile(file, []byte(typed), 0o600); err != nil {
		t.Fatal(err)
	}
	var resp ops.ProposalResponseOutput
	e.json(&resp, "proposal", "response", filed.ID)
	if resp.Items[0].State != model.ItemPending || resp.Items[0].Answer.Via != model.ViaFile || resp.Items[0].Submitted {
		t.Fatalf("before submit = %+v", resp.Items[0])
	}
	e.ok("proposal", "submit", filed.ID)
	e.json(&resp, "proposal", "response", filed.ID)
	if resp.Items[0].State != model.ItemApproved || resp.Response != model.ResponseSubmitted || strings.Join(resp.Todo.Claim, ",") != "m1" {
		t.Fatalf("after submit = %+v", resp)
	}
	// An agent cannot submit.
	if _, _, err := e.run("--actor", "claude", "--why", "x", "proposal", "submit", filed.ID); err == nil || !strings.Contains(err.Error(), "only a person") {
		t.Fatalf("agent submit: %v", err)
	}
}

// TestGateStagesCLIAgentWrites: with the default gate an agent's unrated write
// is staged and the command succeeds; --minor --obvious applies it.
func TestGateStagesCLIAgentWrites(t *testing.T) {
	e := newEnv(t)
	path := e.init("acme")
	if err := os.WriteFile(filepath.Join(path, ".para", "vault.properties"), []byte(strings.ReplaceAll(readFile(t, filepath.Join(path, ".para", "vault.properties")), "gate.require=none\n", "")), 0o600); err != nil {
		t.Fatal(err)
	}
	var staged ops.Staged
	e.json(&staged, "--actor", "claude", "--why", "Owner asked.", "inbox", "add", "Synthetic capture")
	if !staged.Staged || staged.Op != "io_add" || staged.Item != "s1" {
		t.Fatalf("staged = %+v", staged)
	}
	var added ops.IOAddOutput
	e.json(&added, "--actor", "claude", "--why", "Owner asked.", "--minor", "--obvious", "inbox", "add", "Another capture")
	if added.ID == "" {
		t.Fatalf("applied = %+v", added)
	}
}
