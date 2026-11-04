package ops

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/iodesystems/para/internal/model"
)

// Dossiers: a proposal whose body is markdown with typed blocks (model.Dossier).
// The agent writes the whole body (proposal_add with markdown) and revises it in
// place (proposal_revise with markdown). The owner answers in the blocks
// (proposal_draft, then proposal_submit), and the agent reads the answers with
// one call (proposal_response), claims what it carries out (proposal_claim), and
// reports it (proposal_executed). Decisions are in dossierdecide.go.

// paraOwned lists what an agent may not write in a block, by what it holds.
func paraOwned(it model.ProposalItem) []string {
	var out []string
	add := func(on bool, name string) {
		if on {
			out = append(out, name)
		}
	}
	add(it.Answer != nil, "answer")
	add(it.State != "", "state")
	add(it.Execution != nil, "exec")
	add(it.Rev != 0, "rev")
	add(it.Reason != "", "reason")
	add(it.Edited, "edited")
	add(it.ApprovedHash != "", "approved_hash")
	add(it.Result != "", "result")
	add(it.Waits, "waits")
	add(it.ClaimedBy != "" || it.LeaseUntil != "", "claimed_by")
	add(it.Decided != nil, "decided")
	add(len(it.Thread) > 0, "thread")
	return out
}

// clearParaOwned drops what para and the owner own from an agent's block.
func clearParaOwned(it *model.ProposalItem) {
	it.Answer, it.State, it.Execution, it.Rev, it.Reason, it.Edited = nil, "", nil, 0, "", false
	it.ApprovedHash, it.Result, it.Waits, it.ClaimedBy, it.LeaseUntil, it.Decided, it.Thread = "", "", false, "", "", nil, nil
}

// checkBlockItem checks one agent-written item beyond the grammar: an op item
// names a real op with its exact input, claims cite sources, and the subtype
// rules of .para/proposals.yaml hold.
func checkBlockItem(ctx context.Context, d Deps, it *model.ProposalItem, rules ProposalRules) error {
	if err := it.Check(); err != nil {
		return err
	}
	for _, o := range it.OptionDetails {
		if err := o.Check(); err != nil {
			return err
		}
	}
	if it.Reversible != "" && !slices.Contains(model.Reversibles, it.Reversible) {
		return fmt.Errorf("reversible %q: want yes, no, or partly", it.Reversible)
	}
	it.For = normID(it.For)
	switch it.Kind {
	case model.ItemPara:
		if strings.TrimSpace(it.How) == "" || strings.TrimSpace(it.Impact) == "" {
			return errors.New("a change states the change in how and its effect in impact")
		}
		if err := checkOpItem(it.Op, it.Input); err != nil {
			return err
		}
		_, meta, _ := splitOpInput(it.Op, it.Input)
		it.NotDuplicate = strings.TrimSpace(firstOf(it.NotDuplicate, meta.NotDuplicate))
		if err := checkProposedAnswer(ctx, d, *it); err != nil {
			return err
		}
		if agentSurface(d) {
			if err := checkCreate(ctx, d.Store, it.Op, cleanInput(it.Op, it.Input), it.NotDuplicate); err != nil {
				return err
			}
		}
	case model.ItemMessage, model.ItemAction:
		if err := checkItemTarget(it.GateTarget()); err != nil {
			return err
		}
	}
	return rules.apply(it)
}

// parseAgentDossier reads markdown an agent wrote and checks every item.
// strict refuses para's and the owner's keys; otherwise they are dropped.
func parseAgentDossier(ctx context.Context, d Deps, markdown string, strict bool) (*model.Dossier, error) {
	dz, err := model.ParseDossier(strings.TrimSpace(markdown))
	if err != nil {
		return nil, err
	}
	if dz.Header.Response != nil || len(dz.Header.Replies) > 0 {
		if strict {
			return nil, errors.New("para-dossier: response and replies are the owner's and para's; leave them out")
		}
		dz.Header.Response, dz.Header.Replies = nil, nil
	}
	rules, err := loadProposalRules(d)
	if err != nil {
		return nil, err
	}
	for i := range dz.Items {
		it := &dz.Items[i]
		if owned := paraOwned(*it); len(owned) > 0 {
			if strict {
				return nil, fmt.Errorf("item %s: %s are para's and the owner's; leave them out", it.ID, strings.Join(owned, ", "))
			}
			clearParaOwned(it)
		}
		if err := checkBlockItem(ctx, d, it, rules); err != nil {
			return nil, fmt.Errorf("item %s: %w", it.ID, err)
		}
	}
	return dz, nil
}

