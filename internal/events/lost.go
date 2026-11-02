package events

import "github.com/iodesystems/paradize/internal/model"

// LostUpdate is a write that a file-sync tool dropped: another install updated
// the same version of the document, and that version won.
type LostUpdate struct {
	Doc     string
	Install string
	Event   model.Event
}

// LostUpdates finds updates that forked from one version and did not survive.
// evs is oldest first; current returns a document's content hash, or "" if it
// is gone. A lost update stops being reported once its install writes the
// document again, because that write started from the surviving version.
func LostUpdates(evs []model.Event, current func(doc string) string) []LostUpdate {
	type key struct{ doc, prev string }
	forks := map[key][]model.Event{}
	built := map[string]bool{}
	for _, e := range evs {
		if e.Prev == "" {
			continue
		}
		forks[key{e.Doc, e.Prev}] = append(forks[key{e.Doc, e.Prev}], e)
		built[e.Prev] = true
	}
	var out []LostUpdate
	for _, e := range evs {
		group := forks[key{e.Doc, e.Prev}]
		if e.Prev == "" || len(group) < 2 || !otherInstall(group, e) {
			continue
		}
		if built[e.Hash] || current(e.Doc) == e.Hash || wroteLater(evs, e) {
			continue
		}
		out = append(out, LostUpdate{Doc: e.Doc, Install: e.Install, Event: e})
	}
	return out
}

func otherInstall(group []model.Event, e model.Event) bool {
	for _, g := range group {
		if g.Install != e.Install && g.Hash != e.Hash {
			return true
		}
	}
	return false
}

func wroteLater(evs []model.Event, e model.Event) bool {
	for _, x := range evs {
		if x.Doc == e.Doc && x.Install == e.Install && x.TS.After(e.TS) {
			return true
		}
	}
	return false
}
