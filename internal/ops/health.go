package ops

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
	"github.com/iodesystems/para/internal/upgrade"
)

// Health. A read that reports gaps in a vault as warnings, grouped by goal.
// It never fails a read or a write and always succeeds with findings. Lint
// stays format only. A person can dismiss a finding; the dismissal is stored
// in the frontmatter of the document it names (model.HealthDismissal), so it
// stays with the vault under any sync tool.

// Finding kinds.
const (
	HealthCycle              = "cycle"
	HealthNoPath             = "no_path"
	HealthGoalNoOpenTask     = "goal_no_open_task"
	HealthDanglingDependency = "dangling_dependency"
	HealthGoalNoDue          = "goal_no_due"
	HealthAskNoFollowup      = "ask_no_followup"
	HealthBlockerNoFollowup  = "blocker_no_followup"
	HealthGoalNoDoneWhen     = "goal_no_done_when"
	HealthTaskNoDoneWhen     = "task_no_done_when"
	HealthAskNoTarget        = "ask_no_target"
	HealthPersonNoContact    = "person_no_contact"
	HealthBlockerIncomplete  = "blocker_incomplete"
	HealthAreaNoLinks        = "area_no_links"
	HealthNoSource           = "no_source"
	HealthPolicyReviewDue    = "policy_review_due"
	HealthQuestionStale      = "question_stale"
	HealthNotAligned         = "not_aligned"
	HealthExceptionNoPolicy  = "exception_no_policy"
	HealthByUnknown          = "by_unknown"
	HealthClosedWithOpen     = "closed_with_open_items"
	HealthGoalNoSignal       = "goal_no_signal"
	HealthPivotPassed        = "pivot_passed"
	HealthAreaNeglected      = "area_neglected"
	HealthTooManyActive      = "too_many_active_goals"
	HealthAssumptionStale    = "assumption_stale"
	HealthRiskUnmitigated    = "risk_unmitigated"
	HealthDocUnseen          = "doc_unseen"
	HealthDocStale           = "doc_stale"
	HealthDocCommentOpen     = "doc_comment_open"
	HealthSourceUnreached    = "source_unreached"
	HealthSourceOverdue      = "source_overdue"
	HealthSourceMissing      = "source_missing"
	// HealthRetryDue is a failed task whose retry_after has come.
	HealthRetryDue = "retry_due"
	// HealthTaskIsWaiting is an open task whose title reads like waiting on
	// someone: that is an Ask (task_to_asks).
	HealthTaskIsWaiting = "task_is_waiting"
	// HealthUnreviewedOutward is an outward act a sweep saw with no record in
	// the vault: no receipt, sent io item, or interaction at its URL.
	HealthUnreviewedOutward = "unreviewed_outward"
	// HealthAgentWorkStale is work the owner asked of agents (owner-ask,
	// sent-to-agent) queued or claimed with no change in a day.
	HealthAgentWorkStale = "agent_work_stale"
	// HealthGateUnused is the vault setting gate.require, set and read by
	// nothing since 0.3.0.
	HealthGateUnused = "gate_setting_unused"
	// HealthUpgradePending is a vault upgrade step with documents left: the
	// owner runs an automated one, an agent works a judgement one.
	HealthUpgradePending = "upgrade_pending"
)

// HealthKinds lists every finding kind, in the order findings print.
var HealthKinds = []string{
	HealthCycle, HealthDanglingDependency, HealthNoPath, HealthGoalNoOpenTask,
	HealthGoalNoDue, HealthAskNoFollowup, HealthBlockerNoFollowup,
	HealthGoalNoDoneWhen, HealthTaskNoDoneWhen, HealthAskNoTarget, HealthPersonNoContact,
	HealthBlockerIncomplete, HealthAreaNoLinks,
	HealthNoSource, HealthPolicyReviewDue, HealthQuestionStale, HealthNotAligned,
	HealthExceptionNoPolicy, HealthByUnknown, HealthClosedWithOpen,
	HealthGoalNoSignal, HealthPivotPassed, HealthAreaNeglected,
	HealthTooManyActive, HealthRiskUnmitigated, HealthAssumptionStale,
	HealthDocUnseen, HealthDocStale, HealthDocCommentOpen,
	HealthSourceUnreached, HealthSourceOverdue, HealthSourceMissing, HealthRetryDue, HealthTaskIsWaiting,
	HealthUnreviewedOutward, HealthAgentWorkStale, HealthGateUnused, HealthUpgradePending,
}

