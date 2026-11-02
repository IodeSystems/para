package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/ops"
)

// Item actions on a proposal page. Only what the review rule allows for the
// item under the cursor is shown or bound: ops.ItemView.Actions is the rule.
// The verbs are Approve, Deny, Request, Needs first, Edit.
var (
	actApprove   = Target{ID: "act:approve", Label: "Approve", Pref: 'a'}
	actDeny      = Target{ID: "act:deny", Label: "Deny", Pref: 'd'}
	actRequest   = Target{ID: "act:request", Label: "Request", Pref: 'r'}
	actHold      = Target{ID: "act:hold", Label: "Needs first", Pref: 'f'}
	actEdit      = Target{ID: "act:edit", Label: "Edit", Pref: 'e'}
	actSource    = Target{ID: "act:source", Label: "Source", Pref: 'o'}
	actDiff      = Target{ID: "act:diff", Label: "Diff", Pref: 'i'}
	actHistory   = Target{ID: "act:history", Label: "History", Pref: 'h'}
	actCopyDraft = Target{ID: "act:copydraft", Label: "Copy draft", Pref: 'y'}

	// itemActs is every action of a proposal page, in the one order that
	// gives their keys. The footer shows the ones that apply.
	itemActs = []Target{actApprove, actDeny, actRequest, actHold, actEdit, actSource, actDiff, actHistory, actCopyDraft}
)

// itemAction is the rule's action behind a footer action, or "" for a view.
func applies(it ops.ItemView, t Target) bool {
	has := func(a string) bool { return slices.Contains(it.Actions, a) }
	switch t.ID {
	case actApprove.ID:
		return has(ops.ActionApprove)
	case actDeny.ID:
		return has(ops.ActionDeny)
	case actRequest.ID:
		return has(ops.ActionRequest)
	case actHold.ID:
		return has(ops.ActionHold)
	case actEdit.ID:
		return has(ops.ActionEdit)
	case actCopyDraft.ID:
		return it.Kind.Outward() && it.State == model.ItemApproved
	}
	return false
}

// External items: what approving means, in plain words.
const (
	externalPromise = "Approving marks this OK to send. Para does not send it: send it yourself, or tell the agent to."
	notSentYet      = "Approved. Not sent yet."
	coversText      = "Approval covers this exact text."
)

// whyNot says why an item action is not available, in words.
var whyNot = map[string]string{
	"approve": "Approve is not available: the item was edited, has an open request, or is already decided.",
	"deny":    "Deny is not available: the item was edited or has an open request, or is already decided.",
	"hold":    "Needs first is not available for this item now.",
	"request": "Request is not available for this item now.",
	"edit":    "Edit is not available for this item now.",
}

type (
	proposalMsg struct {
		id     string
		out    ops.ProposalDetail
		err    error
		reload bool
	}
	textMsg struct {
		title string
		lines []string
		err   error
	}
	// editedMsg is the end of an $EDITOR run.
	editedMsg struct {
		id   string
		item ops.ItemView
		path string
		err  error
	}
)

func (m *Model) loadProposal(id string, reload bool) tea.Cmd {
	env := m.env
	return func() tea.Msg {
		o, err := ops.ProposalShow.Run(context.Background(), env.Deps, ops.ProposalIDInput{ID: id})
		return proposalMsg{id: id, out: o, err: err, reload: reload}
	}
}

func (m *Model) onProposal(msg proposalMsg) tea.Cmd {
	if msg.err != nil {
		if !msg.reload {
			m.setStatus(msg.err.Error(), true)
		}
		return nil
	}
	if msg.reload {
		if p := m.top(); p != nil && p.kind == pageProposal && p.id == msg.id {
			p.prop = msg.out
			if m.advanceFrom != "" {
				m.advanceCursor(p, m.advanceFrom)
				m.advanceFrom = ""
			}
		}
		return nil
	}
	m.push(page{kind: pageProposal, id: msg.id, title: msg.out.Proposal.Title, prop: msg.out})
	return nil
}

// advanceCursor moves the cursor of a proposal page to the next item that
// still waits after item, wrapping to the first one. With none, it stays.
func (m *Model) advanceCursor(p *page, item string) {
	items := p.prop.Items
	at := slices.IndexFunc(items, func(it ops.ItemView) bool { return it.ID == item })
	for n := 1; n <= len(items); n++ {
		next := items[(at+n+len(items))%len(items)]
		if next.State != model.ItemPending || next.ID == item {
			continue
		}
		for i, r := range m.rowsProposal(p, m.listWidth()) {
			if r.sel && r.id == next.ID {
				p.cursor = i
				return
			}
		}
	}
}

