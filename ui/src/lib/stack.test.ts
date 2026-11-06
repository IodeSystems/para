import { describe, expect, it } from 'vitest'
import { laterDates } from '@/lib/stack'

describe('later dates', () => {
  it('offers tomorrow and the next Monday', () => {
    // Thursday 2026-11-05
    expect(laterDates(new Date(2026, 10, 5, 9))).toEqual({
      tomorrow: '2026-11-06',
      nextWeek: '2026-11-09',
    })
    // A Monday goes to the Monday after.
    expect(laterDates(new Date(2026, 10, 9, 9))).toEqual({
      tomorrow: '2026-11-10',
      nextWeek: '2026-11-16',
    })
    // Month end.
    expect(laterDates(new Date(2026, 10, 30, 9)).tomorrow).toBe('2026-12-01')
  })
})
