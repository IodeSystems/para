package ops

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

// The gate decides whether an agent's write applies. An in-vault write that
// receipt_undo can reverse (Entry.Undoable) applies at once, from any
// surface: its receipt and undo are the review. A write that cannot be undone
// needs the owner: it is refused on an agent surface, and a person (the
// terminal, the web app) makes it. An op can name inputs that make one write
// of it not undoable (withOwnerWhen), such as an archive that items wait on.
//
// Outward acts (send, PR, ticket, deploy, remote doc edit) never run here:
// they are io out drafts the owner approves (io_approve), and the agent
// reports what it sent (io_send). Approval binds the draft's hash.
//
// The tick, and a write an approved dossier item runs, skip the gate: the
// owner decided those. Ratings (minor, obvious) and owner_said on other ops are
// accepted and ignored for one release (IgnoredRatings).

// GateConfig is the vault setting that chose the rating gate. It is no longer
// read; health names it while it is set.
const GateConfig = "gate.require"

// IgnoredRatings are the write fields an agent may still send that decide
// nothing since 0.3.0. A surface accepts them and says so in a note.
var IgnoredRatings = []string{"minor", "obvious", "dossier", "owner_said"}

// RatingsNote is the note a surface gives when a write carried ignored
// ratings. names are the ones it carried.
func RatingsNote(names []string) string {
	return fmt.Sprintf("%s ignored: an undoable write applies at once, with a receipt the owner can undo; a write that cannot be undone needs the owner; owner_said counts only on io_approve (these fields go in a later release)", strings.Join(names, ", "))
}

// withOwnerWhen sets a rule that makes one write of the op need the owner: it
// returns why the write cannot be undone, or "".
func withOwnerWhen[I, O any](o *Op[I, O], rule func(ctx context.Context, d Deps, in I) (string, error)) *Op[I, O] {
	o.ownerWhen = rule
	return o
}

// needsOwner returns why an agent may not make this write itself, or "".
func needsOwner[I, O any](ctx context.Context, d Deps, o *Op[I, O], in I) (string, error) {
	if !agentSurface(d) || o.person || o.risk == RiskRead || slices.Contains(FlowOps, o.name) {
		return "", nil
	}
	if s := d.Store.Info().Surface; s == surfaceProposal || s == TickSurface {
		return "", nil
	}
	if o.noUndo {
		return "receipt_undo cannot reverse it", nil
	}
	if o.ownerWhen != nil {
		return o.ownerWhen(ctx, d, in)
	}
	return "", nil
}

// ownerError is the refusal of a write only the owner may make.
func ownerError(op, why string) error {
	return fmt.Errorf("%s: %w: this write cannot be undone (%s), so only the owner makes it; ask the owner, and do not retry", op, ErrPersonOnly, why)
}
