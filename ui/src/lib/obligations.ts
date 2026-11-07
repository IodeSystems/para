// Obligations: what the owner promised people. The page filters them (Meetings, Promises,
// Recurring, All), shows the coming days as a timeline with each point's open prep, and counts how
// the promises of the last 30 days went. A change (Push out, Redirect, Cancel) takes a reason and
// tells the people it concerns unless the owner unticks "Tell them".

export type ObligationFilter = 'all' | 'meetings' | 'promises' | 'recurring'

export const FILTERS: { value: ObligationFilter; label: string }[] = [
  { value: 'all', label: 'All' },
  { value: 'meetings', label: 'Meetings' },
  { value: 'promises', label: 'Promises' },
  { value: 'recurring', label: 'Recurring' },
]

// parseFilter reads the filter of the page's address; anything else is all.
export function parseFilter(v: unknown): ObligationFilter | undefined {
  return v === 'meetings' || v === 'promises' || v === 'recurring' ? v : undefined
}

export type ObligationDetail = {
  kind: string
  schedule: string
  recur?: string | null
  agent?: boolean | null
}

// matchesFilter says whether an obligation is in a filter. Meetings are kind meeting; Recurring
// is a series, an ongoing one, or one an agent produces; Promises are the rest: one-off things
// owed to people.
export function matchesFilter(
  o: ObligationDetail | null | undefined,
  f: ObligationFilter,
): boolean {
  if (!o) return f === 'all'
  const recurring = o.schedule === 'series' || o.schedule === 'ongoing' || !!o.agent
  switch (f) {
    case 'meetings':
      return o.kind === 'meeting'
    case 'recurring':
      return recurring
    case 'promises':
      return o.kind !== 'meeting' && !recurring
  }
  return true
}

export type ChangeAction = 'push' | 'redirect' | 'cancel'

export const ACTION_WORDS: Record<ChangeAction, string> = {
  push: 'Push out',
  redirect: 'Redirect',
  cancel: 'Cancel',
}

export type ChangeForm = {
  action: ChangeAction
  meeting: boolean
  due: string // YYYY-MM-DD
  start: string // datetime-local, YYYY-MM-DDTHH:MM
  end: string
  to: string
  outcome: '' | 'declined' | 'dropped'
  reason: string
  because: string
  notify: boolean
}

export type ChangeBody = {
  id: string
  action: ChangeAction
  due?: string
  start?: string
  end?: string
  to?: string
  outcome?: string
  reason?: string
  because?: string
  notify: boolean
}

// localToRFC3339 turns a datetime-local value into RFC 3339 with the zone offset of the browser.
export function localToRFC3339(v: string, offsetMin = new Date(v).getTimezoneOffset()): string {
  if (!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}$/.test(v)) return ''
  const off = -offsetMin
  const sign = off >= 0 ? '+' : '-'
  const abs = Math.abs(off)
  const hh = String(Math.floor(abs / 60)).padStart(2, '0')
  const mm = String(abs % 60).padStart(2, '0')
  return `${v}:00${sign}${hh}:${mm}`
}

// changeBody checks the dialog's fields and makes the input of obligation_change, or says what is
// missing. A reason is required: the facts on the ground, or the higher-priority item.
export function changeBody(
  id: string,
  f: ChangeForm,
  offset?: (v: string) => number,
): { body?: ChangeBody; error?: string } {
  const reason = f.reason.trim()
  const because = f.because.trim()
  if (!reason && !because)
    return { error: 'Say why: the facts on the ground, or the item with a higher priority.' }
  const body: ChangeBody = { id, action: f.action, notify: f.notify }
  if (reason) body.reason = reason
  if (because) body.because = because
  switch (f.action) {
    case 'push':
      if (f.meeting) {
        const start = localToRFC3339(f.start, offset?.(f.start))
        if (!start) return { error: 'Give the new start.' }
        body.start = start
        if (f.end) {
          const end = localToRFC3339(f.end, offset?.(f.end))
          if (!end) return { error: 'The end is not a time.' }
          body.end = end
        }
      } else {
        if (!/^\d{4}-\d{2}-\d{2}$/.test(f.due)) return { error: 'Give the new date.' }
        body.due = f.due
      }
      break
    case 'redirect':
      if (!f.to.trim())
        return { error: 'Say who has it now (a person id or agent:<name>), or where it went.' }
      body.to = f.to.trim()
      break
    case 'cancel':
      if (f.outcome) body.outcome = f.outcome
      break
  }
  return { body }
}

export type Party = { id?: string | null; name: string; medium: string }

// tellWords lists who a change tells, by medium: "Ada Example (email); Bo Example (slack)".
export function tellWords(parties: readonly Party[]): string {
  const by = new Map<string, string[]>()
  for (const p of parties) by.set(p.medium, [...(by.get(p.medium) ?? []), p.name])
  return [...by.entries()].map(([m, names]) => `${names.join(', ')} (${m})`).join('; ')
}

export type Score = { days: number; kept: number; renegotiated: number; broken: number }

// scoreWords says the 30-day numbers in one line.
export function scoreWords(s: Score): string {
  return `Last ${s.days} days: ${s.kept} kept, ${s.renegotiated} renegotiated, ${s.broken} broken`
}

// hoursWords says hours as "9h" or "0.25h".
export function hoursWords(h: number): string {
  return `${Math.round(h * 100) / 100}h`
}

// pointWhen says when a point falls: the start time of a meeting, else "due".
export function pointWhen(p: { start?: string | null; date: string }): string {
  if (p.start) {
    const t = new Date(p.start)
    if (!Number.isNaN(t.getTime()))
      return t.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' })
  }
  return 'due'
}

// promiseWords names how a promise stands.
export function promiseWords(p?: string | null): string {
  switch (p) {
    case 'kept':
      return 'Kept'
    case 'renegotiated':
      return 'Renegotiated'
    case 'broken':
      return 'Broken'
  }
  return ''
}