// HealthKindWords names each finding kind in words, for a screen.
var HealthKindWords = map[string]string{
	HealthCycle:              "Circular dependency",
	HealthNoPath:             "Tied to no goal or area",
	HealthGoalNoOpenTask:     "Goal with no open task",
	HealthDanglingDependency: "Depends on something missing",
	HealthGoalNoDue:          "Goal with no due date",
	HealthAskNoFollowup:      "Ask with no follow-up date",
	HealthBlockerNoFollowup:  "Blocked, no follow-up date",
	HealthGoalNoDoneWhen:     "Goal with no done-when",
	HealthTaskNoDoneWhen:     "Task with no done-when",
	HealthAskNoTarget:        "Ask with no one to ask",
	HealthPersonNoContact:    "Person with no contact",
	HealthBlockerIncomplete:  "Blocker reason too short",
	HealthAreaNoLinks:        "Area with no links",
	HealthNoSource:           "Answer with no source",
	HealthPolicyReviewDue:    "Review date has come",
	HealthQuestionStale:      "Stale question",
	HealthNotAligned:         "Not checked against sources",
	HealthExceptionNoPolicy:  "Exception with no policy linked",
	HealthByUnknown:          "Who set it is unknown",
	HealthClosedWithOpen:     "Closed with open items",
	HealthGoalNoSignal:       "Goal signals not read",
	HealthPivotPassed:        "Change-of-course date has come",
	HealthAreaNeglected:      "Area review is overdue",
	HealthRetryDue:           "Failed task can be tried again",
	HealthTaskIsWaiting:      "Task that is waiting on someone",
	HealthTooManyActive:      "Too many active goals",
	HealthAssumptionStale:    "Assumption not checked lately",
	HealthRiskUnmitigated:    "High-impact risk with no mitigation",
	HealthDocUnseen:          "Tracked document changes not seen",
	HealthDocStale:           "Tracked document not read lately",
	HealthDocCommentOpen:     "Comment on a document not settled",
	HealthSourceUnreached:    "No agent reaches the source",
	HealthSourceOverdue:      "Source not swept on time",
	HealthSourceMissing:      "Item cites a source not in the vault",
	HealthUnreviewedOutward:  "Sent as the owner with no record",
	HealthAgentWorkStale:     "Asked of an agent, no change in a day",
	HealthGateUnused:         "Vault setting no longer used",
	HealthUpgradePending:     "Vault upgrade step pending",
}

// HealthOtherGroup is the group of findings that belong to no goal.
const HealthOtherGroup = "other"

// HealthStaleDays is the age after which an open question that blocks nothing
// is stale.
const HealthStaleDays = 30

// SeverityWarning is the only severity: a finding never fails anything.
const SeverityWarning = "warning"

// HealthFinding is one gap.
type HealthFinding struct {
	Kind     string `json:"kind"`
	Label    string `json:"label"` // the kind in words
	Severity string `json:"severity"`
	ID       string `json:"id"`
	Title    string `json:"title"`
	Goal     string `json:"goal,omitempty"` // goal id, empty in the other group
	Message  string `json:"message"`
	Fix      string `json:"fix"` // a suggested op or command
}

// HealthGroup is the findings of one goal, or of none ("other").
type HealthGroup struct {
	Goal     string          `json:"goal"` // goal id, or "other"
	Title    string          `json:"title"`
	Due      string          `json:"due,omitempty"`
	Findings []HealthFinding `json:"findings"`
}

// HealthInput is the input of health.
type HealthInput struct{}

// HealthOutput is the output of health. Count is the findings listed;
// Dismissed is how many a person set aside.
type HealthOutput struct {
	Groups    []HealthGroup `json:"groups"`
	Count     int           `json:"count"`
	Dismissed int           `json:"dismissed"`
}

// Health reads the vault and reports findings.
func Health(ctx context.Context, s store.Store) (HealthOutput, error) {
	return HealthAt(ctx, s, s.Now().Format(dateLayout))
}