// fileDossier writes a new dossier: every item pending at rev 1.
func fileDossier(ctx context.Context, d Deps, title string, source model.Link, sourcePath, actor string, dz *model.Dossier) (model.Document, error) {
	for i := range dz.Items {
		dz.Items[i].State, dz.Items[i].Rev = model.ItemPending, 1
	}
	if err := checkPlan(dz.Items); err != nil {
		return model.Document{}, err
	}
	if err := checkItemLinks(ctx, d, dz.Items); err != nil {
		return model.Document{}, err
	}
	body, err := dz.Render()
	if err != nil {
		return model.Document{}, err
	}
	status := model.ProposalOpen
	if len(dz.Items) > 0 {
		status = proposalStatus(dz.Items)
	}
	return d.Store.Create(ctx, model.Document{
		Folder: model.FolderProposals, Title: title, Body: body,
		Meta: &model.Proposal{Common: model.Common{Type: model.TypeProposal}, Source: source, SourcePath: sourcePath,
			Status: status, Revision: 1, ProposedBy: actor},
	})
}

// reviseInPlace rewrites a proposal as its next revision, in the same file. A
// file in the old form becomes a dossier. The body before goes to a receipt
// (op proposal_revise), which proposal_diff reads.
func reviseInPlace(ctx context.Context, d Deps, id, why string, build func(p *proposal, rev int) error) (*proposal, error) {
	for range claimRetries {
		p, err := loadProposal(ctx, d, id)
		if err != nil {
			return nil, err
		}
		if p.meta.Status == model.ProposalSuperseded {
			return nil, fmt.Errorf("%s is superseded: revise the latest revision", p.doc.ID)
		}
		before := renderText(p.doc)
		rev := p.meta.Revision + 1
		if err := build(p, rev); err != nil {
			return nil, err
		}
		if err := checkPlan(p.content.Items); err != nil {
			return nil, err
		}
		p.meta.Revision = rev
		if r := p.dossier; r != nil && r.Header.Response != nil && r.Header.Response.State == model.ResponseSubmitted {
			r.Header.Response.State = model.ResponseDraft
		}
		err = p.save(ctx, d)
		if errors.Is(err, model.ErrConflict) {
			continue
		}
		if err != nil {
			return nil, err
		}
		info := d.Store.Info()
		if err := fileReceipt(ctx, d, receiptInput{op: "proposal_revise", doc: p.doc.ID, title: p.doc.Title,
			meta:    WriteMeta{Why: firstOf(why, fmt.Sprintf("revision %d", rev)), Request: heldRequest(ctx, d.Store, info.Actor)},
			content: model.ReceiptContent{Folder: model.FolderProposals, Before: before, After: renderText(p.doc)}}); err != nil {
			return nil, err
		}
		return p, nil
	}
	return nil, fmt.Errorf("could not revise %s after %d tries: it keeps changing", id, claimRetries)
}

