import { describe, expect, it } from 'vitest'
import { ioTurnLabel, kindLabel, lastRequestTurn, orderTurns, statusText, threadChips } from './thread'

describe('thread helpers', () => {
  it('labels kinds in the owner words', () => {
    expect(kindLabel('modify')).toBe('Change')
    expect(kindLabel('ask')).toBe('Question')
    expect(kindLabel('redirect')).toBe('Redirect')
    expect(kindLabel(undefined)).toBe('Other')
    expect(kindLabel('zzz')).toBe('Other')
  })
  it('orders oldest first, stable on equal times', () => {
    const t = [
      { at: '2026-11-02T10:00:00Z', n: 'b1' },
      { at: '2026-11-01T10:00:00Z', n: 'a' },
      { at: '2026-11-02T10:00:00Z', n: 'b2' },
    ]
    expect(orderTurns(t).map((x) => x.n)).toEqual(['a', 'b1', 'b2'])
  })
  it('says the status', () => {
    expect(statusText({ kind: 'request', at: '', status: 'queued' })).toBe(
      'Queued: waiting for an agent',
    )
    expect(
      statusText(
        { kind: 'request', at: '', status: 'claimed', claimedBy: 'x', claimedAt: 'T' },
        (s) => `@${s}`,
      ),
    ).toBe('Claimed by x since @T')
    expect(statusText({ kind: 'request', at: '', status: 'done', resolution: 'partial' })).toBe(
      'Partly done',
    )
    expect(statusText({ kind: 'request', at: '', status: 'failed' })).toBe('Failed')
  })
  it('chooses chips', () => {
    expect(threadChips({ queued: 0, waitingNoAgent: false })).toEqual([])
    expect(threadChips({ queued: 2, waitingNoAgent: false })).toHaveLength(1)
    const c = threadChips({ queued: 1, waitingNoAgent: true })
    expect(c.map((x) => x.tone)).toEqual(['quiet', 'attn'])
  })
  it('finds the last owner turn', () => {
    const t = [
      { kind: 'request', at: '2026-11-01T00:00:00Z', n: 1 },
      { kind: 'io', at: '2026-11-03T00:00:00Z', n: 2 },
      { kind: 'request', at: '2026-11-02T00:00:00Z', n: 3 },
    ]
    expect(lastRequestTurn(t)?.n).toBe(3)
    expect(lastRequestTurn([])).toBeUndefined()
  })
})

describe('ioTurnLabel', () => {
  it('shows an outgoing draft by its state, not as Sent', () => {
    expect(ioTurnLabel('out', 'draft')).toBe('Draft')
    expect(ioTurnLabel('out', 'ready')).toBe('Approved')
    expect(ioTurnLabel('out', 'sent')).toBe('Sent')
    expect(ioTurnLabel('in', 'unread')).toBe('Received')
  })
})