// HealthAt is Health for the local date today (YYYY-MM-DD).
func HealthAt(ctx context.Context, s store.Store, today string) (HealthOutput, error) {
	refs, err := s.List(ctx, store.Filter{})
	if err != nil {
		return HealthOutput{}, fmt.Errorf("ops: health: %w", err)
	}
	docs := map[string]model.Document{}
	for _, r := range refs {
		d, err := s.Get(ctx, r.ID)
		if err != nil {
			return HealthOutput{}, fmt.Errorf("ops: health: %w", err)
		}
		docs[r.ID] = obligationShape(d) // a meeting or work owed is checked as a task of its kind
	}
	order, err := openWorkAt(ctx, s, today)
	if err != nil {
		return HealthOutput{}, fmt.Errorf("ops: health: %w", err)
	}
	rows := map[string]ValueRow{}
	goalRank := map[string]int{}
	for i, r := range order.Rows {
		rows[r.ID] = r
		if r.Kind == KindGoal {
			goalRank[r.ID] = i
		}
	}
	h := &healther{docs: docs, rows: rows, today: today, goalDue: map[string]string{}, now: s.Now()}
	h.stale = ""
	if t, err := time.Parse(dateLayout, today); err == nil {
		h.stale = t.AddDate(0, 0, -HealthStaleDays).Format(dateLayout)
	}
	days, err := recheckDays(s)
	if err != nil {
		return HealthOutput{}, fmt.Errorf("ops: health: %w", err)
	}
	h.recheckCutoff = daysBefore(today, days)
	checkDays, err := docsCheckDays(s)
	if err != nil {
		return HealthOutput{}, fmt.Errorf("ops: health: %w", err)
	}
	h.index()
	h.scan()
	h.trackedDocs(checkDays)
	if err := h.sources(s); err != nil {
		return HealthOutput{}, fmt.Errorf("ops: health: %w", err)
	}
	h.graphFindings(ctx, s)
	h.agentWorkStale()
	if err := h.notAligned(ctx, s); err != nil {
		return HealthOutput{}, fmt.Errorf("ops: health: %w", err)
	}
	if err := h.tooManyActive(s, goalRank); err != nil {
		return HealthOutput{}, fmt.Errorf("ops: health: %w", err)
	}
	if v, err := s.Config(GateConfig); err != nil {
		return HealthOutput{}, fmt.Errorf("ops: health: %w", err)
	} else if v != "" {
		h.add(HealthGateUnused, "", "", fmt.Sprintf("%s is set (%s) and no longer used: an undoable agent write applies at once, and one that cannot be undone needs the owner", GateConfig, v),
			"the owner runs para vault config unset "+GateConfig)
	}
	if st, err := upgrade.StatusOf(ctx, s); err == nil {
		for _, p := range st.Pending {
			if p.Auto {
				h.add(HealthUpgradePending, "", "", fmt.Sprintf("vault upgrade step %s has %d documents for the owner to run (%s)", p.ID, p.Count, p.Title),
					"the owner runs para vault upgrade --dry-run, then para vault upgrade")
				continue
			}
			h.add(HealthUpgradePending, "", "", fmt.Sprintf("vault upgrade step %s has %d documents for an agent (judgement: %s)", p.ID, p.Count, p.Title),
				"an agent works the agent work titled \""+upgradePrefix+p.Title+"\" (para vault upgrade --file queues it)")
		}
	}

	// Dismissed findings are left out and counted.
	var kept []HealthFinding
	dismissed := 0
	for _, f := range h.out {
		if dismissedKind(docs[f.ID], f.Kind) {
			dismissed++
			continue
		}
		kept = append(kept, f)
	}
	kindRank := map[string]int{}
	for i, k := range HealthKinds {
		kindRank[k] = i
	}
	byGoal := map[string][]HealthFinding{}
	for _, f := range kept {
		g := f.Goal
		if g == "" {
			g = HealthOtherGroup
		}
		byGoal[g] = append(byGoal[g], f)
	}
	var groups []HealthGroup
	for g, fs := range byGoal {
		slices.SortFunc(fs, func(a, b HealthFinding) int {
			return cmp.Or(cmp.Compare(kindRank[a.Kind], kindRank[b.Kind]), cmp.Compare(a.ID, b.ID))
		})
		gr := HealthGroup{Goal: g, Title: "Other", Findings: fs}
		if g != HealthOtherGroup {
			gr.Title, gr.Due = docs[g].Title, h.goalDue[g]
		}
		groups = append(groups, gr)
	}
	// Goals in the value order (dated goals come first in it); other last.
	slices.SortFunc(groups, func(a, b HealthGroup) int {
		ao, bo := a.Goal == HealthOtherGroup, b.Goal == HealthOtherGroup
		if ao != bo {
			if ao {
				return 1
			}
			return -1
		}
		return cmp.Or(cmp.Compare(rankOr(goalRank, a.Goal), rankOr(goalRank, b.Goal)), cmp.Compare(a.Goal, b.Goal))
	})
	if groups == nil {
		groups = []HealthGroup{}
	}
	return HealthOutput{Groups: groups, Count: len(kept), Dismissed: dismissed}, nil
}

