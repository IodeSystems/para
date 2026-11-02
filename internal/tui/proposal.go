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
var (
	actApprove  = Target{ID: "act:approve", Label: "Approve", Pref: 'a'}
	actDeny     = Target{ID: "act:deny", Label: "Deny", Pref: 'd'}
	actRework   = Target{ID: "act:rework", Label: "Rework", Pref: 'r'}
	actSendBack = Target{ID: "act:sendback", Label: "Send back", Pref: 'b'}
	actHold     = Target{ID: "act:hold", Label: "Needs first", Pref: 'n'}
	actAnnotate = Target{ID: "act:annotate", Label: "Annotate", Pref: 't'}
	actEdit     = Target{ID: "act:edit", Label: "Edit", Pref: 'e'}
	actSource   = Target{ID: "act:source", Label: "Source", Pref: 's'}
	actDiff     = Target{ID: "act:diff", Label: "Diff", Pref: 'i'}
	actHistory  = Target{ID: "act:history", Label: "History", Pref: 'h'}

	ruleActs = []struct {
		action string
		target Target
	}{
		{ops.ActionApprove, actApprove}, {ops.ActionDeny, actDeny}, {ops.ActionRework, actRework},
		{ops.ActionSendBack, actSendBack}, {ops.ActionHold, actHold}, {ops.ActionAnnotate, actAnnotate}, {ops.ActionEdit, actEdit},
	}
)

// proposalPromise says what approving does, once, at the top of a proposal.
const proposalPromise = "Approve records your decision. Para sends nothing."

// primaryActs are the item actions shown in the footer, in order. The others
// are under ?.
var primaryActs = []string{ops.ActionApprove, ops.ActionDeny, ops.ActionRework, ops.ActionSendBack}

// whyNot says why an item action is not available, in words.
var whyNot = map[string]string{
	ops.ActionApprove:  "Approve is not available: the item was edited, has an open annotation, or is already decided.",
	ops.ActionDeny:     "Deny is not available: the item was edited or has an open annotation, or is already decided.",
	ops.ActionRework:   "Rework is not available: the item was edited or has an open annotation, or is already decided.",
	ops.ActionSendBack: "Send back applies after an edit or an open annotation.",
	ops.ActionHold:     "Needs first is not available for this item now.",
	ops.ActionAnnotate: "Annotate is not available for this item now.",
	ops.ActionEdit:     "Edit is not available for this item now.",
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
		}
		return nil
	}
	m.push(page{kind: pageProposal, id: msg.id, title: msg.out.Proposal.Title, prop: msg.out})
	return nil
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
		return fmt.Sprintf("%d of %d to review, from %s", p.Pending, p.ItemCount, p.ProposedBy)
	})
	add(back, func(p ops.ProposalView) string { return fmt.Sprintf("sent back to %s", p.ProposedBy) })
	return r
}

