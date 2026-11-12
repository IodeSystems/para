package upgrade

import (
	"cmp"
	"context"
	"slices"
	"strings"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

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
			Types:   []model.Type{model.TypeGoal, model.TypeTask},
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
		{
			ID:      "io-folder",
			Version: 4,
			Title:   "Move inbox captures to io/ as incoming io items",
			Auto:    true,
			Types:   []model.Type{model.TypeInbox},
			Select:  openInbox,
			Rewrite: inboxAsIO,
		},
		{
			ID:      "calendar-uids",
			Version: 5,
			Title:   "Rewrite a meeting's single calendar_uid as calendar_uids [{uid, role: primary}]",
			Auto:    true,
			Types:   []model.Type{model.TypeTask},
			Select: func(d model.Document) bool {
				t, ok := d.Meta.(*model.Task)
				return ok && t.CalendarUID != ""
			},
			Rewrite: func(d model.Document) (model.Document, error) {
				t := d.Meta.(*model.Task)
				t.CalendarUIDs, t.CalendarUID = t.UIDs(), ""
				return d, nil
			},
		},
		{
			// Dossier blocks gained tags, defer_until, deferred_reason, and
			// evidence via and verify; io gained verify. A para before this
			// step reads dossier blocks strictly and would fail on them.
			ID:      "dossier-tags-later",
			Version: 6,
			Title:   "Mark the vault for topic tags, Later on dossier items, and verifiable evidence (upgrade para on every machine of this vault first)",
			Auto:    true,
			Marker:  true,
			Types:   []model.Type{model.TypeProposal, model.TypeIO},
			Select:  func(model.Document) bool { return true },
			Rewrite: func(d model.Document) (model.Document, error) { return d, nil },
		},
		{
			// A para before this step does not know type obligation and fails
			// on it: upgrade para on every machine first (Run checks).
			ID:      "obligations",
			Version: ObligationFormat,
			Title:   "Make meetings, commitments, and asks to the owner obligations in obligations/ (upgrade para on every machine of this vault first)",
			Auto:    true,
			Types:   []model.Type{model.TypeTask, model.TypeMeeting, model.TypeRFX},
			Select:  foldsToObligation,
			Rewrite: func(d model.Document) (model.Document, error) { return asObligation(d, nil) },
			Prepare: func(ctx context.Context, r Reader) (func(model.Document) (model.Document, error), error) {
				prev, err := previousLinks(ctx, r)
				if err != nil {
					return nil, err
				}
				return func(d model.Document) (model.Document, error) { return asObligation(d, prev) }, nil
			},
		},
		{
			// A para before this step reads the moved state as unknown and
			// fails on the dossier: upgrade para on every machine first.
			ID:      "dossiers",
			Version: DossierFormat,
			Title:   "Move what open dossiers hold to vault items: questions, io drafts, and Later; an agent re-files each staged change (upgrade para on every machine of this vault first)",
			Auto:    true,
			Types:   []model.Type{model.TypeProposal},
			Select:  DossierMovable,
			Expand:  expandDossier,
			Judge:   DossierStaged,
			Explain: explainDossier,
			Instructions: "Each document the selector lists is a dossier that still holds staged changes: change (or upstream) blocks in state pending, " +
				"sent_back, skipped, running, or approved and waiting, which para no longer runs. Para has no review stack now: an undoable write applies at once, with a receipt the owner can undo. " +
				"For each staged item, read it, its evidence, and the vault as it is now. If the change is still right, make it yourself with the op it names " +
				"(the input as the block holds it, or as the owner's draft answer edited it), then record it with dossier_refile {proposal, item, result: the id the op wrote or its receipt}. " +
				"If it is no longer needed (done since, superseded, or the owner denied it in words), record dossier_refile {proposal, item, dropped: the reason}. " +
				"A change that needs the owner (it cannot be undone, or the owner must decide) becomes a question (question_add) linked to the dossier; then record it with result: that question. " +
				"An upstream item is marked seen by the owner only (doc_seen): drop it with the reason \"the tracked document still shows the change unseen\". " +
				"The dossier closes as superseded when its last item is recorded. When no dossier is left, tell the owner to run para vault upgrade once: it raises the vault format.",
			Done: "No open dossier holds a staged change.",
		},
		{
			// A para before this step reads callouts as text and an area
			// with follows as an area that follows nothing; it writes
			// policies and exceptions as files again. Upgrade para on every
			// machine first (Run checks).
			ID:      "policy-docs",
			Version: PolicyDocsFormat,
			Title:   "Make policies areas that follow their source, and exceptions, conventions, and constraints annotations in their areas (upgrade para on every machine of this vault first)",
			Auto:    true,
			Types:   []model.Type{model.TypePolicy, model.TypePolicyException, model.TypeConvention, model.TypeConstraint},
			Select:  foldsToAnnotation,
			Rewrite: func(d model.Document) (model.Document, error) { return d, nil },
			Batch:   policyDocsBatch,
		},
		{
			ID:       "annotation-placement",
			Version:  PolicyDocsFormat,
			Title:    "Move each folded annotation from the top of its area to the section it is about",
			Optional: true,
			Types:    []model.Type{model.TypeArea},
			Select:   placementPending,
			Instructions: "Step policy-docs put each exception, convention, and constraint it could not anchor to a rule at the top of its area. " +
				"For each area the selector lists, read the body and each annotation at the top (area_show lists them with their section). " +
				"Where the text makes the section clear, move the annotation there with annotate {area, id, anchor: \"the heading text\"}. " +
				"Where it does not, leave it and say so; do not guess. The owner may dismiss the rest with health_dismiss {id: the area id, kind: " + AnnotationAtTop + "}.",
			Done: "No folded annotation is at the top of an area, or the owner dismissed it.",
		},
		{
			// A para before this step reads a group of kind role or domain,
			// a logged io item, and a task of kind incident as values it does
			// not know and fails; it writes the older types again. Upgrade
			// para on every machine first (Run checks).
			ID:      "small-types",
			Version: SmallTypesFormat,
			Title:   "Make teams, roles, and domains groups of that kind, interactions closed io items, and incidents tasks of kind incident (upgrade para on every machine of this vault first)",
			Auto:    true,
			Types:   []model.Type{model.TypeTeam, model.TypeRole, model.TypeDomain, model.TypeInteraction, model.TypeIncident},
			Select:  foldsToSmallType,
			Rewrite: asSmallType,
			Explain: explainSmallType,
		},
		{
			ID:       "notes-placement",
			Version:  SmallTypesFormat,
			Title:    "Move each note that links an item into the notes of that item, or into a section of its area",
			Optional: true,
			Types:    []model.Type{model.TypeNote},
			Select:   notePending,
			Instructions: "Each document the selector lists is a note (type note) that links an area or a goal. Notes now live on the item they are about. " +
				"For each note, read it (note_show) and the item it links. Where the text is about that item, append it to the item's notes: notes_read {id}, " +
				"then notes_edit {id, hash, md: the notes as they are, then the note's title and text}; where it is about one section of an area, " +
				"add it there with annotate {area, anchor: \"the heading text\", kind: note, text}. Then archive the note with doc_archive {id, reason: \"moved to the item id\"}. " +
				"Where it is not clear where the text belongs, leave the note and say so; do not guess. A note that links nothing stays a note. " +
				"The owner may keep a note as it is with health_dismiss {id: the note id, kind: " + NotePlacement + "}.",
			Done: "No note that links an item is left outside archive/, or the owner dismissed it.",
		},
		{
			// A para before this step does not know type goal and fails on
			// it: upgrade para on every machine first (Run checks). Only the
			// owner runs it (Confirm): it changes ids, so it writes a vault
			// snapshot first, in this machine's state directory.
			ID:      "goals",
			Version: GoalsFormat,
			Title:   "Make each finite project a goal: type goal in goals/, id goal- in place of project- (the old id keeps resolving), every link to it rewritten, project: links to it moved to goal:, someday goals parked (upgrade para on every machine of this vault first; the owner runs it)",
			Auto:    true,
			Confirm: true,
			Types:   []model.Type{model.TypeProject},
			Select:  legacyGoal,
			Rewrite: asGoal,
			Custom:  runGoals,
			Native:  true,
		},
		{
			ID:         "projects-sort",
			Version:    GoalsFormat,
			Title:      "Sort project areas and ongoing goals: each one becomes an ongoing project, a concept area, or stays a goal",
			Optional:   true,
			KeepListed: true,
			Types:      []model.Type{model.TypeArea, model.TypeGoal},
			Select:     sortPending,
			Instructions: "Each document the selector lists is an area of kind project (or with no kind), or an active goal with no done_when and no due. " +
				"A project is now an ongoing tracked thing with a state line, goals in flight, and an icebox; an area is a facet, information about working in an area. " +
				"For each one, read it (show) and what links it, then ask the owner which it is, in one question with your recommendation and the reason: " +
				"an ongoing project (doc_retype {id, type: project, fields: {status: active, folded_from: area or goal}}; then the goals that name it as their area get project: [[id]] with edit), " +
				"a concept area (edit {id, fields: {kind: concept}}), or, for a goal, a goal that needs a done_when (edit {id, fields: {done_when}}). " +
				"Do not decide for the owner. The owner may keep it as it is with health_dismiss {id, kind: " + ProjectSort + "}.",
			Done: "No area of kind project or with no kind, and no active goal with no done_when and no due, is left, or the owner dismissed it.",
		},
	}
}

