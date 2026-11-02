package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/store"
)

const (
	dateLayout = "2006-01-02"
	// WarnNoSelfServe is the warning for an outbound RFX with no self-serve check.
	// SelfServeNone is stored when an outbound RFX has no self-serve check:
	// lint requires a non-empty value.
	SelfServeNone = "none recorded"
	// SelfServeInbound is stored for an inbound RFX: nobody searched before being asked.
	SelfServeInbound = "n/a (inbound)"
	WarnNoSelfServe  = "no self_serve check: record what was searched before asking a person (policies, constraints, prior RFX: `para route`)"
)

// RFXAddInput is the input of rfx_add.
type RFXAddInput struct {
	Direction model.RFXDirection `json:"direction"`            // in | out
	Ask       string             `json:"ask"`                  // first line is the title
	Route     string             `json:"route"`                // domain id
	Targets   []TargetInput      `json:"targets,omitempty"`    // who is asked (out) or who asked (in)
	SelfServe string             `json:"self_serve,omitempty"` // what was checked first
	Project   string             `json:"project,omitempty"`
	Blocks    []string           `json:"blocks,omitempty"`     // ids this RFX blocks
	DependsOn []string           `json:"depends_on,omitempty"` // ids that must resolve first
	NextNudge string             `json:"next_nudge,omitempty"` // YYYY-MM-DD
	Question  string             `json:"question,omitempty"`   // id of the question this request asks; a reply answers it
}

// TargetInput names one target of an RFX.
type TargetInput struct {
	To     string       `json:"to"`              // person or group id
	Medium model.Medium `json:"medium"`          // how it goes
	Value  string       `json:"value,omitempty"` // address or handle; default the person's preferred contact for the medium
}

// RFXAddOutput is the output of rfx_add.
type RFXAddOutput struct {
	ID       string       `json:"id"`
	Folder   model.Folder `json:"folder"`
	Rev      string       `json:"rev"`
	Warnings []string     `json:"warnings"`
}

// RFXAdd records a request.
var RFXAdd = define("rfx_add", "Record a request for something (in or out)", rfxAdd)

func rfxAdd(ctx context.Context, d Deps, in RFXAddInput) (RFXAddOutput, error) {
	if !validDirection(in.Direction) {
		return RFXAddOutput{}, fmt.Errorf("direction %q: want in or out", in.Direction)
	}
	if strings.TrimSpace(in.Route) == "" {
		return RFXAddOutput{}, errors.New("route is empty: name the domain")
	}
	title, body, err := splitCapture(in.Ask)
	if err != nil {
		return RFXAddOutput{}, fmt.Errorf("ask: %w", err)
	}
	if err := optionalDate(in.NextNudge); err != nil {
		return RFXAddOutput{}, fmt.Errorf("next_nudge: %w", err)
	}
	blocks, err := existingLinks(ctx, d, "blocks", in.Blocks)
	if err != nil {
		return RFXAddOutput{}, err
	}
	deps, err := existingLinks(ctx, d, "depends_on", in.DependsOn)
	if err != nil {
		return RFXAddOutput{}, err
	}
	tgts, err := buildTargets(ctx, d, nil, in.Targets)
	if err != nil {
		return RFXAddOutput{}, err
	}
	meta := &model.RFX{
		Common:         model.Common{Type: model.TypeRFX},
		Direction:      in.Direction,
		Status:         model.RFXOpen,
		Route:          strings.TrimSpace(in.Route),
		SelfServeCheck: strings.TrimSpace(in.SelfServe),
		Targets:        tgts,
		Blocks:         blocks,
		DependsOn:      deps,
		NextNudge:      model.Date(in.NextNudge),
	}
	if id := normID(in.Question); id != "" {
		qd, q, err := getQuestion(ctx, d.Store, id)
		if err != nil {
			return RFXAddOutput{}, fmt.Errorf("question %s: %w", id, err)
		}
		if q.Status != model.QuestionOpen {
			return RFXAddOutput{}, fmt.Errorf("question %s is already %s", id, q.Status)
		}
		if in.Direction != model.RFXOut {
			return RFXAddOutput{}, errors.New("a question is asked by an outbound request")
		}
		meta.Question = model.LinkTo(qd.ID)
	}
	if p := normID(in.Project); p != "" {
		links, err := existingLinks(ctx, d, "project", []string{p})
		if err != nil {
			return RFXAddOutput{}, err
		}
		meta.Project = links[0]
	}
	warn := false
	if meta.SelfServeCheck == "" {
		meta.SelfServeCheck = SelfServeInbound
		if in.Direction == model.RFXOut {
			meta.SelfServeCheck, warn = SelfServeNone, true
		}
	}
	doc, err := d.Store.Create(ctx, model.Document{Folder: model.FolderRFX, Title: title, Body: body, Meta: meta})
	if err != nil {
		return RFXAddOutput{}, err
	}
	out := RFXAddOutput{ID: doc.ID, Folder: doc.Folder, Rev: doc.Rev, Warnings: []string{}}
	if warn {
		out.Warnings = append(out.Warnings, WarnNoSelfServe)
	}
	return out, nil
}

