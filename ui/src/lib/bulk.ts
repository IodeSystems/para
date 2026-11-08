// Select mode on a list: the owner picks rows, then one decision (Done, Drop, Later, Reopen)
// applies to all of them through the bulk op. First a dry run says how many would change and
// which are refused and why; then the run applies, and its batch is one Undo.

export type BulkAction = 'done' | 'drop' | 'later' | 'reopen'

// toggle adds id to the selection, or takes it out.
export function toggle(sel: ReadonlySet<string>, id: string): Set<string> {
  const next = new Set(sel)
  if (next.has(id)) next.delete(id)
  else next.add(id)
  return next
}

// toggleAll selects every id of the list, or none when all of them are selected already.
export function toggleAll(sel: ReadonlySet<string>, ids: readonly string[]): Set<string> {
  if (ids.length > 0 && ids.every((id) => sel.has(id))) return new Set()
  return new Set(ids)
}

// keepShown drops selected ids the list no longer shows, such as a row that closed.
export function keepShown(sel: ReadonlySet<string>, ids: readonly string[]): Set<string> {
  const shown = new Set(ids)
  return new Set([...sel].filter((id) => shown.has(id)))
}

// allState is the header checkbox: all, some, or none of the list selected.
export function allState(
  sel: ReadonlySet<string>,
  ids: readonly string[],
): 'all' | 'some' | 'none' {
  const n = ids.filter((id) => sel.has(id)).length
  if (n === 0) return 'none'
  return n === ids.length ? 'all' : 'some'
}

export type BulkBody = {
  verb: 'close' | 'later' | 'reopen' | 'undo'
  ids?: string[]
  outcome?: string
  reason?: string
  until?: string
  dryRun?: boolean
  batch?: string
}

// bulkBody is the input of bulk for an action on the selected ids. reason is the words the owner
// typed in the bar; a drop needs them, as a single Drop does.
export function bulkBody(
  action: BulkAction,
  ids: readonly string[],
  reason: string,
  until = '',
): BulkBody {
  const r = reason.trim() || undefined
  switch (action) {
    case 'done':
      return { verb: 'close', ids: [...ids], reason: r }
    case 'drop':
      return { verb: 'close', ids: [...ids], outcome: 'dropped', reason: r }
    case 'later':
      return { verb: 'later', ids: [...ids], until: until || undefined, reason: r }
    case 'reopen':
      return { verb: 'reopen', ids: [...ids], reason: r }
  }
}

export type BulkItem = {
  id: string
  title?: string | null
  state: string
  error?: string | null
}

export type BulkResult = {
  batch?: string | null
  applied: number
  refused: number
  items: BulkItem[]
}

const WORDS: Record<BulkAction, { did: string; would: string }> = {
  done: { did: 'Marked done', would: 'marked done' },
  drop: { did: 'Dropped', would: 'dropped' },
  later: { did: 'Put off', would: 'put off' },
  reopen: { did: 'Opened again', would: 'opened again' },
}

const items = (n: number) => `${n} item${n === 1 ? '' : 's'}`

// previewText says what the dry run found: how many would change, and how many are refused.
export function previewText(action: BulkAction, r: BulkResult): string {
  const would = `${items(r.applied)} would be ${WORDS[action].would}`
  return r.refused === 0 ? `${would}.` : `${would}; ${r.refused} refused.`
}

// refusals lists the refused items of a result, each with its title and why.
export function refusals(r: BulkResult): { id: string; title: string; why: string }[] {
  return r.items
    .filter((i) => i.state === 'refused')
    .map((i) => ({ id: i.id, title: i.title || i.id, why: i.error || 'refused' }))
}

// doneText is the toast after the run.
export function doneText(action: BulkAction, r: BulkResult): string {
  const did = `${WORDS[action].did} ${items(r.applied)}`
  return r.refused === 0 ? did : `${did}; ${r.refused} refused`
}

// mustReason says the action needs the owner's words before it runs: a drop says why.
export function mustReason(action: BulkAction): boolean {
  return action === 'drop'
}
