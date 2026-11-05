// Meetings: times in the viewer's zone.

const two = (n: number) => String(n).padStart(2, '0')
const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']

// viewerZone is the IANA zone of the browser, such as Europe/Berlin.
export function viewerZone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
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

// attendSaid is the toast after the owner answers whether they go to a meeting.
export function attendSaid(attend: string, title: string): string {
  switch (attend) {
    case 'yes':
      return `Attending: ${title}`
    case 'no':
      return `Skipped: ${title}`
    case 'maybe':
      return `Maybe: ${title}`
  }
  return `No answer yet: ${title}`
}
