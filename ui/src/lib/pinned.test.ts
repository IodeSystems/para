import { describe, expect, it } from 'vitest'
import {
  isPinned,
  lastWords,
  lotsOfPlates,
  nextWords,
  PLATES,
  tagOf,
  threadWords,
  waitingWords,
} from '@/lib/pinned'

const card = (id: string, stale?: string) => ({ id, title: `T ${id}`, stale })

describe('isPinned', () => {
  const cards = [
    { id: 'task-a', key: null },
    { id: 'thread-x', key: '#launch' },
  ]
  it('finds an item by id and a thread by key', () => {
    expect(isPinned(cards, { id: 'task-a' })).toBe(true)
    expect(isPinned(cards, { key: '#launch' })).toBe(true)
    expect(isPinned(cards, { id: 'task-b' })).toBe(false)
    expect(isPinned(cards, { key: '#other' })).toBe(false)
    expect(isPinned(cards, {})).toBe(false)
  })
})

describe('lotsOfPlates', () => {
  it('says nothing up to the limit', () => {
    const cards = Array.from({ length: PLATES }, (_, i) => card(`a${i}`, 'closed'))
    expect(lotsOfPlates(cards)).toBeNull()
  })
  it('past the limit suggests the closed or quiet ones', () => {
    const cards = Array.from({ length: PLATES + 1 }, (_, i) =>
      card(`a${i}`, i === 2 ? 'closed' : i === 5 ? 'quiet for 30 days' : undefined),
    )
    const h = lotsOfPlates(cards)
    expect(h?.count).toBe(PLATES + 1)
    expect(h?.suggest.map((s) => s.id)).toEqual(['a2', 'a5'])
    expect(h?.suggest[0]?.why).toBe('closed')
  })
})

describe('card lines', () => {
  it('names the next date by kind', () => {
    expect(nextWords({ nextDate: '2026-12-01', nextKind: 'due' })).toBe('due 2026-12-01')
    expect(nextWords({ nextDate: '2026-12-01', nextKind: 'scheduled' })).toBe('on 2026-12-01')
    expect(nextWords({ nextDate: '2026-12-01', nextKind: 'nudge' })).toBe('nudge 2026-12-01')
    expect(nextWords({ nextDate: '2026-12-01', nextKind: 'x' })).toBe('2026-12-01')
    expect(nextWords({})).toBe('')
  })
  it('says who it waits on, by name when known, and the agent turns', () => {
    expect(
      waitingWords({
        waitingOn: ['person-a', 'task-b'],
        waitingNames: ['Ada', null],
        queued: 1,
        claimed: 2,
      }),
    ).toBe('waits on Ada, task-b · 2 with an agent · 1 queued for an agent')
    expect(waitingWords({ waitingOn: [], queued: 0, claimed: 0 })).toBe('')
  })
  it('says the last change with its day', () => {
    expect(
      lastWords({
        last: {
          at: '2026-11-09T10:00:00Z',
          who: 'agent:claude',
          kind: 'edited',
          summary: 'edited',
        },
      }),
    ).toBe('agent:claude edited, 2026-11-09')
    expect(lastWords({})).toBe('')
  })
  it('counts open turns', () => {
    expect(threadWords({ turns: 0 })).toBe('')
    expect(threadWords({ turns: 1 })).toBe('1 open turn')
    expect(threadWords({ turns: 3 })).toBe('3 open turns')
  })
  it('reads the tag of a tag key only', () => {
    expect(tagOf('#launch')).toBe('launch')
    expect(tagOf('channel:src/t1')).toBeNull()
    expect(tagOf(null)).toBeNull()
  })
})
