import { describe, expect, it } from 'vitest'
import { meetingWhen } from '@/lib/meeting'

describe('meetingWhen', () => {
  it('shows a booked time as one range on one day', () => {
    const s = new Date(2026, 10, 4, 10, 0).toISOString()
    const e = new Date(2026, 10, 4, 11, 30).toISOString()
    expect(meetingWhen(s, e)).toBe('Nov 4, 10:00-11:30')
  })
  it('names both days when the meeting crosses midnight', () => {
    const s = new Date(2026, 10, 4, 23, 0).toISOString()
    const e = new Date(2026, 10, 5, 1, 0).toISOString()
    expect(meetingWhen(s, e)).toBe('Nov 4, 23:00 to Nov 5, 01:00')
  })
  it('says an unbooked meeting is not booked', () => {
    expect(meetingWhen(null, null)).toBe('not booked')
  })
})