// ObligationFormat is the format at which meetings, commitments, and asks to
// the owner are obligations: below it ops write them in the older types.
const ObligationFormat = 7

// AsObligation is a new document of a type step obligations folds, made an
// obligation as the step would, without the keys of the old type.
func AsObligation(d model.Document) (model.Document, error) {
	out, err := asObligation(d, nil)
	if err != nil {
		return d, err
	}
	out = out.DropExtra()
	out.Meta.(*model.Obligation).FoldedFrom = ""
	return out, nil
}

// FoldsToObligation reports whether step obligations folds the document.
func FoldsToObligation(d model.Document) bool { return foldsToObligation(d) }

// foldsToObligation selects a task of kind meeting or commitment (or tagged
// commitment), an older meeting, and an ask to the owner (rfx direction in).
func foldsToObligation(d model.Document) bool {
	switch m := d.Meta.(type) {
	case *model.Task:
		k := m.EffectiveKind()
		return k == model.KindMeeting || k == model.KindCommitment
	case *model.Meeting:
		return true
	case *model.RFX:
		return m.Direction == model.RFXIn
	}
	return false
}

// previousLinks maps each task id to the task it recurred from, so a series
// occurrence finds the first of its series.
func previousLinks(ctx context.Context, r Reader) (map[string]string, error) {
	refs, err := r.List(ctx, store.Filter{Type: model.TypeTask, All: true})
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, ref := range refs {
		d, err := r.Get(ctx, ref.ID)
		if err != nil {
			continue
		}
		if t, ok := d.Meta.(*model.Task); ok && t.Previous != "" {
			out[d.ID] = t.Previous.Target()
		}
	}
	return out, nil
}

