package server

import (
	"net/http"

	"github.com/iodesystems/parasolve/internal/netguard"
	"github.com/iodesystems/parasolve/internal/ops"
)

const (
	get  = http.MethodGet
	post = http.MethodPost

	read  = netguard.ScopeVaultRead
	write = netguard.ScopeVaultWrite
)

func registerWork(r *registrar) {
	reg(r, ops.ProjectAdd, post, write)
	reg(r, ops.ProjectList, get, read)
	reg(r, ops.ProjectShow, get, read)
	reg(r, ops.ProjectClose, post, write)
	reg(r, ops.TaskAdd, post, write)
	reg(r, ops.TaskList, get, read)
	reg(r, ops.TaskShow, get, read)
	reg(r, ops.TaskStart, post, write)
	reg(r, ops.TaskDone, post, write)
	reg(r, ops.TaskBlock, post, write)
	reg(r, ops.TaskAssign, post, write)
	reg(r, ops.Waiting, get, read)
	reg(r, ops.Graph, get, read)
	reg(r, ops.Next, get, read)
	reg(r, ops.InboxAdd, post, write)
	reg(r, ops.InboxList, get, read)
	reg(r, ops.InboxShow, get, read)
	reg(r, ops.InboxTriage, post, write)
}

// registerRFX: rfx:draft prepares an ask. rfx:send records that a person sent
// it, the one state a key must be trusted with to claim. Para sends nothing.
func registerRFX(r *registrar) {
	reg(r, ops.RFXAdd, post, write)
	reg(r, ops.RFXList, get, read)
	reg(r, ops.RFXWaiting, get, read)
	reg(r, ops.RFXShow, get, read)
	reg(r, ops.RFXStatus, post, write)
	reg(r, ops.RFXClose, post, write)
	reg(r, ops.RFXDraft, post, netguard.ScopeRFXDraft)
	reg(r, ops.RFXReply, post, write)
	reg(r, ops.RFXTargetAdd, post, netguard.ScopeRFXDraft)
	reg(r, ops.RFXTargetState, post, netguard.ScopeRFXSend)
}

func registerOrg(r *registrar) {
	reg(r, ops.OrgPersonAdd, post, write)
	reg(r, ops.OrgPersonList, get, read)
	reg(r, ops.OrgPersonShow, get, read)
	reg(r, ops.OrgPersonNote, post, write)
	reg(r, ops.OrgPersonAliasAdd, post, write)
	reg(r, ops.OrgPersonAliasRemove, post, write)
	reg(r, ops.OrgPersonContactAdd, post, write)
	reg(r, ops.OrgPersonContactRemove, post, write)
	reg(r, ops.OrgPersonGroupAdd, post, write)
	reg(r, ops.OrgPersonGroupRemove, post, write)
	reg(r, ops.OrgTeamAdd, post, write)
	reg(r, ops.OrgTeamList, get, read)
	reg(r, ops.OrgTeamShow, get, read)
	reg(r, ops.OrgRoleAdd, post, write)
	reg(r, ops.OrgRoleList, get, read)
	reg(r, ops.OrgRoleShow, get, read)
	reg(r, ops.OrgDomainAdd, post, write)
	reg(r, ops.OrgDomainList, get, read)
	reg(r, ops.OrgDomainShow, get, read)
	reg(r, ops.OrgGroupAdd, post, write)
	reg(r, ops.OrgGroupList, get, read)
	reg(r, ops.OrgGroupShow, get, read)
	reg(r, ops.Route, get, read)
	reg(r, ops.Who, get, read)
}

func registerGovernance(r *registrar) {
	reg(r, ops.ConstraintAdd, post, write)
	reg(r, ops.ConstraintList, get, read)
	reg(r, ops.ConstraintShow, get, read)
	reg(r, ops.ConventionAdd, post, write)
	reg(r, ops.ConventionList, get, read)
	reg(r, ops.ConventionShow, get, read)
	reg(r, ops.ExceptionAdd, post, write)
	reg(r, ops.ExceptionList, get, read)
	reg(r, ops.ExceptionShow, get, read)
	reg(r, ops.ExceptionExpiring, get, read)
	reg(r, ops.PolicyList, get, read)
	reg(r, ops.PolicyShow, get, read)
	reg(r, ops.PolicyReview, post, write)
	reg(r, ops.PolicyRefresh, post, write)
	reg(r, ops.RuleAdd, post, write)
	reg(r, ops.RuleList, get, read)
	reg(r, ops.Ingest, post, write)
}

// registerRecords: copy and export are POST because they log an egress event,
// with vault:read because they change no document. They render text and return
// it. No transport runs on the server: `to` only names the egress destination.
func registerRecords(r *registrar) {
	reg(r, ops.Copy, post, read)
	reg(r, ops.Export, post, read)
	reg(r, ops.ExportList, get, read)
	reg(r, ops.CalendarExport, post, read)
	reg(r, ops.InteractionAdd, post, write)
	reg(r, ops.InteractionList, get, read)
	reg(r, ops.InteractionShow, get, read)
	reg(r, ops.JournalDaily, post, write)
	reg(r, ops.JournalWeekly, post, write)
	reg(r, ops.JournalMonthly, post, write)
	reg(r, ops.JournalList, get, read)
	reg(r, ops.JournalShow, get, read)
	// journal_summary changes one marked section of a journal and nothing else.
	reg(r, ops.JournalSummary, post, write)
	reg(r, ops.Search, get, read)
}

func registerRequests(r *registrar) {
	reg(r, ops.RequestAdd, post, netguard.ScopeRequestQueue)
	reg(r, ops.RequestCancel, post, netguard.ScopeRequestQueue)
	reg(r, ops.RequestList, get, read)
	reg(r, ops.RequestShow, get, read)
	reg(r, ops.RequestNext, post, netguard.ScopeRequestRun)
	reg(r, ops.RequestNote, post, netguard.ScopeRequestRun)
	reg(r, ops.RequestDone, post, netguard.ScopeRequestRun)
	reg(r, ops.RequestFail, post, netguard.ScopeRequestRun)
}

// registerProposals: a key with vault:read and proposal:add can suggest and
// change nothing. Review routes take proposal:review, and the ops refuse a
// bearer key, so a decision needs a browser session. Any op an approval runs
// is checked against the approver's own scopes (boundVault.deps).
func registerProposals(r *registrar) {
	reg(r, ops.ProposalAdd, post, netguard.ScopeProposalAdd)
	reg(r, ops.ProposalRevise, post, netguard.ScopeProposalAdd)
	reg(r, ops.ProposalList, get, read)
	reg(r, ops.ProposalShow, get, read)
	reg(r, ops.ProposalDiff, get, read)
	reg(r, ops.ProposalWait, get, read)
	reg(r, ops.ProposalApproved, get, read)
	reg(r, ops.ProposalApprove, post, netguard.ScopeProposalReview)
	reg(r, ops.ProposalDeny, post, netguard.ScopeProposalReview)
	reg(r, ops.ProposalRework, post, netguard.ScopeProposalReview)
	reg(r, ops.ProposalSendback, post, netguard.ScopeProposalReview)
	reg(r, ops.ProposalHold, post, netguard.ScopeProposalReview)
	reg(r, ops.ProposalAnnotate, post, netguard.ScopeProposalReview)
	reg(r, ops.ProposalEdit, post, netguard.ScopeProposalReview)
}

func registerVault(r *registrar) {
	reg(r, ops.Lint, get, read)
	reg(r, ops.Reconcile, post, write)
	reg(r, ops.EventsTail, get, read)
	reg(r, ops.IndexRebuild, post, write)
}
