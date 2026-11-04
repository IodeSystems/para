import type { Tone } from '@/lib/status'

// Helpers for the review stack: the stack states in order, their words, and the flat list of
// lines the stack page shows (a dossier, then its groups, then the items).

// STACK_STATES lists the stack states in order. They match ops.StackStates.
export const STACK_STATES = [
  'proposed',
  'decided',
  'executing',
  'done',
  'failed',
  'closed',
] as const
export type StackState = (typeof STACK_STATES)[number]

// stackWords says a stack state for a person.
export function stackWords(state: string): string {
  switch (state) {
    case 'proposed':
      return 'to decide'
    case 'decided':
      return 'decided, not done'
    case 'executing':
      return 'in progress'
    case 'done':
      return 'done'
    case 'failed':
      return 'failed'
    case 'closed':
      return 'closed'
    default:
      return state.replace(/_/g, ' ')
  }
}

// stackTone is the color of a stack state, with the meanings in lib/status.ts.
export function stackTone(state: string): Tone {
  switch (state) {
    case 'proposed':
      return 'attn'
    case 'decided':
    case 'executing':
      return 'wait'
    case 'done':
      return 'good'
    case 'failed':
      return 'bad'
    default:
      return 'quiet'
  }
}

// kindWords says an item kind for a person.
export function kindWords(kind: string): string {
  switch (kind) {
    case 'question':
      return 'question'
    case 'para':
    case 'op':
      return 'change in the vault'
    case 'message':
    case 'external':
      return 'message'
    case 'action':
      return 'action'
    case 'doc_edit':
      return 'document edit'
    case 'upstream':
      return 'upstream'
    default:
      return kind.replace(/_/g, ' ')
  }
}

// canStackDecide says whether an item can be approved or rejected from the stack. A question
// needs an answer, so it opens in its dossier.
export function canStackDecide(row: { kind: string; stack: string }): boolean {
  return row.stack === 'proposed' && row.kind !== 'question'
}

export type StackRowLike = {
  proposal: string
  title: string
  item: string
  kind: string
  stack: string
  group?: string | null
}

export type GroupLike = {
  proposal: string
  group: string
  done: number
  total: number
  proposed: number
}

export type Line<R> =
  | { type: 'dossier'; key: string; proposal: string; title: string; count: number }
  | {
      type: 'group'
      key: string
      proposal: string
      group: string
      done: number
      total: number
      proposed: number
    }
  | { type: 'item'; key: string; row: R }

// rowKey names one item across dossiers.
export const rowKey = (r: { proposal: string; item: string }) => `${r.proposal}/${r.item}`

// stackLines groups the rows by dossier, then by group, in the order they first come. Items with
// no group come first in their dossier. Group progress comes from the server's groups, which
// count every item of the group and not only the rows on this page.
export function stackLines<R extends StackRowLike>(rows: R[], groups: GroupLike[]): Line<R>[] {
  const progress = new Map(groups.map((g) => [`${g.proposal}/${g.group}`, g]))
  const order: string[] = []
  const byDossier = new Map<string, { title: string; loose: R[]; groups: Map<string, R[]> }>()
  for (const r of rows) {
    let d = byDossier.get(r.proposal)
    if (!d) {
      d = { title: r.title, loose: [], groups: new Map() }
      byDossier.set(r.proposal, d)
      order.push(r.proposal)
    }
    if (!r.group) {
      d.loose.push(r)
      continue
    }
    const g = d.groups.get(r.group)
    if (g) g.push(r)
    else d.groups.set(r.group, [r])
  }
  const out: Line<R>[] = []
  for (const p of order) {
    const d = byDossier.get(p)
    if (!d) continue
    let count = d.loose.length
    for (const g of d.groups.values()) count += g.length
    out.push({ type: 'dossier', key: `d:${p}`, proposal: p, title: d.title, count })
    for (const r of d.loose) out.push({ type: 'item', key: rowKey(r), row: r })
    for (const [name, list] of d.groups) {
      const g = progress.get(`${p}/${name}`)
      out.push({
        type: 'group',
        key: `g:${p}/${name}`,
        proposal: p,
        group: name,
        done: g?.done ?? 0,
        total: g?.total ?? list.length,
        proposed: g?.proposed ?? list.filter((r) => r.stack === 'proposed').length,
      })
      for (const r of list) out.push({ type: 'item', key: rowKey(r), row: r })
    }
  }
  return out
}

// nextItem moves the cursor to the next (step 1) or the previous (step -1) item line, and skips
// dossier and group lines. With no cursor it starts at the first item.
export function nextItem<R>(lines: Line<R>[], cursor: string | null, step: 1 | -1): string | null {
  const items = lines.filter((l) => l.type === 'item')
  if (items.length === 0) return null
  const at = cursor ? items.findIndex((l) => l.key === cursor) : -1
  if (at < 0) return items[step === 1 ? 0 : items.length - 1]?.key ?? null
  const to = Math.min(items.length - 1, Math.max(0, at + step))
  return items[to]?.key ?? null
}
