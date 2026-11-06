import type { Tone } from '@/lib/status'
import { humanTime } from '@/lib/when'

// AskedWork is the part of a piece of agent work these words read.
export type AskedWork = {
  status: string
  claimedBy?: string | null
  claimedAt?: string | null
  resolution?: string | null
  stale?: boolean | null
  needsYou?: boolean | null
}

// sentStatus says where a piece of agent work the owner asked for stands, for a person: queued,
// claimed by an agent since a time, or how it ended (done, partial, stuck, blocked, needs
// clarification). now is for the age of a claim.
export function sentStatus(it: AskedWork, now = Date.now()): { label: string; tone: Tone } {
  const res = it.resolution ? it.resolution.replace(/_/g, ' ') : ''
  switch (it.status) {
    case 'proposed':
      return { label: 'waits for your approval to run', tone: 'attn' }
    case 'queued':
      return { label: 'queued', tone: 'wait' }
    case 'claimed': {
      const who = it.claimedBy || 'an agent'
      const since = it.claimedAt ? ` since ${humanTime(it.claimedAt, now)}` : ''
      return { label: `${who} has it${since}`, tone: 'wait' }
    }
    case 'done':
      if (!res || res === 'done') return { label: 'done', tone: 'good' }
      if (needsOwner(it.resolution)) return { label: `${res}: waits for you`, tone: 'attn' }
      return { label: res, tone: 'wait' }
    case 'failed':
      return { label: res && res !== 'failed' ? `failed: ${res}` : 'failed', tone: 'bad' }
    case 'cancelled':
      return { label: 'taken back', tone: 'quiet' }
  }
  return { label: it.status.replace(/_/g, ' '), tone: 'quiet' }
}

// needsOwner is true for a turn that ended waiting for the owner's response.
export function needsOwner(resolution?: string | null): boolean {
  return resolution === 'stuck' || resolution === 'blocked' || resolution === 'needs_clarification'
}

// staleWords is the flag of work queued or claimed with no change in a day, or '' when it is not
// stale. The server decides stale; this says it.
export function staleWords(it: AskedWork): string {
  if (!it.stale) return ''
  if (it.status === 'queued') return 'stale: no agent took it in a day'
  if (it.status === 'claimed') return 'stale: no update in a day'
  return ''
}

// openLine is Today's line for the work the owner asked of agents, or '' when none is open.
export function openLine(open: number, stale: number): string {
  if (open <= 0) return ''
  return `${open} asked of agents open${stale > 0 ? ` (${stale} stale)` : ''}`
}

// KIND_WORDS names where each kind of agent work came from.
const KIND_WORDS: Record<string, string> = {
  'owner-ask': 'asked in the chat',
  'sent-to-agent': 'sent from the web',
  'meeting-notes': 'meeting notes to file',
  investigate: 'investigate',
  delegate: 'task given to an agent',
  'task-update': 'update to sort',
  sweep: 'sweep of a source',
  'from-agent': 'an agent asked',
  'journal-summary': 'journal summary',
  'recurring-task': 'recurring task',
  'fix-write': 'fix a write marked wrong',
}

// kindWords names where an ask came from.
export function kindWords(kind?: string | null): string {
  return (kind && KIND_WORDS[kind]) || kind || 'agent work'
}

// resultToCheck is finished work (done, partial, or failed) whose result waits for the owner to
// open it: the server says it needs them, and no response is due.
export function resultToCheck(it: AskedWork): boolean {
  if (!it.needsYou) return false
  return it.status === 'failed' || (it.status === 'done' && !needsOwner(it.resolution))
}

// STATUS_GROUPS are the groups of the Asked of agents view, in order, with their titles.
export const STATUS_GROUPS: { status: string; title: string }[] = [
  { status: 'proposed', title: 'Waits for your approval to run' },
  { status: 'queued', title: 'Queued' },
  { status: 'claimed', title: 'An agent has it' },
  { status: 'done', title: 'Done' },
  { status: 'failed', title: 'Failed' },
  { status: 'cancelled', title: 'Taken back' },
]

// byStatus groups rows by status in STATUS_GROUPS order, newest queued first in each group. A
// group with no rows is left out.
export function byStatus<T extends { status: string; queuedAt: string }>(
  rows: T[],
): { status: string; title: string; rows: T[] }[] {
  const newest = [...rows].sort((a, b) =>
    a.queuedAt < b.queuedAt ? 1 : a.queuedAt > b.queuedAt ? -1 : 0,
  )
  return STATUS_GROUPS.map((g) => ({
    ...g,
    rows: newest.filter((r) => r.status === g.status),
  })).filter((g) => g.rows.length > 0)
}
