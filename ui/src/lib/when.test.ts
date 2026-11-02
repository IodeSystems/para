import { describe, expect, it } from 'vitest'
import { humanTime, splitTimes } from '@/lib/when'

describe('humanTime', () => {
  const now = Date.parse('2026-11-02T12:00:00Z')
  it('says the last day in words', () => {
    expect(humanTime('2026-11-02T11:59:50Z', now)).toBe('just now')
    expect(humanTime('2026-11-02T11:30:00Z', now)).toBe('30 minutes ago')
    expect(humanTime('2026-11-02T10:00:00Z', now)).toBe('2 hours ago')
    expect(humanTime('2026-11-02T11:00:00Z', now)).toBe('1 hour ago')
  })
  it('says older times as a local date and time', () => {
    expect(humanTime('2026-10-20T08:05:00Z', now)).toMatch(/^Oct (19|20), \d\d:\d\d$/)
    expect(humanTime('2025-03-01T12:00:00Z', now)).toMatch(/^(Feb 28|Mar 1), 2025, \d\d:\d\d$/)
  })
  it('keeps a bare date as a date', () => {
    expect(humanTime('2026-11-03', now)).toBe('Nov 3')
    expect(humanTime('2025-11-03', now)).toBe('Nov 3, 2025')
  })
  it('gives back what is not a time', () => {
    expect(humanTime('soon', now)).toBe('soon')
  })
})

describe('splitTimes', () => {
  it('finds timestamps in text', () => {
    expect(splitTimes('2026-11-02T07:09:06Z opened sev2')).toEqual([
      { text: '2026-11-02T07:09:06Z', time: true },
      { text: ' opened sev2', time: false },
    ])
    expect(splitTimes('no time here')).toEqual([{ text: 'no time here', time: false }])
  })
})
