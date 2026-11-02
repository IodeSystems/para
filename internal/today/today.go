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

	"github.com/iodesystems/paradize/internal/model"
	"github.com/iodesystems/paradize/internal/registry"
	"github.com/iodesystems/paradize/internal/vault"
)

const dateLayout = "2006-01-02"

// Item is one document that needs attention.
type Item struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// Group is a counted list.
type Group struct {
	Count int    `json:"count"`
	Items []Item `json:"items"`
}

// VaultToday is what one vault has.
type VaultToday struct {
	Vault   string `json:"vault"`
	Due     Group  `json:"due"`     // constraints whose review_by is today or earlier
	Blocked Group  `json:"blocked"` // open tasks with status blocked or a blocked_by entry
	Nudges  Group  `json:"nudges"`  // open RFX whose next_nudge is today or earlier
	// Expiring holds unresolved policy exceptions whose review_by is overdue or
	// within ExpiringDays of today.
	Expiring Group `json:"expiring"`
	// Unverified counts policy summaries no person has reviewed.
	Unverified int `json:"unverified"`
	// Inbox counts captures not yet triaged.
	Inbox int `json:"inbox"`
	// SyncConflicts lists conflict copies a file-sync tool left (finding code
	// sync-conflict in lint). They are not documents.
	SyncConflicts []string `json:"sync_conflicts"`
	// LostUpdates lists documents with a write a sync tool replaced.
	LostUpdates []string `json:"lost_updates"`
	Error       string   `json:"error,omitempty"`
}

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
	vaults, err := in.List()
	if err != nil {
		return Report{}, fmt.Errorf("today: %w", err)
	}
	rep := Report{Date: date, Vaults: []VaultToday{}}
	for _, rv := range vaults {
		vt := VaultToday{Vault: rv.ID, Due: Group{Items: []Item{}}, Blocked: Group{Items: []Item{}},
			Nudges: Group{Items: []Item{}}, Expiring: Group{Items: []Item{}}, SyncConflicts: []string{}, LostUpdates: []string{}}
		if err := readVault(ctx, rv, actor, surface, date, &vt); err != nil {
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
	limit := ""
	if t, err := time.Parse(dateLayout, date); err == nil {
		limit = t.AddDate(0, 0, ExpiringDays).Format(dateLayout)
	}
	for _, r := range refs {
		switch r.Type {
		case model.TypeTask, model.TypeConstraint, model.TypeRFX, model.TypePolicyException, model.TypePolicy:
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
		case *model.Constraint:
			if m.ReviewBy != "" && string(m.ReviewBy) <= date {
				vt.Due.Items = append(vt.Due.Items, item)
			}
		case *model.RFX:
			if m.Status == model.RFXOpen && m.NextNudge != "" && string(m.NextNudge) <= date {
				vt.Nudges.Items = append(vt.Nudges.Items, item)
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
			open := m.Status != model.TaskDone && m.Status != model.TaskDropped
			if open && (m.Status == model.TaskBlocked || len(m.BlockedBy) > 0) {
				vt.Blocked.Items = append(vt.Blocked.Items, item)
			}
		}
	}
	for _, g := range []*Group{&vt.Due, &vt.Blocked, &vt.Nudges, &vt.Expiring} {
		sort.Slice(g.Items, func(i, j int) bool { return g.Items[i].ID < g.Items[j].ID })
		g.Count = len(g.Items)
	}
	return nil
}
