package ops

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"github.com/iodesystems/para/internal/model"
)

const inboxTitleMax = 80

// InboxAddInput is the input of inbox_add.
type InboxAddInput struct {
	Text string `json:"text"` // the capture, para-md body text
	// ImportedFrom is the file the new document comes from (import_verify).
	ImportedFrom *model.ImportedFrom `json:"imported_from,omitempty"`
}

// InboxAddOutput is the output of inbox_add.
type InboxAddOutput struct {
	ID     string       `json:"id"`
	Folder model.Folder `json:"folder"`
	Rev    string       `json:"rev"`
}

// InboxAdd captures text into inbox/.
var InboxAdd = defineSafe("inbox_add", "Capture text into the inbox", inboxAdd)

func inboxAdd(ctx context.Context, d Deps, in InboxAddInput) (InboxAddOutput, error) {
	title, body, err := splitCapture(in.Text)
	if err != nil {
		return InboxAddOutput{}, err
	}
	doc, err := d.Store.Create(ctx, model.Document{
		Folder: model.FolderInbox,
		Title:  title,
		Body:   body,
		Meta:   &model.Inbox{Common: model.Common{Type: model.TypeInbox}},
	})
	if err != nil {
		return InboxAddOutput{}, err
	}
	return InboxAddOutput{ID: doc.ID, Folder: doc.Folder, Rev: doc.Rev}, nil
}

// splitCapture makes the first line the title and the rest the body. A long
// first line is cut for the title and kept whole in the body.
func splitCapture(text string) (title, body string, err error) {
	text = strings.TrimSpace(strings.ReplaceAll(text, "\r\n", "\n"))
	if text == "" {
		return "", "", errors.New("text is empty")
	}
	first, rest, _ := strings.Cut(text, "\n")
	first = strings.Join(strings.Fields(first), " ")
	if utf8.RuneCountInString(first) <= inboxTitleMax {
		return first, strings.TrimSpace(rest), nil
	}
	return string([]rune(first)[:inboxTitleMax]) + "...", text, nil
}
