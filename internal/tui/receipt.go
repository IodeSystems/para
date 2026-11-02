package tui

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/iodesystems/parasolve/internal/model"
	"github.com/iodesystems/parasolve/internal/ops"
)

var (
	actVerified = Target{ID: "act:verified", Label: "Verified", Pref: 'v'}
	actWrong    = Target{ID: "act:wrong", Label: "Wrong", Pref: 'w'}
)

// rowsReceipts lists agent writes nobody has checked.
func (m *Model) rowsReceipts(w int) []row {
	if len(m.snap.Receipts) == 0 {
		return nil
	}
	r := []row{head(fmt.Sprintf("Agent writes to check (%d)", len(m.snap.Receipts)))}
	for _, c := range m.snap.Receipts {
		name := titleOr(m.snap.name(c.Doc), c.Title)
		r = append(r, link("receipt:"+c.ID, "  "+stDim.Render(c.Actor+" ")+strings.ReplaceAll(c.Op, "_", " ")+" ", name, stDim.Render("  "+truncate(oneLine(c.Why), 40)), w))
	}
	return r
}

type receiptMsg struct {
	out ops.ReceiptDetail
	err error
}

func (m *Model) loadReceipt(id string) tea.Cmd {
	env := m.env
	return func() tea.Msg {
		o, err := ops.ReceiptShow.Run(context.Background(), env.Deps, ops.ReceiptIDInput{ID: id})
		return receiptMsg{out: o, err: err}
	}
}

func (m *Model) onReceipt(msg receiptMsg) tea.Cmd {
	if msg.err != nil {
		m.setStatus(msg.err.Error(), true)
		return nil
	}
	m.push(page{kind: pageText, title: msg.out.Receipt.Title, receipt: msg.out.Receipt.ID, lines: receiptLines(msg.out)})
	return nil
}

// receiptLines says what changed, why, on what, and how to verify.
func receiptLines(d ops.ReceiptDetail) []string {
	r := d.Receipt
	out := []string{r.Title, "", "By: " + r.Actor}
	if r.Why != "" {
		out = append(out, "Why: "+r.Why)
	}
	if len(r.Sources) > 0 {
		out = append(out, "Sources: "+strings.Join(r.Sources, ", "))
	}
	if r.Verify != "" {
		out = append(out, "Verify: "+r.Verify)
	}
	if r.Confidence != "" {
		out = append(out, "Confidence: "+string(r.Confidence))
	}
	if r.SelfReported {
		out = append(out, "Self-reported: no hook recorded this.")
	}
	return append(append(out, "", "What changed:"), d.Lines...)
}

func (m *Model) receiptAct(id, name string) tea.Cmd {
	run := func(note string) tea.Cmd {
		env := m.env
		verdict := model.ReceiptVerified
		if name == "wrong" {
			verdict = model.ReceiptWrong
		}
		return func() tea.Msg {
			_, err := ops.ReceiptCheck.Run(context.Background(), env.Deps, ops.ReceiptCheckInput{ID: id, Verdict: verdict, Note: note})
			if err != nil {
				return doneMsg{err: err}
			}
			text := "Marked verified."
			if verdict == model.ReceiptWrong {
				text = "Marked wrong. Queued agent work."
			}
			return doneMsg{text: text, reload: true}
		}
	}
	if name == "wrong" {
		return m.askFor("Wrong", []promptStep{{label: "what is wrong: "}}, func(v []string) tea.Cmd { return run(v[0]) })
	}
	return run("")
}
