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
	reg(r, ops.GoalAdd, post, write)
	reg(r, ops.GoalList, get, read)
	scopeOnly(r, ops.GoalShow, read)
	scopeOnly(r, ops.GoalClose, write)
	scopeOnly(r, ops.GoalEdit, write)
	reg(r, ops.ProjectAdd, post, write)
	scopeOnly(r, ops.ProjectShow, read)
	scopeOnly(r, ops.ProjectRetire, write)
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
	scopeOnly(r, ops.TaskShow, read)
	reg(r, ops.TaskStart, post, write)
	scopeOnly(r, ops.TaskDone, write)
	reg(r, ops.TaskBlock, post, write)
	reg(r, ops.TaskAssign, post, write)
	scopeOnly(r, ops.TaskResolve, write)
	reg(r, ops.TaskToAsks, post, write)
	reg(r, ops.TaskAttempt, post, write)
	reg(r, ops.TaskDelegate, post, write)
	reg(r, ops.TaskUpdate, post, write)
	reg(r, ops.Waiting, get, read)
	reg(r, ops.Graph, get, read)
	reg(r, ops.Next, get, read)
	reg(r, ops.IOAdd, post, write)
	reg(r, ops.IOList, get, read)
	scopeOnly(r, ops.IOShow, read)
	reg(r, ops.IORevisions, get, read)
	reg(r, ops.IODiff, get, read)
	reg(r, ops.IOMade, get, read)
	reg(r, ops.IOProposals, get, read)
	reg(r, ops.IOSent, get, read)
	reg(r, ops.IOEdit, post, write)
	reg(r, ops.IOFile, post, write)
	scopeOnly(r, ops.IODrop, write)
	reg(r, ops.IOSend, post, write)
	reg(r, ops.IOSendCheck, post, write)
	reg(r, ops.IOAttempt, post, write)
	reg(r, ops.TriageRun, post, write)
	// Idea pools: an agent keeps ideas and lists pools (vault:write); the
	// owner picks (review:decide, a person op).
	reg(r, ops.IdeaAdd, post, write)
	reg(r, ops.PoolList, get, read)
	reg(r, ops.PoolRun, post, write)
	reg(r, ops.PoolPick, post, netguard.ScopeReviewDecide)
	reg(r, ops.DelegationRun, get, read)
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
	scopeOnly(r, ops.SourceShow, read)
	reg(r, ops.SourceExample, get, read)
	reg(r, ops.SourceAdd, post, write)
	reg(r, ops.SourceStatus, post, write)
	reg(r, ops.AgentSources, post, write)
	reg(r, ops.SweepPlan, get, read)
	reg(r, ops.SweepRecord, post, write)
	reg(r, ops.SweepReport, get, read)
	reg(r, ops.IOStats, get, read)
}

// registerRFX: rfx:draft prepares an ask. rfx:send records that a person sent
// it, the one state a key must be trusted with to claim. Para sends nothing.
func registerRFX(r *registrar) {
	reg(r, ops.RFXAdd, post, write)
	reg(r, ops.RFXList, get, read)
	reg(r, ops.RFXWaiting, get, read)
	scopeOnly(r, ops.RFXShow, read)
	scopeOnly(r, ops.RFXStatus, write)
	scopeOnly(r, ops.RFXClose, write)
	reg(r, ops.RFXDraft, post, netguard.ScopeRFXDraft)
	reg(r, ops.RFXReply, post, write)
	reg(r, ops.RFXTargetAdd, post, netguard.ScopeRFXDraft)
	reg(r, ops.RFXTargetState, post, netguard.ScopeRFXSend)
	reg(r, ops.RFXPaths, get, read)
	reg(r, ops.FeedbackAdd, post, write)
	reg(r, ops.TagAdd, post, write)
	reg(r, ops.TagRemove, post, write)
	reg(r, ops.TagList, get, read)
	reg(r, ops.RFXPathAdd, post, netguard.ScopeRFXDraft)
}

func registerOrg(r *registrar) {
	reg(r, ops.OrgPersonAdd, post, write)
	reg(r, ops.OrgPersonList, get, read)
	scopeOnly(r, ops.OrgPersonShow, read)
	reg(r, ops.OrgPersonNote, post, write)
	scopeOnly(r, ops.OrgPersonEdit, write)
	scopeOnly(r, ops.OrgPersonAliasAdd, write)
	scopeOnly(r, ops.OrgPersonAliasRemove, write)
	reg(r, ops.OrgPersonContactAdd, post, write)
	scopeOnly(r, ops.OrgPersonContactRemove, write)
	scopeOnly(r, ops.OrgPersonGroupAdd, write)
	scopeOnly(r, ops.OrgPersonGroupRemove, write)
	scopeOnly(r, ops.OrgTeamAdd, write)
	reg(r, ops.OrgTeamList, get, read)
	scopeOnly(r, ops.OrgTeamShow, read)
	scopeOnly(r, ops.OrgRoleAdd, write)
	reg(r, ops.OrgRoleList, get, read)
	scopeOnly(r, ops.OrgRoleShow, read)
	scopeOnly(r, ops.OrgDomainAdd, write)
	reg(r, ops.OrgDomainList, get, read)
	scopeOnly(r, ops.OrgDomainShow, read)
	reg(r, ops.OrgGroupAdd, post, write)
	reg(r, ops.OrgGroupList, get, read)
	scopeOnly(r, ops.OrgGroupShow, read)
	// The owner sets who and what matters from the web app (a person op).
	reg(r, ops.PrioritySet, post, netguard.ScopeReviewDecide)
	// owner 2026-11-10: the owner pins and unpins what they watch (person ops); pinned reads the cards.
	reg(r, ops.Pin, post, netguard.ScopeReviewDecide)
	reg(r, ops.Unpin, post, netguard.ScopeReviewDecide)
	reg(r, ops.Pinned, get, read)
	reg(r, ops.Route, get, read)
	reg(r, ops.Who, get, read)
}

