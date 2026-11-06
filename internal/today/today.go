// Package today answers "what needs me now" across every vault of an
// install. It reads each vault's files directly on every call: there is no
// shared index and no cache, so one vault's data never sits in another's
// store. It is deliberately not an op (internal/ops): an agent surface
// iterates ops, and this view spans trust boundaries, so it is CLI-only.
package today

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/ops"
	"github.com/iodesystems/para/internal/paramd"
	"github.com/iodesystems/para/internal/registry"
	"github.com/iodesystems/para/internal/store/local"
	"github.com/iodesystems/para/internal/upgrade"
	"github.com/iodesystems/para/internal/vault"
)

const dateLayout = "2006-01-02"

// Item is one document that needs attention.
type Item struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Due   string `json:"due,omitempty"` // tasks: the due date
	// Detail is extra text: "sev1 open" for an incident, "decision project-x"
	// (kind and scope) for a question.
	Detail string `json:"detail,omitempty"`
	// Blocks counts what waits on an open question, through any chain.
	Blocks int `json:"blocks,omitempty"`
	// Kind and Why are set on an Agenda item: what it is (incident, task,
	// question, ask, meeting) and why it is where it is, in words.
	Kind string `json:"kind,omitempty"`
	Why  string `json:"why,omitempty"`
	// Bucket is set on an Agenda item: now (actionable now) or not_yet (not
	// yet startable), as in ops.RankValue.
	Bucket string `json:"bucket,omitempty"`
	// Group, Effort, and Prep are set on an Agenda item, as in ops.ValueRow:
	// the group of the value order (reply, focus, ...), the effort, and
	// whether an agent can prepare it.
	Group  string       `json:"group,omitempty"`
	Effort model.Effort `json:"effort,omitempty"`
	Prep   bool         `json:"prep,omitempty"`
	// Ready, ReadyKind, and ReadyRef are set on an Agenda item an agent
	// prepared, as in ops.ValueRow.
	Ready     bool               `json:"ready,omitempty"`
	ReadyKind model.PreparedKind `json:"ready_kind,omitempty"`
	ReadyRef  string             `json:"ready_ref,omitempty"`
	// Proposal marks a question with an answer to check.
	Proposal bool `json:"proposal,omitempty"`
	// Names, for people. Project is a task's project id, Scope a question's
	// scope id ("root" for the vault); each has the title of what it points at.
	// With is who a request goes to, by name.
	Project     string   `json:"project,omitempty"`
	ProjectName string   `json:"project_name,omitempty"`
	Scope       string   `json:"scope,omitempty"`
	ScopeName   string   `json:"scope_name,omitempty"`
	With        []string `json:"with,omitempty"`
	// At is the start of a meeting (RFC 3339); Attend is the owner's answer
	// (yes, no, maybe, or empty), on the Optional and Skipped groups.
	At     string       `json:"at,omitempty"`
	Attend model.Attend `json:"attend,omitempty"`
}

// Group is a counted list.
type Group struct {
	Count int    `json:"count"`
	Items []Item `json:"items"`
}

