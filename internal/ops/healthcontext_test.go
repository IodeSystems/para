package ops

import (
	"slices"
	"testing"
)

// An open task or question whose page says nothing beyond its title is
// flagged no_context; one with a body, a done-when, a linked page with text,
// or a parent that explains it is not.
func TestHealthNoContext(t *testing.T) {
	d, proj := laterVault(t)
	add := func(in TaskAddInput) string {
		t.Helper()
		in.Goal = proj
		return must(t, TaskAdd, d, in).ID
	}
	bare := add(TaskAddInput{Title: "Sync with infra"})
	sourceOnly := add(TaskAddInput{Title: "Vendor portal", Body: "- Source: https://tracker.acme.example/T-12\n\nImported from tracker.csv"})
	withBody := add(TaskAddInput{Title: "Renew the portal cert", Body: "The portal's TLS certificate lapses on 2026-12-01 and the vendor needs a week's notice.\n\n- Source: https://tracker.acme.example/T-13"})
	withDoneWhen := add(TaskAddInput{Title: "Budget row", DoneWhen: "The Q1 budget sheet has the portal row."})
	emptyNote := must(t, NoteAdd, d, NoteAddInput{Title: "Empty note", Goal: proj}).ID
	linksEmpty := add(TaskAddInput{Title: "Ask about it", Body: "From [[" + emptyNote + "]]"})
	note := must(t, NoteAdd, d, NoteAddInput{Title: "Kickoff notes", Goal: proj, Body: "Infra asked us to move the portal to the new cluster before the freeze."}).ID
	linksNote := add(TaskAddInput{Title: "Cluster move", Body: "From [[" + note + "]]"})
	parent := add(TaskAddInput{Title: "Portal launch prep", Body: "Everything the launch needs before the vendor review on Friday."})
	child := add(TaskAddInput{Title: "Ping Sam", Parent: parent})
	closed := add(TaskAddInput{Title: "Old thing"})
	must(t, TaskDone, d, TaskDoneInput{ID: closed})
	q := must(t, QuestionAdd, d, QuestionAddInput{Text: "What's the plan?", Goal: proj}).ID
	qCtx := must(t, QuestionAdd, d, QuestionAddInput{Text: "Which cluster?", Goal: proj, Context: "Infra runs two clusters and only one has the new ingress."}).ID

	for id, want := range map[string]bool{
		bare: true, sourceOnly: true, linksEmpty: true, q: true,
		withBody: false, withDoneWhen: false, linksNote: false, child: false, parent: false, closed: false, qCtx: false,
	} {
		if got := slices.Contains(healthKinds(t, d, id), HealthNoContext); got != want {
			t.Errorf("%s: no_context %v, want %v", id, got, want)
		}
	}
}

func TestMeaningfulText(t *testing.T) {
	for body, want := range map[string]bool{
		"":                                    false,
		"- Source: https://x.example/1":       false,
		"Imported from notes.md line 4":       false,
		"[[note-abc]]\n\n## Updates\n":        false,
		"See ticket":                          false,
		"Renew it before the vendor deadline": true,
		"## Why\n\nFinance needs the number for the board deck.": true,
	} {
		if got := meaningfulText(body); got != want {
			t.Errorf("%q: %v, want %v", body, got, want)
		}
	}
}
