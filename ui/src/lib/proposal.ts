// Helpers for the proposal screen. The owner decides in the chat; the page only shows.

// UNTRUSTED_WORDS is what the untrusted mark means, in one sentence.
export const UNTRUSTED_WORDS = 'This text came from a document, not from you.'

// isOutward says whether an item is an act outside the vault: a message or an action.
export const isOutward = (kind: string) => kind !== 'para' && kind !== 'op'

// DECIDE_HINT is where a decision is made.
export const DECIDE_HINT = 'Decide in the chat with your agent. This page only shows.'

// stateWords says an item's state for a person.
export function stateWords(state: string, kind: string): string {
  const outward = isOutward(kind)
  switch (state) {
    case 'pending':
      return 'waiting for you in the chat'
    case 'approved':
      return outward ? 'approved, not done' : 'approved'
    case 'done':
      return 'done'
    case 'denied':
      return 'denied'
    case 'sent_back':
      return 'sent back'
    default:
      return state.replace(/_/g, ' ')
  }
}