// VaultToday is what one vault has.
type VaultToday struct {
	Vault string `json:"vault"`
	// Incidents holds incidents not resolved (open or mitigated), worst first.
	// They are shown before everything else.
	Incidents Group `json:"incidents"`
	// Agenda is the one list of what needs the owner, in value order
	// (ops.RankValue): incidents, then items with an effective due date, then
	// the rest. Questions are in it like any other item. Goals shape the order
	// and are not listed.
	Agenda Group `json:"agenda"`
	// OrderSummary sums up the reply group of the agenda: how many, about how
	// many minutes, and how many an agent can prepare.
	OrderSummary ops.OrderSummary `json:"order_summary"`
	// Optional holds the optional meetings of the next AgendaDays days the
	// owner has not said they go to. They ask nothing: they show quietly, apart
	// from Agenda. Skipped holds the meetings the owner does not go to.
	Optional Group `json:"optional"`
	Skipped  Group `json:"skipped"`
	// OtherOpen counts open questions that block nothing and have no date,
	// left out of Agenda while it is long. The Questions page lists them.
	OtherOpen int `json:"other_open"`
	// Deferred counts open questions and tasks put off (Later).
	Deferred int `json:"deferred"`
	// HeldUp lists put-off items that hold up work due before they return.
	HeldUp  []ops.HeldUp `json:"held_up"`
	Due     Group        `json:"due"`     // constraints whose review_by is today or earlier
	Blocked Group        `json:"blocked"` // open tasks with status blocked or a blocked_by entry
	Nudges  Group        `json:"nudges"`  // open RFX whose next_nudge is today or earlier
	// Overdue holds open tasks due before today; DueSoon holds open tasks due
	// from today through DueSoonDays days ahead. Both are ordered by due date.
	Overdue Group `json:"overdue"`
	DueSoon Group `json:"due_soon"`
	// Expiring holds unresolved policy exceptions whose review_by is overdue or
	// within ExpiringDays of today.
	Expiring Group `json:"expiring"`
	// Unverified counts policy summaries no person has reviewed.
	Unverified int `json:"unverified"`
	// Waiting counts what you wait on: open tasks assigned to someone else, and
	// open outbound RFX with a target to hear from (the list `para waiting` shows).
	Waiting int `json:"waiting"`
	// AnswersToCheck counts open questions with an answer an agent proposed
	// that no person has accepted yet.
	AnswersToCheck int `json:"answers_to_check"`
	// Health counts health findings (para health) not dismissed. It is a line
	// on Today, not part of the agenda.
	Health int `json:"health"`
	// UpgradeSteps counts vault upgrade steps (para vault upgrade status) that
	// still have documents to change. It shows beside the health line.
	UpgradeSteps int `json:"upgrade_steps"`
	// ApprovedNotDone counts messages and actions the owner approved that nobody
	// has reported done.
	ApprovedNotDone int `json:"approved_not_done"`
	// Inbox counts unread incoming messages (io), with captures still in inbox/.
	Inbox int `json:"inbox"`
	// Outbox counts outgoing messages ready to send.
	Outbox int `json:"outbox"`
	// DocsUnseen counts watched tracked documents with versions the owner has
	// not seen; DocComments counts their comments with no resolution.
	DocsUnseen  int `json:"docs_unseen"`
	DocComments int `json:"doc_comments"`
	// NeedsYou counts every item that needs the owner (ops.NeedsYou): the
	// one count the agenda, the stack, and Today agree on.
	NeedsYou int `json:"needs_you"`
	// Work lists agent work that waits on the owner: proposed (to accept), or
	// it ended stuck, blocked, or needs_clarification.
	Work Group `json:"work"`
	// AgentOpen counts work the owner asked of agents (owner-ask,
	// sent-to-agent) not finished; AgentStale the part of it with no change in
	// a day (health agent_work_stale).
	AgentOpen  int `json:"agent_open"`
	AgentStale int `json:"agent_stale"`
	// SyncConflicts lists conflict copies a file-sync tool left (finding code
	// sync-conflict in lint). They are not documents.
	SyncConflicts []string `json:"sync_conflicts"`
	// LostUpdates lists documents with a write a sync tool replaced.
	LostUpdates []string `json:"lost_updates"`
	Error       string   `json:"error,omitempty"`
}

// DueSoonDays is how far ahead a task due date counts as due soon.
const DueSoonDays = 7

// ExpiringDays is the window for exceptions, the same as exception_expiring.
const ExpiringDays = 30

// Report is the result across the install.
type Report struct {
	Date   string       `json:"date"`
	Vaults []VaultToday `json:"vaults"`
}

// Failed makes the command exit non-zero when a vault could not be read.
func (r Report) Failed() error {
	n := 0
	for _, v := range r.Vaults {
		if v.Error != "" {
			n++
		}
	}
	if n > 0 {
		return fmt.Errorf("today: %d vaults could not be read", n)
	}
	return nil
}

// Read reads every vault of the install. A vault that cannot be read is
// reported with Error and does not stop the others. now is the date.
func Read(ctx context.Context, in registry.Install, actor, surface, date string) (Report, error) {
	vaults, err := in.Entries()
	if err != nil {
		return Report{}, fmt.Errorf("today: %w", err)
	}
	rep := Report{Date: date, Vaults: []VaultToday{}}
	for _, rv := range vaults {
		vt := VaultToday{Vault: rv.ID, Incidents: Group{Items: []Item{}}, Agenda: Group{Items: []Item{}}, Optional: Group{Items: []Item{}}, Skipped: Group{Items: []Item{}}, HeldUp: []ops.HeldUp{}, Due: Group{Items: []Item{}}, Blocked: Group{Items: []Item{}},
			Nudges: Group{Items: []Item{}}, Overdue: Group{Items: []Item{}}, DueSoon: Group{Items: []Item{}}, Expiring: Group{Items: []Item{}}, Work: Group{Items: []Item{}}, SyncConflicts: []string{}, LostUpdates: []string{}}
		if rv.Err != "" {
			vt.Error = "registry entry is broken: " + rv.Err
			rep.Vaults = append(rep.Vaults, vt)
			continue
		}
		if err := readVault(ctx, rv.Vault, actor, surface, date, &vt); err != nil {
			vt.Error = err.Error()
		}
		rep.Vaults = append(rep.Vaults, vt)
	}
	return rep, nil
}