// rowsProposals is the top of the Review tab: proposals that wait for the
// owner, then those that wait for an agent.
func (m *Model) rowsProposals(w int) []row {
	var open, back []ops.ProposalView
	for _, p := range m.snap.Proposals {
		switch p.Status {
		case model.ProposalOpen:
			open = append(open, p)
		case model.ProposalSentBack:
			back = append(back, p)
		}
	}
	r := []row{head(fmt.Sprintf("Proposals (%d)", len(open)))}
	if len(open) == 0 {
		r = append(r, dim("  nothing to review"))
	}
	add := func(list []ops.ProposalView, note func(ops.ProposalView) string) {
		for _, p := range list {
			x := link("prop:"+p.ID, "  ", titleOr(p.Title, p.ID), stDim.Render("  "+note(p)), w)
			x.kind, x.id = "proposal", p.ID
			r = append(r, x)
		}
	}
	add(open, func(p ops.ProposalView) string {
		if p.Error != "" {
			return "broken file: " + p.Error
		}
		return fmt.Sprintf("%d of %d to decide, from %s", p.Pending, p.ItemCount, p.ProposedBy)
	})
	add(back, func(p ops.ProposalView) string { return fmt.Sprintf("sent back to %s", p.ProposedBy) })
	return r
}

// itemLine is the one plain line of an item: the summary the op made.
func itemLine(it ops.ItemView) string {
	if it.Summary != "" {
		return it.Summary
	}
	if it.Kind.Outward() {
		return "Message to " + it.GateTarget()
	}
	return strings.ToUpper(it.Op[:1]) + strings.ReplaceAll(it.Op[1:], "_", " ")
}

// itemNo is the number of an item: i2 is 2.
func itemNo(id string) string { return strings.TrimPrefix(id, "i") }

