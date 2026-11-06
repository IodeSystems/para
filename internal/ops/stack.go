package ops

import (
	"github.com/iodesystems/para/internal/model"
)

// The review stack is every item of every current dossier, by stack state:
//
//	proposed:  waits for the owner (pending, held, sent_back, skipped)
//	decided:   approved outward item not claimed yet, approved change that
//	           waits its turn, answered question with no for
//	executing: running, claimed
//	done:      done, or a vault change para ran, or a question para recorded
//	failed:    failed, halted
//	closed:    denied (listed, with the owner's reason), withdrawn (listed
//	           only when asked)
//	later:     waits for the owner, put off until a date or someday. It only
//	           surfaces later: the unscoped needs-you view hides it until its
//	           date, but a view scoped by tag, dossier, or group lists it
//	           (and with state later). It returns to proposed on its date.
//
// A skip is not a deferral: a skipped item stays proposed. It is only left out
// of the needs-you view of the batch (one visit) that skipped it.
const (
	StackProposed  = "proposed"
	StackDecided   = "decided"
	StackExecuting = "executing"
	StackDone      = "done"
	StackFailed    = "failed"
	StackClosed    = "closed"
	StackLater     = "later"
)

// stackState is the stack state of an item on the date today.
func stackState(it model.ProposalItem, today string) string {
	switch it.State {
	case model.ItemPending, model.ItemHeld, model.ItemSentBack, model.ItemSkipped:
		if it.Deferred(today) {
			return StackLater
		}
		return StackProposed
	case model.ItemApproved:
		if it.Kind.Outward() || it.Waits {
			return StackDecided
		}
		return StackDone
	case model.ItemAnswered:
		if it.For == "" {
			return StackDecided
		}
		return StackDone
	case model.ItemRunning, model.ItemClaimed:
		return StackExecuting
	case model.ItemDone:
		return StackDone
	case model.ItemFailed, model.ItemHalted:
		return StackFailed
	}
	return StackClosed
}