// mergeRevision builds the items of the next revision from the agent's new
// markdown. An item whose text is unchanged keeps its answer and state. A
// changed item waits for the owner again, its answer kept and marked stale. A
// decided item cannot change, and an item that is gone is withdrawn.
func mergeRevision(p *proposal, next *model.Dossier, rev int) error {
	prev := p.content.Items
	byID := map[string]model.ProposalItem{}
	for _, it := range prev {
		byID[it.ID] = it
	}
	out := make([]model.ProposalItem, 0, len(next.Items))
	seen := map[string]bool{}
	for _, it := range next.Items {
		seen[it.ID] = true
		old, ok := byID[it.ID]
		if !ok {
			it.State, it.Rev = model.ItemPending, rev
			out = append(out, it)
			continue
		}
		oldHash, err := old.Hash()
		if err != nil {
			return err
		}
		newHash, err := it.Hash()
		if err != nil {
			return err
		}
		if oldHash == newHash {
			keepParaOwned(&it, old)
			out = append(out, it)
			continue
		}
		if !revisable(old.State) {
			return fmt.Errorf("item %s is %s: the owner has decided it, and it cannot change", it.ID, old.State)
		}
		it.State, it.Rev, it.Thread = model.ItemPending, rev, old.Thread
		if old.Answer != nil {
			a := *old.Answer
			a.Stale = true
			it.Answer = &a
		}
		out = append(out, it)
	}
	for _, old := range prev {
		if seen[old.ID] {
			continue
		}
		if revisable(old.State) {
			old.State, old.Reason = model.ItemWithdrawn, "Removed in revision "+fmt.Sprint(rev)
		}
		out = append(out, old)
	}
	for i := range out {
		if out[i].State == model.ItemSentBack || out[i].State == model.ItemHalted {
			out[i].State, out[i].Reason = model.ItemPending, ""
		}
	}
	next.Header.Response = nil
	if p.dossier != nil {
		next.Header.Response, next.Header.Replies = p.dossier.Header.Response, p.dossier.Header.Replies
	}
	next.Items = out
	p.dossier = next
	p.content = next.Content()
	return nil
}

func keepParaOwned(it *model.ProposalItem, old model.ProposalItem) {
	it.Answer, it.State, it.Execution, it.Rev, it.Reason, it.Edited = old.Answer, old.State, old.Execution, old.Rev, old.Reason, old.Edited
	it.ApprovedHash, it.Result, it.Waits, it.ClaimedBy, it.LeaseUntil, it.Decided, it.Thread = old.ApprovedHash, old.Result, old.Waits, old.ClaimedBy, old.LeaseUntil, old.Decided, old.Thread
}

// ResponseItem is the owner's answer to one item, as the agent reads it.
type ResponseItem struct {
	ID    string          `json:"id"`
	Kind  model.ItemKind  `json:"kind"`
	State model.ItemState `json:"state"`
	Stack string          `json:"stack"`
	Title string          `json:"title,omitempty"`
	For   string          `json:"for,omitempty"`
	// Decision is the owner's: approve, reject, or edit from an answer, or the
	// decision para recorded.
	Decision string            `json:"decision,omitempty"`
	Answer   *model.ItemAnswer `json:"answer,omitempty"`
	// Submitted is false while the answer is a draft: not a decision yet.
	Submitted bool   `json:"submitted"`
	Reason    string `json:"reason,omitempty"`
	Result    string `json:"result,omitempty"` // what an op para ran made
	// Receipts are the receipts of what para ran for the item.
	Receipts  []string         `json:"receipts,omitempty"`
	Execution *model.Execution `json:"execution,omitempty"`
	ClaimedBy string           `json:"claimed_by,omitempty"`
	Replies   []model.Turn     `json:"replies,omitempty"`
}

// ResponseTodo is what the agent does next.
type ResponseTodo struct {
	// Claim lists approved outward items nobody holds: claim, carry out, report.
	Claim []string `json:"claim"`
	// Answered lists answered questions with no for: act on the answer.
	Answered []string `json:"answered"`
	// Reply lists comments with no reply yet: an item id, a section id, or
	// "overall".
	Reply []string `json:"reply"`
}