// seriesRoot is the first occurrence of id's series: previous followed back
// as far as it goes.
func seriesRoot(prev map[string]string, id string) string {
	seen := map[string]bool{id: true}
	for {
		p, ok := prev[id]
		if !ok || p == "" || seen[p] {
			return id
		}
		seen[p] = true
		id = p
	}
}

// asObligation retypes a meeting, a commitment, or an ask to the owner as an
// obligation in obligations/ (an archived one stays in archive/). The id and
// the body stay, so links still resolve; every key of the old type stays in
// the file (model.Retype), and folded_from names the old type. A task's
// status maps to state; a closed one keeps its outcome. A recurring one is a
// series occurrence of the first task of its chain.
func asObligation(d model.Document, prev map[string]string) (model.Document, error) {
	from := d.Meta.Header().Type
	if m, ok := d.Meta.(*model.Meeting); ok {
		d.Meta = m.AsTask()
	}
	set := map[string]any{"folded_from": string(from), "schedule": string(model.ScheduleOnce), "state": string(model.StateOpen)}
	closed := func(outcome model.TaskOutcome) {
		set["state"], set["outcome"] = string(model.StateClosed), string(outcome)
		if day := cmp.Or(d.Meta.Header().Updated, d.Meta.Header().Created); day != "" {
			set["closed"] = string(day)
		}
	}
	switch m := d.Meta.(type) {
	case *model.Task:
		kind := m.EffectiveKind()
		set["kind"] = string(kind)
		switch {
		case m.Recur != "":
			set["schedule"] = string(model.ScheduleSeries)
			if root := seriesRoot(prev, d.ID); root != d.ID {
				set["occurrence_of"] = string(model.LinkTo(root))
			}
		case kind == model.KindMeeting:
			set["schedule"] = string(model.ScheduleAt)
		}
		if kind == model.KindCommitment && len(m.With) > 0 {
			set["owed_to"] = linkStrings(m.With)
		}
		// A commitment assigned to a person is owed by them.
		if a := strings.TrimSpace(m.Assignee); a != "" && !strings.Contains(a, ":") && a != "self" {
			set["owed_by"] = []string{string(model.LinkTo(a))}
		}
		switch m.Status {
		case model.TaskDone:
			closed(cmp.Or(m.Outcome, model.OutcomeDone))
		case model.TaskDropped:
			closed(cmp.Or(m.Outcome, model.OutcomeDropped))
		}
	case *model.RFX:
		set["kind"] = string(model.KindCommitment)
		var to []model.Link
		for _, t := range m.Targets {
			if !slices.Contains(to, t.To) {
				to = append(to, t.To)
			}
		}
		if len(to) > 0 {
			set["owed_to"] = linkStrings(to)
		}
		switch m.Status {
		case model.RFXAnswered, model.RFXClosed:
			closed(model.OutcomeDone)
		case model.RFXDeclined:
			closed(model.OutcomeDeclined)
		}
	}
	folder := d.Folder
	out, err := model.Retype(d, model.TypeObligation, set)
	if err != nil {
		return d, err
	}
	if folder == model.FolderArchive {
		out.Folder = folder
	}
	return out, nil
}

