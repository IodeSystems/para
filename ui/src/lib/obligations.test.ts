import { describe, expect, it } from 'vitest'
import {
  changeBody,
  localToRFC3339,
  matchesFilter,
  parseFilter,
  scoreWords,
  tellWords,
  type ChangeForm,
} from '@/lib/obligations'

const form = (p: Partial<ChangeForm>): ChangeForm => ({
  action: 'push',
  meeting: false,
  due: '',
  start: '',
  end: '',
  to: '',
  outcome: '',
  reason: '',
  because: '',
  notify: true,
  ...p,
})

describe('filters', () => {
  it('reads the address', () => {
    expect(parseFilter('meetings')).toBe('meetings')
    expect(parseFilter('x')).toBeUndefined()
  })
  it('sorts meetings, promises, and recurring ones', () => {
    const meeting = { kind: 'meeting', schedule: 'at' }
    const series = { kind: 'meeting', schedule: 'series', recur: 'weekly' }
    const promise = { kind: 'commitment', schedule: 'once' }
    const artifact = { kind: 'commitment', schedule: 'series', agent: true }
    expect(matchesFilter(meeting, 'meetings')).toBe(true)
    expect(matchesFilter(series, 'meetings')).toBe(true)
    expect(matchesFilter(series, 'recurring')).toBe(true)
    expect(matchesFilter(promise, 'promises')).toBe(true)
    expect(matchesFilter(promise, 'recurring')).toBe(false)
    expect(matchesFilter(artifact, 'promises')).toBe(false)
    expect(matchesFilter(artifact, 'recurring')).toBe(true)
    expect(matchesFilter(meeting, 'promises')).toBe(false)
    expect(matchesFilter(null, 'all')).toBe(true)
  })
})

describe('change dialog', () => {
  it('needs a reason or a higher-priority item', () => {
    expect(changeBody('o', form({ due: '2026-11-12' })).error).toMatch(/Say why/)
    expect(changeBody('o', form({ due: '2026-11-12', because: 'task-x' })).body).toEqual({
      id: 'o',
      action: 'push',
      due: '2026-11-12',
      because: 'task-x',
      notify: true,
    })
  })
  it('pushes a meeting by its start, in the browser zone', () => {
    const r = changeBody(
      'm',
      form({ meeting: true, start: '2026-11-10T14:00', end: '2026-11-10T15:00', reason: 'Clash' }),
      () => 300,
    )
    expect(r.body?.start).toBe('2026-11-10T14:00:00-05:00')
    expect(r.body?.end).toBe('2026-11-10T15:00:00-05:00')
    expect(changeBody('m', form({ meeting: true, reason: 'x' })).error).toMatch(/start/)
  })
  it('redirect needs to; cancel keeps its outcome; tell them can be unticked', () => {
    expect(changeBody('o', form({ action: 'redirect', reason: 'x' })).error).toMatch(/who has it/)
    expect(
      changeBody('o', form({ action: 'redirect', to: ' person-bo ', reason: 'x' })).body?.to,
    ).toBe('person-bo')
    const c = changeBody(
      'o',
      form({ action: 'cancel', outcome: 'dropped', reason: 'Not needed', notify: false }),
    ).body
    expect(c).toEqual({
      id: 'o',
      action: 'cancel',
      outcome: 'dropped',
      reason: 'Not needed',
      notify: false,
    })
  })
  it('writes RFC 3339 offsets', () => {
    expect(localToRFC3339('2026-11-10T09:30', -60)).toBe('2026-11-10T09:30:00+01:00')
    expect(localToRFC3339('2026-11-10', 0)).toBe('')
  })
})

describe('words', () => {
  it('groups who is told by medium', () => {
    expect(
      tellWords([
        { name: 'Ada', medium: 'email' },
        { name: 'Bo', medium: 'slack' },
        { name: 'Dee', medium: 'email' },
      ]),
    ).toBe('Ada, Dee (email); Bo (slack)')
  })
  it('says the 30-day numbers', () => {
    expect(scoreWords({ days: 30, kept: 4, renegotiated: 1, broken: 0 })).toBe(
      'Last 30 days: 4 kept, 1 renegotiated, 0 broken',
    )
  })
})