// ProposalResponseOutput is the output of proposal_response.
type ProposalResponseOutput struct {
	ID        string                  `json:"id"`
	Title     string                  `json:"title"`
	Status    model.ProposalStatus    `json:"status"`
	Revision  int                     `json:"revision"`
	Response  string                  `json:"response"` // none, draft, or submitted
	Submitted string                  `json:"submitted_at,omitempty"`
	Overall   string                  `json:"overall,omitempty"`
	Sections  map[string]string       `json:"sections,omitempty"`
	Replies   map[string][]model.Turn `json:"replies,omitempty"`
	Items     []ResponseItem          `json:"items"`
	Todo      ResponseTodo            `json:"todo"`
	Untrusted string                  `json:"untrusted"`
	Rev       string                  `json:"rev"`
}

// ProposalResponse reads the owner's response to a dossier in one call.
var ProposalResponse = defineRead("proposal_response", "Read the owner's response to a dossier in one call: per item the state, decision, edited text or input, choices, added or edited options, own words, and comment; section and overall comments; receipts of what para ran; and todo: approved outward items to claim, answered questions to act on, and comments to reply to. A draft answer is not a decision until the owner submits", func(ctx context.Context, d Deps, in ProposalIDInput) (ProposalResponseOutput, error) {
	p, err := loadProposal(ctx, d, in.ID)
	if err != nil {
		return ProposalResponseOutput{}, err
	}
	out := ProposalResponseOutput{ID: p.doc.ID, Title: p.doc.Title, Status: p.meta.Status, Revision: p.meta.Revision, Response: "none",
		Items: []ResponseItem{}, Todo: ResponseTodo{Claim: []string{}, Answered: []string{}, Reply: []string{}}, Untrusted: UntrustedNotice, Rev: p.doc.Rev}
	submitted := false
	if p.dossier != nil {
		if r := p.dossier.Header.Response; r != nil {
			out.Response = firstOf(r.State, model.ResponseDraft)
			out.Submitted, out.Overall, out.Sections = r.SubmittedAt, r.Overall, r.Sections
			submitted = r.State == model.ResponseSubmitted
			if strings.TrimSpace(r.Overall) != "" && len(p.dossier.Header.Replies[model.OverallReply]) == 0 {
				out.Todo.Reply = append(out.Todo.Reply, model.OverallReply)
			}
			for _, s := range p.dossier.Sections {
				if strings.TrimSpace(r.Sections[s.ID]) != "" && len(p.dossier.Header.Replies[s.ID]) == 0 {
					out.Todo.Reply = append(out.Todo.Reply, s.ID)
				}
			}
		}
		out.Replies = p.dossier.Header.Replies
	}
	receipts, err := itemReceipts(ctx, d, p.doc.ID)
	if err != nil {
		return out, err
	}
	now := d.Store.Now()
	for _, it := range p.content.Items {
		ri := ResponseItem{ID: it.ID, Kind: it.Kind, State: it.State, Stack: stackState(it), Title: it.Title, For: it.For, Answer: it.Answer,
			Submitted: submitted && it.Answer != nil, Reason: it.Reason, Result: it.Result, Receipts: receipts[it.ID], Execution: it.Execution,
			ClaimedBy: it.ClaimedBy}
		for _, t := range it.Thread {
			if t.Kind == model.TurnReply {
				ri.Replies = append(ri.Replies, t)
			}
		}
		if it.Decided != nil {
			ri.Decision = it.Decided.Decision
		}
		if it.Answer != nil && it.Answer.Decision != "" {
			ri.Decision = it.Answer.Decision
		}
		if it.Execution != nil && it.Execution.Receipt != "" {
			ri.Receipts = append(ri.Receipts, it.Execution.Receipt)
		}
		switch {
		case it.Kind.Outward() && itemClaimable(it, now):
			out.Todo.Claim = append(out.Todo.Claim, it.ID)
		case it.Kind == model.ItemQuestion && it.State == model.ItemAnswered && it.For == "":
			out.Todo.Answered = append(out.Todo.Answered, it.ID)
		}
		if submitted && it.Answer != nil && strings.TrimSpace(it.Answer.Comment) != "" && len(ri.Replies) == 0 {
			out.Todo.Reply = append(out.Todo.Reply, it.ID)
		}
		out.Items = append(out.Items, ri)
	}
	return out, nil
})