func linkStrings(ls []model.Link) []string {
	out := make([]string, len(ls))
	for i, l := range ls {
		out[i] = string(l)
	}
	return out
}

// openInbox selects a capture still in inbox/. A capture triaged before io
// lives in archive/ as type inbox and stays as it is: it is history.
func openInbox(d model.Document) bool {
	_, ok := d.Meta.(*model.Inbox)
	return ok && d.Folder == model.FolderInbox
}

// inboxAsIO makes a capture an unread incoming io item in io/. The id stays,
// so links still resolve. The body becomes the whole capture: an io title is
// the first line of the body, and inbox_add kept that line only in the title.
func inboxAsIO(d model.Document) (model.Document, error) {
	old := d.Meta.Header()
	c := *old
	c.Type = model.TypeIO
	d.Meta = &model.IO{Common: c, Direction: model.IOIn, Medium: model.MediumOther, State: model.IOUnread}
	d.Body = model.CaptureText(d.Title, d.Body)
	d.Folder = model.FolderIO
	return d, nil
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
	case *model.Goal: // below format 11 the vault reads a file of type project as a goal
		return (m.Status == model.GoalActive || m.Status == model.GoalPaused) && m.DoneWhen == ""
	case *model.Task:
		open := m.Status != model.TaskDone && m.Status != model.TaskDropped
		return open && m.EffectiveKind() == model.KindTask && m.DoneWhen == ""
	}
	return false
}