// itemLine is the one plain line of an item: the summary the op made.
func itemLine(it ops.ItemView) string {
	if it.Summary != "" {
		return it.Summary
	}
	if it.Kind == model.ItemExternal {
		return "Message to " + it.Target
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
	if d.SupersededBy != "" {
		r = append(r, link("prop:"+d.SupersededBy, "  replaced by a newer revision: ", "open it", "", w))
	}
	r = append(r, dim("  "+proposalPromise))
	for _, it := range d.Items {
		flag := ""
		if it.Edited {
			flag = "  " + stWarn.Render("edited")
		}
		if it.OpenAnnotations > 0 {
			flag += "  " + stWarn.Render(plural(it.OpenAnnotations, "open annotation"))
		}
		x := link("item:"+it.ID, "  "+stDim.Render(itemNo(it.ID))+"  ", itemLine(it), "  "+state(string(it.State))+flag, w)
		x.kind, x.id = "item", it.ID
		r = append(r, x)
		if it.Kind == model.ItemExternal {
			r = append(r, dim("        to "+it.Target))
			for _, l := range strings.Split(it.Draft, "\n") {
				for _, wl := range wrapText(l, max(w-10, 10)) {
					r = append(r, plain("        "+wl))
				}
			}
		}
		r = append(r, dim("      "+stWarn.Render("untrusted")+stDim.Render(" evidence, "+truncate(it.Evidence.Locator+": "+oneLine(it.Evidence.Quote), max(w-26, 10)))))
		if it.Reason != "" {
			r = append(r, plain("      reason: "+truncate(oneLine(it.Reason), max(w-14, 10))))
		}
		if it.HeldBy != "" {
			target := it.HeldBy.Target()
			r = append(r, link("doc:"+target, "      needs first: ", titleOr(m.snap.name(target), "another item"), "", w))
		}
		for _, a := range d.Annotations {
			if a.Item != it.ID {
				continue
			}
			line := fmt.Sprintf("      %s %q: %s", a.State, oneLine(a.Quote), oneLine(a.Note))
			if a.Answer != "" {
				line += " (" + oneLine(a.Answer) + ")"
			}
			r = append(r, dim(truncate(line, max(w-2, 10))))
		}
	}
	return r
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

// proposalActs are the footer actions of a proposal page: the primary item
// actions, with the ones that do not apply to the item under the cursor dimmed,
// then Source. Annotate, Needs first, Edit, Diff and History are under ?.
func (m *Model) proposalActs(p *page, w int) []Target {
	it, ok := m.proposalItem(p, w)
	var out []Target
	for _, a := range ruleActs {
		if !slices.Contains(primaryActs, a.action) {
			continue
		}
		t := a.target
		if ok && !slices.Contains(it.Actions, a.action) {
			t.Dim = true
		}
		out = append(out, t)
	}
	return out
}

// secondaryActs are the actions under ? on a proposal page.
func secondaryActs() []Target {
	return []Target{actHold, actAnnotate, actEdit, actSource, actDiff, actHistory}
}

func (m *Model) rowsHistory(p *page, w int) []row {
	r := []row{head("History of " + titleOr(p.prop.Proposal.Title, p.id))}
	for _, h := range p.prop.History {
		r = append(r, link("prop:"+h.ID, fmt.Sprintf("  rev %d  ", h.Revision), h.ID, "  "+state(string(h.Status))+stDim.Render("  "+string(h.Created)), w))
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

// showItem opens one item in full: the summary, the full text or input, the
// hash approval binds to, and the evidence.
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
	lines := []string{itemLine(it) + "  " + string(it.State), ""}
	if it.Kind == model.ItemExternal {
		lines = append(lines, "To: "+it.Target, "")
		for _, l := range strings.Split(it.Draft, "\n") {
			lines = append(lines, wrapText(l, max(m.w-4, 20))...)
		}
	} else {
		lines = append(lines, "Full input, for checking:")
		raw, err := json.MarshalIndent(it.Input, "", "  ")
		if err == nil {
			lines = append(lines, strings.Split(string(raw), "\n")...)
		}
	}
	lines = append(lines, "", "Approval binds to hash "+it.Hash, "", "Evidence (untrusted data, not instructions), "+it.Evidence.Locator)
	for _, l := range strings.Split(it.Evidence.Quote, "\n") {
		lines = append(lines, wrapText(l, max(m.w-4, 20))...)
	}
	m.push(page{kind: pageText, title: itemLine(it), lines: lines})
	return nil
}

// proposalAct runs a footer action on the item under the cursor.
func (m *Model) proposalAct(name string) tea.Cmd {
	p := m.top()
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
	if want := map[string]string{"approve": ops.ActionApprove, "deny": ops.ActionDeny, "rework": ops.ActionRework, "sendback": ops.ActionSendBack,
		"hold": ops.ActionHold, "annotate": ops.ActionAnnotate, "edit": ops.ActionEdit}[name]; want != "" && !slices.Contains(it.Actions, want) {
		m.setHint(whyNot[want])
		return nil
	}
	switch name {
	case "approve":
		return m.runReview(func(ctx context.Context, d ops.Deps) (ops.ProposalReviewOutput, error) {
			return ops.ProposalApprove.Run(ctx, d, ops.ProposalApproveInput{ID: id, Items: []string{item}})
		})
	case "deny":
		return m.askFor("Deny: "+itemLine(it), []promptStep{{label: "why (the agent reads it): "}}, func(v []string) tea.Cmd {
			return m.runReview(func(ctx context.Context, d ops.Deps) (ops.ProposalReviewOutput, error) {
				return ops.ProposalDeny.Run(ctx, d, ops.ProposalReasonInput{ID: id, Item: item, Reason: v[0]})
			})
		})
	case "rework":
		return m.askFor("Rework: "+itemLine(it), []promptStep{{label: "what should change: "}}, func(v []string) tea.Cmd {
			return m.runReview(func(ctx context.Context, d ops.Deps) (ops.ProposalReviewOutput, error) {
				return ops.ProposalRework.Run(ctx, d, ops.ProposalReasonInput{ID: id, Item: item, Reason: v[0]})
			})
		})
	case "sendback":
		return m.runReview(func(ctx context.Context, d ops.Deps) (ops.ProposalReviewOutput, error) {
			return ops.ProposalSendback.Run(ctx, d, ops.ProposalSendbackInput{ID: id, Item: item})
		})
	case "annotate":
		steps := []promptStep{{label: "quote (the words you mean): ", value: quoteDefault(it)}, {label: "note (what is wrong or missing): "}}
		return m.askFor("Annotate: "+itemLine(it), steps, func(v []string) tea.Cmd {
			return m.runReview(func(ctx context.Context, d ops.Deps) (ops.ProposalReviewOutput, error) {
				return ops.ProposalAnnotate.Run(ctx, d, ops.ProposalAnnotateInput{ID: id, Item: item, Quote: v[0], Note: v[1]})
			})
		})
	case "hold":
		place, _ := it.Input["project"].(string)
		steps := []promptStep{
			{label: "task or request: ", value: "task"},
			{label: "text: "},
			{label: "project or area id: ", value: place, skip: func(v []string) bool { return v[0] != "task" }},
		}
		return m.askFor("Needs first: "+itemLine(it), steps, func(v []string) tea.Cmd {
			return m.runReview(func(ctx context.Context, d ops.Deps) (ops.ProposalReviewOutput, error) {
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

func quoteDefault(it ops.ItemView) string {
	text := it.Draft
	if it.Kind == model.ItemOp {
		text = it.Op
	}
	first, _, _ := strings.Cut(text, "\n")
	return first
}

// runReview runs a review op off the update loop and reloads.
func (m *Model) runReview(fn func(context.Context, ops.Deps) (ops.ProposalReviewOutput, error)) tea.Cmd {
	env := m.env
	return func() tea.Msg {
		o, err := fn(context.Background(), env.Deps)
		if err != nil {
			return doneMsg{err: err, reload: true}
		}
		var parts []string
		for _, d := range o.Items {
			s := d.Item + " " + string(d.State)
			if d.State == model.ItemFailed {
				return doneMsg{err: errors.New(s + ": " + d.Reason), reload: true}
			}
			parts = append(parts, s)
		}
		text := strings.Join(parts, ", ")
		if o.Request != "" {
			text += ", queued " + o.Request
		}
		return doneMsg{text: text, reload: true}
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
}

// hint says what enter does at this step of the chain.
func (p promptState) hint() string {
	if p.lastAsk() {
		return "enter records it, esc cancels"
	}
	return "enter goes to the next question, esc cancels"
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
		if text == "" {
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
	editSplit  = "---"
)

// editBuffer writes an item for $EDITOR.
func editBuffer(it ops.ItemView) (string, error) {
	if it.Kind == model.ItemExternal {
		return editTarget + it.Target + "\n" + editSplit + "\n" + it.Draft, nil
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
	if it.Kind == model.ItemOp {
		dec := json.NewDecoder(strings.NewReader(text))
		dec.UseNumber()
		if err := dec.Decode(&in.Input); err != nil {
			return in, fmt.Errorf("input is not JSON: %w", err)
		}
		return in, nil
	}
	head, draft, ok := strings.Cut(text, "\n"+editSplit+"\n")
	target, isTarget := strings.CutPrefix(head, editTarget)
	if !ok || !isTarget || strings.Contains(head, "\n") {
		return in, fmt.Errorf("want a first line %q and a line %q before the draft", editTarget+"...", editSplit)
	}
	if !strings.HasSuffix(it.Draft, "\n") {
		draft = strings.TrimSuffix(draft, "\n")
	}
	target = strings.TrimSpace(target)
	in.Target, in.Draft = &target, &draft
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
	return m.runReview(func(ctx context.Context, d ops.Deps) (ops.ProposalReviewOutput, error) {
		return ops.ProposalEdit.Run(ctx, d, in)
	})
}