// proposalRef is how a receipt of an item para ran names the item: one of its
// sources.
func proposalRef(id, item string) string { return id + "#" + item }

// itemReceipts maps item ids to the receipts of what para ran for them.
func itemReceipts(ctx context.Context, d Deps, id string) (map[string][]string, error) {
	list, err := listReceipts(ctx, d.Store)
	if err != nil {
		return nil, err
	}
	out := map[string][]string{}
	prefix := id + "#"
	for _, r := range list {
		for _, s := range r.Sources {
			if item, ok := strings.CutPrefix(s, prefix); ok {
				out[item] = append(out[item], r.ID)
			}
		}
	}
	return out, nil
}

// itemClaimable reports whether an approved outward item waits for an agent: not
// claimed, or its lease ran out.
func itemClaimable(it model.ProposalItem, now time.Time) bool {
	switch it.State {
	case model.ItemApproved:
		return true
	case model.ItemClaimed:
		return leaseOver(it, now)
	}
	return false
}

func leaseOver(it model.ProposalItem, now time.Time) bool {
	until, err := time.Parse(time.RFC3339, it.LeaseUntil)
	return err != nil || !now.Before(until)
}

// ProposalClaimInput is the input of proposal_claim.
type ProposalClaimInput struct {
	ID    string   `json:"id"`
	Items []string `json:"items,omitempty"`
	Group string   `json:"group,omitempty"` // every claimable item of this group
	// LeaseSeconds is how long the claim holds; default 3600. When it runs out,
	// another agent may claim the item.
	LeaseSeconds int `json:"lease_seconds,omitempty"`
}

// ProposalClaimOutput is the output of proposal_claim.
type ProposalClaimOutput struct {
	ID         string   `json:"id"`
	Claimed    []string `json:"claimed"`
	LeaseUntil string   `json:"lease_until"`
	Rev        string   `json:"rev"`
}

const defaultLease = time.Hour

// ProposalClaim takes approved outward items to carry out.
var ProposalClaim = defineSafe("proposal_claim", "Claim approved outward items of a dossier (by ids or group) before you carry them out with your own tools, so no other agent does them too. The claim holds for lease_seconds (default 3600). Then report each with proposal_executed", func(ctx context.Context, d Deps, in ProposalClaimInput) (ProposalClaimOutput, error) {
	info, err := identity(d)
	if err != nil {
		return ProposalClaimOutput{}, err
	}
	if len(in.Items) == 0 && in.Group == "" {
		return ProposalClaimOutput{}, errors.New("no items: give items or group")
	}
	if in.LeaseSeconds < 0 {
		return ProposalClaimOutput{}, errors.New("lease_seconds is negative")
	}
	lease := defaultLease
	if in.LeaseSeconds > 0 {
		lease = time.Duration(in.LeaseSeconds) * time.Second
	}
	now := d.Store.Now()
	until := now.Add(lease).UTC().Format(time.RFC3339)
	var claimed []string
	p, err := changeProposal(ctx, d, in.ID, func(p *proposal) error {
		claimed = nil
		ids := slices.Clone(in.Items)
		if in.Group != "" {
			for _, it := range p.content.Items {
				if it.Group == in.Group && it.Kind.Outward() && itemClaimable(it, now) {
					ids = append(ids, it.ID)
				}
			}
			if len(ids) == len(in.Items) {
				return fmt.Errorf("group %q has no item to claim", in.Group)
			}
		}
		for _, id := range ids {
			it, err := p.item(id)
			if err != nil {
				return err
			}
			if !it.Kind.Outward() {
				return fmt.Errorf("item %s is a %s: para runs vault changes itself", id, it.Kind)
			}
			if it.State == model.ItemClaimed && it.ClaimedBy == info.Actor {
				it.LeaseUntil = until
				claimed = append(claimed, id)
				continue
			}
			if !itemClaimable(*it, now) {
				return fmt.Errorf("item %s is %s%s: only an approved item can be claimed", id, it.State, heldBy(*it))
			}
			if !afterDone(p, it) {
				return fmt.Errorf("item %s runs after %s, which is not done", id, it.After)
			}
			if h, err := it.Hash(); err != nil || h != it.ApprovedHash {
				return fmt.Errorf("item %s changed since it was approved", id)
			}
			it.State, it.ClaimedBy, it.LeaseUntil = model.ItemClaimed, info.Actor, until
			claimed = append(claimed, id)
		}
		return nil
	})
	if err != nil {
		return ProposalClaimOutput{}, err
	}
	for _, id := range claimed {
		if err := record(ctx, d, "proposal_claim", p.doc.ID, id, model.ItemApproved, model.ItemClaimed, ""); err != nil {
			return ProposalClaimOutput{}, err
		}
	}
	return ProposalClaimOutput{ID: p.doc.ID, Claimed: claimed, LeaseUntil: until, Rev: p.doc.Rev}, nil
})

