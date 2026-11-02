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

describe('opPhrase', () => {
  it('says ops in words', async () => {
    const { opPhrase, needWords } = await import('@/lib/events')
    expect(opPhrase('task_add')).toBe('Added task')
    expect(opPhrase('rfx_reply')).toBe('Recorded a reply to ask')
    expect(opPhrase('project_add')).toBe('Added goal')
    expect(opPhrase('request_add')).toBe('Added agent work')
    expect(opPhrase('question_accept')).toBe('Accepted question')
    expect(opPhrase('external_edit')).toBe('Edited outside para')
    expect(opPhrase('mystery_thing')).toBe('mystery thing')
    expect(needWords('draft RFX')).toBe('Message ready to send')
  })
})
