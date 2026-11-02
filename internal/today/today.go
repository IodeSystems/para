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
	"github.com/iodesystems/parasolve/internal/registry"
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
	// Questions holds the open questions of the vault, the ones that block the
	// most first. A vault-level question has the scope root.
	Questions Group `json:"questions"`
	Due       Group `json:"due"`     // constraints whose review_by is today or earlier
	Blocked   Group `json:"blocked"` // open tasks with status blocked or a blocked_by entry
	Nudges    Group `json:"nudges"`  // open RFX whose next_nudge is today or earlier
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
		vt := VaultToday{Vault: rv.ID, Incidents: Group{Items: []Item{}}, Questions: Group{Items: []Item{}}, Due: Group{Items: []Item{}}, Blocked: Group{Items: []Item{}},
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
	users := map[string][]string{} // id -> open tasks and requests that depend on it
	for _, r := range refs {
		switch r.Type {
		case model.TypeIncident, model.TypeQuestion, model.TypeTask, model.TypeConstraint, model.TypeRFX, model.TypePolicyException, model.TypePolicy:
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
				scope := m.Scope.Target()
				if m.IsRoot() {
					scope = "root"
				}
				item.Detail = string(m.AsKind()) + " " + scope
				item.Scope, item.ScopeName = scope, titles[scope]
				vt.Questions.Items = append(vt.Questions.Items, item)
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
			if m.Status == model.RFXOpen {
				for _, l := range m.DependsOn {
					users[l.Target()] = append(users[l.Target()], d.ID)
				}
			}
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
			if open {
				for _, l := range m.DependsOn {
					users[l.Target()] = append(users[l.Target()], d.ID)
				}
			}
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
	for i := range vt.Questions.Items {
		it := &vt.Questions.Items[i]
		seen, queue := map[string]bool{it.ID: true}, []string{it.ID}
		for len(queue) > 0 {
			for _, u := range users[queue[0]] {
				if !seen[u] {
					seen[u] = true
					queue = append(queue, u)
					it.Blocks++
				}
			}
			queue = queue[1:]
		}
	}
	sort.Slice(vt.Questions.Items, func(i, j int) bool {
		a, b := vt.Questions.Items[i], vt.Questions.Items[j]
		if a.Blocks != b.Blocks {
			return a.Blocks > b.Blocks
		}
		return a.ID < b.ID
	})
	vt.Questions.Count = len(vt.Questions.Items)
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
