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

export const HEARD: { code: string; label: string }[] = [
  { code: '', label: 'Not said' },
  { code: 'in person', label: 'In person' },
  { code: 'on a call', label: 'On a call' },
  { code: 'in a message', label: 'In a message' },
]

// replyText is the reply the owner types, with where it was heard in front. The first line is the
// reply's title in the vault, so the place goes there.
export function replyText(answer: string, heard: string): string {
  const a = answer.trim()
  return heard ? `Heard ${heard}: ${a}` : a
}

// needsLink says whether a reply must carry a link: a no is recorded as a constraint, which keeps
// its source URL.
export const needsLink = (outcome: string) => outcome === 'no'

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
