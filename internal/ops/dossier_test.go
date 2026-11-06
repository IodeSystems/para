package ops

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

// dossierMD is an agent's dossier: a question, two changes in a bundle, a
// message, and two alternatives.
func dossierMD(f *agentFix) string {
	ev := "evidence: {quote: \"Ship it.\", locator: \"https://acme.example/notes#L1\"}\n"
	return "Context for the owner.\n\n```yaml para-dossier\ntldr: Pick a vendor and add the tasks\n```\n\n## Findings\n\nVendor B is cheaper.\n\n" +
		"```yaml para-question id=q1\ntitle: Which vendor?\nmulti: true\noptions: [A, B, C]\nrecommended: B\n" + ev + "```\n\n" +
		"## Changes\n\n" +
		"```yaml para-change id=c1\nop: task_add\ninput: {project: " + f.proj + ", title: Order nodes, done_when: Ordered.}\nhow: add a task\nimpact: one more task\nbundle: b1\ngroup: tasks\n" + ev + "```\n\n" +
		"```yaml para-change id=c2\nop: task_add\ninput: {project: " + f.proj + ", title: Pay vendor, done_when: Paid.}\nhow: add a task\nimpact: one more task\nbundle: b1\ngroup: tasks\n" + ev + "```\n\n" +
		"```yaml para-message id=m1\npeople: [ada@acme.example]\nbody: Hi Ada.\n" + ev + "```\n\n" +
		"```yaml para-action id=a1\nwhat: Book room A\nhow: calendar\nchoice: room\n" + ev + "```\n\n" +
		"```yaml para-action id=a2\nwhat: Book room B\nhow: calendar\nchoice: room\n" + ev + "```\n"
}

func eventsText(t *testing.T, root string) string {
	t.Helper()
	var b strings.Builder
	files, _ := filepath.Glob(filepath.Join(root, "events", "*.jsonl"))
	for _, p := range files {
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(data)
	}
	return b.String()
}

func TestSummaryOfQuestionAndDocEdit(t *testing.T) {
	f := newAgentFix(t)
	z := newItemSummarizer(t.Context(), f.owner)
	if got := z.of(model.ProposalItem{Kind: model.ItemQuestion, Title: "Which vendor?"}); got != "Question: Which vendor?" {
		t.Fatalf("question = %q", got)
	}
	if got := z.of(model.ProposalItem{Kind: model.ItemDocEdit, Target: "[[" + f.source + "]]"}); got != "Edit Sync notes" {
		t.Fatalf("doc edit = %q", got)
	}
}