func rankOr(m map[string]int, k string) int {
	if v, ok := m[k]; ok {
		return v
	}
	return 1 << 30
}

func dismissedKind(d model.Document, kind string) bool {
	if d.Meta == nil {
		return false
	}
	for _, x := range d.Meta.Header().HealthDismissed {
		if x.Kind == kind {
			return true
		}
	}
	return false
}

type healther struct {
	docs  map[string]model.Document
	rows  map[string]ValueRow
	today string
	stale string
	now   time.Time
	// recheckCutoff is the date before which an assumption's check is stale;
	// empty when recheck.days is off.
	recheckCutoff string
	goalDue       map[string]string
	out           []HealthFinding
	// openKids maps a task id to its open action items.
	openKids map[string][]string
	// openGoals are active goals that are not deferred.
	openGoals map[string]bool
	// openTasks counts open tasks per goal.
	openTasks map[string]int
	// dependents maps an id to the open items that wait on it.
	dependents map[string][]string
}

// titles maps ids to titles, keeping the id of a document that is not there.
func (h *healther) titles(ids []string) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id
		if d, ok := h.docs[id]; ok && d.Title != "" {
			out[i] = d.Title
		}
	}
	return out
}

func (h *healther) deferred(m model.Meta) bool {
	if df := deferralOf(m); df != nil {
		return df.IsDeferred(h.today)
	}
	return false
}

func healthLabel(kind string) string {
	if w, ok := HealthKindWords[kind]; ok {
		return w
	}
	return kind
}

func (h *healther) add(kind, id, goal, msg, fix string) {
	h.out = append(h.out, HealthFinding{Kind: kind, Severity: SeverityWarning, ID: id, Label: healthLabel(kind), Title: h.docs[id].Title, Goal: goal, Message: msg, Fix: fix})
}

// goalOf returns the open goal an item belongs to, "" for none, and skip when
// the item sits under a goal that is closed, paused, or deferred.
func (h *healther) goalOf(d model.Document) (goal string, skip bool) {
	var g string
	switch m := d.Meta.(type) {
	case *model.Project:
		g = d.ID
	case *model.Task:
		g = m.Project.Target()
	case *model.RFX:
		g = m.Project.Target()
	case *model.Question:
		if !m.IsRoot() {
			sc := m.Scope.Target()
			if sd, ok := h.docs[sc]; ok {
				if t, ok := sd.Meta.(*model.Task); ok {
					g = t.Project.Target()
				} else if _, ok := sd.Meta.(*model.Project); ok {
					g = sc
				}
			}
		}
	}
	if g == "" {
		return "", false
	}
	gd, ok := h.docs[g]
	if !ok {
		return "", false
	}
	p, ok := gd.Meta.(*model.Project)
	if !ok {
		return "", false
	}
	if p.Status != model.ProjectActive || h.deferred(p) {
		return "", true
	}
	return g, false
}

func (h *healther) index() {
	h.openGoals, h.openTasks, h.dependents = map[string]bool{}, map[string]int{}, map[string][]string{}
	h.openKids = map[string][]string{}
	for id, d := range h.docs {
		switch m := d.Meta.(type) {
		case *model.Project:
			if m.Status == model.ProjectActive && !h.deferred(m) {
				h.openGoals[id] = true
				h.goalDue[id] = string(m.Due)
			}
		case *model.Task:
			if p := m.Parent.Target(); p != "" && !taskTerminal(m.Status) {
				h.openKids[p] = append(h.openKids[p], id)
			}
			if !taskTerminal(m.Status) {
				for _, dep := range targets(m.DependsOn) {
					h.dependents[dep] = append(h.dependents[dep], id)
				}
			}
		case *model.Question:
			if m.Status == model.QuestionOpen {
				for _, dep := range targets(m.DependsOn) {
					h.dependents[dep] = append(h.dependents[dep], id)
				}
			}
		case *model.RFX:
			if m.Status == model.RFXOpen {
				for _, dep := range targets(m.DependsOn) {
					h.dependents[dep] = append(h.dependents[dep], id)
				}
				for _, b := range m.Blocks {
					h.dependents[id] = append(h.dependents[id], b.Target())
				}
			}
		}
	}
	for _, d := range h.docs {
		if t, ok := d.Meta.(*model.Task); ok && !taskTerminal(t.Status) {
			h.openTasks[t.Project.Target()]++
		}
	}
}

