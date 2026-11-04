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
