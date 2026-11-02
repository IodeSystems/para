import { describe, expect, it } from 'vitest'
import { highlight, readable } from '@/lib/highlight'

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

describe('readable', () => {
  it('drops table pipes and rules', () => {
    expect(readable('| Name | Owner |\n|---|---|\n| Atlas | Priya |')).not.toContain('|')
    expect(readable('Atlas | Priya')).toBe('Atlas Priya')
  })
  it('shows a timestamp local, in words', () => {
    const now = Date.parse('2026-11-02T08:00:00Z')
    expect(readable('Rolled back at 2026-11-02T07:53:16Z', now)).toBe(
      'Rolled back at 6 minutes ago',
    )
  })
})
