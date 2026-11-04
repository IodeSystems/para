package model

import (
	"strings"
	"testing"
)

const sampleDossier = "Intro text with a [link](https://acme.example).\n\n" +
	"```yaml para-dossier\ntldr: Pick a vendor and tell Jordan\n```\n\n" +
	"## Findings\n\nVendor B is cheaper.\n\n" +
	"```yaml para-question id=q1\n# hand comment kept\ntitle: Which vendor?\noptions: [A, B, C]\nrecommended: B\nevidence: {quote: \"B costs less\", locator: \"https://acme.example/q\"}\n```\n\n" +
	"```js\nconst x = \"```yaml para-change id=zz\"\n```\n\n" +
	"## Changes\n\n" +
	"```yaml para-change id=c1\nop: task_add\ninput: {title: Order nodes}\nhow: add a task\nimpact: one more task\nevidence: {quote: q, locator: l}\nafter: q1\n```\n\n" +
	"````yaml para-message id=m1\npeople: [\"[[jordan-example]]\"]\nbody: |\n  Hi Jordan,\n  ```\n  code\n  ```\nevidence: {quote: q, locator: l}\n````\n"

func TestDossierRoundTripKeepsBytes(t *testing.T) {
	d, err := ParseDossier(sampleDossier)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Items) != 3 || d.Items[0].Kind != ItemQuestion || d.Items[1].Kind != ItemPara || d.Items[2].Kind != ItemMessage {
		t.Fatalf("items: %+v", d.Items)
	}
	if d.Items[0].Section != "findings" || d.Items[1].Section != "changes" {
		t.Fatalf("sections: %q %q", d.Items[0].Section, d.Items[1].Section)
	}
	if !strings.Contains(d.Items[2].Body, "```\n  code") && !strings.Contains(d.Items[2].Body, "```\ncode") {
		t.Fatalf("body: %q", d.Items[2].Body)
	}
	got, err := d.Render()
	if err != nil {
		t.Fatal(err)
	}
	if got != strings.TrimRight(sampleDossier, "\n") {
		t.Fatalf("render changed bytes:\n%s", got)
	}
	// A changed block is rewritten; the others stay as written.
	d.Items[0].Answer = &ItemAnswer{Choices: []string{"B"}, Via: ViaPWA}
	d.Items[0].State = ItemPending
	got, err = d.Render()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "# hand comment kept") {
		t.Fatal("changed block kept its old text")
	}
	if !strings.Contains(got, "op: task_add\ninput: {title: Order nodes}") {
		t.Fatalf("unchanged block rewritten:\n%s", got)
	}
	again, err := ParseDossier(got)
	if err != nil {
		t.Fatalf("%v\n%s", err, got)
	}
	if a := again.Items[0].Answer; a == nil || len(a.Choices) != 1 || a.Choices[0] != "B" {
		t.Fatalf("answer: %+v", a)
	}
	if again.Items[2].Body != d.Items[2].Body {
		t.Fatalf("body changed: %q", again.Items[2].Body)
	}
}

func TestDossierHashIgnoresOwnerAndParaFields(t *testing.T) {
	d, err := ParseDossier(sampleDossier)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range d.Items {
		before, err := it.Hash()
		if err != nil {
			t.Fatal(err)
		}
		it.Answer = &ItemAnswer{Comment: "x"}
		it.State, it.Execution, it.Thread = ItemDone, &Execution{Result: "done"}, []Turn{{ID: "t1", Text: "y"}}
		it.Rev, it.ClaimedBy = 4, "claude"
		after, _ := it.Hash()
		if before != after {
			t.Fatalf("%s: hash moved with para or owner fields", it.ID)
		}
	}
}

