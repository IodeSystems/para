// Helpers for Waits on and Blocks on an item page: what the item waits on, and what waits on it.
// A row is changed with depends_set on the item that waits: on this item's page for Waits on, on
// the other item for Blocks. An ask's blocks list is changed on the ask, so its rows have no
// action here.

// WAIT_TYPES are the types that can wait on something (they hold depends_on).
export const WAIT_TYPES = ['task', 'obligation', 'rfx', 'question']

export function canWait(type: string | null | undefined): boolean {
  return WAIT_TYPES.includes(type ?? '')
}

export type DepRowLike = {
  id?: string | null
  reason?: string | null
  optional: boolean
  via: string
}

export type DepBody = {
  id: string
  on?: string
  reason?: string
  optional?: boolean
  remove?: boolean
}

export type DepActions = {
  remove: DepBody
  // toggle flips optional; a text reason has none: it always blocks.
  toggle?: DepBody
  toggleLabel?: string
}

// depActions says what Remove and Make optional / Make required send for a row, seen from item
// (the page). side is waits (the row is what item waits on) or blocks (the row waits on item).
export function depActions(
  item: string,
  side: 'waits' | 'blocks',
  row: DepRowLike,
): DepActions | null {
  if (row.via === 'rfx_blocks') return null
  if (row.via === 'blocked_by') {
    if (side !== 'waits' || !row.reason) return null
    return { remove: { id: item, reason: row.reason, remove: true } }
  }
  if (!row.id) return null
  const [id, on] = side === 'waits' ? [item, row.id] : [row.id, item]
  return {
    remove: { id, on, remove: true },
    toggle: { id, on, optional: !row.optional },
    toggleLabel: row.optional ? 'Make required' : 'Make optional',
  }
}
