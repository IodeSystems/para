// When a theme ends (a goal, an obligation, a question, a tag), the owner reviews the open items
// that hang on it: each is Drop or Keep, Keep may move it to an area, and nothing changes until
// Confirm. Confirm closes the theme and applies the drops and moves as one batch (theme_close).

import type { Para_ObligationChangeInputInput } from '@/gql/graphql'

export type Mark = { mark: 'drop' | 'keep'; area?: string }
export type Marks = ReadonlyMap<string, Mark>

// mark sets one item's decision.
export function mark(m: Marks, id: string, v: Mark): Map<string, Mark> {
  const next = new Map(m)
  next.set(id, v)
  return next
}

// markRest decides every item not decided yet: Drop the rest, or Keep the rest.
export function markRest(m: Marks, ids: readonly string[], v: 'drop' | 'keep'): Map<string, Mark> {
  const next = new Map(m)
  for (const id of ids) if (!next.has(id)) next.set(id, { mark: v })
  return next
}

// left is the items not decided yet; Confirm waits until there are none.
export function left(m: Marks, ids: readonly string[]): string[] {
  return ids.filter((id) => !m.has(id))
}

// tally says what Confirm will do.
export function tally(
  m: Marks,
  ids: readonly string[],
): { drop: number; keep: number; move: number } {
  let drop = 0
  let keep = 0
  let move = 0
  for (const id of ids) {
    const x = m.get(id)
    if (!x) continue
    if (x.mark === 'drop') drop++
    else if (x.area) move++
    else keep++
  }
  return { drop, keep, move }
}

// How the theme closes: the close verb's outcome and reason, or an obligation_change cancel body.
export type ThemeEnd =
  | { kind: 'close'; outcome?: string; reason?: string }
  | { kind: 'change'; change: Para_ObligationChangeInputInput }
  // closed already (a question just answered): only the drops and moves run.
  | { kind: 'closed' }

export type ThemeCloseBody = {
  theme: string
  outcome?: string
  reason?: string
  change?: Para_ObligationChangeInputInput
  drop: string[]
  keep: { id: string; area?: string }[]
}

// themeCloseBody is the input of theme_close for the owner's marks, in list order.
export function themeCloseBody(
  theme: string,
  end: ThemeEnd,
  m: Marks,
  ids: readonly string[],
): ThemeCloseBody {
  const body: ThemeCloseBody = { theme, drop: [], keep: [] }
  if (end.kind === 'close') {
    if (end.outcome) body.outcome = end.outcome
    if (end.reason?.trim()) body.reason = end.reason.trim()
  } else if (end.kind === 'change') body.change = end.change
  for (const id of ids) {
    const x = m.get(id)
    if (x?.mark === 'drop') body.drop.push(id)
    else if (x) body.keep.push(x.area ? { id, area: x.area } : { id })
  }
  return body
}

// reviewsOnClose says a close opens the review when items hang on the item: a goal, a question or
// decision dropped, an obligation closed. A meeting held keeps what came of it, so only a cancelled
// one asks.
export function reviewsOnClose(kind: string | null | undefined, outcome?: string): boolean {
  switch (kind) {
    case 'goal':
    case 'question':
    case 'obligation':
    case 'commitment':
      return true
    case 'meeting':
      return outcome === 'dropped' || outcome === 'cancelled'
    default:
      return false
  }
}

const n = (k: number, w: string) => `${k} ${w}${k === 1 ? '' : 's'}`

// confirmText is the line above Confirm.
export function confirmText(m: Marks, ids: readonly string[]): string {
  const rest = left(m, ids).length
  if (rest > 0) return `${n(rest, 'item')} left to decide.`
  const t = tally(m, ids)
  const parts = [t.drop && `drop ${t.drop}`, t.keep && `keep ${t.keep}`, t.move && `move ${t.move}`]
  return `Confirm will ${parts.filter(Boolean).join(', ') || 'change nothing under it'}.`
}

// reviewDoneText is the toast after Confirm: what closed, how many were dropped and moved, and how
// many were refused. A move runs doc_edit; a drop runs the item's close.
export function reviewDoneText(
  title: string,
  closed: boolean,
  out: { refused: number; items: readonly { op?: string | null; state: string }[] },
): string {
  const ok = out.items.filter((i) => i.state !== 'refused')
  const moved = ok.filter((i) => i.op === 'doc_edit').length
  const dropped = ok.length - moved
  const parts = [`${closed ? 'Closed' : 'Reviewed'} ${title}`]
  if (dropped) parts.push(`dropped ${dropped}`)
  if (moved) parts.push(`moved ${moved}`)
  if (out.refused) parts.push(`${out.refused} refused`)
  return parts.join('; ')
}
