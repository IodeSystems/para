package ops

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/iodesystems/paradize/internal/model"
)

const draftHeading = "## Draft"

// RFXDraftInput is the input of rfx_draft.
type RFXDraftInput struct {
	ID   string `json:"id"`
	Text string `json:"text"` // the draft message; replaces the earlier draft
}

// RFXDraftOutput is the output of rfx_draft.
type RFXDraftOutput struct {
	ID    string `json:"id"`
	Draft string `json:"draft"`
	Rev   string `json:"rev"`
}

// RFXDraft stores the message to send under "## Draft" in the request body,
// replacing an earlier draft. Para sends nothing: the user sends it.
var RFXDraft = define("rfx_draft", "Store a draft message for a request under its Draft section; nothing is sent", func(ctx context.Context, d Deps, in RFXDraftInput) (RFXDraftOutput, error) {
	doc, m, err := getRFX(ctx, d, normID(in.ID))
	if err != nil {
		return RFXDraftOutput{}, err
	}
	if m.Status == model.RFXClosed {
		return RFXDraftOutput{}, errors.New("request is closed")
	}
	text := strings.TrimSpace(strings.ReplaceAll(in.Text, "\r\n", "\n"))
	if text == "" {
		return RFXDraftOutput{}, errors.New("text is empty")
	}
	for _, l := range strings.Split(text, "\n") {
		if strings.HasPrefix(l, "# ") || strings.HasPrefix(l, "## ") {
			return RFXDraftOutput{}, errors.New("draft lines must not start with # or ##: they would split the Draft section; use ###")
		}
	}
	doc.Body = setSection(doc.Body, draftHeading, text)
	if doc, err = d.Store.Update(ctx, doc); err != nil {
		return RFXDraftOutput{}, err
	}
	return RFXDraftOutput{ID: doc.ID, Draft: text, Rev: doc.Rev}, nil
})

// draftOf returns the text under "## Draft", or "".
func draftOf(body string) string {
	return headingText(body, draftHeading)
}

// headingText returns the text of the section under heading, trimmed.
func headingText(body, heading string) string {
	lines := strings.Split(body, "\n")
	at := slices.IndexFunc(lines, func(l string) bool { return strings.TrimRight(l, " \t") == heading })
	if at < 0 {
		return ""
	}
	end := sectionEnd(lines, at)
	return strings.TrimSpace(strings.Join(lines[at+1:end], "\n"))
}

func sectionEnd(lines []string, at int) int {
	for i := at + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "# ") || strings.HasPrefix(lines[i], "## ") {
			return i
		}
	}
	return len(lines)
}

// setSection replaces the text of the section under heading, or adds the
// section at the end of body.
func setSection(body, heading, text string) string {
	body = strings.Trim(body, "\n")
	lines := strings.Split(body, "\n")
	at := slices.IndexFunc(lines, func(l string) bool { return strings.TrimRight(l, " \t") == heading })
	if body == "" {
		return heading + "\n\n" + text
	}
	if at < 0 {
		return body + "\n\n" + heading + "\n\n" + text
	}
	end := sectionEnd(lines, at)
	out := append([]string(nil), lines[:at+1]...)
	out = append(out, "", text)
	if end < len(lines) {
		out = append(out, "")
		out = append(out, lines[end:]...)
	}
	return strings.Join(out, "\n")
}
