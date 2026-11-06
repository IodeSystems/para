// Times are shown local and in words: "2 hours ago" for the last day, "Nov 2, 07:09" after it.
// The exact time goes in a tooltip. A bare date (YYYY-MM-DD) has no zone and shows as "Nov 2".

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']

// ISO_TS finds an RFC 3339 timestamp inside running text.
export const ISO_TS = /\d{4}-\d\d-\d\dT\d\d:\d\d(?::\d\d(?:\.\d+)?)?(?:Z|[+-]\d\d:?\d\d)/g
const DATE_ONLY = /^\d{4}-\d\d-\d\d$/

const two = (n: number) => String(n).padStart(2, '0')

// exactTime is the full local time, for a tooltip.
export function exactTime(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return `${d.getFullYear()}-${two(d.getMonth() + 1)}-${two(d.getDate())} ${two(d.getHours())}:${two(d.getMinutes())}:${two(d.getSeconds())}`
}

// dateWords says a bare date as "Nov 3", with the year when it is not the year of `today`.
export function dateWords(date: string, today = new Date().toISOString().slice(0, 10)): string {
  if (!DATE_ONLY.test(date)) return date
  const [y, m, d] = date.split('-').map(Number)
  return `${MONTHS[(m ?? 1) - 1]} ${d}${String(y) === today.slice(0, 4) ? '' : `, ${y}`}`
}

// humanTime says a timestamp for a person. It returns the input when it is not a time.
export function humanTime(iso: string, now = Date.now()): string {
  if (DATE_ONLY.test(iso)) {
    const [y, m, d] = iso.split('-').map(Number)
    const sameYear = y === new Date(now).getFullYear()
    return `${MONTHS[(m ?? 1) - 1]} ${d}${sameYear ? '' : `, ${y}`}`
  }
  const t = new Date(iso)
  if (Number.isNaN(t.getTime())) return iso
  const s = Math.round((now - t.getTime()) / 1000)
  if (s >= 0 && s < 60) return 'just now'
  if (s >= 60 && s < 3600) {
    const n = Math.floor(s / 60)
    return `${n} ${n === 1 ? 'minute' : 'minutes'} ago`
  }
  if (s >= 3600 && s < 86400) {
    const n = Math.floor(s / 3600)
    return `${n} ${n === 1 ? 'hour' : 'hours'} ago`
  }
  const year = t.getFullYear() === new Date(now).getFullYear() ? '' : `, ${t.getFullYear()}`
  return `${MONTHS[t.getMonth()]} ${t.getDate()}${year}, ${two(t.getHours())}:${two(t.getMinutes())}`
}

// splitTimes cuts text into plain pieces and the timestamps inside it.
export function splitTimes(text: string): { text: string; time: boolean }[] {
  const out: { text: string; time: boolean }[] = []
  let at = 0
  for (const m of text.matchAll(ISO_TS)) {
    if (m.index > at) out.push({ text: text.slice(at, m.index), time: false })
    out.push({ text: m[0], time: true })
    at = m.index + m[0].length
  }
  if (at < text.length) out.push({ text: text.slice(at), time: false })
  return out
}

// daysAgo says a bare date relative to today: "today", "yesterday", "5 days ago" within two weeks,
// then the date ("Oct 3"). A date after today, or one that is not a date, shows as the date.
export function daysAgo(date: string, today: string): string {
  if (!DATE_ONLY.test(date) || !DATE_ONLY.test(today)) return date
  const n = Math.round(
    (Date.parse(`${today}T00:00:00Z`) - Date.parse(`${date}T00:00:00Z`)) / 86400000,
  )
  if (n === 0) return 'today'
  if (n === 1) return 'yesterday'
  if (n > 1 && n < 14) return `${n} days ago`
  return dateWords(date, today)
}
