package upgrade

import (
	"fmt"

	"github.com/iodesystems/para/internal/model"
)

// SmallTypesFormat is the format at which a team, a role, and a domain are
// groups of that kind, an interaction is a closed io item, and an incident
// is a task of kind incident: below it ops write the older types.
const SmallTypesFormat = 10

// NotePlacement is the health kind the owner dismisses on a note to keep it a
// note: the judgement step notes-placement then leaves it.
const NotePlacement = "note_placement"

// foldsToSmallType selects what step small-types folds: every team, role,
// domain, interaction, and incident file, archived ones too (they stay in
// archive/).
func foldsToSmallType(d model.Document) bool {
	switch d.Meta.(type) {
	case *model.Team, *model.Role, *model.Domain, *model.Interaction, *model.Incident:
		return true
	}
	return false
}

// FoldsToSmallType reports whether step small-types folds the document.
func FoldsToSmallType(d model.Document) bool { return foldsToSmallType(d) }

// asSmallType retypes a team, role, or domain as a group of that kind; an
// interaction as a closed io item (a logged contact: filed when it came in or
// the owner only saw it, sent when the owner wrote it); an incident as a task
// of kind incident with its status held by the task status. The id, the
// body, and every key of the old type stay (model.Retype), and folded_from
// names the old type. An archived one stays in archive/.
func asSmallType(d model.Document) (model.Document, error) {
	from := d.Meta.Header().Type
	set := map[string]any{"folded_from": string(from)}
	var to model.Type
	switch m := d.Meta.(type) {
	case *model.Team, *model.Role, *model.Domain:
		to = model.TypeGroup
		kind, _ := model.GroupKindOf(from)
		set["kind"] = string(kind) // a domain keeps owner and has no org
	case *model.Interaction:
		to = model.TypeIO
		switch m.Direction {
		case model.InteractionOut:
			set["direction"], set["state"], set["expects"] = string(model.IOOut), string(model.IOSent), string(model.ExpectsNone)
		case model.InteractionObserved:
			set["direction"], set["state"], set["observed"] = string(model.IOIn), string(model.IOFiled), true
		default:
			set["direction"], set["state"] = string(model.IOIn), string(model.IOFiled)
		}
	case *model.Incident:
		to = model.TypeTask
		set["kind"], set["status"] = string(model.KindIncident), string(model.TaskStatusOf(m.Status))
	default:
		return d, fmt.Errorf("upgrade: %s is a %s, not a small type", d.ID, from)
	}
	folder := d.Folder
	out, err := model.Retype(d, to, set)
	if err != nil {
		return d, err
	}
	if folder == model.FolderArchive {
		out.Folder = folder
	}
	return out, nil
}

// AsSmallType is a new document of a type step small-types folds, made as
// the step would, without folded_from: what the older add ops write at the
// small types format.
func AsSmallType(d model.Document) (model.Document, error) {
	out, err := asSmallType(d)
	if err != nil {
		return d, err
	}
	out = out.DropExtra()
	switch m := out.Meta.(type) {
	case *model.Group:
		m.FoldedFrom = ""
	case *model.IO:
		m.FoldedFrom = ""
	case *model.Task:
		m.FoldedFrom = ""
	}
	return out, nil
}

// explainSmallType says what step small-types does to one document.
func explainSmallType(d model.Document) []string {
	out, err := asSmallType(d)
	if err != nil {
		return []string{err.Error()}
	}
	line := fmt.Sprintf("%s %s → %s", d.Meta.Header().Type, d.ID, out.Meta.Header().Type)
	switch m := out.Meta.(type) {
	case *model.Group:
		line += " of kind " + string(m.Kind)
	case *model.IO:
		line += fmt.Sprintf(" %s, %s (text kept)", m.Direction, m.State)
	case *model.Task:
		line += fmt.Sprintf(" of kind incident %s, status %s", m.Severity, m.Status)
	}
	return []string{line + " in " + string(out.Folder) + "/"}
}

// NotePending reports whether step notes-placement still has the note.
func NotePending(d model.Document) bool { return notePending(d) }

// notePending selects a note step notes-placement still has: one outside
// archive/ that links an item (area or project), unless the owner dismissed
// it. A note that links nothing stays a note.
func notePending(d model.Document) bool {
	n, ok := d.Meta.(*model.Note)
	if !ok || d.Folder == model.FolderArchive || (n.Area == "" && n.Project == "") {
		return false
	}
	for _, x := range n.HealthDismissed {
		if x.Kind == NotePlacement {
			return false
		}
	}
	return true
}
