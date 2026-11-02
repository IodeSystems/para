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

const VERB: Record<string, string> = {
  add: 'Added',
  create: 'Created',
  update: 'Changed',
  edit: 'Edited',
  done: 'Finished',
  drop: 'Dropped',
  start: 'Started',
  answer: 'Answered',
  assign: 'Assigned',
  block: 'Blocked',
  link: 'Linked',
  draft: 'Drafted',
  reply: 'Recorded a reply to',
  status: 'Changed the status of',
  note: 'Noted on',
  approve: 'Approved',
  deny: 'Denied',
  resolve: 'Resolved',
  accept: 'Accepted',
  reject: 'Rejected',
}

// The words a person reads for a thing, where the op name is older than the word.
const NOUN: Record<string, string> = {
  project: 'goal',
  rfx: 'ask',
  request: 'agent work',
  nudge: 'follow up',
}

// opPhrase says an event's op in words: task_add is "Added task", external_edit is "Edited outside para".
export function opPhrase(op: string): string {
  if (op === 'external_edit') return 'Edited outside para'
  const parts = op.split('_').filter(Boolean)
  const verb = parts[parts.length - 1] ?? ''
  const noun = parts
    .slice(0, -1)
    .map((p) => NOUN[p] ?? p)
    .join(' ')
  const v = VERB[verb]
  if (!v) return op.replace(/_/g, ' ')
  return noun ? `${v} ${noun}` : v
}

// needWords says what a Review "needs you" kind asks of the person.
export function needWords(kind: string): string {
  switch (kind) {
    case 'draft RFX':
      return 'Message ready to send'
    case 'unverified summary':
      return 'Summary to verify'
    case 'proposed request':
      return 'Agent work proposed'
    default:
      return kind
  }
}
