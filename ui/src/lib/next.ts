// The Next page: one card at a time, the top of the value order that needs the owner. Skip is
// navigation, kept in memory only (D8): a reload is a new visit. Items done, put off, sent, or
// approved this visit leave the deck (gone) until Undo brings them back.

import { canDecide, type SessionView } from '@/client/session'

export type DeckRow = {
  id: string
  needsYou: boolean
  bucket: string
  ready?: boolean | null
}

// deckOf is what the cards are drawn from: rows that need the owner and can start now, in the
// server's order. A row that cannot start yet (not_yet) is left out.
export function deckOf<R extends DeckRow>(rows: readonly R[]): R[] {
  return rows.filter((r) => r.needsYou && r.bucket !== 'not_yet')
}

// QUICK_BATCH is the id of the quick approvals card: ready approvals that unblock nothing (group
// quick), one card in the deck where the first of them is. Opening it puts its rows in the deck in
// their place, so the owner walks them one by one.
export const QUICK_BATCH = 'quick:batch'

export type QuickBatch<R> = { id: typeof QUICK_BATCH; batch: R[] }

// isQuickBatch tells the batch card from a row.
export function isQuickBatch<R>(c: R | QuickBatch<R>): c is QuickBatch<R> {
  return (c as { id?: string }).id === QUICK_BATCH && 'batch' in (c as object)
}

// withQuickBatch folds the quick rows not gone into one batch card at the place of the first, or,
// when the batch is open, leaves them in the deck as they are.
export function withQuickBatch<R extends { id: string; group?: string | null }>(
  deck: readonly R[],
  gone: ReadonlySet<string>,
  open: boolean,
): (R | QuickBatch<R>)[] {
  if (open) return [...deck]
  const batch = deck.filter((r) => r.group === 'quick' && !gone.has(r.id))
  const out: (R | QuickBatch<R>)[] = []
  let placed = false
  for (const r of deck) {
    if (r.group !== 'quick') {
      out.push(r)
      continue
    }
    if (!placed && batch.length > 0) {
      out.push({ id: QUICK_BATCH, batch })
      placed = true
    }
  }
  return out
}

// left is the deck without the items gone this visit.
export function left<R extends { id: string }>(deck: readonly R[], gone: ReadonlySet<string>): R[] {
  return deck.filter((r) => !gone.has(r.id))
}

// currentCard is the first card not gone and not skipped, or null.
export function currentCard<R extends { id: string }>(
  deck: readonly R[],
  skipped: readonly string[],
  gone: ReadonlySet<string>,
): R | null {
  return deck.find((r) => !gone.has(r.id) && !skipped.includes(r.id)) ?? null
}

// skipCard adds a card to the skipped list, once, in the order skipped.
export function skipCard(skipped: readonly string[], id: string): string[] {
  return skipped.includes(id) ? [...skipped] : [...skipped, id]
}

// backCard takes the last skip back: that card shows again.
export function backCard(skipped: readonly string[]): string[] {
  return skipped.slice(0, -1)
}

export type OrderSummary = { reply: number; replyMinutes: number }

// headerLine counts what is left: how many need the owner, how many are ready, and the reply batch.
export function headerLine(rows: readonly { ready?: boolean | null }[], s: OrderSummary): string {
  const ready = rows.filter((r) => r.ready).length
  const parts = [`${rows.length} need${rows.length === 1 ? 's' : ''} you`, `${ready} ready`]
  if (s.reply > 0) parts.push(`reply batch ${s.reply}, about ${s.replyMinutes} min`)
  return parts.join(' · ')
}

const GROUP: Record<string, string> = {
  incident: 'Incident',
  owed: 'Owed',
  reply: 'Reply',
  focus: 'Focus',
  quick: 'Quick approval',
  recheck: 'Recheck',
  impact: 'Then',
}

// groupWords names the group of the value order a card is from.
export function groupWords(g?: string | null): string {
  return (g && GROUP[g]) || ''
}

const EFFORT: Record<string, string> = {
  xs: 'xs, under 15 min',
  s: 's, under an hour',
  m: 'm, half a day',
  l: 'l, a day or more',
}

// effortWords is the effort chip of a card.
export function effortWords(e?: string | null): string {
  return (e && EFFORT[e]) || ''
}

// homeFor is where a signed-in session lands: the owner (review:decide) on Pinned (owner
// 2026-11-10; Next is one click away), a read-only key on the agenda.
export function homeFor(s: SessionView | null | undefined): '/pinned' | '/agenda' {
  return canDecide(s) ? '/pinned' : '/agenda'
}
