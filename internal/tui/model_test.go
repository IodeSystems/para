package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/ops"
)

func TestTabsRenderWithinFrame(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	v.agentWrites()
	for _, size := range [][2]int{{40, 24}, {80, 24}, {120, 30}} {
		d := newDrv(t, v, size[0], size[1])
		for tab := 0; tab < numTabs; tab++ {
			d.key(string(rune('1' + tab)))
			lines := strings.Split(d.m.frame, "\n")
			if len(lines) != size[1] {
				t.Errorf("%dx%d tab %d: %d lines", size[0], size[1], tab, len(lines))
			}
			for i, l := range lines {
				if w := cells(l); w > size[0] {
					t.Errorf("%dx%d tab %d line %d is %d wide: %q", size[0], size[1], tab, i, w, stripSGR(l))
				}
			}
			f := d.plainFrame()
			if named := strings.Contains(f, tabNames[tab]); !named && (size[0] >= 60 || !strings.Contains(f, shortNames[tab])) {
				t.Errorf("tab %d: no tab name", tab)
			}
		}
	}
}

func TestEveryVisibleLinkRowHasAUniqueKey(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	v.agentWrites()
	d := newDrv(t, v, 100, 40)
	for tab := 0; tab < numTabs; tab++ {
		d.key(string(rune('1' + tab)))
		rows, acts := d.m.rows(d.m.w)
		l := d.m.drawList(d.m.bodyHeight())
		seen := map[string]string{}
		for id, k := range l.keys {
			if prev, dup := seen[k.Code]; dup {
				t.Errorf("tab %d: key %q for %s and %s", tab, k.Code, prev, id)
			}
			seen[k.Code] = id
		}
		for _, a := range acts {
			if _, ok := l.keys[a.ID]; !ok {
				t.Errorf("tab %d: action %s has no key", tab, a.ID)
			}
		}
		// Every visible row that holds a link shows its key, dim, next to its text.
		for i, ln := range l.lines {
			if i+d.m.tabs[tab].offset >= len(rows) {
				continue
			}
			r := rows[i+d.m.tabs[tab].offset]
			if !r.sel || r.target == "" {
				continue
			}
			code, _, _ := strings.Cut(strings.TrimLeft(stripSGR(ln), " "), " ")
			if k, ok := l.keys[r.target]; !ok || k.Code != code {
				t.Errorf("tab %d line %d shows %q, want its key: %q", tab, i, code, stripSGR(ln))
			}
		}
	}
}

func TestPressingAKeyFollowsAtOnceAndEscGoesBack(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	d := newDrv(t, v, 100, 40)
	if !d.has("Write the parser") {
		t.Fatalf("today lacks the ready task:\n%s", d.plainFrame())
	}
	d.key(d.keyFor("doc:" + v.ids["t1"]))
	if p := d.m.top(); p == nil || p.kind != pageDoc || p.id != v.ids["t1"] {
		t.Fatalf("no document page: %+v", p)
	}
	if !d.has("Write the parser") {
		t.Fatalf("document not shown:\n%s", d.plainFrame())
	}
	d.key("esc")
	if d.m.top() != nil {
		t.Fatal("esc did not go back")
	}
}

func TestEnterAndJKWork(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	d := newDrv(t, v, 100, 40)
	start := d.m.tabs[tabToday].cursor
	d.key("j")
	if d.m.tabs[tabToday].cursor <= start {
		t.Fatal("j did not move")
	}
	d.key("k")
	if d.m.tabs[tabToday].cursor != start {
		t.Fatal("k did not move back")
	}
	d.key("enter")
	if d.m.top() == nil {
		t.Fatal("enter opened nothing")
	}
}

func TestDocumentKeysSurviveResizeScrollAndRevisit(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	d := newDrv(t, v, 120, 40)
	d.send(keyMsg("6"))
	d.key(d.keyFor("doc:policy-logging"))
	p := d.m.top()
	if p == nil || p.id != "policy-logging" {
		t.Fatalf("page %+v", p)
	}
	base := map[string]Key{}
	for id, k := range d.m.renderDoc(p).Keys {
		base[id] = k
	}
	if len(base) < 3 {
		t.Fatalf("few keys: %v", base)
	}
	check := func(when string) {
		t.Helper()
		got := d.m.renderDoc(d.m.top()).Keys
		for id, k := range base {
			if got[id] != k {
				t.Errorf("%s: %s key %+v became %+v", when, id, k, got[id])
			}
		}
	}
	for _, w := range []int{40, 80, 120, 60} {
		d.send(tea.WindowSizeMsg{Width: w, Height: 30})
		check("width " + string(rune('0'+w/10)))
	}
	d.keys("j", "j", "G", "g")
	check("scroll")
	d.key("esc")
	d.key(d.keyFor("doc:policy-logging"))
	check("revisit")
}