func readVault(ctx context.Context, rv registry.Vault, actor, surface, date string, vt *VaultToday) error {
	v, err := vault.Open(rv.Path, vault.Options{ID: rv.ID, Actor: actor, Surface: surface, ReadOnly: true})
	if err != nil {
		return err
	}
	if vt.SyncConflicts, err = v.Conflicts(); err != nil {
		return err
	}
	lost, err := v.LostUpdates()
	if err != nil {
		return fmt.Errorf("today: lost updates: %w", err)
	}
	for _, l := range lost {
		vt.LostUpdates = append(vt.LostUpdates, l.Doc)
	}
	refs, err := v.List(ctx, "", "")
	if err != nil {
		return err
	}
	st, err := local.Open(rv.Path, vault.Options{ID: rv.ID, ReadOnly: true})
	if err != nil {
		return fmt.Errorf("today: %w", err)
	}
	sn, err := ops.SnapshotAt(ctx, st, date)
	if err != nil {
		return fmt.Errorf("today: %w", err)
	}
	// Every list of what needs the owner keeps only what NeedsYou says.
	needs := func(id string) bool { return sn.Need(id).Yes }
	limit, soon := "", ""
	if t, err := time.Parse(dateLayout, date); err == nil {
		limit = t.AddDate(0, 0, ExpiringDays).Format(dateLayout)
		soon = t.AddDate(0, 0, DueSoonDays).Format(dateLayout)
	}
	titles := make(map[string]string, len(refs))
	for _, r := range refs {
		titles[r.ID] = r.Title
	}
	for _, r := range refs {
		if r.Hidden {
			continue
		}
		switch r.Type {
		case model.TypeIncident, model.TypeQuestion, model.TypeTask, model.TypeObligation, model.TypeConstraint, model.TypeRFX, model.TypePolicyException, model.TypePolicy, model.TypeArea:
		case model.TypeProposal:
			if d, err := v.Get(ctx, r.ID); err == nil {
				vt.ApprovedNotDone += approvedNotDone(d)
			}
			continue
		case model.TypeInbox:
			if r.Folder == model.FolderInbox {
				vt.Inbox++
			}
			continue
		case model.TypeTrackedDoc:
			if d, err := v.Get(ctx, r.ID); err == nil {
				if m, ok := d.Meta.(*model.TrackedDoc); ok && m.Status == model.DocWatching {
					if m.Latest > m.LastSeen {
						vt.DocsUnseen++
					}
					for _, c := range m.Comments {
						if c.Open() {
							vt.DocComments++
						}
					}
				}
			}
			continue
		case model.TypeRequest:
			if d, err := v.Get(ctx, r.ID); err == nil {
				if n := sn.Need(d.ID); n.Yes {
					vt.Work.Items = append(vt.Work.Items, Item{ID: d.ID, Title: d.Title, Kind: ops.KindWork, Why: n.Reason})
				}
				if m, ok := d.Meta.(*model.Request); ok {
					if open, stale := ops.AskedCounts(d.Title, m, v.Now()); open {
						vt.AgentOpen++
						if stale {
							vt.AgentStale++
						}
					}
				}
			}
			continue
		case model.TypeIO:
			if d, err := v.Get(ctx, r.ID); err == nil {
				if m, ok := d.Meta.(*model.IO); ok && m.State == model.IOUnread {
					vt.Inbox++
				} else if ok && m.State == model.IOReady {
					vt.Outbox++
				}
			}
			continue
		default:
			continue
		}
		d, err := v.Get(ctx, r.ID)
		if err != nil {
			return err
		}
		d = ops.ObligationShape(d) // a meeting or work owed reads as a task of its kind
		item := Item{ID: d.ID, Title: d.Title}
		switch m := d.Meta.(type) {
		case *model.Question:
			if m.HasProposal() && needs(d.ID) {
				vt.AnswersToCheck++
			}
		case *model.Incident:
			if needs(d.ID) {
				item.Detail = string(m.Severity) + " " + string(m.Status)
				vt.Incidents.Items = append(vt.Incidents.Items, item)
			}
		case *model.Constraint:
			if m.ReviewBy != "" && string(m.ReviewBy) <= date && needs(d.ID) {
				vt.Due.Items = append(vt.Due.Items, item)
			}
		case *model.RFX:
			for _, t := range m.Targets {
				if n := titles[t.To.Target()]; n != "" {
					item.With = append(item.With, n)
				}
			}
			if m.NextNudge != "" && string(m.NextNudge) <= date && needs(d.ID) {
				vt.Nudges.Items = append(vt.Nudges.Items, item)
			}
			if m.Status == model.RFXOpen && m.Direction == model.RFXOut && waitsOnSomeone(m) {
				vt.Waiting++
			}
		case *model.PolicyException:
			if m.Status != model.ExceptionResolved && m.ReviewBy != "" && string(m.ReviewBy) <= limit {
				vt.Expiring.Items = append(vt.Expiring.Items, item)
			}
		case *model.Policy:
			if !m.Verified {
				vt.Unverified++
			}
		case *model.Area:
			// An area that follows a source is what a policy was; its
			// exceptions are annotations in its body.
			if m.Follows != nil && !m.Follows.Verified {
				vt.Unverified++
			}
			for _, c := range paramd.Callouts(d.Body) {
				rb := c.Field("review_by")
				if c.Kind == "exception" && c.Broken == "" && rb != "" && rb <= limit && c.Field("status") != string(model.ExceptionResolved) {
					title, _, _ := strings.Cut(c.Text, "\n")
					vt.Expiring.Items = append(vt.Expiring.Items, Item{ID: d.ID, Title: d.Title + ": " + title, Detail: c.ID})
				}
			}
		case *model.Task:
			if item.Project = m.Project.Target(); item.Project == "" {
				item.Project = m.Area.Target()
			}
			item.ProjectName = titles[item.Project]
			open := m.Status != model.TaskDone && m.Status != model.TaskDropped
			if open && m.Assignee != "" && m.Assignee != "self" {
				vt.Waiting++
			}
			if needs(d.ID) && (m.Status == model.TaskBlocked || len(m.BlockedBy) > 0) {
				vt.Blocked.Items = append(vt.Blocked.Items, item)
			}
			if needs(d.ID) && m.Due != "" {
				item.Due = string(m.Due)
				switch {
				case string(m.Due) < date:
					vt.Overdue.Items = append(vt.Overdue.Items, item)
				case soon != "" && string(m.Due) <= soon:
					vt.DueSoon.Items = append(vt.DueSoon.Items, item)
				}
			}
		}
	}
	// Worst severity first: the detail starts with sev1, sev2, or sev3.
	sort.Slice(vt.Incidents.Items, func(i, j int) bool {
		a, b := vt.Incidents.Items[i], vt.Incidents.Items[j]
		if a.Detail != b.Detail {
			return a.Detail < b.Detail
		}
		return a.ID < b.ID
	})
	vt.Incidents.Count = len(vt.Incidents.Items)
	vt.NeedsYou = len(sn.Set())
	if err := fillAgenda(ctx, st, sn, date, vt); err != nil {
		return err
	}
	for _, g := range []*Group{&vt.Due, &vt.Blocked, &vt.Nudges, &vt.Expiring, &vt.Overdue, &vt.DueSoon, &vt.Work} {
		sort.Slice(g.Items, func(i, j int) bool {
			if a, b := g.Items[i], g.Items[j]; a.Due != b.Due {
				return a.Due < b.Due
			}
			return g.Items[i].ID < g.Items[j].ID
		})
		g.Count = len(g.Items)
	}
	return nil
}

