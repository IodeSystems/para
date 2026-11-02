import { describe, expect, it } from 'vitest'
import { dueLabel, dueState } from '@/lib/due'

describe('due dates', () => {
  it('compares against today', () => {
    expect(dueState('2026-10-31', '2026-11-01')).toBe('overdue')
    expect(dueState('2026-11-01', '2026-11-01')).toBe('today')
    expect(dueState('2026-11-04', '2026-11-01')).toBe('soon')
    expect(dueState('2026-11-05', '2026-11-01')).toBe('later')
  })
  it('crosses a month end', () => {
    expect(dueState('2026-12-01', '2026-11-29')).toBe('soon')
    expect(dueState('2027-01-02', '2026-12-30')).toBe('soon')
  })
  it('labels', () => {
    expect(dueLabel('2026-10-31', '2026-11-01')).toBe('overdue 2026-10-31')
    expect(dueLabel('2026-11-01', '2026-11-01')).toBe('due today')
    expect(dueLabel('2026-11-20', '2026-11-01')).toBe('due 2026-11-20')
  })
})