// needsNoPath is true for a kind of task that stands alone: a meeting, a call,
// or a commitment is not part of a goal's work.
func needsNoPath(k model.TaskKind) bool {
	return k == model.KindMeeting || k == model.KindCall || k == model.KindCommitment
}

// hasPath reports whether an open item leads to a goal or an area.
func (h *healther) hasPath(id string, seen map[string]bool) bool {
	if seen[id] {
		return false
	}
	seen[id] = true
	d, ok := h.docs[id]
	if !ok {
		return false
	}
	switch m := d.Meta.(type) {
	case *model.Project, *model.Area:
		return true
	case *model.Task:
		for _, l := range []string{m.Project.Target(), m.Area.Target()} {
			if _, ok := h.docs[l]; ok && l != "" {
				return true
			}
		}
	case *model.RFX:
		if _, ok := h.docs[m.Project.Target()]; ok && m.Project.Target() != "" {
			return true
		}
	case *model.Question:
		if !m.IsRoot() {
			if sd, ok := h.docs[m.Scope.Target()]; ok {
				switch sd.Meta.(type) {
				case *model.Project, *model.Area, *model.Task:
					return true
				}
			}
		}
	}
	for _, u := range h.dependents[id] {
		if h.hasPath(u, seen) {
			return true
		}
	}
	return false
}