// RFXSummary is one RFX in a list.
type RFXSummary struct {
	ID        string             `json:"id"`
	Title     string             `json:"title"`
	Direction model.RFXDirection `json:"direction"`
	Status    model.RFXStatus    `json:"status"`
	Route     string             `json:"route"`
	NextNudge string             `json:"next_nudge"`
	DependsOn []string           `json:"depends_on"`
	Targets   []model.Target     `json:"targets"`
	// Waiting is true while any target is sent and not answered.
	Waiting bool `json:"waiting"`
}

func summarize(id, title string, m *model.RFX) RFXSummary {
	tg := append([]model.Target{}, m.Targets...)
	return RFXSummary{
		ID: id, Title: title, Direction: m.Direction, Status: m.Status, Route: m.Route,
		NextNudge: string(m.NextNudge), DependsOn: targets(m.DependsOn), Targets: tg, Waiting: isWaiting(m),
	}
}

// loadRFX returns every RFX in the vault with its summary, sorted by id.
func loadRFX(ctx context.Context, d Deps) ([]RFXSummary, error) {
	refs, err := d.Store.List(ctx, store.Filter{Type: model.TypeRFX})
	if err != nil {
		return nil, err
	}
	out := make([]RFXSummary, 0, len(refs))
	for _, r := range refs {
		_, m, err := getRFX(ctx, d, r.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, summarize(r.ID, r.Title, m))
	}
	return out, nil
}

// RFXListInput is the input of rfx_list. Empty fields match everything.
type RFXListInput struct {
	Direction model.RFXDirection `json:"direction,omitempty" query:"direction"`
	Status    model.RFXStatus    `json:"status,omitempty" query:"status"`
	Route     string             `json:"route,omitempty" query:"route"`
}

// RFXListOutput is the output of rfx_list.
type RFXListOutput struct {
	Items []RFXSummary `json:"items"`
}

// RFXList lists RFX.
var RFXList = define("rfx_list", "List requests", func(ctx context.Context, d Deps, in RFXListInput) (RFXListOutput, error) {
	all, err := loadRFX(ctx, d)
	if err != nil {
		return RFXListOutput{}, err
	}
	out := RFXListOutput{Items: []RFXSummary{}}
	for _, s := range all {
		if (in.Direction == "" || s.Direction == in.Direction) &&
			(in.Status == "" || s.Status == in.Status) &&
			(in.Route == "" || sameRoute(s.Route, in.Route)) {
			out.Items = append(out.Items, s)
		}
	}
	return out, nil
})

// isWaiting reports whether any target is sent and not yet answered.
func isWaiting(m *model.RFX) bool {
	return slices.ContainsFunc(m.Targets, func(t model.Target) bool { return t.State == model.TargetSent })
}

// allNoReply reports whether there are targets and each one is no_reply.
func allNoReply(ts []model.Target) bool {
	return len(ts) > 0 && !slices.ContainsFunc(ts, func(t model.Target) bool { return t.State != model.TargetNoReply })
}

// RFXWaitingInput is the input of rfx_waiting.
type RFXWaitingInput struct {
	Direction model.RFXDirection `json:"direction,omitempty" query:"direction"` // empty means both
	Today     string             `json:"today,omitempty" query:"today"`         // YYYY-MM-DD; empty means the local date
}

// WaitingItem is an open RFX with its nudge state.
type WaitingItem struct {
	RFXSummary
	Due       bool     `json:"due"`        // next_nudge is today or earlier
	Stalled   bool     `json:"stalled"`    // every target is no_reply: nobody answered, nobody is left to wait on
	BlockedOn []string `json:"blocked_on"` // open RFX it depends on: do not nudge yet
}

// RFXWaitingOutput is the output of rfx_waiting.
type RFXWaitingOutput struct {
	Today string        `json:"today"`
	Items []WaitingItem `json:"items"`
}

