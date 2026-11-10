// The Input page: incoming messages sorted by what the owner does with them.

export type InputTab = 'needs' | 'changed' | 'rest' | 'reviewed'

type Row = {
  needsYou: boolean
  digest?: boolean | null
  made?: number | null
  reviewed?: string | null
}

// inputTab says which tab an incoming row is on. Reviewed wins. Then needs you (it expects a
// response). Then changed para (an agent already made something from it, unreviewed). The rest is
// the unreviewed digest.
export function inputTab(r: Row): InputTab {
  if (r.reviewed) return 'reviewed'
  if (r.needsYou) return 'needs'
  if ((r.made ?? 0) > 0) return 'changed'
  return 'rest'
}

// splitInput groups rows by tab, keeping their order; Reviewed is newest review first.
export function splitInput<T extends Row>(rows: T[]): Record<InputTab, T[]> {
  const out: Record<InputTab, T[]> = { needs: [], changed: [], rest: [], reviewed: [] }
  for (const r of rows) out[inputTab(r)].push(r)
  out.reviewed.sort((a, b) => (b.reviewed ?? '').localeCompare(a.reviewed ?? ''))
  return out
}
