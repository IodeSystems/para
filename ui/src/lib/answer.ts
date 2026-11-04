// Words for agent answers and their sources.

export type QuestionLike = {
  status: string
  hasProposal: boolean
  waitingOn: readonly string[]
}

// isUrl says whether a source ref is a web address. Anything else is a vault document id.
export function isUrl(ref: string): boolean {
  return /^https?:\/\//i.test(ref.trim())
}

// questionState is the state a question shows: a proposed answer to check, waiting on other
// questions, or its plain status.
export function questionState(q: QuestionLike): { state: string; label: string } {
  if (q.status === 'open' && q.hasProposal) return { state: 'proposed', label: 'proposed answer' }
  if (q.status === 'open' && q.waitingOn.length > 0) return { state: 'waiting', label: 'waiting' }
  return { state: q.status, label: q.status.replace(/_/g, ' ') }
}

// proposedText is what the agent proposed, as one line: free text, else the chosen option.
export function proposedText(q: {
  proposedAnswer?: string | null
  proposedChoice?: string | null
}): string {
  return q.proposedAnswer || q.proposedChoice || ''
}