// RFXWaiting lists open RFX that wait on someone, earliest next_nudge first:
// any target is sent, no target is recorded, or every target is no_reply
// (flagged Stalled: it stays listed so it is not forgotten). An RFX with no
// next_nudge sorts last. Each item carries its per-target state.
var RFXWaiting = define("rfx_waiting", "List open requests waiting on someone, by next nudge", func(ctx context.Context, d Deps, in RFXWaitingInput) (RFXWaitingOutput, error) {
	today := in.Today
	if today == "" {
		today = time.Now().Format(dateLayout)
	}
	if err := optionalDate(today); err != nil {
		return RFXWaitingOutput{}, fmt.Errorf("today: %w", err)
	}
	all, err := loadRFX(ctx, d)
	if err != nil {
		return RFXWaitingOutput{}, err
	}
	status := map[string]model.RFXStatus{}
	for _, s := range all {
		status[s.ID] = s.Status
	}
	out := RFXWaitingOutput{Today: today, Items: []WaitingItem{}}
	for _, s := range all {
		if s.Status != model.RFXOpen || (in.Direction != "" && s.Direction != in.Direction) ||
			(len(s.Targets) > 0 && !s.Waiting && !allNoReply(s.Targets)) {
			continue
		}
		it := WaitingItem{RFXSummary: s, Due: s.NextNudge != "" && s.NextNudge <= today, Stalled: allNoReply(s.Targets), BlockedOn: []string{}}
		for _, dep := range s.DependsOn {
			if status[dep] == model.RFXOpen {
				it.BlockedOn = append(it.BlockedOn, dep)
			}
		}
		out.Items = append(out.Items, it)
	}
	sort.SliceStable(out.Items, func(i, j int) bool {
		a, b := out.Items[i].NextNudge, out.Items[j].NextNudge
		if (a == "") != (b == "") {
			return b == ""
		}
		return a < b
	})
	return out, nil
})

// RFXShowInput is the input of rfx_show.
type RFXShowInput struct {
	ID string `json:"id" query:"id" required:"true"`
}

// ChainNode is one RFX (or other document) upstream of the shown RFX.
type ChainNode struct {
	ID     string `json:"id"`
	Depth  int    `json:"depth"` // 1 = direct dependency
	Type   string `json:"type"`
	Status string `json:"status"` // RFX status; "missing" for an unresolved link
	Title  string `json:"title"`
}

// RFXShowOutput is the output of rfx_show.
type RFXShowOutput struct {
	Doc          model.Document `json:"doc"`
	Chain        []ChainNode    `json:"chain"`          // everything it depends on, transitively
	DependedOnBy []string       `json:"depended_on_by"` // RFX that list this one in depends_on
	Draft        string         `json:"draft"`          // the message under "## Draft"; empty if none
}

