// The owner's edit of an outgoing draft's text. io_edit with state ready is the owner's approval of
// the text it saves, so "and approve" is one call. Without it, an edit of an approved draft makes
// it a draft again (the server's rule).

export type DraftEditBody = { id: string; text?: string; state?: 'ready' }

// draftEditBody is the io_edit input for Save. text is sent only when it changed; approve adds state
// ready, which approves the text as saved. It is null when there is nothing to save.
export function draftEditBody(
  id: string,
  saved: string,
  text: string,
  approve: boolean,
): DraftEditBody | null {
  const changed = text.trim() !== '' && text !== saved
  if (!changed && !approve) return null
  return { id, ...(changed ? { text } : {}), ...(approve ? { state: 'ready' as const } : {}) }
}
