// The Pinned dashboard's pure parts: which target a card or button names, the card's lines, and the
// "lots of plates" hint. The server orders the cards (needs you, next date, newest pin).

export type CardLike = {
  id: string
  type: string
  key?: string | null
  title: string
  state?: string | null
  life: string
  nextDate?: string | null
  nextKind?: string | null
  waitingOn: readonly string[]
  waitingNames?: readonly (string | null)[] | null
  queued: number
  claimed: number
  last?: { at: string; who: string; kind: string; summary: string } | null
  turns: number
  lastReply?: string | null
  needsYou: boolean
  stale?: string | null
}

// PLATES is how many pins pass before the dashboard says so. It is a hint, never a limit.
export const PLATES = 12

// isPinned says whether an item id or a thread key is among the pinned cards.
export function isPinned(
  cards: readonly Pick<CardLike, 'id' | 'key'>[],
  target: { id?: string; key?: string },
): boolean {
  return cards.some(
    (c) =>
      (target.id !== undefined && c.id === target.id) || (!!target.key && c.key === target.key),
  )
}

// lotsOfPlates is the hint: past PLATES pins, name the ones closed or quiet for 30 days (the
// server's stale) that could go. Null while the count is within PLATES.
export function lotsOfPlates(
  cards: readonly Pick<CardLike, 'id' | 'title' | 'stale'>[],
): { count: number; suggest: { id: string; title: string; why: string }[] } | null {
  if (cards.length <= PLATES) return null
  const suggest = cards
    .filter((c) => !!c.stale)
    .map((c) => ({ id: c.id, title: c.title, why: c.stale ?? '' }))
  return { count: cards.length, suggest }
}

const NEXT: Record<string, string> = {
  due: 'due',
  scheduled: 'on',
  nudge: 'nudge',
  turn: 'your turn',
}

// nextWords is the next date of a card: "due 2026-12-01", or "".
export function nextWords(c: Pick<CardLike, 'nextDate' | 'nextKind'>): string {
  if (!c.nextDate) return ''
  const k = NEXT[c.nextKind ?? ''] ?? ''
  return k ? `${k} ${c.nextDate}` : c.nextDate
}

// waitingWords is who or what the card waits on, as names, and the agent turns it has.
export function waitingWords(
  c: Pick<CardLike, 'waitingOn' | 'waitingNames' | 'queued' | 'claimed'>,
): string {
  const names = c.waitingOn.map((id, i) => c.waitingNames?.[i] || id)
  const parts: string[] = []
  if (names.length > 0) parts.push(`waits on ${names.join(', ')}`)
  if (c.claimed > 0) parts.push(`${c.claimed} with an agent`)
  if (c.queued > 0) parts.push(`${c.queued} queued for an agent`)
  return parts.join(' · ')
}

// lastWords is the last change of a card: "owner edited, 2026-11-09", or "".
export function lastWords(c: Pick<CardLike, 'last'>): string {
  const l = c.last
  if (!l) return ''
  const what = l.summary || l.kind
  return `${[l.who, what].filter(Boolean).join(' ')}, ${l.at.slice(0, 10)}`
}

// threadWords is the thread chip: how many turns are open.
export function threadWords(c: Pick<CardLike, 'turns'>): string {
  return c.turns === 0 ? '' : `${c.turns} open turn${c.turns === 1 ? '' : 's'}`
}

// tagOf is the tag of a "#tag" thread key, or null for a channel key or no key.
export function tagOf(key?: string | null): string | null {
  return key?.startsWith('#') ? key.slice(1) : null
}
