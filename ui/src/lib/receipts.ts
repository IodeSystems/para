// Helpers for the Recent agent writes screen. The viewer never writes: an undo is a command to
// copy and run, or to ask the agent for in the chat.

type Stamped = { at?: string | null; created: string; actor: string; request?: string | null }

// receiptTime is when a write happened: `at` (RFC 3339, nanoseconds) when the receipt has it,
// else the day it was filed.
export const receiptTime = (r: Stamped): string => r.at || r.created

// dayOf is the local calendar day of a time, as YYYY-MM-DD. A bare date is its own day.
export function dayOf(t: string): string {
  if (/^\d{4}-\d\d-\d\d$/.test(t)) return t
  const d = new Date(t)
  if (Number.isNaN(d.getTime())) return t.slice(0, 10)
  const two = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${two(d.getMonth() + 1)}-${two(d.getDate())}`
}

// newestFirst sorts by time, newest first. Date.parse drops the nanoseconds, so equal
// milliseconds fall back to comparing the strings, which RFC 3339 in one zone keeps in order.
export function newestFirst<T extends Stamped>(rows: T[]): T[] {
  return [...rows].sort((a, b) => {
    const ta = receiptTime(a)
    const tb = receiptTime(b)
    const d = (Date.parse(tb) || 0) - (Date.parse(ta) || 0)
    return d !== 0 ? d : tb.localeCompare(ta)
  })
}

export type DayGroup<T> = { day: string; runs: { actor: string; request?: string; rows: T[] }[] }

// groupByDay puts receipts, newest first, into days, and inside a day into runs: back-to-back
// writes by one actor for one request (a session) are one run.
export function groupByDay<T extends Stamped>(rows: T[]): DayGroup<T>[] {
  const out: DayGroup<T>[] = []
  for (const r of newestFirst(rows)) {
    const day = dayOf(receiptTime(r))
    let g = out[out.length - 1]
    if (!g || g.day !== day) {
      g = { day, runs: [] }
      out.push(g)
    }
    const req = r.request ?? undefined
    const run = g.runs[g.runs.length - 1]
    if (run && run.actor === r.actor && run.request === req) run.rows.push(r)
    else g.runs.push({ actor: r.actor, request: req, rows: [r] })
  }
  return out
}

// undoCommand is the CLI command that reverts one write.
export const undoCommand = (id: string): string => `para receipt undo ${id}`

export type CheckState = 'unchecked' | 'verified' | 'wrong'

// checkTone is the chip tone of a receipt's check state.
export const checkTone = (c: CheckState): 'quiet' | 'good' | 'bad' =>
  c === 'verified' ? 'good' : c === 'wrong' ? 'bad' : 'quiet'

// isUrl tells a web address in a receipt's sources from a vault id.
export const isUrl = (s: string): boolean => /^https?:\/\//i.test(s)

// emptyToUndefined turns a blank filter field into no filter.
export const emptyToUndefined = (s: string): string | undefined => s.trim() || undefined

// noteRequired: a wrong verdict needs a note that says what is wrong.
export const noteRequired = (verdict: 'verified' | 'wrong', note: string): boolean =>
  verdict === 'wrong' && note.trim() === ''

export type ActorClass = 'owner' | 'agent' | 'para'

// Actors that are the owner: the CLI user, and the owner through the web UI.
const OWNER_ACTORS = new Set(['user', 'owner'])
// Actors that are para itself: scheduled runs, dispatch, upgrades.
const PARA_ACTORS = new Set(['para', 'tick', 'dispatch', 'system', 'upgrade'])

// actorClass sorts a receipt's actor into the owner, an agent, or para (a system write).
export function actorClass(actor: string): ActorClass {
  const a = actor.trim().toLowerCase()
  if (OWNER_ACTORS.has(a)) return 'owner'
  if (PARA_ACTORS.has(a)) return 'para'
  return 'agent'
}

// actorLabel is the chip text: owner, agent <name>, or para.
export function actorLabel(actor: string): string {
  const c = actorClass(actor)
  if (c !== 'agent') return c
  const name = actor.trim().replace(/^agent[:\s-]*/i, '')
  return name ? `agent ${name}` : 'agent'
}

export type OpGroup = 'add' | 'edit' | 'close' | 'send' | 'link' | 'undo' | 'system'

// opGroup groups an op name (note_add, io_send, receipt_undo) by what it did.
export function opGroup(op: string): OpGroup {
  if (/undo|revert/.test(op)) return 'undo'
  if (/^(io_send|io_approve|rfx_nudge|rfx_reply)/.test(op)) return 'send'
  if (/(close|done|drop|cancel|resolve|retire|fail|archive)/.test(op)) return 'close'
  if (/(link|depends|^tag|_tag)/.test(op)) return 'link'
  if (
    /^(index_rebuild|reconcile|triage_run|journal|sweep|recheck|pool_run|policy_refresh|proposal_executed)/.test(
      op,
    )
  )
    return 'system'
  if (/(^add$|_add$|_file$|^ingest$|_draft$|_note$|_reading$|_signal$)/.test(op)) return 'add'
  return 'edit'
}
