import type { Tone } from '@/lib/status'

// sentStatus says where a piece of agent work the owner sent stands, for a person: queued, claimed
// by an agent, or how it ended (done, partial, stuck, blocked, needs clarification).
export function sentStatus(it: {
  status: string
  claimedBy?: string | null
  resolution?: string | null
}): { label: string; tone: Tone } {
  const res = it.resolution ? it.resolution.replace(/_/g, ' ') : ''
  switch (it.status) {
    case 'proposed':
      return { label: 'waits for your approval to run', tone: 'attn' }
    case 'queued':
      return { label: 'queued', tone: 'wait' }
    case 'claimed':
      return { label: `${it.claimedBy || 'an agent'} has it`, tone: 'wait' }
    case 'done':
      if (!res || res === 'done') return { label: 'done', tone: 'good' }
      return { label: res, tone: res === 'partial' ? 'wait' : 'attn' }
    case 'failed':
      return { label: res ? `failed: ${res}` : 'failed', tone: 'bad' }
    case 'cancelled':
      return { label: 'taken back', tone: 'quiet' }
  }
  return { label: it.status.replace(/_/g, ' '), tone: 'quiet' }
}
