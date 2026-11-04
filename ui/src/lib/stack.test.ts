import { describe, expect, it } from 'vitest'
import { stackKeyFor } from '@/lib/keys'
import { canStackDecide, nextItem, rowKey, STACK_STATES, stackLines } from '@/lib/stack'

const row = (
  proposal: string,
  item: string,
  group?: string,
  kind = 'message',
  stack = 'proposed',
) => ({
  proposal,
  title: `Dossier ${proposal}`,
  item,
  kind,
  stack,
  group,
})

describe('review stack', () => {
  it('has the stack states in the order para gives them', () => {
    expect(STACK_STATES).toEqual(['proposed', 'decided', 'executing', 'done', 'failed', 'closed'])
  })
  it('groups rows by dossier, then group, loose items first', () => {
    const rows = [row('p1', 'a', 'g1'), row('p2', 'x'), row('p1', 'b'), row('p1', 'c', 'g1')]
    const lines = stackLines(rows, [
      { proposal: 'p1', group: 'g1', done: 1, total: 3, proposed: 2 },
    ])
    expect(lines.map((l) => l.key)).toEqual([
      'd:p1',
      'p1/b',
      'g:p1/g1',
      'p1/a',
      'p1/c',
      'd:p2',
      'p2/x',
    ])
    const g = lines.find((l) => l.type === 'group')
    expect(g).toMatchObject({ done: 1, total: 3, proposed: 2 })
    expect(lines[0]).toMatchObject({ type: 'dossier', count: 3 })
  })
  it('counts a group from the rows when the server has no progress for it', () => {
    const lines = stackLines([row('p1', 'a', 'g'), row('p1', 'b', 'g', 'message', 'done')], [])
    expect(lines.find((l) => l.type === 'group')).toMatchObject({ total: 2, proposed: 1, done: 0 })
  })
  it('moves the cursor over items only', () => {
    const lines = stackLines([row('p1', 'a'), row('p2', 'b')], [])
    expect(nextItem(lines, null, 1)).toBe('p1/a')
    expect(nextItem(lines, 'p1/a', 1)).toBe('p2/b')
    expect(nextItem(lines, 'p2/b', 1)).toBe('p2/b')
    expect(nextItem(lines, 'p2/b', -1)).toBe('p1/a')
    expect(nextItem(lines, null, -1)).toBe('p2/b')
    expect(nextItem([], null, 1)).toBeNull()
  })
  it('decides only open items that are not questions', () => {
    expect(canStackDecide({ kind: 'message', stack: 'proposed' })).toBe(true)
    expect(canStackDecide({ kind: 'question', stack: 'proposed' })).toBe(false)
    expect(canStackDecide({ kind: 'para', stack: 'done' })).toBe(false)
    expect(rowKey({ proposal: 'p', item: 'i' })).toBe('p/i')
  })
})

describe('stack keys', () => {
  const press = (key: string, extra: Partial<Parameters<typeof stackKeyFor>[0]> = {}) =>
    stackKeyFor({ key, ctrlKey: false, metaKey: false, altKey: false, target: null, ...extra })
  it('maps the letters', () => {
    expect(press('j')).toBe('down')
    expect(press('k')).toBe('up')
    expect(press('a')).toBe('approve')
    expect(press('A')).toBe('approveGroup')
    expect(press('r')).toBe('reject')
    expect(press('e')).toBe('edit')
    expect(press('c')).toBe('comment')
    expect(press('x')).toBe('select')
    expect(press('o')).toBe('open')
    expect(press('z')).toBeNull()
  })
  it('does nothing in a field or with a modifier', () => {
    expect(press('a', { target: { tagName: 'TEXTAREA' } })).toBeNull()
    expect(press('a', { ctrlKey: true })).toBeNull()
  })
})
