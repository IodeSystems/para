import { describe, expect, it } from 'vitest'
import {
  allState,
  bulkBody,
  doneText,
  keepShown,
  mustReason,
  previewText,
  refusals,
  toggle,
  toggleAll,
} from '@/lib/bulk'

describe('bulk selection', () => {
  it('toggles one id in and out', () => {
    const a = toggle(new Set(), 'task-a')
    expect([...a]).toEqual(['task-a'])
    expect([...toggle(a, 'task-a')]).toEqual([])
  })

  it('selects all, then none when all are selected', () => {
    const ids = ['task-a', 'task-b']
    expect([...toggleAll(new Set(['task-a']), ids)]).toEqual(ids)
    expect([...toggleAll(new Set(ids), ids)]).toEqual([])
    expect([...toggleAll(new Set(), [])]).toEqual([])
  })

  it('keeps only what the list still shows', () => {
    expect([...keepShown(new Set(['task-a', 'task-gone']), ['task-a', 'task-b'])]).toEqual([
      'task-a',
    ])
  })

  it('says whether all, some, or none are selected', () => {
    const ids = ['task-a', 'task-b']
    expect(allState(new Set(), ids)).toBe('none')
    expect(allState(new Set(['task-a']), ids)).toBe('some')
    expect(allState(new Set(ids), ids)).toBe('all')
  })
})

describe('bulk input and words', () => {
  it('maps each action to its verb', () => {
    expect(bulkBody('done', ['task-a'], ' ')).toEqual({ verb: 'close', ids: ['task-a'] })
    expect(bulkBody('drop', ['task-a'], 'See the last task.')).toEqual({
      verb: 'close',
      ids: ['task-a'],
      outcome: 'dropped',
      reason: 'See the last task.',
    })
    expect(bulkBody('later', ['task-a'], '', '2026-12-01')).toEqual({
      verb: 'later',
      ids: ['task-a'],
      until: '2026-12-01',
    })
    expect(bulkBody('reopen', ['task-a'], 'Not done.')).toEqual({
      verb: 'reopen',
      ids: ['task-a'],
      reason: 'Not done.',
    })
    expect(mustReason('drop')).toBe(true)
    expect(mustReason('done')).toBe(false)
  })

  it('says what a dry run found and lists refusals', () => {
    const r = {
      applied: 2,
      refused: 1,
      items: [
        { id: 'task-a', title: 'A', state: 'would_apply' },
        { id: 'question-b', title: 'B', state: 'refused', error: 'answer it with question_answer' },
        { id: 'task-c', state: 'would_apply' },
      ],
    }
    expect(previewText('done', r)).toBe('2 items would be marked done; 1 refused.')
    expect(previewText('later', { applied: 1, refused: 0, items: [] })).toBe(
      '1 item would be put off.',
    )
    expect(refusals(r)).toEqual([
      { id: 'question-b', title: 'B', why: 'answer it with question_answer' },
    ])
    expect(doneText('done', r)).toBe('Marked done 2 items; 1 refused')
    expect(doneText('reopen', { applied: 1, refused: 0, items: [] })).toBe('Opened again 1 item')
  })
})