func (h *healther) scan() {
	for id, d := range h.docs {
		if d.Meta == nil {
			continue
		}
		goal, skip := h.goalOf(d)
		if skip || h.deferred(d.Meta) {
			continue
		}
		switch m := d.Meta.(type) {
		case *model.Project:
			if m.Status != model.ProjectActive {
				continue
			}
			if m.Due == "" {
				h.add(HealthGoalNoDue, id, goal, "goal has no due date", fmt.Sprintf("project_edit {id: %s, due: YYYY-MM-DD}, or health_dismiss", id))
			}
			if strings.TrimSpace(m.DoneWhen) == "" {
				h.add(HealthGoalNoDoneWhen, id, goal, "goal has no done-when line", fmt.Sprintf("project_edit {id: %s, done_when: ...}", id))
			}
			h.brief(id, goal, m)
			h.assumptions(d, goal)
			if h.openTasks[id] == 0 {
				h.add(HealthGoalNoOpenTask, id, goal, "goal has no open task", fmt.Sprintf("task_add {project: %s, ...}, or project_close {id: %s}", id, id))
			}
		case *model.Task:
			if taskTerminal(m.Status) && len(h.openKids[id]) > 0 {
				slices.Sort(h.openKids[id])
				h.add(HealthClosedWithOpen, id, goal, fmt.Sprintf("task is closed with %d open action items", len(h.openKids[id])), fmt.Sprintf("finish, drop, or move %s; or reopen the parent", strings.Join(h.openKids[id], " ")))
			}
			if !taskTerminal(m.Status) && !meetingQuiet(m) {
				h.assumptions(d, goal)
			}
			if m.Outcome == model.OutcomeFailed && m.RetryAfter != "" && string(m.RetryAfter) <= h.today {
				h.add(HealthRetryDue, id, goal, "failed task may be tried again since "+string(m.RetryAfter), fmt.Sprintf("task_add again from %s, or health_dismiss", id))
			}
			// An optional meeting the owner has not said they go to, and one
			// they skip, ask nothing.
			if taskTerminal(m.Status) || IsOther(m.Assignee) || meetingQuiet(m) {
				continue
			}
			if strings.TrimSpace(m.DoneWhen) == "" {
				h.add(HealthTaskNoDoneWhen, id, goal, "task has no done-when line", fmt.Sprintf("task_edit {id: %s, done_when: ...}", id))
			}
			if m.EffectiveKind() == model.KindTask && readsLikeWaiting(d.Title) {
				h.add(HealthTaskIsWaiting, id, goal, "the task reads like waiting on someone: waiting on a person is an Ask, not a task",
					fmt.Sprintf("task_to_asks {id: %s, people: [person-...]}: one Ask per person; dependents move to them", id))
			}
			if !h.hasPath(id, map[string]bool{}) && !needsNoPath(m.Kind) {
				h.add(HealthNoPath, id, goal, "open task leads to no goal or area", fmt.Sprintf("task_add again under a goal, or task_done {id: %s}", id))
			}
			if m.Status == model.TaskBlocked {
				h.blocker(id, goal, m)
			}
		case *model.RFX:
			if m.Status != model.RFXOpen || m.Direction != model.RFXOut {
				continue
			}
			if m.NextNudge == "" {
				h.add(HealthAskNoFollowup, id, goal, "ask has no follow-up date", fmt.Sprintf("ask the owner for a date, then rfx_nudge {id: %s, date: YYYY-MM-DD}; rfx_add sets next_nudge when the ask is made", id))
			}
			if len(m.Targets) == 0 {
				h.add(HealthAskNoTarget, id, goal, "ask has no target person or medium", fmt.Sprintf("rfx_target_add {id: %s, to: person-..., medium: ...}", id))
			}
			if !h.hasPath(id, map[string]bool{}) {
				h.add(HealthNoPath, id, goal, "open ask leads to no goal or area", fmt.Sprintf("rfx_close {id: %s}, or ask the owner which goal it serves", id))
			}
		case *model.Question:
			h.question(id, goal, d, m)
		case *model.Person:
			if len(m.Contacts) == 0 {
				h.add(HealthPersonNoContact, id, "", "person has no contact", fmt.Sprintf("org_person_contact_add {id: %s, method: ..., value: ...}", id))
			}
		case *model.Area:
			if m.Status == model.AreaActive && len(m.Links) == 0 {
				h.add(HealthAreaNoLinks, id, "", "area has no links", fmt.Sprintf("area_link_add {id: %s, label: ..., kind: ..., target: ...}", id))
			}
			if areaNeglected(m, h.today) {
				msg := "area has a " + m.Cadence + " review and was never reviewed"
				if m.LastReviewed != "" {
					msg = "area has a " + m.Cadence + " review; last reviewed " + string(m.LastReviewed)
				}
				h.add(HealthAreaNeglected, id, "", msg, fmt.Sprintf("review it with the owner, then area_review {id: %s}", id))
			}
		case *model.Constraint:
			// A constraint always has a source: lint refuses one without.
			if strings.EqualFold(m.By, model.ByUnknown) {
				h.add(HealthByUnknown, id, "", "constraint does not say who set it (by: unknown)", fmt.Sprintf("ask the owner who, then doc_edit {id: %s, fields: {by: ...}}", id))
			}
			if m.ReviewBy != "" && string(m.ReviewBy) <= h.today {
				h.add(HealthPolicyReviewDue, id, "", "constraint review date is "+string(m.ReviewBy), "ask the owner to confirm it, or constraint_add again with a new review_by")
			}
		case *model.PolicyException:
			if m.Policy == "" {
				h.add(HealthExceptionNoPolicy, id, "", "exception names its policy in words only: no policy document is linked", fmt.Sprintf("find the policy with search; doc_edit {id: %s, fields: {policy: \"[[policy-...]]\"}}, or ingest it first", id))
			}
			if strings.EqualFold(m.By, model.ByUnknown) {
				h.add(HealthByUnknown, id, "", "exception does not say who raised or decided it (by: unknown)", fmt.Sprintf("ask the owner who, then doc_edit {id: %s, fields: {by: ...}}", id))
			}
			if m.Status == model.ExceptionOpen && m.ReviewBy != "" && string(m.ReviewBy) <= h.today {
				h.add(HealthPolicyReviewDue, id, "", "policy exception review date is "+string(m.ReviewBy), fmt.Sprintf("exception_show {id: %s}, then ask the owner", id))
			}
		}
	}
}

// brief flags an active goal whose brief defines signals and has read none, and
// one whose pivot date has come. A goal that is stopped or pivoted is settled.
func (h *healther) brief(id, goal string, m *model.Project) {
	if m.Validation == model.ValidationStopped || m.Validation == model.ValidationPivoted {
		return
	}
	if len(m.Signals) > 0 {
		read := false
		for _, s := range m.Signals {
			read = read || len(s.Readings) > 0
		}
		if !read {
			h.add(HealthGoalNoSignal, id, goal, "goal defines signals and none has a reading", fmt.Sprintf("read a signal with your own tools, then goal_signal_add {id: %s, what: ..., value: ..., source: ...}", id))
		}
	}
	var bare []string
	for _, r := range m.Risks {
		if r.Impact == model.LevelHigh && strings.TrimSpace(r.Mitigation) == "" {
			bare = append(bare, fmt.Sprintf("%q", r.Text))
		}
	}
	if len(bare) > 0 {
		h.add(HealthRiskUnmitigated, id, goal, "high-impact risk with no mitigation: "+strings.Join(bare, "; "), fmt.Sprintf("ask the owner what reduces it, or whether it is accepted; then goal_risk_set {id: %s, text: ..., mitigation: ...}", id))
	}
	if pv := m.Pivot; pv != nil && pv.Date != "" && string(pv.Date) <= h.today {
		h.add(HealthPivotPassed, id, goal, fmt.Sprintf("the date to decide on a change of course, %s, has come: %s (%s)", pv.Date, pv.Condition, pv.Action), fmt.Sprintf("ask the owner; then doc_edit {id: %s, fields: {validation: pivoted|stopped|active, pivot: ...}}, or project_close {id: %s}", id, id))
	}
}

