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
	"time"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/ops"
	"github.com/iodesystems/parasolve/internal/registry"
	"github.com/iodesystems/parasolve/internal/store/local"
	"github.com/iodesystems/parasolve/internal/vault"
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
	// ApprovedNotDone counts messages and actions the owner approved that nobody
	// has reported done.
	ApprovedNotDone int `json:"approved_not_done"`
	// Inbox counts captures not yet triaged.
	Inbox int `json:"inbox"`
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
		vt := VaultToday{Vault: rv.ID, Incidents: Group{Items: []Item{}}, Agenda: Group{Items: []Item{}}, HeldUp: []ops.HeldUp{}, Due: Group{Items: []Item{}}, Blocked: Group{Items: []Item{}},
			Nudges: Group{Items: []Item{}}, Overdue: Group{Items: []Item{}}, DueSoon: Group{Items: []Item{}}, Expiring: Group{Items: []Item{}}, SyncConflicts: []string{}, LostUpdates: []string{}}
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
		switch r.Type {
		case model.TypeIncident, model.TypeQuestion, model.TypeTask, model.TypeConstraint, model.TypeRFX, model.TypePolicyException, model.TypePolicy:
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
		default:
			continue
		}
		d, err := v.Get(ctx, r.ID)
		if err != nil {
			return err
		}
		item := Item{ID: d.ID, Title: d.Title}
		switch m := d.Meta.(type) {
		case *model.Question:
			if m.Status == model.QuestionOpen {
				if m.HasProposal() {
					vt.AnswersToCheck++
				}
			}
		case *model.Incident:
			if m.Status != model.IncidentResolved {
				item.Detail = string(m.Severity) + " " + string(m.Status)
				vt.Incidents.Items = append(vt.Incidents.Items, item)
			}
		case *model.Constraint:
			if m.ReviewBy != "" && string(m.ReviewBy) <= date {
				vt.Due.Items = append(vt.Due.Items, item)
			}
		case *model.RFX:
			for _, t := range m.Targets {
				if n := titles[t.To.Target()]; n != "" {
					item.With = append(item.With, n)
				}
			}
			if m.Status == model.RFXOpen && m.NextNudge != "" && string(m.NextNudge) <= date {
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
		case *model.Task:
			if item.Project = m.Project.Target(); item.Project == "" {
				item.Project = m.Area.Target()
			}
			item.ProjectName = titles[item.Project]
			open := m.Status != model.TaskDone && m.Status != model.TaskDropped
			if open && m.Assignee != "" && m.Assignee != "self" {
				vt.Waiting++
			}
			if open && (m.Status == model.TaskBlocked || len(m.BlockedBy) > 0) {
				vt.Blocked.Items = append(vt.Blocked.Items, item)
			}
			if open && m.Due != "" {
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
	if err := fillAgenda(ctx, rv, date, vt); err != nil {
		return err
	}
	for _, g := range []*Group{&vt.Due, &vt.Blocked, &vt.Nudges, &vt.Expiring, &vt.Overdue, &vt.DueSoon} {
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
// from the shared value order.
func fillAgenda(ctx context.Context, rv registry.Vault, date string, vt *VaultToday) error {
	st, err := local.Open(rv.Path, vault.Options{ID: rv.ID, ReadOnly: true})
	if err != nil {
		return fmt.Errorf("today: agenda: %w", err)
	}
	o, err := ops.OrderAt(ctx, st, date)
	if err != nil {
		return fmt.Errorf("today: agenda: %w", err)
	}
	// Health never fails Today: a vault it cannot read shows no count.
	if h, err := ops.HealthAt(ctx, st, date); err == nil {
		vt.Health = h.Count
	}
	ready, err := ops.Next.Run(ctx, ops.Deps{Store: st}, ops.NextInput{})
	if err != nil {
		return fmt.Errorf("today: agenda: %w", err)
	}
	isReady := map[string]bool{}
	for _, t := range ready.Tasks {
		isReady[t.ID] = true
	}
	soon := date
	if t, err := time.Parse(dateLayout, date); err == nil {
		soon = t.AddDate(0, 0, AgendaDays).Format(dateLayout)
	}
	// Goals shape the order and are not listed. A task shows when it can be
	// worked or its date is near; the rest of the tasks wait behind what blocks
	// them, which is listed. Asks and meetings show when their date is near.
	var rows []ops.ValueRow
	for _, r := range o.Rows {
		switch r.Kind {
		case ops.KindIncident, ops.KindQuestion:
		case ops.KindTask:
			if !isReady[r.ID] && (r.EffDue == "" || r.EffDue > soon) {
				continue
			}
		case ops.KindAsk, ops.KindMeeting:
			if r.EffDue == "" || r.EffDue > soon {
				continue
			}
		default:
			continue
		}
		rows = append(rows, r)
	}
	other := func(r ops.ValueRow) bool {
		return r.Kind == ops.KindQuestion && r.Group == ops.GroupUndated && r.Impact == 0 && !r.Proposal
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
		vt.Agenda.Items = append(vt.Agenda.Items, Item{ID: r.ID, Title: r.Title, Due: r.EffDue, Kind: r.Kind, Why: r.Why, Blocks: r.Blocks, Proposal: r.Proposal})
	}
	vt.Agenda.Count = len(vt.Agenda.Items)
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

// approvedNotDone counts a proposal's outward items that wait to be carried out.
// A broken block counts none: Review reports it.
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