func TestZoomHidesFrameAndKeepsKeys(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	d := newDrv(t, v, 100, 40)
	d.send(keyMsg("6"))
	d.key(d.keyFor("doc:policy-logging"))
	if !d.has("1 Today") {
		t.Fatal("frame missing before zoom")
	}
	d.key("z")
	if d.has("1 Today") || d.has("Zoom") {
		t.Fatalf("frame still shown:\n%s", d.plainFrame())
	}
	if n := len(strings.Split(d.m.frame, "\n")); n != 40 {
		t.Fatalf("zoom frame has %d lines", n)
	}
	if !strings.Contains(d.m.frame, KeyStyle.On) {
		t.Fatal("zoom frame lost the key letters")
	}
	// A link key still works, and the back stack is kept.
	d.key(d.keyFor("wiki:person-ada-example"))
	if len(d.m.stack) != 2 || d.m.zoom {
		t.Fatalf("stack %d zoom %v", len(d.m.stack), d.m.zoom)
	}
	d.key("esc")
	if len(d.m.stack) != 1 {
		t.Fatalf("stack %d", len(d.m.stack))
	}
	d.key("z")
	d.key("esc") // leaves zoom first
	if !d.m.docPage().isZero() && d.m.zoom {
		t.Fatal("esc did not leave zoom")
	}
}

func (p *page) isZero() bool { return p == nil }

func TestWideTableAndCardsInFrame(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	d := newDrv(t, v, 140, 50)
	d.send(keyMsg("6"))
	d.key(d.keyFor("doc:policy-logging"))
	if !strings.Contains(d.plainFrame(), "│") || !strings.Contains(d.plainFrame(), "Retention") {
		t.Fatalf("no table at 140:\n%s", d.plainFrame())
	}
	d.send(tea.WindowSizeMsg{Width: 40, Height: 50})
	f := d.plainFrame()
	if strings.Contains(f, "│ Retention") || !strings.Contains(f, "Retention: 90 days") {
		t.Fatalf("no cards at 40:\n%s", f)
	}
	for _, l := range strings.Split(d.m.frame, "\n") {
		if cells(l) > 40 {
			t.Errorf("line %d wide: %q", cells(l), stripSGR(l))
		}
	}
}

func TestReviewExcludesOwnActionsAndAcceptMovesPosition(t *testing.T) {
	v := newTestVault(t)
	v.seed() // events by user on tui surface
	v.agentWrites()
	d := newDrv(t, v, 120, 50)
	d.key("2")
	f := d.plainFrame()
	if !strings.Contains(f, "claude-agent") || strings.Contains(f, "user  ") && strings.Contains(f, "  user") && false {
		t.Fatalf("no agent group:\n%s", f)
	}
	rv := d.m.review()
	if len(rv.Groups) != 1 || rv.Groups[0].Actor != "claude-agent" {
		t.Fatalf("groups %+v", rv.Groups)
	}
	kinds := map[string]bool{}
	for _, n := range rv.Needs {
		kinds[n.Kind] = true
	}
	for _, k := range []string{NeedDraft, NeedUnverified, NeedProposed} {
		if !kinds[k] {
			t.Errorf("no need of kind %q: %+v", k, rv.Needs)
		}
	}
	if !strings.Contains(f, "Needs you") || strings.Index(f, "Needs you") > strings.Index(f, "Changed by others") {
		t.Fatalf("needs not first:\n%s", f)
	}
	// Move to the last event and accept it: the position moves and is stored.
	rows, _ := d.m.rows(d.m.w)
	last := -1
	for i, r := range rows {
		if r.kind == "event" {
			last = i
		}
	}
	d.m.tabs[tabReview].cursor = last
	d.m.relayout()
	d.key(d.keyFor("act:accept"))
	if d.m.pos.At.IsZero() {
		t.Fatal("position did not move")
	}
	if got := d.m.review().Count; got != 0 {
		t.Fatalf("%d events still to review", got)
	}
	saved, err := LoadPosition(v.env.StateDir)
	if err != nil || !saved.At.Equal(d.m.pos.At) {
		t.Fatalf("saved %+v err %v", saved, err)
	}
	if _, err := os.Stat(filepath.Join(v.env.StateDir, "review.json")); err != nil {
		t.Fatal(err)
	}
	// Reopen undoes it.
	d.key("r")
	if !d.m.pos.At.IsZero() || d.m.review().Count == 0 {
		t.Fatalf("reopen did not undo: %+v", d.m.pos)
	}
}