func registerGovernance(r *registrar) {
	scopeOnly(r, ops.ConstraintAdd, write)
	reg(r, ops.ConstraintList, get, read)
	scopeOnly(r, ops.ConstraintShow, read)
	scopeOnly(r, ops.ConventionAdd, write)
	reg(r, ops.ConventionList, get, read)
	scopeOnly(r, ops.ConventionShow, read)
	reg(r, ops.RulesInEffect, get, read)
	scopeOnly(r, ops.ExceptionAdd, write)
	reg(r, ops.ExceptionList, get, read)
	scopeOnly(r, ops.ExceptionShow, read)
	reg(r, ops.ExceptionExpiring, get, read)
	reg(r, ops.PolicyList, get, read)
	scopeOnly(r, ops.PolicyShow, read)
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
	reg(r, ops.PublishPreview, get, read)
	reg(r, ops.PublishSet, post, write)
	reg(r, ops.ExportList, get, read)
	reg(r, ops.CalendarExport, post, read)
	scopeOnly(r, ops.InteractionAdd, write)
	reg(r, ops.InteractionList, get, read)
	scopeOnly(r, ops.InteractionShow, read)
	reg(r, ops.Journal, post, write)
	scopeOnly(r, ops.JournalDaily, write)
	scopeOnly(r, ops.JournalWeekly, write)
	scopeOnly(r, ops.JournalMonthly, write)
	reg(r, ops.JournalList, get, read)
	reg(r, ops.JournalShow, get, read)
	// journal_summary changes one marked section of a journal and nothing else.
	reg(r, ops.JournalSummary, post, write)
	reg(r, ops.Search, get, read)
	reg(r, ops.SimilarOp, get, read)
}

func registerRequests(r *registrar) {
	reg(r, ops.RequestAdd, post, netguard.ScopeRequestQueue)
	scopeOnly(r, ops.RequestCancel, netguard.ScopeRequestQueue)
	reg(r, ops.RequestAcceptOp, post, netguard.ScopeVaultWrite)
	scopeOnly(r, ops.RequestRespond, netguard.ScopeVaultWrite)
	reg(r, ops.RequestSeen, post, netguard.ScopeReviewDecide)
	reg(r, ops.RetryDecide, post, netguard.ScopeReviewDecide)
	reg(r, ops.RequestChanges, get, read)
	reg(r, ops.RequestList, get, read)
	scopeOnly(r, ops.RequestShow, read)
	reg(r, ops.Thread, get, read)
	reg(r, ops.RequestNext, post, netguard.ScopeRequestRun)
	reg(r, ops.RequestNote, post, netguard.ScopeRequestRun)
	reg(r, ops.RequestDone, post, netguard.ScopeRequestRun)
	reg(r, ops.RequestFail, post, netguard.ScopeRequestRun)
	reg(r, ops.RetryPropose, post, netguard.ScopeRequestRun)
}

// registerProposals: closed dossiers are read-only records (vault format 8);
// the owner's actions on what needs them: open vault questions, outgoing
// drafts, and agent work.
func registerProposals(r *registrar) {
	reg(r, ops.ProposalList, get, read)
	reg(r, ops.ProposalShow, get, read)
	reg(r, ops.ProposalRetire, post, netguard.ScopeReviewDecide)
	reg(r, ops.ProposalExecuted, post, write)
	scopeOnly(r, ops.StackLaterOp, netguard.ScopeReviewDecide)
	scopeOnly(r, ops.StackReturn, netguard.ScopeReviewDecide)
	reg(r, ops.SentList, get, read)
	reg(r, ops.QuestionReview, post, netguard.ScopeReviewDecide)
	scopeOnly(r, ops.IODone, netguard.ScopeReviewDecide)
	// Approve on an outgoing draft in the web app: the owner's path of
	// io_approve (a browser session is a person; a bearer key still needs
	// owner_said). Owner 2026-11-06: PU-PREP.
	reg(r, ops.IOApprove, post, netguard.ScopeReviewDecide)
	// The owner marks io reviewed (io_review, person only).
	reg(r, ops.IOReview, post, netguard.ScopeReviewDecide)
	reg(r, ops.AgentActivity, get, read)
	reg(r, ops.AgentList, get, read)
}

// registerReceipts: a receipt is read with vault:read. Checking one and
// reverting one from the receipts page are the owner's (review:decide, person
// ops, so a bearer key is refused); an agent undoes with receipt_undo
// (vault:write).
func registerReceipts(r *registrar) {
	reg(r, ops.ReceiptList, get, read)
	scopeOnly(r, ops.ReceiptShow, read)
	reg(r, ops.History, get, read)
	reg(r, ops.Activity, get, read)
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
	reg(r, ops.OpsStats, get, read)
	reg(r, ops.IndexRebuild, post, write)
	reg(r, ops.Changes, get, read)
	reg(r, ops.VaultUpgradeStatus, get, read)
	reg(r, ops.DossierRefile, post, write)
	reg(r, ops.VaultConfigList, get, read)
	reg(r, ops.VaultConfigGet, get, read)
	reg(r, ops.VaultConfigSet, post, write)
	reg(r, ops.VaultConfigUnset, post, write)
	reg(r, ops.VaultUpgrade, post, write)
	reg(r, ops.VaultCheck, post, write)
}