// waitsOnSomeone is the rule of rfx_waiting: no target is recorded, a target is
// sent, or every target is no_reply.
func waitsOnSomeone(m *model.RFX) bool {
	if len(m.Targets) == 0 {
		return true
	}
	allNoReply := true
	for _, t := range m.Targets {
		if t.State == model.TargetSent || t.State == model.TargetRequested {
			return true
		}
		if t.State != model.TargetNoReply {
			allNoReply = false
		}
	}
	return allNoReply
}

// AgendaDays is how far ahead a date counts as near for a task that cannot be
// worked yet, an ask, or a meeting.
const AgendaDays = 14

// AgendaLimit is how many items Agenda holds before the open questions that
// block nothing and have no date move behind the OtherOpen count.
const AgendaLimit = 12

// fillAgenda sets the Agenda, OtherOpen, Deferred, and HeldUp of the vault
// from the shared value order, which lists only what needs the owner.
func fillAgenda(ctx context.Context, st *local.Store, sn *ops.Snapshot, date string, vt *VaultToday) error {
	o, err := ops.OrderOf(sn)
	if err != nil {
		return fmt.Errorf("today: agenda: %w", err)
	}
	// Health never fails Today: a vault it cannot read shows no count.
	if h, err := ops.HealthAt(ctx, st, date); err == nil {
		vt.Health = h.Count
	}
	if us, err := upgrade.StatusOf(ctx, st); err == nil {
		ids := map[string]bool{}
		for _, p := range us.Pending {
			ids[p.ID] = true
		}
		vt.UpgradeSteps = len(ids)
	}
	soon := date
	if t, err := time.Parse(dateLayout, date); err == nil {
		soon = t.AddDate(0, 0, AgendaDays).Format(dateLayout)
	}
	// The order lists only what needs the owner (ops.NeedsYou): a task that
	// waits on another item, an ask that waits on a reply, and a meeting not
	// yet near are not in it. Goals shape the order and are not listed;
	// agent work is listed in Work.
	var rows []ops.ValueRow
	for _, r := range o.Rows {
		switch r.Kind {
		case ops.KindIncident, ops.KindQuestion, ops.KindIO, ops.KindTask, ops.KindAsk, ops.KindMeeting:
			rows = append(rows, r)
		}
	}
	other := func(r ops.ValueRow) bool {
		return r.Kind == ops.KindQuestion && r.Group == ops.GroupImpact && r.EffDue == "" && r.Impact == 0 && !r.Proposal
	}
	if len(rows) > AgendaLimit {
		keep := rows[:0:0]
		for _, r := range rows {
			if other(r) {
				vt.OtherOpen++
			} else {
				keep = append(keep, r)
			}
		}
		rows = keep
	}
	for _, r := range rows {
		vt.Agenda.Items = append(vt.Agenda.Items, Item{ID: r.ID, Title: r.Title, Due: r.EffDue, Kind: r.Kind, Why: r.Why, Bucket: r.Bucket, Blocks: r.Blocks, Proposal: r.Proposal,
			Group: r.Group, Effort: r.Effort, Prep: r.Prep, Ready: r.Ready, ReadyKind: r.ReadyKind, ReadyRef: r.ReadyRef})
	}
	vt.Agenda.Count = len(vt.Agenda.Items)
	if err := fillMeetings(ctx, st, sn, date, soon, vt); err != nil {
		return fmt.Errorf("today: agenda: %w", err)
	}
	vt.OrderSummary = o.Summary
	vt.HeldUp = o.Held
	qs, err := ops.QuestionList.Run(ctx, ops.Deps{Store: st}, ops.QuestionListInput{Deferred: true})
	if err != nil {
		return err
	}
	for _, q := range qs.Questions {
		if q.Deferred {
			vt.Deferred++
		}
	}
	ts, err := ops.TaskList.Run(ctx, ops.Deps{Store: st}, ops.TaskListInput{Deferred: true})
	if err != nil {
		return err
	}
	for _, t := range ts.Tasks {
		if t.Deferred {
			vt.Deferred++
		}
	}
	return nil
}

