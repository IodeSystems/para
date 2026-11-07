package server

import (
	"net/http"

	"github.com/iodesystems/para/internal/netguard"
	"github.com/iodesystems/para/internal/ops"
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
	reg(r, ops.ProjectEdit, post, write)
	reg(r, ops.GoalSignalAdd, post, write)
	reg(r, ops.GoalRank, post, write)
	reg(r, ops.GoalCompare, get, read)
	reg(r, ops.GoalRiskSet, post, write)
	reg(r, ops.AssumptionSet, post, write)
	reg(r, ops.GrillContext, get, read)
	reg(r, ops.Recheck, post, write)
	reg(r, ops.RecheckRecord, post, write)
	reg(r, ops.TaskAdd, post, write)
	reg(r, ops.TaskList, get, read)
	reg(r, ops.TaskShow, get, read)
	reg(r, ops.TaskStart, post, write)
	reg(r, ops.TaskDone, post, write)
	reg(r, ops.TaskBlock, post, write)
	reg(r, ops.TaskAssign, post, write)
	reg(r, ops.TaskResolve, post, write)
	reg(r, ops.TaskToAsks, post, write)
	reg(r, ops.TaskAttempt, post, write)
	reg(r, ops.TaskDelegate, post, write)
	reg(r, ops.TaskUpdate, post, write)
	reg(r, ops.Waiting, get, read)
	reg(r, ops.Graph, get, read)
	reg(r, ops.Next, get, read)
	reg(r, ops.IOAdd, post, write)
	reg(r, ops.IOList, get, read)
	reg(r, ops.IOShow, get, read)
	reg(r, ops.IOEdit, post, write)
	reg(r, ops.IOFile, post, write)
	reg(r, ops.IODrop, post, write)
	reg(r, ops.IOSend, post, write)
	reg(r, ops.TriageRun, post, write)
}

// registerDocs: tracked documents. An agent tracks a document and passes each
// text it reads (vault:write); the owner marks changes seen (review:decide, a
// person op, so a bearer key is refused).
func registerDocs(r *registrar) {
	reg(r, ops.DocTrack, post, write)
	reg(r, ops.DocVersionAdd, post, write)
	reg(r, ops.DocTracked, get, read)
	reg(r, ops.DocTrackedShow, get, read)
	reg(r, ops.DocTrackedStatus, post, write)
	reg(r, ops.DocDiff, get, read)
	reg(r, ops.DocCommentAdd, post, write)
	reg(r, ops.DocComments, get, read)
	reg(r, ops.DocCommentResolve, post, write)
	reg(r, ops.DocCommentFile, post, write)
	reg(r, ops.DocSeen, post, netguard.ScopeReviewDecide)
}

// registerSources: sources and sweeps. An agent declares the sources it
// reaches and records its sweeps (vault:write); the PWA reads them.
func registerSources(r *registrar) {
	reg(r, ops.SourceList, get, read)
	reg(r, ops.SourceShow, get, read)
	reg(r, ops.SourceExample, get, read)
	reg(r, ops.SourceAdd, post, write)
	reg(r, ops.SourceStatus, post, write)
	reg(r, ops.AgentSources, post, write)
	reg(r, ops.SweepPlan, get, read)
	reg(r, ops.SweepRecord, post, write)
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
	reg(r, ops.RFXPaths, get, read)
	reg(r, ops.ReportFriction, get, read)
	reg(r, ops.FeedbackAdd, post, write)
	reg(r, ops.TagAdd, post, write)
	reg(r, ops.TagRemove, post, write)
	reg(r, ops.TagList, get, read)
	reg(r, ops.RFXPathAdd, post, netguard.ScopeRFXDraft)
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
	// The owner sets who and what matters from the web app (a person op).
	reg(r, ops.PrioritySet, post, netguard.ScopeReviewDecide)
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
	reg(r, ops.RulesInEffect, get, read)
	reg(r, ops.ExceptionAdd, post, write)
	reg(r, ops.ExceptionList, get, read)
	reg(r, ops.ExceptionShow, get, read)
	reg(r, ops.ExceptionExpiring, get, read)
	reg(r, ops.PolicyList, get, read)
	reg(r, ops.PolicyShow, get, read)
	reg(r, ops.PolicyReview, post, write)
	reg(r, ops.PolicyRefresh, post, write)
	reg(r, ops.Annotate, post, write)
	reg(r, ops.AnnotationList, get, read)
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
	reg(r, ops.SimilarOp, get, read)
}

