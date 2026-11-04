package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/iodesystems/para/internal/model"
)

// The gate decides whether an agent's write applies directly or waits in the
// review stack. A write applies directly only when it has every property that
// gate.require (vault.properties) names; unset means all three:
//
//	undoable: receipt_undo can reverse the op (the registry says, Entry.Undoable).
//	minor:    the agent rated it minor. Never for an op of risk review.
//	obvious:  the agent rated it obvious. Never with confidence low.
//
// A held-back write does not run. It becomes a para-change item of a dossier,
// and the surface reports it as a success (*Staged). The gate runs on agent
// surfaces only: flow ops, the tick, and writes that an approved item runs
// skip it. An agent cannot change gate.require (vaultconfig.go).

// Gate properties.
const (
	GateUndoable = "undoable"
	GateMinor    = "minor"
	GateObvious  = "obvious"
	gateNone     = "none"

	gateConfig = "gate.require"
)

// GateProperties lists the properties gate.require may name, in order.
var GateProperties = []string{GateUndoable, GateMinor, GateObvious}

// Staged is what a surface returns for a write the gate held back. It is an
// error so the op's own output type need not change; every surface renders it
// as a success.
type Staged struct {
	Staged   bool     `json:"staged"`
	Op       string   `json:"op"`
	Proposal string   `json:"proposal"`
	Item     string   `json:"item"`
	URL      string   `json:"url,omitempty"`
	URLNote  string   `json:"url_note,omitempty"` // url works only here: server.base_url is unset
	Missing  []string `json:"missing"`            // the properties the write lacked
	Message  string   `json:"message"`
}

func (s *Staged) Error() string { return s.Message }

// AsStaged returns the Staged in err, if any.
func AsStaged(err error) (*Staged, bool) {
	var s *Staged
	ok := errors.As(err, &s)
	return s, ok
}

func checkGate(v string) (string, error) {
	list, err := parseGate(v)
	if err != nil {
		return "", err
	}
	if len(list) == 0 {
		return gateNone, nil
	}
	return strings.Join(list, ","), nil
}

// parseGate reads gate.require: "" means all three, none means nothing.
func parseGate(v string) ([]string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return GateProperties, nil
	}
	if v == gateNone {
		return nil, nil
	}
	var out []string
	for _, p := range strings.Split(v, ",") {
		p = strings.TrimSpace(p)
		if !slices.Contains(GateProperties, p) {
			return nil, fmt.Errorf("%q: want a comma list of undoable, minor, obvious, or none", p)
		}
		if !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out, nil
}

// gateVerdict is what an op's own gate rule says about one write.
type gateVerdict int

const (
	gateDefault gateVerdict = iota // gate.require decides
	gatePass                       // the write applies directly
	gateStage                      // the write waits for review, whatever gate.require says
)

// withGateRule sets an op's own gate rule: it reads the input and the
// agent's WriteMeta, and on gateStage names what the write lacks.
func withGateRule[I, O any](o *Op[I, O], rule func(I, WriteMeta) (gateVerdict, string)) *Op[I, O] {
	o.gateRule = rule
	return o
}

// gated reports whether the gate checks this write.
func gated[I, O any](d Deps, o *Op[I, O]) bool {
	if !agentSurface(d) || o.person || o.risk == RiskRead || slices.Contains(FlowOps, o.name) {
		return false
	}
	s := d.Store.Info().Surface
	return s != surfaceProposal && s != TickSurface
}

// gateMissing lists what the write lacks of what gate.require names.
func gateMissing[I, O any](d Deps, o *Op[I, O], m WriteMeta) ([]string, error) {
	raw, err := d.Store.Config(gateConfig)
	if err != nil {
		return nil, err
	}
	need, err := parseGate(raw)
	if err != nil {
		return nil, fmt.Errorf("vault setting %s: %w", gateConfig, err)
	}
	has := map[string]bool{
		GateUndoable: o.Undoable(),
		GateMinor:    m.Minor && o.risk != RiskReview,
		GateObvious:  m.Obvious && m.Confidence != model.ConfidenceLow,
	}
	var out []string
	for _, p := range need {
		if !has[p] {
			out = append(out, p)
		}
	}
	return out, nil
}

// stagedTitle is the title of an actor's default dossier of held-back writes.
func stagedTitle(actor string) string { return "Staged writes " + actor }