// fillMeetings sets Optional and Skipped: booked meetings from date through
// soon that ask nothing of the owner, by start time.
func fillMeetings(ctx context.Context, st *local.Store, sn *ops.Snapshot, date, soon string, vt *VaultToday) error {
	ms, err := ops.MeetingList.Run(ctx, ops.Deps{Store: st}, ops.MeetingListInput{Status: model.MeetingScheduled})
	if err != nil {
		return err
	}
	for _, m := range ms.Meetings {
		t, err := time.Parse(time.RFC3339, m.Start)
		if err != nil {
			continue
		}
		day := t.In(time.Local).Format(dateLayout)
		if day < date || day > soon || sn.Need(m.ID).Life != ops.LifeOpen {
			continue
		}
		it := Item{ID: m.ID, Title: m.Title, Due: day, Kind: ops.KindMeeting, At: m.Start, Attend: m.Attend, With: append(append([]string{}, m.WithNames...), m.Attendees...)}
		switch {
		case m.Attend == model.AttendNo:
			it.Detail = "skipped"
			vt.Skipped.Items = append(vt.Skipped.Items, it)
		case m.Optional && m.Attend != model.AttendYes:
			it.Detail = "optional"
			vt.Optional.Items = append(vt.Optional.Items, it)
		}
	}
	vt.Optional.Count, vt.Skipped.Count = len(vt.Optional.Items), len(vt.Skipped.Items)
	return nil
}

// approvedNotDone counts a proposal's outward items that wait to be carried out.
// A broken block counts none: proposal_show reports it.
func approvedNotDone(d model.Document) int {
	m, ok := d.Meta.(*model.Proposal)
	if !ok || m.Status == model.ProposalSuperseded {
		return 0
	}
	c, err := model.ParseProposalBody(d.Body)
	if err != nil {
		return 0
	}
	n := 0
	for _, it := range c.Items {
		if it.Kind.Outward() && it.State == model.ItemApproved {
			n++
		}
	}
	return n
}