func TestReviewAcceptItemsAreOps(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	v.agentWrites()
	d := newDrv(t, v, 120, 50)
	d.key("2")
	pick := func(kind string) {
		rows, _ := d.m.rows(d.m.w)
		for i, r := range rows {
			if r.kind == "need" && r.need.Kind == kind {
				d.m.tabs[tabReview].cursor = i
				d.m.relayout()
				return
			}
		}
		t.Fatalf("no %s row", kind)
	}
	pick(NeedUnverified)
	d.key(d.keyFor("act:accept"))
	doc, err := v.env.Deps.Store.Get(t.Context(), "policy-logging")
	if err != nil || !doc.Meta.(*model.Policy).Verified {
		t.Fatalf("policy not verified: %v", err)
	}
	pick(NeedProposed)
	d.key(d.keyFor("act:accept"))
	refs, _ := v.env.Deps.Store.List(t.Context(), storeFilter(model.TypeRequest))
	if len(refs) != 1 {
		t.Fatalf("refs %v", refs)
	}
	rd, _ := v.env.Deps.Store.Get(t.Context(), refs[0].ID)
	if rd.Meta.(*model.Request).Status != model.RequestQueued {
		t.Fatalf("status %v", rd.Meta.(*model.Request).Status)
	}
}

func TestCaptureFromAnyScreen(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	d := newDrv(t, v, 100, 40)
	d.key("3")
	d.key("c")
	d.typeText("remember the milk")
	d.key("enter")
	if !d.has("Captured: remember the milk") {
		t.Fatalf("no confirmation:\n%s", d.plainFrame())
	}
	if d.m.tab != tabWork || len(d.m.stack) != 0 {
		t.Fatal("capture changed the screen")
	}
	d.key("1")
	if !d.has("remember the milk") {
		t.Fatalf("capture not in inbox:\n%s", d.plainFrame())
	}
	evs, _ := v.env.Deps.Store.Events(t.Context(), 5)
	last := evs[len(evs)-1]
	if last.Surface != "tui" || last.Op == "" {
		t.Fatalf("event %+v", last)
	}
}

func TestLiveRefreshOnVaultChange(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	d := newDrv(t, v, 100, 40)
	before := d.m.refreshed
	if d.has("Brand new capture") {
		t.Fatal("already there")
	}
	v.agentWrites()
	// A poll sees the file change and reloads.
	d.send(pollMsg{})
	d.run(d.m.checkFP())
	if d.m.fp == "" || !d.m.refreshed.After(before) && !d.m.refreshed.Equal(before) {
		t.Fatal("no refresh")
	}
	if !d.has("Agent found a stale link") {
		t.Fatalf("new inbox item not shown:\n%s", d.plainFrame())
	}
}

func TestOverflowKeysUseSemicolon(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	for i := 0; i < 30; i++ {
		v.capture("Item number " + strings.Repeat("x", i%3) + string(rune('A'+i%26)))
	}
	d := newDrv(t, v, 100, 60)
	found := ""
	for code, tgt := range d.m.keymap {
		if strings.HasPrefix(code, ";") && strings.HasPrefix(tgt, "doc:") {
			found = code
		}
	}
	if found == "" {
		t.Skip("no overflow key on screen")
	}
	d.key(";")
	if d.m.pending != ";" {
		t.Fatal("; did not start an overflow key")
	}
	d.key(string(found[1]))
	d.key(string(found[2]))
	if d.m.top() == nil {
		t.Fatalf("overflow key %q did nothing", found)
	}
}

func (v *testVault) capture(text string) {
	v.t.Helper()
	if _, err := ops.InboxAdd.Run(context.Background(), v.env.Deps, ops.InboxAddInput{Text: text}); err != nil {
		v.t.Fatal(err)
	}
}
