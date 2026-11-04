package upgrade

import "github.com/iodesystems/para/internal/model"

// steps are in version order. A shipped step never changes its ID or Version.
func steps() []Step {
	return []Step{
		{
			ID:      "meeting-as-task",
			Version: 1,
			Title:   "Rewrite type: meeting files as tasks of kind meeting",
			Auto:    true,
			Types:   []model.Type{model.TypeMeeting},
			Select: func(d model.Document) bool {
				_, ok := d.Meta.(*model.Meeting)
				return ok
			},
			Rewrite: func(d model.Document) (model.Document, error) {
				d.Meta = d.Meta.(*model.Meeting).AsTask()
				return d, nil
			},
		},
		{
			ID:      "done-when",
			Version: 2,
			Title:   "Give each open goal and task a done_when",
			Types:   []model.Type{model.TypeProject, model.TypeTask},
			Select:  lacksDoneWhen,
			Instructions: "Each document the selector lists is an open goal or task with no done_when. " +
				"Read the document, its links, and its history. Where the vault or your own tools show what done means, " +
				"write a done_when in one sentence the owner can check. Where they do not, say so and do not guess. " +
				"Set it with doc_edit (fields {done_when}) on each document, " +
				"citing the source in sources.",
			Done: "No open goal or task lacks done_when.",
		},
		{
			ID:      "proposal-dossier",
			Version: 3,
			Title:   "Rewrite open proposals in the old one-block form as dossiers",
			Auto:    true,
			Types:   []model.Type{model.TypeProposal},
			Select:  legacyOpenProposal,
			Rewrite: func(d model.Document) (model.Document, error) {
				c, err := model.ParseProposalBody(d.Body)
				if err != nil {
					return d, err
				}
				tldr := c.TLDR
				if tldr == "" {
					tldr = d.Title
				}
				body, err := model.NewDossier(model.DossierHeader{TLDR: tldr, Context: c.Context}, "Items", c.Items).Render()
				if err != nil {
					return d, err
				}
				d.Body = body
				return d, nil
			},
		},
	}
}

// legacyOpenProposal selects the head revision of an open proposal whose body
// is the old single para-proposal block. Superseded revisions stay as history.
func legacyOpenProposal(d model.Document) bool {
	m, ok := d.Meta.(*model.Proposal)
	if !ok || model.IsDossier(d.Body) {
		return false
	}
	if m.Status != model.ProposalOpen && m.Status != model.ProposalSentBack {
		return false
	}
	_, err := model.ParseProposalBody(d.Body)
	return err == nil
}

// lacksDoneWhen selects an open goal, or an open plain task, with no done_when.
// A meeting, a call, and the other kinds do not take one.
func lacksDoneWhen(d model.Document) bool {
	switch m := d.Meta.(type) {
	case *model.Project:
		return (m.Status == model.ProjectActive || m.Status == model.ProjectPaused) && m.DoneWhen == ""
	case *model.Task:
		open := m.Status != model.TaskDone && m.Status != model.TaskDropped
		return open && m.EffectiveKind() == model.KindTask && m.DoneWhen == ""
	}
	return false
}
