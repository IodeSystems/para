// A proposed close of a task: the callout line and when it is highlighted. HIGH matches
// model.HighCloseConfidence on the server.

export const HIGH_CLOSE_CONFIDENCE = 80

const OUTCOME: Record<string, string> = {
  done: 'done',
  dropped: 'not needed',
  duplicate: 'a duplicate',
  superseded: 'superseded',
  declined: 'declined',
  failed: 'failed',
  rerouted: 'rerouted',
  partial: 'partly done',
}

// closeLine is the callout: "Likely done, 90%: <reason>".
export function closeLine(c: { outcome: string; confidence: number; reason: string }): string {
  const what = OUTCOME[c.outcome] ?? c.outcome
  return `Likely ${what}, ${c.confidence}%: ${c.reason}`
}

// isHighClose says whether a proposed close gets the accent highlight.
export function isHighClose(confidence: number): boolean {
  return confidence >= HIGH_CLOSE_CONFIDENCE
}

// notItQuote is the quote of the owner's turn when a proposed close is wrong: the words, quoted on the proposal.
export function notItQuote(c: { outcome: string; reason: string }): string {
  return `Proposed close (${c.outcome}): ${c.reason}`
}
