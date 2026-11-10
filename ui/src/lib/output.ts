import type { Tone } from '@/lib/status'
import { toneOf, plainState } from '@/lib/status'

// What a sent message became: chips for the reply, the Ask it made, and the last attempt when it
// did not go through cleanly.

export type SentResult = {
  reply?: { id: string } | null
  ask?: { id: string; type?: string | null; status?: string | null } | null
  lastAttempt?: { result: string; error?: string | null } | null
}

export type ResultChip = {
  key: 'reply' | 'ask' | 'attempt'
  label: string
  tone: Tone
  // io links to an io item's page (the reply).
  io?: string
  // title is the error text of a failed or held attempt.
  title?: string
}

export function resultChips(r: SentResult): ResultChip[] {
  const out: ResultChip[] = []
  if (r.reply) out.push({ key: 'reply', label: 'Replied', tone: 'good', io: r.reply.id })
  if (r.ask) {
    const s = r.ask.status || r.ask.type || 'ask'
    out.push({ key: 'ask', label: `Ask: ${plainState(s)}`, tone: toneOf(r.ask.status ?? '') })
  }
  const a = r.lastAttempt
  if (a && (a.result === 'failed' || a.result === 'held')) {
    out.push({
      key: 'attempt',
      label: a.error ? `${a.result === 'failed' ? 'Failed' : 'Held'}: ${a.error}` : a.result === 'failed' ? 'Failed' : 'Held',
      tone: a.result === 'failed' ? 'bad' : 'attn',
      title: a.error ?? undefined,
    })
  }
  return out
}

// sinceFrom turns a "last N days" choice into the day io_sent takes; '' is no limit.
export function sinceFrom(days: number, now = new Date()): string | undefined {
  if (!days) return undefined
  return new Date(now.getTime() - days * 86_400_000).toISOString().slice(0, 10)
}

// variantWords: "variation 2 of 3", or nothing for a message that was not one of a set.
export function variantWords(variant?: number | null, of?: number | null): string {
  return variant && of ? `variation ${variant} of ${of}` : ''
}
