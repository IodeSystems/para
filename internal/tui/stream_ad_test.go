package tui

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/ops"
)

var idLike = regexp.MustCompile(`(task|project|area|rfx|person|policy|question|incident|inbox|request)-[0-9a-z]{6,}|-1m[0-9a-z]{8,}`)

// A frame shows names. No id appears on a screen of the seeded vault.
func TestNoIdsOnScreen(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	v.agentWrites()
	for _, size := range [][2]int{{120, 36}, {80, 24}, {40, 20}} {
		d := newDrv(t, v, size[0], size[1])
		for tab := 0; tab < numTabs; tab++ {
			d.key(string(rune('1' + tab)))
			if m := idLike.FindString(d.plainFrame()); m != "" {
				t.Errorf("%dx%d tab %d shows an id %q:\n%s", size[0], size[1], tab, m, d.plainFrame())
			}
		}
	}
}

// Nothing is shown twice on Today, and a section with nothing in it is not shown.
func TestTodayShowsEachItemOnce(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	ctx := context.Background()
	if _, err := ops.TaskAdd.Run(ctx, v.env.Deps, ops.TaskAddInput{Project: v.ids["project"], Title: "Late one", Due: "2020-01-01"}); err != nil {
		t.Fatal(err)
	}
	d := newDrv(t, v, 120, 60)
	text := rowText(d.m.rowsToday(120))
	for _, title := range []string{"Late one", "Write the parser", "Can we launch the portal?", "Look at the retention question"} {
		if n := strings.Count(text, title); n != 1 {
			t.Errorf("%q shown %d times:\n%s", title, n, text)
		}
	}
	if strings.Contains(text, "(0)") {
		t.Errorf("an empty section is shown:\n%s", text)
	}
	if !strings.Contains(text, "inbox 1") {
		t.Errorf("the inbox count is not shown:\n%s", text)
	}
	// Overdue comes before ready, and ready before waiting.
	o, r := strings.Index(text, "Overdue"), strings.Index(text, "Ready to work")
	if o < 0 || r < 0 || o > r {
		t.Errorf("order:\n%s", text)
	}
}

// Waiting on others lists the rows it counts, and the count includes every
// request `rfx waiting` lists.
func TestTodayWaitingRowsAndCount(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	d := newDrv(t, v, 120, 60)
	text := rowText(d.m.rowsToday(120))
	if !strings.Contains(text, "Waiting on others (1)") || !strings.Contains(text, "Can we launch the portal?") {
		t.Errorf("waiting:\n%s", text)
	}
	if got := len(d.m.snap.Waiting); got != 1 {
		t.Fatalf("rfx waiting has %d", got)
	}
	if !strings.Contains(text, "to Ada Example") {
		t.Errorf("the request does not say who it waits on:\n%s", text)
	}
}

// On a small screen the title wins, and the frame fits.
func TestNarrowScreensKeepTitles(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	for _, size := range [][2]int{{80, 24}, {40, 20}} {
		d := newDrv(t, v, size[0], size[1])
		f := d.plainFrame()
		if !strings.Contains(f, "Write the parser") {
			t.Errorf("%dx%d: title lost:\n%s", size[0], size[1], f)
		}
		if !strings.Contains(f, "? help") {
			t.Errorf("%dx%d: no help key in the footer:\n%s", size[0], size[1], f)
		}
		for i, l := range strings.Split(f, "\n") {
			if cells(l) > size[0] {
				t.Errorf("%dx%d line %d is %d wide: %q", size[0], size[1], i, cells(l), l)
			}
		}
	}
}

func TestMoreCueWhenContentContinues(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	d := newDrv(t, v, 40, 8)
	if !strings.Contains(d.plainFrame(), "↓") {
		t.Errorf("no cue that Today continues:\n%s", d.plainFrame())
	}
	d = newDrv(t, v, 120, 60)
	if strings.Contains(d.plainFrame(), "↓") {
		t.Errorf("a cue on a screen that shows everything:\n%s", d.plainFrame())
	}
}