// RFXShow shows one RFX with its dependency chain.
var RFXShow = define("rfx_show", "Show a request and its dependency chain", func(ctx context.Context, d Deps, in RFXShowInput) (RFXShowOutput, error) {
	doc, m, err := getRFX(ctx, d, normID(in.ID))
	if err != nil {
		return RFXShowOutput{}, err
	}
	out := RFXShowOutput{Doc: doc, Chain: []ChainNode{}, DependedOnBy: []string{}, Draft: draftOf(doc.Body)}
	seen := map[string]bool{doc.ID: true}
	var walk func(ids []string, depth int) error
	walk = func(ids []string, depth int) error {
		for _, id := range ids {
			if seen[id] {
				continue
			}
			seen[id] = true
			n := ChainNode{ID: id, Depth: depth, Status: "missing"}
			var next []string
			dd, err := d.Store.Get(ctx, id)
			switch {
			case errors.Is(err, model.ErrNotFound):
			case err != nil:
				return err
			default:
				n.Title, n.Type = dd.Title, string(dd.Meta.Header().Type)
				n.Status = ""
				if r, ok := dd.Meta.(*model.RFX); ok {
					n.Status = string(r.Status)
					next = targets(r.DependsOn)
				}
			}
			out.Chain = append(out.Chain, n)
			if err := walk(next, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(targets(m.DependsOn), 1); err != nil {
		return RFXShowOutput{}, err
	}
	all, err := loadRFX(ctx, d)
	if err != nil {
		return RFXShowOutput{}, err
	}
	for _, s := range all {
		for _, dep := range s.DependsOn {
			if dep == doc.ID {
				out.DependedOnBy = append(out.DependedOnBy, s.ID)
			}
		}
	}
	return out, nil
})

// RFXStatusInput is the input of rfx_status.
type RFXStatusInput struct {
	ID     string          `json:"id"`
	Status model.RFXStatus `json:"status"` // open | answered | declined; use rfx_close for closed
}

// RFXStatusOutput is the output of rfx_status and rfx_close.
type RFXStatusOutput struct {
	ID       string          `json:"id"`
	From     model.RFXStatus `json:"from"`
	Status   model.RFXStatus `json:"status"`
	Playbook string          `json:"playbook"`
	Rev      string          `json:"rev"`
}

// RFXStatus changes the status of an RFX. Closing has its own op, so an
// inbound RFX cannot close without a playbook.
var RFXStatus = define("rfx_status", "Set the status of a request", func(ctx context.Context, d Deps, in RFXStatusInput) (RFXStatusOutput, error) {
	if !validStatus(in.Status) {
		return RFXStatusOutput{}, fmt.Errorf("status %q: want open, answered, closed, or declined", in.Status)
	}
	if in.Status == model.RFXClosed {
		return RFXStatusOutput{}, errors.New("use rfx close to close")
	}
	return setRFX(ctx, d, normID(in.ID), func(m *model.RFX) error {
		m.Status = in.Status
		return nil
	})
})

// RFXCloseInput is the input of rfx_close.
type RFXCloseInput struct {
	ID       string `json:"id"`
	Playbook string `json:"playbook,omitempty"` // required for direction in
}

// RFXClose closes an RFX. An inbound RFX closes by linking a playbook: the
// document that answers the same ask next time without the user. Closing an
// already closed RFX with the same playbook does nothing.
var RFXClose = define("rfx_close", "Close a request; an inbound one needs a playbook", func(ctx context.Context, d Deps, in RFXCloseInput) (RFXStatusOutput, error) {
	pb := normID(in.Playbook)
	if pb != "" {
		if _, err := d.Store.Get(ctx, pb); err != nil {
			return RFXStatusOutput{}, fmt.Errorf("playbook %s: %w", pb, err)
		}
	}
	return setRFX(ctx, d, normID(in.ID), func(m *model.RFX) error {
		if m.Status == model.RFXClosed {
			if m.Playbook.Target() != pb {
				return errors.New("already closed")
			}
			return nil
		}
		if m.Direction == model.RFXIn && pb == "" {
			return errors.New("an inbound request closes by linking a playbook: pass playbook")
		}
		m.Status = model.RFXClosed
		if pb != "" {
			m.Playbook = model.LinkTo(pb)
		}
		return nil
	})
})

// setRFX reads an RFX, applies change, and writes it back.
func setRFX(ctx context.Context, d Deps, id string, change func(*model.RFX) error) (RFXStatusOutput, error) {
	doc, m, err := getRFX(ctx, d, id)
	if err != nil {
		return RFXStatusOutput{}, err
	}
	from := m.Status
	if err := change(m); err != nil {
		return RFXStatusOutput{}, err
	}
	if m.Status != from {
		doc, err = d.Store.Update(ctx, doc)
		if err != nil {
			return RFXStatusOutput{}, err
		}
	}
	return RFXStatusOutput{ID: doc.ID, From: from, Status: m.Status, Playbook: m.Playbook.Target(), Rev: doc.Rev}, nil
}

func getRFX(ctx context.Context, d Deps, id string) (model.Document, *model.RFX, error) {
	if id == "" {
		return model.Document{}, nil, errors.New("id is empty")
	}
	doc, err := d.Store.Get(ctx, id)
	if err != nil {
		return doc, nil, err
	}
	m, ok := doc.Meta.(*model.RFX)
	if !ok {
		return doc, nil, fmt.Errorf("%s is %s, not rfx", id, doc.Meta.Header().Type)
	}
	return doc, m, nil
}

// normID accepts "id" or "[[id]]".
func normID(s string) string { return model.Link(strings.TrimSpace(s)).Target() }

func targets(ls []model.Link) []string {
	out := make([]string, 0, len(ls))
	for _, l := range ls {
		out = append(out, l.Target())
	}
	return out
}

// existingLinks turns ids into links and checks each document exists.
func existingLinks(ctx context.Context, d Deps, field string, ids []string) ([]model.Link, error) {
	var out []model.Link
	for _, raw := range ids {
		id := normID(raw)
		if id == "" {
			return nil, fmt.Errorf("%s: empty id", field)
		}
		if _, err := d.Store.Get(ctx, id); err != nil {
			return nil, fmt.Errorf("%s %s: %w", field, id, err)
		}
		out = append(out, model.LinkTo(id))
	}
	return out, nil
}

func optionalDate(s string) error {
	if s == "" {
		return nil
	}
	return checkDate("date", s)
}

func validDirection(v model.RFXDirection) bool {
	for _, x := range model.RFXDirections {
		if v == x {
			return true
		}
	}
	return false
}

func validStatus(v model.RFXStatus) bool {
	for _, x := range model.RFXStatuses {
		if v == x {
			return true
		}
	}
	return false
}

// sameRoute compares a route to a domain given as id or bare slug.
func sameRoute(a, b string) bool {
	return domainSlug(a) == domainSlug(b)
}

func domainSlug(s string) string {
	return strings.TrimPrefix(strings.ToLower(normID(s)), "domain-")
}
