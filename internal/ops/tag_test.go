package ops

import (
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
