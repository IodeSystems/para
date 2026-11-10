// Pure helpers for the owner's three ways to settle an Ask by hand: Answered, Dropped, Rerouted.

export type AskTarget = { to: string; medium: string; state: string }

// openTargets are the targets that have not replied: who can still have answered.
export function openTargets<T extends AskTarget>(targets: T[]): T[] {
  return targets.filter((t) => t.state !== 'replied')
}

// targetKey tells two targets of one person apart: the same person can be asked by two mediums.
export const targetKey = (t: { to: string; medium: string }) => `${t.to}|${t.medium}`

// settlesAsk says whether replying for target key leaves no open target: the Ask is then answered.
export function settlesAsk(targets: AskTarget[], key: string): boolean {
  return openTargets(targets).every((t) => targetKey(t) === key)
}

// HEARD are the places a reply was heard; code is the rfx_reply heard value.
export const HEARD: { code: string; label: string }[] = [
  { code: '', label: 'Not said' },
  { code: 'in_person', label: 'In person' },
  { code: 'call', label: 'On a call' },
  { code: 'message', label: 'In a message' },
  { code: 'other', label: 'Somewhere else' },
]

// needsLink says whether a reply must carry a link: a no is recorded as a constraint, which keeps
// its source. The owner can give where it was heard instead of a link.
export const needsLink = (outcome: string, heard = '') => outcome === 'no' && !heard

// droppedNote is the note a drop leaves on the Ask.
export const droppedNote = (reason: string) => `Dropped: ${reason.trim()}`

// httpUrl says whether text is an http(s) link.
export function httpUrl(s: string): boolean {
  try {
    const u = new URL(s.trim())
    return u.protocol === 'http:' || u.protocol === 'https:'
  } catch {
    return false
  }
}

export const MEDIUMS = [
  'email',
  'slack',
  'teams',
  'jira',
  'confluence',
  'github',
  'phone',
  'meeting',
  'verbal',
  'swivel',
  'other',
] as const