// assumptions flags a goal or a task with an assumption checked before the
// recheck cutoff. A never-checked assumption is not flagged: recheck finds it.
func (h *healther) assumptions(d model.Document, goal string) {
	if h.recheckCutoff == "" {
		return
	}
	var old []string
	for _, a := range assumptionsOf(d, h.recheckCutoff) {
		if a.Stale {
			old = append(old, fmt.Sprintf("%q (checked %s)", a.Text, a.Checked))
		}
	}
	if len(old) > 0 {
		h.add(HealthAssumptionStale, d.ID, goal, fmt.Sprintf("%d assumptions checked before %s (recheck.days): %s", len(old), h.recheckCutoff, strings.Join(old, "; ")),
			fmt.Sprintf("recheck {scope: %s}, then recheck_record per item; or assumption_set with what a check found", d.ID))
	}
}

// blocker flags a blocked task that gives no who, why, and where, or no date.
func (h *healther) blocker(id, goal string, m *model.Task) {
	complete := false
	for _, r := range m.BlockedBy {
		if len(strings.Fields(r)) >= 4 {
			complete = true
		}
	}
	if !complete {
		h.add(HealthBlockerIncomplete, id, goal, "blocker reason is missing or too short: say who said it, why, and where it applies", fmt.Sprintf("task_block {id: %s, reason: who, why, where}", id))
	}
	if r, ok := h.rows[id]; ok && r.EffDue == "" && m.DeferUntil == "" {
		h.add(HealthBlockerNoFollowup, id, goal, "blocked task has no follow-up date and no dated goal", fmt.Sprintf("task_block {id: %s, reason: ..., until: YYYY-MM-DD}, or task_due {id: %s, date: YYYY-MM-DD}", id, id))
	}
}

func (h *healther) question(id, goal string, d model.Document, m *model.Question) {
	switch m.Status {
	case model.QuestionOpen:
		r, ok := h.rows[id]
		if ok && r.Impact == 0 && h.stale != "" && string(m.Created) != "" && string(m.Created) < h.stale {
			h.add(HealthQuestionStale, id, goal, fmt.Sprintf("question blocks nothing and is older than %d days", HealthStaleDays), fmt.Sprintf("question_drop {id: %s, reason: ...}, or defer {ids: [%s]}", id, id))
		}
		if !h.hasPath(id, map[string]bool{}) {
			h.add(HealthNoPath, id, goal, "open question leads to no goal or area", fmt.Sprintf("question_drop {id: %s, reason: ...}", id))
		}
	case model.QuestionAnswered:
		kind := ""
		switch m.AsKind() {
		case model.KindDecision:
			kind = "decision"
		case model.KindQuestion:
			kind = "fact"
		}
		if kind != "" && !strings.Contains(d.Body, "- Source:") {
			h.add(HealthNoSource, id, goal, "answered "+kind+" has no source", "add a source line to the answer, or health_dismiss")
		}
	}
}

