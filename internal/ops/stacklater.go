package ops

import (
	"context"
	"errors"
)

// Later on the review stack: the owner puts stack items off from the web app
// (review:decide) and brings them back with Undo. Both run defer and undefer
// as the owner.

// StackLaterInput is the input of stack_later. Until is YYYY-MM-DD; empty
// means someday.
type StackLaterInput struct {
	Items     []ItemRef `json:"items,omitempty"`     // dossier items
	Questions []string  `json:"questions,omitempty"` // open vault questions
	Until     string    `json:"until,omitempty"`
	Reason    string    `json:"reason,omitempty"`
}

// StackLaterOp puts stack items off as the owner.
var StackLaterOp = definePerson("stack_later", "Put review stack items off until a date, or someday: dossier items and open vault questions leave the stack (state later) and return on their date. People only; agents use defer", func(ctx context.Context, d Deps, in StackLaterInput) (DeferOutput, error) {
	if len(in.Items) == 0 && len(in.Questions) == 0 {
		return DeferOutput{}, errors.New("nothing to put off: give items or questions")
	}
	return Defer.Run(ctx, d, DeferInput{IDs: in.Questions, Items: in.Items, Until: in.Until, Reason: in.Reason})
})

// StackReturnInput is the input of stack_return.
type StackReturnInput struct {
	Items     []ItemRef `json:"items,omitempty"`
	Questions []string  `json:"questions,omitempty"`
}

// StackReturn brings stack items that were put off back now: the Undo of
// stack_later.
var StackReturn = definePerson("stack_return", "Bring review stack items that were put off back now: the undo of stack_later. People only", func(ctx context.Context, d Deps, in StackReturnInput) (DeferOutput, error) {
	if len(in.Items) == 0 && len(in.Questions) == 0 {
		return DeferOutput{}, errors.New("nothing to bring back: give items or questions")
	}
	return Undefer.Run(ctx, d, UndeferInput{IDs: in.Questions, Items: in.Items})
})
