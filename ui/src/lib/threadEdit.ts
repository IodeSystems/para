// Changing an owner turn after it is sent (thread_edit, thread_withdraw). A turn can change while it
// is queued: no agent has claimed it. The toast after a send offers Edit and Undo for a short time;
// the Thread panel offers them on the turn for as long as it stays queued.

// TURN_UNDO_MS is how long the toast after a send stays, with Edit and Undo: about ten seconds.
export const TURN_UNDO_MS = 10000

// EDIT_TURN is the event that opens a turn's text for change in the Thread panel.
export const EDIT_TURN = 'para:edit-turn'

let pending: string | null = null

// requestEditTurn asks the Thread panel to open the turn (a request id) for change. The panel may
// not be on screen yet (the toast sent the owner to the item's page), so the id waits until a turn
// of that id takes it.
export function requestEditTurn(request: string): void {
  pending = request
  if (typeof window !== 'undefined') window.dispatchEvent(new CustomEvent(EDIT_TURN))
}

// takePendingEdit says whether the turn is the one waiting to be edited, and clears the wait.
export function takePendingEdit(request: string | null | undefined): boolean {
  if (!request || pending !== request) return false
  pending = null
  return true
}

// editable: only a turn that no agent has claimed can change.
export const turnEditable = (status: string | null | undefined): boolean =>
  status === 'queued' || status === 'proposed'

// withdrawable: a turn can be withdrawn until it is done; a claimed turn's agent fails on its next
// note.
export const turnWithdrawable = (status: string | null | undefined): boolean =>
  turnEditable(status) || status === 'claimed'
