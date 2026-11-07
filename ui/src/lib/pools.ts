// Idea pools: a firing pool is one row of the order, id pool:<name>, not a document.

// isPoolRow says whether an order row is a firing pool, not an item.
export function isPoolRow(r: { id: string; kind?: string | null }): boolean {
  return r.kind === 'pool' || r.id.startsWith('pool:')
}

// pickWords is the toast after a pick: how many came back, and how many are prep of the trigger.
export function pickWords(picked: number, linked: number, trigger?: string | null): string {
  const n = `${picked} idea${picked === 1 ? '' : 's'}`
  if (linked > 0 && trigger) return `Picked ${n}; ${linked} now prep for ${trigger}`
  return `Picked ${n}; back in the order now`
}

// nextFire says when a pool that does not fire now fires next.
export function nextFire(p: {
  fires: boolean
  next?: string | null
  problem?: string | null
}): string {
  if (p.fires) return 'fires now'
  if (p.problem) return p.problem
  return p.next ? `next ${p.next}` : 'waits on its trigger'
}