func heldBy(it model.ProposalItem) string {
	if it.ClaimedBy != "" && it.State == model.ItemClaimed {
		return " by " + it.ClaimedBy + " until " + it.LeaseUntil
	}
	return ""
}

// ProposalReplyInput is the input of proposal_reply.
type ProposalReplyInput struct {
	ID      string `json:"id"`
	Item    string `json:"item,omitempty"`
	Section string `json:"section,omitempty"` // a section id, or "overall"
	Text    string `json:"text"`
}

// ProposalReplyOutput is the output of proposal_reply.
type ProposalReplyOutput struct {
	ID    string `json:"id"`
	Reply string `json:"reply"`
	Rev   string `json:"rev"`
}

// ProposalReply answers an owner's comment on an item, a section, or the whole.
var ProposalReply = defineSafe("proposal_reply", "Reply to the owner's comment on a dossier item (item), a section (section, its heading id), or the whole (section overall). Keep it short; it is stored in the file", func(ctx context.Context, d Deps, in ProposalReplyInput) (ProposalReplyOutput, error) {
	info, err := identity(d)
	if err != nil {
		return ProposalReplyOutput{}, err
	}
	text := strings.TrimSpace(in.Text)
	if text == "" {
		return ProposalReplyOutput{}, errors.New("text is empty")
	}
	if (in.Item == "") == (in.Section == "") {
		return ProposalReplyOutput{}, errors.New("give item or section, not both")
	}
	var turnID string
	p, err := changeProposal(ctx, d, in.ID, func(p *proposal) error {
		if p.dossier == nil {
			return fmt.Errorf("%s is not a dossier", p.doc.ID)
		}
		turn := model.Turn{Kind: model.TurnReply, Text: text, By: info.Actor, Revision: p.meta.Revision, State: model.TurnNoted}
		if in.Item != "" {
			it, err := p.item(in.Item)
			if err != nil {
				return err
			}
			turn.ID = fmt.Sprintf("r%d", len(it.Thread)+1)
			it.Thread = append(it.Thread, turn)
			turnID = in.Item + ":" + turn.ID
			return nil
		}
		if in.Section != model.OverallReply && !slices.ContainsFunc(p.dossier.Sections, func(s model.DossierSection) bool { return s.ID == in.Section }) {
			return fmt.Errorf("%s has no section %q", p.doc.ID, in.Section)
		}
		if p.dossier.Header.Replies == nil {
			p.dossier.Header.Replies = map[string][]model.Turn{}
		}
		turn.ID = fmt.Sprintf("r%d", len(p.dossier.Header.Replies[in.Section])+1)
		p.dossier.Header.Replies[in.Section] = append(p.dossier.Header.Replies[in.Section], turn)
		turnID = in.Section + ":" + turn.ID
		return nil
	})
	if err != nil {
		return ProposalReplyOutput{}, err
	}
	if err := d.Store.Record(ctx, "proposal_reply", p.doc.ID, "", turnID, ""); err != nil {
		return ProposalReplyOutput{}, err
	}
	return ProposalReplyOutput{ID: p.doc.ID, Reply: turnID, Rev: p.doc.Rev}, nil
})
