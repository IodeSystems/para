package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/iodesystems/para/internal/model"
)

// proposal_executed is an alias of io_send for one release (0.3.0): a
// dossier is read-only since vault format 8, and what a dossier held to send
// is an outgoing io draft (vault upgrade step dossiers). The alias finds the
// draft of the item and records its send with io_send.

// ExecDone and ExecFailed are the results an agent reported.
const (
	ExecDone   = "done"
	ExecFailed = "failed"
)

// ProposalExecutedInput is the input of proposal_executed, as before 0.3.0.
// By, OnBehalfOf, Reason, and Text are read and not used: io_send takes none.
type ProposalExecutedInput struct {
	ID      string `json:"id"`
	Item    string `json:"item"`
	Result  string `json:"result"`            // done; failed is refused: the draft waits
	URL     string `json:"url,omitempty"`     // where the result is
	Locator string `json:"locator,omitempty"` // where the result is, when it has no URL
	Reason  string `json:"reason,omitempty"`
	Text    string `json:"text,omitempty"`
	By      string `json:"by,omitempty"`
	// Observed is io_send's: the owner sent it themself and scan_out saw it.
	Observed   bool   `json:"observed,omitempty"`
	OnBehalfOf string `json:"on_behalf_of,omitempty"`
}

// ProposalExecutedOutput is io_send's output, with alias_of.
type ProposalExecutedOutput struct {
	IOSendOutput
	AliasOf string `json:"alias_of"`
}

// ProposalExecuted runs io_send on the io draft of a dossier item.
var ProposalExecuted = defineSafe("proposal_executed", "Alias of io_send (this name goes in a later release; call io_send). Reports that you sent what a dossier item held: since vault format 8 that is an outgoing io draft (the item's io, or the draft vault upgrade moved it to), and this records its send with io_send (url or locator)", func(ctx context.Context, d Deps, in ProposalExecutedInput) (ProposalExecutedOutput, error) {
	out := ProposalExecutedOutput{AliasOf: "io_send"}
	if r := strings.TrimSpace(in.Result); r != "" && r != ExecDone {
		return out, errors.New("result " + r + ": only done is recorded; a draft that was not sent stays a draft (edit it, or close it with a reason)")
	}
	doc, err := d.Store.Get(ctx, normID(in.ID))
	if err != nil {
		return out, err
	}
	if _, ok := doc.Meta.(*model.Proposal); !ok {
		return out, fmt.Errorf("%s is not a dossier: call io_send with the draft", doc.ID)
	}
	c, err := model.ParseProposalBody(doc.Body)
	if err != nil {
		return out, fmt.Errorf("%s: %w", doc.ID, err)
	}
	draft := ""
	for _, it := range c.Items {
		if it.ID != in.Item {
			continue
		}
		switch {
		case it.State == model.ItemMoved:
			draft = it.Result
		case it.Kind == model.ItemMessage && it.IO != "":
			draft = normID(it.IO)
		}
	}
	if draft == "" {
		return out, fmt.Errorf("%s %s has no io draft: a dossier is read-only since vault format 8; draft what you send with io_add and report it with io_send", doc.ID, in.Item)
	}
	if _, m, err := getIO(ctx, d, draft); err != nil || m.Direction != model.IOOut {
		return out, fmt.Errorf("%s %s went to %s, which is not an outgoing io draft: call io_send with the draft", doc.ID, in.Item, draft)
	}
	res, err := IOSend.Run(ctx, d, IOSendInput{ID: draft, URL: in.URL, Locator: in.Locator, Observed: in.Observed})
	out.IOSendOutput = res
	return out, err
})
