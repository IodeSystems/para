package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/iodesystems/parasolve/internal/model"
)

const opActionRecord = "action_record"

// ActionRecordInput is the input of action_record.
type ActionRecordInput struct {
	What      string   `json:"what"`                 // what the agent did, in plain words
	Where     string   `json:"where"`                // the system: a repo, a channel, a ticket queue
	URL       string   `json:"url,omitempty"`        // where the result can be seen
	OwnerSaid string   `json:"owner_said,omitempty"` // the owner's go-ahead from the chat, verbatim
	Sources   []string `json:"sources,omitempty"`    // documents or URLs the action rests on
}

// ActionRecordOutput is the output of action_record.
type ActionRecordOutput struct {
	Receipt string `json:"receipt"`
}

// ActionRecord keeps a receipt of an outward action the agent made with its
// own tools: a PR, a message, a ticket. Para sends nothing and cannot see the
// chat, so the receipt is self-reported, owner_said included.
var ActionRecord = defineSafe(opActionRecord, "Record an outward action you did with your own tools (a PR, a message, a ticket): what, where, the URL, the owner's go-ahead in their words, and sources. Para sends nothing; this is the record", func(ctx context.Context, d Deps, in ActionRecordInput) (ActionRecordOutput, error) {
	what := strings.Join(strings.Fields(in.What), " ")
	where := strings.Join(strings.Fields(in.Where), " ")
	said := strings.TrimSpace(in.OwnerSaid)
	if what == "" || where == "" {
		return ActionRecordOutput{}, errors.New("what and where are required: say what you did, and in which system")
	}
	url := strings.TrimSpace(in.URL)
	if url != "" {
		u, err := checkURL(url)
		if err != nil {
			return ActionRecordOutput{}, err
		}
		url = u
	}
	for name, v := range map[string]string{"what": what, "where": where, "owner_said": said} {
		if err := lintText(name, v); err != nil {
			return ActionRecordOutput{}, err
		}
	}
	info, err := identity(d)
	if err != nil {
		return ActionRecordOutput{}, err
	}
	text := fmt.Sprintf("Action: %s\nWhere: %s", what, where)
	if url != "" {
		text += "\nURL: " + url
	}
	if said != "" {
		text += "\nOwner said: " + said
	}
	id, err := createReceipt(ctx, d, receiptInput{op: opActionRecord, doc: "vault", title: what,
		meta:    WriteMeta{Why: what, Sources: in.Sources, Verify: url, Request: heldRequest(ctx, d.Store, info.Actor)},
		content: model.ReceiptContent{After: text}, selfReported: true, ownerSaid: said})
	if err != nil {
		return ActionRecordOutput{}, err
	}
	return ActionRecordOutput{Receipt: id}, nil
})
