import { describe, expect, it } from 'vitest'
import {
  appliedCount,
  canUndo,
  failText,
  nextFocus,
  toastText,
  undoAll,
  undoBodies,
  whyNoUndo,
} from '@/lib/stackToast'

describe('undo calls', () => {
  it('makes one call for the items and one per question, with the queued requests', async () => {
    const bodies = undoBodies(
      [{ proposal: 'proposal-1', item: 'm1' }],
      [{ id: 'question-1', request: 'request-9' }],
      ['request-8'],
    )
    const calls: unknown[] = []
    await undoAll(bodies, (b) => Promise.resolve(calls.push(b)))
    expect(calls).toEqual([
      { items: [{ proposal: 'proposal-1', item: 'm1' }], requests: ['request-8'] },
      { question: 'question-1', requests: ['request-9'] },
    ])
  })
  it('stops at the first failure', async () => {
    const bodies = undoBodies([{ proposal: 'p', item: 'a' }], [{ id: 'q' }])
    let n = 0
    await expect(
      undoAll(bodies, () => {
        n++
        return Promise.reject(new Error('changed since'))
      }),
    ).rejects.toThrow('changed since')
    expect(n).toBe(1)
  })
})

describe('toastText', () => {
  it('says what was done in plain words', () => {
    expect(toastText('decline', ['Pay vendor'])).toBe('Declined: Pay vendor')
    expect(toastText('ask', ['Pay vendor'])).toBe('Sent to agent: Pay vendor')
    expect(toastText('approve', ['Pay vendor'], 2)).toBe('Approved: Pay vendor — 2 changes applied')
    expect(toastText('approve', ['A'], 1)).toBe('Approved: A — 1 change applied')
    expect(toastText('skip', ['A', 'B', 'C'])).toBe('Skipped 3 items')
  })
  it('says a failure and what came back', () => {
    expect(failText('ask', ['A'], 'refused')).toBe('Could not send to agent A: refused')
  })
})

describe('undo rules', () => {
  it('offers Undo only when every decision can be taken back', () => {
    expect(canUndo([{ undo: 'reset' }, { undo: 'receipts' }])).toBe(true)
    expect(canUndo([{ undo: 'reset' }, { undo: '' }])).toBe(false)
    expect(canUndo([{}])).toBe(false)
    expect(canUndo([])).toBe(false)
  })
  it('counts the vault changes that ran', () => {
    expect(appliedCount([{ undo: 'receipts' }, { undo: 'reset' }, { undo: 'receipts' }])).toBe(2)
  })
  it('says why a toast has no Undo', () => {
    expect(whyNoUndo('answer')).toMatch(/cannot be undone/)
  })
})

describe('nextFocus', () => {
  const order = ['a', 'b', 'c']
  it('takes the next card', () => expect(nextFocus(order, new Set(['a']))).toBe('b'))
  it('takes the previous card when the last leaves', () =>
    expect(nextFocus(order, new Set(['c']))).toBe('b'))
  it('skips cards that leave together', () =>
    expect(nextFocus(order, new Set(['b', 'c']))).toBe('a'))
  it('gives null when none is left', () => expect(nextFocus(order, new Set(order))).toBeNull())
})
