// Helpers for the proposal (dossier) screen.

// UNTRUSTED_WORDS is what the untrusted mark means, in one sentence.
export const UNTRUSTED_WORDS = 'This text came from a document, not from you.'

// isOutward says whether an item is an act outside the vault: a message or an action.
export const isOutward = (kind: string) => kind !== 'para' && kind !== 'op'

// REVIEW_HINT says where a decision is made.
export const REVIEW_HINT =
  'Open a dossier to answer and decide, then Submit. The Stack shows every item.'

// stateWords says an item's state for a person.
export function stateWords(state: string, kind: string): string {
  const outward = isOutward(kind)
  switch (state) {
    case 'pending':
      return 'waiting for you'
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
