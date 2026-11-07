package ops

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
)

// Gaps become agent work. On each tick, an open item that needs the owner, or
// an active goal, that lacks what the owner needs to act on it gets one
// request of kind fill-gap, linked to it, with the gaps in its body:
//
//   - a task (not a meeting) with no done_when, no do-it (do_url, do_command,
//     or steps), or no effort;
//   - a question of kind question (a fact an agent can research) with no
//     proposed answer;
//   - a goal with no brief.
//
// Value order picks which first: the needs-you rows in order, then the goals
// in the order of open work. At most gap.daily_cap (default 10) are made per
// local day. An item with an open fill-gap gets no second one, and an item
// version (model.ItemHash) is asked for once: when the agent could not fill a
// gap and changed nothing, the next tick does not ask again. gap.enabled false
// turns it off. The tick makes them only when it can queue work
// (request.surfaces lists tick): proposed work would ask the owner to approve
// each one, which is the opposite of the point.
//
// The agent's writes are normal undoable writes; the done request is a result
// to check. Fill-gap work is not what the owner asked for: Asked of agents
// lists it in its own group, and the open count leaves it out.

// FillGapKind is the kind of a request to fill the gaps of one item.
const FillGapKind = "fill-gap"

// Vault settings of gap work.
const (
	GapEnabledKey = "gap.enabled"
	GapCapKey     = "gap.daily_cap"
	// DefaultGapCap is how many fill-gap requests a day when unset.
	DefaultGapCap = 10
)

// Gap names, in the order they are listed.
const (
	GapDoneWhen = "done_when"
	GapDoIt     = "do_it"
	GapEffort   = "effort"
	GapAnswer   = "proposed_answer"
	GapBrief    = "brief"
)

// gapHow says what an agent writes for each gap.
var gapHow = map[string]string{
	GapDoneWhen: "done_when: one line that says how we know it is done (task_edit done_when)",
	GapDoIt:     "do_it: where or how the owner does it: do_url (the page), do_command (the exact command), or steps (1 to 5 lines), with task_edit; no secrets",
	GapEffort:   "effort: xs, s, m, or l (task_edit effort)",
	GapAnswer:   "proposed_answer: research the question and propose an answer with sources (question_answer from you proposes; the owner accepts it)",
	GapBrief:    "brief: draft the goal's brief (prompt grill_goal, part Brief)",
}

// itemGaps lists what an item lacks, or nil.
func itemGaps(doc model.Document) []string {
	var out []string
	switch m := doc.Meta.(type) {
	case *model.Task:
		if taskTerminal(m.Status) || m.EffectiveKind() == model.KindMeeting {
			return nil
		}
		if strings.TrimSpace(m.DoneWhen) == "" {
			out = append(out, GapDoneWhen)
		}
		if strings.TrimSpace(m.DoURL) == "" && strings.TrimSpace(m.DoCommand) == "" && strings.TrimSpace(m.Steps) == "" {
			out = append(out, GapDoIt)
		}
		if m.Effort == "" {
			out = append(out, GapEffort)
		}
	case *model.Question:
		if m.Status == model.QuestionOpen && (m.Kind == "" || m.Kind == model.KindQuestion) && !m.HasProposal() {
			out = append(out, GapAnswer)
		}
	case *model.Project:
		if m.Status == model.ProjectActive && !m.HasBrief() {
			out = append(out, GapBrief)
		}
	}
	return out
}

// itemVersionRe finds the item version line of a request body.
var itemVersionRe = regexp.MustCompile(`(?m)^Item version: (\S+)$`)

// itemVersion is the item version a request body records, or "".
func itemVersion(body string) string {
	if m := itemVersionRe.FindStringSubmatch(body); m != nil {
		return m[1]
	}
	return ""
}

// itemWork is the agent work of one kind on items: which items have one open,
// which item versions were asked for, and how many were made on the local day
// of now.
type itemWork struct {
	open  map[string]bool
	tried map[string]bool // item id + " " + item version
	today int
}

