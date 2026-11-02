package server

import (
	"github.com/iodesystems/parasolve/internal/netguard"
	"github.com/iodesystems/parasolve/internal/ops"
)

// registerLife: areas, notes, incidents, meetings, questions, links, and dependency edits. Reads are GET with
// vault:read, writes POST with vault:write.
func registerLife(r *registrar) {
	reg(r, ops.AreaAdd, post, write)
	reg(r, ops.AreaList, get, read)
	reg(r, ops.AreaShow, get, read)
	reg(r, ops.NoteAdd, post, write)
	reg(r, ops.NoteList, get, read)
	reg(r, ops.NoteShow, get, read)
	reg(r, ops.AreaLinkAdd, post, write)
	reg(r, ops.AreaLinkRemove, post, write)
	reg(r, ops.ProjectLinkAdd, post, write)
	reg(r, ops.ProjectLinkRemove, post, write)
	reg(r, ops.Where, get, read)
	reg(r, ops.IncidentAdd, post, write)
	reg(r, ops.IncidentList, get, read)
	reg(r, ops.IncidentShow, get, read)
	reg(r, ops.IncidentNote, post, write)
	reg(r, ops.IncidentStatus, post, write)
	reg(r, ops.IncidentLinkAdd, post, write)
	reg(r, ops.IncidentLinkRemove, post, write)
	reg(r, ops.QuestionAdd, post, write)
	reg(r, ops.QuestionList, get, read)
	reg(r, ops.QuestionShow, get, read)
	reg(r, ops.QuestionAnswer, post, write)
	reg(r, ops.QuestionDrop, post, write)
	reg(r, ops.QuestionEdit, post, write)
	reg(r, ops.QuestionDependsAdd, post, write)
	reg(r, ops.QuestionDependsRemove, post, write)
	// An agent's answer is a proposal; only a person's browser session can
	// accept or reject it, and the ops refuse a bearer key.
	reg(r, ops.QuestionAccept, post, netguard.ScopeProposalReview)
	reg(r, ops.QuestionReject, post, netguard.ScopeProposalReview)
	reg(r, ops.QuestionAsk, post, write)
	reg(r, ops.MeetingAdd, post, write)
	reg(r, ops.MeetingList, get, read)
	reg(r, ops.MeetingShow, get, read)
	reg(r, ops.MeetingSchedule, post, write)
	reg(r, ops.MeetingCancel, post, write)
	reg(r, ops.MeetingHold, post, write)
	reg(r, ops.Agenda, get, read)
	reg(r, ops.TaskToQuestion, post, write)
	reg(r, ops.TaskDue, post, write)
	reg(r, ops.TaskEdit, post, write)
	reg(r, ops.TaskDependsAdd, post, write)
	reg(r, ops.TaskDependsRemove, post, write)
}
