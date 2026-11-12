package ops

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/upgrade"
)

// The persona: Robin Example keeps two pools. Build-day ideas fire when an
// obligation tagged build-day is within 14 days; reading ideas fire every
// quarter from 2026-10-01. All synthetic.
const poolsYAML = `pools:
  - name: build-day
    tag: build-idea
    area: area-tinkering
    when:
      exists: {type: obligation, tag: build-day, within_days: 14}
  - name: reading
    tag: reading-idea
    pick: 2
    area: area-tinkering
    when: {date: 2026-10-01, recur: quarterly}
`

func writePools(t *testing.T, root, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, ".para", "pools.yaml"), []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

// poolFixture is the persona's vault on Friday 2026-11-06, with the pools
// file, an area, and a person.
func poolFixture(t *testing.T) (Deps, string, *time.Time) {
	t.Helper()
	old := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = old })
	now := time.Date(2026, 11, 6, 10, 0, 0, 0, time.UTC)
	d, root := sourceClockDeps(t, &now)
	if err := d.Store.SetConfig(ctx, upgrade.ConfigKey, "7"); err != nil {
		t.Fatal(err)
	}
	must(t, AreaAdd, d, AreaAddInput{Title: "Tinkering", ID: "area-tinkering"})
	must(t, OrgPersonAdd, d, OrgAddInput{Name: "Ada Example"})
	writePools(t, root, poolsYAML)
	return d, root, &now
}

func poolOf(t *testing.T, d Deps, name string) PoolView {
	t.Helper()
	out := must(t, PoolList, d, PoolListInput{Name: name})
	return out.Pools[0]
}

func rowsOf(t *testing.T, d Deps) map[string]ValueRow {
	t.Helper()
	o, err := Order(ctx, d.Store)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]ValueRow{}
	for _, r := range o.Rows {
		out[r.ID] = r
	}
	return out
}

