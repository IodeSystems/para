package ops

import (
	"fmt"
	"slices"

	"github.com/iodesystems/para/internal/model"
	"github.com/iodesystems/para/internal/store"
)

// Health of sources: an active source no agent on this machine declared
// lately, one whose sweep is late, and io items or tracked documents whose
// via.source names no source. Sources are recommended, never required: a
// vault with no source document gets none of these.

// sources adds the findings of source documents.
func (h *healther) sources(s store.Store) error {
	var srcs []model.Document
	for _, d := range h.docs {
		if _, ok := d.Meta.(*model.SourceDoc); ok {
			srcs = append(srcs, d)
		}
	}
	if len(srcs) == 0 {
		return nil
	}
	days, err := reachDays(s)
	if err != nil {
		return err
	}
	decl, err := readDeclarations(s.Info().StateDir)
	if err != nil {
		return err
	}
	cut := reachCut(s.Now(), days)
	for _, d := range srcs {
		m := d.Meta.(*model.SourceDoc)
		if m.Status != model.SourceActive {
			continue
		}
		if !anyReaches(decl, d.ID, cut) {
			fix := fmt.Sprintf("start an agent that reaches %s and have it call agent_sources", d.ID)
			if len(m.ReachedBy) > 0 {
				fix = fmt.Sprintf("start %s (reached_by) and have it call agent_sources with %s", m.ReachedBy[0], d.ID)
			}
			h.add(HealthSourceUnreached, d.ID, "", fmt.Sprintf("no agent on this machine declared it in %d days (sources.reach_days)", days), fix)
		}
		if sourceOverdue(m, h.today) {
			h.add(HealthSourceOverdue, d.ID, "", fmt.Sprintf("last swept %s; cadence %s, due %s", firstOf(dayOf(m.LastScan), "never"), m.Cadence, sourceDue(m)),
				fmt.Sprintf("sweep it: sweep_plan {sources: [%s]}, or pause it with source_status", d.ID))
		}
	}
	recorded := recordedURLs(h.docs)
	for _, d := range srcs {
		for _, o := range unreviewedOutward(d.Body, recorded) {
			h.add(HealthUnreviewedOutward, d.ID, "", fmt.Sprintf("%s at %s (%s): no approved item and no record", o.what, o.url, firstOf(dayOf(o.at), o.at)),
				"if an agent did it, record it with action_record (url, by the agent, on_behalf_of owner) and have the owner check it in receipts; if the owner did it, the owner records it with para action record")
		}
	}
	res := newSourceResolver(srcs)
	var ids []string
	for id := range h.docs {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		via := viaOf(h.docs[id])
		if via == nil || via.Source == "" || res.resolve(via.Source) != "" {
			continue
		}
		h.add(HealthSourceMissing, id, "", fmt.Sprintf("via.source %q names no source document", via.Source),
			fmt.Sprintf("add one with source_add (source_example %s), or point via.source at an existing source", via.Source))
	}
	return nil
}
