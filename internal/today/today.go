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

	"github.com/iodesystems/paradize/internal/model"
	"github.com/iodesystems/paradize/internal/registry"
	"github.com/iodesystems/paradize/internal/vault"
)

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
	Error   string `json:"error,omitempty"`
}

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
		vt := VaultToday{Vault: rv.ID, Due: Group{Items: []Item{}}, Blocked: Group{Items: []Item{}}}
		if err := readVault(ctx, rv, actor, surface, date, &vt); err != nil {
			vt.Error = err.Error()
		}
		rep.Vaults = append(rep.Vaults, vt)
	}
	return rep, nil
}

func readVault(ctx context.Context, rv registry.Vault, actor, surface, date string, vt *VaultToday) error {
	v, err := vault.Open(rv.Path, vault.Options{ID: rv.ID, Actor: actor, Surface: surface})
	if err != nil {
		return err
	}
	refs, err := v.List(ctx, "", "")
	if err != nil {
		return err
	}
	for _, r := range refs {
		if r.Type != model.TypeTask && r.Type != model.TypeConstraint {
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
		case *model.Task:
			open := m.Status != model.TaskDone && m.Status != model.TaskDropped
			if open && (m.Status == model.TaskBlocked || len(m.BlockedBy) > 0) {
				vt.Blocked.Items = append(vt.Blocked.Items, item)
			}
		}
	}
	for _, g := range []*Group{&vt.Due, &vt.Blocked} {
		sort.Slice(g.Items, func(i, j int) bool { return g.Items[i].ID < g.Items[j].ID })
		g.Count = len(g.Items)
	}
	return nil
}
