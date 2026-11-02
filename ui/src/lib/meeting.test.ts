import { describe, expect, it } from 'vitest'
import { meetingWhen, questionIds, targetStates, toLocalInput, toRFC3339 } from '@/lib/meeting'

describe('toRFC3339', () => {
  it('writes the viewer zone offset for that date', () => {
    const out = toRFC3339('2026-11-04T10:00')
    expect(out).toMatch(/^2026-11-04T10:00:00[+-]\d\d:\d\d$/)
    // The same instant read back is the wall clock that was typed.
    const t = new Date(out)
    expect(t.getHours()).toBe(10)
    expect(t.getDate()).toBe(4)
  })
  it('round-trips through the local input value', () => {
    expect(toLocalInput(toRFC3339('2026-07-01T23:30'))).toBe('2026-07-01T23:30')
  })
  it('rejects what is not a time', () => {
    expect(toRFC3339('')).toBe('')
    expect(toRFC3339('tomorrow')).toBe('')
    expect(toLocalInput(null)).toBe('')
    expect(toLocalInput('x')).toBe('')
  })
})

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

describe('targetStates', () => {
  it('offers requested and scheduled for a meeting only', () => {
    expect(targetStates('meeting')).toEqual(expect.arrayContaining(['requested', 'scheduled']))
    expect(targetStates('email')).not.toContain('requested')
    expect(targetStates('email')).not.toContain('scheduled')
    expect(targetStates('email')).toContain('sent')
  })
})

describe('questionIds', () => {
  it('lists linked questions once, in order', () => {
    const body =
      '## Questions\n\n- [[question-a1]] one\n- [[question-b2|Two]]\n- [[question-a1]]\n- [[task-x]]'
    expect(questionIds(body)).toEqual(['question-a1', 'question-b2'])
  })
})