// A row with no free letter shows its key at the left, dim, and never after the label.
func TestKeyInGutterNotAfterTheLabel(t *testing.T) {
	links := []Target{{ID: "a", Label: "Wow"}, {ID: "b", Label: "Aa"}, {ID: "c", Label: "!!!"}}
	keys := Assign(nil, links, "")
	if keys["c"].At >= 0 {
		t.Fatalf("a label with no letters has a mark at %d", keys["c"].At)
	}
	if got, ok := placeKey("!!!", keys["c"]); ok || got != "!!!" {
		t.Errorf("placeKey = %q, %v", got, ok)
	}
	v := newTestVault(t)
	v.seed()
	d := newDrv(t, v, 120, 40)
	// Whatever is on screen, no line ends in a stray " k" after its title.
	for _, l := range strings.Split(d.plainFrame(), "\n") {
		if regexp.MustCompile(`[a-z] [a-z]$`).MatchString(strings.TrimRight(l, " ")) && strings.Contains(l, "  ") && strings.HasPrefix(l, "  ") {
			if !strings.Contains(l, "help") {
				t.Errorf("a trailing key letter: %q", l)
			}
		}
	}
}

func TestStatusColoursHaveOneMeaning(t *testing.T) {
	if state("sent") == state("due") || state("sent") == state("blocked") || state("sent") == state("done") {
		t.Error("sent shares a colour with a warning, a failure, or done")
	}
	if state("sent") != stDim.Render("sent") {
		t.Error("sent is not neutral")
	}
	if state("due") != stWarn.Render("due") || state("blocked") != stBad.Render("blocked") || state("done") != stGood.Render("done") {
		t.Error("a status lost its colour")
	}
}

func TestRuleLinesInDocuments(t *testing.T) {
	src := "---\ntype: policy\n---\n\n# P\n\n## Rules\n\n- R1 policy-logging: Services log to one place. ^R1\nR2 policy-x1: Keep 30 days. ^R2\n"
	got := PreRender(src, PreOpts{Width: 80}).Markdown
	for _, bad := range []string{"policy-logging", "policy-x1", "^R1", "^R2"} {
		if strings.Contains(got, bad) {
			t.Errorf("%q in\n%s", bad, got)
		}
	}
	if !strings.Contains(got, "**R1** Services log to one place.") || !strings.Contains(got, "**R2** Keep 30 days.") {
		t.Errorf("rule labels:\n%s", got)
	}
	if strings.Contains(PreRender(src, PreOpts{Width: 80}).Markdown, "> **type**") {
		t.Error("the details strip is shown by default")
	}
	if !strings.Contains(PreRender(src, PreOpts{Width: 80, Meta: true}).Markdown, "**type** policy") {
		t.Error("the details strip does not come back")
	}
}

func TestHeadingsHaveNoMarkdownPrefix(t *testing.T) {
	var r renderers
	rd := r.render(Prepared{Markdown: "# Title\n\n## Rules\n\n### Deep\n\ntext\n"}, 60)
	if txt := stripSGR(strings.Join(rd.Lines, "\n")); strings.Contains(txt, "##") {
		t.Errorf("markdown heading marks in the render:\n%s", txt)
	}
}

