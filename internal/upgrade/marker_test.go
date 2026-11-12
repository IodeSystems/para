package upgrade

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/iodesystems/para/internal/model"
)

func TestMarkerStepRaisesFormatAndNewerRefuses(t *testing.T) {
	f := newFixture(t)
	s := f.open("one")
	ctx := context.Background()
	if _, err := s.Create(ctx, model.Document{Folder: model.FolderIO, Title: "Hi", Body: "Hi",
		Meta: &model.IO{Common: model.Common{Type: model.TypeIO}, Direction: model.IOIn, Medium: model.MediumOther, State: model.IOUnread}}); err != nil {
		t.Fatal(err)
	}
	pending := func() bool {
		st, err := StatusOf(ctx, s)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range st.Pending {
			if p.ID == "dossier-tags-later" {
				return true
			}
		}
		return false
	}
	// A judgement step before the marker holds format_version back: a run
	// cannot raise the marker, so status must not list it as work.
	if pending() {
		t.Fatal("the marker step is listed while an earlier judgement step is pending")
	}
	// Clear the judgement step first: the marker comes after it.
	for _, id := range []string{"project-open", "task-open"} {
		d, err := s.Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		switch m := d.Meta.(type) {
		case *model.Goal:
			m.DoneWhen = "Shipped."
		case *model.Task:
			m.DoneWhen = "Done."
		}
		if _, err := s.Update(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	if !pending() {
		st, _ := StatusOf(ctx, s)
		t.Fatalf("the marker step is not pending for a vault with io: %+v", st.Pending)
	}
	res, err := Run(ctx, s, false)
	if err != nil {
		t.Fatal(err)
	}
	if res.FormatVersion != Latest() || pending() {
		t.Fatalf("after run format %d, pending %v", res.FormatVersion, pending())
	}
	if err := Check(s); err != nil {
		t.Fatalf("check at the latest format: %v", err)
	}
	if err := s.SetConfig(ctx, ConfigKey, strconv.Itoa(Latest()+1)); err != nil {
		t.Fatal(err)
	}
	if err := Check(s); err == nil || !strings.Contains(err.Error(), "newer than this para") {
		t.Fatalf("check of a newer vault = %v", err)
	}
}