func TestDossierLintCodes(t *testing.T) {
	head := "```yaml para-dossier\ntldr: t\n```\n"
	ev := "evidence: {quote: q, locator: l}\n"
	cases := map[string]struct{ body, code string }{
		"bad info":       {head + "```yaml para-thing id=x\n" + ev + "```\n", CodeDossierBlock},
		"unknown key":    {head + "```yaml para-action id=a1\nwhat: w\nhow: h\nzap: 1\n" + ev + "```\n", CodeDossierBlock},
		"missing key":    {head + "```yaml para-message id=m1\nbody: b\n" + ev + "```\n", CodeDossierBlock},
		"no evidence":    {head + "```yaml para-action id=a1\nwhat: w\nhow: h\n```\n", CodeDossierBlock},
		"no header":      {"```yaml para-action id=a1\nwhat: w\nhow: h\n" + ev + "```\n", CodeDossierBlock},
		"no id":          {head + "```yaml para-action\nwhat: w\nhow: h\n" + ev + "```\n", CodeDossierID},
		"dup id":         {head + "```yaml para-action id=a1\nwhat: w\nhow: h\n" + ev + "```\n```yaml para-action id=a1\nwhat: w\nhow: h\n" + ev + "```\n", CodeDossierID},
		"dup section":    {head + "## A\n## a\n", CodeDossierID},
		"after missing":  {head + "```yaml para-action id=a1\nwhat: w\nhow: h\nafter: zz\n" + ev + "```\n", CodeDossierRef},
		"after cycle":    {head + "```yaml para-action id=a1\nwhat: w\nhow: h\nafter: a2\n" + ev + "```\n```yaml para-action id=a2\nwhat: w\nhow: h\nafter: a1\n" + ev + "```\n", CodeDossierRef},
		"lone choice":    {head + "```yaml para-action id=a1\nwhat: w\nhow: h\nchoice: x\n" + ev + "```\n", CodeDossierRef},
		"choice unknown": {head + "```yaml para-question id=q1\ntitle: t\noptions: [A]\n" + ev + "answer: {choices: [Z]}\n```\n", CodeDossierAnswer},
		"two choices":    {head + "```yaml para-question id=q1\ntitle: t\noptions: [A, B]\n" + ev + "answer: {choices: [A, B]}\n```\n", CodeDossierAnswer},
		"wrong keys":     {head + "```yaml para-action id=a1\nwhat: w\nhow: h\n" + ev + "answer: {choices: [A]}\n```\n", CodeDossierAnswer},
		"bad state":      {head + "```yaml para-action id=a1\nwhat: w\nhow: h\nstate: lost\n" + ev + "```\n", CodeDossierState},
		"not closed":     {head + "```yaml para-action id=a1\nwhat: w\n", CodeDossierBlock},
	}
	for name, c := range cases {
		found := LintDossier(c.body)
		ok := false
		for _, f := range found {
			ok = ok || f.Code == c.code
		}
		if !ok {
			t.Errorf("%s: want %s, got %v", name, c.code, found)
		}
	}
	multi := head + "```yaml para-question id=q1\ntitle: t\nmulti: true\noptions: [A, B]\n" + ev + "answer: {choices: [A, D], added_options: [D], via: file}\n```\n"
	if found := LintDossier(multi); len(found) > 0 {
		t.Fatalf("clean multi answer: %v", found)
	}
}

// A hand-edited answer in a file parses as a draft that says via file.
func TestDossierHandEditedAnswer(t *testing.T) {
	body := strings.Replace(sampleDossier, "recommended: B\n", "recommended: B\nanswer:\n  choices: [C]\n  via: file\n", 1)
	d, err := ParseDossier(body)
	if err != nil {
		t.Fatal(err)
	}
	if a := d.Items[0].Answer; a == nil || a.Via != ViaFile || a.Choices[0] != "C" {
		t.Fatalf("answer: %+v", a)
	}
	c, err := ParseProposalBody(body)
	if err != nil || len(c.Items) != 3 || c.TLDR != "Pick a vendor and tell Jordan" {
		t.Fatalf("content: %+v %v", c, err)
	}
}

func TestNewDossierRenders(t *testing.T) {
	items := []ProposalItem{{ID: "i1", Kind: ItemAction, What: "w", How: "h", Evidence: Evidence{Quote: "\nq", Locator: "l"}, State: ItemPending, Rev: 1}}
	d := NewDossier(DossierHeader{TLDR: "t", Context: "c"}, "Items", items)
	body, err := d.Render()
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseDossier(body)
	if err != nil {
		t.Fatalf("%v\n%s", err, body)
	}
	if back.Items[0].Evidence.Quote != "\nq" || back.Header.Context != "c" || back.Items[0].Section != "items" {
		t.Fatalf("back: %+v\n%s", back.Items[0], body)
	}
}
