import { describe, expect, it } from 'vitest'
import { highlight } from '@/lib/highlight'

describe('highlight', () => {
  it('cuts a snippet at the marks', () => {
    expect(highlight('Logs are kept', [{ start: 0, end: 4 }])).toEqual([
      { text: 'Logs', mark: true },
      { text: ' are kept', mark: false },
    ])
  })
  it('counts code points, not UTF-16 units', () => {
    expect(highlight('😀 log', [{ start: 2, end: 5 }])).toEqual([
      { text: '😀 ', mark: false },
      { text: 'log', mark: true },
    ])
  })
  it('skips bad and overlapping spans', () => {
    expect(
      highlight('abcdef', [
        { start: 4, end: 2 },
        { start: 1, end: 3 },
        { start: 2, end: 5 },
      ]).map((p) => p.text),
    ).toEqual(['a', 'bc', 'def'])
  })
  it('passes through with no marks', () => {
    expect(highlight('plain', null)).toEqual([{ text: 'plain', mark: false }])
    expect(highlight('', [])).toEqual([])
  })
})