// A detail page holds at least what its list row held.
func TestDetailPagesHoldTheirRows(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	ctx := context.Background()
	a, err := ops.AreaAdd.Run(ctx, v.env.Deps, ops.AreaAddInput{Title: "Home"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ops.ProjectAdd.Run(ctx, v.env.Deps, ops.ProjectAddInput{Title: "Paint the hall", Area: a.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := ops.TaskAdd.Run(ctx, v.env.Deps, ops.TaskAddInput{Area: a.ID, Title: "Water plants"}); err != nil {
		t.Fatal(err)
	}
	if _, err := ops.IncidentAdd.Run(ctx, v.env.Deps, ops.IncidentAddInput{Title: "Roof leak", Severity: model.Sev2, Area: a.ID}); err != nil {
		t.Fatal(err)
	}
	d := newDrv(t, v, 120, 40)
	d.run(d.m.loadDoc(a.ID, false))
	f := d.plainFrame()
	for _, want := range []string{"Paint the hall", "Water plants", "Roof leak", "Projects (1)", "Incidents (1)"} {
		if !strings.Contains(f, want) {
			t.Errorf("area page lacks %q:\n%s", want, f)
		}
	}
	if strings.Contains(f, "type area") || strings.Contains(f, "created") {
		t.Errorf("the details strip is shown by default:\n%s", f)
	}
	d.key(d.keyFor("act:meta"))
	if !strings.Contains(d.plainFrame(), "created") {
		t.Errorf("the details key does not show the strip:\n%s", d.plainFrame())
	}
	d.key("esc")
	d.run(d.m.loadDoc(v.ids["rfx"], false))
	f = d.plainFrame()
	for _, want := range []string{"Sent to (1)", "Ada Example", "email", "sent"} {
		if !strings.Contains(f, want) {
			t.Errorf("request page lacks %q:\n%s", want, f)
		}
	}
}

func TestPromptsSayWhatComesNext(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	d := newDrv(t, v, 100, 30)
	d.key("/")
	if !d.has("enter searches") || d.has("enter saves") {
		t.Errorf("search prompt:\n%s", d.plainFrame())
	}
	d.key("esc")
	d.key("c")
	if !d.has("enter saves it to the inbox") {
		t.Errorf("capture prompt:\n%s", d.plainFrame())
	}
}

func TestAnswerHintIsQuietAndFooterFollowsTheRow(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	ctx := context.Background()
	if _, err := ops.QuestionAdd.Run(ctx, v.env.Deps, ops.QuestionAddInput{Text: "Who signs off?", Root: true}); err != nil {
		t.Fatal(err)
	}
	d := newDrv(t, v, 120, 40)
	// The cursor starts on the first row; walk to a row that is not a question.
	if r, _ := d.m.current(); r.kind == "question" {
		d.key("j")
	}
	if d.has(" Answer ") || strings.Contains(strings.Split(d.plainFrame(), "\n")[39], "Answer") {
		t.Errorf("Answer offered off a question row:\n%s", d.plainFrame())
	}
	d.key("a")
	if d.m.statErr || !strings.Contains(d.m.status, "Move to an open question") {
		t.Errorf("status %q, error %v", d.m.status, d.m.statErr)
	}
	d.key("g") // the top: the question
	if r, _ := d.m.current(); r.kind != "question" {
		t.Fatalf("the top row is %q", r.kind)
	}
	if !strings.Contains(strings.Split(d.plainFrame(), "\n")[39], "Answer") {
		t.Errorf("Answer missing on a question row:\n%s", d.plainFrame())
	}
}

func TestGraphMarksQuestionsAndWrapsLabels(t *testing.T) {
	out := RenderDAG([]DNode{{ID: "a", Label: "? Which cutover strategy for the database"}, {ID: "b", Label: "Short"}}, []DEdge{{From: "a", To: "b"}}, nil, 60)
	if !strings.Contains(out, "? Which") {
		t.Errorf("question not marked:\n%s", out)
	}
	if strings.Contains(out, "…") && !strings.Contains(out, "strategy") {
		t.Errorf("label cut before it wrapped:\n%s", out)
	}
	if got := strings.Count(out, "│ "); got < 3 {
		t.Errorf("label is not on two lines:\n%s", out)
	}
}

func TestWrapLabelBreaksAtWords(t *testing.T) {
	got := wrapLabel("Inventory extensions used by Atlas", 20)
	if len(got) != 2 || got[0] != "Inventory extensions" || got[1] != "used by Atlas" {
		t.Errorf("wrapLabel = %q", got)
	}
}

func TestHelpListsAKeyOnce(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	d := newDrv(t, v, 120, 50)
	d.key("?")
	seen := map[string]bool{}
	section := false
	for _, l := range strings.Split(d.plainFrame(), "\n") {
		if strings.Contains(l, "On this screen") {
			section = true
			continue
		}
		f := strings.Fields(l)
		if section && len(f) > 1 && len(f[0]) == 1 {
			if seen[f[0]] {
				t.Errorf("key %q listed twice on one screen:\n%s", f[0], d.plainFrame())
			}
			seen[f[0]] = true
		}
	}
}
