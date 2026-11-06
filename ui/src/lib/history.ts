// Helpers for the Activity section of an item's page: who did it, what kind of thing it was, when,
// and where its link goes.
import type { Tone } from '@/lib/status'
import { dayOf } from '@/lib/receipts'

type Who = { who: string; actor?: string | null }

// whoLabel names who did it: the owner is "You"; an agent is its name.
export function whoLabel(r: Who): string {
  if (r.who === 'owner') return 'You'
  if (r.who.startsWith('agent:')) return r.who.slice('agent:'.length) || 'agent'
  if (r.who === 'tick') return 'Tick'
  if (r.who === 'hand edit') return 'Hand edit'
  if (r.who === 'person') return r.actor || 'Someone'
  return r.who
}

// whoTone colors the who chip: the owner and people are plain, agents and the tick are quiet, a
// hand edit asks for attention.
export function whoTone(who: string): Tone {
  if (who === 'hand edit') return 'attn'
  if (who.startsWith('agent:') || who === 'tick') return 'wait'
  return 'quiet'
}

const KINDS: Record<string, string> = {
  created: 'Created',
  edited: 'Edited',
  state: 'State',
  moved: 'Moved',
  renamed: 'Renamed',
  note: 'Note',
  asked_agent: 'Asked agent',
  agent_claimed: 'Agent took it',
  agent_result: 'Agent result',
  message_in: 'Message in',
  message_out: 'Message out',
  receipt: 'Agent write',
}

// kindLabel says the kind of a row in words.
export function kindLabel(kind: string): string {
  return KINDS[kind] ?? kind.replace(/_/g, ' ')
}

// rowTime is the time to show: a row whose day only is known shows as its local day.
export function rowTime(r: { at: string; dateOnly?: boolean | null }): string {
  return r.dateOnly ? dayOf(r.at) : r.at
}

// refTarget is where the link of a row goes: an io item opens on its io page, any other ref (a
// receipt, agent work) on the document page. Null when the row has no ref.
export function refTarget(r: {
  kind: string
  ref?: string | null
}): { to: '/io/$id' | '/doc/$id'; id: string } | null {
  if (!r.ref) return null
  const io = r.kind === 'message_in' || r.kind === 'message_out'
  return { to: io ? '/io/$id' : '/doc/$id', id: r.ref }
}

// refLabel is the link text of a row's ref.
export function refLabel(kind: string): string {
  if (kind === 'receipt') return 'receipt'
  if (kind === 'message_in' || kind === 'message_out') return 'message'
  return 'agent work'
}
