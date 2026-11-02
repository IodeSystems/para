import { describe, expect, it } from 'vitest'
import { ago, newestTS, transition } from '@/lib/events'

describe('events', () => {
  it('finds the newest timestamp across zones', () => {
    expect(newestTS([{ ts: '2026-11-01T10:00:00Z' }, { ts: '2026-11-01T12:00:00+03:00' }])).toBe(
      '2026-11-01T10:00:00Z',
    )
    expect(newestTS([])).toBe('')
  })
  it('describes a transition', () => {
    expect(transition({ from: 'open', to: 'done' })).toBe('open to done')
    expect(transition({ from: '', to: 'draft' })).toBe('- to draft')
    expect(transition({ from: '', to: '' })).toBe('')
  })
  it('says how long ago', () => {
    const now = Date.parse('2026-11-01T12:00:00Z')
    expect(ago('2026-11-01T11:59:30Z', now)).toBe('just now')
    expect(ago('2026-11-01T11:00:00Z', now)).toBe('1 h ago')
    expect(ago('2026-10-30T12:00:00Z', now)).toBe('2 d ago')
    expect(ago('bad', now)).toBe('')
  })
})