// graphFindings adds cycles and dangling dependencies from the work graph.
func (h *healther) graphFindings(ctx context.Context, s store.Store) {
	w, err := loadWork(ctx, s, "", "")
	if err != nil {
		return
	}
	for _, c := range w.g.Cycles() {
		if len(c) == 0 {
			continue
		}
		id := c[0]
		goal, skip := "", false
		if d, ok := h.docs[id]; ok {
			goal, skip = h.goalOf(d)
			skip = skip || h.deferred(d.Meta)
		}
		if skip {
			continue
		}
		fix := fmt.Sprintf("depends_cut {id: %s, dep: ..., reason: ...} on one edge of %s", id, strings.Join(c, " "))
		for _, e := range w.edges {
			if slices.Contains(c, e.From) && slices.Contains(c, e.To) {
				fix = fmt.Sprintf("depends_cut {id: %s, dep: %s, reason: ...}", e.From, e.To)
				break
			}
		}
		h.add(HealthCycle, id, goal, "dependency cycle: "+strings.Join(h.titles(c), " -> "), fix)
	}
	missing := map[string]bool{}
	for _, n := range w.nodes {
		if n.State == StateMissing {
			missing[n.ID] = true
		}
	}
	for _, e := range w.edges {
		if !missing[e.To] {
			continue
		}
		d, ok := h.docs[e.From]
		if !ok || resolved(d) {
			continue
		}
		goal, skip := h.goalOf(d)
		if skip || h.deferred(d.Meta) {
			continue
		}
		h.add(HealthDanglingDependency, e.From, goal, "depends on "+e.To+", which does not exist", fmt.Sprintf("depends_cut {id: %s, dep: %s, reason: ...}", e.From, e.To))
	}
}

// HealthOp lists the gaps in a vault.
var HealthOp = defineRead("health", "Report gaps in the vault as warnings grouped by goal (cycles, missing dates, metadata, sources, stale questions), each with a suggested fix; never fails; dismissed findings are left out and counted", func(ctx context.Context, d Deps, _ HealthInput) (HealthOutput, error) {
	return Health(ctx, d.Store)
})

// HealthDismissInput is the input of health_dismiss.
type HealthDismissInput struct {
	ID     string `json:"id"`     // the document the finding names
	Kind   string `json:"kind"`   // finding kind
	Reason string `json:"reason"` // why it can stay as it is
}

// HealthDismissOutput is the output of health_dismiss and health_undismiss.
type HealthDismissOutput struct {
	ID        string   `json:"id"`
	Dismissed []string `json:"dismissed"` // kinds now dismissed on the document
	Rev       string   `json:"rev"`
}

func healthChange(ctx context.Context, d Deps, id string, edit func(c *model.Common) error) (HealthDismissOutput, error) {
	doc, err := d.Store.Get(ctx, normID(id))
	if err != nil {
		return HealthDismissOutput{}, err
	}
	c := doc.Meta.Header()
	if err := edit(c); err != nil {
		return HealthDismissOutput{}, err
	}
	saved, err := d.Store.Update(ctx, doc)
	if err != nil {
		return HealthDismissOutput{}, err
	}
	kinds := []string{}
	for _, x := range c.HealthDismissed {
		kinds = append(kinds, x.Kind)
	}
	return HealthDismissOutput{ID: doc.ID, Dismissed: kinds, Rev: saved.Rev}, nil
}

// HealthDismiss sets one finding aside.
var HealthDismiss = definePerson("health_dismiss", "Dismiss a health finding on a document, with a reason; it stays out of health until undismissed", func(ctx context.Context, d Deps, in HealthDismissInput) (HealthDismissOutput, error) {
	if !slices.Contains(HealthKinds, in.Kind) {
		return HealthDismissOutput{}, fmt.Errorf("kind %q: want one of %s", in.Kind, strings.Join(HealthKinds, ", "))
	}
	reason := strings.Join(strings.Fields(in.Reason), " ")
	if reason == "" {
		return HealthDismissOutput{}, fmt.Errorf("reason is empty")
	}
	if err := lintText("reason", reason); err != nil {
		return HealthDismissOutput{}, err
	}
	today := d.Store.Now().Format(dateLayout)
	return healthChange(ctx, d, in.ID, func(c *model.Common) error {
		c.HealthDismissed = slices.DeleteFunc(c.HealthDismissed, func(x model.HealthDismissal) bool { return x.Kind == in.Kind })
		c.HealthDismissed = append(c.HealthDismissed, model.HealthDismissal{Kind: in.Kind, Reason: reason, Date: model.Date(today)})
		return nil
	})
})

// HealthUndismissInput is the input of health_undismiss.
type HealthUndismissInput struct {
	ID   string `json:"id"`
	Kind string `json:"kind"`
}

// HealthUndismiss brings a dismissed finding back.
var HealthUndismiss = definePerson("health_undismiss", "Bring a dismissed health finding back", func(ctx context.Context, d Deps, in HealthUndismissInput) (HealthDismissOutput, error) {
	return healthChange(ctx, d, in.ID, func(c *model.Common) error {
		c.HealthDismissed = slices.DeleteFunc(c.HealthDismissed, func(x model.HealthDismissal) bool { return x.Kind == in.Kind })
		return nil
	})
})
