import type { Tone } from '@/lib/status'
import { humanTime } from '@/lib/when'

// AskedWork is the part of a piece of agent work these words read.
export type AskedWork = {
  status: string
  claimedBy?: string | null
  claimedAt?: string | null
  resolution?: string | null
  stale?: boolean | null
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

// kindWords names where an ask came from.
export function kindWords(kind?: string | null): string {
  return kind === 'owner-ask' ? 'asked in the chat' : 'sent from the web'
}

// waiting lists the agent work still in the queue or held by an agent, oldest first: what the
// loop has not finished yet.
export function waiting<T extends { status: string; queuedAt: string }>(items: T[]): T[] {
  return items
    .filter((r) => r.status === 'queued' || r.status === 'claimed')
    .sort((a, b) => (a.queuedAt < b.queuedAt ? -1 : a.queuedAt > b.queuedAt ? 1 : 0))
}
