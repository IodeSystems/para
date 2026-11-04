import { describe, expect, it } from 'vitest'
import { commentState, diffLines, markOf, runs } from '@/lib/docs'

describe('tracked document marks', () => {
  it('cuts text at marks, by code point', () => {
    expect(runs('café au lait', [{ id: 'c1', start: 5, end: 7 }])).toEqual([
      { text: 'café ', ids: [] },
      { text: 'au', ids: ['c1'] },
      { text: ' lait', ids: [] },
    ])
  })
  it('lists both marks where they overlap and clips one past the end', () => {
    const r = runs('abcdef', [
      { id: 'c1', start: 0, end: 4 },
      { id: 'c2', start: 2, end: 99 },
    ])
    expect(r).toEqual([
      { text: 'ab', ids: ['c1'] },
      { text: 'cd', ids: ['c1', 'c2'] },
      { text: 'ef', ids: ['c2'] },
    ])
  })
  it('places a comment by its anchor in a later version, or not when lost', () => {
    const c = { id: 'c1', version: 1, start: 0, end: 3, anchor: { version: 2, start: 4, end: 7 } }
    expect(markOf(c, 1)).toEqual({ id: 'c1', start: 0, end: 3 })
    expect(markOf(c, 2)).toEqual({ id: 'c1', start: 4, end: 7 })
    expect(markOf({ ...c, anchor: { version: 2, lost: true } }, 2)).toBeNull()
    expect(markOf(c, 3)).toBeNull()
  })
  it('names a comment state', () => {
    expect(commentState({ resolution: 'became' })).toBe('became an item')
    expect(commentState({ lost: true })).toBe('open, quote gone')
  })
})

describe('diff lines', () => {
  it('prefixes each line of a part and writes a gap', () => {
    expect(
      diffLines([
        { op: 'gap', text: '3 unchanged' },
        { op: 'same', text: 'a' },
        { op: 'del', text: 'b\nc' },
        { op: 'add', text: 'd' },
      ]),
    ).toEqual(['@@ 3 unchanged @@', '  a', '- b', '- c', '+ d'])
  })
})
