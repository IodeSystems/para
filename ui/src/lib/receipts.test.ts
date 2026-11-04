import { describe, expect, it } from 'vitest'
import { dayOf, groupByDay, newestFirst, receiptTime, undoCommand } from '@/lib/receipts'

const r = (at: string, actor = 'agent-a', request?: string) => ({
  at,
  created: at.slice(0, 10),
  actor,
  request,
})

describe('receipts', () => {
  it('uses at, else created', () => {
    expect(
      receiptTime({ at: '2026-11-03T10:00:00.123456789Z', created: '2026-11-03', actor: 'a' }),
    ).toBe('2026-11-03T10:00:00.123456789Z')
    expect(receiptTime({ at: null, created: '2026-11-02', actor: 'a' })).toBe('2026-11-02')
  })
  it('sorts newest first, nanoseconds included', () => {
    const rows = [
      r('2026-11-03T10:00:00.000000001Z'),
      r('2026-11-03T10:00:00.000000002Z'),
      r('2026-11-03T11:00:00Z'),
    ]
    expect(newestFirst(rows).map((x) => x.at)).toEqual([
      '2026-11-03T11:00:00Z',
      '2026-11-03T10:00:00.000000002Z',
      '2026-11-03T10:00:00.000000001Z',
    ])
  })
  it('groups by day, then by actor and request runs', () => {
    const rows = [
      r('2026-11-03T12:00:00', 'agent-a', 'req-1'),
      r('2026-11-03T11:00:00', 'agent-a', 'req-1'),
      r('2026-11-03T10:00:00', 'agent-b'),
      r('2026-11-02T10:00:00', 'agent-a'),
    ]
    const g = groupByDay(rows)
    expect(g.map((d) => d.day)).toEqual(['2026-11-03', '2026-11-02'])
    expect(g[0]?.runs.map((x) => [x.actor, x.rows.length])).toEqual([
      ['agent-a', 2],
      ['agent-b', 1],
    ])
  })
  it('keeps a bare date as its day', () => {
    expect(dayOf('2026-11-02')).toBe('2026-11-02')
  })
  it('says the undo command', () => {
    expect(undoCommand('rcpt-1')).toBe('para receipt undo rcpt-1')
  })
})