// wrapText breaks a line at spaces to at most w cells.
func wrapText(s string, w int) []string {
	if w < 10 || cells(s) <= w {
		return []string{s}
	}
	var out []string
	cur := ""
	for _, word := range strings.Fields(s) {
		switch {
		case cur == "":
			cur = word
		case cells(cur)+1+cells(word) <= w:
			cur += " " + word
		default:
			out = append(out, cur)
			cur = word
		}
		for cells(cur) > w { // a word longer than a line
			out = append(out, truncate(cur, w))
			cur = ""
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// rowsProposal is one proposal: a selectable row per item in words, the full
// text of a draft below it, and the evidence marked as untrusted.
func (m *Model) rowsProposal(p *page, w int) []row {
	d := p.prop
	v := d.Proposal
	r := []row{head(titleOr(v.Title, v.ID))}
	meta := fmt.Sprintf("  from %s  %s", v.ProposedBy, state(string(v.Status)))
	if v.Revision > 1 {
		meta += stDim.Render(fmt.Sprintf("  revision %d", v.Revision))
	}
	r = append(r, plain(meta))
	for _, x := range []string{d.TLDR, d.Context} {
		for _, l := range wrapText(x, max(w-4, 10)) {
			if l != "" {
				r = append(r, plain("  "+l))
			}
		}
	}
	if d.SupersededBy != "" {
		r = append(r, link("prop:"+d.SupersededBy, "  replaced by a newer revision: ", "open it", "", w))
	}
	for _, it := range d.Items {
		flag := ""
		if it.Edited {
			flag = "  " + stWarn.Render("edited")
		}
		if it.OpenNotes > 0 {
			flag += "  " + stWarn.Render(plural(it.OpenNotes, "open request"))
		}
		x := link("item:"+it.ID, "  "+stDim.Render(itemNo(it.ID))+"  ", itemLine(it), "  "+state(string(it.State))+flag, w)
		x.kind, x.id = "item", it.ID
		r = append(r, x)
		if !it.Kind.Outward() {
			for _, l := range it.Change {
				r = append(r, plain("        "+truncate(l, max(w-10, 10))))
			}
		}
		r = append(r, m.outcomeRows(it, w)...)
		if it.Kind.Outward() {
			r = append(r, dim("        to "+it.GateTarget()))
		}
		r = append(r, itemFullRows(it, w)...)
		r = append(r, dim("      "+stDim.Render("evidence ("+it.Evidence.Locator+") ")+stWarn.Render("[untrusted]")+stDim.Render(": "+truncate(oneLine(it.Evidence.Quote), max(w-40-cells(it.Evidence.Locator), 10)))))
		if it.Reason != "" {
			r = append(r, plain("      reason: "+truncate(oneLine(it.Reason), max(w-14, 10))))
		}
		if it.HeldBy != "" {
			target := it.HeldBy.Target()
			x := link("doc:"+target, "      needs first: ", titleOr(m.snap.name(target), "another item"), "", w)
			x.id = it.ID
			r = append(r, x)
		}
	}
	return r
}

// itemFullRows is the content of an item, shown in full on the proposal page.
func itemFullRows(it ops.ItemView, w int) []row {
	var r []row
	wrap := func(head, text string) {
		if strings.TrimSpace(text) == "" {
			return
		}
		lines := strings.Split(text, "\n")
		for i, l := range lines {
			for j, wl := range wrapText(l, max(w-10, 10)) {
				if i == 0 && j == 0 && head != "" {
					r = append(r, plain("        "+stDim.Render(head+": ")+wl))
					continue
				}
				r = append(r, plain("        "+wl))
			}
		}
	}
	wrap("subject", it.Subject)
	wrap("what", it.What)
	wrap("previous", it.Previous)
	wrap("how", it.How)
	wrap("impact", it.Impact)
	wrap("title", it.Title)
	wrap("", it.Body)
	wrap("", it.Diff)
	wrap("with results filled in", it.Filled)
	claim := func(head string, c model.Claim) { wrap(head, c.Text+" ["+c.Source+"]") }
	for _, c := range it.Pros {
		claim("pro", c)
	}
	for _, c := range it.Cons {
		claim("con", c)
	}
	if it.Cost != nil {
		claim("cost", *it.Cost)
	}
	if it.Benefit != nil {
		claim("benefit", *it.Benefit)
	}
	if it.Confidence > 0 {
		wrap("confidence", fmt.Sprintf("%d%% %s", it.Confidence, it.ConfidenceBasis))
	}
	if it.Recommendation > 0 {
		wrap("recommended", fmt.Sprintf("%d%%", it.Recommendation))
	}
	if it.Reversible != "" {
		wrap("reversible", strings.TrimSpace(string(it.Reversible)+" "+it.ReversibleHow))
	}
	for _, f := range []struct{ head, v string }{{"runs after", it.After}, {"bundle", it.Bundle}, {"choice", it.Choice}, {"group", it.Group}} {
		wrap(f.head, f.v)
	}
	if pv := it.Preview; pv != nil {
		wrap("would fail", pv.Error)
		for _, c := range pv.Changes {
			r = append(r, dim("        "+truncate("changes "+titleOr(c.Title, c.Doc), max(w-10, 10))))
			for _, l := range c.Lines {
				switch {
				case strings.HasPrefix(l, "+ "):
					r = append(r, plain("        "+stGood.Render(truncate(l, max(w-10, 10)))))
				case strings.HasPrefix(l, "- "):
					r = append(r, plain("        "+stBad.Render(truncate(l, max(w-10, 10)))))
				default:
					r = append(r, dim("        "+truncate(l, max(w-10, 10))))
				}
			}
		}
		for _, x := range pv.Downstream {
			wrap("then", titleOr(x.Title, x.Doc)+": "+x.Effect)
		}
	}
	for _, t := range it.Thread {
		line := string(t.Kind) + " by " + t.By + ": " + oneLine(t.Text)
		if t.State == model.TurnOpen {
			line += " (waits for the agent)"
		}
		if t.Quote != "" {
			line += fmt.Sprintf(" on %q", oneLine(t.Quote))
		}
		wrap("", line)
		if t.Answer != "" || (t.State != model.TurnOpen && t.State != model.TurnNoted) {
			wrap("answer", strings.TrimSpace(string(t.State)+" "+oneLine(t.Answer)))
		}
	}
	return r
}

// outcomeRows says what an approved item did: the name of what it made, as a
// link, or that nothing was sent. The hash is in the full view.
func (m *Model) outcomeRows(it ops.ItemView, w int) []row {
	switch {
	case it.State == model.ItemFailed && it.Result != "":
		return []row{plain("      " + stBad.Render("failed: "+truncate(oneLine(it.Result), max(w-18, 10))))}
	case it.State == model.ItemHalted:
		return []row{plain("      " + stBad.Render(truncate(oneLine(it.Reason), max(w-8, 10))))}
	case it.State != model.ItemApproved:
		return nil
	case it.Waits:
		return []row{plain("      " + stGood.Render("Approved.") + stDim.Render(" Runs after item "+itemNo(it.After)+"."))}
	case it.ResultName != "":
		x := link("doc:"+it.Result, "      "+stGood.Render("done")+", made ", it.ResultName, "", w)
		x.id = it.ID // the decisions on the footer stay those of this item
		return []row{x}
	case it.Kind.Outward():
		return []row{plain("      " + stGood.Render("Approved.") + stDim.Render(" Not sent yet."))}
	}
	return []row{plain("      " + stGood.Render("done"))}
}

// proposalItem is the item under the cursor.
func (m *Model) proposalItem(p *page, w int) (ops.ItemView, bool) {
	rows := m.rowsProposal(p, w)
	if len(rows) == 0 {
		return ops.ItemView{}, false
	}
	id := rows[clampSel(rows, p.cursor)].id
	i := slices.IndexFunc(p.prop.Items, func(it ops.ItemView) bool { return it.ID == id })
	if i < 0 {
		return ops.ItemView{}, false
	}
	return p.prop.Items[i], true
}

// proposalActs is the footer of a proposal page: the decisions that apply to
// the item under the cursor, each with its key. The rest is under ?.
func (m *Model) proposalActs(p *page, w int) []Target {
	it, ok := m.proposalItem(p, w)
	if !ok {
		return nil
	}
	var out []Target
	for _, t := range itemActs {
		if t.ID == actRequest.ID || t.ID == actApprove.ID || t.ID == actDeny.ID || t.ID == actCopyDraft.ID {
			if applies(it, t) {
				out = append(out, t)
			}
		}
	}
	return out
}

// proposalHint is what the item under the cursor needs said: for a pending
// email, what approving does.
func (m *Model) proposalHint() string {
	p := m.top()
	if p == nil || p.kind != pageProposal {
		return ""
	}
	if it, ok := m.proposalItem(p, m.listWidth()); ok && it.Kind.Outward() && it.State == model.ItemPending {
		return externalPromise
	}
	return ""
}

// extraActs are actions with a key and no place in the footer: they are under
// ?. On a proposal page every action has its key all the time, so a letter
// never moves with the cursor.
func (m *Model) extraActs() []Target {
	if p := m.top(); p != nil && p.kind == pageProposal {
		return itemActs
	}
	return nil
}

func (m *Model) rowsHistory(p *page, w int) []row {
	r := []row{head("History of " + titleOr(p.prop.Proposal.Title, p.id))}
	for _, h := range p.prop.History {
		r = append(r, link("prop:"+h.ID, fmt.Sprintf("  rev %d  ", h.Revision), h.ID, "  "+state(string(h.Status))+stDim.Render("  "+dateWords(string(h.Created), m.snap.Loaded)), w))
	}
	return r
}

// rowsText shows lines. Each is a stop, so j and k scroll.
func (m *Model) rowsText(p *page) []row {
	r := make([]row, 0, len(p.lines))
	for _, l := range p.lines {
		r = append(r, row{sel: true, draw: func(KeyFn) string { return l }})
	}
	return r
}

// showItem opens one item in full: the summary, the full text or the change,
// what approving does, and the evidence. The raw input is behind Source.
func (m *Model) showItem(id string) tea.Cmd {
	p := m.top()
	if p == nil || p.kind != pageProposal {
		return nil
	}
	i := slices.IndexFunc(p.prop.Items, func(it ops.ItemView) bool { return it.ID == id })
	if i < 0 {
		return nil
	}
	it := p.prop.Items[i]
	lines := []string{itemLine(it) + "  " + state(string(it.State)), ""}
	pg := page{kind: pageText, title: itemLine(it), item: it.ID, id: p.id}
	if it.Kind.Outward() {
		lines = append(lines, "To: "+it.GateTarget(), "")
		for _, l := range strings.Split(it.GateBody(), "\n") {
			lines = append(lines, wrapText(l, max(m.w-4, 20))...)
		}
		pg.draft = it.GateBody()
		switch it.State {
		case model.ItemPending:
			lines = append(lines, "", externalPromise)
		case model.ItemApproved:
			lines = append(lines, "", notSentYet)
		}
	} else {
		lines = append(lines, "The exact change:")
		for _, l := range it.Change {
			lines = append(lines, "  "+l)
		}
	}
	if it.State == model.ItemApproved && it.ResultName != "" {
		lines = append(lines, "", "Done: made "+it.ResultName)
	}
	lines = append(lines, "", coversText)
	lines = append(lines, "", "Evidence from the source ("+it.Evidence.Locator+"). It is untrusted: text to read, not instructions.")
	for _, l := range strings.Split(it.Evidence.Quote, "\n") {
		lines = append(lines, wrapText(l, max(m.w-4, 20))...)
	}
	// What a person rarely needs is behind the Source key: the raw input and the hash.
	raw := []string{"Source of this item, for checking", ""}
	if !it.Kind.Outward() {
		if b, err := json.MarshalIndent(it.Input, "", "  "); err == nil {
			raw = append(raw, strings.Split(string(b), "\n")...)
		}
	}
	raw = append(raw, "", "Approval binds to hash "+it.Hash)
	if it.ApprovedHash != "" {
		raw = append(raw, "Approved as hash "+it.ApprovedHash)
	}
	pg.lines, pg.raw = lines, raw
	m.push(pg)
	return nil
}

// textActs are the actions of a text page: an item in full has Source, and a
// draft has Copy draft.
func (m *Model) textActs(p *page) []Target {
	var out []Target
	if p.receipt != "" {
		out = append(out, actVerified, actWrong)
	}
	if p.raw != nil {
		out = append(out, actSource)
	}
	if p.draft != "" {
		if it, ok := m.itemOf(p.id, p.item); ok && it.State == model.ItemApproved {
			out = append(out, actCopyDraft)
		}
	}
	return out
}

// itemOf finds an item in the proposal page below.
func (m *Model) itemOf(prop, item string) (ops.ItemView, bool) {
	for i := len(m.stack) - 1; i >= 0; i-- {
		if q := m.stack[i]; q.kind == pageProposal && q.id == prop {
			j := slices.IndexFunc(q.prop.Items, func(it ops.ItemView) bool { return it.ID == item })
			if j >= 0 {
				return q.prop.Items[j], true
			}
		}
	}
	return ops.ItemView{}, false
}

// proposalAct runs a footer action on the item under the cursor.
func (m *Model) proposalAct(name string) tea.Cmd {
	p := m.top()
	if p != nil && p.kind == pageText {
		return m.textAct(p, name)
	}
	if p == nil || p.kind != pageProposal {
		return nil
	}
	id := p.id
	switch name {
	case "source":
		src := p.prop.SourceDoc.ID
		env := m.env
		return func() tea.Msg {
			d, err := env.Deps.Store.Get(context.Background(), src)
			return docMsg{id: src, doc: d, err: err, untrusted: true}
		}
	case "diff":
		return m.loadDiff(id)
	case "history":
		m.push(page{kind: pageHistory, id: id, title: "history", prop: p.prop})
		return nil
	}
	it, ok := m.proposalItem(p, m.w)
	if !ok {
		return nil
	}
	item := it.ID
	if name == "copydraft" {
		if !applies(it, actCopyDraft) {
			m.setHint("Copy draft applies after you approve an email.")
			return nil
		}
		return m.copyText(id, it.GateBody())
	}
	if t, known := map[string]Target{"approve": actApprove, "deny": actDeny, "request": actRequest, "hold": actHold, "edit": actEdit}[name]; known && !applies(it, t) {
		m.setHint(whyNot[name])
		return nil
	}
	switch name {
	case "approve":
		return m.runReview(item, true, func(ctx context.Context, d ops.Deps) (ops.ProposalReviewOutput, error) {
			return ops.ProposalApprove.Run(ctx, d, ops.ProposalApproveInput{ID: id, Items: []string{item}})
		})
	case "deny":
		return m.askFor("Deny: "+itemLine(it), []promptStep{{label: "why (the agent reads it): "}}, func(v []string) tea.Cmd {
			return m.runReview(item, true, func(ctx context.Context, d ops.Deps) (ops.ProposalReviewOutput, error) {
				return ops.ProposalDeny.Run(ctx, d, ops.ProposalReasonInput{ID: id, Item: item, Reason: v[0]})
			})
		})
	case "request":
		steps := []promptStep{{label: "changes, clarification, or investigation: ", value: "changes"}, {label: "text: "}}
		return m.askFor("Request: "+itemLine(it), steps, func(v []string) tea.Cmd {
			intent, err := requestIntent(v[0])
			if err != nil {
				return func() tea.Msg { return doneMsg{err: err} }
			}
			return m.runReview("", false, func(ctx context.Context, d ops.Deps) (ops.ProposalReviewOutput, error) {
				return ops.ProposalRequest.Run(ctx, d, ops.ProposalRequestInput{ID: id, Item: item, Note: v[1], Intent: intent})
			})
		})
	case "hold":
		place, _ := it.Input["project"].(string)
		steps := []promptStep{
			{label: "task or agent work: ", value: "task"},
			{label: "text: "},
			{label: "goal or area id: ", value: place, skip: func(v []string) bool { return v[0] != "task" }},
		}
		return m.askFor("Needs first: "+itemLine(it), steps, func(v []string) tea.Cmd {
			return m.runReview(item, true, func(ctx context.Context, d ops.Deps) (ops.ProposalReviewOutput, error) {
				in := ops.ProposalHoldInput{ID: id, Item: item, Kind: ops.HoldKind(v[0]), Text: v[1]}
				if strings.HasPrefix(v[2], "area-") {
					in.Area = v[2]
				} else {
					in.Project = v[2]
				}
				return ops.ProposalHold.Run(ctx, d, in)
			})
		})
	case "edit":
		return m.openEditor(id, it)
	}
	return nil
}

// requestIntent reads the type a person typed for a request: changes (the
// default), clarification, or investigation. A prefix is enough.
func requestIntent(s string) (ops.NoteIntent, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch {
	case s == "" || strings.HasPrefix("changes", s):
		return ops.IntentChange, nil
	case strings.HasPrefix("clarification", s):
		return ops.IntentClarify, nil
	case strings.HasPrefix("investigation", s):
		return ops.IntentInvestigate, nil
	}
	return "", fmt.Errorf("request type %q: want changes, clarification, or investigation", s)
}

// textAct runs an action of an item in full: Source shows the raw input, and
// Copy draft copies the draft of an approved email.
func (m *Model) textAct(p *page, name string) tea.Cmd {
	switch name {
	case "source":
		if p.raw != nil {
			m.push(page{kind: pageText, title: "source of " + p.title, lines: p.raw})
		}
	case "copydraft":
		if p.draft != "" {
			return m.copyText(p.id, p.draft)
		}
	case "verified", "wrong":
		if p.receipt != "" {
			return m.receiptAct(p.receipt, name)
		}
	}
	return nil
}

// reviewVerbs say a decision in the one family of words: Approve for items.
var reviewVerbs = map[model.ItemState]string{
	model.ItemApproved:  "Approved",
	model.ItemDenied:    "Denied",
	model.ItemDone:      "Done",
	model.ItemHalted:    "Halted",
	model.ItemSentBack:  "Returned",
	model.ItemHeld:      "Holding",
	model.ItemRunning:   "Approved, running",
	model.ItemPending:   "Updated",
	model.ItemWithdrawn: "Withdrawn",
}

// runReview runs a review op off the update loop and reloads. Its result is
// one line that names the item, not its number. A decision on item moves the
// cursor to the next item that waits; a note does not.
func (m *Model) runReview(item string, advance bool, fn func(context.Context, ops.Deps) (ops.ProposalReviewOutput, error)) tea.Cmd {
	env := m.env
	names := map[string]string{}
	external := map[string]bool{}
	if p := m.top(); p != nil && p.kind == pageProposal {
		for _, it := range p.prop.Items {
			names[it.ID] = itemLine(it)
			external[it.ID] = it.Kind.Outward()
		}
	}
	return func() tea.Msg {
		o, err := fn(context.Background(), env.Deps)
		if err != nil {
			return doneMsg{err: err, reload: true}
		}
		var parts []string
		for _, d := range o.Items {
			what := names[d.Item]
			if what == "" {
				what = "item " + itemNo(d.Item)
			}
			if d.State == model.ItemFailed {
				return doneMsg{err: errors.New(what + " failed: " + d.Reason), reload: true}
			}
			verb := reviewVerbs[d.State]
			if verb == "" {
				verb = "Updated"
			}
			if d.State == model.ItemApproved && external[d.Item] {
				parts = append(parts, notSentYet+" "+what)
				continue
			}
			parts = append(parts, verb+": "+what)
		}
		text := strings.Join(parts, "; ")
		if o.Request != "" {
			text += ". Queued agent work"
		}
		msg := doneMsg{text: text, reload: true}
		if advance {
			msg.advance = item
		}
		return msg
	}
}

func (m *Model) loadDiff(id string) tea.Cmd {
	env := m.env
	return func() tea.Msg {
		o, err := ops.ProposalDiff.Run(context.Background(), env.Deps, ops.ProposalIDInput{ID: id})
		if err != nil {
			return textMsg{err: err}
		}
		lines := []string{fmt.Sprintf("rev %d from %s", o.Revision, titleOr(o.Previous, "nothing: the first revision")), ""}
		for _, it := range o.Items {
			lines = append(lines, it.Item+" "+it.Change)
			for _, f := range it.Fields {
				lines = append(lines, "  "+f.Field)
				for _, l := range f.Lines {
					lines = append(lines, "    "+l)
				}
			}
		}
		return textMsg{title: "diff " + id, lines: lines}
	}
}

// promptStep is one line of a prompt chain. Skip leaves the step out, and its
// value is "".
type promptStep struct {
	label, value string
	skip         func(vals []string) bool
	// optional lets enter on an empty line move on, with the value "".
	optional bool
}

// hint says what enter does at this step of the chain.
func (p promptState) hint() string {
	if p.lastAsk() {
		return "enter records it, esc cancels"
	}
	return "enter continues, esc cancels"
}

// lastAsk reports whether no later step will be asked.
func (p promptState) lastAsk() bool {
	pad := make([]string, len(p.steps))
	copy(pad, p.vals)
	for i := len(p.vals) + 1; i < len(p.steps); i++ {
		if s := p.steps[i]; s.skip == nil || !s.skip(pad) {
			return false
		}
	}
	return true
}

// promptState is the prompt overlay: steps run in order, then done.
type promptState struct {
	title string
	lines []string // shown between the title and the input
	steps []promptStep
	vals  []string
	done  func(vals []string) tea.Cmd
}

func (m *Model) askFor(title string, steps []promptStep, done func([]string) tea.Cmd) tea.Cmd {
	m.ov = overlay{kind: ovPrompt, prompt: promptState{title: title, steps: steps, done: done}}
	return m.nextPrompt()
}

// nextPrompt shows the next step, or finishes the chain.
func (m *Model) nextPrompt() tea.Cmd {
	pr := &m.ov.prompt
	for len(pr.vals) < len(pr.steps) {
		s := pr.steps[len(pr.vals)]
		if s.skip != nil && s.skip(pr.vals) {
			pr.vals = append(pr.vals, "")
			continue
		}
		m.ov.input = newInput(s.label, s.value)
		_ = m.ov.input.Focus() // its command only blinks the cursor
		return nil
	}
	vals, done := pr.vals, pr.done
	m.ov = overlay{}
	return done(vals)
}

func (m *Model) onPromptKey(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		m.ov = overlay{}
		return nil
	case "enter":
		text := strings.TrimSpace(m.ov.input.Value())
		if text == "" && !m.ov.prompt.steps[len(m.ov.prompt.vals)].optional {
			return nil
		}
		m.ov.prompt.vals = append(m.ov.prompt.vals, text)
		return m.nextPrompt()
	}
	var cmd tea.Cmd
	m.ov.input, cmd = m.ov.input.Update(k)
	return cmd
}

// The buffer $EDITOR gets. An external item is its target, a separator line,
// and the draft, so repointing and rewording happen in one place. An op item is
// its input as JSON, so repointing a person or a project is a field change.
const (
	editTarget = "target: "
	editTitle  = "title: "   // an action's title
	editSubj   = "subject: " // a message's subject
	editSplit  = "---"
)

// editBuffer writes an item for $EDITOR.
func editBuffer(it ops.ItemView) (string, error) {
	if it.Kind.Outward() {
		head := editTarget + it.GateTarget() + "\n"
		switch it.Kind {
		case model.ItemAction:
			head += editTitle + it.Title + "\n"
		case model.ItemMessage:
			head += editSubj + it.Subject + "\n"
		}
		return head + editSplit + "\n" + it.Body, nil
	}
	b, err := json.MarshalIndent(it.Input, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b) + "\n", nil
}

// parseEditBuffer reads an edited buffer back into an edit. It keeps the draft
// exactly, but for the one newline an editor adds at the end of a file.
func parseEditBuffer(it ops.ItemView, text string) (ops.ProposalEditInput, error) {
	var in ops.ProposalEditInput
	if it.Kind == model.ItemPara {
		dec := json.NewDecoder(strings.NewReader(text))
		dec.UseNumber()
		if err := dec.Decode(&in.Input); err != nil {
			return in, fmt.Errorf("input is not JSON: %w", err)
		}
		return in, nil
	}
	head, draft, ok := strings.Cut(text, "\n"+editSplit+"\n")
	first, rest, _ := strings.Cut(head, "\n")
	target, isTarget := strings.CutPrefix(first, editTarget)
	if !ok || !isTarget {
		return in, fmt.Errorf("want a first line %q and a line %q before the draft", editTarget+"...", editSplit)
	}
	if rest != "" {
		switch {
		case it.Kind == model.ItemAction && strings.HasPrefix(rest, editTitle) && !strings.Contains(rest, "\n"):
			v := strings.TrimSpace(strings.TrimPrefix(rest, editTitle))
			in.Title = &v
		case it.Kind == model.ItemMessage && strings.HasPrefix(rest, editSubj) && !strings.Contains(rest, "\n"):
			v := strings.TrimSpace(strings.TrimPrefix(rest, editSubj))
			in.Subject = &v
		default:
			return in, fmt.Errorf("want a first line %q and a line %q before the draft", editTarget+"...", editSplit)
		}
	}
	if !strings.HasSuffix(it.Body, "\n") {
		draft = strings.TrimSuffix(draft, "\n")
	}
	target = strings.TrimSpace(target)
	in.Target, in.Body = &target, &draft
	return in, nil
}

// openEditor runs $EDITOR on the item. The buffer sits in this vault's state
// dir, not in the shared temporary directory, and is removed afterwards.
func (m *Model) openEditor(id string, it ops.ItemView) tea.Cmd {
	text, err := editBuffer(it)
	if err != nil {
		m.setStatus(err.Error(), true)
		return nil
	}
	f, err := os.CreateTemp(m.env.StateDir, "edit-*.txt")
	if err != nil {
		m.setStatus(err.Error(), true)
		return nil
	}
	path := f.Name()
	_, werr := f.WriteString(text)
	if err := errors.Join(werr, f.Close()); err != nil {
		_ = os.Remove(path)
		m.setStatus(err.Error(), true)
		return nil
	}
	editor := strings.Fields(m.env.getenv("EDITOR"))
	if len(editor) == 0 {
		editor = []string{"vi"}
	}
	c := exec.Command(editor[0], append(editor[1:], path)...)
	return tea.ExecProcess(c, func(err error) tea.Msg { return editedMsg{id: id, item: it, path: path, err: err} })
}

func (m *Model) onEdited(msg editedMsg) tea.Cmd {
	defer func() { _ = os.Remove(msg.path) }()
	if msg.err != nil {
		m.setStatus("editor: "+msg.err.Error(), true)
		return nil
	}
	b, err := os.ReadFile(msg.path)
	if err != nil {
		m.setStatus(err.Error(), true)
		return nil
	}
	in, err := parseEditBuffer(msg.item, string(b))
	if err != nil {
		m.setStatus(err.Error(), true)
		return nil
	}
	in.ID, in.Item = msg.id, msg.item.ID
	return m.runReview("", false, func(ctx context.Context, d ops.Deps) (ops.ProposalReviewOutput, error) {
		return ops.ProposalEdit.Run(ctx, d, in)
	})
}

// noteEditedMsg is the end of an $EDITOR run on a note.
type noteEditedMsg struct {
	id, path string
	doc      model.Document
	err      error
}

// noteBuffer is a note as the editor shows it: the title, a split line, the body.
func noteBuffer(d model.Document) string {
	return editTitle + d.Title + "\n" + editSplit + "\n" + d.Body
}

// parseNoteBuffer reads an edited note buffer back into an edit.
func parseNoteBuffer(d model.Document, text string) (ops.NoteEditInput, error) {
	head, body, ok := strings.Cut(text, "\n"+editSplit+"\n")
	title, isTitle := strings.CutPrefix(head, editTitle)
	if !ok || !isTitle || strings.Contains(head, "\n") {
		return ops.NoteEditInput{}, fmt.Errorf("want a first line %q and a line %q before the body", editTitle+"...", editSplit)
	}
	if !strings.HasSuffix(d.Body, "\n") {
		body = strings.TrimSuffix(body, "\n")
	}
	title = strings.TrimSpace(title)
	return ops.NoteEditInput{ID: d.ID, Title: &title, Body: &body}, nil
}

// editNote runs $EDITOR on the note of the page showing.
func (m *Model) editNote() tea.Cmd {
	p := m.top()
	if p == nil || p.kind != pageDoc {
		return nil
	}
	if _, ok := p.doc.Meta.(*model.Note); !ok {
		m.setStatus("Edit is for notes.", true)
		return nil
	}
	f, err := os.CreateTemp(m.env.StateDir, "edit-*.txt")
	if err != nil {
		m.setStatus(err.Error(), true)
		return nil
	}
	path := f.Name()
	_, werr := f.WriteString(noteBuffer(p.doc))
	if err := errors.Join(werr, f.Close()); err != nil {
		_ = os.Remove(path)
		m.setStatus(err.Error(), true)
		return nil
	}
	editor := strings.Fields(m.env.getenv("EDITOR"))
	if len(editor) == 0 {
		editor = []string{"vi"}
	}
	doc := p.doc
	c := exec.Command(editor[0], append(editor[1:], path)...)
	return tea.ExecProcess(c, func(err error) tea.Msg { return noteEditedMsg{id: doc.ID, path: path, doc: doc, err: err} })
}

func (m *Model) onNoteEdited(msg noteEditedMsg) tea.Cmd {
	defer func() { _ = os.Remove(msg.path) }()
	if msg.err != nil {
		m.setStatus("editor: "+msg.err.Error(), true)
		return nil
	}
	b, err := os.ReadFile(msg.path)
	if err != nil {
		m.setStatus(err.Error(), true)
		return nil
	}
	in, err := parseNoteBuffer(msg.doc, string(b))
	if err != nil {
		m.setStatus(err.Error(), true)
		return nil
	}
	env := m.env
	load := m.loadDoc(msg.id, true)
	return func() tea.Msg {
		if _, err := ops.NoteEdit.Run(context.Background(), env.Deps, in); err != nil {
			return doneMsg{err: err}
		}
		return load()
	}
}
