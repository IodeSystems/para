package ops

import (
	"slices"
	"sort"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

// socialMD is a dossier of two questions for the owner.
func socialMD(title string) string {
	ev := "evidence: {quote: \"Post it.\", locator: \"https://acme.example/notes#L1\"}\n"
	return "```yaml para-dossier\ntldr: " + title + "\n```\n\n## Posts\n\n" +
		"```yaml para-question id=q1\ntitle: Post on Monday?\noptions: [yes, no]\n" + ev + "```\n\n" +
		"```yaml para-question id=q2\ntitle: Post on Friday?\noptions: [yes, no]\n" + ev + "```\n"
}

func stackRefs(out ProposalStackOutput) []string {
	var ids []string
	for _, r := range out.Rows {
		if r.Question != "" {
			ids = append(ids, r.Question)
		} else {
			ids = append(ids, r.Proposal+"#"+r.Item)
		}
	}
	sort.Strings(ids)
	return ids
}

func TestTagTopicAcrossDossiersAndQuestions(t *testing.T) {
	f := newAgentFix(t)
	a := fileDossierMD(t, f, socialMD("Posts A"))
	b := fileDossierMD(t, f, socialMD("Posts B"))
	q := must(t, QuestionAdd, f.owner, QuestionAddInput{Text: "Which network first?", Project: f.proj}).ID
	must(t, QuestionAdd, f.owner, QuestionAddInput{Text: "Unrelated?", Project: f.proj})

	out, err := TagAdd.Run(writeCtx(t), f.agent, TagInput{IDs: []string{q}, Items: []ItemRef{{Proposal: a, Item: "q1"}, {Proposal: b, Item: "q2"}}, Tags: []string{" #Socials "}})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Changed) != 3 || !slices.Equal(out.Tags, []string{"socials"}) {
		t.Fatalf("tag_add = %+v", out)
	}
	want := []string{a + "#q1", b + "#q2", q}
	sort.Strings(want)
	if got := stackRefs(must(t, ProposalStack, f.owner, ProposalStackInput{Tag: "socials"})); !slices.Equal(got, want) {
		t.Fatalf("stack by tag = %v, want %v", got, want)
	}

	// A dossier's tag reaches every item in it.
	must(t, TagAdd, f.owner, TagInput{IDs: []string{b}, Tags: []string{"launch"}})
	if got := stackRefs(must(t, ProposalStack, f.owner, ProposalStackInput{Tag: "launch"})); !slices.Equal(got, []string{b + "#q1", b + "#q2"}) {
		t.Fatalf("stack by dossier tag = %v", got)
	}
	list := must(t, TagList, f.owner, TagListInput{}).Tags
	if len(list) != 2 || list[0].Tag != "socials" || list[0].Stack != 2 || list[0].Open != 1 {
		t.Fatalf("tag_list = %+v", list)
	}

	// Each tag write has one receipt for every document it changed; undo,
	// newest first, restores them.
	rs := must(t, ReceiptList, f.owner, ReceiptListInput{Op: "tag_add", Actor: "claude"}).Items
	if len(rs) != 1 {
		t.Fatalf("receipts = %+v", rs)
	}
	mine := must(t, ReceiptList, f.owner, ReceiptListInput{Op: "tag_add", Actor: "user"}).Items
	if len(mine) != 1 {
		t.Fatalf("owner receipts = %+v", mine)
	}
	must(t, ReceiptUndo, f.owner, ReceiptUndoInput{IDs: []string{mine[0].ID, rs[0].ID}})
	if got := stackRefs(must(t, ProposalStack, f.owner, ProposalStackInput{Tag: "socials"})); len(got) != 0 {
		t.Fatalf("after undo, stack by tag = %v", got)
	}
}

func TestTagListLeavesOutSystemTags(t *testing.T) {
	f := newAgentFix(t)
	if _, err := TagAdd.Run(t.Context(), f.owner, TagInput{IDs: []string{f.proj}, Tags: []string{model.CommitmentTag}}); err == nil {
		t.Fatal("the commitment tag was set by tag_add")
	}
	doc, err := f.owner.Store.Get(t.Context(), f.proj)
	if err != nil {
		t.Fatal(err)
	}
	doc.Meta.Header().Tags = []string{model.CommitmentTag, "socials"}
	if _, err := f.owner.Store.Update(t.Context(), doc); err != nil {
		t.Fatal(err)
	}
	list := must(t, TagList, f.owner, TagListInput{}).Tags
	if len(list) != 1 || list[0].Tag != "socials" {
		t.Fatalf("tag_list = %+v", list)
	}
}

func TestReviseKeepsItemTags(t *testing.T) {
	f := newAgentFix(t)
	a := fileDossierMD(t, f, socialMD("Posts A"))
	must(t, TagAdd, f.owner, TagInput{Items: []ItemRef{{Proposal: a, Item: "q1"}}, Tags: []string{"socials"}})
	if _, err := ProposalRevise.Run(writeCtx(t), f.agent, ProposalReviseInput{ID: a, Markdown: socialMD("Posts A, revised")}); err != nil {
		t.Fatal(err)
	}
	if got := stackRefs(must(t, ProposalStack, f.owner, ProposalStackInput{Tag: "socials"})); !slices.Equal(got, []string{a + "#q1"}) {
		t.Fatalf("after revise, stack by tag = %v", got)
	}
}