// workOf reads the requests of a kind in the snapshot.
func workOf(sn *Snapshot, kind string) itemWork {
	w := itemWork{open: map[string]bool{}, tried: map[string]bool{}}
	day := sn.Now.Format(dateLayout)
	for _, doc := range sn.Docs() {
		r, ok := doc.Meta.(*model.Request)
		if !ok || r.Kind != kind {
			continue
		}
		if t, err := time.Parse(time.RFC3339Nano, r.Queued); err == nil && t.In(sn.Now.Location()).Format(dateLayout) == day {
			w.today++
		}
		v := itemVersion(doc.Body)
		for _, l := range r.Links {
			if requestOpen(r) {
				w.open[l.Target()] = true
			}
			if v != "" {
				w.tried[l.Target()+" "+v] = true
			}
		}
	}
	return w
}

// gapCandidates are the items gap work looks at, best first: the needs-you
// rows in value order (a firing pool's row is its ideas, ranked), then the
// active goals in the order of open work.
func gapCandidates(sn *Snapshot) ([]string, error) {
	o, err := OrderOf(sn)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, r := range o.Rows {
		if r.Kind != KindPool {
			ids = append(ids, r.ID)
			continue
		}
		// A firing pool's row stands for its ideas: they get filled in, best
		// first, when the trigger comes.
		ideas, err := pooledIdeas(sn, r.ID)
		if err != nil {
			return nil, err
		}
		ids = append(ids, ideas...)
	}
	all, err := orderOf(sn, true)
	if err != nil {
		return nil, err
	}
	for _, r := range all.Rows {
		if r.Kind == KindGoal {
			ids = append(ids, r.ID)
		}
	}
	return ids, nil
}

// tickGaps makes the fill-gap requests of this pass.
func tickGaps(ctx context.Context, d Deps, out *TickOutput) error {
	on, err := d.Store.Config(GapEnabledKey)
	if err != nil {
		return err
	}
	if strings.TrimSpace(on) == "false" {
		return nil
	}
	limit, err := configInt(d.Store, GapCapKey, DefaultGapCap)
	if err != nil || limit == 0 {
		return err
	}
	if ok, err := canQueue(d, TickSurface); err != nil || !ok {
		if err == nil {
			out.notQueued()
		}
		return err
	}
	sn, err := NewSnapshot(ctx, d.Store)
	if err != nil {
		return err
	}
	ids, err := gapCandidates(sn)
	if err != nil {
		return err
	}
	return fileGaps(ctx, d, sn, ids, limit, out)
}

// fileGaps makes fill-gap requests for the items, in order, up to limit a
// local day: tick and pool_run.
func fileGaps(ctx context.Context, d Deps, sn *Snapshot, ids []string, limit int, out *TickOutput) error {
	w := workOf(sn, FillGapKind)
	for _, id := range ids {
		if w.today >= limit {
			break
		}
		doc, err := sn.Get(id)
		if err != nil {
			return err
		}
		gaps := itemGaps(doc)
		v := model.ItemHash(doc.Meta, doc.Body)
		if len(gaps) == 0 || w.open[id] || w.tried[id+" "+v] {
			continue
		}
		req, err := addRequest(ctx, d, RequestAddInput{Title: "Fill in: " + shorten(doc.Title), Ask: gapAsk(doc, gaps, v), Kind: FillGapKind, Links: []string{id}}, "")
		if err != nil {
			out.Warnings = append(out.Warnings, fmt.Sprintf("gap %s: %v", id, err))
			continue
		}
		w.open[id], w.tried[id+" "+v] = true, true
		w.today++
		out.Gaps = append(out.Gaps, req.ID)
		out.noteRequest(ctx, d, req.ID)
	}
	return nil
}

// gapAsk is the body of a fill-gap request.
func gapAsk(doc model.Document, gaps []string, version string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Fill in what %s (%s) lacks, so the owner can act on it:\n\n", doc.Title, doc.ID)
	for _, g := range gaps {
		b.WriteString("- " + gapHow[g] + "\n")
	}
	b.WriteString("\nWrite only what the vault or a source you can cite supports. Leave a field you cannot support and say why in request_done. ")
	b.WriteString("Pass request (this request's id) on each write. Change nothing else, and send nothing outward.\n\n")
	b.WriteString("Gaps: " + strings.Join(gaps, ", ") + "\n")
	b.WriteString("Item version: " + version + "\n")
	return b.String()
}