// stageWrite appends the write as a para-change item to m.Dossier, or to the
// actor's open "Staged writes" dossier, which it makes when there is none.
func stageWrite(ctx context.Context, d Deps, op string, input map[string]any, m WriteMeta, missing []string, undoable bool) (*Staged, error) {
	info := d.Store.Info()
	if input == nil {
		input = map[string]any{}
	}
	for k, v := range map[string]any{"why": m.Why, "not_duplicate": m.NotDuplicate} {
		if s, _ := v.(string); strings.TrimSpace(s) != "" {
			if _, own := input[k]; !own {
				input[k] = s
			}
		}
	}
	if len(m.Sources) > 0 {
		if _, own := input["sources"]; !own {
			list := make([]any, 0, len(m.Sources))
			for _, s := range m.Sources {
				list = append(list, s)
			}
			input["sources"] = list
		}
	}
	if err := checkOpItem(op, input); err != nil {
		return nil, err
	}
	why := firstOf(strings.TrimSpace(m.Why), "no why given")
	locator := "agent " + info.Actor
	if len(m.Sources) > 0 {
		locator = strings.Join(m.Sources, ", ")
	}
	rev := model.ReversibleYes
	if !undoable {
		rev = model.ReversibleNo
	}
	it := model.ProposalItem{Kind: model.ItemPara, Op: op, Input: input, How: why,
		Impact:   fmt.Sprintf("%s, held for review: the write lacks %s", opWords(op), strings.Join(missing, ", ")),
		Evidence: model.Evidence{Quote: why, Locator: locator}, Group: op, Reversible: rev, State: model.ItemPending,
		NotDuplicate: strings.TrimSpace(m.NotDuplicate)}
	if m.Confidence == model.ConfidenceLow {
		it.Confidence, it.ConfidenceBasis = model.LowConfidence, "the agent said low"
	}
	id := normID(m.Dossier)
	if id == "" {
		var err error
		if id, err = defaultDossier(ctx, d, info.Actor); err != nil {
			return nil, err
		}
	}
	var itemID string
	p, err := changeProposal(ctx, d, id, func(p *proposal) error {
		if p.dossier == nil {
			return fmt.Errorf("%s is not a dossier: give the id of a dossier you filed", p.doc.ID)
		}
		if p.meta.ProposedBy != info.Actor {
			return fmt.Errorf("%s was filed by %s: stage writes in your own dossier", p.doc.ID, p.meta.ProposedBy)
		}
		if p.meta.Status == model.ProposalSuperseded {
			return fmt.Errorf("%s is superseded", p.doc.ID)
		}
		x := it
		x.ID = nextItemID(p.content.Items, "s")
		x.Rev = max(p.meta.Revision, 1)
		itemID = x.ID
		p.content.Items = append(p.content.Items, x)
		return nil
	})
	if err != nil {
		return nil, err
	}
	hash, _ := it.Hash()
	if err := record(ctx, d, "proposal_stage", p.doc.ID, itemID, "", model.ItemPending, hash); err != nil {
		return nil, err
	}
	url, err := reviewURL(d, p.doc.ID)
	if err != nil {
		return nil, err
	}
	return &Staged{Staged: true, Op: op, Proposal: p.doc.ID, Item: itemID, URL: url, URLNote: urlNote(d, url), Missing: missing,
		Message: stagedMessage(p.doc.Title, itemID, firstOf(url, p.doc.ID), missing)}, nil
}

// stagedMessage says where a held write went and when it applies, in words
// an agent relays to the owner as they are: it is not a failure.
func stagedMessage(title, item, where string, missing []string) string {
	return fmt.Sprintf("held for review in %s item %s: %s; it applies when the owner approves (it lacked %s)", title, item, where, strings.Join(missing, ", "))
}

// nextItemID is prefix plus one more than the highest number of that prefix.
func nextItemID(items []model.ProposalItem, prefix string) string {
	n := 0
	for _, it := range items {
		var v int
		if _, err := fmt.Sscanf(it.ID, prefix+"%d", &v); err == nil && it.ID == fmt.Sprintf("%s%d", prefix, v) {
			n = max(n, v)
		}
	}
	return fmt.Sprintf("%s%d", prefix, n+1)
}

// defaultDossier finds the actor's open "Staged writes" dossier, or files one.
func defaultDossier(ctx context.Context, d Deps, actor string) (string, error) {
	all, err := allProposals(ctx, d)
	if err != nil {
		return "", err
	}
	title := stagedTitle(actor)
	for i := len(all) - 1; i >= 0; i-- {
		p := all[i]
		if p.dossier != nil && p.doc.Title == title && p.meta.ProposedBy == actor && p.meta.Status == model.ProposalOpen {
			return p.doc.ID, nil
		}
	}
	dz := model.NewDossier(model.DossierHeader{TLDR: "Writes " + actor + " made that the gate held back for review"}, "Staged writes", nil)
	body, err := dz.Render()
	if err != nil {
		return "", err
	}
	doc, err := d.Store.Create(ctx, model.Document{Folder: model.FolderProposals, Title: title, Body: body,
		Meta: &model.Proposal{Common: model.Common{Type: model.TypeProposal}, Status: model.ProposalOpen, Revision: 1, ProposedBy: actor}})
	if err != nil {
		return "", err
	}
	return doc.ID, nil
}
