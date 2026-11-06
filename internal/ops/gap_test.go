package ops

import (
	"slices"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

// gapVault is a vault whose tick may queue work, with extra settings.
func gapVault(t *testing.T, props ...string) Deps {
	t.Helper()
	tv := newTickVault(t, "2026-11-02T09:00:00Z", append([]string{"request.runner=ia", "request.surfaces=cli,pwa,tick"}, props...)...)
	return tv.open("ia", "cli")
}

// briefed gives a goal a brief (a rank), so it has no gap.
func briefed(t *testing.T, d Deps, id string) {
	t.Helper()
	doc, err := d.Store.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	doc.Meta.(*model.Project).Rank = 1
	if _, err := d.Store.Update(ctx, doc); err != nil {
		t.Fatal(err)
	}
}

// gapItems maps each fill-gap request to the item it links.
func gapItems(t *testing.T, d Deps, ids []string) []string {
	t.Helper()
	var out []string
	for _, id := range ids {
		_, r, err := getRequest(ctx, d.Store, id)
		if err != nil {
			t.Fatal(err)
		}
		if r.Kind != FillGapKind || r.Status != model.RequestQueued || len(r.Links) != 1 {
			t.Fatalf("%s: kind %q status %s links %v", id, r.Kind, r.Status, r.Links)
		}
		out = append(out, r.Links[0].Target())
	}
	return out
}

func TestTickGapCapAndOrder(t *testing.T) {
	d := gapVault(t, "gap.daily_cap=2")
	proj := must(t, ProjectAdd, d, ProjectAddInput{DoneWhen: "Done.", Title: "Portal"}).ID
	briefed(t, d, proj)
	late := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Overdue", Due: "2026-11-01"}).ID
	soon := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Due soon", Due: "2026-11-03"}).ID
	must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Undated"})
	full := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Complete", Due: "2026-11-01", DoneWhen: "It is.", Steps: "1. Do it.", Effort: model.EffortS}).ID

	out := tick(t, d)
	got := gapItems(t, d, out.Gaps)
	if !slices.Equal(got, []string{late, soon}) {
		t.Fatalf("gap items = %v, want the two best by value order", got)
	}
	if !slices.Equal(out.Requests, out.Gaps) {
		t.Errorf("requests %v, gaps %v", out.Requests, out.Gaps)
	}
	doc, _, _ := getRequest(ctx, d.Store, out.Gaps[0])
	if !strings.Contains(doc.Body, "Gaps: done_when, do_it, effort") || !strings.Contains(doc.Body, "Item version: sha256:") {
		t.Errorf("body:\n%s", doc.Body)
	}
	// The cap is per day: a second pass today makes none.
	if again := tick(t, d); len(again.Gaps) != 0 {
		t.Errorf("second pass made %v", again.Gaps)
	}
	// A complete item never gets one; a filled item leaves needs-you work to the owner.
	sn, err := NewSnapshot(ctx, d.Store)
	if err != nil {
		t.Fatal(err)
	}
	if !sn.Need(late).Yes {
		t.Errorf("fill-gap work took the item from the owner: %+v", sn.Need(late))
	}
	_ = full
}

func TestTickGapNoDuplicateAndFixed(t *testing.T) {
	tv := newTickVault(t, "2026-11-02T09:00:00Z", "request.runner=ia", "request.surfaces=cli,pwa,tick")
	d := tv.open("ia", "cli")
	proj := must(t, ProjectAdd, d, ProjectAddInput{DoneWhen: "Done.", Title: "Portal"}).ID
	briefed(t, d, proj)
	a := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Alpha"}).ID
	b := must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Beta"}).ID
	first := tick(t, d)
	if got := gapItems(t, d, first.Gaps); len(got) != 2 {
		t.Fatalf("first pass: %v", got)
	}
	// Open work for an item: no second request, on any day.
	tv.now = tv.now.AddDate(0, 0, 1)
	if out := tick(t, d); len(out.Gaps) != 0 {
		t.Fatalf("open work duplicated: %v", out.Gaps)
	}
	// The agent fills alpha; beta's work ends with nothing changed.
	must(t, TaskEdit, d, TaskEditInput{ID: a, DoneWhen: "Shipped.", DoURL: "https://example.com/alpha", Effort: model.EffortXS})
	for _, id := range first.Gaps {
		must(t, RequestCancel, d, RequestCancelInput{ID: id})
	}
	tv.now = tv.now.AddDate(0, 0, 1)
	if out := tick(t, d); len(out.Gaps) != 0 {
		t.Fatalf("fixed or already tried: %v", out.Gaps)
	}
	// Beta changes but still lacks effort: a new version, asked once more.
	must(t, TaskEdit, d, TaskEditInput{ID: b, DoneWhen: "Shipped."})
	out := tick(t, d)
	if got := gapItems(t, d, out.Gaps); !slices.Equal(got, []string{b}) {
		t.Fatalf("new version: %v", got)
	}
	doc, _, _ := getRequest(ctx, d.Store, out.Gaps[0])
	if !strings.Contains(doc.Body, "Gaps: do_it, effort\n") {
		t.Errorf("body:\n%s", doc.Body)
	}
}

func TestTickGapDisabledAndNotQueued(t *testing.T) {
	d := gapVault(t, "gap.enabled=false")
	proj := must(t, ProjectAdd, d, ProjectAddInput{DoneWhen: "Done.", Title: "Portal"}).ID
	must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Alpha"})
	if out := tick(t, d); len(out.Gaps) != 0 || out.NotQueued != "" {
		t.Fatalf("disabled: %+v", out)
	}

	// The default surfaces cannot queue from tick: nothing is proposed to the owner.
	tv := newTickVault(t, "2026-11-02T09:00:00Z", "request.runner=ia")
	d = tv.open("ia", "cli")
	proj = must(t, ProjectAdd, d, ProjectAddInput{DoneWhen: "Done.", Title: "Portal"}).ID
	must(t, TaskAdd, d, TaskAddInput{Project: proj, Title: "Alpha"})
	out := tick(t, d)
	if len(out.Gaps) != 0 || out.Proposed != 0 || !strings.Contains(out.NotQueued, "request.surfaces") {
		t.Fatalf("not queued: %+v", out)
	}
}

// A vault shaped unlike the owner's: no meetings, no tasks, a question and a
// goal in a household area. Gaps are questions to research and a goal brief.
func TestTickGapPersonaVault(t *testing.T) {
	d := gapVault(t)
	area := must(t, AreaAdd, d, AreaAddInput{Title: "Household", Kind: model.AreaKindProject}).ID
	goal := must(t, ProjectAdd, d, ProjectAddInput{DoneWhen: "The garden is planted.", Title: "Garden", Area: area}).ID
	research := must(t, QuestionAdd, d, QuestionAddInput{Text: "Which soil suits tomatoes?", Project: goal}).ID
	must(t, QuestionAdd, d, QuestionAddInput{Text: "Which bed first?", Project: goal, Kind: model.KindDecision, Options: []string{"north", "south"}})
	out := tick(t, d)
	got := gapItems(t, d, out.Gaps)
	if !slices.Equal(got, []string{research, goal}) {
		t.Fatalf("gap items = %v, want the question to research, then the goal", got)
	}
	doc, _, _ := getRequest(ctx, d.Store, out.Gaps[1])
	if !strings.Contains(doc.Body, "Gaps: brief") {
		t.Errorf("goal body:\n%s", doc.Body)
	}
}