// idea_add from an agent makes a task put off someday with the pool's tag,
// from a title and a sentence: no done_when, and it lints.
func TestIdeaAddMakesASomedayTaggedItem(t *testing.T) {
	d, _, _ := poolFixture(t)
	agent := agentDeps(d, "agent")
	out := must(t, IdeaAdd, agent, IdeaAddInput{Pool: "build-day", Title: "Sensor dashboard", Text: "Plot the garden sensors live. More later."})
	doc, err := d.Store.Get(ctx, out.ID)
	if err != nil {
		t.Fatal(err)
	}
	tk := doc.Meta.(*model.Task)
	if tk.DeferUntil != model.SomedayWord || !slices.Contains(tk.Tags, "build-idea") || tk.Area != model.LinkTo("area-tinkering") || tk.DoneWhen != "" {
		t.Fatalf("idea = %+v", tk)
	}
	if out.Tag != "build-idea" || len(out.Warnings) != 0 {
		t.Fatalf("out = %+v", out)
	}
	findings, err := d.Store.Lint(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range findings {
		if strings.Contains(f.Path, out.ID) && f.Severity == model.SeverityError {
			t.Errorf("lint: %+v", f)
		}
	}
	// A goal idea; and a pool not in the file yet takes the name as its tag.
	g := must(t, IdeaAdd, agent, IdeaAddInput{Pool: "build-day", Title: "Home robot", Kind: "goal"})
	if doc, _ := d.Store.Get(ctx, g.ID); doc.Meta.(*model.Goal).DeferUntil != model.SomedayWord {
		t.Fatalf("goal idea = %+v", doc.Meta)
	}
	n := must(t, IdeaAdd, d, IdeaAddInput{Pool: "garden", Title: "Rain barrel", Area: "area-tinkering"})
	if n.Tag != "garden" || len(n.Warnings) != 1 {
		t.Fatalf("new pool = %+v", n)
	}
	if _, err := IdeaAdd.Run(ctx, d, IdeaAddInput{Pool: "garden", Title: "No home"}); err == nil || !strings.Contains(err.Error(), "goal or an area") {
		t.Fatalf("no area: %v", err)
	}
	if _, ok := rowsOf(t, d)[out.ID]; ok {
		t.Error("an idea is a row of the order")
	}
}

// The pools file parses strictly, and only the owner writes it: no op does,
// vault_config_set refuses a pool key, and an agent cannot pick.
func TestPoolsFileParseAndOwnerOnly(t *testing.T) {
	d, root, _ := poolFixture(t)
	for text, want := range map[string]string{
		"pools:\n  - name: x\n    when: {date: 2026-01-01, closed: task-a}\n":                            "exactly one",
		"pools:\n  - name: x\n    when: {exists: {type: person, tag: a}}\n":                              "want obligation, task, or goal",
		"pools:\n  - name: x\n    when: {recur: weekly}\n":                                               "exactly one",
		"pools:\n  - name: x\n    when: {date: 2026-01-01, recur: sometimes}\n":                          "recur",
		"pools:\n  - name: x\n    when: {date: 2026-01-01}\n  - name: X\n    when: {date: 2026-01-02}\n": "used twice",
		"pools:\n  - name: x\n    colour: red\n    when: {date: 2026-01-01}\n":                           "colour",
		"pools:\n  - name: \"a b!\"\n    when: {date: 2026-01-01}\n":                                     "plain word",
	} {
		writePools(t, root, text)
		if _, err := PoolList.Run(ctx, d, PoolListInput{}); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: %v, want %q", text, err, want)
		}
		// A bad file gives no rows; it never fails the order.
		if _, err := Order(ctx, d.Store); err != nil {
			t.Errorf("order with a bad pools file: %v", err)
		}
	}
	writePools(t, root, poolsYAML)
	if _, err := VaultConfigSet.Run(ctx, d, VaultConfigSetInput{Key: "pool.reading.rounds", Value: "2026-10-01"}); err == nil {
		t.Error("vault_config_set wrote a pool's rounds")
	}
	if _, err := PoolPick.Run(ctx, agentDeps(d, "agent"), PoolPickInput{Pool: "reading", Skip: true}); !errors.Is(err, ErrPersonOnly) {
		t.Errorf("agent pick: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(root, ".para", "pools.yaml"))
	if err != nil || string(b) != poolsYAML {
		t.Errorf("pools.yaml changed: %v", err)
	}
}

// Each trigger kind fires and does not.
func TestPoolTriggers(t *testing.T) {
	d, root, now := poolFixture(t)
	idea := func(pool, title string) string {
		return must(t, IdeaAdd, d, IdeaAddInput{Pool: pool, Title: title}).ID
	}
	idea("build-day", "Sensor dashboard")
	idea("reading", "Read about compilers")
	// exists: no obligation yet; one 20 days out does not fire, and says when.
	if p := poolOf(t, d, "build-day"); p.Fires {
		t.Fatalf("build-day fires with no obligation: %+v", p)
	}
	far := must(t, ObligationAdd, d, ObligationAddInput{Title: "Build day in spring", OwedTo: []string{"person-ada-example"}, Due: "2026-11-26", Area: "area-tinkering"}).ID
	must(t, TagAdd, d, TagInput{IDs: []string{far}, Tags: []string{"build-day"}})
	if p := poolOf(t, d, "build-day"); p.Fires || p.Next != "2026-11-12" {
		t.Fatalf("far obligation: %+v", p)
	}
	*now = time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)
	if p := poolOf(t, d, "build-day"); !p.Fires || p.TriggerID != far || p.Title != "Build day in spring: pick 1 of 1 idea" {
		t.Fatalf("within 14 days: %+v", p)
	}
	// date with recur: the quarter's instance is 2026-10-01; the next 2027-01-01.
	if p := poolOf(t, d, "reading"); !p.Fires || p.Instance != "2026-10-01" || p.Next != "2027-01-01" {
		t.Fatalf("reading: %+v", p)
	}
	// date with no recur, before and on it.
	writePools(t, root, poolsYAML+"  - name: later\n    tag: reading-idea\n    when: {date: 2026-12-01}\n")
	if p := poolOf(t, d, "later"); p.Fires || p.Next != "2026-12-01" {
		t.Fatalf("before the date: %+v", p)
	}
	// closed: fires when the item closes.
	task := must(t, TaskAdd, d, TaskAddInput{Title: "Finish the shed", Area: "area-tinkering"}).ID
	writePools(t, root, poolsYAML+"  - name: after-shed\n    tag: build-idea\n    when: {closed: "+task+"}\n")
	if p := poolOf(t, d, "after-shed"); p.Fires {
		t.Fatalf("open item fires: %+v", p)
	}
	must(t, TaskDone, d, TaskDoneInput{ID: task})
	if p := poolOf(t, d, "after-shed"); !p.Fires || p.Instance != task {
		t.Fatalf("closed item: %+v", p)
	}
	writePools(t, root, poolsYAML+"  - name: gone\n    when: {closed: task-none}\n")
	if p := poolOf(t, d, "gone"); p.Fires || p.Problem == "" {
		t.Fatalf("missing item: %+v", p)
	}
	*now = time.Date(2026, 12, 2, 10, 0, 0, 0, time.UTC)
	writePools(t, root, poolsYAML+"  - name: later\n    tag: reading-idea\n    when: {date: 2026-12-01}\n")
	if p := poolOf(t, d, "later"); !p.Fires || p.Instance != "2026-12-01" {
		t.Fatalf("after the date: %+v", p)
	}
}

// A firing pool is one row of the order and one agenda line; its ideas are
// neither. The row is prep for the obligation that fired it.
func TestFiringPoolIsOneRow(t *testing.T) {
	d, _, _ := poolFixture(t)
	ids := []string{
		must(t, IdeaAdd, d, IdeaAddInput{Pool: "build-day", Title: "Sensor dashboard"}).ID,
		must(t, IdeaAdd, d, IdeaAddInput{Pool: "build-day", Title: "Door chime"}).ID,
		must(t, IdeaAdd, d, IdeaAddInput{Pool: "reading", Title: "Read about compilers"}).ID,
	}
	ob := must(t, ObligationAdd, d, ObligationAddInput{Title: "Build day", OwedTo: []string{"person-ada-example"}, Due: "2026-11-10", Area: "area-tinkering"}).ID
	must(t, TagAdd, d, TagInput{IDs: []string{ob}, Tags: []string{"build-day"}})
	rows := rowsOf(t, d)
	for _, id := range ids {
		if _, ok := rows[id]; ok {
			t.Errorf("idea %s is a row", id)
		}
	}
	r, ok := rows["pool:build-day"]
	if !ok || r.Kind != KindPool || !r.NeedsYou || r.Reason != "pick from pool build-day" || r.Title != "Build day: pick 1 of 2 ideas" {
		t.Fatalf("build-day row = %+v (rows %v)", r, rows)
	}
	if r.Group != GroupOwed || r.For != ob {
		t.Errorf("build-day row is not prep of the obligation: %+v", r)
	}
	if r, ok := rows["pool:reading"]; !ok || r.Title != "reading: pick 1 of 1 idea" {
		t.Errorf("reading row = %+v", r)
	}
	ag := must(t, Agenda, d, AgendaInput{Days: 7})
	n := 0
	for _, it := range ag.Items {
		if it.Kind == AgendaPool {
			n++
		}
		if slices.Contains(ids, it.ID) {
			t.Errorf("idea %s on the agenda", it.ID)
		}
	}
	if n != 2 {
		t.Errorf("agenda pool lines = %d, want 2: %+v", n, ag.Items)
	}
}

// Ideas rank by the value order's score: less effort and more unblocked
// first.
func TestPoolIdeasRankByScore(t *testing.T) {
	d, _, _ := poolFixture(t)
	big := must(t, IdeaAdd, d, IdeaAddInput{Pool: "reading", Title: "Read a whole textbook", Effort: model.EffortL}).ID
	small := must(t, IdeaAdd, d, IdeaAddInput{Pool: "reading", Title: "Read one paper", Effort: model.EffortXS}).ID
	mid := must(t, IdeaAdd, d, IdeaAddInput{Pool: "reading", Title: "Read a long essay", Effort: model.EffortS}).ID
	// mid unblocks two tasks: (2+1)/2 = 1.5 beats small's (0+1)/1 = 1.
	must(t, TaskAdd, d, TaskAddInput{Title: "Write notes", Area: "area-tinkering", DependsOn: []string{mid}})
	must(t, TaskAdd, d, TaskAddInput{Title: "Share the notes", Area: "area-tinkering", DependsOn: []string{mid}})
	p := poolOf(t, d, "reading")
	var got []string
	for _, it := range p.Ideas {
		got = append(got, it.ID)
	}
	if !slices.Equal(got, []string{mid, small, big}) {
		t.Fatalf("order = %v, want %v", got, []string{mid, small, big})
	}
	if p.Ideas[0].Score != 1.5 || p.Ideas[0].Impact != 2 || p.Ideas[1].Score != 1 || p.Ideas[2].Score != 0.13 {
		t.Errorf("scores = %+v", p.Ideas)
	}
}

// Pick brings the picks back and makes a task prep of the obligation; the
// rest stay put off; the card leaves for that obligation, and the next
// obligation fires again. Skip passes a round the same way.
func TestPoolPickAndSkip(t *testing.T) {
	d, _, now := poolFixture(t)
	a := must(t, IdeaAdd, d, IdeaAddInput{Pool: "build-day", Title: "Sensor dashboard"}).ID
	b := must(t, IdeaAdd, d, IdeaAddInput{Pool: "build-day", Title: "Door chime"}).ID
	ob := must(t, ObligationAdd, d, ObligationAddInput{Title: "Build day", OwedTo: []string{"person-ada-example"}, Due: "2026-11-10", Area: "area-tinkering"}).ID
	must(t, TagAdd, d, TagInput{IDs: []string{ob}, Tags: []string{"build-day"}})
	if _, err := PoolPick.Run(ctx, d, PoolPickInput{Pool: "build-day", IDs: []string{"task-other"}}); err == nil {
		t.Error("picked an item that is not an idea")
	}
	out := must(t, PoolPick, d, PoolPickInput{Pool: "build-day", IDs: []string{a}})
	if out.Instance != ob || !slices.Equal(out.Picked, []string{a}) || !slices.Equal(out.Linked, []string{a}) {
		t.Fatalf("pick = %+v", out)
	}
	doc, _ := d.Store.Get(ctx, a)
	if tk := doc.Meta.(*model.Task); tk.DeferUntil != "" || tk.Parent != model.LinkTo(ob) {
		t.Fatalf("picked = %+v", tk)
	}
	if doc, _ := d.Store.Get(ctx, b); doc.Meta.(*model.Task).DeferUntil != model.SomedayWord {
		t.Error("the rest came back")
	}
	rows := rowsOf(t, d)
	if _, ok := rows["pool:build-day"]; ok {
		t.Error("the card stays after a pick")
	}
	if _, ok := rows[a]; !ok {
		t.Error("the pick is not in the order")
	}
	// The hub lists the pick as prep.
	parts := must(t, ObligationParts, d, ObligationPartsInput{ID: ob})
	if !slices.Contains(partIDs(parts.Before), a) {
		t.Errorf("hub prep = %v", partIDs(parts.Before))
	}
	// The next build day fires again; skip passes it.
	must(t, Close, d, CloseInput{ID: ob, Reason: "Held."})
	next := must(t, ObligationAdd, d, ObligationAddInput{Title: "Build day two", OwedTo: []string{"person-ada-example"}, Due: "2026-11-15", Area: "area-tinkering"}).ID
	must(t, TagAdd, d, TagInput{IDs: []string{next}, Tags: []string{"build-day"}})
	if r, ok := rowsOf(t, d)["pool:build-day"]; !ok || r.Title != "Build day two: pick 1 of 1 idea" {
		t.Fatalf("next instance: %+v", r)
	}
	if out := must(t, PoolPick, d, PoolPickInput{Pool: "build-day", Skip: true}); out.Instance != next || !out.Skipped {
		t.Fatalf("skip = %+v", out)
	}
	if _, ok := rowsOf(t, d)["pool:build-day"]; ok {
		t.Error("the card stays after a skip")
	}
	if doc, _ := d.Store.Get(ctx, b); doc.Meta.(*model.Task).DeferUntil != model.SomedayWord {
		t.Error("skip brought an idea back")
	}
	// A date pool: skip this quarter, and the next quarter fires again.
	must(t, IdeaAdd, d, IdeaAddInput{Pool: "reading", Title: "Read about compilers"})
	must(t, PoolPick, d, PoolPickInput{Pool: "reading", Skip: true})
	if _, ok := rowsOf(t, d)["pool:reading"]; ok {
		t.Error("reading shows after skip")
	}
	*now = time.Date(2027, 1, 2, 10, 0, 0, 0, time.UTC)
	if p := poolOf(t, d, "reading"); !p.Fires || p.Instance != "2027-01-01" {
		t.Fatalf("next quarter: %+v", p)
	}
}

// pool_run with dry_run lists the firing pools and makes no work; without,
// it asks an agent to fill in the ideas.
func TestPoolRunDryRun(t *testing.T) {
	d, _, _ := poolFixture(t)
	id := must(t, IdeaAdd, d, IdeaAddInput{Pool: "reading", Title: "Read about compilers"}).ID
	// The tick's gap work reads the pool's row as its ideas.
	sn, err := NewSnapshot(ctx, d.Store)
	if err != nil {
		t.Fatal(err)
	}
	cands, err := gapCandidates(sn)
	if err != nil || !slices.Contains(cands, id) || slices.Contains(cands, "pool:reading") {
		t.Fatalf("gap candidates = %v, %v", cands, err)
	}
	dry := must(t, PoolRun, d, PoolRunInput{DryRun: true})
	if len(dry.Pools) != 1 || dry.Pools[0].Name != "reading" || len(dry.Gaps) != 0 {
		t.Fatalf("dry run = %+v", dry)
	}
	run := must(t, PoolRun, d, PoolRunInput{})
	if len(run.Gaps) != 1 {
		t.Fatalf("run = %+v", run)
	}
	again := must(t, PoolRun, d, PoolRunInput{})
	if len(again.Gaps) != 0 {
		t.Fatalf("second run asks again: %+v", again)
	}
}

// Undoing a pool_pick receipt brings the ideas back and takes the round
// back, so the card returns for that instance.
func TestPoolPickUndoRestoresRound(t *testing.T) {
	d, _, _ := poolFixture(t)
	a := must(t, IdeaAdd, d, IdeaAddInput{Pool: "build-day", Title: "Sensor dashboard"}).ID
	ob := must(t, ObligationAdd, d, ObligationAddInput{Title: "Build day", OwedTo: []string{"person-ada-example"}, Due: "2026-11-10", Area: "area-tinkering"}).ID
	must(t, TagAdd, d, TagInput{IDs: []string{ob}, Tags: []string{"build-day"}})
	must(t, PoolPick, d, PoolPickInput{Pool: "build-day", IDs: []string{a}})
	if _, ok := rowsOf(t, d)["pool:build-day"]; ok {
		t.Fatal("the card stays after a pick")
	}
	rl := must(t, ReceiptList, d, ReceiptListInput{Op: "pool_pick"})
	if len(rl.Items) != 1 {
		t.Fatalf("receipts = %+v", rl.Items)
	}
	must(t, ReceiptRevert, d, ReceiptIDInput{ID: rl.Items[0].ID})
	if doc, _ := d.Store.Get(ctx, a); doc.Meta.(*model.Task).DeferUntil != model.SomedayWord {
		t.Error("the idea did not go back")
	}
	if r, ok := rowsOf(t, d)["pool:build-day"]; !ok || r.Kind != KindPool {
		t.Errorf("the card did not return: %+v", r)
	}
}
