import { describe, expect, it } from 'vitest'
import { partLabel, partLink, partShown, type PartRef } from '@/lib/partof'
import {
  confirmText,
  left,
  mark,
  markRest,
  reviewDoneText,
  reviewsOnClose,
  tally,
  themeCloseBody,
} from '@/lib/theme'

const parts: PartRef[] = [
  { kind: 'parent', via: 'parent', id: 'task-p', title: 'Plan the trip' },
  { kind: 'obligation', via: 'depends_on', id: 'obligation-t', title: 'Trip' },
  { kind: 'goal', via: 'goal', id: 'goal-g', title: 'Portal' },
  { kind: 'tag', via: 'tag', id: 'summer', title: 'summer' },
]

describe('part of', () => {
  it('shows the nearest one and +N on a phone', () => {
    expect(partShown(parts, true)).toEqual({ shown: [parts[0]], more: 3 })
  })
  it('shows all of them on a wider screen', () => {
    expect(partShown(parts, false)).toEqual({ shown: parts, more: 0 })
  })
  it('leaves out the page the list is on', () => {
    expect(partShown([parts[2]], true, 'goal-g')).toEqual({ shown: [], more: 0 })
    expect(partShown(parts.slice(2), true, 'goal-g')).toEqual({ shown: [parts[3]], more: 0 })
  })
  it('takes no parts', () => {
    expect(partShown(null, true)).toEqual({ shown: [], more: 0 })
  })
  it('links each kind to its page', () => {
    expect(partLink(parts[3])).toEqual({ to: '/tags/$tag', params: { tag: 'summer' } })
    expect(partLink(parts[2]).to).toBe('/goals/$id')
    expect(partLink(parts[1]).to).toBe('/obligations/$id')
    expect(partLink(parts[0]).to).toBe('/docs/$id')
    expect(partLabel(parts[3])).toBe('#summer')
  })
})

describe('theme review', () => {
  const ids = ['a', 'b', 'c']
  it('waits until every item is decided', () => {
    const m = mark(new Map(), 'a', { mark: 'drop' })
    expect(left(m, ids)).toEqual(['b', 'c'])
    expect(confirmText(m, ids)).toBe('2 items left to decide.')
  })
  it('drops or keeps the rest without changing what is decided', () => {
    const m = markRest(mark(new Map(), 'a', { mark: 'keep', area: 'area-home' }), ids, 'drop')
    expect(left(m, ids)).toEqual([])
    expect(tally(m, ids)).toEqual({ drop: 2, keep: 0, move: 1 })
    expect(confirmText(m, ids)).toBe('Confirm will drop 2, move 1.')
    expect(markRest(m, ids, 'keep').get('b')).toEqual({ mark: 'drop' })
  })
  it('builds one theme_close body in list order', () => {
    let m = mark(new Map(), 'c', { mark: 'drop' })
    m = mark(m, 'a', { mark: 'keep' })
    m = mark(m, 'b', { mark: 'keep', area: 'area-home' })
    expect(
      themeCloseBody('goal-g', { kind: 'close', outcome: 'dropped', reason: ' Off. ' }, m, ids),
    ).toEqual({
      theme: 'goal-g',
      outcome: 'dropped',
      reason: 'Off.',
      drop: ['c'],
      keep: [{ id: 'a' }, { id: 'b', area: 'area-home' }],
    })
    const change = { id: 'obligation-t', action: 'cancel', reason: 'Off.' }
    expect(themeCloseBody('obligation-t', { kind: 'change', change }, m, ids).change).toBe(change)
    expect(themeCloseBody('question-q', { kind: 'closed' }, m, ids).outcome).toBeUndefined()
  })
  it('says what Confirm did', () => {
    const items = [
      { op: 'task_resolve', state: 'applied' },
      { op: 'doc_edit', state: 'applied' },
      { op: 'task_resolve', state: 'refused' },
    ]
    expect(reviewDoneText('Trip', true, { refused: 1, items })).toBe(
      'Closed Trip; dropped 1; moved 1; 1 refused',
    )
    expect(reviewDoneText('Q', false, { refused: 0, items: [] })).toBe('Reviewed Q')
  })
  it('opens on a goal, question, or obligation; a meeting only when cancelled', () => {
    expect(reviewsOnClose('goal')).toBe(true)
    expect(reviewsOnClose('question', 'dropped')).toBe(true)
    expect(reviewsOnClose('meeting')).toBe(false)
    expect(reviewsOnClose('meeting', 'dropped')).toBe(true)
    expect(reviewsOnClose('task')).toBe(false)
  })
})
