// One meaning per color, across the app:
//   good  green  finished or answered
//   wait  blue   in progress, or in someone else's hands
//   attn  amber  needs a look soon
//   bad   red    blocked, overdue, or failed
//   quiet gray   a plain state with no call to act
export type Tone = 'good' | 'wait' | 'attn' | 'bad' | 'quiet'

const TONE: Record<string, Tone> = {
  done: 'good',
  answered: 'good',
  replied: 'good',
  approved: 'good',
  resolved: 'good',
  closed: 'good',
  filed: 'good',
  sent: 'wait',
  doing: 'wait',
  queued: 'wait',
  claimed: 'wait',
  mitigated: 'wait',
  waiting: 'wait',
  no_reply: 'attn',
  stalled: 'attn',
  partial: 'attn',
  proposed: 'attn',
  unread: 'attn',
  ready: 'attn',
  sent_back: 'attn',
  superseded: 'attn',
  unverified: 'attn',
  blocked: 'bad',
  failed: 'bad',
  overdue: 'bad',
  declined: 'bad',
  rerouted: 'wait',
}

export function toneOf(state: string): Tone {
  return TONE[state] ?? 'quiet'
}

// plainState is a state in words: no_reply is "no reply".
export function plainState(state: string): string {
  return state.replace(/_/g, ' ')
}

// toneOfSeverity: sev1 is red, sev2 amber, the rest gray.
export function toneOfSeverity(sev: string): Tone {
  return sev === 'sev1' ? 'bad' : sev === 'sev2' ? 'attn' : 'quiet'
}
