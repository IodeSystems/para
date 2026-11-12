// Pure helpers of the Thread panel: turn order, the words of a status line, and the chips on top.

export type PostKind = 'modify' | 'ask' | 'redirect' | 'other'

export const POST_KINDS: { kind: PostKind; label: string }[] = [
  { kind: 'modify', label: 'Change' },
  { kind: 'ask', label: 'Question' },
  { kind: 'redirect', label: 'Redirect' },
  { kind: 'other', label: 'Other' },
]

// ioTurnLabel is the chip word of an io turn: an incoming message is Received; an outgoing one
// shows its real state (Draft, Approved, Sent), never Sent before it went out.
export function ioTurnLabel(direction: string | null | undefined, state: string | null | undefined): string {
  if (direction !== 'out') return 'Received'
  switch (state) {
    case 'draft':
      return 'Draft'
    case 'ready':
      return 'Approved'
    case 'dropped':
      return 'Dropped'
    case 'superseded':
      return 'Replaced'
    default:
      return 'Sent'
  }
}

// kindLabel is the owner's word for a turn kind; an unknown kind reads as Other.
export function kindLabel(kind: string | null | undefined): string {
  return POST_KINDS.find((k) => k.kind === kind)?.label ?? 'Other'
}

export type TurnLike = {
  kind: string
  at: string
  status?: string | null
  resolution?: string | null
  claimedBy?: string | null
  claimedAt?: string | null
}

// orderTurns puts the oldest turn first. Equal or unparsable times keep their order.
export function orderTurns<T extends { at: string }>(turns: readonly T[]): T[] {
  const t = (s: string) => {
    const n = Date.parse(s)
    return Number.isNaN(n) ? 0 : n
  }
  return turns
    .map((x, i) => ({ x, i }))
    .sort((a, b) => t(a.x.at) - t(b.x.at) || a.i - b.i)
    .map((e) => e.x)
}

const RESOLUTION: Record<string, string> = {
  done: 'Done',
  partial: 'Partly done',
  blocked: 'Blocked',
  stuck: 'Stuck',
  needs_clarification: 'The agent needs a clarification',
  cancelled: 'Cancelled',
  failed: 'Failed',
}

// statusText is the status line of a request turn. when formats a time for a person.
export function statusText(t: TurnLike, when: (s: string) => string = (s) => s): string {
  switch (t.status) {
    case 'queued':
      return 'Queued: waiting for an agent'
    case 'claimed': {
      const by = t.claimedBy ? ` by ${t.claimedBy}` : ''
      return `Claimed${by}${t.claimedAt ? ` since ${when(t.claimedAt)}` : ''}`
    }
    case 'done':
      return (t.resolution && RESOLUTION[t.resolution]) || 'Done'
    case 'failed':
      return 'Failed'
    case 'cancelled':
      return 'Cancelled'
    case 'proposed':
      return 'Proposed: waiting for you'
    default:
      return t.status ?? ''
  }
}

export type Chip = { tone: 'attn' | 'wait' | 'quiet'; label: string }

// threadChips are the lasting chips at the top of a thread.
export function threadChips(t: { queued: number; waitingNoAgent: boolean }): Chip[] {
  const out: Chip[] = []
  if (t.queued > 0) out.push({ tone: 'quiet', label: `Queued: waiting for an agent (${t.queued})` })
  if (t.waitingNoAgent)
    out.push({
      tone: 'attn',
      label: 'No agent is connected on this machine: start /para loop',
    })
  return out
}

// lastRequestTurn is the newest owner turn: what a card shows to say why an item came back.
export function lastRequestTurn<T extends TurnLike>(turns: readonly T[]): T | undefined {
  return orderTurns(turns.filter((x) => x.kind === 'request')).at(-1)
}
