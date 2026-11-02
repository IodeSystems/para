// transition is the from-to part of an event: "open to done", or '' when the event has none.
export function transition(e: { from: string; to: string }): string {
  return e.from || e.to ? `${e.from || '-'} to ${e.to || '-'}` : ''
}

// newestTS is the latest timestamp of a list of events, or ''.
export function newestTS(events: { ts: string }[]): string {
  let best = ''
  let bestMs = -Infinity
  for (const e of events) {
    const ms = Date.parse(e.ts)
    if (ms > bestMs) {
      bestMs = ms
      best = e.ts
    }
  }
  return best
}

// ago is a short relative time: "5 min ago", "3 h ago", "2 d ago".
export function ago(iso: string, now = Date.now()): string {
  const ms = Date.parse(iso)
  if (Number.isNaN(ms)) return ''
  const s = Math.max(0, Math.round((now - ms) / 1000))
  if (s < 60) return 'just now'
  if (s < 3600) return `${Math.floor(s / 60)} min ago`
  if (s < 86400) return `${Math.floor(s / 3600)} h ago`
  return `${Math.floor(s / 86400)} d ago`
}
