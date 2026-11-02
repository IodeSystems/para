package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/store"
)

// longWords is n characters of numbered words, so a missing piece shows.
func longWords(n int) string {
	var b strings.Builder
	for i := 0; b.Len() < n; i++ {
		fmt.Fprintf(&b, "w%03d ", i)
	}
	return strings.TrimSpace(b.String()[:n])
}

func squash(s string) string { return strings.Join(strings.Fields(s), "") }

func inboxBodies(t *testing.T, v *testVault) []string {
	t.Helper()
	refs, err := v.env.Deps.Store.List(t.Context(), store.Filter{Folder: model.FolderInbox})
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, r := range refs {
		d, err := v.env.Deps.Store.Get(t.Context(), r.ID)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, d.Title+"\n"+d.Body)
	}
	return out
}

// A 300 character entry is all on screen, wrapped, and saved whole.
func TestLongInboxEntryIsVisibleAndSaved(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {60, 24}} {
		v := newTestVault(t)
		v.seed()
		d := newDrv(t, v, size[0], size[1])
		text := longWords(300)
		d.key("c")
		d.typeText(text[:12]) // typing is slow here: each key waits on the cursor blink
		d.send(tea.PasteMsg{Content: text[12:]})
		if !strings.Contains(squash(d.plainFrame()), squash(text)) {
			t.Fatalf("%v: text clipped:\n%s", size, d.plainFrame())
		}
		if strings.Contains(d.plainFrame(), "...") || strings.Contains(d.plainFrame(), "…") {
			t.Fatalf("%v: ellipsis in the frame:\n%s", size, d.plainFrame())
		}
		if n := len(strings.Split(d.m.frame, "\n")); n != size[1] {
			t.Fatalf("%v: frame has %d rows", size, n)
		}
		d.key("enter")
		found := false
		for _, b := range inboxBodies(t, v) {
			found = found || strings.Contains(b, text)
		}
		if !found {
			t.Fatalf("%v: entry not saved whole: %q", size, inboxBodies(t, v))
		}
	}
}

// There is no silent length limit, and a paste keeps its lines.
func TestPasteOverTwoThousandAndNewlines(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	d := newDrv(t, v, 80, 24)
	d.key("c")
	text := longWords(2600)
	d.send(tea.PasteMsg{Content: text})
	d.send(tea.PasteMsg{Content: "\nsecond line\nthird line"})
	d.key("enter")
	found := false
	for _, b := range inboxBodies(t, v) {
		found = found || strings.Contains(b, text) && strings.Contains(b, "\nsecond line\nthird line")
	}
	if !found {
		t.Fatalf("paste lost text or lines: %d chars", len(strings.Join(inboxBodies(t, v), "")))
	}
}

// Shift+enter and ctrl+j add a line; enter submits. A short field takes neither.
func TestNewLineKeys(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	d := newDrv(t, v, 80, 24)
	d.key("c")
	d.typeText("one")
	d.send(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
	d.typeText("two")
	d.send(tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl})
	d.typeText("three")
	if !d.has("shift+enter or ctrl+j adds a line") {
		t.Errorf("no hint:\n%s", d.plainFrame())
	}
	d.key("enter")
	found := false
	for _, b := range inboxBodies(t, v) {
		found = found || strings.Contains(b, "one\ntwo\nthree")
	}
	if !found {
		t.Fatalf("lines lost: %q", inboxBodies(t, v))
	}
	d.key("/")
	d.typeText("a")
	d.send(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
	if strings.Contains(d.m.ov.input.Value(), "\n") {
		t.Error("search box took a new line")
	}
}

// A long text over many lines grows the field to a limit and keeps the cursor line visible.
func TestFieldGrowsThenScrolls(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	d := newDrv(t, v, 60, 24)
	d.key("c")
	d.send(tea.PasteMsg{Content: longWords(1500)})
	if n := d.m.ov.input.Height(); n != maxInputRows {
		t.Errorf("height %d, want %d", n, maxInputRows)
	}
	if !d.has(longWords(1500)[len(longWords(1500))-5:]) {
		t.Errorf("the end of the text is not visible:\n%s", d.plainFrame())
	}
	if n := len(strings.Split(d.m.frame, "\n")); n != 24 {
		t.Errorf("frame has %d rows", n)
	}
}

// The prompt names the action, and the old labels are gone.
func TestInboxAndInvestigateLabels(t *testing.T) {
	v := newTestVault(t)
	v.seed()
	d := newDrv(t, v, 100, 30)
	d.key("c")
	if !d.has("add to inbox, to sort later") {
		t.Errorf("prompt:\n%s", d.plainFrame())
	}
	d.key("esc")
	if !strings.Contains(d.footer(), "Add to inbox") || strings.Contains(d.footer(), "Capture") {
		t.Errorf("footer: %s", d.footer())
	}
}