func registerRequests(r *registrar) {
	reg(r, ops.RequestAdd, post, netguard.ScopeRequestQueue)
	reg(r, ops.RequestCancel, post, netguard.ScopeRequestQueue)
	reg(r, ops.RequestAcceptOp, post, netguard.ScopeVaultWrite)
	reg(r, ops.RequestRespond, post, netguard.ScopeVaultWrite)
	reg(r, ops.RequestSeen, post, netguard.ScopeReviewDecide)
	reg(r, ops.RequestChanges, get, read)
	reg(r, ops.RequestList, get, read)
	reg(r, ops.RequestShow, get, read)
	reg(r, ops.RequestNext, post, netguard.ScopeRequestRun)
	reg(r, ops.RequestNote, post, netguard.ScopeRequestRun)
	reg(r, ops.RequestDone, post, netguard.ScopeRequestRun)
	reg(r, ops.RequestFail, post, netguard.ScopeRequestRun)
}

// registerProposals: closed dossiers are read-only records (vault format 8);
// the owner's actions on what needs them: open vault questions, outgoing
// drafts, and agent work.
func registerProposals(r *registrar) {
	reg(r, ops.ProposalList, get, read)
	reg(r, ops.ProposalShow, get, read)
	reg(r, ops.ProposalExecuted, post, write)
	reg(r, ops.StackLaterOp, post, netguard.ScopeReviewDecide)
	reg(r, ops.StackReturn, post, netguard.ScopeReviewDecide)
	reg(r, ops.SentList, get, read)
	reg(r, ops.QuestionReview, post, netguard.ScopeReviewDecide)
	reg(r, ops.IODone, post, netguard.ScopeReviewDecide)
	// Approve on an outgoing draft in the web app: the owner's path of
	// io_approve (a browser session is a person; a bearer key still needs
	// owner_said). Owner 2026-11-06: PU-PREP.
	reg(r, ops.IOApprove, post, netguard.ScopeReviewDecide)
	reg(r, ops.AgentActivity, get, read)
}

// registerReceipts: a receipt is read with vault:read. Checking one and
// reverting one from the receipts page are the owner's (review:decide, person
// ops, so a bearer key is refused); an agent undoes with receipt_undo
// (vault:write).
func registerReceipts(r *registrar) {
	reg(r, ops.ReceiptList, get, read)
	reg(r, ops.ReceiptShow, get, read)
	reg(r, ops.History, get, read)
	reg(r, ops.ItemContext, get, read)
	reg(r, ops.ReportOutcomes, get, read)
	reg(r, ops.ReceiptCheck, post, netguard.ScopeReviewDecide)
	reg(r, ops.ReceiptRevert, post, netguard.ScopeReviewDecide)
	reg(r, ops.ReceiptUndo, post, write)
	reg(r, ops.AlignScope, get, read)
	reg(r, ops.AlignRecord, post, netguard.ScopeVaultWrite)
	// import_verify and calendar_compare take lists, so they are POST; they
	// change nothing.
	reg(r, ops.ImportVerify, post, read)
	reg(r, ops.ImportList, get, read)
	reg(r, ops.CalendarCompare, post, read)
}

func registerVault(r *registrar) {
	reg(r, ops.Lint, get, read)
	reg(r, ops.Reconcile, post, write)
	reg(r, ops.EventsTail, get, read)
	reg(r, ops.IndexRebuild, post, write)
	reg(r, ops.Changes, get, read)
	reg(r, ops.VaultUpgradeStatus, get, read)
	reg(r, ops.DossierRefile, post, write)
	reg(r, ops.VaultConfigList, get, read)
	reg(r, ops.VaultConfigGet, get, read)
	reg(r, ops.VaultConfigSet, post, write)
	reg(r, ops.VaultConfigUnset, post, write)
	reg(r, ops.VaultUpgrade, post, write)
}
