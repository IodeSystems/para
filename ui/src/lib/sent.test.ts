import { describe, expect, it } from 'vitest'
import {
  byStatus,
  kindWords,
  needsOwner,
  openLine,
  resultToCheck,
  sentStatus,
  staleWords,
} from '@/lib/sent'

describe('asked of agents status', () => {
  it('says where the work stands', () => {
    expect(sentStatus({ status: 'queued' }).label).toBe('queued')
    expect(sentStatus({ status: 'claimed', claimedBy: 'claude' }).label).toBe('claude has it')
    expect(sentStatus({ status: 'done', resolution: 'done' })).toEqual({
      label: 'done',
      tone: 'good',
    })
    expect(sentStatus({ status: 'done', resolution: 'needs_clarification' })).toEqual({
      label: 'needs clarification: waits for you',
      tone: 'attn',
    })
    expect(sentStatus({ status: 'done', resolution: 'blocked' }).tone).toBe('attn')
    expect(sentStatus({ status: 'done', resolution: 'partial' })).toEqual({
      label: 'partial',
      tone: 'wait',
    })
    expect(sentStatus({ status: 'failed', resolution: 'failed' }).label).toBe('failed')
    expect(sentStatus({ status: 'cancelled' }).label).toBe('taken back')
  })

  it('says since when an agent has it', () => {
    const now = Date.parse('2026-11-05T12:00:00Z')
    expect(
      sentStatus({ status: 'claimed', claimedBy: 'claude', claimedAt: '2026-11-05T09:00:00Z' }, now)
        .label,
    ).toBe('claude has it since 3 hours ago')
  })

  it('flags stale work only while it is queued or claimed', () => {
    expect(staleWords({ status: 'queued', stale: true })).toBe('stale: no agent took it in a day')
    expect(staleWords({ status: 'claimed', stale: true })).toBe('stale: no update in a day')
    expect(staleWords({ status: 'claimed', stale: false })).toBe('')
    expect(staleWords({ status: 'done', stale: true })).toBe('')
  })

  it('knows what waits for the owner', () => {
    expect(['stuck', 'blocked', 'needs_clarification'].every((r) => needsOwner(r))).toBe(true)
    expect(needsOwner('partial')).toBe(false)
    expect(needsOwner(null)).toBe(false)
  })

  it('counts open work for Today', () => {
    expect(openLine(0, 0)).toBe('')
    expect(openLine(3, 0)).toBe('3 asked of agents open')
    expect(openLine(3, 1)).toBe('3 asked of agents open (1 stale)')
  })

  it('groups by status, newest first in a group', () => {
    const g = byStatus([
      { id: 'b', status: 'queued', queuedAt: '2026-11-05T10:00:00Z' },
      { id: 'd', status: 'done', queuedAt: '2026-11-01T10:00:00Z' },
      { id: 'c', status: 'queued', queuedAt: '2026-11-06T10:00:00Z' },
      { id: 'a', status: 'claimed', queuedAt: '2026-11-04T10:00:00Z' },
    ])
    expect(g.map((x) => x.status)).toEqual(['queued', 'claimed', 'done'])
    expect(g[0].rows.map((r) => r.id)).toEqual(['c', 'b'])
  })

  it('names each kind', () => {
    expect(kindWords('owner-ask')).toBe('asked in the chat')
    expect(kindWords('sent-to-agent')).toBe('sent from the web')
    expect(kindWords('delegate')).toBe('task given to an agent')
    expect(kindWords('custom')).toBe('custom')
    expect(kindWords('')).toBe('agent work')
  })

  it('knows a result to check', () => {
    expect(resultToCheck({ status: 'done', resolution: 'done', needsYou: true })).toBe(true)
    expect(resultToCheck({ status: 'failed', needsYou: true })).toBe(true)
    expect(resultToCheck({ status: 'done', resolution: 'done', needsYou: false })).toBe(false)
    expect(resultToCheck({ status: 'done', resolution: 'stuck', needsYou: true })).toBe(false)
    expect(resultToCheck({ status: 'queued', needsYou: true })).toBe(false)
  })
})
