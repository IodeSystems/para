// Meetings: times in the viewer's zone, and the states a target may take.

const two = (n: number) => String(n).padStart(2, '0')
const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']

// viewerZone is the IANA zone of the browser, such as Europe/Berlin.
export function viewerZone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
}

// toRFC3339 turns the value of a datetime-local input (YYYY-MM-DDTHH:mm, the viewer's wall
// clock) into RFC 3339 with the viewer's zone offset on that date, such as 2026-11-04T10:00:00-05:00.
// It returns '' for a value that is not a time.
export function toRFC3339(local: string): string {
  const m = /^(\d{4})-(\d\d)-(\d\d)T(\d\d):(\d\d)(?::(\d\d))?$/.exec(local)
  if (!m) return ''
  const [y, mo, d, h, mi, sec] = [m[1], m[2], m[3], m[4], m[5], m[6] ?? '0'].map(Number) as [
    number,
    number,
    number,
    number,
    number,
    number,
  ]
  const t = new Date(y, mo - 1, d, h, mi, sec)
  if (Number.isNaN(t.getTime())) return ''
  const off = -t.getTimezoneOffset()
  const sign = off < 0 ? '-' : '+'
  const a = Math.abs(off)
  return `${y}-${two(mo)}-${two(d)}T${two(h)}:${two(mi)}:${two(sec)}${sign}${two(Math.floor(a / 60))}:${two(a % 60)}`
}

// toLocalInput is the value of a datetime-local input for an RFC 3339 time, on the viewer's
// wall clock. It returns '' for an empty or unreadable time.
export function toLocalInput(iso: string | null | undefined): string {
  if (!iso) return ''
  const t = new Date(iso)
  if (Number.isNaN(t.getTime())) return ''
  return `${t.getFullYear()}-${two(t.getMonth() + 1)}-${two(t.getDate())}T${two(t.getHours())}:${two(t.getMinutes())}`
}

// meetingWhen says when a meeting is, in the viewer's zone: "Nov 4, 10:00-11:00". Without a
// start it says it is not booked.
export function meetingWhen(
  start: string | null | undefined,
  end: string | null | undefined,
): string {
  const s = start ? new Date(start) : null
  if (!s || Number.isNaN(s.getTime())) return 'not booked'
  const day = (d: Date) => `${MONTHS[d.getMonth()]} ${d.getDate()}`
  const clock = (d: Date) => `${two(d.getHours())}:${two(d.getMinutes())}`
  const head = `${day(s)}, ${clock(s)}`
  const e = end ? new Date(end) : null
  if (!e || Number.isNaN(e.getTime())) return head
  const sameDay =
    e.getFullYear() === s.getFullYear() &&
    e.getMonth() === s.getMonth() &&
    e.getDate() === s.getDate()
  return sameDay ? `${head}-${clock(e)}` : `${head} to ${day(e)}, ${clock(e)}`
}

// targetStates are the states a person may set on an ask target. requested and scheduled
// belong to a meeting target only.
export function targetStates(medium: string): string[] {
  const base = ['drafted', 'sent', 'replied', 'no_reply']
  return medium === 'meeting' ? ['requested', 'scheduled', ...base] : base
}

// questionIds are the questions linked in a meeting body, in order, without repeats.
export function questionIds(body: string): string[] {
  const ids: string[] = []
  for (const m of body.matchAll(/\[\[(question-[^\]|#]+)/g)) {
    const id = m[1]
    if (id && !ids.includes(id)) ids.push(id)
  }
  return ids
}
